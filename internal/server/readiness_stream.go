package server

import (
	"context"
	"encoding/json"
	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	"github.com/brudnak/ha-rancher-rke2/internal/prbuild"
	"net/http"
	"strings"
	"sync"
	"time"
)

func (h PRHandlers) StreamReadiness(w http.ResponseWriter, r *http.Request, req prbuild.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Minute)
	defer cancel()
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	var mu sync.Mutex
	send := func(event any) {
		mu.Lock()
		defer mu.Unlock()
		if err := json.NewEncoder(w).Encode(event); err != nil {
			cancel()
			return
		}
		_ = http.NewResponseController(w).Flush()
	}
	ctx = prbuild.WithProgress(ctx, func(progress prbuild.Progress) { send(map[string]any{"type": "progress", "progress": progress}) })
	prbuild.Notify(ctx, "Starting issue readiness scan…")
	report, err := h.Backend().CheckIssue(ctx, req)
	if err != nil {
		send(map[string]any{"type": "error", "error": imagelookup.SafeError(err)})
		return
	}
	send(map[string]any{"type": "report", "report": report})
}
func readinessStreamRequested(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "application/x-ndjson")
}
