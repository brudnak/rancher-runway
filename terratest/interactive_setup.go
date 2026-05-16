package test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/brudnak/ha-rancher-rke2/terratest/settings"
	"github.com/brudnak/ha-rancher-rke2/terratest/ui"
	"github.com/spf13/viper"
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
	Mode                  string                           `json:"mode"`
	Versions              []string                         `json:"versions"`
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
	BasePath         string
	ConfigPath       string
	Embedded         bool
	InitialStateJSON template.JS
}

type interactiveServer struct {
	token      string
	configPath string

	mu          sync.Mutex
	phase       interactivePhase
	logs        []string
	planText    string
	resolveErr  string
	plans       []*RancherResolvedPlan
	subscribers []chan interactiveEvent
	submitted   bool

	resultCh        chan interactiveResult
	responseHandler func(action string, plans []*RancherResolvedPlan) error
}

func resolveRancherSetup() ([]*RancherResolvedPlan, error) {
	mode := rancherMode()
	autoApprove := viper.GetBool("rancher.auto_approve") || panelNonInteractiveMode()

	if mode == "auto" && !autoApprove {
		return runInteractiveAutoModeSetup()
	}

	totalHAs := viper.GetInt("total_has")
	if totalHAs < 1 {
		return nil, fmt.Errorf("total_has must be at least 1")
	}
	if err := settings.ValidateCustomHostnameConfig(totalHAs); err != nil {
		return nil, err
	}

	plans, err := prepareRancherConfiguration(totalHAs)
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

func (s *interactiveServer) registerHandlers(mux *http.ServeMux, initialVersions []string) {
	s.registerHandlersAt(mux, initialVersions, "")
}

func (s *interactiveServer) registerHandlersAt(mux *http.ServeMux, initialVersions []string, basePath string) {
	basePath = normalizeInteractiveBasePath(basePath)
	templateData := interactiveSetupTemplateDataFor(s.token, s.configPath, initialVersions, basePath, false)
	pageTemplate := template.Must(template.New("interactive-setup").Parse(ui.InteractiveSetupHTML))

	mux.HandleFunc(interactiveSetupPath(basePath, "/static/interactive_setup.js"), func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			http.Error(w, "invalid interactive setup token", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(ui.InteractiveSetupJS))
	})

	handlePage := func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			http.Error(w, "invalid interactive setup token", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_ = pageTemplate.Execute(w, templateData)
	}
	mux.HandleFunc(interactiveSetupPath(basePath, "/"), handlePage)
	if basePath != "" {
		mux.HandleFunc(basePath, handlePage)
	}

	mux.HandleFunc(interactiveSetupPath(basePath, "/api/readiness"), func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			http.Error(w, "invalid interactive setup token", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		writeJSON(w, collectSystemReadiness(s.configPath))
	})

	mux.HandleFunc(interactiveSetupPath(basePath, "/api/validate-helm"), func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			http.Error(w, "invalid interactive setup token", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed: "+r.Method, http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			HelmCommands []string `json:"helmCommands"`
			K8SVersions  []string `json:"k8sVersions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]any{
			"results": validateManualHelmCommandsForPlanning(req.HelmCommands, req.K8SVersions),
		})
	})

	mux.HandleFunc(interactiveSetupPath(basePath, "/api/recommend-rke2"), func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			http.Error(w, "invalid interactive setup token", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed: "+r.Method, http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			HelmCommands []string `json:"helmCommands"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]any{
			"results": recommendManualRKE2Versions(req.HelmCommands),
		})
	})

	mux.HandleFunc(interactiveSetupPath(basePath, "/submit"), func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			http.Error(w, "invalid interactive setup token", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed: "+r.Method, http.StatusMethodNotAllowed)
			return
		}

		readiness := collectSystemReadiness(s.configPath)
		if !readiness.Ready {
			http.Error(w, readiness.Summary, http.StatusBadRequest)
			return
		}

		req, err := decodePreflightConfigUpdateRequest(r)
		if err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		req.Mode = strings.ToLower(strings.TrimSpace(req.Mode))
		if req.Mode == "" {
			req.Mode = "auto"
		}
		if req.Mode == "auto" {
			normalizedVersions, err := normalizePreflightVersions(req.Versions)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			req.Versions = normalizedVersions
		} else if req.Mode == "manual" {
			for _, result := range validateManualHelmCommandsForPlanning(req.HelmCommands, req.K8SVersions) {
				if !result.OK {
					message := fmt.Sprintf("Helm command for HA %d failed validation: %s", result.Index+1, result.Summary)
					if strings.TrimSpace(result.Detail) != "" {
						message += ": " + result.Detail
					}
					http.Error(w, message, http.StatusBadRequest)
					return
				}
			}
		}

		if err := updateAutoModeConfigFile(s.configPath, req); err != nil {
			http.Error(w, fmt.Sprintf("failed to update tool-config.yml: %v", err), http.StatusInternalServerError)
			return
		}

		s.mu.Lock()
		if s.submitted {
			s.mu.Unlock()
			writeJSON(w, map[string]string{"status": "already_running"})
			return
		}
		s.submitted = true
		s.phase = phaseResolving
		s.logs = nil
		s.planText = ""
		s.resolveErr = ""
		s.plans = nil
		s.mu.Unlock()

		s.broadcast(interactiveEvent{Type: "phase", Phase: phaseResolving})
		writeJSON(w, map[string]string{"status": "resolving"})

		go s.runResolution()
	})

	mux.HandleFunc(interactiveSetupPath(basePath, "/state"), func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			http.Error(w, "invalid interactive setup token", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		s.mu.Lock()
		snapshot := interactiveSetupSnapshot{
			Phase:     s.phase,
			Logs:      append([]string(nil), s.logs...),
			Plan:      s.planText,
			Error:     s.resolveErr,
			Submitted: s.submitted,
		}
		s.mu.Unlock()

		writeJSON(w, snapshot)
	})

	mux.HandleFunc(interactiveSetupPath(basePath, "/respond"), func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			http.Error(w, "invalid interactive setup token", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "failed to parse form", http.StatusBadRequest)
			return
		}

		action := r.FormValue("action")
		shouldContinue := action == "continue"
		s.mu.Lock()
		plans := s.plans
		s.mu.Unlock()

		if s.responseHandler != nil {
			if err := s.responseHandler(action, plans); err != nil {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}

			s.mu.Lock()
			s.phase = phaseEditor
			s.logs = nil
			s.planText = ""
			s.resolveErr = ""
			s.plans = nil
			s.submitted = false
			s.mu.Unlock()

			s.broadcast(interactiveEvent{Type: "phase", Phase: phaseEditor})
			writeJSON(w, map[string]string{
				"status": action,
			})
			return
		}

		s.mu.Lock()
		s.phase = phaseDone
		s.mu.Unlock()

		s.broadcast(interactiveEvent{Type: "phase", Phase: phaseDone})

		writeJSON(w, map[string]string{
			"status": action,
		})

		if s.resultCh != nil {
			select {
			case s.resultCh <- func() interactiveResult {
				if shouldContinue {
					return interactiveResult{plans: plans, err: nil}
				}
				return interactiveResult{plans: nil, err: fmt.Errorf("user canceled interactive Rancher setup")}
			}():
			default:
			}
		}
	})

	mux.HandleFunc(interactiveSetupPath(basePath, "/events"), func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			http.Error(w, "invalid interactive setup token", http.StatusForbidden)
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		s.mu.Lock()
		phase := s.phase
		logsCopy := append([]string(nil), s.logs...)
		planText := s.planText
		resolveErr := s.resolveErr
		sub := make(chan interactiveEvent, 256)
		s.subscribers = append(s.subscribers, sub)
		s.mu.Unlock()
		defer s.removeSubscriber(sub)

		writeSSE(w, flusher, interactiveEvent{Type: "phase", Phase: phase})
		for _, line := range logsCopy {
			writeSSE(w, flusher, interactiveEvent{Type: "log", Line: line})
		}
		if planText != "" {
			writeSSE(w, flusher, interactiveEvent{Type: "plan", Plan: planText})
		}
		if resolveErr != "" {
			writeSSE(w, flusher, interactiveEvent{Type: "error", Error: resolveErr})
		}

		heartbeat := time.NewTicker(15 * time.Second)
		defer heartbeat.Stop()

		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-sub:
				if !ok {
					return
				}
				writeSSE(w, flusher, ev)
				if ev.Type == "phase" && ev.Phase == phaseDone {
					return
				}
			case <-heartbeat.C:
				fmt.Fprint(w, ": heartbeat\n\n")
				flusher.Flush()
			}
		}
	})
}

func interactiveSetupTemplateDataFor(token string, configPath string, initialVersions []string, basePath string, embedded bool) interactiveSetupTemplateData {
	initialCustomHostname := settings.CurrentCustomHostnamePrefix()
	initialStateJSON, _ := json.Marshal(interactiveSetupState{
		Token:                 token,
		BasePath:              normalizeInteractiveBasePath(basePath),
		ConfigPath:            configPath,
		Mode:                  rancherMode(),
		Versions:              initialVersions,
		HelmCommands:          currentManualHelmCommands(),
		K8SVersions:           currentManualK8SVersions(),
		InstallerSHA256s:      currentManualInstallerSHA256s(),
		ResolveInstallerSHA:   currentManualResolveInstallerSHA(),
		Config:                settings.CurrentEditablePreflightConfig(),
		CustomHostnameEnabled: initialCustomHostname != "",
		CustomHostname:        initialCustomHostname,
		Embedded:              embedded,
	})

	return interactiveSetupTemplateData{
		Token:            token,
		BasePath:         normalizeInteractiveBasePath(basePath),
		ConfigPath:       configPath,
		Embedded:         embedded,
		InitialStateJSON: template.JS(string(initialStateJSON)),
	}
}

func currentManualHelmCommands() []string {
	commands := viper.GetStringSlice("rancher.helm_commands")
	if len(commands) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(commands))
	for _, command := range commands {
		out = append(out, strings.TrimSpace(command))
	}
	return out
}

func currentManualK8SVersions() []string {
	versions := viper.GetStringSlice("k8s.versions")
	if len(versions) > 0 {
		out := make([]string, 0, len(versions))
		for _, version := range versions {
			out = append(out, strings.TrimSpace(version))
		}
		return out
	}
	if version := strings.TrimSpace(viper.GetString("k8s.version")); version != "" {
		return []string{version}
	}
	return []string{}
}

func currentManualInstallerSHA256s() []string {
	versions := currentManualK8SVersions()
	checksums := viper.GetStringMapString("rke2.install_script_sha256s")
	out := make([]string, 0, len(versions))
	for _, version := range versions {
		checksum := strings.TrimSpace(checksums[version])
		if checksum == "" && strings.TrimSpace(viper.GetString("k8s.version")) == version {
			checksum = strings.TrimSpace(viper.GetString("rke2.install_script_sha256"))
		}
		out = append(out, checksum)
	}
	return out
}

func currentManualResolveInstallerSHA() bool {
	if rancherMode() != "manual" {
		return true
	}
	for _, checksum := range currentManualInstallerSHA256s() {
		if strings.TrimSpace(checksum) == "" {
			return true
		}
	}
	return false
}

func normalizeInteractiveBasePath(basePath string) string {
	basePath = "/" + strings.Trim(strings.TrimSpace(basePath), "/")
	if basePath == "/" {
		return ""
	}
	return basePath
}

func interactiveSetupPath(basePath string, path string) string {
	if basePath == "" {
		return path
	}
	if path == "/" {
		return basePath + "/"
	}
	return basePath + path
}

func (s *interactiveServer) authorized(r *http.Request) bool {
	if strings.TrimSpace(r.URL.Query().Get("token")) == s.token {
		return true
	}
	if r.FormValue("token") == s.token {
		return true
	}
	return requestFromLoopback(r) && sameOriginBrowserRequest(r)
}

func decodePreflightConfigUpdateRequest(r *http.Request) (settings.PreflightConfigUpdate, error) {
	contentType := strings.ToLower(r.Header.Get("Content-Type"))
	if strings.Contains(contentType, "application/json") {
		var req settings.PreflightConfigUpdate
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return req, err
		}
		return req, nil
	}

	if err := r.ParseForm(); err != nil {
		return settings.PreflightConfigUpdate{}, err
	}
	tfVars := make(map[string]string, len(settings.EditableTFVarKeys))
	for _, key := range settings.EditableTFVarKeys {
		tfVars[key] = r.FormValue("tfVars." + key)
	}

	return settings.PreflightConfigUpdate{
		Mode:                  r.FormValue("mode"),
		Versions:              r.Form["versions"],
		HelmCommands:          r.Form["helmCommands"],
		K8SVersions:           r.Form["k8sVersions"],
		InstallerSHA256s:      r.Form["installerSHA256s"],
		ResolveInstallerSHA:   parseHTMLBool(r.FormValue("resolveInstallerSHA")),
		Distro:                r.FormValue("distro"),
		BootstrapPassword:     r.FormValue("bootstrapPassword"),
		PreloadImages:         parseHTMLBool(r.FormValue("preloadImages")),
		UserFirstName:         r.FormValue("userFirstName"),
		UserLastName:          r.FormValue("userLastName"),
		TFVars:                tfVars,
		CustomHostnameEnabled: parseHTMLBool(r.FormValue("customHostnameEnabled")),
		CustomHostnameInput:   r.FormValue("customHostname"),
	}, nil
}

func parseHTMLBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "on", "1", "yes":
		return true
	default:
		return false
	}
}

func (s *interactiveServer) runResolution() {
	tap := &logTap{}
	tap.onLine = func(line string) { s.appendLog(line) }

	originalWriter := log.Writer()
	originalFlags := log.Flags()
	log.SetOutput(io.MultiWriter(originalWriter, tap))
	defer func() {
		log.SetOutput(originalWriter)
		log.SetFlags(originalFlags)
	}()

	totalHAs := viper.GetInt("total_has")
	err := settings.ValidateCustomHostnameConfig(totalHAs)
	var plans []*RancherResolvedPlan
	if err == nil {
		plans, err = prepareRancherConfiguration(totalHAs)
	}
	if err == nil {
		logResolvedPlans(plans)
	}

	tap.flush()

	if err != nil {
		s.mu.Lock()
		s.resolveErr = err.Error()
		s.mu.Unlock()
		s.broadcast(interactiveEvent{Type: "error", Error: err.Error()})
		select {
		case s.resultCh <- interactiveResult{plans: nil, err: fmt.Errorf("plan resolution failed: %w", err)}:
		default:
		}
		return
	}

	planText := buildResolvedPlansDialogMessage(plans)

	s.mu.Lock()
	s.plans = plans
	s.planText = planText
	s.phase = phaseReview
	s.mu.Unlock()

	s.broadcast(interactiveEvent{Type: "plan", Plan: planText})
	s.broadcast(interactiveEvent{Type: "phase", Phase: phaseReview})
}

func (s *interactiveServer) appendLog(line string) {
	s.mu.Lock()
	s.logs = append(s.logs, line)
	subs := append([]chan interactiveEvent(nil), s.subscribers...)
	s.mu.Unlock()

	for _, sub := range subs {
		select {
		case sub <- interactiveEvent{Type: "log", Line: line}:
		default:
		}
	}
}

func (s *interactiveServer) broadcast(ev interactiveEvent) {
	s.mu.Lock()
	subs := append([]chan interactiveEvent(nil), s.subscribers...)
	s.mu.Unlock()

	for _, sub := range subs {
		select {
		case sub <- ev:
		default:
		}
	}
}

func (s *interactiveServer) removeSubscriber(target chan interactiveEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, sub := range s.subscribers {
		if sub == target {
			s.subscribers = append(s.subscribers[:i], s.subscribers[i+1:]...)
			return
		}
	}
}

func writeSSE(w io.Writer, flusher http.Flusher, ev interactiveEvent) {
	payload, err := json.Marshal(ev)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", payload)
	flusher.Flush()
}

type logTap struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	onLine func(string)
}

func (t *logTap) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	n, err := t.buf.Write(p)
	for {
		data := t.buf.Bytes()
		idx := bytes.IndexByte(data, '\n')
		if idx < 0 {
			break
		}
		line := string(data[:idx])
		t.buf.Next(idx + 1)
		if strings.TrimSpace(line) != "" && t.onLine != nil {
			t.onLine(line)
		}
	}
	return n, err
}

func (t *logTap) flush() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.buf.Len() == 0 {
		return
	}
	line := strings.TrimRight(t.buf.String(), "\r\n")
	t.buf.Reset()
	if strings.TrimSpace(line) != "" && t.onLine != nil {
		t.onLine(line)
	}
}
