// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// InlineProvider authenticates against credentials defined directly in config.
// Supports ${env.VARNAME} expression in username and password fields.
type InlineProvider struct {
	credentials []inlineCredential
}

type inlineCredential struct {
	username string
	password string
}

// NewInlineProvider creates an InlineProvider from raw credential entries.
// Expression resolution (${env.VARNAME}) is performed at construction time.
func NewInlineProvider(entries []struct{ Username, Password string }) *InlineProvider {
	creds := make([]inlineCredential, len(entries))
	for i, e := range entries {
		creds[i] = inlineCredential{
			username: resolveExpression(e.Username),
			password: resolveExpression(e.Password),
		}
	}
	return &InlineProvider{credentials: creds}
}

// Authenticate checks username/password against the inline credential list.
func (p *InlineProvider) Authenticate(_ context.Context, req *AuthRequest) (string, error) {
	for _, c := range p.credentials {
		if c.username == req.Username && c.password == req.Password {
			return req.Username, nil
		}
	}
	return "", fmt.Errorf("%w: invalid credentials", ErrAuthentication)
}

// LookupSecret returns the configured password for username.
func (p *InlineProvider) LookupSecret(username string) (string, bool) {
	for _, c := range p.credentials {
		if c.username == username {
			return c.password, true
		}
	}
	return "", false
}

// resolveExpression replaces ${env.VARNAME} patterns with environment variable values.
func resolveExpression(s string) string {
	const prefix = "${env."
	const suffix = "}"

	result := s
	for {
		start := strings.Index(result, prefix)
		if start == -1 {
			break
		}
		end := strings.Index(result[start:], suffix)
		if end == -1 {
			break
		}
		end += start

		varName := result[start+len(prefix) : end]
		envVal := os.Getenv(varName)
		result = result[:start] + envVal + result[end+1:]
	}
	return result
}
