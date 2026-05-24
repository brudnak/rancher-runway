package test

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const steveRepoURL = "https://github.com/rancher/steve.git"

type steveLabPanelState struct {
	Preflight   systemReadinessState   `json:"preflight"`
	Operation   panelOperationSnapshot `json:"operation"`
	Runs        []steveLabRunRecord    `json:"runs"`
	K3SVersions []string               `json:"k3sVersions"`
}

type steveLabRunRecord struct {
	RunID       string    `json:"runId"`
	Status      string    `json:"status"`
	SteveRef    string    `json:"steveRef"`
	SteveCommit string    `json:"steveCommit,omitempty"`
	K3SVersion  string    `json:"k3sVersion"`
	ClusterName string    `json:"clusterName"`
	Kubeconfig  string    `json:"kubeconfig"`
	RunDir      string    `json:"runDir"`
	SourceDir   string    `json:"sourceDir"`
	HTTPPort    int       `json:"httpPort,omitempty"`
	HTTPSPort   int       `json:"httpsPort,omitempty"`
	HTTPURL     string    `json:"httpUrl,omitempty"`
	HTTPSURL    string    `json:"httpsUrl,omitempty"`
	StevePID    int       `json:"stevePid,omitempty"`
	LogPath     string    `json:"logPath,omitempty"`
	KeepCluster bool      `json:"keepCluster"`
	SQLCache    bool      `json:"sqlCache"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	Error       string    `json:"error,omitempty"`
}

type steveVersionRef struct {
	Name   string `json:"name"`
	Commit string `json:"commit"`
	Kind   string `json:"kind"`
}

type steveVersionDiscovery struct {
	RepoURL string            `json:"repoUrl"`
	Tags    []steveVersionRef `json:"tags"`
	Error   string            `json:"error,omitempty"`
}

type steveRefDetails struct {
	Ref                     string   `json:"ref"`
	KubernetesModule        string   `json:"kubernetesModule,omitempty"`
	KubernetesModuleVersion string   `json:"kubernetesModuleVersion,omitempty"`
	RecommendedMinor        string   `json:"recommendedMinor,omitempty"`
	RecommendedK3SVersions  []string `json:"recommendedK3sVersions"`
	Error                   string   `json:"error,omitempty"`
}

type steveLabStartRequest struct {
	SteveRef       string `json:"steveRef"`
	K3SVersion     string `json:"k3sVersion"`
	KeepCluster    bool   `json:"keepCluster"`
	HTTPPort       int    `json:"httpPort"`
	HTTPSPort      int    `json:"httpsPort"`
	HeaderAuth     bool   `json:"headerAuth"`
	EnableSQLCache bool   `json:"enableSqlCache"`
	Replace        bool   `json:"replace"`
}

func (p *localControlPanel) handleSteveLabState(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedReadOnly(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, p.steveLabPanelState(true))
}

func (p *localControlPanel) handleSteveLabVersions(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedReadOnly(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, discoverSteveVersions())
}

func (p *localControlPanel) handleSteveLabRef(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedReadOnly(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ref := strings.TrimSpace(r.URL.Query().Get("ref"))
	if ref == "" {
		http.Error(w, "ref is required", http.StatusBadRequest)
		return
	}
	writeJSON(w, inspectSteveRefDetails(ref))
}

func (p *localControlPanel) handleSteveLabStart(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req steveLabStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := p.startSteveLab(req); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, map[string]string{"status": "steve lab started"})
}

func (p *localControlPanel) handleSteveLabStop(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		RunID string `json:"runId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	record, err := p.stopSteveLabEndpoint(req.RunID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, map[string]any{"status": "stopped", "run": record})
}

func (p *localControlPanel) handleSteveLabCleanup(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		RunID     string `json:"runId"`
		DeleteDir bool   `json:"deleteDir"`
		DeleteK3D bool   `json:"deleteK3d"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	runID := safeRunPathSegment(req.RunID)
	if runID == "" {
		http.Error(w, "runId is required", http.StatusBadRequest)
		return
	}
	if p.snapshotOperation(panelOperationSteveLab).Running {
		http.Error(w, "Steve Lab is running; stop it before cleanup", http.StatusConflict)
		return
	}
	record, ok := p.readSteveLabRunRecord(runID)
	if !ok {
		http.Error(w, "Steve Lab run not found", http.StatusNotFound)
		return
	}
	if record.StevePID > 0 {
		stopped, err := p.stopSteveLabEndpoint(record.RunID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		record = stopped
	}
	var removed []string
	if req.DeleteK3D && strings.TrimSpace(record.ClusterName) != "" {
		k3dPath, err := resolveLocalToolPath("k3d")
		if err != nil {
			http.Error(w, "k3d was not found", http.StatusBadGateway)
			return
		}
		cmd := exec.Command(k3dPath, "cluster", "delete", record.ClusterName)
		cmd.Env = localToolEnv(nil)
		if output, err := cmd.CombinedOutput(); err != nil {
			trimmed := strings.TrimSpace(string(output))
			if !k3dDeleteMissingClusterOK(trimmed) {
				http.Error(w, fmt.Sprintf("failed to delete k3d cluster: %s", trimmed), http.StatusBadGateway)
				return
			}
		}
		removed = append(removed, record.ClusterName)
	}
	if req.DeleteDir && strings.TrimSpace(record.RunDir) != "" {
		if err := os.RemoveAll(record.RunDir); err != nil {
			http.Error(w, fmt.Sprintf("failed to remove run directory: %v", err), http.StatusInternalServerError)
			return
		}
		removed = append(removed, record.RunDir)
	}
	record.Status = "cleaned"
	record.UpdatedAt = time.Now()
	record.Error = ""
	if req.DeleteDir {
		_ = p.deleteSteveLabRunRecord(record.RunID)
	} else {
		_ = p.writeSteveLabRunRecord(record)
	}
	writeJSON(w, map[string]any{"status": "cleaned", "removed": removed})
}

func (p *localControlPanel) handleSteveLabKubeconfigSave(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		RunID string `json:"runId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	runID := safeRunPathSegment(req.RunID)
	if runID == "" {
		http.Error(w, "runId is required", http.StatusBadRequest)
		return
	}
	record, ok := p.readSteveLabRunRecord(runID)
	if !ok {
		http.Error(w, "Steve Lab run not found", http.StatusNotFound)
		return
	}
	content, filename, err := localLabKubeconfigContent(record.Kubeconfig, steveLabKubeconfigDownloadName(record))
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

func (p *localControlPanel) handleSteveLabSqliteVacuum(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		RunID string `json:"runId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	runID := safeRunPathSegment(req.RunID)
	if runID == "" {
		http.Error(w, "runId is required", http.StatusBadRequest)
		return
	}
	record, ok := p.readSteveLabRunRecord(runID)
	if !ok {
		http.Error(w, "Steve Lab run not found", http.StatusNotFound)
		return
	}

	sourceDBPath := filepath.Join(record.SourceDir, "informer_object_cache.db")
	if _, err := os.Stat(sourceDBPath); os.IsNotExist(err) {
		http.Error(w, "SQLite database file not found yet. Make sure Steve has started and initialized the cache.", http.StatusNotFound)
		return
	}

	home, err := os.UserHomeDir()
	if err != nil {
		http.Error(w, "failed to find home directory: "+err.Error(), http.StatusInternalServerError)
		return
	}
	downloadsDir := filepath.Join(home, "Downloads")
	if err := os.MkdirAll(downloadsDir, 0o755); err != nil {
		http.Error(w, "failed to create Downloads directory: "+err.Error(), http.StatusInternalServerError)
		return
	}

	filename := fmt.Sprintf("steve-cache-%s.db", record.RunID)
	targetDBPath := uniqueDownloadPath(downloadsDir, filename)

	db, err := sql.Open("sqlite", sourceDBPath)
	if err != nil {
		http.Error(w, "failed to open source database: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer db.Close()

	escapedPath := strings.ReplaceAll(targetDBPath, "'", "''")
	query := fmt.Sprintf("VACUUM INTO '%s'", escapedPath)
	if _, err := db.Exec(query); err != nil {
		http.Error(w, "failed to vacuum SQLite database: "+err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]string{
		"filename": filepath.Base(targetDBPath),
		"path":     targetDBPath,
	})
}

func (p *localControlPanel) handleSteveLabOutputClear(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	p.mu.Lock()
	op := p.operationLocked(panelOperationSteveLab)
	op.Output = nil
	now := time.Now()
	op.UpdatedAt = &now
	p.persistOperationsLocked()
	p.mu.Unlock()
	writeJSON(w, map[string]string{"status": "cleared"})
}

func (p *localControlPanel) handleSteveLabLogs(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedReadOnly(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	runID := safeRunPathSegment(r.URL.Query().Get("runId"))
	if runID == "" {
		http.Error(w, "runId is required", http.StatusBadRequest)
		return
	}
	record, ok := p.readSteveLabRunRecord(runID)
	if !ok {
		http.Error(w, "Steve Lab run not found", http.StatusNotFound)
		return
	}

	content, err := os.ReadFile(record.LogPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, map[string]string{"text": "(no Steve log file found yet)"})
			return
		}
		http.Error(w, "failed to read Steve log: "+err.Error(), http.StatusInternalServerError)
		return
	}

	outputText := string(content)
	if len(outputText) > 1000000 {
		outputText = outputText[len(outputText)-1000000:]
	}

	writeJSON(w, map[string]string{"text": outputText})
}

func (p *localControlPanel) steveLabPanelState(includePreflight bool) steveLabPanelState {
	preflight := systemReadinessState{Ready: false, Summary: "Open Steve Lab to check local Docker and k3d tools."}
	if includePreflight {
		preflight = collectSteveLabPreflight()
	}
	return steveLabPanelState{
		Preflight:   preflight,
		Operation:   p.snapshotOperation(panelOperationSteveLab),
		Runs:        p.listSteveLabRunRecords(),
		K3SVersions: defaultK3SVersions(),
	}
}

func collectSteveLabPreflight() systemReadinessState {
	tools := []systemReadinessToolConfig{
		{Name: "Go", Command: "go", Args: []string{"version"}, VersionPattern: `go([0-9]+\.[0-9]+(?:\.[0-9]+)?)`, MinimumVersion: "1.26.1", RecommendedVersion: "1.26.1"},
		{Name: "git", Command: "git", Args: []string{"--version"}, VersionPattern: `git version ([0-9]+\.[0-9]+(?:\.[0-9]+)?)`, MinimumVersion: "2.39.0"},
		{Name: "Docker", Command: "docker", Args: []string{"version", "--format", "{{.Server.Version}}"}, VersionPattern: `([0-9]+\.[0-9]+(?:\.[0-9]+)?)`, MinimumVersion: "24.0.0"},
		{Name: "k3d", Command: "k3d", Args: []string{"version"}, VersionPattern: `k3d version v?([0-9]+\.[0-9]+(?:\.[0-9]+)?)`, MinimumVersion: "5.6.0", RecommendedVersion: "5.8.3"},
		{Name: "kubectl", Command: "kubectl", Args: []string{"version", "--client=true"}, VersionPattern: `Client Version: v?([0-9]+\.[0-9]+(?:\.[0-9]+)?)`, MinimumVersion: "1.30.0"},
	}
	items := make([]systemReadinessItem, 0, len(tools))
	for _, tool := range tools {
		items = append(items, checkSystemReadinessTool(tool))
	}
	ready := true
	warnings := 0
	for _, item := range items {
		if item.Status == "error" {
			ready = false
		}
		if item.Status == "warning" {
			warnings++
		}
	}
	summary := "Ready to run Steve locally"
	if !ready {
		summary = "Steve Lab needs local tools before it can run"
	} else if warnings > 0 {
		summary = fmt.Sprintf("Ready with %d warning(s)", warnings)
	}
	return systemReadinessState{Ready: ready, Summary: summary, Items: items}
}

func discoverSteveVersions() steveVersionDiscovery {
	gitPath, err := resolveLocalToolPath("git")
	if err != nil {
		return steveVersionDiscovery{RepoURL: steveRepoURL, Error: err.Error()}
	}
	cmd := exec.Command(gitPath, "ls-remote", "--tags", "--refs", steveRepoURL)
	cmd.Env = localToolEnv(nil)
	output, err := cmd.Output()
	if err != nil {
		return steveVersionDiscovery{RepoURL: steveRepoURL, Error: err.Error()}
	}
	seen := map[string]bool{}
	var refs []steveVersionRef
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "refs/tags/")
		if !regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+`).MatchString(name) || seen[name] {
			continue
		}
		seen[name] = true
		refs = append(refs, steveVersionRef{Name: name, Commit: fields[0], Kind: "tag"})
	}
	sort.Slice(refs, func(i, j int) bool {
		return compareVersionStrings(strings.TrimPrefix(refs[i].Name, "v"), strings.TrimPrefix(refs[j].Name, "v")) > 0
	})
	if len(refs) > 80 {
		refs = refs[:80]
	}
	return steveVersionDiscovery{RepoURL: steveRepoURL, Tags: refs}
}

func inspectSteveRefDetails(ref string) steveRefDetails {
	ref = strings.TrimSpace(ref)
	details := steveRefDetails{Ref: ref, RecommendedK3SVersions: defaultK3SVersions()}
	if !validSteveRef(ref) {
		details.Error = "Ref can contain letters, numbers, dots, dashes, underscores, slashes, plus signs, and @ only."
		return details
	}
	client := http.Client{Timeout: 8 * time.Second}
	url := fmt.Sprintf("https://raw.githubusercontent.com/rancher/steve/%s/go.mod", ref)
	resp, err := client.Get(url)
	if err != nil {
		details.Error = err.Error()
		return details
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		details.Error = fmt.Sprintf("go.mod lookup returned %s", resp.Status)
		return details
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		details.Error = err.Error()
		return details
	}
	module, version := findSteveKubernetesModule(string(data))
	details.KubernetesModule = module
	details.KubernetesModuleVersion = version
	if minor := kubernetesMinorFromModule(version); minor != "" {
		details.RecommendedMinor = "1." + minor
		details.RecommendedK3SVersions = k3sVersionsForMinor(minor)
	}
	return details
}

func findSteveKubernetesModule(goMod string) (string, string) {
	for _, line := range strings.Split(goMod, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) >= 2 && (fields[0] == "k8s.io/client-go" || fields[0] == "k8s.io/apimachinery") {
			return fields[0], fields[1]
		}
	}
	return "", ""
}

func kubernetesMinorFromModule(version string) string {
	matches := regexp.MustCompile(`v0\.([0-9]+)\.`).FindStringSubmatch(version)
	if len(matches) != 2 {
		return ""
	}
	return matches[1]
}

func defaultK3SVersions() []string {
	return []string{
		"v1.33.5-k3s1",
		"v1.32.9-k3s1",
		"v1.31.13-k3s1",
		"v1.30.14-k3s1",
		"v1.29.15-k3s1",
	}
}

func k3sVersionsForMinor(minor string) []string {
	for _, version := range defaultK3SVersions() {
		if strings.HasPrefix(version, "v1."+minor+".") {
			return append([]string{version}, removeString(defaultK3SVersions(), version)...)
		}
	}
	return defaultK3SVersions()
}

func removeString(values []string, remove string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != remove {
			out = append(out, value)
		}
	}
	return out
}

func validSteveRef(ref string) bool {
	return regexp.MustCompile(`^[A-Za-z0-9._/@+-]+$`).MatchString(strings.TrimSpace(ref))
}

func normalizeSteveLabK3SVersion(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "rancher/k3s:")
	return value
}

func k3sImage(value string) string {
	value = normalizeSteveLabK3SVersion(value)
	if value == "" {
		value = defaultK3SVersions()[0]
	}
	return "rancher/k3s:" + value
}

func (p *localControlPanel) startSteveLab(req steveLabStartRequest) error {
	preflight := collectSteveLabPreflight()
	if !preflight.Ready {
		return fmt.Errorf("Steve Lab preflight blocked run: %s", preflight.Summary)
	}
	ref := strings.TrimSpace(req.SteveRef)
	if ref == "" {
		return fmt.Errorf("Steve ref is required")
	}
	if !validSteveRef(ref) {
		return fmt.Errorf("Steve ref contains unsupported characters")
	}
	k3sVersion := normalizeSteveLabK3SVersion(req.K3SVersion)
	if k3sVersion == "" {
		k3sVersion = defaultK3SVersions()[0]
	}

	if active := p.activeSteveLabRunRecords(); len(active) > 0 {
		if !req.Replace {
			return fmt.Errorf("Steve Lab already has an active endpoint; stop it or replace it before launching another")
		}
		if err := p.cleanupActiveSteveLabRuns(active); err != nil {
			return err
		}
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conflictingOperationRunningLocked(panelOperationSteveLab) {
		return fmt.Errorf("Steve Lab is already running")
	}
	token, err := randomConfirmationToken()
	if err != nil {
		return fmt.Errorf("failed to create Steve Lab run id: %w", err)
	}
	runID := "steve-" + token[:8]
	now := time.Now()
	runDir := filepath.Join(p.steveLabRunsDir(), runID)
	httpsPort := req.HTTPSPort
	if httpsPort <= 0 {
		var err error
		httpsPort, err = p.allocateLocalLabPort()
		if err != nil {
			return fmt.Errorf("failed to allocate HTTPS port: %w", err)
		}
	} else if err := p.ensureLocalLabPortAvailable(httpsPort); err != nil {
		return err
	}
	record := steveLabRunRecord{
		RunID:       runID,
		Status:      "running",
		SteveRef:    ref,
		K3SVersion:  k3sVersion,
		ClusterName: "rancher-runway-" + runID,
		Kubeconfig:  filepath.Join(runDir, "kubeconfig.yaml"),
		RunDir:      runDir,
		SourceDir:   filepath.Join(runDir, "steve"),
		HTTPSPort:   httpsPort,
		HTTPSURL:    fmt.Sprintf("https://127.0.0.1:%d", httpsPort),
		LogPath:     filepath.Join(runDir, "steve.log"),
		KeepCluster: true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	op := p.operationLocked(panelOperationSteveLab)
	op.Running = true
	op.PID = 0
	op.StartedAt = &now
	op.FinishedAt = nil
	op.Error = ""
	op.RunID = runID
	op.Command = fmt.Sprintf("Steve %s on k3d %s", ref, k3sVersion)
	op.UpdatedAt = &now
	op.Output = []string{
		fmt.Sprintf("[steve-lab] Run %s", runID),
		fmt.Sprintf("[steve-lab] Steve ref: %s", ref),
		fmt.Sprintf("[steve-lab] k3d image: %s", k3sImage(k3sVersion)),
	}
	p.persistOperationsLocked()
	if err := p.writeSteveLabRunRecord(record); err != nil {
		op.Running = false
		op.Error = err.Error()
		p.persistOperationsLocked()
		return err
	}
	go p.runSteveLab(record)
	return nil
}

func (p *localControlPanel) runSteveLab(record steveLabRunRecord) {
	err := p.runSteveLabSteps(&record)
	if err != nil {
		record.Status = "failed"
		record.Error = err.Error()
		record.UpdatedAt = time.Now()
		_ = p.writeSteveLabRunRecord(record)
		p.finishSteveLabOperation(err)
		return
	}
	record.Status = "serving"
	record.Error = ""
	record.UpdatedAt = time.Now()
	_ = p.writeSteveLabRunRecord(record)
	p.finishSteveLabOperation(nil)
}

func (p *localControlPanel) runSteveLabSteps(record *steveLabRunRecord) error {
	if err := os.MkdirAll(record.RunDir, 0o755); err != nil {
		return err
	}
	if err := p.downloadSteveSourceArchive(record); err != nil {
		return err
	}
	record.SteveCommit = record.SteveRef
	if commit, err := resolveSteveGitHubCommit(record.SteveRef); err == nil && commit != "" {
		record.SteveCommit = commit
	}

	// Check if the downloaded Steve version supports SQL Cache
	hasSQLCache := false
	if _, err := os.Stat(filepath.Join(record.SourceDir, "pkg", "sqlcache")); err == nil {
		hasSQLCache = true
	}
	record.SQLCache = hasSQLCache
	record.UpdatedAt = time.Now()
	_ = p.writeSteveLabRunRecord(*record)
	
	if hasSQLCache {
		p.appendOperationOutput(panelOperationSteveLab, "[steve-lab] Detected SQL cache support in Steve version")
	} else {
		p.appendOperationOutput(panelOperationSteveLab, "[steve-lab] Resolved Steve commit "+record.SteveCommit)
	}

	if err := p.runSteveLabCommand(record, record.RunDir, "k3d", "cluster", "create", record.ClusterName, "--image", k3sImage(record.K3SVersion), "--wait", "--timeout", "180s"); err != nil {
		return err
	}
	k3dPath, err := resolveLocalToolPath("k3d")
	if err != nil {
		return fmt.Errorf("k3d was not found: %w", err)
	}
	kubeconfigCmd := exec.Command(k3dPath, "kubeconfig", "get", record.ClusterName)
	kubeconfigCmd.Env = localToolEnv(nil)
	kubeconfig, err := kubeconfigCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to export k3d kubeconfig: %w", err)
	}
	if err := os.WriteFile(record.Kubeconfig, kubeconfig, 0o600); err != nil {
		return fmt.Errorf("failed to write kubeconfig: %w", err)
	}
	p.appendOperationOutput(panelOperationSteveLab, "[steve-lab] Wrote kubeconfig "+record.Kubeconfig)
	if err := p.runSteveLabCommand(record, record.RunDir, "kubectl", "--kubeconfig", record.Kubeconfig, "wait", "node", "--all", "--for=condition=Ready", "--timeout=180s"); err != nil {
		return err
	}

	if record.SQLCache {
		p.appendOperationOutput(panelOperationSteveLab, "[steve-lab] Applying Project CRD prerequisites for SQL cache...")
		if err := ensureSQLCachePrereqs(record.Kubeconfig); err != nil {
			p.appendOperationOutput(panelOperationSteveLab, "[steve-lab] Warning: failed to apply SQL cache prerequisites: "+err.Error())
		}
	}

	return p.startSteveEndpoint(record)
}

func (p *localControlPanel) downloadSteveSourceArchive(record *steveLabRunRecord) error {
	if err := os.RemoveAll(record.SourceDir); err != nil {
		return fmt.Errorf("failed to reset Steve source directory: %w", err)
	}
	if err := os.MkdirAll(record.SourceDir, 0o755); err != nil {
		return fmt.Errorf("failed to create Steve source directory: %w", err)
	}

	archiveURL := "https://api.github.com/repos/rancher/steve/tarball/" + url.PathEscape(record.SteveRef)
	p.appendOperationOutput(panelOperationSteveLab, "[steve-lab] Downloading Steve source archive for "+record.SteveRef)
	req, err := http.NewRequest(http.MethodGet, archiveURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "rancher-runway")
	client := &http.Client{Timeout: 3 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download Steve source archive: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download Steve source archive for %q: GitHub returned %s", record.SteveRef, resp.Status)
	}
	if err := extractGitHubTarball(resp.Body, record.SourceDir); err != nil {
		return err
	}
	return nil
}

func resolveSteveGitHubCommit(ref string) (string, error) {
	commitURL := "https://api.github.com/repos/rancher/steve/commits/" + url.PathEscape(ref)
	req, err := http.NewRequest(http.MethodGet, commitURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "rancher-runway")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub returned %s", resp.Status)
	}
	var payload struct {
		SHA string `json:"sha"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	return strings.TrimSpace(payload.SHA), nil
}

func extractGitHubTarball(reader io.Reader, destDir string) error {
	gzipReader, err := gzip.NewReader(reader)
	if err != nil {
		return fmt.Errorf("failed to read Steve source archive: %w", err)
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to unpack Steve source archive: %w", err)
		}
		target, ok := githubTarballTarget(destDir, header.Name)
		if !ok {
			continue
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(header.Mode) & 0o777
			if mode == 0 {
				mode = 0o644
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(file, tarReader)
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			_ = os.Remove(target)
			if err := os.Symlink(header.Linkname, target); err != nil {
				return err
			}
		}
	}
	return nil
}

func githubTarballTarget(destDir string, name string) (string, bool) {
	name = strings.TrimPrefix(filepath.ToSlash(name), "/")
	parts := strings.SplitN(name, "/", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[1]) == "" {
		return "", false
	}
	relative := filepath.Clean(filepath.FromSlash(parts[1]))
	if relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || relative == ".." {
		return "", false
	}
	target := filepath.Join(destDir, relative)
	cleanDest := filepath.Clean(destDir)
	cleanTarget := filepath.Clean(target)
	if cleanTarget != cleanDest && !strings.HasPrefix(cleanTarget, cleanDest+string(filepath.Separator)) {
		return "", false
	}
	return target, true
}

func (p *localControlPanel) runSteveLabCommand(record *steveLabRunRecord, dir string, name string, args ...string) error {
	return p.runSteveLabCommandWithEnv(record, dir, nil, name, args...)
}

func (p *localControlPanel) runSteveLabCommandWithEnv(record *steveLabRunRecord, dir string, env []string, name string, args ...string) error {
	p.appendOperationOutput(panelOperationSteveLab, "[steve-lab] $ "+name+" "+strings.Join(args, " "))
	path, err := resolveLocalToolPath(name)
	if err != nil {
		return err
	}
	cmd := exec.Command(path, args...)
	cmd.Dir = dir
	cmd.Env = localToolEnv(env)
	cmd.SysProcAttr = panelCommandSysProcAttr()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	p.setOperationPID(panelOperationSteveLab, cmd.Process.Pid)
	var wg sync.WaitGroup
	wg.Add(2)
	go p.capturePanelCommandStream(&wg, panelOperationSteveLab, stdout)
	go p.capturePanelCommandStream(&wg, panelOperationSteveLab, stderr)
	wg.Wait()
	record.UpdatedAt = time.Now()
	_ = p.writeSteveLabRunRecord(*record)
	return cmd.Wait()
}

func (p *localControlPanel) startSteveEndpoint(record *steveLabRunRecord) error {
	goPath, err := resolveLocalToolPath("go")
	if err != nil {
		return err
	}
	args := []string{
		"run", "main.go",
		"--kubeconfig", record.Kubeconfig,
		"--http-listen-port", fmt.Sprintf("%d", record.HTTPPort),
		"--https-listen-port", fmt.Sprintf("%d", record.HTTPSPort),
	}
	if record.SQLCache {
		args = append(args, "--sql-cache")
	}
	p.appendOperationOutput(panelOperationSteveLab, "[steve-lab] $ go "+strings.Join(args, " "))
	logFile, err := os.OpenFile(record.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("failed to open Steve log: %w", err)
	}
	cmd := exec.Command(goPath, args...)
	cmd.Dir = record.SourceDir
	cmd.Env = localToolEnv([]string{
		"CGO_ENABLED=0",
		"KUBECONFIG=" + record.Kubeconfig,
	})
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = panelCommandSysProcAttr()
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("failed to start Steve endpoint: %w", err)
	}
	record.StevePID = cmd.Process.Pid
	record.Status = "starting"
	record.UpdatedAt = time.Now()
	_ = p.writeSteveLabRunRecord(*record)
	p.appendOperationOutput(panelOperationSteveLab, fmt.Sprintf("[steve-lab] Steve endpoint started pid %d", cmd.Process.Pid))
	p.appendOperationOutput(panelOperationSteveLab, "[steve-lab] Logs "+record.LogPath)
	go p.watchSteveEndpointProcess(*record, cmd, logFile)
	if err := waitForLocalPort(record.HTTPSPort, cmd.Process.Pid, 2*time.Minute); err != nil {
		_ = interruptProcessTree(cmd.Process.Pid)
		return err
	}
	record.Status = "serving"
	record.UpdatedAt = time.Now()
	_ = p.writeSteveLabRunRecord(*record)
	p.appendOperationOutput(panelOperationSteveLab, "[steve-lab] HTTPS endpoint "+record.HTTPSURL)
	return nil
}

func (p *localControlPanel) watchSteveEndpointProcess(record steveLabRunRecord, cmd *exec.Cmd, logFile *os.File) {
	err := cmd.Wait()
	_ = logFile.Close()
	current, ok := p.readSteveLabRunRecord(record.RunID)
	if !ok {
		return
	}
	if current.StevePID != record.StevePID {
		return
	}
	current.StevePID = 0
	current.UpdatedAt = time.Now()
	if err != nil && current.Status != "stopped" && current.Status != "cleaned" {
		current.Status = "failed"
		current.Error = "Steve endpoint exited: " + err.Error()
	} else if current.Status == "serving" {
		current.Status = "stopped"
	}
	_ = p.writeSteveLabRunRecord(current)
}

func (p *localControlPanel) stopSteveLabEndpoint(runID string) (steveLabRunRecord, error) {
	runID = safeRunPathSegment(runID)
	if runID == "" {
		return steveLabRunRecord{}, fmt.Errorf("runId is required")
	}
	record, ok := p.readSteveLabRunRecord(runID)
	if !ok {
		return steveLabRunRecord{}, fmt.Errorf("Steve Lab run not found: %s", runID)
	}
	if record.StevePID <= 0 {
		record.Status = "stopped"
		record.UpdatedAt = time.Now()
		_ = p.writeSteveLabRunRecord(record)
		return record, nil
	}
	if processAlive(record.StevePID) {
		if err := interruptProcessTree(record.StevePID); err != nil {
			return record, err
		}
	}
	record.StevePID = 0
	record.Status = "stopped"
	record.UpdatedAt = time.Now()
	record.Error = ""
	if err := p.writeSteveLabRunRecord(record); err != nil {
		return record, err
	}
	return record, nil
}

func (p *localControlPanel) activeSteveLabRunRecords() []steveLabRunRecord {
	var active []steveLabRunRecord
	for _, record := range p.listSteveLabRunRecords() {
		if record.StevePID > 0 || record.Status == "running" || record.Status == "starting" || record.Status == "serving" {
			active = append(active, record)
		}
	}
	return active
}

func (p *localControlPanel) cleanupActiveSteveLabRuns(records []steveLabRunRecord) error {
	for _, record := range records {
		if record.StevePID > 0 && processAlive(record.StevePID) {
			if err := interruptProcessTree(record.StevePID); err != nil {
				return fmt.Errorf("failed to stop active Steve endpoint %s: %w", record.RunID, err)
			}
		}
		if strings.TrimSpace(record.ClusterName) != "" {
			if err := deleteK3DCluster(record.ClusterName); err != nil {
				return fmt.Errorf("failed to delete active Steve k3d cluster %s: %w", record.ClusterName, err)
			}
		}
		if strings.TrimSpace(record.RunDir) != "" {
			if err := os.RemoveAll(record.RunDir); err != nil {
				return fmt.Errorf("failed to remove active Steve run directory %s: %w", record.RunDir, err)
			}
		}
		if err := p.deleteSteveLabRunRecord(record.RunID); err != nil {
			return err
		}
	}
	return nil
}

func deleteK3DCluster(clusterName string) error {
	k3dPath, err := resolveLocalToolPath("k3d")
	if err != nil {
		return err
	}
	cmd := exec.Command(k3dPath, "cluster", "delete", clusterName)
	cmd.Env = localToolEnv(nil)
	output, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(output))
	if err != nil && !strings.Contains(trimmed, "No nodes found") && !strings.Contains(trimmed, "not found") {
		if trimmed != "" {
			return fmt.Errorf("%s", trimmed)
		}
		return err
	}
	return nil
}

func freeLocalPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("unexpected listener address %s", listener.Addr())
	}
	return addr.Port, nil
}

func waitForLocalPort(port int, pid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	address := fmt.Sprintf("127.0.0.1:%d", port)
	var lastErr error
	for time.Now().Before(deadline) {
		if pid > 0 && !processAlive(pid) {
			return fmt.Errorf("Steve process exited before %s opened", address)
		}
		conn, err := net.DialTimeout("tcp", address, 250*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		lastErr = err
		time.Sleep(1 * time.Second)
	}
	if lastErr != nil {
		return fmt.Errorf("timed out waiting for Steve endpoint %s: %w", address, lastErr)
	}
	return fmt.Errorf("timed out waiting for Steve endpoint %s", address)
}

func (p *localControlPanel) finishSteveLabOperation(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	op := p.operationLocked(panelOperationSteveLab)
	op.Running = false
	op.PID = 0
	finished := time.Now()
	op.FinishedAt = &finished
	op.UpdatedAt = &finished
	if err != nil {
		op.Error = err.Error()
		op.Output = append(op.Output, "[steve-lab] Finished with error: "+err.Error())
	} else {
		op.Error = ""
		op.Output = append(op.Output, "[steve-lab] Steve Lab completed successfully")
	}
	p.persistOperationsLocked()
}

func (p *localControlPanel) steveLabRunsDir() string {
	path := filepath.Join(automationOutputDir(), "control-panel", "steve-lab", "runs")
	if abs, err := absoluteFromWorkingDir(path); err == nil {
		return abs
	}
	return path
}

func (p *localControlPanel) steveLabRunRecordPath(runID string) string {
	return filepath.Join(p.steveLabRunsDir(), safeRunPathSegment(runID)+".json")
}

func (p *localControlPanel) writeSteveLabRunRecord(record steveLabRunRecord) error {
	if err := os.MkdirAll(p.steveLabRunsDir(), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p.steveLabRunRecordPath(record.RunID), data, 0o600)
}

func (p *localControlPanel) deleteSteveLabRunRecord(runID string) error {
	path := p.steveLabRunRecordPath(runID)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (p *localControlPanel) readSteveLabRunRecord(runID string) (steveLabRunRecord, bool) {
	data, err := os.ReadFile(p.steveLabRunRecordPath(runID))
	if err != nil {
		return steveLabRunRecord{}, false
	}
	var record steveLabRunRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return steveLabRunRecord{}, false
	}
	return record, true
}

func (p *localControlPanel) listSteveLabRunRecords() []steveLabRunRecord {
	entries, err := os.ReadDir(p.steveLabRunsDir())
	if err != nil {
		return nil
	}
	var records []steveLabRunRecord
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(p.steveLabRunsDir(), entry.Name()))
		if err != nil {
			continue
		}
		var record steveLabRunRecord
		if err := json.Unmarshal(data, &record); err == nil {
			if record.Status == "cleaned" || record.Status == "deleted" {
				continue
			}
			if record.StevePID > 0 && !processAlive(record.StevePID) {
				record.StevePID = 0
				if record.Status == "serving" || record.Status == "starting" {
					record.Status = "stopped"
				}
				record.UpdatedAt = time.Now()
				_ = p.writeSteveLabRunRecord(record)
			}
			records = append(records, record)
		}
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].CreatedAt.After(records[j].CreatedAt)
	})
	return records
}

func k3dDeleteMissingClusterOK(output string) bool {
	output = strings.ToLower(output)
	return strings.Contains(output, "not found") ||
		strings.Contains(output, "no nodes found") ||
		strings.Contains(output, "no cluster") ||
		strings.Contains(output, "does not exist")
}

func ensureSQLCachePrereqs(kubeconfigPath string) error {
	kubectlPath, err := resolveLocalToolPath("kubectl")
	if err != nil {
		return err
	}
	cmd := exec.Command(kubectlPath, "--kubeconfig", kubeconfigPath, "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(projectCRD)
	cmd.Env = localToolEnv(nil)
	return cmd.Run()
}

const projectCRD = `
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: projects.management.cattle.io
spec:
  group: management.cattle.io
  names:
    kind: Project
    listKind: ProjectList
    plural: projects
    singular: project
  scope: Namespaced
  versions:
  - name: v3
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        type: object
        x-kubernetes-preserve-unknown-fields: true
`
