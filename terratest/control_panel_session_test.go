package test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

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

func TestPanelSessionReuseAndOwnership(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("GITHUB_WORKSPACE", workspace)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	panel := &localControlPanel{baseURL: server.URL + "/?token=test", repoRoot: workspace, sessionID: "current", startedAt: time.Now()}
	if err := panel.persistPanelSession(); err != nil {
		t.Fatal(err)
	}
	previous := &localControlPanel{sessionID: "previous"}
	previous.removePanelSession()
	got, ok, err := existingControlPanelURL(workspace)
	if err != nil || !ok || got != panel.baseURL {
		t.Fatalf("existing session lost: %q %v %v", got, ok, err)
	}
	if _, ok, err := existingControlPanelURL(t.TempDir()); err != nil || ok {
		t.Fatalf("reused another workspace: %v %v", ok, err)
	}
	panel.removePanelSession()
	if _, ok, err := existingControlPanelURL(workspace); err != nil || ok {
		t.Fatalf("removed session reused: %v %v", ok, err)
	}
}
