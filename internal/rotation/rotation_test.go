// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package rotation

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/jc-lab/backupgate/internal/reader"
	"github.com/jc-lab/backupgate/internal/storage"
)

func TestApplyIgnoresNonTimestampedFiles(t *testing.T) {
	backend := &recordingBackend{
		entries: []storage.Entry{
			{Name: "legacy.sql.zst", Size: 1, ModTime: time.Now().Add(-48 * time.Hour)},
			{Name: "20260429T120000Z_backup.sql.zst", Size: 1, ModTime: time.Now()},
			{Name: "20260428T120000Z_backup.sql.zst", Size: 1, ModTime: time.Now()},
		},
	}

	mgr := NewManager()
	err := mgr.Apply(context.Background(), backend, "db", Policy{MaxCount: 1})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if len(backend.deleted) != 1 {
		t.Fatalf("deleted = %v, want 1 entry", backend.deleted)
	}
	if backend.deleted[0] != "db/20260428T120000Z_backup.sql.zst" {
		t.Fatalf("deleted[0] = %q, want older timestamped file", backend.deleted[0])
	}
}

func TestApplyIgnoresNonTimestampedFilesForAge(t *testing.T) {
	backend := &recordingBackend{
		entries: []storage.Entry{
			{Name: "legacy.sql.zst", Size: 1, ModTime: time.Now().Add(-48 * time.Hour)},
			{Name: "20260401T120000Z_backup.sql.zst", Size: 1, ModTime: time.Now()},
		},
	}

	mgr := NewManager()
	err := mgr.Apply(context.Background(), backend, "db", Policy{MaxAge: 7 * 24 * time.Hour})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if len(backend.deleted) != 1 {
		t.Fatalf("deleted = %v, want 1 entry", backend.deleted)
	}
	if backend.deleted[0] != "db/20260401T120000Z_backup.sql.zst" {
		t.Fatalf("deleted[0] = %q, want timestamped file", backend.deleted[0])
	}
}

type recordingBackend struct {
	entries []storage.Entry
	deleted []string
}

func (b *recordingBackend) Upload(context.Context, string, reader.UploadReader, int64) error {
	return errors.New("not implemented")
}

func (b *recordingBackend) ResumeUpload(context.Context, string, reader.UploadReader, int64, int64) error {
	return errors.New("not implemented")
}

func (b *recordingBackend) Download(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("not implemented")
}

func (b *recordingBackend) Delete(_ context.Context, remotePath string) error {
	b.deleted = append(b.deleted, remotePath)
	return nil
}

func (b *recordingBackend) List(context.Context, string) ([]storage.Entry, error) {
	return b.entries, nil
}

func (b *recordingBackend) RemoteSize(context.Context, string) (int64, error) {
	return 0, errors.New("not implemented")
}
