// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// FileProvider authenticates against credentials loaded from a JSON or YAML file.
type FileProvider struct {
	path        string
	credentials []fileCredential
}

type fileCredential struct {
	Username string `json:"username" yaml:"username"`
	Password string `json:"password" yaml:"password"`
}

// NewFileProvider creates a FileProvider that loads credentials from the given path.
func NewFileProvider(path string) (*FileProvider, error) {
	p := &FileProvider{path: path}
	if err := p.load(); err != nil {
		return nil, err
	}
	return p, nil
}

// Authenticate checks username/password against the file-loaded credentials.
func (p *FileProvider) Authenticate(_ context.Context, req *AuthRequest) (string, error) {
	for _, c := range p.credentials {
		if c.Username == req.Username && c.Password == req.Password {
			return req.Username, nil
		}
	}
	return "", fmt.Errorf("%w: invalid credentials", ErrAuthentication)
}

// LookupSecret returns the loaded password for username.
func (p *FileProvider) LookupSecret(username string) (string, bool) {
	for _, c := range p.credentials {
		if c.Username == username {
			return c.Password, true
		}
	}
	return "", false
}

// load reads and parses the credentials file.
func (p *FileProvider) load() error {
	data, err := os.ReadFile(p.path)
	if err != nil {
		return fmt.Errorf("reading credentials file: %w", err)
	}

	var doc struct {
		Credentials []fileCredential `json:"credentials" yaml:"credentials"`
	}

	ext := strings.ToLower(filepath.Ext(p.path))
	switch ext {
	case ".json":
		err = json.Unmarshal(data, &doc)
	case ".yaml", ".yml":
		err = yaml.Unmarshal(data, &doc)
	default:
		trimmed := strings.TrimSpace(string(data))
		if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
			err = json.Unmarshal(data, &doc)
		} else {
			err = yaml.Unmarshal(data, &doc)
		}
	}
	if err != nil {
		return fmt.Errorf("parsing credentials file: %w", err)
	}
	if len(doc.Credentials) == 0 {
		return fmt.Errorf("credentials file %q contains no credentials", p.path)
	}
	p.credentials = doc.Credentials
	return nil
}
