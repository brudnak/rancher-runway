package test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Issue Radar only reads GitHub. Credentials stay in the existing gh CLI login.
type issueRadarService struct{ runCommand imageLookupCommandRunner }

type issueRadarConfig struct {
	Repo        string   `json:"repo"`
	Label       string   `json:"label"`
	Milestone   string   `json:"milestone"`
	NoMilestone bool     `json:"noMilestone"`
	Users       []string `json:"users"`
}

type issueRadarMilestone struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
	DueOn  string `json:"due_on"`
}

type issueRadarIssue struct {
	Number      int       `json:"number"`
	Title       string    `json:"title"`
	URL         string    `json:"html_url"`
	State       string    `json:"state"`
	Body        string    `json:"body,omitempty"`
	CreatedAt   string    `json:"created_at"`
	UpdatedAt   string    `json:"updated_at"`
	ClosedAt    string    `json:"closed_at,omitempty"`
	PullRequest *struct{} `json:"pull_request,omitempty"`
	Labels      []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Assignees []struct {
		Login string `json:"login"`
	} `json:"assignees"`
	Milestone *issueRadarMilestone `json:"milestone"`
}

type issueRadarSnapshot struct {
	Config      issueRadarConfig  `json:"config"`
	GeneratedAt time.Time         `json:"generatedAt"`
	Issues      []issueRadarIssue `json:"issues"`
}

var issueRadarRepoPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9_.-]{1,100}$`)
var issueRadarLoginPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)

func issueRadarRepo(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !issueRadarRepoPattern.MatchString(value) || strings.HasSuffix(value, "/.") || strings.HasSuffix(value, "/..") {
		return "", &imageLookupInputError{message: "Enter a GitHub repository as owner/repo."}
	}
	return value, nil
}

func normalizeIssueRadarConfig(config issueRadarConfig) (issueRadarConfig, error) {
	var err error
	config.Repo, err = issueRadarRepo(config.Repo)
	if err != nil {
		return config, err
	}
	config.Milestone = strings.TrimSpace(config.Milestone)
	if len(config.Milestone) > 200 || strings.ContainsAny(config.Milestone, "\r\n\x00") {
		return config, &imageLookupInputError{message: "Enter a milestone title of at most 200 characters."}
	}
	if config.NoMilestone && config.Milestone != "" {
		return config, &imageLookupInputError{message: "Choose a milestone or issues without a milestone, not both."}
	}
	labels := []string{}
	seen := map[string]bool{}
	for _, label := range strings.Split(config.Label, ",") {
		label = strings.TrimSpace(label)
		if label == "" {
			continue
		}
		if len(label) > 100 || strings.ContainsAny(label, "\r\n\x00") {
			return config, &imageLookupInputError{message: "Each team label must be at most 100 characters on one line."}
		}
		if !seen[strings.ToLower(label)] {
			labels = append(labels, label)
			seen[strings.ToLower(label)] = true
		}
	}
	if len(labels) == 0 || len(labels) > 8 {
		return config, &imageLookupInputError{message: "Enter one to eight comma-separated team labels. Issues must match every label."}
	}
	config.Label = strings.Join(labels, ",")
	users := []string{}
	seen = map[string]bool{}
	for _, user := range config.Users {
		user = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(user), "@"))
		if user == "" {
			continue
		}
		if !issueRadarLoginPattern.MatchString(user) || strings.Contains(user, "--") {
			return config, &imageLookupInputError{message: "Enter GitHub usernames using letters, numbers, and single hyphens."}
		}
		if !seen[user] {
			users = append(users, user)
			seen[user] = true
		}
	}
	if len(users) == 0 || len(users) > 8 {
		return config, &imageLookupInputError{message: "Add one to eight GitHub usernames to compare assignments."}
	}
	config.Users = users
	return config, nil
}

func (s *issueRadarService) githubGet(ctx context.Context, endpoint, projection string, result any) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	args := []string{"api", "--include", "--hostname", "github.com", "--method", "GET",
		"-H", "Accept:application/vnd.github+json", "-H", "X-GitHub-Api-Version:2022-11-28", endpoint, "--jq", projection}
	raw, err := s.runCommand(ctx, "gh", args, imageLookupSanitizedGHEnvironment(), 2<<20)
	status, payload := parsePRBuildGitHubIncludedResponse(raw)
	if ctx.Err() != nil {
		return fmt.Errorf("GitHub issue lookup timed out or was cancelled: %w", ctx.Err())
	}
	if status >= 400 {
		return &prBuildGitHubHTTPError{status: status, operation: "issue board"}
	}
	if errors.Is(err, errImageLookupCommandOutputLimit) {
		return errors.New("GitHub issue response was too large; narrow the team labels or milestone.")
	}
	if err != nil {
		return errors.New("Could not read GitHub issues. Install GitHub CLI (brew install gh), then run gh auth login with access to this repository.")
	}
	if err := json.Unmarshal(payload, result); err != nil {
		return errors.New("GitHub returned invalid issue metadata. Try refreshing the board.")
	}
	return nil
}

func (s *issueRadarService) milestones(ctx context.Context, repo string) ([]issueRadarMilestone, error) {
	items := []issueRadarMilestone{}
	for page := 1; page <= 20; page++ {
		var batch []issueRadarMilestone
		endpoint := fmt.Sprintf("repos/%s/milestones?state=all&per_page=100&page=%d&sort=due_on&direction=desc", repo, page)
		if err := s.githubGet(ctx, endpoint, "map({number,title,state,due_on})", &batch); err != nil {
			return nil, err
		}
		items = append(items, batch...)
		if len(batch) < 100 {
			return items, nil
		}
	}
	return nil, errors.New("This repository has too many milestones to load completely.")
}

func (s *issueRadarService) issues(ctx context.Context, config issueRadarConfig, closedUser string, limit int) ([]issueRadarIssue, error) {
	query := url.Values{"state": {"open"}, "labels": {config.Label}, "per_page": {"100"}, "sort": {"created"}, "direction": {"asc"}}
	if closedUser != "" {
		query.Set("state", "closed")
		query.Set("assignee", closedUser)
		query.Set("sort", "updated")
		query.Set("direction", "desc")
	} else if config.NoMilestone {
		query.Set("milestone", "none")
	} else if config.Milestone != "" {
		milestones, err := s.milestones(ctx, config.Repo)
		if err != nil {
			return nil, err
		}
		for _, milestone := range milestones {
			if milestone.Title == config.Milestone {
				query.Set("milestone", strconv.Itoa(milestone.Number))
				break
			}
		}
		if query.Get("milestone") == "" {
			return nil, &imageLookupInputError{message: fmt.Sprintf("Milestone %q was not found in %s. Load milestones to choose an exact title.", config.Milestone, config.Repo)}
		}
	}
	projection := "map({number,title,html_url,state,created_at,updated_at,closed_at,pull_request,labels:[.labels[]|{name}],assignees:[.assignees[]|{login}],milestone:(.milestone|if . == null then null else {number,title,state,due_on} end)})"
	if closedUser != "" {
		projection = strings.Replace(projection, "number,title,html_url", "number,title,html_url,body:((.body // \"\")[0:1200])", 1)
	}
	items := []issueRadarIssue{}
	seen := map[int]bool{}
	for page := 1; page <= 100; page++ {
		query.Set("page", strconv.Itoa(page))
		var batch []issueRadarIssue
		if err := s.githubGet(ctx, "repos/"+config.Repo+"/issues?"+query.Encode(), projection, &batch); err != nil {
			return nil, err
		}
		for _, issue := range batch {
			if issue.PullRequest != nil || issue.Number <= 0 || seen[issue.Number] {
				continue
			}
			// Reconstruct links from the validated repository and issue number.
			issue.URL = fmt.Sprintf("https://github.com/%s/issues/%d", config.Repo, issue.Number)
			seen[issue.Number] = true
			items = append(items, issue)
			if limit > 0 && len(items) >= limit {
				return items, nil
			}
		}
		if len(batch) < 100 {
			return items, nil
		}
	}
	return nil, errors.New("The issue board reached the 100-page fetch limit. Narrow the labels or milestone; no partial report was generated.")
}

func (p *localControlPanel) issueRadarBackend() *issueRadarService {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.issueRadar == nil {
		p.issueRadar = &issueRadarService{runCommand: imageLookupExecCommand}
	}
	return p.issueRadar
}

func (p *localControlPanel) issueRadarRequest(w http.ResponseWriter, r *http.Request, payload any, limits ...int64) bool {
	w.Header().Set("Cache-Control", "no-store")
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return false
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	limit := int64(64 << 10)
	if len(limits) > 0 {
		limit = limits[0]
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(payload); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "Issue Radar request is too large.", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "invalid JSON request", http.StatusBadRequest)
		}
		return false
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		http.Error(w, "request body must contain exactly one JSON object", http.StatusBadRequest)
		return false
	}
	return true
}

func issueRadarError(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), prBuildHTTPStatus(err))
}

func (p *localControlPanel) handleIssueRadar(w http.ResponseWriter, r *http.Request) {
	var config issueRadarConfig
	if !p.issueRadarRequest(w, r, &config) {
		return
	}
	config, err := normalizeIssueRadarConfig(config)
	if err != nil {
		issueRadarError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	issues, err := p.issueRadarBackend().issues(ctx, config, "", 0)
	if err != nil {
		issueRadarError(w, err)
		return
	}
	writeJSON(w, issueRadarSnapshot{Config: config, GeneratedAt: time.Now().UTC(), Issues: issues})
}

func (p *localControlPanel) handleIssueRadarMilestones(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Repo string `json:"repo"`
	}
	if !p.issueRadarRequest(w, r, &payload) {
		return
	}
	repo, err := issueRadarRepo(payload.Repo)
	if err != nil {
		issueRadarError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	milestones, err := p.issueRadarBackend().milestones(ctx, repo)
	if err != nil {
		issueRadarError(w, err)
		return
	}
	writeJSON(w, map[string]any{"repo": repo, "milestones": milestones})
}

func (p *localControlPanel) handleIssueRadarHistory(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Config issueRadarConfig `json:"config"`
		Limit  int              `json:"limit"`
	}
	if !p.issueRadarRequest(w, r, &payload) {
		return
	}
	config, err := normalizeIssueRadarConfig(payload.Config)
	if err != nil {
		issueRadarError(w, err)
		return
	}
	if payload.Limit != 30 && payload.Limit != 50 {
		http.Error(w, "Choose 30 or 50 history samples per owner.", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	history := map[string][]issueRadarIssue{}
	warnings := []string{}
	for _, user := range config.Users {
		issues, err := p.issueRadarBackend().issues(ctx, config, user, payload.Limit)
		if err != nil {
			if ctx.Err() != nil {
				issueRadarError(w, ctx.Err())
				return
			}
			warnings = append(warnings, fmt.Sprintf("History for @%s could not be loaded: %s", user, err))
			continue
		}
		history[user] = issues
	}
	writeJSON(w, map[string]any{"history": history, "warnings": warnings, "generatedAt": time.Now().UTC(), "limit": payload.Limit})
}

func (p *localControlPanel) handleIssueRadarSave(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Content string `json:"content"`
	}
	if !p.issueRadarRequest(w, r, &payload, 8<<20) {
		return
	}
	if strings.TrimSpace(payload.Content) == "" {
		http.Error(w, "Generate an issue report before saving.", http.StatusBadRequest)
		return
	}
	path, err := saveDownloadFile("issue-radar-report.md", []byte(payload.Content), 0o600)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"filename": filepath.Base(path), "path": path})
}
