// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package pipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
)

// SHA256Hasher wraps an io.Writer and computes SHA256 as data passes through.
type SHA256Hasher struct {
	h      hash.Hash
	writer io.Writer // downstream writer (buffer or storage)
}

// NewSHA256Hasher creates a hasher that tees writes to the given downstream writer.
func NewSHA256Hasher(downstream io.Writer) *SHA256Hasher {
	return &SHA256Hasher{
		h:      sha256.New(),
		writer: downstream,
	}
}

// Write hashes the data and forwards it to the downstream writer.
func (h *SHA256Hasher) Write(p []byte) (int, error) {
	// Hash first (never fails for sha256)
	h.h.Write(p)

	// Forward to downstream
	return h.writer.Write(p)
}

// Sum returns the final SHA256 hash as a hex-encoded string.
func (h *SHA256Hasher) Sum() string {
	return hex.EncodeToString(h.h.Sum(nil))
}

// Reset resets the hash state for reuse.
func (h *SHA256Hasher) Reset() {
	h.h.Reset()
}
