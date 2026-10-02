package test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/cachelab"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type myWorkConfig struct {
	Repo      string `json:"repo"`
	Milestone int    `json:"milestone"`
	Scope     string `json:"scope"`
	User      string `json:"user"`
	Label     string `json:"label"`
}
type myWorkPoint struct {
	At     time.Time `json:"at"`
	Open   int       `json:"open"`
	Closed int       `json:"closed"`
}
type myWorkSnapshot struct {
	Config      myWorkConfig        `json:"config"`
	Milestone   issueRadarMilestone `json:"milestone"`
	GeneratedAt time.Time           `json:"generatedAt"`
	Issues      []issueRadarIssue   `json:"issues"`
	Departed    []issueRadarIssue   `json:"departed"`
	History     []myWorkPoint       `json:"history"`
	BucketID    string              `json:"bucketId"`
	Created     int                 `json:"created"`
}

func normalizeMyWorkConfig(c myWorkConfig) (myWorkConfig, error) {
	var err error
	c.Repo, err = issueRadarRepo(c.Repo)
	if err != nil {
		return c, err
	}
	c.Repo = strings.ToLower(c.Repo)
	if c.Milestone <= 0 || !packageEnum(c.Scope, "mine", "unassigned", "all") {
		return c, fmt.Errorf("choose a milestone and owner scope")
	}
	c.User = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(c.User), "@"))
	if c.User != "" && (!issueRadarLoginPattern.MatchString(c.User) || strings.Contains(c.User, "--")) {
		return c, fmt.Errorf("enter a valid GitHub username")
	}
	labels := []string{}
	for _, label := range strings.Split(c.Label, ",") {
		label = strings.TrimSpace(label)
		if label == "" {
			continue
		}
		if !packageText(label, 100, true) || strings.ContainsAny(label, "\r\n") {
			return c, fmt.Errorf("enter valid labels")
		}
		labels = append(labels, label)
	}
	if len(labels) > 8 {
		return c, fmt.Errorf("choose at most eight labels")
	}
	c.Label = strings.Join(labels, ",")
	if c.Scope != "mine" {
		c.User = ""
	}
	return c, nil
}
func (s *issueRadarService) pullMyWork(ctx context.Context, c myWorkConfig) (myWorkSnapshot, error) {
	out := myWorkSnapshot{Config: c, GeneratedAt: time.Now().UTC(), Issues: []issueRadarIssue{}, Departed: []issueRadarIssue{}, History: []myWorkPoint{}}
	if c.Scope == "mine" && c.User == "" {
		if err := s.githubGet(ctx, "user", ".login", &c.User); err != nil {
			return out, err
		}
		if !issueRadarLoginPattern.MatchString(c.User) {
			return out, fmt.Errorf("GitHub returned an invalid username")
		}
		out.Config.User = strings.ToLower(c.User)
	}
	if err := s.githubGet(ctx, fmt.Sprintf("repos/%s/milestones/%d", c.Repo, c.Milestone), "{number,title,state,due_on}", &out.Milestone); err != nil {
		return out, err
	}
	if out.Milestone.Number != c.Milestone || !packageText(out.Milestone.Title, 200, true) {
		return out, fmt.Errorf("GitHub returned an invalid milestone")
	}
	q := url.Values{"state": {"all"}, "milestone": {strconv.Itoa(c.Milestone)}, "per_page": {"100"}, "sort": {"created"}, "direction": {"asc"}}
	if c.Label != "" {
		q.Set("labels", c.Label)
	}
	if c.Scope == "mine" {
		q.Set("assignee", out.Config.User)
	}
	if c.Scope == "unassigned" {
		q.Set("assignee", "none")
	}
	seen := map[int]bool{}
	for page := 1; page <= 100; page++ {
		q.Set("page", strconv.Itoa(page))
		var batch []issueRadarIssue
		if err := s.githubGet(ctx, "repos/"+c.Repo+"/issues?"+q.Encode(), "map({number,title,html_url,state,state_reason,body:((.body // \"\")[0:1200]),created_at,updated_at,closed_at,pull_request,labels:[.labels[]|{name}],assignees:[.assignees[]|{login}],milestone:(.milestone|if . == null then null else {number,title,state,due_on} end)})", &batch); err != nil {
			return out, err
		}
		for _, issue := range batch {
			if issue.PullRequest != nil || issue.Number <= 0 || seen[issue.Number] {
				continue
			}
			issue.URL = fmt.Sprintf("https://github.com/%s/issues/%d", c.Repo, issue.Number)
			seen[issue.Number] = true
			out.Issues = append(out.Issues, issue)
		}
		if len(batch) < 100 {
			return out, nil
		}
	}
	return out, fmt.Errorf("milestone exceeds the 100-page limit; narrow its scope")
}
func (s *testPackageService) myWorkLocked() (myWorkSnapshot, error) {
	var out myWorkSnapshot
	raw, err := testPackageReadBounded(filepath.Join(s.root, ".my-work.json"), 16<<20)
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	err = decodeTestPackage(raw, &out)
	return out, err
}
func (s *testPackageService) myWork() (myWorkSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.myWorkLocked()
}

// Refresh is additive: existing plans and manually organized packages are never rewritten.
func (s *testPackageService) prepareMyWork(out myWorkSnapshot) (myWorkSnapshot, error) {
	return s.prepareTrackedWork(out, true)
}

func (s *testPackageService) prepareTrackedWork(out myWorkSnapshot, active bool) (myWorkSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, err := s.myWorkLocked()
	if err != nil {
		return out, err
	}
	lib := s.libraryLocked()
	bucketIndex := -1
	for i, b := range lib.Buckets {
		if strings.EqualFold(b.SourceRepo, out.Config.Repo) && b.SourceMilestone == out.Config.Milestone {
			bucketIndex = i
			break
		}
	}
	newBucket := bucketIndex < 0
	if newBucket {
		if len(lib.Buckets) >= 100 {
			return out, fmt.Errorf("bucket limit reached (100)")
		}
		name := ""
		nameRunes := []rune(out.Config.Repo + " · " + out.Milestone.Title)
		for len(string(nameRunes)) > 120 {
			nameRunes = nameRunes[:len(nameRunes)-1]
		}
		name = string(nameRunes)
		lib.Buckets = append(lib.Buckets, testPackageBucket{ID: cachelab.ID(), Name: name, PackageIDs: []string{}, SourceRepo: out.Config.Repo, SourceMilestone: out.Config.Milestone})
		bucketIndex = len(lib.Buckets) - 1
	}
	out.BucketID = lib.Buckets[bucketIndex].ID
	if newBucket {
		urls := map[string]bool{}
		for _, issue := range out.Issues {
			urls[strings.ToLower(issue.URL)] = true
		}
		remaining := []string{}
		for _, id := range lib.Unfiled {
			pkg := s.packages[id]
			if urls[strings.ToLower(strings.TrimSuffix(strings.TrimSpace(pkg.IssueURL), "/"))] {
				lib.Buckets[bucketIndex].PackageIDs = append(lib.Buckets[bucketIndex].PackageIDs, id)
			} else {
				remaining = append(remaining, id)
			}
		}
		lib.Unfiled = remaining
	}
	known := map[string]bool{}
	for _, pkg := range s.packages {
		known[strings.ToLower(strings.TrimSuffix(strings.TrimSpace(pkg.IssueURL), "/"))] = true
	}
	needed := 0
	for _, issue := range out.Issues {
		if issue.State == "open" && !issueQANone(issue) && !known[strings.ToLower(issue.URL)] {
			needed++
		}
	}
	if len(s.packages)+needed > 500 {
		return out, fmt.Errorf("milestone needs %d starter packages; library limit is 500. Narrow the scope", needed)
	}
	created := []string{}
	rollback := func() {
		for _, id := range created {
			delete(s.packages, id)
			_ = os.RemoveAll(filepath.Join(s.root, id))
		}
	}
	for _, issue := range out.Issues {
		key := strings.ToLower(issue.URL)
		if issue.State != "open" || issueQANone(issue) || known[key] {
			continue
		}
		now := time.Now().UTC()
		pkg := testPackage{ID: cachelab.ID(), Revision: cachelab.ID(), Title: issue.Title, IssueURL: issue.URL, IssueTitle: issue.Title, Summary: issue.Body, Status: "planning", Cases: []testPackageCase{}, Sessions: []testPackageSession{}, CreatedAt: now, UpdatedAt: now}
		if err := s.saveLocked(pkg); err != nil {
			_ = os.RemoveAll(filepath.Join(s.root, pkg.ID))
			rollback()
			return out, err
		}
		created = append(created, pkg.ID)
		known[key] = true
		lib.Buckets[bucketIndex].PackageIDs = append(lib.Buckets[bucketIndex].PackageIDs, pkg.ID)
	}
	out.Created = len(created)
	out = mergeMyWorkSnapshot(previous, out)
	lib.Revision = cachelab.ID()
	if err := validatePackageLibrary(lib, s.packages); err != nil {
		rollback()
		return out, err
	}
	raw, err := json.MarshalIndent(lib, "", "  ")
	if err != nil {
		rollback()
		return out, err
	}
	if err = writePrivateConfigAtomically(filepath.Join(s.root, ".library.json"), raw); err != nil {
		rollback()
		return out, err
	}
	s.library = lib
	if !active {
		return out, nil
	}
	raw, err = json.MarshalIndent(out, "", "  ")
	if err == nil {
		err = writePrivateConfigAtomically(filepath.Join(s.root, ".my-work.json"), raw)
	}
	if err != nil {
		return out, fmt.Errorf("packages were prepared, but the work snapshot could not be saved; retry refresh: %w", err)
	}
	return out, nil
}
func (p *localControlPanel) handleMyWork(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		if !p.authorizedLocalBrowserRead(r) {
			http.Error(w, "unauthorized", 401)
			return
		}
		s, err := p.testPackageService()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		out, err := s.myWork()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, map[string]any{"snapshot": out})
		return
	}
	var c myWorkConfig
	if !p.issueRadarRequest(w, r, &c) {
		return
	}
	c, err := normalizeMyWorkConfig(c)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	out, err := p.issueRadarBackend().pullMyWork(ctx, c)
	if err != nil {
		issueRadarError(w, err)
		return
	}
	s, err := p.testPackageService()
	if err == nil {
		out, err = s.prepareMyWork(out)
	}
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, map[string]any{"snapshot": out})
}

func mergeMyWorkSnapshot(previous, out myWorkSnapshot) myWorkSnapshot {
	if previous.Config == out.Config {
		out.History = previous.History
		current := map[int]bool{}
		for _, issue := range out.Issues {
			current[issue.Number] = true
		}
		seen := map[int]bool{}
		for _, issue := range append(previous.Issues, previous.Departed...) {
			if !current[issue.Number] && !seen[issue.Number] {
				out.Departed = append(out.Departed, issue)
				seen[issue.Number] = true
			}
		}
	}
	point := myWorkPoint{At: out.GeneratedAt}
	for _, issue := range out.Issues {
		if issue.State == "closed" {
			point.Closed++
		} else if !issueQANone(issue) {
			point.Open++
		}
	}
	out.History = append(out.History, point)
	if len(out.History) > 60 {
		out.History = out.History[len(out.History)-60:]
	}
	return out
}

// Refresh scope metadata only: saved plans and bucket placement are independent history.
func (s *testPackageService) refreshMyWorkSnapshot(out myWorkSnapshot) (myWorkSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, err := s.myWorkLocked()
	if err != nil {
		return out, err
	}
	if previous.Config != out.Config {
		return out, fmt.Errorf("My Work scope changed during refresh; refresh the current scope again")
	}
	if previous.GeneratedAt.After(out.GeneratedAt) {
		return previous, nil
	}
	out.BucketID = previous.BucketID
	out.Created = 0
	out = mergeMyWorkSnapshot(previous, out)
	raw, err := json.MarshalIndent(out, "", "  ")
	if err == nil {
		err = writePrivateConfigAtomically(filepath.Join(s.root, ".my-work.json"), raw)
	}
	return out, err
}
func (p *localControlPanel) handleRefreshMyWork(w http.ResponseWriter, r *http.Request) {
	var req struct{}
	if !p.issueRadarRequest(w, r, &req) {
		return
	}
	s, err := p.testPackageService()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	saved, err := s.myWork()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if saved.Config.Milestone == 0 {
		writeJSON(w, map[string]any{"snapshot": saved})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	fresh, err := p.issueRadarBackend().pullMyWork(ctx, saved.Config)
	if err != nil {
		issueRadarError(w, err)
		return
	}
	fresh, err = s.refreshMyWorkSnapshot(fresh)
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	writeJSON(w, map[string]any{"snapshot": fresh})
}
