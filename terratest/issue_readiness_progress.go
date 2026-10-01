package test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

type readinessProgress struct {
	At      time.Time `json:"at"`
	Message string    `json:"message"`
}
type readinessProgressKey struct{}

func readinessNotify(ctx context.Context, message string) {
	if callback, ok := ctx.Value(readinessProgressKey{}).(func(readinessProgress)); ok && ctx.Err() == nil {
		callback(readinessProgress{At: time.Now().UTC(), Message: message})
	}
}
func readinessWithProgress(ctx context.Context, callback func(readinessProgress)) context.Context {
	return context.WithValue(ctx, readinessProgressKey{}, callback)
}
func (p *localControlPanel) streamIssueReadiness(w http.ResponseWriter, r *http.Request, req issueReadinessRequest) {
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
	ctx = readinessWithProgress(ctx, func(progress readinessProgress) { send(map[string]any{"type": "progress", "progress": progress}) })
	readinessNotify(ctx, "Starting issue readiness scan…")
	report, err := p.prBuildVerifierBackend().CheckIssue(ctx, req)
	if err != nil {
		send(map[string]any{"type": "error", "error": imageLookupSafeError(err)})
		return
	}
	send(map[string]any{"type": "report", "report": report})
}
func readinessStreamRequested(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "application/x-ndjson")
}
