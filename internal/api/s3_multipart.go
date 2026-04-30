// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"sync"

	"github.com/jc-lab/backupgate/internal/config"
	"github.com/jc-lab/backupgate/internal/pipeline"
	"github.com/jc-lab/backupgate/internal/reader"
)

// multipartSession tracks the state of an in-progress multipart upload.
type multipartSession struct {
	key      string
	uploadID string
	parts    map[int]*multipartPart
	mu       sync.Mutex
}

// multipartPart holds a single part's buffered data and metadata.
type multipartPart struct {
	partNumber int
	size       int64
	etag       string
	buffer     reader.UploadReader
}

// createMultipartUpload handles POST ?uploads (CreateMultipartUpload).
func (s *S3Handler) createMultipartUpload(areq *AwsRequest, w http.ResponseWriter) {
	if areq.KeyCfg.BufferMode != config.BufferModeFullyComplete {
		writeS3Error(w, http.StatusBadRequest, "InvalidRequest", "multipart upload requires FULLY_COMPLETE buffer mode")
		return
	}

	uploadID, err := randomUploadID()
	if err != nil {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	if _, err = io.Copy(io.Discard, areq.GetBody()); err != nil {
		writeS3Error(w, http.StatusBadRequest, "InvalidRequest", "reading request body: "+err.Error())
		return
	}
	if !areq.ValidateContentHash() {
		writeS3Error(w, http.StatusBadRequest, "InvalidRequest", "content hash mismatch")
		return
	}

	session := &multipartSession{
		key:      areq.Key,
		uploadID: uploadID,
		parts:    make(map[int]*multipartPart),
	}
	s.mu.Lock()
	s.sessions[uploadID] = session
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/xml")
	_ = xml.NewEncoder(w).Encode(struct {
		XMLName  xml.Name `xml:"InitiateMultipartUploadResult"`
		Bucket   string   `xml:"Bucket"`
		Key      string   `xml:"Key"`
		UploadID string   `xml:"UploadId"`
	}{Bucket: "backupgate", Key: areq.Key, UploadID: uploadID})
}

// uploadPart handles PUT ?partNumber=N&uploadId=X (UploadPart).
func (s *S3Handler) uploadPart(areq *AwsRequest, w http.ResponseWriter, uploadID string, partNumber string) {
	session := s.getMultipartSession(uploadID)
	if session == nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchUpload", "uploadId not found")
		return
	}
	n, err := strconv.Atoi(partNumber)
	if err != nil || n <= 0 {
		writeS3Error(w, http.StatusBadRequest, "InvalidArgument", "invalid partNumber")
		return
	}

	buf, err := s.handler.pipeline.NewUploadReader(areq.Key, areq.ContentLength)
	if err != nil {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	hash := md5.New()
	size, err := io.Copy(io.MultiWriter(buf, hash), areq.GetBody())
	if err != nil {
		buf.Close()
		writeS3Error(w, http.StatusBadRequest, "InvalidRequest", err.Error())
		return
	}
	if !areq.ValidateContentHash() {
		writeS3Error(w, http.StatusBadRequest, "InvalidRequest", "content hash mismatch")
		return
	}
	etag := hex.EncodeToString(hash.Sum(nil))

	session.mu.Lock()
	if old := session.parts[n]; old != nil {
		old.buffer.Close()
	}
	session.parts[n] = &multipartPart{partNumber: n, size: size, etag: etag, buffer: buf}
	session.mu.Unlock()

	w.Header().Set("ETag", `"`+etag+`"`)
	w.WriteHeader(http.StatusOK)
}

// completeMultipartUpload handles POST ?uploadId=X (CompleteMultipartUpload).
func (s *S3Handler) completeMultipartUpload(areq *AwsRequest, w http.ResponseWriter, uploadID string) {
	session := s.getMultipartSession(uploadID)
	if session == nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchUpload", "uploadId not found")
		return
	}
	partNumbers, err := requestedPartNumbers(areq, session)
	if err != nil {
		writeS3Error(w, http.StatusBadRequest, "InvalidPart", err.Error())
		return
	}
	if !areq.ValidateContentHash() {
		writeS3Error(w, http.StatusBadRequest, "InvalidRequest", "content hash mismatch")
		return
	}

	mu := s.handler.acquireKeyLock(session.key)
	defer mu.Unlock()

	var total int64
	partReaders := make([]reader.UploadReader, 0, len(partNumbers))
	session.mu.Lock()
	for _, n := range partNumbers {
		part := session.parts[n]
		if part == nil {
			session.mu.Unlock()
			writeS3Error(w, http.StatusBadRequest, "InvalidPart", fmt.Sprintf("missing part %d", n))
			return
		}
		partReaders = append(partReaders, part.buffer)
		total += part.size
	}
	session.mu.Unlock()

	assembled := reader.NewAssembledReader(partReaders)
	defer assembled.Close()

	result, err := s.handler.pipeline.Process(areq.Context(), session.key, assembled, pipeline.UploadMetadata{
		ContentLength:  total,
		ExpectedSHA256: "",
	})
	if err != nil {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	s.deleteMultipartSession(uploadID)
	s.closeMultipartParts(session)

	w.Header().Set("Content-Type", "application/xml")
	_ = xml.NewEncoder(w).Encode(struct {
		XMLName  xml.Name `xml:"CompleteMultipartUploadResult"`
		Location string   `xml:"Location"`
		Bucket   string   `xml:"Bucket"`
		Key      string   `xml:"Key"`
		ETag     string   `xml:"ETag"`
	}{Location: result.StoragePath, Bucket: "backupgate", Key: session.key, ETag: `"` + result.MD5 + `"`})
}

// abortMultipartUpload handles DELETE ?uploadId=X (AbortMultipartUpload).
func (s *S3Handler) abortMultipartUpload(areq *AwsRequest, w http.ResponseWriter, uploadID string) {
	session := s.getMultipartSession(uploadID)
	if session == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	s.deleteMultipartSession(uploadID)
	s.closeMultipartParts(session)
	w.WriteHeader(http.StatusNoContent)
}

func (s *S3Handler) getMultipartSession(uploadID string) *multipartSession {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessions[uploadID]
}

func (s *S3Handler) deleteMultipartSession(uploadID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, uploadID)
}

func (s *S3Handler) closeMultipartParts(session *multipartSession) {
	session.mu.Lock()
	defer session.mu.Unlock()
	for _, part := range session.parts {
		part.buffer.Close()
	}
}

func requestedPartNumbers(areq *AwsRequest, session *multipartSession) ([]int, error) {
	var req struct {
		Parts []struct {
			PartNumber int `xml:"PartNumber"`
		} `xml:"Part"`
	}
	if body := areq.GetBody(); body != nil {
		_ = xml.NewDecoder(body).Decode(&req)
	}
	var nums []int
	if len(req.Parts) > 0 {
		for _, p := range req.Parts {
			nums = append(nums, p.PartNumber)
		}
	} else {
		session.mu.Lock()
		for n := range session.parts {
			nums = append(nums, n)
		}
		session.mu.Unlock()
	}
	sort.Ints(nums)
	return nums, nil
}

func randomUploadID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
