// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/jc-lab/backupgate/internal/storage"
)

// ErrVerificationFailed is returned when the uploaded file hash
// does not match the expected hash.
var ErrVerificationFailed = fmt.Errorf("verification failed: SHA256 mismatch")

// Verifier downloads the uploaded file from storage and compares
// its SHA256 hash against the expected value.
type Verifier struct {
	backend storage.Backend
}

// NewVerifier creates a new Verifier.
func NewVerifier(backend storage.Backend) *Verifier {
	return &Verifier{backend: backend}
}

// Verify downloads the remote file and compares its SHA256 with expectedHash.
func (v *Verifier) Verify(ctx context.Context, remotePath string, expectedHash string) error {
	rc, err := v.backend.Download(ctx, remotePath)
	if err != nil {
		return fmt.Errorf("downloading for verification: %w", err)
	}
	defer rc.Close()

	h := sha256.New()
	if _, err := io.Copy(h, rc); err != nil {
		return fmt.Errorf("reading remote file for verification: %w", err)
	}

	actualHash := hex.EncodeToString(h.Sum(nil))
	if actualHash != expectedHash {
		return fmt.Errorf("%w: expected %s, got %s", ErrVerificationFailed, expectedHash, actualHash)
	}

	return nil
}
