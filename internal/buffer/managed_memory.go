// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package buffer

import "sync"

type managedMemoryBuffer struct {
	*MemoryBuffer
	release func()
	once    sync.Once
}

func newManagedMemoryBuffer(expectedSize int64, release func()) *managedMemoryBuffer {
	return &managedMemoryBuffer{
		MemoryBuffer: NewMemoryBuffer(expectedSize),
		release:      release,
	}
}

func (b *managedMemoryBuffer) Close() error {
	err := b.MemoryBuffer.Close()
	b.once.Do(b.release)
	return err
}
