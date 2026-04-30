// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package buffer

import (
	"os"
	"testing"

	"github.com/jc-lab/backupgate/internal/config"
)

func TestFileBufferFileName(t *testing.T) {
	buf, err := NewFileBuffer(t.TempDir(), "db/backup")
	if err != nil {
		t.Fatalf("NewFileBuffer() error = %v", err)
	}
	defer buf.Close()

	name, ok := buf.FileName()
	if !ok {
		t.Fatal("FileName() ok = false, want true")
	}
	if name == "" {
		t.Fatal("FileName() returned empty name")
	}
	if _, err := os.Stat(name); err != nil {
		t.Fatalf("stat backing file: %v", err)
	}
}

func TestMemoryBufferFileName(t *testing.T) {
	buf := NewMemoryBuffer(16)
	name, ok := buf.FileName()
	if ok || name != "" {
		t.Fatalf("FileName() = (%q, %v), want empty false", name, ok)
	}
}

func TestManagerNewFullBufferForceFileBuffer(t *testing.T) {
	mgr := NewManager(testBufferConfig(t), true)

	buf, err := mgr.NewFullBuffer("db/backup", 1)
	if err != nil {
		t.Fatalf("NewFullBuffer() error = %v", err)
	}
	defer buf.Close()

	name, ok := buf.FileName()
	if !ok || name == "" {
		t.Fatalf("FileName() = (%q, %v), want backing filename", name, ok)
	}
}

func TestManagerNewFullBufferUsesMemoryWhenSmall(t *testing.T) {
	mgr := NewManager(testBufferConfig(t))

	buf, err := mgr.NewFullBuffer("db/backup", 1)
	if err != nil {
		t.Fatalf("NewFullBuffer() error = %v", err)
	}
	defer buf.Close()

	name, ok := buf.FileName()
	if ok || name != "" {
		t.Fatalf("FileName() = (%q, %v), want no backing filename", name, ok)
	}
}

func TestManagerNewFullBufferUsesFileWhenTotalMemoryWouldExceedMax(t *testing.T) {
	cfg := testBufferConfig(t)
	cfg.Memory = 1024
	cfg.MaxMemory = 1024
	mgr := NewManager(cfg)

	first, err := mgr.NewFullBuffer("db/first", 1024)
	if err != nil {
		t.Fatalf("first NewFullBuffer() error = %v", err)
	}
	defer first.Close()

	second, err := mgr.NewFullBuffer("db/second", 1)
	if err != nil {
		t.Fatalf("second NewFullBuffer() error = %v", err)
	}
	defer second.Close()

	name, ok := second.FileName()
	if !ok || name == "" {
		t.Fatalf("second FileName() = (%q, %v), want backing filename", name, ok)
	}
}

func TestManagerNewFullBufferReleasesMemoryOnClose(t *testing.T) {
	cfg := testBufferConfig(t)
	cfg.Memory = 1024
	cfg.MaxMemory = 1024
	mgr := NewManager(cfg)

	first, err := mgr.NewFullBuffer("db/first", 1024)
	if err != nil {
		t.Fatalf("first NewFullBuffer() error = %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}

	second, err := mgr.NewFullBuffer("db/second", 1024)
	if err != nil {
		t.Fatalf("second NewFullBuffer() error = %v", err)
	}
	defer second.Close()

	name, ok := second.FileName()
	if ok || name != "" {
		t.Fatalf("second FileName() = (%q, %v), want memory buffer", name, ok)
	}
}

func testBufferConfig(t *testing.T) config.BufferConfig {
	t.Helper()
	return config.BufferConfig{
		Memory:            1024,
		SlidingWindowSize: 1024,
		MaxMemory:         1024 * 1024,
		TmpDir:            t.TempDir(),
	}
}
