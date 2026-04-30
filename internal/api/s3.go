// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jc-lab/backupgate/internal/config"
	"github.com/jc-lab/backupgate/internal/pipeline"
)

// S3Handler handles S3-compatible API requests.
type S3Handler struct {
	handler *Handler

	mu       sync.RWMutex
	sessions map[string]*multipartSession
}

var _ http.Handler = (*S3Handler)(nil)

func NewS3Handler(h *Handler) *S3Handler {
	return &S3Handler{
		handler:  h,
		sessions: make(map[string]*multipartSession),
	}
}

// ServeHTTP routes S3 requests based on method and query parameters.
func (s *S3Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	areq := NewAwsRequest(r)

	w.Header().Set("x-amz-request-id", areq.RequestId)

	query := r.URL.Query()

	if _, ok := query["location"]; ok && r.Method == http.MethodGet {
		s.getBucketLocation(w, r)
		return
	}

	chain, ok := s.handler.authChain(areq.Key)
	if !ok {
		writeS3Error(w, http.StatusBadRequest, "InvalidRequest", "no auth chain for key")
	}
	if _, err := areq.Authenticate(chain); err != nil {
		writeS3Error(w, http.StatusForbidden, "SignatureDoesNotMatch", err.Error())
		return
	}

	keyCfg, ok := s.handler.cfg.ResolveKeyConfig(areq.Key)
	if !ok {
		writeS3Error(w, http.StatusNotFound, "NoSuchKey", "unknown key")
		return
	}
	areq.KeyCfg = keyCfg

	if _, ok := query["uploads"]; ok && r.Method == http.MethodPost {
		s.createMultipartUpload(areq, w)
		return
	}

	if uploadID := query.Get("uploadId"); uploadID != "" {
		switch r.Method {
		case http.MethodPut:
			s.uploadPart(areq, w, uploadID, query.Get("partNumber"))
			return
		case http.MethodPost:
			s.completeMultipartUpload(areq, w, uploadID)
			return
		case http.MethodDelete:
			s.abortMultipartUpload(areq, w, uploadID)
			return
		}
	}

	if r.Method == http.MethodPut {
		s.putObject(areq, w)
		return
	}

	writeS3Error(w, http.StatusMethodNotAllowed, "MethodNotAllowed", "only PUT is supported for single uploads")
}

// putObject handles S3 PUT object (single upload).
func (s *S3Handler) putObject(areq *AwsRequest, w http.ResponseWriter) {
	mu := s.handler.acquireKeyLock(areq.Key)
	defer mu.Unlock()

	keyCfg, ok := s.handler.cfg.ResolveKeyConfig(areq.Key)
	if !ok {
		writeS3Error(w, http.StatusNotFound, "NoSuchKey", "unknown key")
		return
	}
	body := pipeline.NewStreamingUploadReader(areq.GetBody())
	if keyCfg.BufferMode != config.BufferModeOff {
		buf, err := s.handler.pipeline.NewUploadReader(areq.Key, areq.ContentLength)
		if err != nil {
			writeS3Error(w, http.StatusInternalServerError, "InternalError", err.Error())
			return
		}
		defer buf.Close()
		if _, err := io.Copy(buf, areq.GetBody()); err != nil {
			writeS3Error(w, http.StatusBadRequest, "InvalidRequest", "reading request body: "+err.Error())
			return
		}
		body = buf
	}

	result, err := s.handler.pipeline.Process(areq.Request.Context(), areq.Key, body, pipeline.UploadMetadata{
		ContentLength:  areq.Request.ContentLength,
		ExpectedSHA256: areq.ExpectedContentSha256,
	})
	if err != nil {
		status := http.StatusInternalServerError
		code := "InternalError"
		if errors.Is(err, pipeline.ErrVerificationFailed) {
			status = http.StatusConflict
			code = "BadDigest"
		}
		writeS3Error(w, status, code, err.Error())
		return
	}

	w.Header().Set("ETag", `"`+result.MD5+`"`)
	w.WriteHeader(http.StatusOK)
}

func (s *S3Handler) getBucketLocation(w http.ResponseWriter, r *http.Request) {
	_ = s
	_ = r
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)

	data := NewBucketLocationResult()
	_, _ = w.Write([]byte(xmlHeader))
	_ = xml.NewEncoder(w).Encode(data)
}

func parseAuthFields(s string) map[string]string {
	fields := make(map[string]string)
	for _, part := range strings.Split(s, ",") {
		key, val, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok {
			fields[key] = val
		}
	}
	return fields
}

func canonicalRequest(r *http.Request, signedHeaders []string) string {
	var canonicalHeaders strings.Builder
	for _, name := range signedHeaders {
		lower := strings.ToLower(name)
		value := strings.Join(r.Header.Values(lower), ",")
		if lower == "host" {
			value = r.Host
		}
		canonicalHeaders.WriteString(lower)
		canonicalHeaders.WriteByte(':')
		canonicalHeaders.WriteString(strings.Join(strings.Fields(value), " "))
		canonicalHeaders.WriteByte('\n')
	}
	return strings.Join([]string{
		r.Method,
		canonicalURI(r.URL.EscapedPath()),
		canonicalQuery(r.URL.Query()),
		canonicalHeaders.String(),
		strings.Join(signedHeaders, ";"),
		r.Header.Get("x-amz-content-sha256"),
	}, "\n")
}

func canonicalURI(p string) string {
	if p == "" {
		return "/"
	}
	return p
}

func canonicalQuery(v url.Values) string {
	var parts []string
	for key, vals := range v {
		sort.Strings(vals)
		for _, val := range vals {
			parts = append(parts, awsEscape(key)+"="+awsEscape(val))
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, "&")
}

func awsEscape(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

func sigV4SigningKey(secret, date, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), []byte(date))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	return hmacSHA256(kService, []byte("aws4_request"))
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func normalizeS3PayloadHash(v string) string {
	if v == "" || v == "UNSIGNED-PAYLOAD" || strings.HasPrefix(v, "STREAMING-") {
		return ""
	}
	return v
}

func writeS3Error(w http.ResponseWriter, status int, code, message string) {
	level := slog.LevelWarn
	if status >= http.StatusInternalServerError {
		level = slog.LevelError
	}
	slog.LogAttrs(
		context.Background(),
		level,
		"s3 api error",
		slog.Int("status", status),
		slog.String("code", code),
		slog.String("message", message),
	)
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(xmlHeader))
	_ = xml.NewEncoder(w).Encode(S3ErrorResult{Code: code, Message: message})
}

func newS3RequestID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return hex.EncodeToString([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:])
}
