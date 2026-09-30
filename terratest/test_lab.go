package test

// Test Lab reads source to build its catalog. Only an explicitly reviewed run
// executes external test code. Credentials never belong in the library manifest.
import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

type testLabEntry struct {
	ID          string `json:"id"`
	Package     string `json:"package"`
	Suite       string `json:"suite"`
	Test        string `json:"test"`
	File        string `json:"file"`
	Line        int    `json:"line"`
	Constraint  string `json:"constraint"`
	Description string `json:"description"`
}
type testLabCatalog struct {
	Ref       string         `json:"ref"`
	SHA       string         `json:"sha"`
	GoVersion string         `json:"goVersion"`
	FetchedAt time.Time      `json:"fetchedAt"`
	Entries   []testLabEntry `json:"entries"`
}
type testLabPlan struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Host      string    `json:"host"`
	SHA       string    `json:"sha"`
	Ref       string    `json:"ref"`
	Selection []string  `json:"selection"`
	Tags      string    `json:"tags"`
	Timeout   int       `json:"timeout"`
	SavedAt   time.Time `json:"savedAt"`
	HasConfig bool      `json:"hasConfig"`
}
type testLabResult struct {
	Package string  `json:"package"`
	Name    string  `json:"name"`
	Status  string  `json:"status"`
	Elapsed float64 `json:"elapsed"`
}
type testLabRun struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Host       string          `json:"host"`
	SHA        string          `json:"sha"`
	Ref        string          `json:"ref"`
	Selection  []string        `json:"selection"`
	Tags       string          `json:"tags"`
	Timeout    int             `json:"timeout"`
	Status     string          `json:"status"`
	Stage      string          `json:"stage"`
	StartedAt  time.Time       `json:"startedAt"`
	FinishedAt time.Time       `json:"finishedAt,omitempty"`
	Error      string          `json:"error,omitempty"`
	Results    []testLabResult `json:"results"`
}
type testLabLibrary struct {
	Version int            `json:"version"`
	Catalog testLabCatalog `json:"catalog"`
	Plans   []testLabPlan  `json:"plans"`
	Runs    []testLabRun   `json:"runs"`
	GitHub  testLabGitHub  `json:"github"`
}
type testLabRequest struct {
	Action     string               `json:"action"`
	ID         string               `json:"id"`
	Name       string               `json:"name"`
	Ref        string               `json:"ref"`
	SHA        string               `json:"sha"`
	Path       string               `json:"path"`
	Selection  []string             `json:"selection"`
	Tags       string               `json:"tags"`
	Timeout    int                  `json:"timeout"`
	Config     string               `json:"config"`
	Confirm    string               `json:"confirm"`
	Remember   bool                 `json:"remember"`
	ClientID   string               `json:"clientId"`
	Slug       string               `json:"slug"`
	Repository string               `json:"repository"`
	Folder     string               `json:"folder"`
	Revision   string               `json:"revision"`
	Scope      string               `json:"scope"`
	Bundle     *testLabConfigBundle `json:"bundle,omitempty"`
}
type testLabService struct {
	mu           sync.Mutex
	root         string
	library      testLabLibrary
	busy         bool
	catalogError string
	cancel       context.CancelFunc
	logs         map[string]string
	keychain     func(string, string, string) (string, error)
	client       *http.Client
	githubMu     sync.Mutex
	credential   testLabCredential
	device       testLabDevice
	configRoot   string
	exportRoot   string
	configMu     sync.Mutex
	sourceMu     sync.Mutex
	sourceIndex  *testLabSourceIndex
}

func (p *localControlPanel) testLabService() (*testLabService, error) {
	p.testLabMu.Lock()
	defer p.testLabMu.Unlock()
	if p.testLab != nil {
		return p.testLab, nil
	}
	root, err := absoluteFromWorkingDir(filepath.Join(automationOutputDir(), "control-panel", "test-lab"))
	if err != nil {
		return nil, err
	}
	s, err := newTestLabService(root)
	if err == nil {
		p.testLab = s
	}
	return s, err
}
func newTestLabService(root string) (*testLabService, error) {
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	if err := ensureGoDataModule(root); err != nil {
		return nil, fmt.Errorf("isolate Test Lab data from app builds: %w", err)
	}
	s := &testLabService{root: root, logs: map[string]string{}, keychain: testLabKeychain, client: &http.Client{Timeout: 90 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}, library: testLabLibrary{Version: 1, Plans: []testLabPlan{}, Runs: []testLabRun{}, Catalog: testLabCatalog{Entries: []testLabEntry{}}}}
	b, err := os.ReadFile(filepath.Join(root, "library.json"))
	if err == nil {
		if err = json.Unmarshal(b, &s.library); err != nil {
			return nil, fmt.Errorf("Test Lab library cannot be read; existing files were preserved")
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if s.library.Version != 1 {
		return nil, fmt.Errorf("unsupported Test Lab library version")
	}
	for i := range s.library.Runs {
		r := &s.library.Runs[i]
		if !cacheLabIDPattern.MatchString(r.ID) {
			return nil, fmt.Errorf("invalid run record")
		}
		if r.Status == "running" {
			r.Status = "interrupted"
			r.Stage = "Runway closed"
			r.Error = "Runway closed before the run finished. The test process may still be running. Check local processes and the Rancher target before running again."
			r.FinishedAt = time.Now()
		}
	}
	// Config files are ephemeral even after an unclean exit. Only generated run
	// directories are removed; results live separately in the library.
	dirs, _ := filepath.Glob(filepath.Join(root, "work-*"))
	for _, dir := range dirs {
		if cacheLabIDPattern.MatchString(strings.TrimPrefix(filepath.Base(dir), "work-")) {
			_ = os.RemoveAll(dir)
		}
	}
	for _, p := range s.library.Plans {
		if !cacheLabIDPattern.MatchString(p.ID) {
			return nil, fmt.Errorf("invalid saved plan")
		}
	}
	if s.library.GitHub.ClientID == "" {
		s.library.GitHub.ClientID = strings.TrimSpace(os.Getenv("RUNWAY_GITHUB_CLIENT_ID"))
		s.library.GitHub.Slug = strings.TrimSpace(os.Getenv("RUNWAY_GITHUB_APP_SLUG"))
	}
	s.library.GitHub.Connected = false
	return s, s.persistLocked()
}
func (s *testLabService) persistLocked() error {
	data, err := json.MarshalIndent(s.library, "", "  ")
	if err != nil {
		return err
	}
	if len(data) > 16<<20 {
		return fmt.Errorf("Test Lab library is full; remove old runs")
	}
	f, err := os.CreateTemp(s.root, ".library-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), filepath.Join(s.root, "library.json"))
}
func testLabKeychain(action, id, value string) (string, error) {
	if runtime.GOOS != "darwin" {
		return "", fmt.Errorf("saved credentials require macOS Keychain; leave Remember off to use this session")
	}
	if id != "github" && !strings.HasPrefix(id, "plan-") {
		return "", fmt.Errorf("invalid credential identifier")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	service := "com.rancher-runway.test-lab"
	args := []string{"find-generic-password", "-s", service, "-a", id, "-w"}
	if action == "delete" {
		args = []string{"delete-generic-password", "-s", service, "-a", id}
	}
	if action == "write" {
		encoded := base64.StdEncoding.EncodeToString([]byte(value))
		cmd := exec.CommandContext(ctx, "/usr/bin/security", "-i")
		cmd.Stdin = strings.NewReader(fmt.Sprintf("add-generic-password -U -s %s -a %s -w %s\n", service, id, encoded))
		if _, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("Keychain could not save the credential")
		}
		got, err := testLabKeychain("read", id, "")
		if err != nil || got != value {
			return "", fmt.Errorf("Keychain did not save the credential")
		}
		return "", nil
	}
	data, err := exec.CommandContext(ctx, "/usr/bin/security", args...).Output()
	if err != nil {
		if code, ok := err.(*exec.ExitError); action == "delete" && ok && code.ExitCode() == 44 {
			return "", nil
		}
		return "", fmt.Errorf("Keychain credential unavailable; reconnect or enter the configuration again")
	}
	if action == "delete" {
		return "", nil
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil {
		return "", fmt.Errorf("Keychain credential is unreadable")
	}
	return string(decoded), nil
}
func (p *localControlPanel) stopTestLab() {
	p.testLabMu.Lock()
	s := p.testLab
	p.testLabMu.Unlock()
	if s != nil {
		s.mu.Lock()
		if s.cancel != nil {
			s.cancel()
		}
		s.mu.Unlock()
	}
}
func (p *localControlPanel) handleTestLab(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	// Configs and logs may contain target information; require local browser
	// origin or the panel token even for reads.
	if r.Method == http.MethodGet && !p.authorizedLocalBrowserRead(r) || r.Method == http.MethodPost && !p.authorizedLocalAction(r) {
		http.Error(w, "unauthorized", 401)
		return
	}
	s, err := p.testLabService()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if r.Method == http.MethodGet {
		s.mu.Lock()
		defer s.mu.Unlock()
		writeJSON(w, map[string]any{"library": s.library, "catalogBusy": s.busy, "catalogError": s.catalogError, "keychain": runtime.GOOS == "darwin"})
		return
	}
	var req testLabRequest
	if err = json.NewDecoder(http.MaxBytesReader(w, r.Body, 96<<20)).Decode(&req); err != nil {
		http.Error(w, "invalid Test Lab request", 400)
		return
	}
	var result any
	switch req.Action {
	case "config-library", "config-save", "config-load", "config-delete", "folder-save", "folder-delete", "config-export", "config-import-preview", "config-import":
		result, err = s.configAction(req)
	case "source-docs", "source-file", "preflight":
		result, err = s.sourceAction(req)
	case "catalog":
		result, err = s.startCatalog(req.Ref)
	case "review":
		result, err = s.review(req)
	case "run":
		result, err = s.startRun(req)
	case "targets":
		p.mu.Lock()
		targets := []clusterView{}
		for _, c := range p.clusterSnapshot {
			if c.RancherURL != "" && !c.Provisioning {
				c.Pods = nil
				c.KubeconfigPath = ""
				targets = append(targets, c)
			}
		}
		p.mu.Unlock()
		sort.Slice(targets, func(i, j int) bool { return targets[i].Name < targets[j].Name })
		result = map[string]any{"targets": targets}
	case "github-config", "github-start", "github-poll", "github-status", "github-disconnect", "github-repositories", "github-link", "github-unlink", "github-create":
		result, err = s.githubAction(r.Context(), req)
	default:
		result, err = s.mutate(req)
	}
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	writeJSON(w, result)
}
func (s *testLabService) mutate(req testLabRequest) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch req.Action {
	case "cancel":
		if s.cancel != nil {
			s.cancel()
		}
		return map[string]bool{"ok": true}, nil
	case "logs":
		if !cacheLabIDPattern.MatchString(req.ID) {
			return nil, fmt.Errorf("invalid run")
		}
		log := s.logs[req.ID]
		if log == "" {
			b, _ := os.ReadFile(filepath.Join(s.root, req.ID+".log"))
			log = string(b)
		}
		return map[string]string{"text": log}, nil
	case "save-plan":
		host, _, err := testLabConfig(req.Config)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(req.Name) == "" {
			return nil, fmt.Errorf("name this plan")
		}
		if len(s.library.Plans) >= 100 {
			return nil, fmt.Errorf("saved plan limit reached")
		}
		if _, err = s.commandsLocked(req); err != nil {
			return nil, err
		}
		id := cacheLabID()
		if req.Remember {
			if _, err = s.keychain("write", "plan-"+id, req.Config); err != nil {
				return nil, err
			}
		}
		plan := testLabPlan{ID: id, Name: cacheLabText(req.Name, 100), Host: host, SHA: req.SHA, Ref: s.library.Catalog.Ref, Selection: req.Selection, Tags: req.Tags, Timeout: req.Timeout, SavedAt: time.Now(), HasConfig: req.Remember}
		s.library.Plans = append(s.library.Plans, plan)
		if err = s.persistLocked(); err != nil {
			s.library.Plans = s.library.Plans[:len(s.library.Plans)-1]
			if req.Remember {
				_, _ = s.keychain("delete", "plan-"+id, "")
			}
			return nil, err
		}
		return plan, nil
	case "load-plan":
		for _, plan := range s.library.Plans {
			if plan.ID == req.ID {
				config := ""
				var err error
				if plan.HasConfig {
					config, err = s.keychain("read", "plan-"+plan.ID, "")
				}
				if err != nil {
					return nil, err
				}
				return map[string]any{"plan": plan, "config": config}, nil
			}
		}
		return nil, fmt.Errorf("plan not found")
	case "delete-plan":
		if req.Confirm != "DELETE PLAN" {
			return nil, fmt.Errorf("type DELETE PLAN to remove the saved plan")
		}
		for i, plan := range s.library.Plans {
			if plan.ID == req.ID {
				if plan.HasConfig {
					if _, err := s.keychain("delete", "plan-"+plan.ID, ""); err != nil {
						return nil, err
					}
				}
				previous := append([]testLabPlan(nil), s.library.Plans...)
				s.library.Plans = append(s.library.Plans[:i], s.library.Plans[i+1:]...)
				if err := s.persistLocked(); err != nil {
					s.library.Plans = previous
					// The Keychain deletion already succeeded. Keep the plan usable
					// in this session without claiming its configuration still exists.
					s.library.Plans[i].HasConfig = false
					return nil, err
				}
				return map[string]bool{"ok": true}, nil
			}
		}
		return nil, fmt.Errorf("plan not found")
	case "delete-run":
		if req.Confirm != "DELETE RUN" {
			return nil, fmt.Errorf("type DELETE RUN to remove this local result")
		}
		for i, run := range s.library.Runs {
			if run.ID == req.ID {
				if run.Status == "running" {
					return nil, fmt.Errorf("stop this run first")
				}
				previous := append([]testLabRun(nil), s.library.Runs...)
				s.library.Runs = append(s.library.Runs[:i], s.library.Runs[i+1:]...)
				if err := s.persistLocked(); err != nil {
					s.library.Runs = previous
					return nil, err
				}
				delete(s.logs, req.ID)
				removeErr := os.Remove(filepath.Join(s.root, req.ID+".log"))
				if os.IsNotExist(removeErr) {
					removeErr = nil
				}
				return map[string]bool{"ok": true}, removeErr
			}
		}
		return nil, fmt.Errorf("run not found")
	}
	return nil, fmt.Errorf("unknown Test Lab action")
}

func (p *localControlPanel) testLabRunning() bool {
	p.testLabMu.Lock()
	s := p.testLab
	p.testLabMu.Unlock()
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancel != nil
}

type testLabActivityState struct {
	Running bool   `json:"running"`
	RunID   string `json:"runId,omitempty"`
	Stage   string `json:"stage,omitempty"`
}

func (p *localControlPanel) testLabActivity() testLabActivityState {
	p.testLabMu.Lock()
	s := p.testLab
	p.testLabMu.Unlock()
	if s == nil {
		return testLabActivityState{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.library.Runs {
		if r.Status == "running" {
			return testLabActivityState{Running: true, RunID: r.ID, Stage: r.Stage}
		}
	}
	return testLabActivityState{}
}
