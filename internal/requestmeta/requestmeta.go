// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package requestmeta

import (
	"context"
	"crypto/rand"
	"fmt"
)

type ctxKey struct{}

// Info carries request-scoped metadata for logs.
type Info struct {
	RequestID string
	User      string
}

// WithInfo attaches a mutable request info holder to the context.
func WithInfo(ctx context.Context) context.Context {
	return context.WithValue(ctx, ctxKey{}, &Info{RequestID: newRequestID()})
}

// FromContext returns the request info holder if present.
func FromContext(ctx context.Context) *Info {
	if info, ok := ctx.Value(ctxKey{}).(*Info); ok {
		return info
	}
	return nil
}

// SetUser updates the authenticated user for this request.
func SetUser(ctx context.Context, user string) {
	if info := FromContext(ctx); info != nil {
		info.User = user
	}
}

func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
