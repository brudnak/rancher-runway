package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

func ReadIssueRequest(w http.ResponseWriter, r *http.Request, payload any, authorize func(*http.Request) bool, limits ...int64) bool {
	w.Header().Set("Cache-Control", "no-store")
	if !authorize(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return false
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	limit := int64(64 << 10)
	if len(limits) > 0 {
		limit = limits[0]
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(payload); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "Issue Radar request is too large.", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "invalid JSON request", http.StatusBadRequest)
		}
		return false
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		http.Error(w, "request body must contain exactly one JSON object", http.StatusBadRequest)
		return false
	}
	return true
}
