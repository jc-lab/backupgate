// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package reader

import (
	"errors"
	"io"
)

// ErrOffsetUnavailable is returned when the requested offset is outside
// the sliding window range in StreamUploadReader.
var ErrOffsetUnavailable = errors.New("requested offset is outside the available sliding window range")

// ReaderCapabilities describes which optional I/O operations a reader supports.
type ReaderCapabilities struct {
	ReadAt bool // supports io.ReaderAt
	Seek   bool // supports io.Seeker
}

// UploadReader extends io.Reader with buffer-mode-aware capabilities.
// Depending on the buffer mode, concrete implementations may additionally
// satisfy io.ReaderAt and/or io.Seeker.
type UploadReader interface {
	io.Reader
	io.Writer // receives incoming data
	io.Closer

	// Capabilities returns which optional operations this reader supports.
	Capabilities() ReaderCapabilities

	// Size returns the total number of bytes written so far.
	Size() int64

	// FileName returns the local backing filename when the upload is fully
	// buffered on disk. Non-file-backed and streaming readers return false.
	FileName() (name string, ok bool)
}

// SeekableUploadReader is used in FULLY_IMMEDIATELY / FULLY_COMPLETE modes.
// The entire payload is kept in a buffer so ReadAt and Seek are fully supported.
type SeekableUploadReader interface {
	UploadReader
	io.ReaderAt
	io.Seeker
}

// StreamUploadReader is used in OFF mode.
// Only the most recent N bytes are retained in a sliding window ring buffer.
type StreamUploadReader interface {
	UploadReader

	// ReadAtIfAvailable reads from the given absolute offset.
	// Returns ErrOffsetUnavailable when off is outside the current window.
	ReadAtIfAvailable(p []byte, off int64) (n int, err error)

	// AvailableRange returns the [start, end) byte range currently held
	// in the sliding window.
	AvailableRange() (start, end int64)
}
