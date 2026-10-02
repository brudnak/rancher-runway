package test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	"github.com/brudnak/ha-rancher-rke2/internal/prbuild"
	"github.com/brudnak/ha-rancher-rke2/internal/server"

	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Issue Radar only reads GitHub. Credentials stay in the existing gh CLI login.
type issueRadarService struct{ runCommand imagelookup.CommandRunner }

type issueRadarConfig struct {
	Repo        string   `json:"repo"`
	Label       string   `json:"label"`
	Milestone   string   `json:"milestone"`
	NoMilestone bool     `json:"noMilestone"`
	Users       []string `json:"users"`
}

type issueRadarSnapshot struct {
	Config      issueRadarConfig  `json:"config"`
	GeneratedAt time.Time         `json:"generatedAt"`
	Issues      []issueRadarIssue `json:"issues"`
}

var issueRadarLoginPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)

func normalizeIssueRadarConfig(config issueRadarConfig) (issueRadarConfig, error) {
	var err error
	config.Repo, err = issueRadarRepo(config.Repo)
	if err != nil {
		return config, err
	}
	config.Milestone = strings.TrimSpace(config.Milestone)
	if len(config.Milestone) > 200 || strings.ContainsAny(config.Milestone, "\r\n\x00") {
		return config, &imagelookup.InputError{Message: "Enter a milestone title of at most 200 characters."}
	}
	if config.NoMilestone && config.Milestone != "" {
		return config, &imagelookup.InputError{Message: "Choose a milestone or issues without a milestone, not both."}
	}
	labels := []string{}
	seen := map[string]bool{}
	for _, label := range strings.Split(config.Label, ",") {
		label = strings.TrimSpace(label)
		if label == "" {
			continue
		}
		if len(label) > 100 || strings.ContainsAny(label, "\r\n\x00") {
			return config, &imagelookup.InputError{Message: "Each team label must be at most 100 characters on one line."}
		}
		if !seen[strings.ToLower(label)] {
			labels = append(labels, label)
			seen[strings.ToLower(label)] = true
		}
	}
	if len(labels) == 0 || len(labels) > 8 {
		return config, &imagelookup.InputError{Message: "Enter one to eight comma-separated team labels. Issues must match every label."}
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
			return config, &imagelookup.InputError{Message: "Enter GitHub usernames using letters, numbers, and single hyphens."}
		}
		if !seen[user] {
			users = append(users, user)
			seen[user] = true
		}
	}
	if len(users) == 0 || len(users) > 8 {
		return config, &imagelookup.InputError{Message: "Add one to eight GitHub usernames to compare assignments."}
	}
	config.Users = users
	return config, nil
}

func (s *issueRadarService) githubGet(ctx context.Context, endpoint, projection string, result any) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	args := []string{"api", "--include", "--hostname", "github.com", "--method", "GET",
		"-H", "Accept:application/vnd.github+json", "-H", "X-GitHub-Api-Version:2022-11-28", endpoint, "--jq", projection}
	raw, err := s.runCommand(ctx, "gh", args, imagelookup.SanitizedGHEnvironment(), 2<<20)
	status, payload := prbuild.ParsePRBuildGitHubIncludedResponse(raw)
	if ctx.Err() != nil {
		return fmt.Errorf("GitHub issue lookup timed out or was cancelled: %w", ctx.Err())
	}
	if status >= 400 {
		return &prbuild.GitHubHTTPError{Status: status, Operation: "issue board"}
	}
	if errors.Is(err, imagelookup.ErrImageLookupCommandOutputLimit) {
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
			return nil, &imagelookup.InputError{Message: fmt.Sprintf("Milestone %q was not found in %s. Load milestones to choose an exact title.", config.Milestone, config.Repo)}
		}
	}
	projection := "map({number,title,html_url,state,state_reason,body:((.body // \"\")[0:1200]),created_at,updated_at,closed_at,pull_request,labels:[.labels[]|{name}],assignees:[.assignees[]|{login}],milestone:(.milestone|if . == null then null else {number,title,state,due_on} end)})"
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
		p.issueRadar = &issueRadarService{runCommand: imagelookup.ExecCommand}
	}
	return p.issueRadar
}

func (p *localControlPanel) issueRadarRequest(w http.ResponseWriter, r *http.Request, payload any, limits ...int64) bool {
	return server.ReadIssueRequest(w, r, payload, p.authorizedLocalAction, limits...)
}

func issueRadarError(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), prbuild.HTTPStatus(err))
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
	writeJSON(w, map[string]any{"config": config, "history": history, "warnings": warnings, "generatedAt": time.Now().UTC(), "limit": payload.Limit})
}

func (p *localControlPanel) handleIssueRadarSave(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Content string `json:"content"`
		Kind    string `json:"kind,omitempty"`
	}
	if !p.issueRadarRequest(w, r, &payload, 8<<20) {
		return
	}
	if strings.TrimSpace(payload.Content) == "" {
		http.Error(w, "Generate an issue report before saving.", http.StatusBadRequest)
		return
	}
	filename := "issue-radar-report.md"
	if payload.Kind == "prompt" {
		filename = "issue-radar-assignment-prompt.md"
	} else if payload.Kind != "" && payload.Kind != "report" {
		http.Error(w, "Choose a report or assignment prompt export.", http.StatusBadRequest)
		return
	}
	path, err := saveDownloadFile(filename, []byte(payload.Content), 0o600)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"filename": filepath.Base(path), "path": path})
}

type issueRadarIssue = prbuild.Issue
type issueRadarMilestone = prbuild.Milestone

func issueRadarRepo(value string) (string, error) { return prbuild.NormalizeRepository(value) }
func issueQANone(issue issueRadarIssue) bool      { return prbuild.IssueQANone(issue) }
