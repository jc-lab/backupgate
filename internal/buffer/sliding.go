// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package buffer

import (
	"fmt"
	"io"
	"sync"

	"github.com/jc-lab/backupgate/internal/reader"
)

// SlidingBuffer implements StreamUploadReader using a ring buffer.
// Only the most recent windowSize bytes are retained in memory.
type SlidingBuffer struct {
	mu         sync.RWMutex
	ring       []byte
	windowSize int64
	writePos   int64 // absolute write position (total bytes written)
	closed     bool
}

var _ reader.StreamUploadReader = (*SlidingBuffer)(nil)

// NewSlidingBuffer creates a new sliding window ring buffer.
func NewSlidingBuffer(windowSize int64) *SlidingBuffer {
	return &SlidingBuffer{
		ring:       make([]byte, windowSize),
		windowSize: windowSize,
	}
}

// Write stores data into the ring buffer, overwriting old data as needed.
func (sb *SlidingBuffer) Write(p []byte) (int, error) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	if sb.closed {
		return 0, fmt.Errorf("buffer is closed")
	}

	n := len(p)
	for i, b := range p {
		idx := (sb.writePos + int64(i)) % sb.windowSize
		sb.ring[idx] = b
	}
	sb.writePos += int64(n)
	return n, nil
}

// Read is a sequential read from the stream. In OFF mode the primary consumer
// is the storage backend, so Read advances an internal cursor.
// For SlidingBuffer this always returns EOF because the data flows
// directly to storage via the pipeline, not through sequential reads.
func (sb *SlidingBuffer) Read(_ []byte) (int, error) {
	return 0, io.EOF
}

// ReadAtIfAvailable reads data from the given absolute offset if it is
// within the current sliding window. Returns ErrOffsetUnavailable otherwise.
func (sb *SlidingBuffer) ReadAtIfAvailable(p []byte, off int64) (int, error) {
	sb.mu.RLock()
	defer sb.mu.RUnlock()

	start, end := sb.availableRangeLocked()
	if off < start || off >= end {
		return 0, reader.ErrOffsetUnavailable
	}

	available := end - off
	n := int64(len(p))
	if n > available {
		n = available
	}

	for i := int64(0); i < n; i++ {
		idx := (off + i) % sb.windowSize
		p[i] = sb.ring[idx]
	}

	return int(n), nil
}

// AvailableRange returns the [start, end) byte range in the sliding window.
func (sb *SlidingBuffer) AvailableRange() (start, end int64) {
	sb.mu.RLock()
	defer sb.mu.RUnlock()
	return sb.availableRangeLocked()
}

func (sb *SlidingBuffer) availableRangeLocked() (start, end int64) {
	end = sb.writePos
	start = end - sb.windowSize
	if start < 0 {
		start = 0
	}
	return start, end
}

// Size returns the total bytes written so far.
func (sb *SlidingBuffer) Size() int64 {
	sb.mu.RLock()
	defer sb.mu.RUnlock()
	return sb.writePos
}

// Capabilities returns limited capabilities (no Seek, limited ReadAt via ReadAtIfAvailable).
func (sb *SlidingBuffer) Capabilities() reader.ReaderCapabilities {
	return reader.ReaderCapabilities{ReadAt: false, Seek: false}
}

// FileName reports that sliding buffers are not file-backed.
func (sb *SlidingBuffer) FileName() (string, bool) {
	return "", false
}

// Close marks the buffer as closed.
func (sb *SlidingBuffer) Close() error {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	sb.closed = true
	return nil
}
