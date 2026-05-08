package test

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

type panelSessionRecord struct {
	PID       int       `json:"pid"`
	URL       string    `json:"url"`
	RepoRoot  string    `json:"repoRoot"`
	SessionID string    `json:"sessionId"`
	StartedAt time.Time `json:"startedAt"`
}

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

func existingControlPanelURL(repoRoot string) (string, bool, error) {
	session, ok, err := readPanelSession()
	if err != nil || !ok {
		return "", false, err
	}

	if !samePath(session.RepoRoot, repoRoot) {
		return "", false, nil
	}

	if !processAlive(session.PID) || !panelSessionHealthy(session.URL) {
		removePanelSessionFile()
		return "", false, nil
	}

	return session.URL, true, nil
}

func (p *localControlPanel) persistPanelSession() error {
	session := panelSessionRecord{
		PID:       os.Getpid(),
		URL:       p.baseURL,
		RepoRoot:  p.repoRoot,
		SessionID: p.sessionID,
		StartedAt: p.startedAt,
	}
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}

	path := panelSessionPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func (p *localControlPanel) removePanelSession() {
	session, ok, err := readPanelSession()
	if err != nil || !ok {
		return
	}
	if session.PID != os.Getpid() || session.SessionID != p.sessionID {
		return
	}
	removePanelSessionFile()
}

func readPanelSession() (panelSessionRecord, bool, error) {
	data, err := os.ReadFile(panelSessionPath())
	if err != nil {
		if os.IsNotExist(err) {
			return panelSessionRecord{}, false, nil
		}
		return panelSessionRecord{}, false, err
	}

	var session panelSessionRecord
	if err := json.Unmarshal(data, &session); err != nil {
		removePanelSessionFile()
		return panelSessionRecord{}, false, nil
	}
	if session.PID <= 0 || strings.TrimSpace(session.URL) == "" || strings.TrimSpace(session.RepoRoot) == "" {
		removePanelSessionFile()
		return panelSessionRecord{}, false, nil
	}
	return session, true, nil
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

func removePanelSessionFile() {
	if err := os.Remove(panelSessionPath()); err != nil && !os.IsNotExist(err) {
		log.Printf("[control-panel] Failed to remove panel session file: %v", err)
	}
}

func panelSessionHealthy(rawURL string) bool {
	healthURL, err := panelStateURL(rawURL)
	if err != nil {
		return false
	}

	client := http.Client{Timeout: 750 * time.Millisecond}
	resp, err := client.Get(healthURL)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func panelStateURL(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	parsed.Path = "/api/state"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
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

func samePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	if leftErr == nil {
		left = leftAbs
	}
	if rightErr == nil {
		right = rightAbs
	}
	return filepath.Clean(left) == filepath.Clean(right)
}
