package test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPanelStateURLStripsTokenAndUsesStateEndpoint(t *testing.T) {
	got, err := panelStateURL("http://127.0.0.1:1234/?token=secret#frag")
	if err != nil {
		t.Fatalf("panelStateURL failed: %v", err)
	}
	if want := "http://127.0.0.1:1234/api/state"; got != want {
		t.Fatalf("panelStateURL() = %q, want %q", got, want)
	}
}

func TestPanelSessionHealthyUsesStateEndpoint(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	if !panelSessionHealthy(server.URL + "/?token=secret") {
		t.Fatal("expected panel session to be healthy")
	}
	if gotPath != "/api/state" {
		t.Fatalf("expected health check to use /api/state, got %q", gotPath)
	}
}

func TestPersistAndRemovePanelSession(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("GITHUB_WORKSPACE", workspace)

	startedAt := time.Now()
	panel := &localControlPanel{
		baseURL:   "http://127.0.0.1:1/?token=test",
		repoRoot:  workspace,
		sessionID: "session1",
		startedAt: startedAt,
	}

	if err := panel.persistPanelSession(); err != nil {
		t.Fatalf("persistPanelSession failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "automation-output", "control-panel", "panel-session.json")); err != nil {
		t.Fatalf("expected panel session file: %v", err)
	}

	panel.removePanelSession()
	if _, err := os.Stat(filepath.Join(workspace, "automation-output", "control-panel", "panel-session.json")); !os.IsNotExist(err) {
		t.Fatalf("expected panel session file to be removed, stat err=%v", err)
	}
}

func TestInspectLocalPanelSessionReportsRunningSession(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("GITHUB_WORKSPACE", workspace)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	startedAt := time.Now()
	panel := &localControlPanel{
		baseURL:   server.URL + "/?token=test",
		repoRoot:  workspace,
		sessionID: "session1",
		startedAt: startedAt,
	}
	if err := panel.persistPanelSession(); err != nil {
		t.Fatalf("persistPanelSession failed: %v", err)
	}

	session := inspectLocalPanelSession(workspace)
	if !session.Running {
		t.Fatalf("expected running panel session, got %#v", session)
	}
	if session.URL != panel.baseURL {
		t.Fatalf("expected panel URL %q, got %q", panel.baseURL, session.URL)
	}
}
