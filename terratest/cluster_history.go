package test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/brudnak/ha-rancher-rke2/internal/history"
)

type clusterHistoryEvent = history.Event

// The default is resolved from the workspace on each call, just as before.
// Tests can supply a store before workers start; it must not change during use.
func (p *localControlPanel) clusterHistoryStore() history.Store {
	if p.historyStore != nil {
		return p.historyStore
	}
	return history.FileStore{Root: durableDataPath("cluster-history")}
}

func (p *localControlPanel) clusterHistoryDir(id string) string {
	return (history.FileStore{Root: durableDataPath("cluster-history")}).Dir(id)
}
func (p *localControlPanel) saveClusterHistory(id, kind string, value any) error {
	if id == "" {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	key := id + ":" + kind
	p.historyMu.Lock()
	defer p.historyMu.Unlock()
	p.clusterWorkspacesMu.Lock()
	err = p.loadClusterWorkspacesLocked()
	purged := p.clusterWorkspaces[id].Purged
	p.clusterWorkspacesMu.Unlock()
	if err != nil {
		return err
	}
	if purged {
		return nil
	}
	if p.historyHashes == nil {
		p.historyHashes = map[string]string{}
	}
	if p.historyHashes[key] == hash {
		return nil
	}
	event := clusterHistoryEvent{ID: operationID(), At: time.Now().UTC(), Kind: kind, Data: raw}
	err = p.clusterHistoryStore().Write(id, event)
	if err == nil {
		p.historyHashes[key] = hash
	}
	return err
}

func (p *localControlPanel) recordClusterHistory(id, kind string, value any) {
	if err := p.saveClusterHistory(id, kind, value); err != nil {
		log.Printf("[control-panel] Cluster history could not be saved: %v", err)
	}
}
func (p *localControlPanel) retainDiscoveredClusters(clusters []clusterView) {
	for _, cluster := range clusters {
		if p.workers.Context().Err() != nil {
			return
		}
		saved := cloneClusterDeploymentView(cluster)
		saved.KubeconfigPath = ""
		saved.DownloadName = ""
		saved.Error = ""
		saved.ProvisioningMessage = ""
		for i := range saved.Pods {
			saved.Pods[i].Age = ""
		}
		sort.Slice(saved.Pods, func(i, j int) bool {
			return saved.Pods[i].Namespace+saved.Pods[i].Name < saved.Pods[j].Namespace+saved.Pods[j].Name
		})
		p.recordClusterHistory(cluster.ID, "discovery", saved)
		if command, err := p.helmCommandForCluster(cluster); err == nil {
			p.recordClusterHistory(cluster.ID, "helm-install", map[string]string{"command": sanitizeIssuePackageHelmCommand(command), "source": "Saved install.sh; credentials, hostnames and local paths redacted"})
		}
		if !cluster.Reachable || cluster.KubeconfigPath == "" {
			continue
		}
		p.historyMu.Lock()
		if p.historyProbes == nil {
			p.historyProbes = map[string]time.Time{}
		}
		due := time.Since(p.historyProbes[cluster.ID]) >= 5*time.Minute
		if due {
			p.historyProbes[cluster.ID] = time.Now()
		}
		p.historyMu.Unlock()
		if due {
			ctx, cancel := context.WithTimeout(p.workers.Context(), clusterDeploymentProbeTimeout)
			details := p.collectClusterDeploymentDetails(ctx, cluster)
			cancel()
			p.retainDeploymentDetails(details)
			p.retainHelmHistory(cluster)
		}
	}
}
func (p *localControlPanel) retainDeploymentDetails(details clusterDeploymentDetailsResponse) {
	// Persist typed facts only. Probe error text may contain credentials or paths.
	warnings := []string{}
	if len(details.Warnings) > 0 {
		warnings = append(warnings, fmt.Sprintf("%d probes reported unavailable or partial data; empty values are unknown, not removed", len(details.Warnings)))
	}
	p.recordClusterHistory(details.ClusterID, "deployment", struct {
		ConfiguredVersion   string                   `json:"configuredVersion"`
		RancherVersion      string                   `json:"rancherVersion,omitempty"`
		KubernetesVersion   string                   `json:"kubernetesVersion,omitempty"`
		WebhookChartVersion string                   `json:"webhookChartVersion,omitempty"`
		Images              []clusterDeploymentImage `json:"images"`
		Warnings            []string                 `json:"warnings"`
	}{details.ConfiguredVersion, details.RancherVersion, details.KubernetesVersion, details.WebhookChartVersion, details.Images, warnings})
}

func (p *localControlPanel) readClusterHistory(id string) ([]clusterHistoryEvent, error) {
	return p.clusterHistoryStore().List(id)
}
func (p *localControlPanel) handleClusterHistory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", 403)
		return
	}
	if r.Method != "GET" {
		http.Error(w, "method not allowed", 405)
		return
	}
	id := r.URL.Query().Get("cluster")
	// Snapshot worker state before taking historyMu to preserve the purge lock order.
	p.rancherOps.mu.Lock()
	activeOperation := p.rancherOps.active[id]
	p.rancherOps.mu.Unlock()
	p.historyMu.Lock()
	defer p.historyMu.Unlock()
	p.clusterWorkspacesMu.Lock()
	err := p.loadClusterWorkspacesLocked()
	record, exists := p.clusterWorkspaces[id]
	p.clusterWorkspacesMu.Unlock()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if !exists || record.Purged {
		http.Error(w, "history not found", 404)
		return
	}
	events, err := p.readClusterHistory(id)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	operations := []rancherOperationRecord{}
	entries, err := os.ReadDir(p.rancherOperationsDir())
	if err != nil && !os.IsNotExist(err) {
		http.Error(w, err.Error(), 500)
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(p.rancherOperationsDir(), entry.Name()))
		if err != nil {
			continue
		}
		var op rancherOperationRecord
		if json.Unmarshal(raw, &op) == nil && op.ClusterID == id {
			if op.Status == "running" && activeOperation != op.ID {
				op.Status = "interrupted"
			}
			operations = append(operations, op)
		}
	}
	sort.Slice(operations, func(i, j int) bool { return operations[i].Started.Before(operations[j].Started) })
	record.Kubeconfig = ""
	record.Context = ""
	writeJSON(w, map[string]any{"schemaVersion": 1, "cluster": record, "events": events, "operations": operations})
}
func (p *localControlPanel) purgeClusterHistory(in clusterHistoryRequest) error {
	if in.ConfirmID != in.ID || in.ConfirmPhrase != "DELETE PERMANENTLY" {
		return fmt.Errorf("type the exact cluster ID and DELETE PERMANENTLY to confirm")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.anyOperationRunningLocked() {
		return fmt.Errorf("wait for active operations to finish")
	}
	p.rancherOps.mu.Lock()
	defer p.rancherOps.mu.Unlock()
	if len(p.rancherOps.active) > 0 {
		return fmt.Errorf("wait for Rancher operations to finish")
	}
	p.historyMu.Lock()
	defer p.historyMu.Unlock()
	p.clusterWorkspacesMu.Lock()
	defer p.clusterWorkspacesMu.Unlock()
	if err := p.loadClusterWorkspacesLocked(); err != nil {
		return err
	}
	record, exists := p.clusterWorkspaces[in.ID]
	if !exists || !record.Deleted {
		return fmt.Errorf("move this history to Trash first")
	}
	if _, live := p.clusterSnapshot[in.ID]; live {
		return fmt.Errorf("cannot purge a discovered cluster")
	}
	// Store a minimal suppression marker before removing files, so a stale probe
	// cannot recreate evidence. A failed removal can be retried from Trash.
	old := record
	record.Purged = true
	p.clusterWorkspaces[in.ID] = record
	if err := p.persistClusterWorkspacesLocked(); err != nil {
		p.clusterWorkspaces[in.ID] = old
		return err
	}
	fail := func(err error) error {
		p.clusterWorkspaces[in.ID] = old
		_ = p.persistClusterWorkspacesLocked()
		return err
	}
	if err := p.clusterHistoryStore().Delete(in.ID); err != nil {
		return fail(err)
	}
	entries, err := os.ReadDir(p.rancherOperationsDir())
	if err != nil && !os.IsNotExist(err) {
		return fail(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(p.rancherOperationsDir(), entry.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			return fail(err)
		}
		var op rancherOperationRecord
		if json.Unmarshal(raw, &op) == nil && op.ClusterID == in.ID {
			if err = os.Remove(path); err != nil {
				return fail(err)
			}
		}
	}
	p.clusterWorkspaces[in.ID] = clusterWorkspaceRecord{ID: in.ID, Deleted: true, Purged: true, Archived: true}
	if err := p.persistClusterWorkspacesLocked(); err != nil {
		return fail(err)
	}
	return nil
}

// Helm's history contains chart/app versions and revision/status without values
// or Kubernetes Secrets. Do not retain free-form descriptions from hook errors.
func (p *localControlPanel) retainHelmHistory(cluster clusterView) {
	if !isKubeconfigBackedManagementCluster(cluster) {
		return
	}
	for _, release := range []string{"rancher", "rancher-webhook"} {
		ctx, cancel := context.WithTimeout(p.workers.Context(), 12*time.Second)
		raw, err := operationCommand(ctx, nil, "helm", "--kubeconfig", cluster.KubeconfigPath, "history", release, "-n", "cattle-system", "--max", "1000", "-o", "json")
		cancel()
		if err != nil {
			p.recordClusterHistory(cluster.ID, "helm-history-warning-"+release, map[string]string{"warning": "Helm revision history unavailable for " + release})
			continue
		}
		var revisions []struct {
			Revision   int    `json:"revision"`
			Updated    string `json:"updated"`
			Status     string `json:"status"`
			Chart      string `json:"chart"`
			AppVersion string `json:"app_version"`
		}
		if json.Unmarshal(raw, &revisions) == nil {
			p.recordClusterHistory(cluster.ID, "helm-history-"+release, revisions)
		}
	}
}
