package test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/awspricing"
	"github.com/brudnak/ha-rancher-rke2/terratest/settings"
	"github.com/brudnak/ha-rancher-rke2/terratest/ui"
	"github.com/spf13/viper"
	"html/template"
	"net/http"
	"strings"
	"time"
)

func (s *interactiveServer) registerHandlers(mux *http.ServeMux, initialVersions []string) {
	s.registerHandlersAt(mux, initialVersions, "")
}

func (s *interactiveServer) registerHandlersAt(mux *http.ServeMux, initialVersions []string, basePath string) {
	basePath = normalizeInteractiveBasePath(basePath)
	pageTemplate := template.Must(template.New("interactive-setup").Parse(ui.InteractiveSetupHTML))
	mux.HandleFunc(interactiveSetupPath(basePath, "/api/import-config"), s.handleConfigImport)
	mux.HandleFunc(interactiveSetupPath(basePath, "/static/control_panel.css"), func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			http.Error(w, "invalid interactive setup token", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(ui.ControlPanelCSS))
	})

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
		// Read the current config so an import appears immediately after reload.
		data := interactiveSetupTemplateDataFor(s.token, s.configPath, currentPreflightVersions(), basePath, false)
		_ = pageTemplate.Execute(w, data)
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

	mux.HandleFunc(interactiveSetupPath(basePath, "/api/linode-image-search"), func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			http.Error(w, "invalid interactive setup token", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed: "+r.Method, http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Version     string `json:"version"`
			CustomImage string `json:"customImage"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		tag, results, err := searchLinodeDockerImageSources(req.Version, req.CustomImage)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]any{
			"tag":     tag,
			"results": results,
		})
	})

	mux.HandleFunc(interactiveSetupPath(basePath, "/api/linode-catalog"), func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			http.Error(w, "invalid interactive setup token", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		catalog, err := collectLinodeCatalog(ctx, &http.Client{Timeout: 15 * time.Second}, linodeCatalogDefaultAPIBaseURL, linodeAccessToken())
		if err != nil {
			http.Error(w, err.Error(), http.StatusPreconditionFailed)
			return
		}
		writeJSON(w, catalog)
	})

	mux.HandleFunc(interactiveSetupPath(basePath, "/api/gpu-price"), func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			http.Error(w, "invalid interactive setup token", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed: "+r.Method, http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			InstanceType string `json:"instanceType"`
			Region       string `json:"region"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		instanceType := settings.GPUWorkerInstanceType(req.InstanceType)
		if strings.EqualFold(strings.TrimSpace(req.InstanceType), "p5.4xlarge") {
			instanceType = "p5.4xlarge"
		}
		region := strings.TrimSpace(req.Region)
		if region == "" {
			region = strings.TrimSpace(viper.GetString("tf_vars.aws_region"))
		}
		if region == "" {
			http.Error(w, "AWS region is required for GPU price lookup", http.StatusBadRequest)
			return
		}
		hourly, err := awspricing.EC2HourlyUSD(region, instanceType)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, map[string]any{
			"instanceType": instanceType,
			"region":       region,
			"hourlyUSD":    hourly,
			"fourHourUSD":  hourly * 4,
			"dayUSD":       hourly * 24,
			"source":       "AWS Pricing API Linux On-Demand",
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

		s.configMu.Lock()
		defer s.configMu.Unlock()
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
		if err := validateConfiguredAWSEC2KeyPair(); err != nil {
			http.Error(w, fmt.Sprintf("AWS EC2 key pair preflight failed: %v", err), http.StatusBadRequest)
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
