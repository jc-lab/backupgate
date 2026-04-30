// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package rotation

import (
	"context"
	"errors"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jc-lab/backupgate/internal/storage"
)

// Policy defines how old backups are rotated out.
type Policy struct {
	MaxCount int           // maximum number of backups to keep (0 = unlimited)
	MaxAge   time.Duration // maximum age of backups (0 = unlimited)
}

// Manager handles backup rotation for keys.
type Manager struct{}

// NewManager creates a new rotation Manager.
func NewManager() *Manager {
	return &Manager{}
}

// Apply executes the rotation policy for a given key directory.
// It lists existing backups, sorts by timestamp, and deletes those
// exceeding MaxCount or older than MaxAge.
func (m *Manager) Apply(ctx context.Context, backend storage.Backend, key string, policy Policy) error {
	if policy.MaxCount == 0 && policy.MaxAge == 0 {
		return nil // no rotation configured
	}

	entries, err := backend.List(ctx, key)
	if err != nil {
		if isNotExist(err) {
			return nil
		}
		return err
	}

	var files []storage.Entry
	for _, e := range entries {
		if e.IsDir {
			continue
		}
		if _, ok := parseTimestamp(e.Name); ok {
			files = append(files, e)
		}
	}

	// Sort by filename timestamp descending (newest first).
	sort.Slice(files, func(i, j int) bool {
		ti, _ := parseTimestamp(files[i].Name)
		tj, _ := parseTimestamp(files[j].Name)
		return ti.After(tj)
	})

	toDelete := m.selectForDeletion(files, policy)

	for _, entry := range toDelete {
		remotePath := key + "/" + entry.Name
		if err := backend.Delete(ctx, remotePath); err != nil {
			return err
		}
	}

	return nil
}

// selectForDeletion determines which files should be removed based on the policy.
func (m *Manager) selectForDeletion(files []storage.Entry, policy Policy) []storage.Entry {
	var result []storage.Entry
	now := time.Now()

	for i, f := range files {
		ts, _ := parseTimestamp(f.Name)
		byCount := policy.MaxCount > 0 && i >= policy.MaxCount
		byAge := policy.MaxAge > 0 && now.Sub(ts) > policy.MaxAge

		if byCount || byAge {
			result = append(result, f)
		}
	}

	return result
}

func parseTimestamp(name string) (time.Time, bool) {
	prefix, _, ok := strings.Cut(name, "_")
	if !ok {
		return time.Time{}, false
	}
	ts, err := time.Parse("20060102T150405Z", prefix)
	if err != nil {
		return time.Time{}, false
	}
	return ts, true
}

func isNotExist(err error) bool {
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "file does not exist") ||
		strings.Contains(msg, "no such file") ||
		strings.Contains(msg, "not found")
}
