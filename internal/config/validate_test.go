// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"strings"
	"testing"
)

func TestValidateRsyncRequiresFullyComplete(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Storage = StorageConfig{
		Type: StorageTypeSFTP,
		SFTP: &SFTPConfig{
			Host: "example.com",
		},
		Rsync: &RsyncConfig{
			Enabled:  true,
			BasePath: "/backups",
		},
	}
	cfg.Keys = map[string]KeyConfig{
		"default": {
			BufferMode: BufferModeOff,
		},
	}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() succeeded, want error")
	}
	if !strings.Contains(err.Error(), `rsync uploads require buffer_mode "FULLY_COMPLETE"`) {
		t.Fatalf("Validate() error = %v, want rsync FULLY_COMPLETE constraint", err)
	}
}

//func TestValidateRsyncAllowsFullyComplete(t *testing.T) {
//	cfg := DefaultConfig()
//	cfg.Storage = StorageConfig{
//		Type: StorageTypeSFTP,
//		SFTP: &SFTPConfig{
//			Host: "example.com",
//		},
//		Rsync: &RsyncConfig{
//			Enabled:  true,
//			BasePath: "/backups",
//		},
//	}
//	cfg.Keys = map[string]KeyConfig{
//		"default": {
//			BufferMode: BufferModeFullyComplete,
//		},
//	}
//
//	if err := Validate(cfg); err != nil {
//		t.Fatalf("Validate() error = %v, want nil", err)
//	}
//}

func TestValidateRsyncEnabledRequiresBasePath(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Storage = StorageConfig{
		Type: StorageTypeSFTP,
		SFTP: &SFTPConfig{
			Host: "example.com",
		},
		Rsync: &RsyncConfig{
			Enabled: true,
		},
	}
	cfg.Keys = map[string]KeyConfig{
		"default": {
			BufferMode: BufferModeFullyComplete,
		},
	}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() succeeded, want error")
	}
	if !strings.Contains(err.Error(), "storage.rsync: base_path is required when enabled") {
		t.Fatalf("Validate() error = %v, want rsync base_path constraint", err)
	}
}

func TestValidateRejectsRsyncStorageType(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Storage = StorageConfig{
		Type: StorageType("rsync"),
	}
	cfg.Keys = map[string]KeyConfig{
		"default": {
			BufferMode: BufferModeFullyComplete,
		},
	}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() succeeded, want error")
	}
	if !strings.Contains(err.Error(), "only 'sftp' is supported") {
		t.Fatalf("Validate() error = %v, want sftp-only constraint", err)
	}
}
