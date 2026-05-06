// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package server

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/jc-lab/backupgate/internal/api"
	"github.com/jc-lab/backupgate/internal/config"
	"github.com/jc-lab/backupgate/internal/requestmeta"
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
	start := time.Now()
	remoteIP := remoteIP(req.RemoteAddr)
	protocol := r.detectProtocol(req)
	req = req.WithContext(requestmeta.WithInfo(req.Context()))
	requestID := requestID(req.Context())
	slog.Debug("request start",
		"request_id", requestID,
		"method", req.Method,
		"remote_ip", remoteIP,
		"path", req.URL.Path,
		"protocol", protocol,
		"content-length", req.Header.Get("content-length"),
	)

	rw := &statusResponseWriter{ResponseWriter: w}
	defer func() {
		slog.Debug("request end",
			"request_id", requestID,
			"method", req.Method,
			"remote_ip", remoteIP,
			"path", req.URL.Path,
			"protocol", protocol,
			"status_code", rw.statusCode,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}()

	switch protocol {
	case "s3":
		r.handler.HandleS3(rw, req)
	case "http":
		r.handler.HandleHTTP(rw, req)
	default:
		http.Error(rw, "unable to detect protocol", http.StatusBadRequest)
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

func remoteIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

func requestID(ctx context.Context) string {
	if info := requestmeta.FromContext(ctx); info != nil {
		return info.RequestID
	}
	return ""
}

type statusResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *statusResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *statusResponseWriter) Write(p []byte) (int, error) {
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	return w.ResponseWriter.Write(p)
}
