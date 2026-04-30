// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package buffer

import (
	"sync"

	"github.com/jc-lab/backupgate/internal/config"
	"github.com/jc-lab/backupgate/internal/reader"
)

// Manager creates appropriate buffer implementations based on buffer mode.
type Manager struct {
	cfg             config.BufferConfig
	forceFileBuffer bool
	mu              sync.Mutex
	memoryInUse     int64
}

// NewManager creates a new buffer Manager.
func NewManager(cfg config.BufferConfig, forceFileBuffer ...bool) *Manager {
	force := false
	if len(forceFileBuffer) > 0 {
		force = forceFileBuffer[0]
	}
	if cfg.Memory == 0 {
		cfg.Memory = 64 * 1024 * 1024
	}
	if cfg.MaxMemory == 0 {
		cfg.MaxMemory = 1 * 1024 * 1024 * 1024
	}
	if cfg.SlidingWindowSize == 0 {
		cfg.SlidingWindowSize = 1 * 1024 * 1024
	}
	return &Manager{cfg: cfg, forceFileBuffer: force}
}

// NewFullBuffer creates a SeekableUploadReader for FULLY_IMMEDIATELY / FULLY_COMPLETE modes.
// If expectedSize fits within memory and the total reserved memory stays under
// max_memory, a memory buffer is used.
// Otherwise, a temporary file buffer is created.
func (m *Manager) NewFullBuffer(key string, expectedSize int64) (reader.SeekableUploadReader, error) {
	if m.forceFileBuffer {
		return NewFileBuffer(m.cfg.TmpDir, key)
	}

	if expectedSize > 0 && expectedSize <= m.cfg.Memory && m.tryReserveMemory(expectedSize) {
		return newManagedMemoryBuffer(expectedSize, func() {
			m.releaseMemory(expectedSize)
		}), nil
	}

	return NewFileBuffer(m.cfg.TmpDir, key)
}

// NewFileBackedBuffer creates a SeekableUploadReader that is always backed by
// a local temporary file.
func (m *Manager) NewFileBackedBuffer(key string) (reader.SeekableUploadReader, error) {
	return NewFileBuffer(m.cfg.TmpDir, key)
}

// NewStreamBuffer creates a StreamUploadReader for OFF mode.
// Uses a sliding window ring buffer of configured size.
func (m *Manager) NewStreamBuffer(key string) (reader.StreamUploadReader, error) {
	_ = key
	return NewSlidingBuffer(m.cfg.SlidingWindowSize), nil
}

func (m *Manager) tryReserveMemory(size int64) bool {
	if size <= 0 {
		return false
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.memoryInUse+size > m.cfg.MaxMemory {
		return false
	}
	m.memoryInUse += size
	return true
}

func (m *Manager) releaseMemory(size int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.memoryInUse -= size
	if m.memoryInUse < 0 {
		m.memoryInUse = 0
	}
}
