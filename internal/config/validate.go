// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"fmt"
	"strings"
)

// Validate checks the configuration for logical errors and missing fields.
func Validate(cfg *Config) error {
	var errs []string

	if err := validateAPI(&cfg.Server.API); err != nil {
		errs = append(errs, err.Error())
	}
	if cfg.Server.Debug != nil && cfg.Server.Debug.Listen == "" {
		errs = append(errs, "server.debug: listen is required when debug is configured")
	}

	if err := validateStorage(&cfg.Storage); err != nil {
		errs = append(errs, err.Error())
	}

	if err := validateBuffer(&cfg.Buffer); err != nil {
		errs = append(errs, err.Error())
	}

	rsyncEnabled := cfg.Storage.Rsync != nil && cfg.Storage.Rsync.Enabled
	if rsyncEnabled {
		errs = append(errs, "rsync uploads are not yet supported")
	}
	for name, kc := range cfg.Keys {
		if err := validateKeyConfig(name, &kc, rsyncEnabled); err != nil {
			errs = append(errs, err.Error())
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("config validation failed:\n  %s", strings.Join(errs, "\n  "))
	}
	return nil
}

func validateAPI(api *APIConfig) error {
	switch api.Mode {
	case APIModeHeader:
		if api.Listen == "" {
			return fmt.Errorf("api: header mode requires 'listen' field")
		}
	case APIModePort:
		if api.S3 == nil || api.S3.Listen == "" {
			return fmt.Errorf("api: port mode requires 's3.listen' field")
		}
		if api.HTTP == nil || api.HTTP.Listen == "" {
			return fmt.Errorf("api: port mode requires 'http.listen' field")
		}
	default:
		return fmt.Errorf("api: unknown mode %q (expected 'header' or 'port')", api.Mode)
	}
	return nil
}

func validateStorage(s *StorageConfig) error {
	switch s.Type {
	case StorageTypeSFTP:
		if s.SFTP == nil {
			return fmt.Errorf("storage: type is sftp but sftp config is missing")
		}
		if s.SFTP.Host == "" {
			return fmt.Errorf("storage.sftp: host is required")
		}
	default:
		return fmt.Errorf("storage: unknown type %q (only 'sftp' is supported; enable rsync uploads with storage.rsync.enabled)", s.Type)
	}

	if s.Rsync != nil && s.Rsync.Enabled && s.Rsync.BasePath == "" {
		return fmt.Errorf("storage.rsync: base_path is required when enabled")
	}
	return nil
}

func validateBuffer(b *BufferConfig) error {
	if b.Memory < 0 {
		return fmt.Errorf("buffer: memory must be non-negative")
	}
	if b.MaxMemory < 0 {
		return fmt.Errorf("buffer: max_memory must be non-negative")
	}
	if b.SlidingWindowSize < 0 {
		return fmt.Errorf("buffer: sliding_window_size must be non-negative")
	}
	return nil
}

func validateKeyConfig(name string, kc *KeyConfig, rsyncEnabled bool) error {
	switch kc.BufferMode {
	case BufferModeFullyImmediately, BufferModeFullyComplete, BufferModeOff:
		// valid
	default:
		return fmt.Errorf("key %q: unknown buffer_mode %q", name, kc.BufferMode)
	}

	if rsyncEnabled && kc.BufferMode != BufferModeFullyComplete {
		return fmt.Errorf("key %q: rsync uploads require buffer_mode %q", name, BufferModeFullyComplete)
	}

	if len(kc.Auth) == 0 && name != "default" {
		return fmt.Errorf("key %q: at least one auth provider is required", name)
	}

	for i, a := range kc.Auth {
		if err := validateAuthConfig(name, i, &a); err != nil {
			return err
		}
	}

	return nil
}

func validateAuthConfig(keyName string, idx int, a *AuthConfig) error {
	prefix := fmt.Sprintf("key %q auth[%d]", keyName, idx)
	switch a.Type {
	case AuthTypeInline:
		if len(a.Credentials) == 0 {
			return fmt.Errorf("%s: inline auth requires at least one credential", prefix)
		}
	case AuthTypeFile:
		if a.Path == "" {
			return fmt.Errorf("%s: file auth requires 'path'", prefix)
		}
	case AuthTypeHtpasswd:
		if a.Path == "" {
			return fmt.Errorf("%s: htpasswd auth requires 'path'", prefix)
		}
	case AuthTypeExternal:
		if a.Command == "" {
			return fmt.Errorf("%s: external auth requires 'command'", prefix)
		}
	default:
		return fmt.Errorf("%s: unknown auth type %q", prefix, a.Type)
	}
	return nil
}
