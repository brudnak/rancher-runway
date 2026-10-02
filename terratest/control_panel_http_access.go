package test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
)

func (p *localControlPanel) handleKubeconfigDownload(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedLocalBrowserRead(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clusterID := strings.TrimSpace(r.URL.Query().Get("cluster"))
	if clusterID == "" {
		http.Error(w, "cluster is required", http.StatusBadRequest)
		return
	}

	cluster, err := p.clusterByID(clusterID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	content, filename, err := p.kubeconfigContentForDownload(cluster)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/x-yaml; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	_, _ = w.Write(content)
}

func (p *localControlPanel) handleKubeconfigSave(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Cluster string `json:"cluster"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	clusterID := strings.TrimSpace(req.Cluster)
	if clusterID == "" {
		http.Error(w, "cluster is required", http.StatusBadRequest)
		return
	}

	cluster, err := p.clusterByID(clusterID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	content, filename, err := p.kubeconfigContentForDownload(cluster)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	path, err := saveDownloadFile(filename, content, 0o600)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]string{
		"filename": filepath.Base(path),
		"path":     path,
	})
}

func (p *localControlPanel) handleHelmCommandDownload(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedLocalBrowserRead(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clusterID := strings.TrimSpace(r.URL.Query().Get("cluster"))
	if clusterID == "" {
		http.Error(w, "cluster is required", http.StatusBadRequest)
		return
	}

	// A Helm command is stored beside the local HA kubeconfig. Resolve that
	// cluster directly from its run record so copying the command does not wait
	// for the live Terraform, kubectl, and downstream-cluster discovery used by
	// the rest of the cluster panel. Fall back for non-local and legacy cases so
	// their existing lookup and error behavior remains unchanged.
	cluster, found := p.recordedLocalHAClusterByID(clusterID)
	if !found {
		var err error
		cluster, err = p.clusterByID(clusterID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
	}

	command, err := p.helmCommandForCluster(cluster)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("mode")), "upgrade") {
		command, err = prepareHelmUpgradeCommand(command)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(command))
}

func (p *localControlPanel) handleOpenURL(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	rawURL := strings.TrimSpace(req.URL)
	if err := openExternalURL(rawURL); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, map[string]string{"status": "opened"})
}

func (p *localControlPanel) handleOpenPath(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Path   string `json:"path"`
		Reveal bool   `json:"reveal"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	path, err := p.resolveAllowedLocalPath(req.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := openLocalPath(path, req.Reveal); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, map[string]string{
		"status": "opened",
		"path":   path,
	})
}
