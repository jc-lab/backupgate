// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package reader

import (
	"fmt"
	"io"
)

// AssembledReader presents multiple upload readers as one contiguous reader.
type AssembledReader struct {
	parts []UploadReader
	sizes []int64
	size  int64
	pos   int64
}

var _ SeekableUploadReader = (*AssembledReader)(nil)

// NewAssembledReader creates a seekable reader over already-buffered parts.
func NewAssembledReader(parts []UploadReader) *AssembledReader {
	copied := append([]UploadReader(nil), parts...)
	sizes := make([]int64, len(copied))
	var total int64
	for i, part := range copied {
		sizes[i] = part.Size()
		total += sizes[i]
	}
	return &AssembledReader{
		parts: copied,
		sizes: sizes,
		size:  total,
	}
}

func (r *AssembledReader) Read(p []byte) (int, error) {
	n, err := r.ReadAt(p, r.pos)
	r.pos += int64(n)
	return n, err
}

func (r *AssembledReader) ReadAt(p []byte, off int64) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if off < 0 {
		return 0, fmt.Errorf("negative read offset: %d", off)
	}
	if off >= r.size {
		return 0, io.EOF
	}

	written := 0
	for i, part := range r.parts {
		partSize := r.sizes[i]
		if off >= partSize {
			off -= partSize
			continue
		}

		partReaderAt, ok := part.(io.ReaderAt)
		if !ok {
			return written, fmt.Errorf("multipart part %d does not support ReadAt", i+1)
		}

		limit := int64(len(p) - written)
		available := partSize - off
		if limit > available {
			limit = available
		}
		n, err := partReaderAt.ReadAt(p[written:written+int(limit)], off)
		written += n
		off = 0
		if err != nil && err != io.EOF {
			return written, err
		}
		if written == len(p) {
			return written, nil
		}
	}

	return written, io.EOF
}

func (r *AssembledReader) Seek(offset int64, whence int) (int64, error) {
	var next int64
	switch whence {
	case io.SeekStart:
		next = offset
	case io.SeekCurrent:
		next = r.pos + offset
	case io.SeekEnd:
		next = r.size + offset
	default:
		return 0, fmt.Errorf("invalid whence: %d", whence)
	}
	if next < 0 {
		return 0, fmt.Errorf("negative seek position: %d", next)
	}
	r.pos = next
	return r.pos, nil
}

func (r *AssembledReader) Write(_ []byte) (int, error) {
	return 0, fmt.Errorf("assembled reader is read-only")
}

func (r *AssembledReader) Close() error {
	var err error
	for _, part := range r.parts {
		if closeErr := part.Close(); err == nil {
			err = closeErr
		}
	}
	return err
}

func (r *AssembledReader) Capabilities() ReaderCapabilities {
	return ReaderCapabilities{ReadAt: true, Seek: true}
}

func (r *AssembledReader) Size() int64 {
	return r.size
}

func (r *AssembledReader) FileName() (string, bool) {
	if len(r.parts) == 1 {
		return r.parts[0].FileName()
	}
	return "", false
}
