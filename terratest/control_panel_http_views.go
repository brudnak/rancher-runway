package test

import (
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/buildinfo"
	"github.com/brudnak/ha-rancher-rke2/terratest/ui"
	"html/template"
	"net/http"
)

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
		Build           buildinfo.Info
		SetupEditorHTML template.HTML
	}{
		Token:           p.token,
		Build:           buildinfo.Current(),
		SetupEditorHTML: setupEditorHTML,
	})
}

type controlPanelStaticAsset struct {
	ContentType string
	Body        string
}

var controlPanelStaticAssets = map[string]controlPanelStaticAsset{
	"/static/release_plan_import.js": {ContentType: "application/javascript; charset=utf-8", Body: ui.ReleasePlanImportJS},
	"/static/control_panel.css": {
		ContentType: "text/css; charset=utf-8",
		Body:        ui.ControlPanelCSS,
	},
	"/static/control_panel_components.css": {
		ContentType: "text/css; charset=utf-8",
		Body:        ui.ControlPanelComponentsCSS,
	},
	"/static/control_panel.js": {
		ContentType: "application/javascript; charset=utf-8",
		Body:        ui.ControlPanelJS,
	},
	"/static/control_panel_header_vue.js": {
		ContentType: "application/javascript; charset=utf-8",
		Body:        ui.ControlPanelHeaderVueJS,
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
