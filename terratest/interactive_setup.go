package test

import (
	"context"
	"errors"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/terratest/settings"
	"github.com/spf13/viper"
	"html/template"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

type interactivePhase string

const (
	phaseEditor    interactivePhase = "editor"
	phaseResolving interactivePhase = "resolving"
	phaseReview    interactivePhase = "review"
	phaseDone      interactivePhase = "done"
)

type interactiveEvent struct {
	Type  string           `json:"type"`
	Phase interactivePhase `json:"phase,omitempty"`
	Line  string           `json:"line,omitempty"`
	Plan  string           `json:"plan,omitempty"`
	Error string           `json:"error,omitempty"`
}

type interactiveSetupSnapshot struct {
	Phase     interactivePhase `json:"phase"`
	Logs      []string         `json:"logs"`
	Plan      string           `json:"plan,omitempty"`
	Error     string           `json:"error,omitempty"`
	Submitted bool             `json:"submitted"`
}

type interactiveResult struct {
	plans []*RancherResolvedPlan
	err   error
}

type interactiveSetupState struct {
	Token                 string                           `json:"token"`
	BasePath              string                           `json:"basePath,omitempty"`
	ConfigPath            string                           `json:"configPath"`
	DeploymentType        string                           `json:"deploymentType"`
	Mode                  string                           `json:"mode"`
	Versions              []string                         `json:"versions"`
	AgentImages           []string                         `json:"agentImages"`
	HelmCommands          []string                         `json:"helmCommands"`
	K8SVersions           []string                         `json:"k8sVersions"`
	InstallerSHA256s      []string                         `json:"installerSHA256s"`
	ResolveInstallerSHA   bool                             `json:"resolveInstallerSHA"`
	Config                settings.EditablePreflightConfig `json:"config"`
	CustomHostnameEnabled bool                             `json:"customHostnameEnabled"`
	CustomHostname        string                           `json:"customHostname"`
	Embedded              bool                             `json:"embedded,omitempty"`
}

type interactiveSetupTemplateData struct {
	Token            string
	BuildLabel       string
	BasePath         string
	ConfigPath       string
	Embedded         bool
	InitialStateJSON template.JS
}

type interactiveServer struct {
	token      string
	configPath string

	mu          sync.Mutex
	configMu    sync.Mutex
	phase       interactivePhase
	logs        []string
	planText    string
	resolveErr  string
	plans       []*RancherResolvedPlan
	subscribers []chan interactiveEvent
	submitted   bool

	resultCh        chan interactiveResult
	responseHandler func(action string, plans []*RancherResolvedPlan) error
	configImporter  func([]byte, string) (string, error)
}

func resolveRancherSetup() ([]*RancherResolvedPlan, error) {
	mode := rancherMode()
	autoApprove := viper.GetBool("rancher.auto_approve") || panelNonInteractiveMode()

	if mode == "auto" && !autoApprove {
		return runInteractiveAutoModeSetup()
	}

	totalHAs := configuredRancherInstanceCount()
	if totalHAs < 1 {
		return nil, fmt.Errorf("configured Rancher instance count must be at least 1")
	}
	if !isHostedTenantK3SDeployment() && !isLinodeDockerDeployment() {
		if err := settings.ValidateCustomHostnameConfig(totalHAs); err != nil {
			return nil, err
		}
	}

	var plans []*RancherResolvedPlan
	var err error
	if isHostedTenantK3SDeployment() {
		plans, err = prepareHostedTenantRancherConfiguration(totalHAs)
	} else if isLinodeDockerDeployment() {
		plans, err = prepareLinodeDockerPlans(totalHAs)
	} else {
		plans, err = prepareRancherConfiguration(totalHAs)
	}
	if err != nil {
		return nil, err
	}
	if mode == "auto" {
		logResolvedPlans(plans)
		if autoApprove {
			log.Printf("[resolver] Auto-approve enabled, continuing without prompt")
		}
	}
	return plans, nil
}

func panelNonInteractiveMode() bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(panelNonInteractiveEnv)))
	return value == "1" || value == "true" || value == "yes"
}

func runInteractiveAutoModeSetup() ([]*RancherResolvedPlan, error) {
	configPath := strings.TrimSpace(viper.ConfigFileUsed())
	if configPath == "" {
		return nil, fmt.Errorf("failed to determine tool-config.yml path for interactive setup")
	}

	versions := currentPreflightVersions()
	for len(versions) < 1 {
		versions = append(versions, "")
	}

	token, err := randomConfirmationToken()
	if err != nil {
		return nil, fmt.Errorf("failed to create interactive setup token: %w", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to start interactive setup listener: %w", err)
	}

	srv := &interactiveServer{
		token:      token,
		configPath: configPath,
		phase:      phaseEditor,
		resultCh:   make(chan interactiveResult, 1),
	}

	mux := http.NewServeMux()
	srv.registerHandlers(mux, versions)

	server := &http.Server{Handler: mux}
	serverErrCh := make(chan error, 1)
	go func() {
		if serveErr := server.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			serverErrCh <- serveErr
		}
	}()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	setupURL := fmt.Sprintf("http://%s/?token=%s", listener.Addr().String(), token)
	if err := openBrowser(setupURL); err != nil {
		return nil, fmt.Errorf("failed to open interactive setup page: %w", err)
	}
	log.Printf("[setup] Opened interactive setup at %s", setupURL)

	select {
	case result := <-srv.resultCh:
		srv.broadcast(interactiveEvent{Type: "phase", Phase: phaseDone})
		return result.plans, result.err
	case serveErr := <-serverErrCh:
		return nil, fmt.Errorf("interactive setup server failed: %w", serveErr)
	case <-time.After(45 * time.Minute):
		return nil, fmt.Errorf("timed out waiting for interactive setup response")
	}
}
