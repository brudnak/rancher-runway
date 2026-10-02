package server

import (
	"context"
	"github.com/brudnak/ha-rancher-rke2/internal/prbuild"

	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	"net/http"

	"time"
)

func (h PRHandlers) Readiness(w http.ResponseWriter, r *http.Request) {
	var req prbuild.Request
	if !ReadIssueRequest(w, r, &req, h.Authorize) {
		return
	}
	if readinessStreamRequested(r) {
		h.StreamReadiness(w, r, req)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Minute)
	defer cancel()
	report, err := h.Backend().CheckIssue(ctx, req)
	if err != nil {
		http.Error(w, imagelookup.SafeError(err), prbuild.HTTPStatus(err))
		return
	}
	WriteJSON(w, report)
}
