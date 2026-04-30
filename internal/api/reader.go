// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"hash"
	"io"
)

type hashedReader struct {
	r io.ReadCloser
	h hash.Hash
}

var _ io.Reader = (*hashedReader)(nil)

func newHashedReader(r io.ReadCloser, h hash.Hash) *hashedReader {
	return &hashedReader{r: r, h: h}
}

func (hr *hashedReader) Read(p []byte) (int, error) {
	n, err := hr.r.Read(p)
	if n > 0 {
		hr.h.Write(p[:n])
	}
	return n, err
}

func (hr *hashedReader) Close() error {
	return hr.r.Close()
}

func (hr *hashedReader) Digest() []byte {
	return hr.h.Sum(nil)
}
