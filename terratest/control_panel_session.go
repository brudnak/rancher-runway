package test

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"syscall"

	"github.com/brudnak/ha-rancher-rke2/internal/panelsession"
)

func openExistingControlPanel(repoRoot string) (bool, error) {
	existingURL, ok, err := existingControlPanelURL(repoRoot)
	if err != nil || !ok {
		return ok, err
	}

	log.Printf("[control-panel] Reusing existing local control panel %s", existingURL)
	if err := openBrowser(existingURL); err != nil {
		return false, fmt.Errorf("failed to open existing control panel: %w", err)
	}
	return true, nil
}

func panelSessions() panelsession.Store { return panelsession.Store{Path: panelSessionPath()} }

func existingControlPanelURL(repoRoot string) (string, bool, error) {
	return panelSessions().ExistingURL(repoRoot, processAlive)
}

func (p *localControlPanel) persistPanelSession() error {
	return panelSessions().Write(panelsession.Record{
		PID: os.Getpid(), URL: p.baseURL, RepoRoot: p.repoRoot,
		SessionID: p.sessionID, StartedAt: p.startedAt,
	})
}

func (p *localControlPanel) removePanelSession() {
	panelSessions().RemoveOwned(os.Getpid(), p.sessionID)
}

func panelSessionPath() string {
	return filepath.Join(automationOutputDir(), "control-panel", "panel-session.json")
}

func panelLaunchLogPath() string {
	path := filepath.Join(automationOutputDir(), "control-panel", "app-launch.log")
	absPath, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return absPath
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return process.Signal(syscall.Signal(0)) == nil
}
