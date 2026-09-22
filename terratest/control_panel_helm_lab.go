package test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
)

// Save through the local backend, like kubeconfigs: browser blob downloads are
// not handled by the desktop webview.
func (p *localControlPanel) handleHelmLabSave(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Filename string `json:"filename"`
		Content  string `json:"content"`
	}
	// Allow room for JSON escaping of an imported values file (up to 1 MB).
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&req)
	if err == nil {
		if trailing := decoder.Decode(new(any)); trailing != io.EOF {
			err = trailing
			if err == nil {
				err = errors.New("multiple JSON values")
			}
		}
	}
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "Helm Lab export is too large to save (maximum 8 MB).", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "invalid request body", http.StatusBadRequest)
		}
		return
	}
	if req.Filename != "values.yaml" && req.Filename != "setup.sh" {
		http.Error(w, "filename must be values.yaml or setup.sh", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		http.Error(w, "no Helm Lab output to save", http.StatusBadRequest)
		return
	}

	// Both exports can include passwords and environment values. Keep them
	// private, and never execute a downloaded setup script.
	path, err := saveDownloadFile(req.Filename, []byte(req.Content), 0o600)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{
		"filename": filepath.Base(path),
		"path":     path,
	})
}
