// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// BufferMode represents the buffering strategy for uploads.
type BufferMode string

const (
	BufferModeFullyImmediately BufferMode = "FULLY_IMMEDIATELY"
	BufferModeFullyComplete    BufferMode = "FULLY_COMPLETE"
	BufferModeOff              BufferMode = "OFF"
)

// APIMode represents how S3 and HTTP protocols are detected.
type APIMode string

const (
	APIModeHeader APIMode = "header"
	APIModePort   APIMode = "port"
)

// StorageType represents the storage backend type.
type StorageType string

const (
	StorageTypeSFTP StorageType = "sftp"
)

// AuthType represents an authentication provider type.
type AuthType string

const (
	AuthTypeInline   AuthType = "inline"
	AuthTypeFile     AuthType = "file"
	AuthTypeHtpasswd AuthType = "htpasswd"
	AuthTypeExternal AuthType = "external"
)

// Config is the root configuration structure.
type Config struct {
	Server  ServerConfig         `yaml:"server"`
	Buffer  BufferConfig         `yaml:"buffer"`
	Storage StorageConfig        `yaml:"storage"`
	Keys    map[string]KeyConfig `yaml:"keys"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	API   APIConfig     `yaml:"api"`
	Debug *ListenConfig `yaml:"debug,omitempty"`
}

// APIConfig holds API endpoint settings.
type APIConfig struct {
	Mode   APIMode       `yaml:"mode"`
	Listen string        `yaml:"listen,omitempty"` // used in header mode
	S3     *ListenConfig `yaml:"s3,omitempty"`     // used in port mode
	HTTP   *ListenConfig `yaml:"http,omitempty"`   // used in port mode
}

// ListenConfig holds a listen address.
type ListenConfig struct {
	Listen string `yaml:"listen"`
}

// BufferConfig holds buffer-related settings.
type BufferConfig struct {
	Memory            int64  `yaml:"memory"`              // bytes, default 64MB
	SlidingWindowSize int64  `yaml:"sliding_window_size"` // bytes, default 1MB
	MaxMemory         int64  `yaml:"max_memory"`          // bytes, default 1GB
	TmpDir            string `yaml:"tmp_dir"`
}

// StorageConfig holds storage backend settings.
type StorageConfig struct {
	Type  StorageType  `yaml:"type"`
	SFTP  *SFTPConfig  `yaml:"sftp,omitempty"`
	Rsync *RsyncConfig `yaml:"rsync,omitempty"`
}

// SFTPConfig holds SFTP connection settings.
type SFTPConfig struct {
	Host     string `yaml:"host"`
	Username string `yaml:"username"`
	KeyFile  string `yaml:"key_file"`
	Password string `yaml:"password,omitempty"`
	BasePath string `yaml:"base_path"`
}

// RsyncConfig holds rsync upload settings.
type RsyncConfig struct {
	Enabled  bool   `yaml:"enabled"`
	BasePath string `yaml:"base_path"`
}

// KeyConfig holds per-key policy settings.
type KeyConfig struct {
	BufferMode   BufferMode `yaml:"buffer_mode"`
	Verification bool       `yaml:"verification"`
	// DetachBackupFromRequest controls whether backup jobs are detached from the
	// lifecycle of the HTTP request.
	//
	// When false, the backup uses the request context and is canceled if the
	// client disconnects, the request times out, or the request context is otherwise
	// canceled.
	//
	// When true, the backup uses a background context and continues running even if
	// the client request is canceled.
	//
	// The default value is true.
	DetachBackupFromRequest *bool          `yaml:"detach_backup_from_request"`
	Rotation                RotationConfig `yaml:"rotation"`
	Auth                    []AuthConfig   `yaml:"auth"`
}

func (k *KeyConfig) GetDetachBackupFromRequest() bool {
	if k.DetachBackupFromRequest == nil {
		return true
	}
	return *k.DetachBackupFromRequest
}

// RotationConfig holds rotation policy settings.
type RotationConfig struct {
	MaxCount int    `yaml:"max_count"`
	MaxAge   string `yaml:"max_age,omitempty"` // duration string e.g. "168h"
}

// MaxAgeDuration parses MaxAge into time.Duration.
func (r *RotationConfig) MaxAgeDuration() (time.Duration, error) {
	if r.MaxAge == "" {
		return 0, nil
	}
	return time.ParseDuration(r.MaxAge)
}

// AuthConfig holds a single auth provider configuration.
type AuthConfig struct {
	Type        AuthType          `yaml:"type"`
	Credentials []CredentialEntry `yaml:"credentials,omitempty"` // inline
	Path        string            `yaml:"path,omitempty"`        // file, htpasswd
	Command     string            `yaml:"command,omitempty"`     // external
	Args        []string          `yaml:"args,omitempty"`        // external
	Timeout     string            `yaml:"timeout,omitempty"`     // external
}

// CredentialEntry holds a username/password pair for inline auth.
type CredentialEntry struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// DefaultConfig returns a Config with sensible defaults filled in.
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			API: APIConfig{
				Mode:   APIModeHeader,
				Listen: ":8080",
			},
		},
		Buffer: BufferConfig{
			Memory:            64 * 1024 * 1024, // 64MB
			SlidingWindowSize: 1 * 1024 * 1024,  // 1MB
			MaxMemory:         1 * 1024 * 1024 * 1024,
			TmpDir:            os.TempDir(),
		},
	}
}

// LoadConfig reads a YAML file and returns the parsed Config.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config YAML: %w", err)
	}

	if err := cfg.applyDefaults(); err != nil {
		return nil, fmt.Errorf("applying defaults: %w", err)
	}

	return cfg, nil
}

// applyDefaults fills in zero-value fields with defaults after unmarshalling.
func (c *Config) applyDefaults() error {
	if c.Buffer.Memory == 0 {
		c.Buffer.Memory = 64 * 1024 * 1024
	}
	if c.Buffer.SlidingWindowSize == 0 {
		c.Buffer.SlidingWindowSize = 1 * 1024 * 1024
	}
	if c.Buffer.MaxMemory == 0 {
		c.Buffer.MaxMemory = 1 * 1024 * 1024 * 1024
	}
	if c.Buffer.TmpDir == "" {
		c.Buffer.TmpDir = os.TempDir()
	}
	return nil
}

// ResolveKeyConfig returns the config for a specific key using path-prefix matching.
//
// Matching rules:
//  1. Exact match is checked first.
//  2. Then configured keys are checked as path prefixes, requiring a "/" boundary.
//     e.g. configured "database-backup" matches "database-backup/prod" but NOT
//     "database-backup-xxx/prod". The longest matching prefix wins.
//  3. Falls back to "default" if no match is found.
func (c *Config) ResolveKeyConfig(key string) (*KeyConfig, bool) {
	// 1. Exact match
	if kc, ok := c.Keys[key]; ok {
		return &kc, true
	}

	// 2. Path-prefix match (longest prefix wins)
	// Collect candidate keys sorted by length descending for longest-match-first
	candidates := make([]string, 0, len(c.Keys))
	for k := range c.Keys {
		if k == "default" {
			continue
		}
		candidates = append(candidates, k)
	}
	sort.Slice(candidates, func(i, j int) bool {
		return len(candidates[i]) > len(candidates[j])
	})

	for _, candidate := range candidates {
		if isPathPrefix(candidate, key) {
			kc := c.Keys[candidate]
			return &kc, true
		}
	}

	// 3. Default fallback
	if kc, ok := c.Keys["default"]; ok {
		return &kc, true
	}
	return nil, false
}

// isPathPrefix checks whether prefix is a path-segment prefix of key.
// "a/b" is a prefix of "a/b/c" and "a/b" itself, but NOT of "a/bc".
// A prefix must be followed by "/" or match the full key exactly.
func isPathPrefix(prefix, key string) bool {
	if !strings.HasPrefix(key, prefix) {
		return false
	}
	// Exact match already handled above, but guard anyway
	if len(key) == len(prefix) {
		return true
	}
	// The character right after the prefix must be "/"
	return key[len(prefix)] == '/'
}
