// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/jc-lab/backupgate/internal/config"
	"github.com/jc-lab/backupgate/internal/pipeline"
)

// HTTPHandler handles HTTP API requests (POST uploads with Basic Auth).
type HTTPHandler struct {
	handler *Handler
}

// ServeHTTP handles HTTP POST upload requests.
func (h *HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "only POST is supported")
		return
	}

	h.postUpload(w, r)
}

// postUpload handles HTTP POST file upload.
func (h *HTTPHandler) postUpload(w http.ResponseWriter, r *http.Request) {
	key := extractKey(r)
	if key == "" {
		writeError(w, http.StatusBadRequest, "missing upload key")
		return
	}
	if _, err := h.handler.authenticate(r, key); err != nil {
		writeError(w, http.StatusUnauthorized, "authentication failed")
		return
	}
	keyCfg, ok := h.handler.cfg.ResolveKeyConfig(key)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown key")
		return
	}

	mu := h.handler.acquireKeyLock(key)
	defer mu.Unlock()

	body := pipeline.NewStreamingUploadReader(r.Body)
	if keyCfg.BufferMode != config.BufferModeOff {
		buf, err := h.handler.pipeline.NewUploadReader(key, r.ContentLength)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer buf.Close()
		if _, err := io.Copy(buf, r.Body); err != nil {
			writeError(w, http.StatusBadRequest, "reading request body: "+err.Error())
			return
		}
		body = buf
	}

	result, err := h.handler.pipeline.Process(r.Context(), key, body, pipeline.UploadMetadata{
		ContentLength:  r.ContentLength,
		ExpectedSHA256: r.Header.Get("X-Backup-SHA256"),
	})
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, pipeline.ErrVerificationFailed) {
			status = http.StatusConflict
		}
		writeError(w, status, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("ETag", `"`+result.MD5+`"`)
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(result)
}
