package server

import (
	"encoding/json"
	"github.com/brudnak/ha-rancher-rke2/internal/cachelab"
	"net/http"
	"runtime"
	"strings"
)

func (h CacheHandlers) Handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !h.Authorize(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	s, err := h.Backend()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if r.Method == http.MethodGet {
		if s.HasClusterResolver() {
			if err := h.Backfill(s); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		}

		WriteJSON(w, map[string]any{"library": s.Library(), "root": s.Root(), "keychain": runtime.GOOS == "darwin"})
		return
	}
	if strings.HasSuffix(r.URL.Path, "/import") {
		h.Import(s, w, r)
		return
	}
	var req cachelab.Request
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&req); err != nil {
		http.Error(w, "invalid Cache Lab request", 400)
		return
	}
	var result any
	switch req.Action {
	case "save-workspace":
		result, err = s.SaveWorkspace(r.Context(), req)
	case "discover":
		result, err = s.Discover(r.Context(), req.Workspace)
	case "capture":
		result, err = s.StartCapture(req)
	case "steve":
		result, err = h.Steve(s, req.RunID)
	case "schema", "rows", "query", "diff":
		result, err = s.Inspect(r.Context(), req)
	case "export":
		result, err = s.ExportSnapshot(req.Snapshot)
	default:
		result, err = s.Mutate(req)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	WriteJSON(w, result)
}

type CacheHandlers struct {
	Authorize func(*http.Request) bool
	Backend   func() (*cachelab.Service, error)
	Backfill  func(*cachelab.Service) error
	Steve     func(*cachelab.Service, string) (any, error)
}
