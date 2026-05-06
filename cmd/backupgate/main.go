// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jc-lab/backupgate/internal/api"
	"github.com/jc-lab/backupgate/internal/auth"
	"github.com/jc-lab/backupgate/internal/buffer"
	"github.com/jc-lab/backupgate/internal/config"
	"github.com/jc-lab/backupgate/internal/pipeline"
	"github.com/jc-lab/backupgate/internal/rotation"
	"github.com/jc-lab/backupgate/internal/server"
	"github.com/jc-lab/backupgate/internal/storage"
	"github.com/jc-lab/backupgate/internal/version"
)

func main() {
	var configPath string
	var logLevelStr string
	var showVersion bool
	flag.StringVar(&configPath, "config", getEnvOrDefault("BACKUPGATE_CONFIG", "config.yaml"), "path to configuration file")
	flag.StringVar(&logLevelStr, "log-level", getEnvOrDefault("BACKUPGATE_LOG_LEVEL", "info"), "path to configuration file")
	flag.BoolVar(&showVersion, "version", false, "print version information and exit")
	flag.Parse()

	if showVersion {
		fmt.Fprintln(os.Stdout, version.String())
		return
	}

	var logLevel slog.Level
	if err := logLevel.UnmarshalText([]byte(strings.ToUpper(logLevelStr))); err != nil {
		logLevel = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,
	})))

	if err := run(configPath); err != nil {
		slog.Error("fatal error", "err", err)
		os.Exit(1)
	}
}

func getEnvOrDefault(name string, defaultValue string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return defaultValue
}

func run(configPath string) error {
	slog.Info("loading config", "path", configPath)

	// Load and validate configuration
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if err := config.Validate(cfg); err != nil {
		return fmt.Errorf("validating config: %w", err)
	}
	slog.Info("config loaded and validated", "path", configPath)

	// Initialize storage backend
	backend, err := initStorage(cfg)
	if err != nil {
		return fmt.Errorf("initializing storage: %w", err)
	}
	slog.Info("storage backend initialized", "type", cfg.Storage.Type, "rsync_uploads", rsyncUploadsEnabled(cfg))

	// Initialize buffer manager
	bufMgr := buffer.NewManager(cfg.Buffer, rsyncUploadsEnabled(cfg))

	// Initialize rotation manager
	rotMgr := rotation.NewManager()

	// Initialize upload pipeline
	p := pipeline.NewPipeline(cfg, bufMgr, backend, rotMgr)

	// Initialize auth chains per key
	auths, err := initAuthChains(cfg)
	if err != nil {
		return fmt.Errorf("initializing auth: %w", err)
	}
	slog.Info("auth chains initialized", "keys", len(auths))

	// Create API handler
	handler := api.NewHandler(cfg, p, auths)

	// Create and start server
	srv := server.NewServer(cfg, handler)

	if err := srv.Start(context.Background()); err != nil {
		return err
	}

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	select {
	case sig := <-sigCh:
		slog.Info("shutdown signal received", "signal", sig.String())

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		return srv.Shutdown(shutdownCtx)
	}
}

// initStorage creates the storage backend based on configuration.
func initStorage(cfg *config.Config) (storage.Backend, error) {
	switch cfg.Storage.Type {
	case config.StorageTypeSFTP:
		sftpBackend, err := storage.NewSFTPBackend(cfg.Storage.SFTP)
		if err != nil {
			return nil, err
		}
		if rsyncUploadsEnabled(cfg) {
			return storage.NewRsyncUploadBackend(sftpBackend, cfg.Storage.Rsync), nil
		}
		return sftpBackend, nil
	default:
		return nil, fmt.Errorf("unknown storage type: %s", cfg.Storage.Type)
	}
}

func rsyncUploadsEnabled(cfg *config.Config) bool {
	return cfg.Storage.Rsync != nil && cfg.Storage.Rsync.Enabled
}

// initAuthChains builds auth.Chain instances for each key from configuration.
func initAuthChains(cfg *config.Config) (map[string]*auth.Chain, error) {
	chains := make(map[string]*auth.Chain)

	for keyName, keyCfg := range cfg.Keys {
		providers, err := buildProviders(keyCfg.Auth)
		if err != nil {
			return nil, fmt.Errorf("key %q: %w", keyName, err)
		}
		chains[keyName] = auth.NewChain(providers...)
	}

	return chains, nil
}

// buildProviders creates AuthProvider instances from auth config entries.
func buildProviders(authCfgs []config.AuthConfig) ([]auth.AuthProvider, error) {
	var providers []auth.AuthProvider

	for _, ac := range authCfgs {
		switch ac.Type {
		case config.AuthTypeInline:
			entries := make([]struct{ Username, Password string }, len(ac.Credentials))
			for i, c := range ac.Credentials {
				entries[i] = struct{ Username, Password string }{c.Username, c.Password}
			}
			providers = append(providers, auth.NewInlineProvider(entries))

		case config.AuthTypeFile:
			p, err := auth.NewFileProvider(ac.Path)
			if err != nil {
				return nil, fmt.Errorf("file provider: %w", err)
			}
			providers = append(providers, p)

		case config.AuthTypeHtpasswd:
			p, err := auth.NewHtpasswdProvider(ac.Path)
			if err != nil {
				return nil, fmt.Errorf("htpasswd provider: %w", err)
			}
			providers = append(providers, p)

		case config.AuthTypeExternal:
			timeout, err := parseTimeout(ac.Timeout)
			if err != nil {
				return nil, fmt.Errorf("external provider timeout: %w", err)
			}
			providers = append(providers, auth.NewExternalProvider(ac.Command, ac.Args, timeout))

		default:
			return nil, fmt.Errorf("unknown auth type: %s", ac.Type)
		}
	}

	return providers, nil
}

// parseTimeout parses a duration string, returning a default of 5s if empty.
func parseTimeout(s string) (time.Duration, error) {
	if s == "" {
		return 5 * time.Second, nil
	}
	return time.ParseDuration(s)
}
