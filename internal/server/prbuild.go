package server

import (
	"context"
	"errors"
	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	"github.com/brudnak/ha-rancher-rke2/internal/prbuild"
	"net/http"
)

func (h PRHandlers) Verify(w http.ResponseWriter, request *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !h.Authorize(request) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if request.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload prbuild.VerifyRequest
	if err := DecodeImageJSON(w, request, &payload); err != nil {
		http.Error(w, imagelookup.SafeError(err), imagelookup.HTTPStatus(err))
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), prbuild.VerifyTimeout)
	defer cancel()
	response, err := h.Backend().Verify(ctx, payload)
	if err != nil {
		message := imagelookup.SafeError(err)
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			message = "PR image verification timed out"
		}
		http.Error(w, message, prbuild.HTTPStatus(err))
		return
	}
	WriteJSON(w, response)
}

type PRHandlers struct {
	Authorize func(*http.Request) bool
	Backend   func() *prbuild.Service
}
