package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jc-lab/backupgate/internal/api"
	"github.com/jc-lab/backupgate/internal/auth"
	"github.com/jc-lab/backupgate/internal/buffer"
	"github.com/jc-lab/backupgate/internal/config"
	"github.com/jc-lab/backupgate/internal/pipeline"
	"github.com/jc-lab/backupgate/internal/reader"
	"github.com/jc-lab/backupgate/internal/rotation"
	"github.com/jc-lab/backupgate/internal/storage"
)

func TestRouterLogsRequestStartAndEnd(t *testing.T) {
	collector := &recordingLogHandler{}
	prev := slog.Default()
	slog.SetDefault(slog.New(collector))
	t.Cleanup(func() { slog.SetDefault(prev) })

	router := NewRouter(api.NewHandler(nil, nil, nil), config.APIModeHeader)
	req := httptest.NewRequest(http.MethodPost, "/bucket/object", nil)
	req.RemoteAddr = "10.1.2.3:4567"
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	records := collector.recordsSnapshot()
	var requestLogs []slog.Record
	for _, record := range records {
		if record.Message == "request start" || record.Message == "request end" {
			requestLogs = append(requestLogs, record)
		}
	}
	if len(requestLogs) != 2 {
		t.Fatalf("request log count = %d, want 2", len(requestLogs))
	}
	if requestLogs[0].Message != "request start" {
		t.Fatalf("first request log = %q, want request start", requestLogs[0].Message)
	}
	if requestLogs[1].Message != "request end" {
		t.Fatalf("second request log = %q, want request end", requestLogs[1].Message)
	}

	startAttrs := attrsToMap(requestLogs[0])
	endAttrs := attrsToMap(requestLogs[1])

	if startAttrs["method"] != http.MethodPost {
		t.Fatalf("start method = %v, want POST", startAttrs["method"])
	}
	if startAttrs["remote_ip"] != "10.1.2.3" {
		t.Fatalf("start remote_ip = %v, want 10.1.2.3", startAttrs["remote_ip"])
	}
	if startAttrs["path"] != "/bucket/object" {
		t.Fatalf("start path = %v, want /bucket/object", startAttrs["path"])
	}
	if endAttrs["status_code"] != int64(http.StatusUnauthorized) {
		t.Fatalf("end status_code = %v, want 401", endAttrs["status_code"])
	}
}

func TestRouterLogsAuthenticatedUser(t *testing.T) {
	collector := &recordingLogHandler{}
	prev := slog.Default()
	slog.SetDefault(slog.New(collector))
	t.Cleanup(func() { slog.SetDefault(prev) })

	cfg := &config.Config{
		Buffer: config.BufferConfig{
			Memory:            1024,
			MaxMemory:         1024,
			SlidingWindowSize: 1024,
		},
		Storage: config.StorageConfig{
			Type: config.StorageTypeSFTP,
		},
		Keys: map[string]config.KeyConfig{
			"demo": {
				BufferMode: config.BufferModeOff,
				Auth: []config.AuthConfig{
					{
						Type: config.AuthTypeInline,
						Credentials: []config.CredentialEntry{
							{Username: "backup-agent", Password: "secret"},
						},
					},
				},
			},
		},
	}
	p := pipeline.NewPipeline(cfg, buffer.NewManager(cfg.Buffer), &noopBackend{}, rotation.NewManager())
	h := api.NewHandler(cfg, p, map[string]*auth.Chain{
		"demo": auth.NewChain(auth.NewInlineProvider([]struct{ Username, Password string }{
			{Username: "backup-agent", Password: "secret"},
		})),
	})
	router := NewRouter(h, config.APIModeHeader)

	req := httptest.NewRequest(http.MethodPost, "/demo", io.NopCloser(strings.NewReader("payload")))
	req.RemoteAddr = "10.1.2.3:4567"
	req.SetBasicAuth("backup-agent", "secret")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}

	records := collector.recordsSnapshot()
	var requestLogs []slog.Record
	var authLog *slog.Record
	for _, record := range records {
		if record.Message == "request start" || record.Message == "request end" {
			requestLogs = append(requestLogs, record)
		}
		if record.Message == "authentication success" {
			r := record
			authLog = &r
		}
	}
	if len(requestLogs) != 2 {
		t.Fatalf("request log count = %d, want 2", len(requestLogs))
	}
	if authLog == nil {
		t.Fatal("authentication success log not found")
	}

	authAttrs := attrsToMap(*authLog)
	if authAttrs["user"] != "backup-agent" {
		t.Fatalf("auth user = %v, want backup-agent", authAttrs["user"])
	}
	if authAttrs["request_id"] == "" {
		t.Fatal("auth request_id is empty")
	}
}

func TestRouterLogsAuthenticationFailure(t *testing.T) {
	collector := &recordingLogHandler{}
	prev := slog.Default()
	slog.SetDefault(slog.New(collector))
	t.Cleanup(func() { slog.SetDefault(prev) })

	router := NewRouter(api.NewHandler(nil, nil, nil), config.APIModeHeader)
	req := httptest.NewRequest(http.MethodPost, "/bucket/object", io.NopCloser(strings.NewReader("payload")))
	req.RemoteAddr = "10.1.2.3:4567"
	req.SetBasicAuth("backup-agent", "wrong")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	records := collector.recordsSnapshot()
	var failure *slog.Record
	for _, record := range records {
		if record.Message == "authentication failed" {
			r := record
			failure = &r
			break
		}
	}
	if failure == nil {
		t.Fatal("authentication failed log not found")
	}

	attrs := attrsToMap(*failure)
	if attrs["request_id"] == "" {
		t.Fatal("failure request_id is empty")
	}
	if attrs["user"] != "backup-agent" {
		t.Fatalf("failure user = %v, want backup-agent", attrs["user"])
	}
}

type recordingLogHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordingLogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordingLogHandler) Handle(_ context.Context, record slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, record.Clone())
	return nil
}

func (h *recordingLogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h *recordingLogHandler) WithGroup(string) slog.Handler { return h }

func (h *recordingLogHandler) recordsSnapshot() []slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]slog.Record, len(h.records))
	copy(out, h.records)
	return out
}

func attrsToMap(record slog.Record) map[string]any {
	out := make(map[string]any)
	record.Attrs(func(attr slog.Attr) bool {
		out[attr.Key] = attr.Value.Any()
		return true
	})
	return out
}

type noopBackend struct{}

func (b *noopBackend) Upload(context.Context, string, reader.UploadReader, int64) error { return nil }

func (b *noopBackend) ResumeUpload(context.Context, string, reader.UploadReader, int64, int64) error {
	return nil
}

func (b *noopBackend) Download(context.Context, string) (io.ReadCloser, error) {
	return nil, storage.ErrUnsupported
}

func (b *noopBackend) Delete(context.Context, string) error { return storage.ErrUnsupported }

func (b *noopBackend) List(context.Context, string) ([]storage.Entry, error) {
	return nil, storage.ErrUnsupported
}

func (b *noopBackend) RemoteSize(context.Context, string) (int64, error) {
	return 0, storage.ErrUnsupported
}

func (b *noopBackend) HealthCheck(context.Context) error { return nil }
