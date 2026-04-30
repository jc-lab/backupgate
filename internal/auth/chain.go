// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"context"
	"fmt"
)

// Chain tries multiple AuthProviders in order.
// Authentication succeeds if any provider returns successfully.
type Chain struct {
	providers []AuthProvider
}

// NewChain creates a Chain from the given providers.
func NewChain(providers ...AuthProvider) *Chain {
	return &Chain{providers: providers}
}

// Authenticate iterates through providers and returns on the first success.
// If all providers fail, returns ErrAuthentication.
func (c *Chain) Authenticate(ctx context.Context, req *AuthRequest) (string, error) {
	if len(c.providers) == 0 {
		return "", fmt.Errorf("%w: no auth providers configured", ErrAuthentication)
	}

	for _, p := range c.providers {
		identity, err := p.Authenticate(ctx, req)
		if err == nil {
			return identity, nil
		}
	}

	return "", ErrAuthentication
}

// LookupSecret returns the first secret exposed by a provider for username.
func (c *Chain) LookupSecret(username string) (string, bool) {
	for _, p := range c.providers {
		if sp, ok := p.(SecretProvider); ok {
			if secret, ok := sp.LookupSecret(username); ok {
				return secret, true
			}
		}
	}
	return "", false
}
