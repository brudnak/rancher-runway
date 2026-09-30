package test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"gopkg.in/yaml.v3"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// This registry owns display metadata only. Credentials remain in each lab's
// private store; cluster IDs never change when users rename a workspace.
type clusterWorkspaceRecord struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Nickname   string `json:"nickname,omitempty"`
	RunID      string `json:"runId,omitempty"`
	URL        string `json:"url,omitempty"`
	Version    string `json:"version,omitempty"`
	Role       string `json:"role,omitempty"`
	Kubeconfig string `json:"kubeconfig,omitempty"`
	Context    string `json:"context,omitempty"`
	Archived   bool   `json:"archived"`
}

type clusterWorkspaceManifest struct {
	Version  int                      `json:"version"`
	Clusters []clusterWorkspaceRecord `json:"clusters"`
}

type clusterWorkspaceResponse struct {
	clusterWorkspaceRecord
	clusterLabData
}

func (p *localControlPanel) clusterWorkspacesPath() string {
	return filepath.Join(automationOutputDir(), "control-panel", "cluster-workspaces.json")
}

func (p *localControlPanel) loadClusterWorkspacesLocked() error {
	if p.clusterWorkspaces != nil {
		return nil
	}
	manifest := clusterWorkspaceManifest{Version: 1}
	raw, err := os.ReadFile(p.clusterWorkspacesPath())
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cluster workspace history could not be read: %w", err)
	}
	if err == nil {
		if len(raw) > 8<<20 || json.Unmarshal(raw, &manifest) != nil || manifest.Version != 1 {
			return errors.New("cluster workspace history could not be read; the existing file was preserved")
		}
	}
	records := map[string]clusterWorkspaceRecord{}
	for _, record := range manifest.Clusters {
		if !validClusterWorkspaceText(record.ID, 512, false) {
			return errors.New("cluster workspace history has an invalid identity; the existing file was preserved")
		}
		if _, exists := records[record.ID]; exists {
			return errors.New("cluster workspace history has duplicate identities; the existing file was preserved")
		}
		records[record.ID] = record
	}
	p.clusterWorkspaces = records
	return nil
}

func (p *localControlPanel) persistClusterWorkspacesLocked() error {
	manifest := clusterWorkspaceManifest{Version: 1, Clusters: make([]clusterWorkspaceRecord, 0, len(p.clusterWorkspaces))}
	for _, record := range p.clusterWorkspaces {
		manifest.Clusters = append(manifest.Clusters, record)
	}
	sort.Slice(manifest.Clusters, func(i, j int) bool { return manifest.Clusters[i].ID < manifest.Clusters[j].ID })
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if len(raw) > 8<<20 {
		return errors.New("cluster workspace metadata exceeds 8 MiB")
	}
	return writePrivateConfigAtomically(p.clusterWorkspacesPath(), raw)
}

func validClusterWorkspaceText(s string, limit int, empty bool) bool {
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) > limit || (!empty && strings.TrimSpace(s) == "") {
		return false
	}
	return !strings.ContainsFunc(s, unicode.IsControl)
}

func clusterWorkspaceURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	base, err := cacheLabURL(raw)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	if (u.Scheme == "https" && u.Port() == "443") || (u.Scheme == "http" && u.Port() == "80") {
		u.Host = u.Hostname()
		if strings.Contains(u.Host, ":") {
			u.Host = "[" + u.Host + "]"
		}
	}
	return u.String(), nil
}

func clusterWorkspaceHostKey(raw string) (string, error) {
	base, err := clusterWorkspaceURL(raw)
	if err != nil || base == "" {
		return "", err
	}
	u, _ := url.Parse(base)
	return u.Host + strings.TrimRight(u.EscapedPath(), "/"), nil
}

func clusterWorkspaceKubeconfigKey(path, context string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if full, err := filepath.Abs(path); err == nil {
		path = full
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	return filepath.Clean(path) + "\x00" + clusterWorkspaceKubeconfigContext(path, context)
}

// Only the current-context label is decoded. No kubeconfig exec plugin runs.
func clusterWorkspaceKubeconfigContext(path, context string) string {
	if context = strings.TrimSpace(context); context != "" {
		return context
	}
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	if info, err := file.Stat(); err != nil || !info.Mode().IsRegular() || info.Size() > 4<<20 {
		return ""
	}
	raw, err := io.ReadAll(io.LimitReader(file, 4<<20+1))
	if err != nil || len(raw) > 4<<20 {
		return ""
	}
	var config struct {
		CurrentContext string `yaml:"current-context"`
	}
	if yaml.Unmarshal(raw, &config) != nil {
		return ""
	}
	return strings.TrimSpace(config.CurrentContext)
}

func (p *localControlPanel) rememberClusterWorkspaces(clusters []clusterView) error {
	p.clusterWorkspacesMu.Lock()
	defer p.clusterWorkspacesMu.Unlock()
	if err := p.loadClusterWorkspacesLocked(); err != nil {
		return err
	}
	before := make(map[string]clusterWorkspaceRecord, len(p.clusterWorkspaces))
	for id, record := range p.clusterWorkspaces {
		before[id] = record
	}
	changed := false
	for _, cluster := range clusters {
		if !validClusterWorkspaceText(cluster.ID, 512, false) {
			continue
		}
		old := p.clusterWorkspaces[cluster.ID]
		record := old
		record.ID = cluster.ID
		if cluster.Name != "" {
			record.Name = cluster.Name
		}
		if record.Name == "" {
			record.Name = record.ID
		}
		if cluster.RunID != "" {
			record.RunID = cluster.RunID
		}
		if base, err := clusterWorkspaceURL(cluster.RancherURL); err == nil && base != "" {
			record.URL = base
		}
		if cluster.Version != "" {
			record.Version = cluster.Version
		}
		if cluster.Role != "" {
			record.Role = cluster.Role
		}
		if cluster.KubeconfigPath != "" {
			if record.Kubeconfig != cluster.KubeconfigPath || record.Context == "" {
				record.Context = clusterWorkspaceKubeconfigContext(cluster.KubeconfigPath, "")
			}
			record.Kubeconfig = cluster.KubeconfigPath
		}
		record.Archived = false
		if record != old {
			p.clusterWorkspaces[record.ID] = record
			changed = true
		}
	}
	if !changed {
		return nil
	}
	if err := p.persistClusterWorkspacesLocked(); err != nil {
		p.clusterWorkspaces = before
		return err
	}
	return nil
}

// Candidate lookup never contacts Rancher or acquires a lab service lock.
func (p *localControlPanel) labClusterCandidates() ([]clusterWorkspaceRecord, error) {
	p.mu.Lock()
	clusters := make([]clusterView, 0, len(p.clusterSnapshot))
	discovered := map[string]bool{}
	for _, cluster := range p.clusterSnapshot {
		clusters = append(clusters, cluster)
		discovered[cluster.ID] = true
	}
	p.mu.Unlock()
	for _, cluster := range p.localLabClusterWorkspaceViews() {
		clusters = append(clusters, cluster)
		discovered[cluster.ID] = true
	}
	if err := p.rememberClusterWorkspaces(clusters); err != nil {
		return nil, err
	}
	recorded := map[string]bool{}
	for _, record := range p.listRunRecords() {
		recorded[record.RunID] = true
	}
	p.clusterWorkspacesMu.Lock()
	defer p.clusterWorkspacesMu.Unlock()
	result := make([]clusterWorkspaceRecord, 0, len(p.clusterWorkspaces))
	for _, record := range p.clusterWorkspaces {
		record.Archived = !discovered[record.ID] && !recorded[record.RunID]
		result = append(result, record)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

// Explicit IDs are validated against their target. Automatic association requires
// a unique identity, including archived matches; a reused hostname is ambiguous.
func (p *localControlPanel) resolveLabCluster(clusterID, host, kubeconfig, context string) (string, error) {
	candidates, err := p.labClusterCandidates()
	if err != nil {
		return "", err
	}
	hostKey, err := clusterWorkspaceHostKey(host)
	if err != nil {
		return "", fmt.Errorf("invalid cluster workspace host: %w", err)
	}
	kubeKey := clusterWorkspaceKubeconfigKey(kubeconfig, context)
	clusterID = strings.TrimSpace(clusterID)
	matches := []clusterWorkspaceRecord{}
	for _, candidate := range candidates {
		candidateHost, _ := clusterWorkspaceHostKey(candidate.URL)
		candidateKube := clusterWorkspaceKubeconfigKey(candidate.Kubeconfig, candidate.Context)
		hostMatch := hostKey != "" && candidateHost == hostKey
		kubeMatch := kubeKey != "" && candidateKube == kubeKey
		if candidate.ID == clusterID && clusterID != "" {
			if hostKey != "" && candidateHost != "" && !hostMatch {
				return "", errors.New("selected cluster does not match the Rancher URL; choose the matching cluster")
			}
			if kubeKey != "" && candidateKube != "" && !kubeMatch {
				return "", errors.New("selected cluster does not match the kubeconfig and context")
			}
			return candidate.ID, nil
		}
		if hostMatch || kubeMatch {
			matches = append(matches, candidate)
		}
	}
	if clusterID != "" {
		return "", errors.New("selected cluster workspace no longer exists; choose it again")
	}
	if len(matches) == 1 {
		return matches[0].ID, nil
	}
	if len(matches) > 1 {
		return "", errors.New("this target matches several cluster histories; choose its cluster explicitly")
	}
	if hostKey == "" && kubeKey == "" {
		return "", nil
	}
	key := "host:" + hostKey
	base, _ := clusterWorkspaceURL(host)
	name := hostKey
	if hostKey == "" {
		key = "kubeconfig:" + kubeKey
		name = filepath.Base(kubeconfig)
		if context != "" {
			name += " · " + context
		}
	}
	digest := sha256.Sum256([]byte(key))
	id := "external-" + hex.EncodeToString(digest[:12])
	record := clusterWorkspaceRecord{ID: id, Name: name, URL: base, Kubeconfig: kubeconfig, Context: clusterWorkspaceKubeconfigContext(kubeconfig, context), Role: "external", Archived: true}
	p.clusterWorkspacesMu.Lock()
	defer p.clusterWorkspacesMu.Unlock()
	if old, exists := p.clusterWorkspaces[id]; exists {
		return old.ID, nil
	}
	p.clusterWorkspaces[id] = record
	if err := p.persistClusterWorkspacesLocked(); err != nil {
		delete(p.clusterWorkspaces, id)
		return "", err
	}
	return id, nil
}

func (p *localControlPanel) renameClusterWorkspace(id, nickname string) error {
	if _, err := p.labClusterCandidates(); err != nil {
		return err
	}
	nickname = strings.TrimSpace(nickname)
	if !validClusterWorkspaceText(nickname, 80, true) {
		return errors.New("nickname must be at most 80 characters without control characters")
	}
	p.clusterWorkspacesMu.Lock()
	defer p.clusterWorkspacesMu.Unlock()
	record, exists := p.clusterWorkspaces[id]
	if !exists {
		return errors.New("cluster workspace not found")
	}
	old := record
	record.Nickname = nickname
	p.clusterWorkspaces[id] = record
	if err := p.persistClusterWorkspacesLocked(); err != nil {
		p.clusterWorkspaces[id] = old
		return err
	}
	return nil
}

func (p *localControlPanel) handleClusterWorkspaces(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", 403)
		return
	}
	if r.Method == http.MethodPost {
		var req struct {
			Action   string `json:"action"`
			ID       string `json:"id"`
			Nickname string `json:"nickname"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil {
			http.Error(w, "invalid cluster workspace request", 400)
			return
		}
		if req.Action != "rename" {
			http.Error(w, "unknown cluster workspace action", 400)
			return
		}
		if err := p.renameClusterWorkspace(req.ID, req.Nickname); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
	} else if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", 405)
		return
	}
	data, err := p.labClusterRecords()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	clusters, err := p.labClusterCandidates()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	result := make([]clusterWorkspaceResponse, 0, len(clusters))
	for _, cluster := range clusters {
		cluster.Kubeconfig = ""
		cluster.Context = "" // Connection paths are supplied separately by the target picker.
		row := clusterWorkspaceResponse{clusterWorkspaceRecord: cluster, clusterLabData: data[cluster.ID]}
		if row.TestRuns == nil {
			row.TestRuns = []clusterLabTestRun{}
		}
		if row.TestPlans == nil {
			row.TestPlans = []clusterLabTestPlan{}
		}
		if row.CacheWorkspaces == nil {
			row.CacheWorkspaces = []clusterLabCacheWorkspace{}
		}
		if row.Snapshots == nil {
			row.Snapshots = []clusterLabSnapshot{}
		}
		result = append(result, row)
	}
	writeJSON(w, map[string]any{"clusters": result})
}

// Optional cleanup is frozen against exact cluster IDs before infrastructure
// destruction. The default deliberately preserves all lab history.
type panelLabCleanupOptions struct {
	TestLab    bool
	CacheLab   bool
	ClusterIDs []string
}

func (p *localControlPanel) freezeRunLabCleanup(runID string, tests, cache bool) (panelLabCleanupOptions, error) {
	options := panelLabCleanupOptions{TestLab: tests, CacheLab: cache}
	// Metadata is worth preserving even when no local data deletion was requested.
	candidates, err := p.labClusterCandidates()
	if err != nil {
		return options, err
	}
	if tests || cache {
		if _, err = p.labClusterRecords(); err != nil {
			return options, fmt.Errorf("cannot review linked lab data: %w", err)
		}
	}
	for _, candidate := range candidates {
		if candidate.RunID == runID {
			options.ClusterIDs = append(options.ClusterIDs, candidate.ID)
		}
	}
	return options, nil
}

func (p *localControlPanel) finishRunLabCleanup(runID string, options panelLabCleanupOptions, operation panelOperationName, canceled bool) {
	// Discovery may still hold the last healthy snapshot after a successful destroy.
	// Retain its metadata in the registry, but stop treating those rows as live.
	p.mu.Lock()
	for id, cluster := range p.clusterSnapshot {
		if cluster.RunID == runID {
			delete(p.clusterSnapshot, id)
		}
	}
	p.mu.Unlock()
	if !options.TestLab && !options.CacheLab {
		return
	}
	warnings := []string{}
	if canceled {
		warnings = append(warnings, "Local lab data was retained because cleanup was canceled.")
	} else {
		warnings = p.cleanupLabClusters(options.ClusterIDs, options.TestLab, options.CacheLab)
	}
	if len(warnings) == 0 {
		p.appendOperationOutput(operation, "[control-panel] Requested local lab cleanup completed for run "+runID+". Saved plans and config templates were retained.")
		return
	}
	p.mu.Lock()
	op := p.operationLocked(operation)
	for _, warning := range warnings {
		text := "Infrastructure was destroyed for run " + runID + ". " + warning
		op.Warning = appendPanelWarning(op.Warning, text)
		op.Output = appendBatchOutput(op.Output, "[control-panel] "+text)
	}
	p.persistOperationsLocked()
	p.mu.Unlock()
}

// Local lab records are decoded without probing processes or Kubernetes. This
// keeps Steve SQL imports linkable without turning metadata polling into work.
func (p *localControlPanel) localLabClusterWorkspaceViews() []clusterView {
	result := []clusterView{}
	for _, source := range []struct{ role, dir string }{{"steve", p.steveLabRunsDir()}, {"k3d", p.k3dLabRunsDir()}} {
		entries, err := os.ReadDir(source.dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(source.dir, entry.Name()))
			if err != nil || len(raw) > 1<<20 {
				continue
			}
			var record struct {
				RunID       string `json:"runId"`
				Status      string `json:"status"`
				ClusterName string `json:"clusterName"`
				Kubeconfig  string `json:"kubeconfig"`
				K3SVersion  string `json:"k3sVersion"`
				SteveRef    string `json:"steveRef"`
			}
			if json.Unmarshal(raw, &record) != nil || record.RunID == "" || record.Status == "deleted" || record.Status == "cleaned" {
				continue
			}
			version := record.K3SVersion
			if source.role == "steve" {
				version = record.SteveRef
			}
			result = append(result, clusterView{ID: localLabClusterWorkspaceID(source.role, record.RunID), Name: record.ClusterName, RunID: record.RunID, Version: version, Role: source.role, KubeconfigPath: record.Kubeconfig})
		}
	}
	return result
}

func localLabClusterWorkspaceID(role, runID string) string { return "local-" + role + "-" + runID }
