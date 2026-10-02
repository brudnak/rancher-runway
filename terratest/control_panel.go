package test

import (
	"context"
	"errors"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/cachelab"
	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	"github.com/brudnak/ha-rancher-rke2/internal/linodeinventory"
	"github.com/brudnak/ha-rancher-rke2/internal/prbuild"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type localControlPanel struct {
	linodeInventory      linodeinventory.Service
	token                string
	sessionID            string
	startedAt            time.Time
	totalHAs             int
	repoRoot             string
	testDir              string
	configPath           string
	readinessCollector   func(string) systemReadinessState
	starterConfigCreated bool
	trustedLocalOrigin   bool
	listener             net.Listener
	server               *http.Server
	baseURL              string
	doneCh               chan error

	// Includes final record writes and chained completion callbacks, not just the subprocess.
	workers                 panelWorkers
	historyDiscovery        panelDiscoverySnapshot[struct{}]
	lifecycleWorkers        sync.WaitGroup
	mu                      sync.Mutex
	operations              map[panelOperationName]*panelOperationState
	awsMu                   sync.Mutex
	awsCache                panelAWSInventoryState
	awsCacheKey             string
	awsCleanupPlan          *awsCleanupPlan
	awsCleanupResults       []awsCleanupResult
	awsCleanupClientFactory func(context.Context, string) (awsCleanupClient, error)
	setupEditor             *interactiveServer
	imageLookup             *imagelookup.Service
	prBuildVerifier         *prbuild.Service
	issueRadar              *issueRadarService
	dailyReadiness          *dailyReadinessService
	helmCatalog             helmLabCatalogService
	rancherOps              rancherOperations
	cacheLabMu              sync.Mutex
	cacheLab                *cachelab.Service
	testLabMu               sync.Mutex
	testLab                 *testLabService
	testPackagesMu          sync.Mutex
	testPackages            *testPackageService
	testPackageEnvironments map[string]testPackageEnvironment // guarded by mu; never rewrites preserved sessions
	historyMu               sync.Mutex
	historyHashes           map[string]string
	historyProbes           map[string]time.Time
	clusterWorkspacesMu     sync.Mutex
	clusterWorkspaces       map[string]clusterWorkspaceRecord

	// cleanupBatchRunner is nil in production. Tests may replace it with a
	// deterministic runner so the batch coordinator can be exercised without
	// launching Terraform.
	cleanupBatchRunner func(string) error

	rancherTokens             map[int]string
	downstreamKubeconfigCache map[string]string
	clusterSnapshot           map[string]clusterView
	clusterDiscovery          panelDiscoverySnapshot[panelClusterState]
	awsDiscovery              panelDiscoverySnapshot[panelAWSInventoryState]
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

	if err := migrateDurableData(); err != nil {
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
		clusterSnapshot:           map[string]clusterView{},
	}
	panel.setupEditor = panel.newSetupEditor()
	panel.loadPersistedOperations(true)
	panel.server = &http.Server{Handler: panel.handler()}
	return panel, nil
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
	return s != nil && s.panel != nil && (s.panel.anyOperationRunning() || s.panel.testLabRunning())
}

func (s *ControlPanelServer) RunningOperation() string {
	if s == nil || s.panel == nil {
		return ""
	}
	if s.panel.testLabRunning() {
		return "test lab"
	}
	s.panel.mu.Lock()
	defer s.panel.mu.Unlock()
	if !s.panel.anyOperationRunningLocked() {
		return ""
	}
	return s.panel.runningOperationNameLocked()
}

func (s *ControlPanelServer) GPUInfrastructure() GPUInfrastructureSummary {
	if s == nil || s.panel == nil {
		return GPUInfrastructureSummary{}
	}
	return s.panel.gpuInfrastructure()
}

func (s *ControlPanelServer) Wait() error {
	if s == nil || s.panel == nil {
		return nil
	}
	err := s.panel.wait()
	return errors.Join(err, s.Shutdown(context.Background()))
}

func (s *ControlPanelServer) Shutdown(ctx context.Context) error {
	if s == nil || s.panel == nil {
		return nil
	}
	s.panel.workers.Stop()
	err := s.panel.server.Shutdown(ctx)
	if stopErr := s.panel.stopWorkers(ctx); stopErr != nil {
		return stopErr
	}
	if err != nil {
		return err
	}
	s.cleanup()
	return nil
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
