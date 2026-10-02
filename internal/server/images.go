package server

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	"io"
	"net/http"
)

func DecodeImageJSON(w http.ResponseWriter, request *http.Request, destination any) error {
	request.Body = http.MaxBytesReader(w, request.Body, imagelookup.RequestLimit)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return &imagelookup.InputError{Message: "invalid JSON request: " + err.Error()}
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return &imagelookup.InputError{Message: "request body must contain exactly one JSON object"}
	}
	return nil
}

func (h ImageHandlers) Search(w http.ResponseWriter, request *http.Request) {
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
	var payload imagelookup.SearchRequest
	if err := DecodeImageJSON(w, request, &payload); err != nil {
		http.Error(w, imagelookup.SafeError(err), imagelookup.HTTPStatus(err))
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), imagelookup.SearchTimeout)
	defer cancel()
	response, err := h.Backend().Search(ctx, payload)
	if err != nil {
		http.Error(w, imagelookup.SafeError(err), imagelookup.HTTPStatus(err))
		return
	}
	WriteJSON(w, response)
}

func (h ImageHandlers) Inspect(w http.ResponseWriter, request *http.Request) {
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
	var payload imagelookup.InspectRequest
	if err := DecodeImageJSON(w, request, &payload); err != nil {
		http.Error(w, imagelookup.SafeError(err), imagelookup.HTTPStatus(err))
		return
	}
	timeout := imagelookup.SearchTimeout
	if payload.IncludeBuildYAML {
		timeout = imagelookup.InspectTimeout
	}
	ctx, cancel := context.WithTimeout(request.Context(), timeout)
	defer cancel()
	response, err := h.Backend().Inspect(ctx, payload)
	if err != nil {
		http.Error(w, imagelookup.SafeError(err), imagelookup.HTTPStatus(err))
		return
	}
	WriteJSON(w, response)
}

func (h ImageHandlers) SourceBuildYAML(w http.ResponseWriter, request *http.Request) {
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
	var payload imagelookup.SourceBuildYAMLRequest
	if err := DecodeImageJSON(w, request, &payload); err != nil {
		http.Error(w, imagelookup.SafeError(err), imagelookup.HTTPStatus(err))
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), imagelookup.SourceTimeout)
	defer cancel()
	response, err := h.Backend().FetchSourceBuildYAML(ctx, payload)
	if err != nil {
		http.Error(w, imagelookup.SafeError(err), imagelookup.HTTPStatus(err))
		return
	}
	WriteJSON(w, response)
}

type ImageHandlers struct {
	Authorize func(*http.Request) bool
	Backend   func() *imagelookup.Service
}
