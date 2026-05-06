// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/jc-lab/backupgate/internal/auth"
	"github.com/jc-lab/backupgate/internal/config"
	"github.com/jc-lab/backupgate/internal/pipeline"
)

// Handler is the top-level API handler that dispatches to S3 or HTTP sub-handlers.
type Handler struct {
	cfg      *config.Config
	pipeline *pipeline.Pipeline
	auths    map[string]*auth.Chain // key -> auth chain
	keyMu    sync.Map               // per-key mutex for serializing concurrent uploads

	httpHandler http.Handler
	s3Handler   http.Handler
}

// NewHandler creates a new API Handler.
func NewHandler(cfg *config.Config, p *pipeline.Pipeline, auths map[string]*auth.Chain) *Handler {
	h := &Handler{
		cfg:      cfg,
		pipeline: p,
		auths:    auths,
	}
	h.httpHandler = &HTTPHandler{handler: h}
	h.s3Handler = NewS3Handler(h)
	return h
}

// HandleS3 handles S3-compatible API requests.
func (h *Handler) HandleS3(w http.ResponseWriter, r *http.Request) {
	h.s3Handler.ServeHTTP(w, r)
}

// HandleHTTP handles HTTP API requests.
func (h *Handler) HandleHTTP(w http.ResponseWriter, r *http.Request) {
	h.httpHandler.ServeHTTP(w, r)
}

// HealthCheck verifies the storage backend is reachable.
func (h *Handler) HealthCheck(ctx context.Context) error {
	return h.pipeline.HealthCheck(ctx)
}

// authenticate performs authentication for the given key using the configured auth chain.
func (h *Handler) authenticate(r *http.Request, key string) (string, error) {
	username, password, _ := r.BasicAuth()

	chain, ok := h.authChain(key)
	if !ok {
		return username, auth.ErrAuthentication
	}

	return chain.Authenticate(r.Context(), &auth.AuthRequest{
		Key:      key,
		Username: username,
		Password: password,
		Headers:  r.Header,
	})
}

func (h *Handler) authChain(key string) (*auth.Chain, bool) {
	if chain, ok := h.auths[key]; ok {
		return chain, true
	}
	best := ""
	for candidate := range h.auths {
		if candidate == "default" {
			continue
		}
		if key == candidate || strings.HasPrefix(key, candidate+"/") {
			if len(candidate) > len(best) {
				best = candidate
			}
		}
	}
	if best != "" {
		return h.auths[best], true
	}
	chain, ok := h.auths["default"]
	return chain, ok
}

// acquireKeyLock acquires a per-key mutex to serialize concurrent uploads.
func (h *Handler) acquireKeyLock(key string) *sync.Mutex {
	val, _ := h.keyMu.LoadOrStore(key, &sync.Mutex{})
	mu := val.(*sync.Mutex)
	mu.Lock()
	return mu
}

// extractKey extracts the backup key from the request URL path.
func extractKey(r *http.Request) string {
	key := r.URL.Path
	// Strip leading slash
	if len(key) > 0 && key[0] == '/' {
		key = key[1:]
	}
	return key
}

// writeError writes an error response with the given status code.
func writeError(w http.ResponseWriter, statusCode int, msg string) {
	logHTTPError(statusCode, msg)
	http.Error(w, msg, statusCode)
}

func logHTTPError(statusCode int, msg string) {
	level := slog.LevelWarn
	if statusCode >= http.StatusInternalServerError {
		level = slog.LevelError
	}
	slog.LogAttrs(
		context.Background(),
		level,
		"http api error",
		slog.Int("status", statusCode),
		slog.String("message", msg),
	)
}
