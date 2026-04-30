// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package server

import (
	"net/http"
	"strings"

	"github.com/jc-lab/backupgate/internal/api"
	"github.com/jc-lab/backupgate/internal/config"
)

// Router dispatches incoming requests to the appropriate S3 or HTTP handler
// based on protocol detection mode.
type Router struct {
	handler       *api.Handler
	mode          config.APIMode
	forceProtocol string // "s3" or "http", set in port mode
}

// NewRouter creates a new Router.
func NewRouter(handler *api.Handler, mode config.APIMode) *Router {
	return &Router{
		handler: handler,
		mode:    mode,
	}
}

// ServeHTTP routes requests to the S3 or HTTP handler based on detection mode.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	protocol := r.detectProtocol(req)

	switch protocol {
	case "s3":
		r.handler.HandleS3(w, req)
	case "http":
		r.handler.HandleHTTP(w, req)
	default:
		http.Error(w, "unable to detect protocol", http.StatusBadRequest)
	}
}

// detectProtocol determines whether the request is S3 or HTTP.
func (r *Router) detectProtocol(req *http.Request) string {
	// In port mode, the protocol is predetermined
	if r.forceProtocol != "" {
		return r.forceProtocol
	}

	// In header mode, check for AWS S3 authorization header
	authHeader := req.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "AWS4-HMAC-SHA256") {
		return "s3"
	}

	// Default to HTTP API
	return "http"
}
