// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"

	"github.com/jc-lab/backupgate/internal/api"
	"github.com/jc-lab/backupgate/internal/config"
)

// Server manages one or more HTTP listeners for the BackupGate API.
type Server struct {
	cfg         *config.Config
	handler     *api.Handler
	healthCheck func(context.Context) error
	servers     []*http.Server
	wg          sync.WaitGroup
}

// NewServer creates a new Server.
func NewServer(cfg *config.Config, handler *api.Handler) *Server {
	return &Server{
		cfg:         cfg,
		handler:     handler,
		healthCheck: handler.HealthCheck,
	}
}

// Start creates and starts HTTP server(s) based on the API mode configuration.
// In header mode, a single server listens on one port.
// In port mode, separate servers listen for S3 and HTTP traffic.
func (s *Server) Start(ctx context.Context) error {
	if err := s.startDebugServer(); err != nil {
		return err
	}
	switch s.cfg.Server.API.Mode {
	case config.APIModeHeader:
		return s.startHeaderMode(ctx)
	case config.APIModePort:
		return s.startPortMode(ctx)
	default:
		return fmt.Errorf("unknown API mode: %s", s.cfg.Server.API.Mode)
	}
}

func (s *Server) startDebugServer() error {
	if s.cfg.Server.Debug == nil {
		return nil
	}

	srv := &http.Server{
		Addr:    s.cfg.Server.Debug.Listen,
		Handler: s.debugHandler(),
	}
	s.servers = append(s.servers, srv)

	slog.Info("starting server", "mode", "debug", "listen", s.cfg.Server.Debug.Listen)
	l, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return fmt.Errorf("failed to debug server listen: %w", err)
	}

	s.wg.Go(func() {
		defer l.Close()
		if err := srv.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server serve failed", "mode", "debug", "listen", s.cfg.Server.Debug.Listen, "err", err)
		}
	})

	return nil
}

func (s *Server) debugHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/health", s.handleDebugHealth)
	return mux
}

func (s *Server) handleDebugHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if s.healthCheck != nil {
		if err := s.healthCheck(r.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status": "DOWN",
				"error":  err.Error(),
			})
			return
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "UP"})
}

// startHeaderMode starts a single server with the header-detection router.
func (s *Server) startHeaderMode(ctx context.Context) error {
	router := NewRouter(s.handler, config.APIModeHeader)
	srv := &http.Server{
		Addr:    s.cfg.Server.API.Listen,
		Handler: router,
	}
	s.servers = append(s.servers, srv)

	slog.Info("starting server", "mode", "header", "listen", s.cfg.Server.API.Listen)
	l, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	s.wg.Go(func() {
		defer l.Close()
		if err := srv.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server serve failed", "mode", "header", "listen", s.cfg.Server.API.Listen, "err", err)
		}
	})

	return nil
}

// startPortMode starts two servers: one for S3, one for HTTP.
func (s *Server) startPortMode(ctx context.Context) error {
	var success bool

	s3Router := NewRouter(s.handler, config.APIModePort)
	s3Router.forceProtocol = "s3"
	httpRouter := NewRouter(s.handler, config.APIModePort)
	httpRouter.forceProtocol = "http"

	httpSrv := &http.Server{
		Addr:    s.cfg.Server.API.HTTP.Listen,
		Handler: httpRouter,
	}
	s3Srv := &http.Server{
		Addr:    s.cfg.Server.API.S3.Listen,
		Handler: s3Router,
	}
	s.servers = append(s.servers, httpSrv, s3Srv)

	slog.Info("starting server", "mode", "port", "protocol", "http", "listen", s.cfg.Server.API.HTTP.Listen)
	httpListener, err := net.Listen("tcp", httpSrv.Addr)
	if err != nil {
		return fmt.Errorf("failed to http server listen: %w", err)
	}
	defer func() {
		if !success {
			_ = httpListener.Close()
		}
	}()

	slog.Info("starting server", "mode", "port", "protocol", "s3", "listen", s.cfg.Server.API.S3.Listen)
	s3Listener, err := net.Listen("tcp", s3Srv.Addr)
	if err != nil {
		return fmt.Errorf("failed to s3 server listen: %w", err)
	}
	defer func() {
		if !success {
			_ = s3Listener.Close()
		}
	}()

	s.wg.Go(func() {
		defer httpListener.Close()

		if err := httpSrv.Serve(httpListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server serve failed", "mode", "port", "protocol", "http", "listen", s.cfg.Server.API.HTTP.Listen, "err", err)
		}
	})

	s.wg.Go(func() {
		defer s3Listener.Close()

		if err := s3Srv.Serve(s3Listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server serve failed", "mode", "port", "protocol", "s3", "listen", s.cfg.Server.API.S3.Listen, "err", err)
		}
	})

	success = true

	return nil
}

// Shutdown gracefully shuts down all servers.
func (s *Server) Shutdown(ctx context.Context) error {
	var lastErr error

	for _, srv := range s.servers {
		slog.Info("shutting down server", "listen", srv.Addr)
		if err := srv.Shutdown(ctx); err != nil {
			slog.Error("server shutdown failed", "listen", srv.Addr, "err", err)
			lastErr = err
		} else {
			slog.Info("server shutdown complete", "listen", srv.Addr)
		}
	}

	if lastErr != nil {
		return lastErr
	}

	waitCh := make(chan struct{})
	go func() {
		s.wg.Wait()
		waitCh <- struct{}{}
	}()

	select {
	case <-waitCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
