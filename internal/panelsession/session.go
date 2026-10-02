// Package panelsession manages persisted local control-panel sessions.
// Callers choose the storage path and supply process liveness checks.
package panelsession

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Record preserves the launch URL and ownership of a running app instance.
type Record struct {
	PID       int       `json:"pid"`
	URL       string    `json:"url"`
	RepoRoot  string    `json:"repoRoot"`
	SessionID string    `json:"sessionId"`
	StartedAt time.Time `json:"startedAt"`
}

// Store holds the session record at an application-selected path.
type Store struct{ Path string }

// ExistingURL reuses a healthy session only when it belongs to this workspace.
func (s Store) ExistingURL(repoRoot string, processAlive func(int) bool) (string, bool, error) {
	session, ok, err := s.Read()
	if err != nil || !ok {
		return "", false, err
	}

	if !SamePath(session.RepoRoot, repoRoot) {
		return "", false, nil
	}

	if !processAlive(session.PID) || !Healthy(session.URL) {
		s.remove()
		return "", false, nil
	}

	return session.URL, true, nil
}

// Write persists a private session record.
func (s Store) Write(session Record) error {
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}

	path := s.Path
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// RemoveOwned removes only the calling process and session's record.
func (s Store) RemoveOwned(pid int, sessionID string) {
	session, ok, err := s.Read()
	if err != nil || !ok {
		return
	}
	if session.PID != pid || session.SessionID != sessionID {
		return
	}
	s.remove()
}

// Read discards malformed records and distinguishes missing files from read errors.
func (s Store) Read() (Record, bool, error) {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return Record{}, false, nil
		}
		return Record{}, false, err
	}

	var session Record
	if err := json.Unmarshal(data, &session); err != nil {
		s.remove()
		return Record{}, false, nil
	}
	if session.PID <= 0 || strings.TrimSpace(session.URL) == "" || strings.TrimSpace(session.RepoRoot) == "" {
		s.remove()
		return Record{}, false, nil
	}
	return session, true, nil
}

func (s Store) remove() {
	if err := os.Remove(s.Path); err != nil && !os.IsNotExist(err) {
		log.Printf("[control-panel] Failed to remove panel session file: %v", err)
	}
}

// Healthy probes the state endpoint with a bounded timeout.
func Healthy(rawURL string) bool {
	healthURL, err := StateURL(rawURL)
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

// StateURL removes launch query parameters and fragments from the health probe.
func StateURL(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	parsed.Path = "/api/state"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

// SamePath compares cleaned absolute paths without resolving symlinks.
func SamePath(left, right string) bool {
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
