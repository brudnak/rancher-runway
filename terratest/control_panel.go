package test

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/brudnak/ha-rancher-rke2/internal/buildinfo"
	"github.com/brudnak/ha-rancher-rke2/terratest/ui"
	"github.com/spf13/viper"
)

type localControlPanel struct {
	token                string
	sessionID            string
	startedAt            time.Time
	totalHAs             int
	repoRoot             string
	testDir              string
	configPath           string
	starterConfigCreated bool
	trustedLocalOrigin   bool
	listener             net.Listener
	server               *http.Server
	baseURL              string
	doneCh               chan error

	mu          sync.Mutex
	operations  map[panelOperationName]*panelOperationState
	awsMu       sync.Mutex
	awsCache    panelAWSInventoryState
	awsCacheKey string
	setupEditor *interactiveServer

	rancherTokens             map[int]string
	downstreamKubeconfigCache map[string]string
}

type ControlPanelServerOptions struct {
	OpenBrowser   bool
	ReuseExisting bool
}

type ControlPanelServer struct {
	panel       *localControlPanel
	baseURL     string
	reused      bool
	originalDir string
	cleanupOnce sync.Once
}

type panelState struct {
	Panel         panelSessionState      `json:"panel"`
	Workspace     panelWorkspaceState    `json:"workspace"`
	Setup         panelOperationSnapshot `json:"setup"`
	Readiness     panelOperationSnapshot `json:"readiness"`
	LinodeSetup   panelOperationSnapshot `json:"linodeSetup"`
	LinodeCleanup panelOperationSnapshot `json:"linodeCleanup"`
	Clusters      panelClusterState      `json:"clusters"`
	AWS           panelAWSInventoryState `json:"aws"`
	Cleanup       panelOperationSnapshot `json:"cleanup"`
	Costs         panelCostHistoryState  `json:"costs"`
}

type panelSessionState struct {
	SessionID            string         `json:"sessionId"`
	StartedAt            time.Time      `json:"startedAt"`
	RepoRoot             string         `json:"repoRoot"`
	ConfigPath           string         `json:"configPath"`
	StarterConfigCreated bool           `json:"starterConfigCreated"`
	Build                buildinfo.Info `json:"build"`
}

type panelClusterState struct {
	Items []clusterView `json:"items"`
}

type panelAWSInventoryState struct {
	UpdatedAt time.Time         `json:"updatedAt"`
	Region    string            `json:"region"`
	Owner     string            `json:"owner,omitempty"`
	Queries   []string          `json:"queries"`
	Items     []awsResourceView `json:"items"`
	Error     string            `json:"error,omitempty"`
}

type awsResourceView struct {
	Type    string            `json:"type"`
	ID      string            `json:"id"`
	Name    string            `json:"name,omitempty"`
	Region  string            `json:"region,omitempty"`
	Status  string            `json:"status,omitempty"`
	RunID   string            `json:"runId,omitempty"`
	Owner   string            `json:"owner,omitempty"`
	Source  string            `json:"source"`
	Details string            `json:"details,omitempty"`
	Tags    map[string]string `json:"tags,omitempty"`
}

type clusterView struct {
	ID                  string    `json:"id"`
	RunID               string    `json:"runId,omitempty"`
	Type                string    `json:"type"`
	DeploymentType      string    `json:"deploymentType,omitempty"`
	Role                string    `json:"role,omitempty"`
	HAIndex             int       `json:"haIndex"`
	Name                string    `json:"name"`
	Version             string    `json:"version,omitempty"`
	RancherURL          string    `json:"rancherUrl,omitempty"`
	LoadBalancer        string    `json:"loadBalancer,omitempty"`
	Namespace           string    `json:"namespace,omitempty"`
	ManagementClusterID string    `json:"managementClusterId,omitempty"`
	KubeconfigPath      string    `json:"kubeconfigPath,omitempty"`
	DownloadName        string    `json:"downloadName,omitempty"`
	Provisioning        bool      `json:"provisioning,omitempty"`
	ProvisioningMessage string    `json:"provisioningMessage,omitempty"`
	Available           bool      `json:"available"`
	Reachable           bool      `json:"reachable"`
	Error               string    `json:"error,omitempty"`
	Pods                []podView `json:"pods"`
}

type podView struct {
	Namespace   string `json:"namespace,omitempty"`
	Name        string `json:"name"`
	Ready       string `json:"ready"`
	Status      string `json:"status"`
	Restarts    int    `json:"restarts"`
	Age         string `json:"age"`
	Node        string `json:"node,omitempty"`
	Containers  string `json:"containers"`
	Leader      bool   `json:"leader"`
	LeaderLabel string `json:"leaderLabel,omitempty"`
}

type panelOperationSnapshot struct {
	Running    bool       `json:"running"`
	PID        int        `json:"pid,omitempty"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	Error      string     `json:"error,omitempty"`
	Output     []string   `json:"output"`
	RunID      string     `json:"runId,omitempty"`
	Command    string     `json:"command,omitempty"`
	UpdatedAt  *time.Time `json:"updatedAt,omitempty"`
}

type panelOperationName string

const (
	panelOperationSetup         panelOperationName = "setup"
	panelOperationReadiness     panelOperationName = "readiness"
	panelOperationCleanup       panelOperationName = "cleanup"
	panelOperationLinodeSetup   panelOperationName = "linodeSetup"
	panelOperationLinodeCleanup panelOperationName = "linodeCleanup"
)

type panelOperationState struct {
	Running    bool
	PID        int
	StartedAt  *time.Time
	FinishedAt *time.Time
	Error      string
	Output     []string
	RunID      string
	Command    string
	UpdatedAt  *time.Time
}

type panelCommandSpec struct {
	Operation      panelOperationName
	DisplayName    string
	TestName       string
	Timeout        string
	RunID          string
	StartLine      string
	SuccessLine    string
	AfterSuccess   func()
	AllowWhileDone bool
}

type kubectlPodList struct {
	Items []kubectlPod `json:"items"`
}

type kubectlPod struct {
	Metadata struct {
		Namespace         string    `json:"namespace"`
		Name              string    `json:"name"`
		CreationTimestamp time.Time `json:"creationTimestamp"`
	} `json:"metadata"`
	Spec struct {
		NodeName   string `json:"nodeName"`
		Containers []struct {
			Name string `json:"name"`
		} `json:"containers"`
		InitContainers []struct {
			Name string `json:"name"`
		} `json:"initContainers"`
	} `json:"spec"`
	Status struct {
		Phase             string `json:"phase"`
		Reason            string `json:"reason"`
		ContainerStatuses []struct {
			Name         string `json:"name"`
			Ready        bool   `json:"ready"`
			RestartCount int    `json:"restartCount"`
			State        struct {
				Waiting struct {
					Reason string `json:"reason"`
				} `json:"waiting"`
				Terminated struct {
					Reason string `json:"reason"`
				} `json:"terminated"`
			} `json:"state"`
		} `json:"containerStatuses"`
		InitContainerStatuses []struct {
			Name         string `json:"name"`
			Ready        bool   `json:"ready"`
			RestartCount int    `json:"restartCount"`
			State        struct {
				Waiting struct {
					Reason string `json:"reason"`
				} `json:"waiting"`
				Terminated struct {
					Reason string `json:"reason"`
				} `json:"terminated"`
			} `json:"state"`
		} `json:"initContainerStatuses"`
	} `json:"status"`
}

type provisioningClusterList struct {
	Items []provisioningClusterItem `json:"items"`
}

type provisioningClusterItem struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Status struct {
		ClusterName string `json:"clusterName"`
	} `json:"status"`
}

type managementClusterList struct {
	Items []managementClusterItem `json:"items"`
}

type managementClusterItem struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		DisplayName string `json:"displayName"`
	} `json:"spec"`
}

type discoveredDownstreamCluster struct {
	Name                string
	Namespace           string
	ManagementClusterID string
}

func newLocalControlPanel(totalHAs int) (*localControlPanel, error) {
	return newLocalControlPanelWithNetwork(totalHAs, true)
}

func newEmbeddedLocalControlPanel(totalHAs int) (*localControlPanel, error) {
	return newLocalControlPanelWithNetwork(totalHAs, false)
}

func newLocalControlPanelWithNetwork(totalHAs int, bindNetwork bool) (*localControlPanel, error) {
	token, err := randomConfirmationToken()
	if err != nil {
		return nil, fmt.Errorf("failed to create control panel token: %w", err)
	}
	sessionID, err := randomConfirmationToken()
	if err != nil {
		return nil, fmt.Errorf("failed to create control panel session id: %w", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to determine working directory: %w", err)
	}
	repoRoot, testDir, err := resolveControlPanelPaths(cwd)
	if err != nil {
		return nil, err
	}

	var listener net.Listener
	baseURL := "/"
	if bindNetwork {
		listener, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, fmt.Errorf("failed to start control panel listener: %w", err)
		}
		baseURL = fmt.Sprintf("http://%s/?token=%s", listener.Addr().String(), token)
	}

	panel := &localControlPanel{
		token:                     token,
		sessionID:                 sessionID[:8],
		startedAt:                 time.Now(),
		totalHAs:                  totalHAs,
		repoRoot:                  repoRoot,
		testDir:                   testDir,
		configPath:                filepath.Join(repoRoot, "tool-config.yml"),
		trustedLocalOrigin:        !bindNetwork,
		listener:                  listener,
		baseURL:                   baseURL,
		doneCh:                    make(chan error, 1),
		operations:                newPanelOperations(),
		rancherTokens:             map[int]string{},
		downstreamKubeconfigCache: map[string]string{},
	}
	panel.setupEditor = panel.newSetupEditor()
	panel.loadPersistedOperations(true)
	panel.server = &http.Server{Handler: panel.handler()}
	return panel, nil
}

func (p *localControlPanel) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", p.handleIndex)
	p.registerSetupEditorHandlers(mux)
	mux.HandleFunc("/static/", p.handleControlPanelStaticAsset)
	mux.HandleFunc("/api/preflight", p.handlePreflight)
	mux.HandleFunc("/api/state", p.handleState)
	mux.HandleFunc("/api/logs", p.handleLogs)
	mux.HandleFunc("/api/logs/stream", p.handleLogStream)
	mux.HandleFunc("/api/docker-logs", p.handleDockerLogs)
	mux.HandleFunc("/api/kubeconfig", p.handleKubeconfigDownload)
	mux.HandleFunc("/api/kubeconfig/save", p.handleKubeconfigSave)
	mux.HandleFunc("/api/helm-command", p.handleHelmCommandDownload)
	mux.HandleFunc("/api/open-url", p.handleOpenURL)
	mux.HandleFunc("/api/open-path", p.handleOpenPath)
	mux.HandleFunc("/api/setup", p.handleSetup)
	mux.HandleFunc("/api/run-slots/start", p.handleRunSlotStart)
	mux.HandleFunc("/api/operations/abort", p.handleAbortOperation)
	mux.HandleFunc("/api/readiness", p.handleReadiness)
	mux.HandleFunc("/api/cleanup", p.handleCleanup)
	mux.HandleFunc("/api/costs/reset", p.handleCostLedgerReset)
	mux.HandleFunc("/api/local-artifacts/clean", p.handleLocalArtifactsClean)
	mux.HandleFunc("/api/shutdown", p.handleShutdown)
	return mux
}

func StartHAControlPanelServer(repoRoot string, opts ControlPanelServerOptions) (*ControlPanelServer, error) {
	resolvedRoot := strings.TrimSpace(repoRoot)
	var testDir string
	var err error
	if resolvedRoot == "" {
		cwd, cwdErr := os.Getwd()
		if cwdErr != nil {
			return nil, fmt.Errorf("failed to determine working directory: %w", cwdErr)
		}
		resolvedRoot, testDir, err = resolveControlPanelPaths(cwd)
	} else {
		resolvedRoot, testDir, err = resolveControlPanelPaths(resolvedRoot)
	}
	if err != nil {
		return nil, err
	}

	starterConfigCreated := false
	if configPath, created, err := ensureStarterToolConfigForPanel(resolvedRoot); err != nil {
		return nil, err
	} else if created {
		starterConfigCreated = true
		log.Printf("[control-panel] Created starter local config at %s", configPath)
	}

	if err := setupConfigE(resolvedRoot); err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	totalHAs := configuredRancherInstanceCount()
	if totalHAs < 1 {
		return nil, fmt.Errorf("configured Rancher instance count must be at least 1")
	}

	originalDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to determine working directory: %w", err)
	}
	if err := os.Chdir(testDir); err != nil {
		return nil, fmt.Errorf("failed to enter terratest directory: %w", err)
	}

	server := &ControlPanelServer{originalDir: originalDir}
	if opts.ReuseExisting {
		existingURL, ok, err := existingControlPanelURL(resolvedRoot)
		if err != nil {
			log.Printf("[control-panel] Existing panel reuse failed: %v", err)
		}
		if ok {
			server.baseURL = existingURL
			server.reused = true
			server.cleanup()
			log.Printf("[control-panel] Reusing existing local control panel %s", existingURL)
			if opts.OpenBrowser {
				if err := openBrowser(existingURL); err != nil {
					return nil, fmt.Errorf("failed to open existing control panel: %w", err)
				}
			}
			return server, nil
		}
	}

	panel, err := newLocalControlPanel(totalHAs)
	if err != nil {
		return nil, fmt.Errorf("failed to start local control panel: %w", err)
	}
	panel.starterConfigCreated = starterConfigCreated
	server.panel = panel
	server.baseURL = panel.baseURL

	panel.start()
	if err := panel.persistPanelSession(); err != nil {
		log.Printf("[control-panel] Failed to persist panel session: %v", err)
	}

	log.Printf("[control-panel] Local control panel available at %s", panel.baseURL)

	if opts.OpenBrowser {
		if err := openBrowser(panel.baseURL); err != nil {
			log.Printf("[control-panel] Failed to open browser automatically: %v", err)
		}
	}

	return server, nil
}

func StartHAControlPanelHandler(repoRoot string) (*ControlPanelServer, http.Handler, error) {
	server, err := startHAControlPanel(repoRoot, ControlPanelServerOptions{})
	if err != nil {
		return nil, nil, err
	}
	return server, server.panel.handler(), nil
}

func startHAControlPanel(repoRoot string, opts ControlPanelServerOptions) (*ControlPanelServer, error) {
	resolvedRoot := strings.TrimSpace(repoRoot)
	var testDir string
	var err error
	if resolvedRoot == "" {
		cwd, cwdErr := os.Getwd()
		if cwdErr != nil {
			return nil, fmt.Errorf("failed to determine working directory: %w", cwdErr)
		}
		resolvedRoot, testDir, err = resolveControlPanelPaths(cwd)
	} else {
		resolvedRoot, testDir, err = resolveControlPanelPaths(resolvedRoot)
	}
	if err != nil {
		return nil, err
	}

	starterConfigCreated := false
	if configPath, created, err := ensureStarterToolConfigForPanel(resolvedRoot); err != nil {
		return nil, err
	} else if created {
		starterConfigCreated = true
		log.Printf("[control-panel] Created starter local config at %s", configPath)
	}

	if err := setupConfigE(resolvedRoot); err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	totalHAs := configuredRancherInstanceCount()
	if totalHAs < 1 {
		return nil, fmt.Errorf("configured Rancher instance count must be at least 1")
	}

	originalDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to determine working directory: %w", err)
	}
	if err := os.Chdir(testDir); err != nil {
		return nil, fmt.Errorf("failed to enter terratest directory: %w", err)
	}

	panel, err := newEmbeddedLocalControlPanel(totalHAs)
	if err != nil {
		if restoreErr := os.Chdir(originalDir); restoreErr != nil {
			log.Printf("[control-panel] Failed to restore working directory %s: %v", originalDir, restoreErr)
		}
		return nil, fmt.Errorf("failed to start local control panel: %w", err)
	}
	panel.starterConfigCreated = starterConfigCreated
	server := &ControlPanelServer{
		panel:       panel,
		baseURL:     panel.baseURL,
		originalDir: originalDir,
	}
	return server, nil
}

func RunHAControlPanel(repoRoot string) error {
	server, err := StartHAControlPanelServer(repoRoot, ControlPanelServerOptions{
		OpenBrowser:   true,
		ReuseExisting: true,
	})
	if err != nil {
		return err
	}
	if server.Reused() {
		return nil
	}
	defer server.cleanup()

	if err := server.Wait(); err != nil {
		return fmt.Errorf("local control panel exited with error: %w", err)
	}
	return nil
}

func (s *ControlPanelServer) URL() string {
	if s == nil {
		return ""
	}
	return s.baseURL
}

func (s *ControlPanelServer) Reused() bool {
	return s != nil && s.reused
}

func (s *ControlPanelServer) LifecycleRunning() bool {
	return s != nil && s.panel != nil && s.panel.anyOperationRunning()
}

func (s *ControlPanelServer) RunningOperation() string {
	if s == nil || s.panel == nil {
		return ""
	}
	s.panel.mu.Lock()
	defer s.panel.mu.Unlock()
	if !s.panel.anyOperationRunningLocked() {
		return ""
	}
	return s.panel.runningOperationNameLocked()
}

func (s *ControlPanelServer) Wait() error {
	if s == nil || s.panel == nil {
		return nil
	}
	err := s.panel.wait()
	s.cleanup()
	return err
}

func (s *ControlPanelServer) Shutdown(ctx context.Context) error {
	if s == nil || s.panel == nil {
		return nil
	}
	err := s.panel.server.Shutdown(ctx)
	s.cleanup()
	return err
}

func (s *ControlPanelServer) cleanup() {
	if s == nil {
		return
	}
	s.cleanupOnce.Do(func() {
		if s.panel != nil {
			s.panel.removePanelSession()
		}
		if strings.TrimSpace(s.originalDir) != "" {
			if restoreErr := os.Chdir(s.originalDir); restoreErr != nil {
				log.Printf("[control-panel] Failed to restore working directory %s: %v", s.originalDir, restoreErr)
			}
		}
	})
}

func (p *localControlPanel) start() {
	go func() {
		if p.listener == nil {
			p.doneCh <- nil
			return
		}
		err := p.server.Serve(p.listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			p.doneCh <- err
			return
		}
		p.doneCh <- nil
	}()
}

func (p *localControlPanel) wait() error {
	return <-p.doneCh
}

func (p *localControlPanel) handleIndex(w http.ResponseWriter, r *http.Request) {
	if !p.authorized(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	setupEditorHTML, err := p.renderSetupEditorHTML()
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to render setup editor: %v", err), http.StatusInternalServerError)
		return
	}

	page := template.Must(template.New("control-panel").Parse(ui.ControlPanelHTML))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = page.Execute(w, struct {
		Token           string
		SetupEditorHTML template.HTML
	}{
		Token:           p.token,
		SetupEditorHTML: setupEditorHTML,
	})
}

type controlPanelStaticAsset struct {
	ContentType string
	Body        string
}

var controlPanelStaticAssets = map[string]controlPanelStaticAsset{
	"/static/control_panel.css": {
		ContentType: "text/css; charset=utf-8",
		Body:        ui.ControlPanelCSS,
	},
	"/static/control_panel.js": {
		ContentType: "application/javascript; charset=utf-8",
		Body:        ui.ControlPanelJS,
	},
	"/static/control_panel_clusters.js": {
		ContentType: "application/javascript; charset=utf-8",
		Body:        ui.ControlPanelClustersJS,
	},
	"/static/control_panel_modals.js": {
		ContentType: "application/javascript; charset=utf-8",
		Body:        ui.ControlPanelModalsJS,
	},
	"/static/control_panel_runs.js": {
		ContentType: "application/javascript; charset=utf-8",
		Body:        ui.ControlPanelRunsJS,
	},
	"/static/control_panel_utils.js": {
		ContentType: "application/javascript; charset=utf-8",
		Body:        ui.ControlPanelUtilsJS,
	},
}

func (p *localControlPanel) handleControlPanelStaticAsset(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedLocalBrowserRead(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	asset, ok := controlPanelStaticAssets[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", asset.ContentType)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(asset.Body))
}

func (p *localControlPanel) handleState(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedReadOnly(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	state := p.buildState()
	writeJSON(w, state)
}

func (p *localControlPanel) handlePreflight(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedReadOnly(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	writeJSON(w, p.collectPanelPreflight())
}

func (p *localControlPanel) handleLogs(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedReadOnly(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cluster, pod, namespace, container, err := p.logRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	args := []string{"logs", pod, "-n", namespace, "--tail=200"}
	if container != "" {
		args = append(args, "-c", container)
	} else {
		args = append(args, "--all-containers=true")
	}

	output, err := runKubectl(cluster.KubeconfigPath, args...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	writeJSON(w, map[string]string{"text": output})
}

func (p *localControlPanel) handleLogStream(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedReadOnly(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	cluster, pod, namespace, container, err := p.logRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	args := []string{"logs", "-f", pod, "-n", namespace, "--tail=20"}
	if container != "" {
		args = append(args, "-c", container)
	} else {
		args = append(args, "--all-containers=true")
	}

	cmd := exec.CommandContext(r.Context(), "kubectl", append([]string{"--kubeconfig", cluster.KubeconfigPath}, args...)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to open log stream: %v", err), http.StatusInternalServerError)
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to open log stream: %v", err), http.StatusInternalServerError)
		return
	}
	if err := cmd.Start(); err != nil {
		http.Error(w, fmt.Sprintf("failed to start log stream: %v", err), http.StatusBadGateway)
		return
	}
	defer cmd.Wait()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	sendLine := func(eventName, line string) {
		fmt.Fprintf(w, "event: %s\n", eventName)
		fmt.Fprintf(w, "data: %s\n\n", strings.ReplaceAll(line, "\n", "\\n"))
		flusher.Flush()
	}

	stdoutDone := make(chan struct{})
	go func() {
		defer close(stdoutDone)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			sendLine("line", scanner.Text())
		}
	}()

	stderrBytes, _ := io.ReadAll(stderr)
	<-stdoutDone
	if len(stderrBytes) > 0 {
		sendLine("error", string(stderrBytes))
	}
	sendLine("end", "stream closed")
}

func (p *localControlPanel) handleDockerLogs(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedReadOnly(r) {
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
	if cluster.DeploymentType != deploymentTypeLinodeDocker && cluster.Type != "linode" {
		http.Error(w, "Docker logs are only available for Linode Docker installs", http.StatusBadRequest)
		return
	}
	if !cluster.Available {
		http.Error(w, "Linode Docker install is not available yet", http.StatusConflict)
		return
	}

	output, err := runLinodeDockerSSHCommand(cluster.LoadBalancer, linodeRootPassword(), linodeDockerLogSnapshotCommand(220))
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to collect Docker logs over SSH: %v", err), http.StatusBadGateway)
		return
	}
	output = sanitizeDiagnosticOutput(output)
	output = lastNonEmptyLines(output, 420)
	if output == "" {
		output = "(no Docker output)"
	}
	writeJSON(w, map[string]string{"text": output})
}

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

	cluster, err := p.clusterByID(clusterID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
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

func (p *localControlPanel) handleSetup(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	http.Error(w, "direct setup start is disabled; use Start isolated run so the run gets a dedicated slot and state", http.StatusGone)
}

func (p *localControlPanel) handleRunSlotStart(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	http.Error(w, "direct isolated run start is disabled; open the Setup tab, resolve the plan, then press Continue", http.StatusGone)
}

func (p *localControlPanel) handleReadiness(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := p.startReadiness(); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	writeJSON(w, map[string]string{"status": "readiness started"})
}

func (p *localControlPanel) handleAbortOperation(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Operation string `json:"operation"`
		RunID     string `json:"runId"`
		Confirm   string `json:"confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(strings.ToLower(req.Confirm)) != "stop" {
		http.Error(w, "typed confirmation must equal stop", http.StatusBadRequest)
		return
	}

	operation := panelOperationName(strings.TrimSpace(strings.ToLower(req.Operation)))
	switch operation {
	case panelOperationSetup, panelOperationReadiness, panelOperationCleanup, panelOperationLinodeSetup, panelOperationLinodeCleanup:
	default:
		http.Error(w, "operation must be setup, readiness, cleanup, linodeSetup, or linodeCleanup", http.StatusBadRequest)
		return
	}

	if err := p.abortOperation(operation, req.RunID); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	writeJSON(w, map[string]string{"status": "stop requested"})
}

func (p *localControlPanel) handleCleanup(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Confirm string `json:"confirm"`
		RunID   string `json:"runId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	confirm := strings.TrimSpace(strings.ToLower(req.Confirm))
	if confirm != "cleanup" && confirm != "destroy" {
		http.Error(w, "typed confirmation must equal destroy", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.RunID) != "" {
		if err := p.startCleanupForRun(req.RunID); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		writeJSON(w, map[string]string{"status": "cleanup started"})
		return
	}

	if err := p.startCleanup(); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	writeJSON(w, map[string]string{"status": "cleanup started"})
}

func (p *localControlPanel) handleCostLedgerReset(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if p.anyOperationRunning() {
		http.Error(w, fmt.Sprintf("cannot reset cost ledger while %s is running", p.runningOperationName()), http.StatusConflict)
		return
	}

	var req struct {
		Confirm string `json:"confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(strings.ToLower(req.Confirm)) != "reset costs" {
		http.Error(w, "typed confirmation must equal reset costs", http.StatusBadRequest)
		return
	}

	if err := resetCostLedger(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{
		"status": "cost ledger reset",
		"costs":  discoverCostHistory(),
	})
}

func (p *localControlPanel) handleLocalArtifactsClean(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if p.anyOperationRunning() {
		http.Error(w, fmt.Sprintf("cannot clean local artifacts while %s is running", p.runningOperationName()), http.StatusConflict)
		return
	}

	var req struct {
		Confirm string `json:"confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(strings.ToLower(req.Confirm)) != "clean local artifacts" {
		http.Error(w, "typed confirmation must equal clean local artifacts", http.StatusBadRequest)
		return
	}

	result, err := p.cleanLocalArtifacts()
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	writeJSON(w, map[string]any{
		"status":    "local artifacts cleaned",
		"removed":   result.Removed,
		"workspace": p.workspaceState(),
		"costs":     discoverCostHistory(),
	})
}

func (p *localControlPanel) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if p.anyOperationRunning() {
		http.Error(w, fmt.Sprintf("cannot stop panel while %s is running", p.runningOperationName()), http.StatusConflict)
		return
	}

	writeJSON(w, map[string]string{"status": "shutting down"})

	go func() {
		time.Sleep(150 * time.Millisecond)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = p.server.Shutdown(shutdownCtx)
	}()
}

func (p *localControlPanel) buildState() panelState {
	workspace := p.workspaceState()
	activeRunIDs := map[string]bool{}
	for _, record := range workspace.Runs {
		activeRunIDs[safeRunPathSegment(record.RunID)] = true
	}
	clusters := p.discoverClusters()
	return panelState{
		Panel: panelSessionState{
			SessionID:            p.sessionID,
			StartedAt:            p.startedAt,
			RepoRoot:             p.repoRoot,
			ConfigPath:           p.configPath,
			StarterConfigCreated: p.starterConfigCreated,
			Build:                buildinfo.Current(),
		},
		Workspace:     workspace,
		Setup:         p.snapshotOperationForRuns(panelOperationSetup, activeRunIDs),
		Readiness:     p.snapshotOperationForRuns(panelOperationReadiness, activeRunIDs),
		LinodeSetup:   p.snapshotOperationForRuns(panelOperationLinodeSetup, activeRunIDs),
		LinodeCleanup: p.snapshotOperationForRuns(panelOperationLinodeCleanup, activeRunIDs),
		Clusters: panelClusterState{
			Items: clusters,
		},
		AWS:     p.discoverAWSInventory(workspace.Runs),
		Cleanup: p.snapshotOperationForRuns(panelOperationCleanup, activeRunIDs),
		Costs:   discoverCostHistory(),
	}
}

func (p *localControlPanel) snapshotOperation(name panelOperationName) panelOperationSnapshot {
	return p.snapshotOperationForRuns(name, nil)
}

func (p *localControlPanel) snapshotOperationForRuns(name panelOperationName, activeRunIDs map[string]bool) panelOperationSnapshot {
	p.mu.Lock()
	defer p.mu.Unlock()

	op := p.operationLocked(name)
	if op.Running && op.PID > 0 && !processAlive(op.PID) {
		now := time.Now()
		op.Running = false
		op.FinishedAt = &now
		op.UpdatedAt = &now
		op.Error = "operation process exited before reporting completion"
		op.Output = append(op.Output, "[control-panel] Operation process exited before reporting completion; status marked stale.")
		p.persistOperationsLocked()
	}
	recentCleanup := (name == panelOperationCleanup || name == panelOperationLinodeCleanup) && op.FinishedAt != nil && op.Error == "" && time.Since(*op.FinishedAt) < time.Hour
	if activeRunIDs != nil && !op.Running && op.RunID != "" && !activeRunIDs[safeRunPathSegment(op.RunID)] && !recentCleanup {
		return panelOperationSnapshot{Output: []string{}}
	}
	outputCopy := append([]string(nil), op.Output...)
	if outputCopy == nil {
		outputCopy = []string{}
	}
	return panelOperationSnapshot{
		Running:    op.Running,
		PID:        op.PID,
		StartedAt:  op.StartedAt,
		FinishedAt: op.FinishedAt,
		Error:      op.Error,
		Output:     outputCopy,
		RunID:      op.RunID,
		Command:    op.Command,
		UpdatedAt:  op.UpdatedAt,
	}
}

func (p *localControlPanel) discoverClusters() []clusterView {
	runRecords := p.listRunRecords()
	if len(runRecords) == 0 {
		if record, ok := p.readCurrentRunRecord(); ok {
			runRecords = []panelRunRecord{record}
		}
	}
	if len(runRecords) == 0 {
		return p.discoverClustersForRun(panelRunRecord{
			RunID:           "",
			TotalHAs:        p.totalHAs,
			RancherVersions: readRequestedRancherVersionsForPanel(p.totalHAs),
			HAOutputRoot:    p.currentHAOutputRoot(),
		})
	}

	clusters := make([]clusterView, 0)
	for _, record := range runRecords {
		clusters = append(clusters, p.discoverClustersForRun(record)...)
	}
	return clusters
}

func (p *localControlPanel) discoverClustersForRun(record panelRunRecord) []clusterView {
	outputs, _ := readTerraformFlatOutputsWithModule(p.repoRoot, record.TerraformStatePath, record.TerraformDataDir, record.TerraformModuleDir)
	switch recordDeploymentType(record, outputs) {
	case deploymentTypeHostedTenantK3S:
		return p.discoverHostedTenantClustersForRun(record, outputs)
	case deploymentTypeLinodeDocker:
		return p.discoverLinodeDockerClustersForRun(record, outputs)
	}
	versions := record.RancherVersions
	if len(versions) == 0 {
		versions = readRequestedRancherVersionsForPanel(p.totalHAs)
	}
	downstreamRecords, _ := readDownstreamOutputRecords()
	recordsByHA := downstreamRecordsByHA(downstreamRecords)
	setupRunning := p.operationRunning(panelOperationSetup)
	readinessRunning := p.operationRunning(panelOperationReadiness)
	runID := safeRunPathSegment(record.RunID)
	totalHAs := record.TotalHAs
	if totalHAs < 1 {
		totalHAs = p.totalHAs
	}

	clusters := make([]clusterView, 0, totalHAs)
	for i := 1; i <= totalHAs; i++ {
		haDir := p.haInstanceDirForRun(record, i)
		kubeconfigPath := filepath.Join(haDir, "kube_config.yaml")
		kubeconfigExists := pathExists(kubeconfigPath)
		hasRunSignal := kubeconfigExists ||
			pathExists(haDir) ||
			hasHAFlatOutput(outputs, i) ||
			len(recordsByHA[i]) > 0 ||
			setupRunning ||
			readinessRunning
		if !hasRunSignal {
			continue
		}

		cluster := clusterView{
			ID:           localClusterIDForRun(runID, i),
			RunID:        runID,
			Type:         "local",
			HAIndex:      i,
			Name:         runScopedClusterName(runID, fmt.Sprintf("HA %d Local", i)),
			DownloadName: runScopedDownloadName(runID, fmt.Sprintf("local-ha-%d.yaml", i)),
		}
		if len(versions) >= i {
			cluster.Version = versions[i-1]
		}
		cluster.KubeconfigPath = kubeconfigPath
		if outputs != nil {
			cluster.RancherURL = clickableURL(outputs[fmt.Sprintf("ha_%d_rancher_url", i)])
			cluster.LoadBalancer = outputs[fmt.Sprintf("ha_%d_aws_lb", i)]
		}

		if !kubeconfigExists {
			if setupRunning {
				cluster.Provisioning = true
				cluster.ProvisioningMessage = "Setup is running. Kubeconfig will appear after Terraform and RKE2 bootstrap complete."
			}
			cluster.Error = "kubeconfig not found"
			clusters = append(clusters, cluster)
			continue
		}

		cluster.Available = true
		pods, err := fetchLocalRancherPods(cluster.KubeconfigPath)
		if err != nil {
			cluster.Error = err.Error()
			clusters = append(clusters, cluster)
			clusters = append(clusters, p.discoverDownstreamClusters(cluster, recordsByHA[i])...)
			continue
		}

		cluster.Reachable = true
		cluster.Pods = pods
		clusters = append(clusters, cluster)
		clusters = append(clusters, p.discoverDownstreamClusters(cluster, recordsByHA[i])...)
	}

	return clusters
}

func recordDeploymentType(record panelRunRecord, outputs map[string]string) string {
	if record.DeploymentType != "" {
		return record.DeploymentType
	}
	if hasHostedTenantFlatOutputs(outputs) {
		return deploymentTypeHostedTenantK3S
	}
	return deploymentType()
}

func (p *localControlPanel) discoverLinodeDockerClustersForRun(record panelRunRecord, outputs map[string]string) []clusterView {
	versions := record.RancherVersions
	if len(versions) == 0 {
		versions = readRequestedRancherVersionsForPanel(p.totalHAs)
	}
	setupRunning := p.operationRunning(panelOperationLinodeSetup)
	runID := safeRunPathSegment(record.RunID)
	total := record.TotalHAs
	if total < 1 {
		total = len(versions)
	}
	if total < 1 {
		total = 1
	}

	clusters := make([]clusterView, 0, total)
	for i := 1; i <= total; i++ {
		rancherURL := ""
		ip := ""
		if outputs != nil {
			rancherURL = clickableURL(outputs[fmt.Sprintf("linode_%d_rancher_url", i)])
			ip = outputs[fmt.Sprintf("linode_%d_ip", i)]
		}
		if rancherURL == "" && ip == "" && !setupRunning {
			continue
		}
		cluster := clusterView{
			ID:             runScopedClusterName(runID, fmt.Sprintf("linode-docker-%d", i)),
			RunID:          runID,
			Type:           "linode",
			DeploymentType: deploymentTypeLinodeDocker,
			Role:           "docker",
			HAIndex:        i,
			Name:           runScopedClusterName(runID, fmt.Sprintf("Docker Rancher %d", i)),
			RancherURL:     rancherURL,
			LoadBalancer:   ip,
			Available:      rancherURL != "" || ip != "",
		}
		if len(versions) >= i {
			cluster.Version = versions[i-1]
		}
		if setupRunning && !cluster.Available {
			cluster.Provisioning = true
			cluster.ProvisioningMessage = "Linode setup is running. The Rancher URL will appear after Terraform apply completes."
			cluster.Error = "waiting for Linode output"
		}
		if cluster.Available && rancherURL != "" && linodeDockerRancherHTTPReachable(rancherURL) {
			cluster.Reachable = true
		}
		clusters = append(clusters, cluster)
	}
	return clusters
}

func linodeDockerRancherHTTPReachable(rancherURL string) bool {
	rancherURL = strings.TrimSpace(rancherURL)
	if rancherURL == "" {
		return false
	}
	client := &http.Client{
		Timeout: 1500 * time.Millisecond,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	ready, _ := rancherHTTPReadyForPanel(client, rancherURL)
	return ready
}

func rancherHTTPReadyForPanel(client *http.Client, rancherURL string) (bool, string) {
	rootProbe, rootErr := rancherHTTPProbeForPanel(client, rancherURL)
	if rootErr != nil {
		return false, fmt.Sprintf("root error: %v", rootErr)
	}
	apiProbe, apiErr := rancherHTTPProbeForPanel(client, strings.TrimRight(rancherURL, "/")+"/v3")
	if apiErr != nil {
		return false, fmt.Sprintf("root=%d api error: %v", rootProbe.Status, apiErr)
	}
	if !rancherHTTPProbeReadyForPanel(rootProbe) {
		return false, fmt.Sprintf("root=%d %s api=%d", rootProbe.Status, rancherHTTPProbeNotReadyReasonForPanel(rootProbe), apiProbe.Status)
	}
	if !rancherHTTPProbeReadyForPanel(apiProbe) {
		return false, fmt.Sprintf("root=%d api=%d %s", rootProbe.Status, apiProbe.Status, rancherHTTPProbeNotReadyReasonForPanel(apiProbe))
	}
	return true, fmt.Sprintf("root=%d api=%d", rootProbe.Status, apiProbe.Status)
}

type rancherHTTPProbeResultForPanel struct {
	Status int
	Body   string
}

func rancherHTTPProbeForPanel(client *http.Client, target string) (rancherHTTPProbeResultForPanel, error) {
	resp, err := client.Get(target)
	if err != nil {
		return rancherHTTPProbeResultForPanel{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return rancherHTTPProbeResultForPanel{Status: resp.StatusCode, Body: string(body)}, nil
}

func rancherHTTPProbeReadyForPanel(probe rancherHTTPProbeResultForPanel) bool {
	if rancherHTTPProbeAPIAggregationNotReadyForPanel(probe) {
		return false
	}
	switch probe.Status {
	case http.StatusOK,
		http.StatusMovedPermanently,
		http.StatusFound,
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusNotFound:
		return true
	default:
		return false
	}
}

func rancherHTTPProbeAPIAggregationNotReadyForPanel(probe rancherHTTPProbeResultForPanel) bool {
	return strings.Contains(strings.ToLower(probe.Body), "api aggregation not ready")
}

func rancherHTTPProbeNotReadyReasonForPanel(probe rancherHTTPProbeResultForPanel) string {
	if rancherHTTPProbeAPIAggregationNotReadyForPanel(probe) {
		return "body=API Aggregation not ready"
	}
	return "not ready"
}

func (p *localControlPanel) discoverHostedTenantClustersForRun(record panelRunRecord, outputs map[string]string) []clusterView {
	versions := record.RancherVersions
	if len(versions) == 0 {
		versions = readRequestedRancherVersionsForPanel(p.totalHAs)
	}
	setupRunning := p.operationRunning(panelOperationSetup)
	runID := safeRunPathSegment(record.RunID)
	totalInstances := record.TotalHAs
	if totalInstances < 1 {
		totalInstances = configuredRancherInstanceCount()
	}
	if totalInstances < 1 {
		totalInstances = p.totalHAs
	}

	clusters := make([]clusterView, 0, totalInstances)
	for i := 1; i <= totalInstances; i++ {
		instanceDir := p.hostedTenantInstanceDirForRun(record, i)
		kubeconfigPath := filepath.Join(instanceDir, "kube_config.yaml")
		kubeconfigExists := pathExists(kubeconfigPath)
		hasRunSignal := kubeconfigExists ||
			pathExists(instanceDir) ||
			hasHostedTenantFlatOutput(outputs, i) ||
			setupRunning
		if !hasRunSignal {
			continue
		}

		role := "tenant"
		displayName := fmt.Sprintf("Tenant Rancher %d", i-1)
		if i == 1 {
			role = "host"
			displayName = "Host Rancher"
		}
		cluster := clusterView{
			ID:             hostedTenantClusterIDForRun(runID, i),
			RunID:          runID,
			Type:           "local",
			DeploymentType: deploymentTypeHostedTenantK3S,
			Role:           role,
			HAIndex:        i,
			Name:           runScopedClusterName(runID, displayName),
			DownloadName:   runScopedDownloadName(runID, fmt.Sprintf("hosted-tenant-%d.yaml", i)),
			KubeconfigPath: kubeconfigPath,
		}
		if len(versions) >= i {
			cluster.Version = versions[i-1]
		}
		if outputs != nil {
			cluster.RancherURL = clickableURL(outputs[fmt.Sprintf("hosted_%d_rancher_url", i)])
		}
		if !kubeconfigExists {
			if setupRunning {
				cluster.Provisioning = true
				cluster.ProvisioningMessage = "Setup is running. Kubeconfig will appear after Terraform and K3s bootstrap complete."
			}
			cluster.Error = "kubeconfig not found"
			clusters = append(clusters, cluster)
			continue
		}
		cluster.Available = true
		pods, err := fetchLocalRancherPods(cluster.KubeconfigPath)
		if err != nil {
			cluster.Error = err.Error()
			clusters = append(clusters, cluster)
			continue
		}
		cluster.Reachable = true
		cluster.Pods = pods
		clusters = append(clusters, cluster)
	}
	return clusters
}

func (p *localControlPanel) operationRunning(name panelOperationName) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.operationLocked(name).Running
}

func hasHAFlatOutput(outputs map[string]string, instanceNum int) bool {
	if outputs == nil {
		return false
	}

	prefix := fmt.Sprintf("ha_%d_", instanceNum)
	for key, value := range outputs {
		if strings.HasPrefix(key, prefix) && strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func hasHostedTenantFlatOutputs(outputs map[string]string) bool {
	if outputs == nil {
		return false
	}
	for key, value := range outputs {
		if strings.HasPrefix(key, "hosted_") && strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func hasHostedTenantFlatOutput(outputs map[string]string, instanceNum int) bool {
	if outputs == nil {
		return false
	}
	prefix := fmt.Sprintf("hosted_%d_", instanceNum)
	for key, value := range outputs {
		if strings.HasPrefix(key, prefix) && strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (p *localControlPanel) discoverDownstreamClusters(local clusterView, records []downstreamOutputRecord) []clusterView {
	if !local.Available {
		return nil
	}

	provisioningClusters, err := discoverProvisioningDownstreamClusters(local.KubeconfigPath)
	if err != nil {
		return downstreamClustersFromRecords(local, records, err)
	}

	recordByName := downstreamRecordsByClusterKey(records)
	activeIDs := map[string]bool{}
	clusters := make([]clusterView, 0, len(provisioningClusters))
	for _, item := range provisioningClusters {
		key := provisioningClusterRecordKey(item.Namespace, item.Name)
		record := recordByName[key]
		clusterID := downstreamClusterIDForRun(local.RunID, local.HAIndex, item.Namespace, item.Name)
		activeIDs[clusterID] = true
		cluster := clusterView{
			ID:                  clusterID,
			RunID:               local.RunID,
			Type:                "downstream",
			HAIndex:             local.HAIndex,
			Name:                item.Name,
			Version:             record.K3SVersion,
			RancherURL:          local.RancherURL,
			Namespace:           item.Namespace,
			ManagementClusterID: item.ManagementClusterID,
			DownloadName:        safeKubeconfigDownloadName(item.Name),
			Available:           true,
		}
		if record.KubeconfigPath != "" {
			cluster.KubeconfigPath = record.KubeconfigPath
		}
		if cluster.ManagementClusterID == "" {
			cluster.Provisioning = true
			cluster.ProvisioningMessage = "Waiting for Rancher to assign a downstream cluster id"
			clusters = append(clusters, cluster)
			continue
		}

		kubeconfigPath, err := p.ensureDownstreamKubeconfig(local.HAIndex, local.RancherURL, cluster.ID, item.ManagementClusterID, record.KubeconfigPath)
		if err != nil {
			cluster.Provisioning = true
			cluster.ProvisioningMessage = "Waiting for downstream kubeconfig"
			clusters = append(clusters, cluster)
			continue
		}
		cluster.KubeconfigPath = kubeconfigPath

		pods, err := fetchAllPods(kubeconfigPath)
		if err != nil {
			cluster.Provisioning = true
			cluster.ProvisioningMessage = "Waiting for downstream Kubernetes API"
			clusters = append(clusters, cluster)
			continue
		}
		cluster.Reachable = true
		cluster.Pods = pods
		clusters = append(clusters, cluster)
	}
	p.pruneStaleDownstreamKubeconfigs(local.RunID, local.HAIndex, activeIDs)

	return clusters
}

func downstreamClustersFromRecords(local clusterView, records []downstreamOutputRecord, discoverErr error) []clusterView {
	clusters := make([]clusterView, 0, len(records))
	for _, record := range records {
		cluster := clusterView{
			ID:                  downstreamClusterIDForRun(local.RunID, local.HAIndex, record.Namespace, record.ClusterName),
			RunID:               local.RunID,
			Type:                "downstream",
			HAIndex:             local.HAIndex,
			Name:                record.ClusterName,
			Version:             record.K3SVersion,
			RancherURL:          local.RancherURL,
			Namespace:           record.Namespace,
			ManagementClusterID: record.ManagementClusterID,
			KubeconfigPath:      record.KubeconfigPath,
			DownloadName:        safeKubeconfigDownloadName(record.ClusterName),
			Available:           record.KubeconfigPath != "" || record.ManagementClusterID != "",
			Provisioning:        true,
			ProvisioningMessage: fmt.Sprintf("Waiting for downstream discovery (%v)", discoverErr),
		}
		clusters = append(clusters, cluster)
	}
	return clusters
}

func discoverProvisioningDownstreamClusters(kubeconfigPath string) ([]discoveredDownstreamCluster, error) {
	output, err := runKubectl(kubeconfigPath, "get", "clusters.provisioning.cattle.io", "-A", "-o", "json")
	if err != nil {
		return nil, err
	}

	var list provisioningClusterList
	if err := json.Unmarshal([]byte(output), &list); err != nil {
		return nil, fmt.Errorf("failed to parse provisioning clusters: %w", err)
	}

	clusters := make([]discoveredDownstreamCluster, 0, len(list.Items))
	for _, item := range list.Items {
		name := strings.TrimSpace(item.Metadata.Name)
		namespace := strings.TrimSpace(item.Metadata.Namespace)
		if name == "" || namespace == "" {
			continue
		}
		if name == "local" || namespace == "local" {
			continue
		}
		clusters = append(clusters, discoveredDownstreamCluster{
			Name:                name,
			Namespace:           namespace,
			ManagementClusterID: strings.TrimSpace(item.Status.ClusterName),
		})
	}

	managementClusters, err := discoverManagementDownstreamClusters(kubeconfigPath)
	if err == nil {
		seenManagementIDs := map[string]bool{}
		for _, cluster := range clusters {
			if cluster.ManagementClusterID != "" {
				seenManagementIDs[cluster.ManagementClusterID] = true
			}
		}
		for _, cluster := range managementClusters {
			if seenManagementIDs[cluster.ManagementClusterID] {
				continue
			}
			clusters = append(clusters, cluster)
		}
	}

	sort.Slice(clusters, func(i, j int) bool {
		left := provisioningClusterRecordKey(clusters[i].Namespace, clusters[i].Name)
		right := provisioningClusterRecordKey(clusters[j].Namespace, clusters[j].Name)
		return left < right
	})
	return clusters, nil
}

func discoverManagementDownstreamClusters(kubeconfigPath string) ([]discoveredDownstreamCluster, error) {
	output, err := runKubectl(kubeconfigPath, "get", "clusters.management.cattle.io", "-o", "json")
	if err != nil {
		return nil, err
	}

	var list managementClusterList
	if err := json.Unmarshal([]byte(output), &list); err != nil {
		return nil, fmt.Errorf("failed to parse management clusters: %w", err)
	}

	clusters := make([]discoveredDownstreamCluster, 0, len(list.Items))
	for _, item := range list.Items {
		clusterID := strings.TrimSpace(item.Metadata.Name)
		if clusterID == "" || clusterID == "local" {
			continue
		}
		name := strings.TrimSpace(item.Spec.DisplayName)
		if name == "" {
			name = clusterID
		}
		clusters = append(clusters, discoveredDownstreamCluster{
			Name:                name,
			ManagementClusterID: clusterID,
		})
	}
	return clusters, nil
}

func downstreamRecordsByHA(records []downstreamOutputRecord) map[int][]downstreamOutputRecord {
	byHA := map[int][]downstreamOutputRecord{}
	for _, record := range records {
		byHA[record.HAIndex] = append(byHA[record.HAIndex], record)
	}
	return byHA
}

func downstreamRecordsByClusterKey(records []downstreamOutputRecord) map[string]downstreamOutputRecord {
	byName := map[string]downstreamOutputRecord{}
	for _, record := range records {
		key := provisioningClusterRecordKey(record.Namespace, record.ClusterName)
		byName[key] = record
	}
	return byName
}

func provisioningClusterRecordKey(namespace, name string) string {
	return strings.TrimSpace(namespace) + "/" + strings.TrimSpace(name)
}

func localClusterID(instanceNum int) string {
	return localClusterIDForRun("", instanceNum)
}

func localClusterIDForRun(runID string, instanceNum int) string {
	runID = safeRunPathSegment(runID)
	if runID != "" && runID != "unknown" {
		return fmt.Sprintf("run-%s-ha-%d-local", runID, instanceNum)
	}
	return fmt.Sprintf("ha-%d-local", instanceNum)
}

func hostedTenantClusterIDForRun(runID string, instanceNum int) string {
	role := "tenant"
	if instanceNum == 1 {
		role = "host"
	}
	runID = safeRunPathSegment(runID)
	if runID != "" && runID != "unknown" {
		return fmt.Sprintf("run-%s-hosted-%s-%d", runID, role, instanceNum)
	}
	return fmt.Sprintf("hosted-%s-%d", role, instanceNum)
}

func downstreamClusterID(instanceNum int, namespace, name string) string {
	return downstreamClusterIDForRun("", instanceNum, namespace, name)
}

func downstreamClusterIDForRun(runID string, instanceNum int, namespace, name string) string {
	namespacePart := sanitizeIDPart(namespace)
	namePart := sanitizeIDPart(name)
	prefix := fmt.Sprintf("ha-%d", instanceNum)
	runID = safeRunPathSegment(runID)
	if runID != "" && runID != "unknown" {
		prefix = fmt.Sprintf("run-%s-ha-%d", runID, instanceNum)
	}
	if namespacePart == "" {
		return fmt.Sprintf("%s-downstream-%s", prefix, namePart)
	}
	return fmt.Sprintf("%s-downstream-%s-%s", prefix, namespacePart, namePart)
}

func sanitizeIDPart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func safeKubeconfigDownloadName(clusterName string) string {
	name := sanitizeIDPart(clusterName)
	if name == "" {
		name = "downstream"
	}
	return name + ".yaml"
}

func (p *localControlPanel) pruneStaleDownstreamKubeconfigs(runID string, haIndex int, activeIDs map[string]bool) {
	prefix := fmt.Sprintf("ha-%d-downstream-", haIndex)
	runID = safeRunPathSegment(runID)
	if runID != "" && runID != "unknown" {
		prefix = fmt.Sprintf("run-%s-ha-%d-downstream-", runID, haIndex)
	}

	p.mu.Lock()
	for clusterID, path := range p.downstreamKubeconfigCache {
		if !strings.HasPrefix(clusterID, prefix) || activeIDs[clusterID] {
			continue
		}
		delete(p.downstreamKubeconfigCache, clusterID)
		RemoveFile(path)
	}
	p.mu.Unlock()

	cacheDir := filepath.Join(automationOutputDir(), "control-panel")
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) || filepath.Ext(name) != ".yaml" {
			continue
		}
		clusterID := strings.TrimSuffix(name, ".yaml")
		if activeIDs[clusterID] {
			continue
		}
		RemoveFile(filepath.Join(cacheDir, name))
	}
}

func fetchLocalRancherPods(kubeconfigPath string) ([]podView, error) {
	pods, err := fetchPods(kubeconfigPath, "cattle-system")
	if err != nil {
		return nil, err
	}

	filtered := make([]podView, 0, len(pods))
	for _, pod := range pods {
		nameLower := strings.ToLower(pod.Name)
		if !strings.Contains(nameLower, "rancher") && !strings.Contains(nameLower, "webhook") {
			continue
		}
		filtered = append(filtered, pod)
	}
	return filtered, nil
}

func fetchAllPods(kubeconfigPath string) ([]podView, error) {
	return fetchPods(kubeconfigPath, "")
}

func fetchRelevantPods(kubeconfigPath string) ([]podView, error) {
	return fetchLocalRancherPods(kubeconfigPath)
}

func fetchPods(kubeconfigPath, namespace string) ([]podView, error) {
	args := []string{"get", "pods"}
	if namespace == "" {
		args = append(args, "-A")
	} else {
		args = append(args, "-n", namespace)
	}
	args = append(args, "-o", "json")

	output, err := runKubectl(kubeconfigPath, args...)
	if err != nil {
		return nil, err
	}

	var list kubectlPodList
	if err := json.Unmarshal([]byte(output), &list); err != nil {
		return nil, fmt.Errorf("failed to parse pod list: %w", err)
	}

	leaderLabels := discoverLeaderLabels(kubeconfigPath)

	pods := make([]podView, 0)
	for _, item := range list.Items {
		totalContainers := len(item.Spec.Containers)
		readyContainers := 0
		restarts := 0
		status := item.Status.Phase
		for _, containerStatus := range item.Status.ContainerStatuses {
			if containerStatus.Ready {
				readyContainers++
			}
			restarts += containerStatus.RestartCount
			if containerStatus.State.Waiting.Reason != "" {
				status = containerStatus.State.Waiting.Reason
			}
			if containerStatus.State.Terminated.Reason != "" {
				status = containerStatus.State.Terminated.Reason
			}
		}
		if item.Status.Reason != "" {
			status = item.Status.Reason
		}

		containerNames := make([]string, 0, len(item.Spec.Containers))
		for _, container := range item.Spec.Containers {
			containerNames = append(containerNames, container.Name)
		}

		leaderLabel := leaderLabels[item.Metadata.Name]
		pods = append(pods, podView{
			Namespace:   item.Metadata.Namespace,
			Name:        item.Metadata.Name,
			Ready:       fmt.Sprintf("%d/%d", readyContainers, totalContainers),
			Status:      status,
			Restarts:    restarts,
			Age:         humanDurationSince(item.Metadata.CreationTimestamp),
			Node:        item.Spec.NodeName,
			Containers:  strings.Join(containerNames, ", "),
			Leader:      leaderLabel != "",
			LeaderLabel: leaderLabel,
		})
	}

	sort.Slice(pods, func(i, j int) bool {
		return pods[i].Name < pods[j].Name
	})

	return pods, nil
}

func discoverLeaderLabels(kubeconfigPath string) map[string]string {
	leaders := map[string]string{}
	if holder, err := leaseHolderIdentity(kubeconfigPath, "kube-system", "cattle-controllers"); err == nil && holder != "" {
		leaders[holder] = "Leader"
	}
	if holder, err := leaseHolderIdentity(kubeconfigPath, "cattle-system", "rancher-webhook-leader"); err == nil && holder != "" {
		leaders[holder] = "Webhook Leader"
	}
	return leaders
}

func leaseHolderIdentity(kubeconfigPath, namespace, name string) (string, error) {
	output, err := runKubectl(kubeconfigPath, "get", "lease", name, "-n", namespace, "-o", "json")
	if err != nil {
		return "", err
	}

	var lease struct {
		Spec struct {
			HolderIdentity string `json:"holderIdentity"`
		} `json:"spec"`
	}
	if err := json.Unmarshal([]byte(output), &lease); err != nil {
		return "", fmt.Errorf("failed to parse %s/%s lease: %w", namespace, name, err)
	}

	return strings.TrimSpace(lease.Spec.HolderIdentity), nil
}

func humanDurationSince(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	d := time.Since(ts)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func runKubectl(kubeconfigPath string, args ...string) (string, error) {
	cmd := exec.Command("kubectl", append([]string{"--kubeconfig", kubeconfigPath, "--request-timeout=5s"}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("kubectl %s failed: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func readTerraformFlatOutputs(repoRoot string) (map[string]string, error) {
	return readTerraformFlatOutputsWithState(repoRoot, "", "")
}

func (p *localControlPanel) readTerraformFlatOutputs() (map[string]string, error) {
	record, ok := p.readCurrentRunRecord()
	if !ok {
		return readTerraformFlatOutputs(p.repoRoot)
	}
	return readTerraformFlatOutputsWithModule(p.repoRoot, record.TerraformStatePath, record.TerraformDataDir, record.TerraformModuleDir)
}

func readTerraformFlatOutputsWithState(repoRoot string, statePath string, dataDir string) (map[string]string, error) {
	return readTerraformFlatOutputsWithModule(repoRoot, statePath, dataDir, "")
}

func readTerraformFlatOutputsWithModule(repoRoot string, statePath string, dataDir string, moduleDir string) (map[string]string, error) {
	args := []string{"output", "-no-color", "-json"}
	if strings.TrimSpace(statePath) != "" && pathExists(statePath) {
		args = append(args, "-state="+statePath)
	}
	args = append(args, "flat_outputs")
	cmd := exec.Command("terraform", args...)
	cmd.Dir = strings.TrimSpace(moduleDir)
	if cmd.Dir == "" {
		cmd.Dir = filepath.Join(repoRoot, "modules", "aws")
	}
	if strings.TrimSpace(dataDir) != "" {
		cmd.Env = append(os.Environ(), "TF_DATA_DIR="+dataDir)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("terraform output failed: %w (%s)", err, strings.TrimSpace(string(output)))
	}

	var outputs map[string]string
	if err := json.Unmarshal(output, &outputs); err != nil {
		return nil, fmt.Errorf("failed to parse terraform outputs: %w", err)
	}
	return outputs, nil
}

func readRequestedRancherVersionsForPanel(totalHAs int) []string {
	versions := viper.GetStringSlice("rancher.versions")
	if len(versions) == totalHAs {
		out := make([]string, 0, len(versions))
		for _, version := range versions {
			out = append(out, normalizeVersionInput(version))
		}
		return out
	}

	version := normalizeVersionInput(viper.GetString("rancher.version"))
	if version == "" {
		return nil
	}
	if totalHAs == 1 {
		return []string{version}
	}

	out := make([]string, totalHAs)
	for i := range out {
		out[i] = version
	}
	return out
}

func (p *localControlPanel) logRequest(r *http.Request) (clusterView, string, string, string, error) {
	clusterID := strings.TrimSpace(r.URL.Query().Get("cluster"))
	pod := strings.TrimSpace(r.URL.Query().Get("pod"))
	namespace := strings.TrimSpace(r.URL.Query().Get("namespace"))
	container := strings.TrimSpace(r.URL.Query().Get("container"))
	if clusterID == "" || pod == "" {
		return clusterView{}, "", "", "", fmt.Errorf("cluster and pod are required")
	}
	if namespace == "" {
		namespace = "cattle-system"
	}

	for _, cluster := range p.discoverClusters() {
		if cluster.ID == clusterID {
			if !cluster.Available {
				return clusterView{}, "", "", "", fmt.Errorf("cluster is not available")
			}
			if !cluster.Reachable {
				return clusterView{}, "", "", "", fmt.Errorf("cluster is not reachable")
			}
			return cluster, pod, namespace, container, nil
		}
	}

	return clusterView{}, "", "", "", fmt.Errorf("cluster %s not found", clusterID)
}

func (p *localControlPanel) clusterByID(clusterID string) (clusterView, error) {
	for _, cluster := range p.discoverClusters() {
		if cluster.ID == clusterID {
			return cluster, nil
		}
	}
	return clusterView{}, fmt.Errorf("cluster %s not found", clusterID)
}

func (p *localControlPanel) kubeconfigContentForDownload(cluster clusterView) ([]byte, string, error) {
	filename := strings.TrimSpace(cluster.DownloadName)
	if filename == "" {
		filename = "kubeconfig.yaml"
	}

	switch cluster.Type {
	case "local":
		if cluster.KubeconfigPath == "" {
			return nil, "", fmt.Errorf("local kubeconfig path is unavailable")
		}
		data, err := os.ReadFile(cluster.KubeconfigPath)
		if err != nil {
			return nil, "", fmt.Errorf("failed to read local kubeconfig: %w", err)
		}
		return data, filename, nil
	case "downstream":
		if cluster.ManagementClusterID == "" {
			return nil, "", fmt.Errorf("downstream cluster has no management cluster id yet")
		}
		token, err := p.rancherToken(cluster.HAIndex, cluster.RancherURL)
		if err != nil {
			return nil, "", err
		}
		kubeconfig, err := generateRancherKubeconfig(cluster.RancherURL, token, cluster.ManagementClusterID)
		if err != nil {
			return nil, "", err
		}
		return []byte(kubeconfig), filename, nil
	default:
		return nil, "", fmt.Errorf("unsupported cluster type %q", cluster.Type)
	}
}

func (p *localControlPanel) helmCommandForCluster(cluster clusterView) (string, error) {
	if cluster.Type != "local" {
		return "", fmt.Errorf("Helm install command is only available for local HA clusters")
	}
	if cluster.KubeconfigPath == "" {
		return "", fmt.Errorf("local kubeconfig path is unavailable")
	}

	installScriptPath := filepath.Join(filepath.Dir(cluster.KubeconfigPath), "install.sh")
	data, err := os.ReadFile(installScriptPath)
	if err != nil {
		return "", fmt.Errorf("failed to read install script: %w", err)
	}

	command, err := extractHelmCommandFromInstallScript(string(data))
	if err != nil {
		return "", fmt.Errorf("failed to extract Helm command from %s: %w", installScriptPath, err)
	}
	return command, nil
}

func extractHelmCommandFromInstallScript(script string) (string, error) {
	lines := strings.Split(script, "\n")
	var command []string
	capturing := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !capturing {
			if trimmed == `echo "Installing Rancher..."` {
				capturing = true
			}
			continue
		}
		if trimmed == "" {
			if len(command) == 0 {
				continue
			}
			break
		}
		if strings.HasPrefix(trimmed, "echo ") {
			break
		}
		command = append(command, line)
	}

	result := strings.TrimSpace(strings.Join(command, "\n"))
	if result == "" {
		return "", fmt.Errorf("no Helm install command found")
	}
	if !strings.HasPrefix(strings.TrimSpace(result), "helm ") {
		return "", fmt.Errorf("install command does not start with helm")
	}
	return result, nil
}

func prepareHelmUpgradeCommand(command string) (string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", fmt.Errorf("Helm command is empty")
	}

	fields, err := parseHelmCommandFields(command)
	if err != nil {
		return "", err
	}
	if len(fields) < 2 || fields[0] != "helm" {
		return "", fmt.Errorf("command does not start with helm")
	}
	switch fields[1] {
	case "install":
		return rewriteFirstHelmOperation(command, "helm install ", "helm upgrade --install "), nil
	case "upgrade":
		for _, field := range fields[2:] {
			if field == "--install" {
				return command, nil
			}
		}
		return rewriteFirstHelmOperation(command, "helm upgrade ", "helm upgrade --install "), nil
	default:
		return "", fmt.Errorf("command must use helm install or helm upgrade")
	}
}

func rewriteFirstHelmOperation(command, from, to string) string {
	lines := strings.Split(command, "\n")
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, from) {
			prefix := line[:len(line)-len(trimmed)]
			lines[i] = prefix + strings.Replace(trimmed, from, to, 1)
			return strings.TrimSpace(strings.Join(lines, "\n"))
		}
	}
	return command
}

func saveDownloadFile(filename string, content []byte, perm os.FileMode) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to find home directory: %w", err)
	}

	downloadsDir := filepath.Join(home, "Downloads")
	if err := os.MkdirAll(downloadsDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create Downloads directory: %w", err)
	}

	path := uniqueDownloadPath(downloadsDir, filename)
	if err := os.WriteFile(path, content, perm); err != nil {
		return "", fmt.Errorf("failed to save kubeconfig to Downloads: %w", err)
	}
	return path, nil
}

func uniqueDownloadPath(dir, filename string) string {
	filename = safeDownloadFilename(filename)
	candidate := filepath.Join(dir, filename)
	if _, err := os.Stat(candidate); os.IsNotExist(err) {
		return candidate
	}

	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)
	for i := 1; ; i++ {
		candidate = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

func safeDownloadFilename(filename string) string {
	filename = filepath.Base(strings.TrimSpace(filename))
	filename = strings.NewReplacer("/", "-", "\\", "-", ":", "-").Replace(filename)
	if filename == "." || filename == string(filepath.Separator) || filename == "" {
		return "kubeconfig.yaml"
	}
	return filename
}

func openExternalURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || u == nil || u.Host == "" {
		return fmt.Errorf("invalid URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("only http and https URLs can be opened")
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", rawURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	default:
		cmd = exec.Command("xdg-open", rawURL)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to open browser: %w", err)
	}
	return nil
}

func (p *localControlPanel) resolveAllowedLocalPath(rawPath string) (string, error) {
	path := strings.TrimSpace(rawPath)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(p.repoRoot, path)
	}
	path = filepath.Clean(path)

	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("path is unavailable")
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("symlink paths cannot be opened from the panel")
	}

	for _, root := range p.allowedLocalPathRoots() {
		if pathWithinRoot(root, path) {
			return path, nil
		}
	}
	return "", fmt.Errorf("path is outside this checkout's local run artifacts")
}

func (p *localControlPanel) allowedLocalPathRoots() []string {
	roots := []string{p.repoRoot, p.testDir}
	if outputRoot, err := absoluteFromWorkingDir(automationOutputDir()); err == nil {
		roots = append(roots, outputRoot)
	}

	var result []string
	seen := map[string]bool{}
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		absRoot, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		absRoot = filepath.Clean(absRoot)
		if seen[absRoot] {
			continue
		}
		seen[absRoot] = true
		result = append(result, absRoot)
	}
	return result
}

func pathWithinRoot(root, path string) bool {
	root, rootErr := filepath.Abs(filepath.Clean(root))
	path, pathErr := filepath.Abs(filepath.Clean(path))
	if rootErr != nil || pathErr != nil {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..")
}

func openLocalPath(path string, reveal bool) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		if reveal {
			cmd = exec.Command("open", "-R", path)
		} else {
			cmd = exec.Command("open", path)
		}
	case "windows":
		if reveal {
			cmd = exec.Command("explorer", "/select,"+path)
		} else {
			cmd = exec.Command("explorer", path)
		}
	default:
		openPath := path
		if reveal {
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				openPath = filepath.Dir(path)
			}
		}
		cmd = exec.Command("xdg-open", openPath)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to open local path: %w", err)
	}
	return nil
}

func (p *localControlPanel) ensureDownstreamKubeconfig(haIndex int, rancherURL, clusterKey, managementClusterID, existingPath string) (string, error) {
	if existingPath != "" {
		if _, err := os.Stat(existingPath); err == nil {
			return existingPath, nil
		}
	}

	p.mu.Lock()
	if path := p.downstreamKubeconfigCache[clusterKey]; path != "" {
		if _, err := os.Stat(path); err == nil {
			p.mu.Unlock()
			return path, nil
		}
	}
	p.mu.Unlock()

	token, err := p.rancherToken(haIndex, rancherURL)
	if err != nil {
		return "", err
	}
	kubeconfig, err := generateRancherKubeconfig(rancherURL, token, managementClusterID)
	if err != nil {
		return "", err
	}

	cacheDir := filepath.Join(automationOutputDir(), "control-panel")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(cacheDir, clusterKey+".yaml")
	if err := os.WriteFile(path, []byte(kubeconfig), 0o600); err != nil {
		return "", err
	}

	p.mu.Lock()
	p.downstreamKubeconfigCache[clusterKey] = path
	p.mu.Unlock()
	return path, nil
}

func (p *localControlPanel) rancherToken(haIndex int, rancherURL string) (string, error) {
	p.mu.Lock()
	if token := p.rancherTokens[haIndex]; token != "" {
		p.mu.Unlock()
		return token, nil
	}
	p.mu.Unlock()

	token, err := createRancherAdminToken(rancherURL, viper.GetString("rancher.bootstrap_password"))
	if err != nil {
		return "", err
	}

	p.mu.Lock()
	p.rancherTokens[haIndex] = token
	p.mu.Unlock()
	return token, nil
}

func (p *localControlPanel) authorized(r *http.Request) bool {
	if p.trustedLocalOrigin {
		return true
	}
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		token = strings.TrimSpace(r.Header.Get("X-Control-Panel-Token"))
	}
	return token != "" && token == p.token
}

func (p *localControlPanel) authorizedReadOnly(r *http.Request) bool {
	return p.authorized(r) || requestFromLoopback(r)
}

func (p *localControlPanel) authorizedLocalBrowserRead(r *http.Request) bool {
	return p.authorized(r) || (requestFromLoopback(r) && sameOriginBrowserRequest(r))
}

func (p *localControlPanel) authorizedLocalAction(r *http.Request) bool {
	return p.authorized(r) || (requestFromLoopback(r) && sameOriginBrowserRequest(r))
}

func requestFromLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		host = strings.TrimSpace(r.RemoteAddr)
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func sameOriginBrowserRequest(r *http.Request) bool {
	if !sameOriginHeaderHost(r.Header.Get("Origin"), r.Host) {
		return sameOriginHeaderHost(r.Header.Get("Referer"), r.Host)
	}
	return true
}

func sameOriginHeaderHost(rawValue, requestHost string) bool {
	rawValue = strings.TrimSpace(rawValue)
	if rawValue == "" {
		return false
	}

	u, err := url.Parse(rawValue)
	if err != nil {
		return false
	}

	return strings.EqualFold(u.Host, requestHost)
}

func resolveControlPanelPaths(startDir string) (repoRoot string, testDir string, err error) {
	current := filepath.Clean(startDir)
	for {
		goModPath := filepath.Join(current, "go.mod")
		terratestDir := filepath.Join(current, "terratest")
		if fileExists(goModPath) && dirExists(terratestDir) {
			return current, terratestDir, nil
		}

		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	return "", "", fmt.Errorf("failed to locate repository root from %s", startDir)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func writeJSON(w http.ResponseWriter, value interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(value)
}
