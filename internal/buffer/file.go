// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package buffer

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/jc-lab/backupgate/internal/reader"
)

// FileBuffer implements SeekableUploadReader using a temporary file.
// Used when the expected payload size exceeds the memory limit.
type FileBuffer struct {
	mu      sync.RWMutex
	file    *os.File
	size    int64
	readPos int64
	closed  bool
}

var _ reader.SeekableUploadReader = (*FileBuffer)(nil)

// NewFileBuffer creates a new file-backed buffer in the given directory.
func NewFileBuffer(tmpDir, key string) (*FileBuffer, error) {
	// Sanitize key for use as filename prefix
	safeKey := strings.ReplaceAll(key, "/", "_")
	pattern := fmt.Sprintf("backupgate_%s_*", safeKey)

	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		return nil, fmt.Errorf("creating tmp dir: %w", err)
	}

	f, err := os.CreateTemp(filepath.Clean(tmpDir), pattern)
	if err != nil {
		return nil, fmt.Errorf("creating temp file: %w", err)
	}

	return &FileBuffer{file: f}, nil
}

// Write appends data to the temp file.
func (fb *FileBuffer) Write(p []byte) (int, error) {
	fb.mu.Lock()
	defer fb.mu.Unlock()
	if fb.closed {
		return 0, fmt.Errorf("buffer is closed")
	}

	n, err := fb.file.WriteAt(p, fb.size)
	fb.size += int64(n)
	return n, err
}

// Read reads from the current read position.
func (fb *FileBuffer) Read(p []byte) (int, error) {
	fb.mu.RLock()
	defer fb.mu.RUnlock()

	n, err := fb.file.ReadAt(p, fb.readPos)
	fb.readPos += int64(n)
	return n, err
}

// ReadAt reads from the given absolute offset.
func (fb *FileBuffer) ReadAt(p []byte, off int64) (int, error) {
	fb.mu.RLock()
	defer fb.mu.RUnlock()
	return fb.file.ReadAt(p, off)
}

// Seek sets the read position.
func (fb *FileBuffer) Seek(offset int64, whence int) (int64, error) {
	fb.mu.Lock()
	defer fb.mu.Unlock()

	var newPos int64
	switch whence {
	case io.SeekStart:
		newPos = offset
	case io.SeekCurrent:
		newPos = fb.readPos + offset
	case io.SeekEnd:
		newPos = fb.size + offset
	default:
		return 0, fmt.Errorf("invalid whence: %d", whence)
	}

	if newPos < 0 {
		return 0, fmt.Errorf("negative seek position: %d", newPos)
	}

	fb.readPos = newPos
	return newPos, nil
}

// Size returns the total bytes written.
func (fb *FileBuffer) Size() int64 {
	fb.mu.RLock()
	defer fb.mu.RUnlock()
	return fb.size
}

// Capabilities returns full ReadAt and Seek support.
func (fb *FileBuffer) Capabilities() reader.ReaderCapabilities {
	return reader.ReaderCapabilities{ReadAt: true, Seek: true}
}

// FileName returns the local temporary file backing this buffer.
func (fb *FileBuffer) FileName() (string, bool) {
	fb.mu.RLock()
	defer fb.mu.RUnlock()
	if fb.closed {
		return "", false
	}
	return fb.file.Name(), true
}

// Sync flushes file-backed buffer contents to disk for consumers that open the
// backing file directly.
func (fb *FileBuffer) Sync() error {
	fb.mu.RLock()
	defer fb.mu.RUnlock()
	if fb.closed {
		return fmt.Errorf("buffer is closed")
	}
	return fb.file.Sync()
}

// Close removes the temporary file and releases resources.
func (fb *FileBuffer) Close() error {
	fb.mu.Lock()
	defer fb.mu.Unlock()
	if fb.closed {
		return nil
	}
	fb.closed = true

	name := fb.file.Name()
	if err := fb.file.Close(); err != nil {
		return err
	}
	return os.Remove(name)
}
