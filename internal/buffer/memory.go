// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package buffer

import (
	"bytes"
	"fmt"
	"io"
	"sync"

	"github.com/jc-lab/backupgate/internal/reader"
)

// MemoryBuffer implements SeekableUploadReader using an in-memory byte buffer.
type MemoryBuffer struct {
	mu      sync.RWMutex
	buf     *bytes.Buffer
	readPos int64
	closed  bool
}

var _ reader.SeekableUploadReader = (*MemoryBuffer)(nil)

// NewMemoryBuffer creates a new memory-backed buffer.
func NewMemoryBuffer(expectedSize int64) *MemoryBuffer {
	var buf *bytes.Buffer
	if expectedSize > 0 {
		buf = bytes.NewBuffer(make([]byte, 0, expectedSize))
	} else {
		buf = new(bytes.Buffer)
	}
	return &MemoryBuffer{buf: buf}
}

// Write appends data to the buffer.
func (m *MemoryBuffer) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return 0, fmt.Errorf("buffer is closed")
	}
	return m.buf.Write(p)
}

// Read reads from the current read position.
func (m *MemoryBuffer) Read(p []byte) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data := m.buf.Bytes()
	if m.readPos >= int64(len(data)) {
		return 0, io.EOF
	}

	n := copy(p, data[m.readPos:])
	m.readPos += int64(n)
	return n, nil
}

// ReadAt reads from the given absolute offset without changing internal state.
func (m *MemoryBuffer) ReadAt(p []byte, off int64) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data := m.buf.Bytes()
	if off >= int64(len(data)) {
		return 0, io.EOF
	}

	n := copy(p, data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

// Seek sets the read position.
func (m *MemoryBuffer) Seek(offset int64, whence int) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var newPos int64
	size := int64(m.buf.Len())

	switch whence {
	case io.SeekStart:
		newPos = offset
	case io.SeekCurrent:
		newPos = m.readPos + offset
	case io.SeekEnd:
		newPos = size + offset
	default:
		return 0, fmt.Errorf("invalid whence: %d", whence)
	}

	if newPos < 0 {
		return 0, fmt.Errorf("negative seek position: %d", newPos)
	}

	m.readPos = newPos
	return newPos, nil
}

// Size returns the total bytes written.
func (m *MemoryBuffer) Size() int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return int64(m.buf.Len())
}

// Capabilities returns full ReadAt and Seek support.
func (m *MemoryBuffer) Capabilities() reader.ReaderCapabilities {
	return reader.ReaderCapabilities{ReadAt: true, Seek: true}
}

// FileName reports that memory buffers are not file-backed.
func (m *MemoryBuffer) FileName() (string, bool) {
	return "", false
}

// Close marks the buffer as closed.
func (m *MemoryBuffer) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}
