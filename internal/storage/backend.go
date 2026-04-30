// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package storage

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/jc-lab/backupgate/internal/reader"
)

// ErrUnsupported is returned by backends when an operation cannot be expressed
// by that backend's protocol.
var ErrUnsupported = errors.New("storage operation unsupported")

// Entry represents a single file or directory on the remote storage.
type Entry struct {
	Name    string
	Size    int64
	ModTime time.Time
	IsDir   bool
}

type Uploader interface {
	// Upload writes the full content from reader to the remote path.
	Upload(ctx context.Context, remotePath string, r reader.UploadReader, size int64) error

	// ResumeUpload continues a previously interrupted upload from the given offset.
	// For SeekableUploadReader, the exact offset is seeked.
	// For StreamUploadReader, AvailableRange is checked first.
	ResumeUpload(ctx context.Context, remotePath string, r reader.UploadReader, offset int64, totalSize int64) error
}

// Backend is the interface for remote storage operations.
type Backend interface {
	Uploader

	// Download reads the remote file for verification purposes.
	Download(ctx context.Context, remotePath string) (io.ReadCloser, error)

	// Delete removes a file from the remote path.
	Delete(ctx context.Context, remotePath string) error

	// List returns entries in the remote directory.
	List(ctx context.Context, remoteDir string) ([]Entry, error)

	// RemoteSize returns the current size of the file on remote storage.
	// Used for resume offset detection.
	RemoteSize(ctx context.Context, remotePath string) (int64, error)
}
