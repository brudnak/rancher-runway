package test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/cachelab"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type releaseCheckpoint struct {
	Kind      string `json:"kind,omitempty"`
	Disabled  bool   `json:"disabled,omitempty"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	Date      string `json:"date"`
	Milestone string `json:"milestone"` // Empty means shared across the release.
	Tentative bool   `json:"tentative"`
	Source    string `json:"source"`
}
type releaseMilestone struct {
	Config   myWorkConfig    `json:"config"`
	Snapshot *myWorkSnapshot `json:"snapshot,omitempty"`
}
type releaseTrackedIssue struct {
	Repo        string           `json:"repo"`
	Number      int              `json:"number"`
	Snapshot    *issueRadarIssue `json:"snapshot,omitempty"`
	RefreshedAt time.Time        `json:"refreshedAt"`
}
type releaseChange struct {
	URL    string `json:"url"`
	Number int    `json:"number"`
	Title  string `json:"title"`
	Reason string `json:"reason"`
}
type releaseTracker struct {
	TeamMembers  []string              `json:"teamMembers,omitempty"`
	TeamMe       string                `json:"teamMe,omitempty"`
	Changes      []releaseChange       `json:"changes,omitempty"`
	RefreshedAt  time.Time             `json:"refreshedAt,omitzero"`
	PlanRepo     string                `json:"planRepo,omitempty"`
	IssueLabel   string                `json:"issueLabel,omitempty"`
	PlanVersions []string              `json:"planVersions,omitempty"`
	Issues       []releaseTrackedIssue `json:"issues,omitempty"`
	Month        string                `json:"month,omitempty"`
	ID           string                `json:"id"`
	Name         string                `json:"name"`
	Notes        string                `json:"notes"`
	Archived     bool                  `json:"archived"`
	Holidays     []string              `json:"holidays"`
	Checkpoints  []releaseCheckpoint   `json:"checkpoints"`
	Milestones   []releaseMilestone    `json:"milestones"`
}
type releaseTrackerLibrary struct {
	Revision string           `json:"revision"`
	Trackers []releaseTracker `json:"trackers"`
}

func (s *issuePackageService) releaseTrackersLocked() (releaseTrackerLibrary, error) {
	out := releaseTrackerLibrary{Revision: "initial", Trackers: []releaseTracker{}}
	raw, err := issuePackageReadBounded(filepath.Join(s.root, ".release-trackers.json"), 64<<20)
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	err = decodeIssuePackage(raw, &out)
	return out, err
}
func (s *issuePackageService) releaseTrackers() (releaseTrackerLibrary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.releaseTrackersLocked()
}
func releaseMilestoneKey(c myWorkConfig) string { return fmt.Sprintf("%s#%d", c.Repo, c.Milestone) }
func validateReleaseTracker(t *releaseTracker) error {
	t.Name = strings.TrimSpace(t.Name)
	if len(t.TeamMembers) > 8 {
		return fmt.Errorf("choose up to eight team members")
	}
	for i, user := range t.TeamMembers {
		user = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(user), "@"))
		if !issueRadarLoginPattern.MatchString(user) || strings.Contains(user, "--") {
			return fmt.Errorf("invalid team GitHub username")
		}
		t.TeamMembers[i] = user
	}
	t.TeamMe = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(t.TeamMe), "@"))
	if t.TeamMe != "" && (!issueRadarLoginPattern.MatchString(t.TeamMe) || strings.Contains(t.TeamMe, "--")) {
		return fmt.Errorf("invalid personal GitHub username")
	}
	if t.Month != "" {
		if _, err := time.Parse("2006-01", t.Month); err != nil {
			return fmt.Errorf("choose a valid release month")
		}
	}
	if !cachelab.IDPattern.MatchString(t.ID) || !packageText(t.Name, 120, true) || len(t.Notes) > 10000 || len(t.PlanVersions) > 30 || len(t.Issues) > 100 || len(t.Milestones) > 30 || len(t.Checkpoints) > 100 || len(t.Holidays) > 366 {
		return fmt.Errorf("invalid tracker or tracker limit exceeded")
	}
	for _, version := range t.PlanVersions {
		if !packageText(version, 100, true) {
			return fmt.Errorf("invalid planned version")
		}
	}
	if t.PlanRepo == "" {
		t.PlanRepo = "rancher/rancher"
	}
	config, err := normalizeMyWorkConfig(myWorkConfig{Repo: t.PlanRepo, Milestone: 1, Scope: "all", Label: t.IssueLabel})
	if err != nil {
		return err
	}
	t.PlanRepo = config.Repo
	t.IssueLabel = config.Label
	issueKeys := map[string]bool{}
	for i := range t.Issues {
		issue := &t.Issues[i]
		repo, err := issueRadarRepo(issue.Repo)
		if err != nil || issue.Number <= 0 {
			return fmt.Errorf("choose a valid repository and issue number")
		}
		issue.Repo = strings.ToLower(repo)
		key := fmt.Sprintf("%s#%d", issue.Repo, issue.Number)
		if issueKeys[key] {
			return fmt.Errorf("issue already tracked")
		}
		issueKeys[key] = true
	}
	date := func(v string) bool {
		d, e := time.Parse("2006-01-02", v)
		return e == nil && d.Year() >= 2000 && d.Year() <= 2200
	}
	seen := map[string]bool{}
	for i := range t.Milestones {
		m := &t.Milestones[i]
		c, e := normalizeMyWorkConfig(m.Config)
		if e != nil {
			return e
		}
		m.Config = c
		k := releaseMilestoneKey(c)
		if seen[k] {
			return fmt.Errorf("milestone already linked")
		}
		seen[k] = true
	}
	ids := map[string]bool{}
	for _, c := range t.Checkpoints {
		if c.ID == "" || len(c.ID) > 100 || ids[c.ID] || !packageText(c.Name, 120, true) || (!(c.Disabled && c.Date == "") && !date(c.Date)) || len(c.Kind) > 100 || len(c.Source) > 2000 || (c.Milestone != "" && !seen[c.Milestone]) {
			return fmt.Errorf("check checkpoint names, dates, and milestone assignments")
		}
		ids[c.ID] = true
	}
	for _, h := range t.Holidays {
		if !date(h) {
			return fmt.Errorf("holidays must use YYYY-MM-DD")
		}
	}
	return nil
}

// Resolve every planned version before fetching issues; never silently omit a release line.
func linkReleasePlan(t *releaseTracker, milestones []issueRadarMilestone) error {
	versionKey := func(s string) string { return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(s)), "v") }
	missing := []string{}
	links := append([]releaseMilestone{}, t.Milestones...)
	for _, version := range t.PlanVersions {
		matches := []issueRadarMilestone{}
		for _, milestone := range milestones {
			if versionKey(milestone.Title) == versionKey(version) {
				matches = append(matches, milestone)
			}
		}
		if len(matches) != 1 {
			missing = append(missing, version)
			continue
		}
		config := myWorkConfig{Repo: t.PlanRepo, Milestone: matches[0].Number, Scope: "all", Label: t.IssueLabel}
		found := false
		for i := range links {
			if releaseMilestoneKey(links[i].Config) == releaseMilestoneKey(config) {
				links[i].Config = config
				found = true
				break
			}
		}
		if !found {
			links = append(links, releaseMilestone{Config: config})
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("could not uniquely match planned milestones in %s: %s; check the repository and GitHub milestone names", t.PlanRepo, strings.Join(missing, ", "))
	}
	t.Milestones = links
	return validateReleaseTracker(t)
}

func (s *issuePackageService) saveReleaseTracker(revision string, tracker releaseTracker, remove bool) (releaseTrackerLibrary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	lib, err := s.releaseTrackersLocked()
	if err != nil {
		return lib, err
	}
	if revision != lib.Revision {
		return lib, fmt.Errorf("trackers changed in another view; reload before saving")
	}
	index := -1
	for i, t := range lib.Trackers {
		if t.ID == tracker.ID {
			index = i
			break
		}
	}
	if remove {
		if index < 0 {
			return lib, fmt.Errorf("tracker no longer exists")
		}
		lib.Trackers = append(lib.Trackers[:index], lib.Trackers[index+1:]...)
	} else {
		if err = validateReleaseTracker(&tracker); err != nil {
			return lib, err
		}
		if index < 0 {
			if len(lib.Trackers) >= 100 {
				return lib, fmt.Errorf("tracker limit reached (100)")
			}
			lib.Trackers = append(lib.Trackers, tracker)
		} else {
			lib.Trackers[index] = tracker
		}
	}
	lib.Revision = cachelab.ID()
	raw, err := json.MarshalIndent(lib, "", "  ")
	if err == nil && len(raw) > 64<<20 {
		return lib, fmt.Errorf("tracker storage exceeds 64 MB; reduce linked scopes")
	}
	if err == nil {
		err = writePrivateConfigAtomically(filepath.Join(s.root, ".release-trackers.json"), raw)
	}
	return lib, err
}
func (p *localControlPanel) handleReleaseTrackers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		if !p.authorizedLocalBrowserRead(r) {
			http.Error(w, "unauthorized", 401)
			return
		}
		s, e := p.issuePackageService()
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		lib, e := s.releaseTrackers()
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		writeJSON(w, lib)
		return
	}
	var req struct {
		ClearPackages bool                `json:"clearPackages"`
		Confirm       string              `json:"confirm"`
		Action        string              `json:"action"`
		Revision      string              `json:"revision"`
		Tracker       releaseTracker      `json:"tracker"`
		Issue         releaseTrackedIssue `json:"issue"`
	}
	if !p.issueRadarRequest(w, r, &req, 2<<20) {
		return
	}
	if !packageEnum(req.Action, "save", "refresh", "delete", "attach-saved-work", "add-issue", "scan", "reset") {
		http.Error(w, "unknown tracker action", 400)
		return
	}
	s, err := p.issuePackageService()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if req.Action == "reset" {
		if req.Confirm != "clear-my-work" {
			http.Error(w, "confirm clearing My Work", 400)
			return
		}
		p.mu.Lock()
		daily := p.dailyReadiness
		p.mu.Unlock()
		if daily != nil {
			daily.mu.Lock()
			defer daily.mu.Unlock()
			if daily.cancel != nil {
				http.Error(w, "Finish or cancel the readiness scan before clearing work", 409)
				return
			}
		}
		backup, e := s.clearReleaseWork(req.Revision, req.ClearPackages)
		if e != nil {
			http.Error(w, e.Error(), 409)
			return
		}
		if daily != nil {
			if e = daily.persistLocked(dailyReadinessState{Version: 1}); e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
		}
		writeJSON(w, map[string]any{"backup": backup})
		return
	}
	lib, err := s.releaseTrackers()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if lib.Revision != req.Revision {
		http.Error(w, "Trackers changed; reload before saving.", 409)
		return
	}
	var old *releaseTracker
	for i := range lib.Trackers {
		if lib.Trackers[i].ID == req.Tracker.ID {
			old = &lib.Trackers[i]
		}
	}
	if req.Action == "attach-saved-work" {
		if old == nil {
			http.Error(w, "tracker not found", 404)
			return
		}
		saved, e := s.myWork()
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		if saved.Config.Milestone == 0 {
			http.Error(w, "No saved My Work milestone to attach", 400)
			return
		}
		req.Tracker = *old
		link := releaseMilestone{Config: saved.Config, Snapshot: &saved}
		found := false
		for i, m := range req.Tracker.Milestones {
			if releaseMilestoneKey(m.Config) == releaseMilestoneKey(saved.Config) {
				req.Tracker.Milestones[i] = link
				found = true
				break
			}
		}
		if !found {
			req.Tracker.Milestones = append(req.Tracker.Milestones, link)
		}
		out, e := s.saveReleaseTracker(req.Revision, req.Tracker, false)
		if e != nil {
			http.Error(w, e.Error(), 409)
			return
		}
		writeJSON(w, out)
		return
	}
	if req.Action == "add-issue" {
		if old == nil || old.Archived {
			http.Error(w, "Choose an active tracker", 400)
			return
		}
		req.Tracker = *old
		req.Tracker.Milestones = append([]releaseMilestone{}, old.Milestones...)
		req.Tracker.Issues = append([]releaseTrackedIssue{}, old.Issues...)
		found := false
		for _, issue := range req.Tracker.Issues {
			if strings.EqualFold(issue.Repo, req.Issue.Repo) && issue.Number == req.Issue.Number {
				found = true
			}
		}
		if !found {
			req.Issue.Snapshot = nil
			req.Tracker.Issues = append(req.Tracker.Issues, req.Issue)
		}
	}
	if req.Action == "refresh" || req.Action == "scan" {
		if old == nil {
			http.Error(w, "tracker not found", 404)
			return
		}
		label := req.Tracker.IssueLabel
		team, me := req.Tracker.TeamMembers, req.Tracker.TeamMe
		req.Tracker = *old
		if req.Action == "scan" {
			req.Tracker.IssueLabel = label
			req.Tracker.TeamMembers = team
			req.Tracker.TeamMe = me
		}
		req.Tracker.Milestones = append([]releaseMilestone{}, old.Milestones...)
		req.Tracker.Issues = append([]releaseTrackedIssue{}, old.Issues...)
	}
	if req.Tracker.ID == "" {
		req.Tracker.ID = cachelab.ID()
	}
	if req.Action != "delete" {
		if err = validateReleaseTracker(&req.Tracker); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
		defer cancel()
		if len(req.Tracker.PlanVersions) > 0 && packageEnum(req.Action, "save", "refresh", "scan") {
			milestones, e := p.issueRadarBackend().milestones(ctx, req.Tracker.PlanRepo)
			if e != nil {
				issueRadarError(w, e)
				return
			}
			if e = linkReleasePlan(&req.Tracker, milestones); e != nil {
				http.Error(w, e.Error(), 400)
				return
			}
		}
		if req.Action == "scan" {
			for i := range req.Tracker.Milestones {
				req.Tracker.Milestones[i].Config.Scope = "all"
				req.Tracker.Milestones[i].Config.User = ""
				req.Tracker.Milestones[i].Config.Label = req.Tracker.IssueLabel
			}
		}
		for i := range req.Tracker.Issues {
			issue := &req.Tracker.Issues[i]
			issue.Snapshot = nil
			if old != nil {
				for _, previous := range old.Issues {
					if previous.Repo == issue.Repo && previous.Number == issue.Number {
						issue.Snapshot = previous.Snapshot
						issue.RefreshedAt = previous.RefreshedAt
						break
					}
				}
			}
			if issue.Snapshot != nil && req.Action != "refresh" && req.Action != "scan" {
				continue
			}
			var fresh issueRadarIssue
			endpoint := fmt.Sprintf("repos/%s/issues/%d", issue.Repo, issue.Number)
			if e := p.issueRadarBackend().githubGet(ctx, endpoint, "{number,title,html_url,state,state_reason,body,created_at,updated_at,closed_at,pull_request,labels:[.labels[]|{name}],assignees:[.assignees[]|{login}],milestone:(.milestone|if . == null then null else {number,title,state,due_on} end)}", &fresh); e != nil {
				issueRadarError(w, e)
				return
			}
			if fresh.Number != issue.Number || fresh.PullRequest != nil {
				http.Error(w, "Choose a GitHub issue, not a pull request", 400)
				return
			}
			fresh.URL = fmt.Sprintf("https://github.com/%s/issues/%d", issue.Repo, issue.Number)
			issue.Snapshot = &fresh
			issue.RefreshedAt = time.Now().UTC()
		}
		for i := range req.Tracker.Milestones {
			m := &req.Tracker.Milestones[i]
			m.Snapshot = nil
			if old != nil {
				for _, previous := range old.Milestones {
					if previous.Config == m.Config {
						m.Snapshot = previous.Snapshot
						break
					}
				}
			}
			if m.Snapshot == nil && req.Action == "save" {
				saved, e := s.myWork()
				if e != nil {
					http.Error(w, e.Error(), 500)
					return
				}
				if saved.Config == m.Config && saved.Config.Milestone > 0 {
					m.Snapshot = &saved
				}
			}
			if m.Snapshot != nil && req.Action != "refresh" && req.Action != "scan" {
				continue
			}
			fresh, e := p.issueRadarBackend().pullMyWork(ctx, m.Config)
			if e != nil {
				issueRadarError(w, e)
				return
			}
			previous := m.Snapshot
			if previous == nil {
				fresh, e = s.prepareTrackedWork(fresh, false)
				if e != nil {
					http.Error(w, e.Error(), 500)
					return
				}
			} else {
				fresh.BucketID = previous.BucketID
			}
			if previous != nil {
				fresh = mergeMyWorkSnapshot(*previous, fresh)
			}
			m.Config = fresh.Config
			m.Snapshot = &fresh
		}
		if old != nil && packageEnum(req.Action, "refresh", "scan") {
			req.Tracker.Changes = p.releaseChanges(ctx, *old, req.Tracker)
			req.Tracker.RefreshedAt = time.Now().UTC()
		}
	}
	out, err := s.saveReleaseTracker(req.Revision, req.Tracker, req.Action == "delete")
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	writeJSON(w, out)
}
