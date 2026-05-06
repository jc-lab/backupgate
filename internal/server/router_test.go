package server

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/jc-lab/backupgate/internal/api"
	"github.com/jc-lab/backupgate/internal/config"
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
