// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"context"
	"errors"
	"net/http"
)

// ErrAuthentication is returned when authentication fails.
var ErrAuthentication = errors.New("authentication failed")

// AuthRequest holds the credentials extracted from an incoming request.
type AuthRequest struct {
	Key      string
	Username string
	Password string
	Headers  http.Header
}

// AuthProvider verifies credentials for a single authentication method.
type AuthProvider interface {
	// Authenticate checks the request credentials.
	// Returns the authenticated identity string on success.
	Authenticate(ctx context.Context, req *AuthRequest) (identity string, err error)
}

// SecretProvider can expose a stored password/secret for protocols that need
// to verify request signatures instead of comparing a submitted password.
type SecretProvider interface {
	LookupSecret(username string) (secret string, ok bool)
}
