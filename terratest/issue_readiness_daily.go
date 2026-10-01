package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const dailyReadinessLimit = 100

type dailyReadinessBuild struct {
	Label          string `json:"label"`
	Reference      string `json:"reference"`
	AgentReference string `json:"agentReference"`
	ServerDigest   string `json:"serverDigest"`
	AgentDigest    string `json:"agentDigest"`
}
type dailyReadinessEntry struct {
	IssueURL   string                 `json:"issueUrl"`
	Title      string                 `json:"title"`
	Number     int                    `json:"number"`
	PackageID  string                 `json:"packageId,omitempty"`
	NeedsPlan  bool                   `json:"needsPlan"`
	QANone     bool                   `json:"qaNone"`
	Closed     bool                   `json:"closed"`
	Verdict    string                 `json:"verdict"`
	Detail     string                 `json:"detail"`
	Complete   bool                   `json:"complete"`
	QAState    string                 `json:"qaState"`
	QAComplete bool                   `json:"qaComplete"`
	QAURL      string                 `json:"qaUrl,omitempty"`
	Statuses   []issueReadinessStatus `json:"statuses"`
	Workflow   readinessWorkflow      `json:"workflow"`
	Builds     []dailyReadinessBuild  `json:"builds"`
	Error      string                 `json:"error,omitempty"`
}
type dailyReadinessReport struct {
	ID           string                `json:"id"`
	Progress     []readinessProgress   `json:"progress,omitempty"`
	CurrentIssue string                `json:"currentIssue,omitempty"`
	Trigger      string                `json:"trigger"`
	Day          string                `json:"day"`
	Timezone     string                `json:"timezone"`
	StartedAt    time.Time             `json:"startedAt"`
	FinishedAt   *time.Time            `json:"finishedAt,omitempty"`
	Config       myWorkConfig          `json:"config"`
	Milestone    string                `json:"milestone"`
	Status       string                `json:"status"`
	Total        int                   `json:"total"`
	Checked      int                   `json:"checked"`
	Remaining    int                   `json:"remaining"`
	NeedsPlan    int                   `json:"needsPlan"`
	Ready        int                   `json:"ready"`
	QAFound      int                   `json:"qaFound"`
	QAMissing    int                   `json:"qaMissing"`
	QAUnknown    int                   `json:"qaUnknown"`
	Incomplete   int                   `json:"incomplete"`
	Entries      []dailyReadinessEntry `json:"entries"`
	Error        string                `json:"error,omitempty"`
}
type dailyReadinessState struct {
	Version       int                   `json:"version"`
	Enabled       bool                  `json:"enabled"`
	LastAutoDay   string                `json:"lastAutoDay,omitempty"`
	LastAutoScope myWorkConfig          `json:"lastAutoScope"`
	Report        *dailyReadinessReport `json:"report,omitempty"`
	StorageError  string                `json:"storageError,omitempty"`
}
type dailyReadinessRequest struct {
	Action   string `json:"action"`
	Enabled  *bool  `json:"enabled,omitempty"`
	Timezone string `json:"timezone,omitempty"`
}
type dailyReadinessService struct {
	mu          sync.Mutex
	root        string
	state       dailyReadinessState
	cancel      context.CancelFunc
	now         func() time.Time
	scope       func() (myWorkSnapshot, error)
	fetch       func(context.Context, myWorkConfig) (myWorkSnapshot, error)
	plans       func() ([]testPackage, error)
	scanFactory func() func(context.Context, issueReadinessRequest) (issueReadinessReport, error)
}

func newDailyReadinessService(root string) (*dailyReadinessService, error) {
	s := &dailyReadinessService{root: root, state: dailyReadinessState{Version: 1}, now: time.Now}
	raw, err := testPackageReadBounded(filepath.Join(root, ".daily-readiness.json"), 8<<20)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err = decodeTestPackage(raw, &s.state); err != nil {
		return nil, fmt.Errorf("daily readiness data is invalid; existing file preserved: %w", err)
	}
	if s.state.Version != 1 {
		return nil, fmt.Errorf("unsupported daily readiness data version")
	}
	if r := s.state.Report; r != nil && (r.Status == "running" || r.Status == "cancelling") {
		r.Status = "interrupted"
		r.Error = "Runway closed before this scan finished. Use Scan now to retry."
		now := s.now().UTC()
		r.FinishedAt = &now
		if err = s.persistLocked(s.state); err != nil {
			return nil, err
		}
	}
	return s, nil
}
func (s *dailyReadinessService) persistLocked(next dailyReadinessState) error {
	raw, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	if len(raw) > 8<<20 {
		return fmt.Errorf("daily readiness report exceeds 8 MiB")
	}
	if err = writePrivateConfigAtomically(filepath.Join(s.root, ".daily-readiness.json"), raw); err != nil {
		return err
	}
	s.state = next
	return nil
}
func (s *dailyReadinessService) snapshot() dailyReadinessState {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, _ := json.Marshal(s.state)
	var out dailyReadinessState
	_ = json.Unmarshal(raw, &out)
	return out
}
func (s *dailyReadinessService) change(req dailyReadinessRequest) error {
	if req.Action == "settings" {
		if req.Enabled == nil {
			return fmt.Errorf("choose whether daily scanning is enabled")
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		next := s.state
		next.Enabled = *req.Enabled
		if err := s.persistLocked(next); err != nil {
			return err
		}
		if !next.Enabled && s.cancel != nil && s.state.Report.Trigger == "auto" {
			s.cancel()
		}
		return nil
	}
	if req.Action == "cancel" {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.cancel != nil {
			s.cancel()
		}
		return nil
	}
	if req.Action != "auto" && req.Action != "scan" {
		return fmt.Errorf("unknown daily readiness action")
	}
	zone, err := time.LoadLocation(req.Timezone)
	if err != nil || req.Timezone == "" {
		return fmt.Errorf("choose a valid calendar timezone")
	}
	scope, err := s.scope()
	if err != nil {
		return err
	}
	if scope.Config.Milestone == 0 {
		if req.Action == "auto" {
			return nil
		}
		return fmt.Errorf("pull a milestone in My Work before scanning")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return nil
	}
	day := s.now().In(zone).Format("2006-01-02")
	if req.Action == "auto" && (!s.state.Enabled || s.state.LastAutoDay == day && s.state.LastAutoScope == scope.Config) {
		return nil
	}
	next := s.state
	next.StorageError = ""
	next.LastAutoDay = day
	next.LastAutoScope = scope.Config
	next.Report = &dailyReadinessReport{ID: cacheLabID(), Trigger: req.Action, Day: day, Timezone: req.Timezone, StartedAt: s.now().UTC(), Config: scope.Config, Milestone: scope.Milestone.Title, Status: "running", Entries: []dailyReadinessEntry{}}
	if err = s.persistLocked(next); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	s.cancel = cancel
	go s.run(ctx, next.Report.ID, scope.Config)
	return nil
}
func (s *dailyReadinessService) update(id string, fn func(*dailyReadinessReport)) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Report == nil || s.state.Report.ID != id {
		return false
	}
	raw, _ := json.Marshal(s.state)
	var next dailyReadinessState
	_ = json.Unmarshal(raw, &next)
	fn(next.Report)
	if err := s.persistLocked(next); err != nil {
		s.state.StorageError = "The latest progress could not be saved: " + err.Error()
		if s.cancel != nil {
			s.cancel()
		}
		return false
	}
	return true
}
func (s *dailyReadinessService) finish(id, status, message string) {
	s.update(id, func(r *dailyReadinessReport) {
		r.Status = status
		r.Error = message
		now := s.now().UTC()
		r.FinishedAt = &now
	})
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	if s.state.StorageError != "" && s.state.Report != nil {
		s.state.Report.Status = "failed"
		s.state.Report.Error = s.state.StorageError
	}
}
func dailyIssueKey(raw string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(raw), "/"))
}
func (s *dailyReadinessService) run(ctx context.Context, id string, config myWorkConfig) {
	fresh, err := s.fetch(ctx, config)
	if err != nil {
		if ctx.Err() != nil {
			s.finish(id, "cancelled", "Scan stopped while refreshing the milestone.")
		} else {
			s.finish(id, "failed", "Could not refresh the milestone: "+err.Error())
		}
		return
	}
	packages, err := s.plans()
	if err != nil {
		s.finish(id, "failed", "Could not read saved plans: "+err.Error())
		return
	}
	latest := map[string]testPackage{}
	for _, p := range packages {
		key := dailyIssueKey(p.IssueURL)
		previous, ok := latest[key]
		if !ok || p.UpdatedAt.After(previous.UpdatedAt) {
			latest[key] = p
		}
	}
	entries := []dailyReadinessEntry{}
	for _, issue := range fresh.Issues {
		if issue.State != "open" || issueQANone(issue) {
			continue
		}
		pkg := latest[dailyIssueKey(issue.URL)]
		entries = append(entries, dailyReadinessEntry{IssueURL: issue.URL, Title: issue.Title, Number: issue.Number, PackageID: pkg.ID, NeedsPlan: len(pkg.Cases) == 0, Verdict: "pending", QAState: "unknown", Statuses: []issueReadinessStatus{}, Builds: []dailyReadinessBuild{}})
	}
	if !s.update(id, func(r *dailyReadinessReport) {
		r.Total = len(entries)
		r.Remaining = len(entries)
		r.Milestone = fresh.Milestone.Title
		r.Entries = entries
		for _, entry := range entries {
			if entry.NeedsPlan {
				r.NeedsPlan++
			}
		}
	}) {
		s.finish(id, "failed", "Could not save scan progress.")
		return
	}
	scan := s.scanFactory()
	for i, entry := range entries {
		if ctx.Err() != nil || i >= dailyReadinessLimit {
			break
		}
		issueCtx, cancel := context.WithTimeout(ctx, 4*time.Minute)
		issueCtx = readinessWithProgress(issueCtx, func(progress readinessProgress) {
			s.mu.Lock()
			defer s.mu.Unlock()
			if r := s.state.Report; r != nil && r.ID == id {
				r.CurrentIssue = entry.Title
				r.Progress = append(r.Progress, progress)
				if len(r.Progress) > 12 {
					r.Progress = r.Progress[len(r.Progress)-12:]
				}
			}
		})
		report, err := scan(issueCtx, issueReadinessRequest{IssueURL: entry.IssueURL})
		cancel()
		if ctx.Err() != nil {
			break
		}
		if err != nil {
			entry.Error = err.Error()
			entry.Verdict = "unknown"
		} else {
			entry.Title = report.Issue.Title
			entry.Closed = report.Issue.State == "closed"
			if issueQANone(report.Issue) {
				entry.QANone = true
			}
			entry.Verdict = report.Verdict
			entry.Detail = report.Detail
			entry.Complete = report.Complete
			entry.QAState = report.QA.State
			entry.QAComplete = report.QA.Complete
			entry.Statuses = report.Statuses
			entry.Workflow = report.Workflow
			if len(report.QA.Templates) > 0 {
				entry.QAURL = report.QA.Templates[0].URL
			}
			for _, build := range report.Builds {
				if build.Ready && (report.TargetLine == "" || build.Line == report.TargetLine) {
					entry.Builds = append(entry.Builds, dailyReadinessBuild{Label: build.Label, Reference: build.Server.Reference, AgentReference: build.Agent.Reference, ServerDigest: build.Server.PlatformDigest, AgentDigest: build.Agent.PlatformDigest})
				}
			}
		}
		if !s.update(id, func(r *dailyReadinessReport) {
			r.Entries[i] = entry
			r.Checked++
			r.Remaining = r.Total - r.Checked
			if !entry.Complete {
				r.Incomplete++
			}
			if entry.Closed || entry.QANone {
				if entry.NeedsPlan {
					r.NeedsPlan--
				}
				return
			}
			if entry.Verdict == "ready" {
				r.Ready++
			}
			if entry.QAState == "found" {
				r.QAFound++
			} else if entry.QAComplete {
				r.QAMissing++
			} else {
				r.QAUnknown++
			}
		}) {
			break
		}
	}
	state := s.snapshot()
	r := state.Report
	switch {
	case ctx.Err() != nil:
		s.finish(id, "cancelled", "Scan stopped. Completed observations are saved; remaining issues are unchecked.")
	case r.Remaining > 0:
		s.finish(id, "partial", fmt.Sprintf("Checked at most %d issues per scan. Narrow My Work to scan the rest.", dailyReadinessLimit))
	case r.Incomplete > 0:
		s.finish(id, "partial", "Some issues have missing or inaccessible evidence. See each result before choosing what to test.")
	default:
		s.finish(id, "completed", "")
	}
}
func (p *localControlPanel) dailyReadinessBackend() (*dailyReadinessService, error) {
	packages, err := p.testPackageService()
	if err != nil {
		return nil, err
	}
	verifier := p.prBuildVerifierBackend()
	radar := p.issueRadarBackend()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.dailyReadiness != nil {
		return p.dailyReadiness, nil
	}
	s, err := newDailyReadinessService(packages.root)
	if err != nil {
		return nil, err
	}
	s.scope = packages.myWork
	s.plans = packages.list
	s.fetch = func(ctx context.Context, c myWorkConfig) (myWorkSnapshot, error) {
		fresh, err := radar.pullMyWork(ctx, c)
		if err != nil {
			return fresh, err
		}
		_, err = packages.refreshMyWorkSnapshot(fresh)
		return fresh, err
	}
	s.scanFactory = func() func(context.Context, issueReadinessRequest) (issueReadinessReport, error) {
		cache := &readinessEvidenceCache{entries: map[string]readinessCachedEvidence{}}
		type observed struct {
			builds   []issueReadinessBuild
			warnings []string
		}
		observations := map[readinessSearchScope]observed{}
		return func(ctx context.Context, req issueReadinessRequest) (issueReadinessReport, error) {
			ctx = context.WithValue(ctx, readinessCacheKey{}, cache)
			return verifier.checkIssue(ctx, req, func(ctx context.Context) ([]issueReadinessBuild, []string) {
				scope, _ := ctx.Value(readinessScopeKey{}).(readinessSearchScope)
				if saved, ok := observations[scope]; ok {
					readinessNotify(ctx, "Reusing this scan’s observed head images for the same release.")
					return saved.builds, saved.warnings
				}
				builds, warnings := verifier.readinessBuilds(ctx)
				if ctx.Err() == nil {
					observations[scope] = observed{builds, warnings}
				}
				return builds, warnings
			})
		}
	}
	p.dailyReadiness = s
	return s, nil
}
func (p *localControlPanel) stopDailyReadiness() {
	p.mu.Lock()
	s := p.dailyReadiness
	p.mu.Unlock()
	if s != nil {
		s.mu.Lock()
		if s.cancel != nil {
			s.cancel()
		}
		s.mu.Unlock()
	}
}
func (p *localControlPanel) handleDailyReadiness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var req dailyReadinessRequest
	if r.Method == http.MethodGet {
		if !p.authorizedLocalBrowserRead(r) {
			http.Error(w, "unauthorized", 401)
			return
		}
	} else if !p.issueRadarRequest(w, r, &req) {
		return
	}
	s, err := p.dailyReadinessBackend()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if r.Method == http.MethodPost {
		if err = s.change(req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
	}
	writeJSON(w, s.snapshot())
}
