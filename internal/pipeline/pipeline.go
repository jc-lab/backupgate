// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package pipeline

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"path"
	"strings"
	"time"

	"github.com/jc-lab/backupgate/internal/buffer"
	"github.com/jc-lab/backupgate/internal/config"
	"github.com/jc-lab/backupgate/internal/reader"
	"github.com/jc-lab/backupgate/internal/rotation"
	"github.com/jc-lab/backupgate/internal/storage"
)

// UploadMetadata holds metadata from the incoming request.
type UploadMetadata struct {
	ContentLength  int64
	ExpectedSHA256 string // hex-encoded SHA256 from request header
}

// UploadResult holds the outcome of a completed upload.
type UploadResult struct {
	Key         string
	Size        int64
	SHA256      string
	MD5         string
	StoragePath string
	Verified    bool
}

// Pipeline orchestrates the upload flow: hashing, buffering, storage, verification, rotation.
type Pipeline struct {
	cfg        *config.Config
	bufMgr     *buffer.Manager
	backend    storage.Backend
	rotMgr     *rotation.Manager
	maxRetries int
}

// NewPipeline creates a new upload pipeline.
func NewPipeline(
	cfg *config.Config,
	bufMgr *buffer.Manager,
	backend storage.Backend,
	rotMgr *rotation.Manager,
) *Pipeline {
	return &Pipeline{
		cfg:        cfg,
		bufMgr:     bufMgr,
		backend:    backend,
		rotMgr:     rotMgr,
		maxRetries: 1,
	}
}

// Process handles the entire upload lifecycle for a given key.
func (p *Pipeline) Process(ctx context.Context, key string, body reader.UploadReader, meta UploadMetadata) (*UploadResult, error) {
	keyCfg, ok := p.cfg.ResolveKeyConfig(key)
	if !ok {
		return nil, fmt.Errorf("no configuration found for key %q", key)
	}

	switch keyCfg.BufferMode {
	case config.BufferModeFullyImmediately:
		return p.processFullyImmediately(ctx, key, keyCfg, body, meta)
	case config.BufferModeFullyComplete:
		return p.processFullyComplete(ctx, key, keyCfg, body, meta)
	case config.BufferModeOff:
		return p.processOff(ctx, key, keyCfg, body, meta)
	default:
		return nil, fmt.Errorf("unknown buffer mode %q for key %q", keyCfg.BufferMode, key)
	}
}

// NewUploadReader creates the request body buffer used by API handlers.
func (p *Pipeline) NewUploadReader(key string, expectedSize int64) (reader.UploadReader, error) {
	return p.bufMgr.NewFullBuffer(key, expectedSize)
}

// processFullyImmediately: receive + hash + buffer + upload in parallel.
// On disconnect, resume from buffer offset. On verification failure, re-upload from buffer (once).
func (p *Pipeline) processFullyImmediately(ctx context.Context, key string, keyCfg *config.KeyConfig, body reader.UploadReader, meta UploadMetadata) (*UploadResult, error) {
	return p.processBuffered(ctx, key, keyCfg, body, meta, false)
}

// processFullyComplete: receive + hash + buffer, then verify hash, then upload.
// On disconnect, resume from buffer offset. On verification failure, re-upload from buffer (once).
func (p *Pipeline) processFullyComplete(ctx context.Context, key string, keyCfg *config.KeyConfig, body reader.UploadReader, meta UploadMetadata) (*UploadResult, error) {
	return p.processBuffered(ctx, key, keyCfg, body, meta, true)
}

// processOff: receive + hash + stream directly to storage via sliding window.
// On disconnect, retry from sliding window if offset is available. On verification failure, return error.
func (p *Pipeline) processOff(ctx context.Context, key string, keyCfg *config.KeyConfig, body reader.UploadReader, meta UploadMetadata) (*UploadResult, error) {
	remotePath := generateStoragePath(key)
	hashingBody := newHashingUploadReader(body)
	if err := p.backend.Upload(ctx, remotePath, hashingBody, meta.ContentLength); err != nil {
		return nil, err
	}
	sha := hashingBody.SHA256()
	if meta.ExpectedSHA256 != "" && !strings.EqualFold(meta.ExpectedSHA256, sha) {
		return nil, fmt.Errorf("%w: expected %s, got %s", ErrVerificationFailed, meta.ExpectedSHA256, sha)
	}

	verified := false
	if keyCfg.Verification {
		expected := meta.ExpectedSHA256
		if expected == "" {
			expected = sha
		}
		var err error
		verified, err = p.verifyAndRetransmit(ctx, remotePath, expected, body, hashingBody.Size())
		if err != nil {
			return nil, err
		}
	}

	if err := p.applyRotation(ctx, rotationDir(key), keyCfg); err != nil {
		return nil, err
	}
	return &UploadResult{
		Key:         key,
		Size:        hashingBody.Size(),
		SHA256:      sha,
		MD5:         hashingBody.MD5(),
		StoragePath: remotePath,
		Verified:    verified,
	}, nil
}

// generateStoragePath creates the remote path for the backup file.
// Format: {dir of key}/{timestamp}_{filename}
func generateStoragePath(key string) string {
	cleanKey := strings.Trim(path.Clean("/"+key), "/")
	ts := time.Now().UTC().Format("20060102T150405Z")
	filename := path.Base(cleanKey)
	dir := rotationDir(cleanKey)
	if dir == "." {
		return ts + "_" + filename
	}
	return path.Join(dir, ts+"_"+filename)
}

func rotationDir(key string) string {
	cleanKey := strings.Trim(path.Clean("/"+key), "/")
	return path.Dir(cleanKey)
}

// uploadWithRetry attempts to upload and retries on connection failure.
func (p *Pipeline) uploadWithRetry(ctx context.Context, remotePath string, r reader.UploadReader, size int64) error {
	var lastErr error
	for attempt := 0; attempt <= p.maxRetries; attempt++ {
		if attempt == 0 {
			lastErr = p.backend.Upload(ctx, remotePath, r, size)
			if lastErr == nil {
				return nil
			}
		} else {
			offset, err := p.backend.RemoteSize(ctx, remotePath)
			if err != nil || offset < 0 || offset > size {
				offset = 0
			}
			lastErr = p.backend.ResumeUpload(ctx, remotePath, r, offset, size)
			if lastErr == nil {
				return nil
			}
		}
	}
	return fmt.Errorf("upload failed after retries: %w", lastErr)
}

// verifyAndRetransmit verifies the upload and retransmits from buffer if hash mismatches.
// Only applicable when verification is enabled and buffer is available.
func (p *Pipeline) verifyAndRetransmit(ctx context.Context, remotePath string, expectedHash string, r reader.UploadReader, size int64) (bool, error) {
	if expectedHash == "" {
		return false, nil
	}
	verifier := NewVerifier(p.backend)
	if err := verifier.Verify(ctx, remotePath, expectedHash); err == nil {
		return true, nil
	} else if !errors.Is(err, ErrVerificationFailed) {
		return false, err
	}

	if _, ok := r.(io.Seeker); !ok {
		return false, ErrVerificationFailed
	}
	if err := p.backend.Upload(ctx, remotePath, r, size); err != nil {
		return false, fmt.Errorf("retransmitting after verification failure: %w", err)
	}
	if err := verifier.Verify(ctx, remotePath, expectedHash); err != nil {
		return false, err
	}
	return true, nil
}

// applyRotation runs the rotation policy after a successful upload.
func (p *Pipeline) applyRotation(ctx context.Context, key string, keyCfg *config.KeyConfig) error {
	dur, err := keyCfg.Rotation.MaxAgeDuration()
	if err != nil {
		return fmt.Errorf("parsing rotation max_age: %w", err)
	}

	policy := rotation.Policy{
		MaxCount: keyCfg.Rotation.MaxCount,
		MaxAge:   dur,
	}

	return p.rotMgr.Apply(ctx, p.backend, key, policy)
}

func (p *Pipeline) processBuffered(ctx context.Context, key string, keyCfg *config.KeyConfig, body reader.UploadReader, meta UploadMetadata, rejectHashMismatch bool) (*UploadResult, error) {
	digests, err := hashUploadReader(body)
	if err != nil {
		return nil, err
	}
	if meta.ExpectedSHA256 != "" && !strings.EqualFold(meta.ExpectedSHA256, digests.SHA256) && rejectHashMismatch {
		return nil, fmt.Errorf("%w: expected %s, got %s", ErrVerificationFailed, meta.ExpectedSHA256, digests.SHA256)
	}

	remotePath := generateStoragePath(key)
	size := body.Size()
	if meta.ContentLength >= 0 && meta.ContentLength > size {
		size = meta.ContentLength
	}
	if err := p.uploadWithRetry(ctx, remotePath, body, size); err != nil {
		return nil, err
	}

	verified := false
	if keyCfg.Verification {
		expected := meta.ExpectedSHA256
		if expected == "" {
			expected = digests.SHA256
		}
		verified, err = p.verifyAndRetransmit(ctx, remotePath, expected, body, size)
		if err != nil {
			return nil, err
		}
	}

	if err := p.applyRotation(ctx, rotationDir(key), keyCfg); err != nil {
		return nil, err
	}
	return &UploadResult{
		Key:         key,
		Size:        size,
		SHA256:      digests.SHA256,
		MD5:         digests.MD5,
		StoragePath: remotePath,
		Verified:    verified,
	}, nil
}

type payloadDigests struct {
	SHA256 string
	MD5    string
}

func hashUploadReader(r reader.UploadReader) (payloadDigests, error) {
	if seeker, ok := r.(io.Seeker); ok {
		if _, err := seeker.Seek(0, io.SeekStart); err != nil {
			return payloadDigests{}, fmt.Errorf("seeking upload reader: %w", err)
		}
	}
	shaHasher := sha256.New()
	md5Hasher := md5.New()
	if _, err := io.Copy(io.MultiWriter(shaHasher, md5Hasher), r); err != nil {
		return payloadDigests{}, fmt.Errorf("hashing upload payload: %w", err)
	}
	if seeker, ok := r.(io.Seeker); ok {
		if _, err := seeker.Seek(0, io.SeekStart); err != nil {
			return payloadDigests{}, fmt.Errorf("resetting upload reader: %w", err)
		}
	}
	return payloadDigests{
		SHA256: hex.EncodeToString(shaHasher.Sum(nil)),
		MD5:    hex.EncodeToString(md5Hasher.Sum(nil)),
	}, nil
}

type streamingUploadReader struct {
	r    io.ReadCloser
	size int64
}

// NewStreamingUploadReader adapts an incoming request body for OFF mode.
func NewStreamingUploadReader(r io.ReadCloser) reader.UploadReader {
	return &streamingUploadReader{r: r}
}

func (r *streamingUploadReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	r.size += int64(n)
	return n, err
}

func (r *streamingUploadReader) Write(_ []byte) (int, error) {
	return 0, fmt.Errorf("streaming upload reader is read-only")
}

func (r *streamingUploadReader) Close() error {
	return r.r.Close()
}

func (r *streamingUploadReader) Capabilities() reader.ReaderCapabilities {
	return reader.ReaderCapabilities{}
}

func (r *streamingUploadReader) Size() int64 {
	return r.size
}

func (r *streamingUploadReader) FileName() (string, bool) {
	return "", false
}

type hashingUploadReader struct {
	reader.UploadReader
	shaHasher hash.Hash
	md5Hasher hash.Hash
	size      int64
}

func newHashingUploadReader(r reader.UploadReader) *hashingUploadReader {
	return &hashingUploadReader{
		UploadReader: r,
		shaHasher:    sha256.New(),
		md5Hasher:    md5.New(),
	}
}

func (r *hashingUploadReader) Read(p []byte) (int, error) {
	n, err := r.UploadReader.Read(p)
	if n > 0 {
		r.shaHasher.Write(p[:n])
		r.md5Hasher.Write(p[:n])
		r.size += int64(n)
	}
	return n, err
}

func (r *hashingUploadReader) Size() int64 {
	return r.size
}

func (r *hashingUploadReader) SHA256() string {
	return hex.EncodeToString(r.shaHasher.Sum(nil))
}

func (r *hashingUploadReader) MD5() string {
	return hex.EncodeToString(r.md5Hasher.Sum(nil))
}
