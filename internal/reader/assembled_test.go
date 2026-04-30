// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package reader

import (
	"io"
	"testing"
)

func TestAssembledReaderReadSeekAndReadAt(t *testing.T) {
	r := NewAssembledReader([]UploadReader{
		newTestUploadPart("hello "),
		newTestUploadPart("world"),
		newTestUploadPart("!"),
	})
	defer r.Close()

	buf := make([]byte, 12)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatalf("Read() error = %v, want nil", err)
	}
	if got := string(buf[:n]); got != "hello world!" {
		t.Fatalf("Read() = %q, want hello world!", got)
	}

	if _, err := r.Seek(6, io.SeekStart); err != nil {
		t.Fatalf("Seek() error = %v", err)
	}
	buf = make([]byte, 5)
	n, err = r.Read(buf)
	if err != nil {
		t.Fatalf("Read() after seek error = %v, want nil", err)
	}
	if got := string(buf[:n]); got != "world" {
		t.Fatalf("Read() after seek = %q, want world", got)
	}

	buf = make([]byte, 7)
	n, err = r.ReadAt(buf, 4)
	if err != nil {
		t.Fatalf("ReadAt() error = %v, want nil", err)
	}
	if got := string(buf[:n]); got != "o world" {
		t.Fatalf("ReadAt() = %q, want o world", got)
	}
}

func TestAssembledReaderMetadata(t *testing.T) {
	part := newTestUploadPart("backup")
	part.filename = "/tmp/part"
	r := NewAssembledReader([]UploadReader{part})
	defer r.Close()

	if got := r.Size(); got != 6 {
		t.Fatalf("Size() = %d, want 6", got)
	}
	name, ok := r.FileName()
	if !ok || name != "/tmp/part" {
		t.Fatalf("FileName() = (%q, %v), want /tmp/part true", name, ok)
	}
	if caps := r.Capabilities(); !caps.ReadAt || !caps.Seek {
		t.Fatalf("Capabilities() = %+v, want ReadAt and Seek", caps)
	}
}

type testUploadPart struct {
	data     []byte
	pos      int64
	filename string
	closed   bool
}

func newTestUploadPart(s string) *testUploadPart {
	return &testUploadPart{data: []byte(s)}
}

func (p *testUploadPart) Read(buf []byte) (int, error) {
	n, err := p.ReadAt(buf, p.pos)
	p.pos += int64(n)
	return n, err
}

func (p *testUploadPart) Write(buf []byte) (int, error) {
	p.data = append(p.data, buf...)
	return len(buf), nil
}

func (p *testUploadPart) Close() error {
	p.closed = true
	return nil
}

func (p *testUploadPart) Capabilities() ReaderCapabilities {
	return ReaderCapabilities{ReadAt: true, Seek: true}
}

func (p *testUploadPart) Size() int64 {
	return int64(len(p.data))
}

func (p *testUploadPart) FileName() (string, bool) {
	if p.filename == "" {
		return "", false
	}
	return p.filename, true
}

func (p *testUploadPart) ReadAt(buf []byte, off int64) (int, error) {
	if off >= int64(len(p.data)) {
		return 0, io.EOF
	}
	n := copy(buf, p.data[off:])
	if n < len(buf) {
		return n, io.EOF
	}
	return n, nil
}

func (p *testUploadPart) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		p.pos = offset
	case io.SeekCurrent:
		p.pos += offset
	case io.SeekEnd:
		p.pos = int64(len(p.data)) + offset
	}
	return p.pos, nil
}
