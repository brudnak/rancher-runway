package test

// Git is an optional, explicitly reviewed product action. This implementation
// never uses a shell and never operates in the user's working repository.
import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/cachelab"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type issuePackageGitCommand struct {
	Directory string   `json:"directory"`
	Args      []string `json:"args"`
	Display   string   `json:"display"`
	Condition string   `json:"condition,omitempty"`
	step      string
}
type issuePackageGitFile struct {
	Path  string `json:"path"`
	Bytes int    `json:"bytes"`
}
type issuePackageGitConnection struct {
	Repository string `json:"repository"`
	Branch     string `json:"branch"`
	WebURL     string `json:"webUrl"`
}
type issuePackageGitHistory struct {
	At        time.Time `json:"at"`
	Operation string    `json:"operation"`
	Status    string    `json:"status"`
	Summary   string    `json:"summary"`
	Commit    string    `json:"commit,omitempty"`
	URL       string    `json:"url,omitempty"`
}
type issuePackageGitCommit struct {
	Commit  string `json:"commit"`
	Subject string `json:"subject"`
	At      string `json:"at"`
	URL     string `json:"url"`
}
type issuePackageGitSaved struct {
	Connection   issuePackageGitConnection       `json:"connection"`
	Explicit     bool                            `json:"explicit,omitempty"`
	Disabled     bool                            `json:"disabled,omitempty"`
	SourcePath   string                          `json:"sourcePath,omitempty"`
	Baselines    map[string]string               `json:"baselines,omitempty"`
	Pending      map[string]string               `json:"pending,omitempty"`
	PendingHead  string                          `json:"pendingHead,omitempty"`
	LocalHistory []issuePackageGitHistory        `json:"localHistory"`
	GitHistory   []issuePackageGitCommit         `json:"gitHistory"`
	CheckedAt    *time.Time                      `json:"checkedAt,omitempty"`
	CheckStatus  string                          `json:"checkStatus,omitempty"`
	CheckMessage string                          `json:"checkMessage,omitempty"`
	Previous     map[string]issuePackageGitSaved `json:"previous,omitempty"`
}
type issuePackageGitState struct {
	Version             int                             `json:"version"`
	DefaultConnection   issuePackageGitConnection       `json:"defaultConnection"`
	DefaultCheckedAt    *time.Time                      `json:"defaultCheckedAt,omitempty"`
	DefaultCheckStatus  string                          `json:"defaultCheckStatus,omitempty"`
	DefaultCheckMessage string                          `json:"defaultCheckMessage,omitempty"`
	OnboardingDismissed bool                            `json:"onboardingDismissed,omitempty"`
	Packages            map[string]issuePackageGitSaved `json:"packages"`
}
type issuePackageGitPreview struct {
	ReviewToken         string                   `json:"reviewToken"`
	Digest              string                   `json:"digest"`
	Kind                string                   `json:"kind"`
	Repository          string                   `json:"repository"`
	Branch              string                   `json:"branch"`
	WebURL              string                   `json:"webUrl"`
	CheckoutPath        string                   `json:"checkoutPath"`
	ExpiresAt           time.Time                `json:"expiresAt"`
	Commands            []issuePackageGitCommand `json:"commands"`
	Files               []issuePackageGitFile    `json:"files"`
	Warnings            []string                 `json:"warnings"`
	packageID, revision string
	contents            map[string][]byte
	baselines           map[string]string
	pending             map[string]string
	existing            bool
}
type issuePackageGitRunResult struct {
	Output   string
	ExitCode int
}
type issuePackageGitRunner func(context.Context, string, []string) (issuePackageGitRunResult, error)
type issuePackageGitService struct {
	mu       sync.Mutex
	packages *issuePackageService
	runner   issuePackageGitRunner
	reviews  map[string]issuePackageGitPreview
	now      func() time.Time
}

var issuePackageGitServices sync.Map

func (s *issuePackageService) handleIssuePackagePlanGit(ctx context.Context, req issuePackageRequest) (any, error) {
	value, _ := issuePackageGitServices.LoadOrStore(s, &issuePackageGitService{packages: s, runner: runIssuePackageGit, reviews: map[string]issuePackageGitPreview{}, now: time.Now})
	g := value.(*issuePackageGitService)
	g.mu.Lock()
	defer g.mu.Unlock()
	switch req.Action {
	case "plan-git-onboarding":
		return g.onboarding(req.ID)
	case "plan-git-onboarding-dismiss":
		return g.dismissOnboarding(req)
	case "plan-git-status":
		return g.status(req.ID)
	case "plan-git-link":
		return g.link(req)
	case "plan-git-unlink":
		return g.unlink(req.ID)
	case "plan-git-preview":
		return g.preview(req)
	case "plan-git-execute":
		return g.execute(ctx, req)
	case "plan-git-import":
		return g.importMarkdown(req)
	}
	return nil, fmt.Errorf("unknown test plan repository action")
}

var issuePackageGitName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$`)
var issuePackageGitBranch = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,149}$`)
var issuePackageGitSHA = regexp.MustCompile(`^(?:[a-f0-9]{40}|[a-f0-9]{64})$`)

func issuePackageGitRemote(raw, branch string) (issuePackageGitConnection, error) {
	raw = strings.TrimSpace(raw)
	remotePath, scheme := "", "https"
	switch {
	case strings.HasPrefix(raw, "git@github.com:"):
		remotePath = strings.TrimPrefix(raw, "git@github.com:")
		scheme = "ssh"
	case strings.Contains(raw, "://"):
		u, err := url.Parse(raw)
		if err != nil || u.Host != "github.com" || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
			return issuePackageGitConnection{}, fmt.Errorf("use a github.com HTTPS or SSH repository URL without credentials or query parameters")
		}
		if u.Scheme == "https" && u.User == nil {
			scheme = "https"
		} else if u.Scheme == "ssh" && u.User != nil && u.User.Username() == "git" {
			if _, present := u.User.Password(); present {
				return issuePackageGitConnection{}, fmt.Errorf("repository URLs cannot contain credentials")
			}
			scheme = "ssh"
		} else {
			return issuePackageGitConnection{}, fmt.Errorf("use HTTPS or SSH with the git user and existing local credentials")
		}
		remotePath = strings.TrimPrefix(u.Path, "/")
	default:
		remotePath = raw
	}
	remotePath = strings.TrimSuffix(strings.TrimSuffix(remotePath, "/"), ".git")
	parts := strings.Split(remotePath, "/")
	if len(parts) != 2 || !issuePackageGitName.MatchString(parts[0]) || !issuePackageGitName.MatchString(parts[1]) || strings.HasSuffix(parts[0], ".") || strings.HasSuffix(parts[1], ".") {
		return issuePackageGitConnection{}, fmt.Errorf("choose a GitHub repository as owner/repository or its HTTPS/SSH URL")
	}
	if branch == "" {
		branch = "main"
	}
	if !issuePackageGitBranch.MatchString(branch) || strings.Contains(branch, "..") || strings.Contains(branch, "//") || strings.HasSuffix(branch, "/") || strings.HasSuffix(branch, ".") {
		return issuePackageGitConnection{}, fmt.Errorf("choose a valid branch name")
	}
	for _, part := range strings.Split(branch, "/") {
		if strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return issuePackageGitConnection{}, fmt.Errorf("choose a valid branch name")
		}
	}
	web := "https://github.com/" + remotePath
	remote := web + ".git"
	if scheme == "ssh" {
		remote = "git@github.com:" + remotePath + ".git"
	}
	return issuePackageGitConnection{Repository: remote, Branch: branch, WebURL: web}, nil
}

func (g *issuePackageGitService) storageRoot() string {
	return filepath.Join(g.packages.root, ".plan-git")
}
func (g *issuePackageGitService) checkout(c issuePackageGitConnection) string {
	return filepath.Join(g.storageRoot(), "checkouts", issuePackageDigest([]byte(c.Repository + "\n" + c.Branch))[:24])
}
func (g *issuePackageGitService) loadState() (issuePackageGitState, error) {
	state := issuePackageGitState{Version: 1, Packages: map[string]issuePackageGitSaved{}}
	if err := issuePackageGitCheckPath(g.packages.root, filepath.Join(".plan-git", "state.json"), true); err != nil {
		if os.IsNotExist(err) {
			return state, nil
		}
		return state, err
	}
	raw, err := issuePackageReadBounded(filepath.Join(g.storageRoot(), "state.json"), 2<<20)
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if err = decodeIssuePackage(raw, &state); err != nil || state.Version != 1 || state.Packages == nil {
		return state, fmt.Errorf("saved repository history is invalid; its file was preserved")
	}
	return state, nil
}
func (g *issuePackageGitService) saveState(state issuePackageGitState) error {
	if err := issuePackageGitSafeDirectory(g.packages.root, ".plan-git"); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if len(raw) > 2<<20 {
		return fmt.Errorf("repository history exceeds its storage limit")
	}
	return writePrivateConfigAtomically(filepath.Join(g.storageRoot(), "state.json"), raw)
}
func (g *issuePackageGitService) status(id string) (map[string]any, error) {
	if id != "" {
		if _, err := g.packages.get(id); err != nil {
			return nil, err
		}
	}
	state, err := g.loadState()
	if err != nil {
		return nil, err
	}
	c, scope, linkScope := issuePackageGitEffective(state, id)
	out := map[string]any{"connection": nil, "connectionScope": scope, "linkScope": linkScope, "localHistory": []issuePackageGitHistory{}, "gitHistory": []issuePackageGitCommit{}, "markdownFiles": []issuePackageGitFile{}, "checkedAt": nil, "checkStatus": "not-checked", "checkMessage": "Read access has not been checked."}
	saved := issuePackageGitActivate(state.Packages[id], c)
	if saved.LocalHistory != nil {
		out["localHistory"] = saved.LocalHistory
	}
	out["gitHistory"] = issuePackageGitAllHistory(saved)
	if c.Repository == "" {
		return out, nil
	}
	if _, err := issuePackageGitRemote(c.Repository, c.Branch); err != nil {
		return nil, err
	}
	out["connection"] = c
	checkedAt, checkStatus, checkMessage := saved.CheckedAt, saved.CheckStatus, saved.CheckMessage
	if scope == "default" {
		checkedAt, checkStatus, checkMessage = state.DefaultCheckedAt, state.DefaultCheckStatus, state.DefaultCheckMessage
	}
	if checkedAt != nil {
		out["checkedAt"] = checkedAt
	}
	if checkStatus != "" {
		out["checkStatus"] = checkStatus
		out["checkMessage"] = checkMessage
	}
	files := []issuePackageGitFile{}
	err = issuePackageGitCheckPath(g.packages.root, filepath.Join(".plan-git", "checkouts", filepath.Base(g.checkout(c))), true)
	if err == nil {
		files, err = issuePackageGitMarkdownFiles(g.checkout(c))
	}
	if err != nil && !os.IsNotExist(err) {
		out["warning"] = "Repository files could not be listed: " + err.Error()
	} else if err == nil {
		out["markdownFiles"] = files
	}
	return out, nil
}

func issuePackageGitQuote(value string) string {
	if regexp.MustCompile(`^[A-Za-z0-9_./:=@+-]+$`).MatchString(value) {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
func (g *issuePackageGitService) command(dir, step string, args ...string) issuePackageGitCommand {
	// These settings also apply to clone: repository hooks, submodules, local
	// protocols and file-system monitor commands cannot execute through this flow.
	prefix := []string{"-c", "core.hooksPath=" + filepath.Join(g.storageRoot(), "disabled-hooks"), "-c", "core.fsmonitor=false", "-c", "submodule.recurse=false", "-c", "protocol.file.allow=never", "-c", "protocol.ext.allow=never", "-c", "init.templateDir="}
	prefix = append(prefix, issuePackageGitIdentityArgs()...)
	args = append(prefix, args...)
	display := []string{"git"}
	for _, arg := range args {
		display = append(display, issuePackageGitQuote(arg))
	}
	return issuePackageGitCommand{Directory: dir, Args: args, Display: strings.Join(display, " "), step: step}
}
func (g *issuePackageGitService) planFiles(id, repository, branch string) (map[string][]byte, error) {
	folder, err := g.packages.preparePlanFolder(id)
	if err != nil {
		return nil, err
	}
	if folder.Warning != "" {
		return nil, fmt.Errorf("local Markdown has edits to review before Git save: %s", folder.Warning)
	}
	files := map[string][]byte{}
	state, err := g.loadState()
	if err != nil {
		return nil, err
	}
	connection, _, _ := issuePackageGitEffective(state, id)
	saved := issuePackageGitActivate(state.Packages[id], connection)
	source := ""
	if saved.Connection.Repository == repository && saved.Connection.Branch == branch {
		source = saved.SourcePath
	}
	if source != "" && (!issuePackageGitSafeRelative(source) || !strings.EqualFold(path.Ext(source), ".md")) {
		return nil, fmt.Errorf("saved repository source path is invalid")
	}
	for name, raw := range folder.Contents {
		if strings.EqualFold(path.Ext(name), ".md") {
			relative := path.Join(folder.RelativePath, name)
			if source != "" {
				relative = source
			}
			if !issuePackageGitSafeRelative(relative) {
				return nil, fmt.Errorf("test plan folder has an unsafe Markdown path")
			}
			files[relative] = append([]byte{}, raw...)
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("this plan has no Markdown files to save")
	}
	return files, nil
}
func (g *issuePackageGitService) preview(req issuePackageRequest) (issuePackageGitPreview, error) {
	var zero issuePackageGitPreview
	pkg, err := g.packages.get(req.ID)
	if err != nil {
		return zero, err
	}
	if req.Revision != "" && req.Revision != pkg.Revision {
		return zero, errIssuePackageConflict
	}
	if req.Kind != "save" && req.Kind != "pull" && req.Kind != "check" {
		return zero, fmt.Errorf("choose save, pull, or check")
	}
	c, err := issuePackageGitRemote(req.Repository, req.GitBranch)
	if err != nil {
		return zero, err
	}
	state, err := g.loadState()
	if err != nil {
		return zero, err
	}
	linked, _, _ := issuePackageGitEffective(state, pkg.ID)
	if linked != c {
		return zero, fmt.Errorf("link this repository and branch before reviewing repository actions")
	}
	if req.Kind == "check" {
		return g.previewCheck(pkg, c)
	}
	checkout := g.checkout(c)
	if err := issuePackageGitCheckPath(g.packages.root, filepath.Join(".plan-git", "checkouts", filepath.Base(checkout)), false); err != nil {
		return zero, err
	}
	existing := false
	if info, err := os.Lstat(checkout); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return zero, fmt.Errorf("managed checkout is not a regular directory")
		}
		existing = true
	} else if !os.IsNotExist(err) {
		return zero, err
	}
	p := issuePackageGitPreview{ReviewToken: cachelab.ID(), Kind: req.Kind, Repository: c.Repository, Branch: c.Branch, WebURL: c.WebURL, CheckoutPath: checkout, ExpiresAt: g.now().Add(10 * time.Minute).UTC(), Files: []issuePackageGitFile{}, Warnings: []string{"Uses existing local Git credentials. Private repositories remain private; repository visibility is not changed.", "A failed pull or conflict stops the operation. No force push or automatic conflict resolution is used."}, packageID: pkg.ID, revision: pkg.Revision, contents: map[string][]byte{}, existing: existing}
	if !existing {
		p.Commands = append(p.Commands, g.command(filepath.Dir(checkout), "clone", "clone", "--no-recurse-submodules", "--single-branch", "--branch", c.Branch, "--", c.Repository, checkout))
	}
	p.Commands = append(p.Commands, g.command(checkout, "clean", "status", "--porcelain=v1", "-z", "--untracked-files=all"), g.command(checkout, "remote", "remote", "get-url", "origin"), g.command(checkout, "branch", "symbolic-ref", "--short", "HEAD"), g.command(checkout, "pull", "pull", "--ff-only", "--no-rebase", "origin", c.Branch))
	if p.Kind == "save" {
		p.contents, err = g.planFiles(pkg.ID, c.Repository, c.Branch)
		if err != nil {
			return zero, err
		}
		saved := issuePackageGitActivate(state.Packages[pkg.ID], c)
		p.baselines = map[string]string{}
		p.pending = map[string]string{}
		if saved.Connection.Repository == c.Repository && saved.Connection.Branch == c.Branch {
			for name, digest := range saved.Baselines {
				p.baselines[name] = digest
			}
			for name, digest := range saved.Pending {
				p.pending[name] = digest
			}
		}
		paths := []string{}
		for name, raw := range p.contents {
			paths = append(paths, name)
			p.Files = append(p.Files, issuePackageGitFile{Path: name, Bytes: len(raw)})
		}
		sort.Strings(paths)
		sort.Slice(p.Files, func(i, j int) bool { return p.Files[i].Path < p.Files[j].Path })
		message := strings.TrimSpace(req.GitMessage)
		if message == "" {
			message = "Update test plan"
		}
		if !packageText(message, 240, true) || strings.ContainsAny(message, "\r\n") {
			return zero, fmt.Errorf("use a commit message of up to 240 bytes on one line")
		}
		p.Commands = append(p.Commands, g.command(checkout, "add", append([]string{"add", "--"}, paths...)...), g.command(checkout, "staged", "diff", "--cached", "--name-only", "-z"), g.command(checkout, "changed", "diff", "--cached", "--quiet", "--exit-code"))
		commit := g.command(checkout, "commit", "commit", "-m", message)
		commit.Condition = "Only when the reviewed Markdown changed"
		p.Commands = append(p.Commands, commit, g.command(checkout, "local-head", "rev-parse", "HEAD"), g.command(checkout, "push", "push", "origin", "HEAD:refs/heads/"+c.Branch))
	}
	historyArgs := []string{"log", "-20", "--format=%H%x00%aI%x00%s%x00"}
	if p.Kind == "save" {
		historyArgs = append(historyArgs, "--")
		for _, file := range p.Files {
			historyArgs = append(historyArgs, file.Path)
		}
	}
	p.Commands = append(p.Commands, g.command(checkout, "head", "rev-parse", "--verify", "HEAD"), g.command(checkout, "history", historyArgs...))
	raw, _ := json.Marshal(struct {
		Preview   issuePackageGitPreview
		Revision  string
		Contents  map[string][]byte
		Baselines map[string]string
		Pending   map[string]string
	}{p, p.revision, p.contents, p.baselines, p.pending})
	p.Digest = issuePackageDigest(raw)
	for token, review := range g.reviews {
		if review.ExpiresAt.Before(g.now()) {
			delete(g.reviews, token)
		}
	}
	if len(g.reviews) >= 100 {
		return zero, fmt.Errorf("too many pending repository reviews; let an earlier review expire")
	}
	g.reviews[p.ReviewToken] = p
	return p, nil
}

func (g *issuePackageGitService) execute(ctx context.Context, req issuePackageRequest) (map[string]any, error) {
	p, ok := g.reviews[req.ReviewToken]
	if !ok || req.Confirm != "execute" || p.packageID != req.ID || g.now().After(p.ExpiresAt) {
		return nil, fmt.Errorf("review these repository commands again before approving them")
	}
	delete(g.reviews, req.ReviewToken)
	if p.Kind == "check" {
		return g.executeCheck(ctx, p)
	}
	pkg, err := g.packages.get(req.ID)
	if err != nil {
		return nil, err
	}
	if pkg.Revision != p.revision {
		return nil, fmt.Errorf("the plan changed after review: %w", errIssuePackageConflict)
	}
	if p.Kind == "save" {
		files, err := g.planFiles(req.ID, p.Repository, p.Branch)
		if err != nil {
			return nil, err
		}
		a, _ := json.Marshal(files)
		b, _ := json.Marshal(p.contents)
		if !bytes.Equal(a, b) {
			return nil, fmt.Errorf("the Markdown or folder changed after review; review the new files before saving: %w", errIssuePackageConflict)
		}
	}
	if err = issuePackageGitSafeDirectory(g.packages.root, ".plan-git/checkouts"); err != nil {
		return nil, err
	}
	if err = issuePackageGitCheckPath(g.packages.root, filepath.Join(".plan-git", "checkouts", filepath.Base(p.CheckoutPath)), p.existing); err != nil {
		return nil, err
	}
	if err = issuePackageGitSafeDirectory(g.packages.root, ".plan-git/disabled-hooks"); err != nil {
		return nil, err
	}
	if entries, err := os.ReadDir(filepath.Join(g.storageRoot(), "disabled-hooks")); err != nil || len(entries) != 0 {
		return nil, fmt.Errorf("disabled hooks directory must remain empty")
	}
	state, err := g.loadState()
	if err != nil {
		return nil, err
	}
	connection := issuePackageGitConnection{Repository: p.Repository, Branch: p.Branch, WebURL: p.WebURL}
	linked, _, _ := issuePackageGitEffective(state, req.ID)
	if linked != connection {
		return nil, fmt.Errorf("the linked repository changed after review; review the current connection instead")
	}
	saved := issuePackageGitActivate(state.Packages[req.ID], connection)
	entry := issuePackageGitHistory{At: g.now().UTC(), Operation: p.Kind, Status: "failed", Summary: "Repository operation stopped before completion"}
	finish := func(runErr error) (map[string]any, error) {
		if runErr != nil {
			entry.Summary = runErr.Error()
		} else {
			entry.Status = "completed"
			entry.Summary = map[string]string{"save": "Reviewed test plan saved to GitHub", "pull": "Repository pulled; choose Markdown files to import"}[p.Kind]
			if p.Kind == "save" {
				if saved.Baselines == nil {
					saved.Baselines = map[string]string{}
				}
				for name, content := range p.contents {
					saved.Baselines[name] = issuePackageDigest(content)
				}
				saved.Pending = nil
				saved.PendingHead = ""
			}
		}
		saved.LocalHistory = append([]issuePackageGitHistory{entry}, saved.LocalHistory...)
		if len(saved.LocalHistory) > 100 {
			saved.LocalHistory = saved.LocalHistory[:100]
		}
		state.Packages[req.ID] = saved
		if persistErr := g.saveState(state); persistErr != nil {
			return nil, fmt.Errorf("repository operation finished, but its local history could not be saved: %w", persistErr)
		}
		if runErr != nil {
			return nil, runErr
		}
		out, err := g.status(req.ID)
		if err == nil {
			out["message"] = entry.Summary
		}
		return out, err
	}
	changed := false
	wrote := false
	for _, command := range p.Commands {
		if command.step == "push" {
			current, err := g.packages.get(req.ID)
			if err != nil || current.Revision != p.revision {
				return finish(fmt.Errorf("the plan changed before push; review the current plan before publishing"))
			}
		}
		if command.step == "commit" && !changed {
			continue
		}
		if command.step == "add" && !wrote {
			// A concurrent plan edit during pull must not publish an older review.
			current, err := g.packages.get(req.ID)
			if err != nil || current.Revision != p.revision {
				return finish(fmt.Errorf("the plan changed while pulling; review the current plan before saving"))
			}
			if err := issuePackageGitCheckBaseline(p.CheckoutPath, p.contents, p.baselines, p.pending); err != nil {
				return finish(err)
			}
			if err := issuePackageGitWriteFiles(p.CheckoutPath, p.contents); err != nil {
				return finish(err)
			}
			saved.Pending = map[string]string{}
			for name, content := range p.contents {
				saved.Pending[name] = issuePackageDigest(content)
			}
			state.Packages[req.ID] = saved
			if err := g.saveState(state); err != nil {
				return finish(fmt.Errorf("could not secure pending repository recovery: %w", err))
			}
			wrote = true
		}
		if command.step != "clone" {
			if err := issuePackageGitCheckConfig(p.CheckoutPath); err != nil {
				return finish(err)
			}
		}
		output, err := g.runner(ctx, command.Directory, command.Args)
		if err != nil {
			return finish(fmt.Errorf("%s could not finish: %w", command.step, err))
		}
		if command.step == "changed" && output.ExitCode == 1 {
			changed = true
			continue
		}
		if output.ExitCode != 0 {
			safe, _ := redactIssuePackageLog([]byte(output.Output), nil)
			if len(safe) > 2000 {
				safe = safe[:2000]
			}
			return finish(fmt.Errorf("%s stopped (exit %d): %s", command.step, output.ExitCode, strings.TrimSpace(string(safe))))
		}
		switch command.step {
		case "clean":
			if err := issuePackageGitCheckPending(p.CheckoutPath, output.Output, p.pending); err != nil {
				return finish(fmt.Errorf("the managed checkout has unfinished changes; inspect %s before retrying", p.CheckoutPath))
			}
		case "remote":
			remote, err := issuePackageGitRemote(strings.TrimSpace(output.Output), p.Branch)
			if err != nil || remote.Repository != p.Repository {
				return finish(fmt.Errorf("the managed checkout remote changed; no pull or push was attempted"))
			}
		case "branch":
			if strings.TrimSpace(output.Output) != p.Branch {
				return finish(fmt.Errorf("the managed checkout branch changed; no pull or push was attempted"))
			}
		case "staged":
			for _, name := range strings.Split(output.Output, "\x00") {
				if name != "" {
					if _, owned := p.contents[name]; !owned {
						return finish(fmt.Errorf("unreviewed files were staged; no commit or push was attempted"))
					}
				}
			}
		case "head":
			commit := strings.TrimSpace(output.Output)
			if !issuePackageGitSHA.MatchString(commit) {
				return finish(fmt.Errorf("repository returned an invalid commit identity"))
			}
			entry.Commit = commit
			entry.URL = p.WebURL + "/commit/" + commit
		case "local-head":
			commit := strings.TrimSpace(output.Output)
			if !issuePackageGitSHA.MatchString(commit) {
				return finish(fmt.Errorf("repository returned an invalid local commit identity"))
			}
			saved.PendingHead = commit
		case "history":
			saved.GitHistory = issuePackageGitParseHistory(output.Output, p.WebURL)
		}
	}
	return finish(nil)
}

func (g *issuePackageGitService) importMarkdown(req issuePackageRequest) (map[string]any, error) {
	if req.ID != "" {
		if _, err := g.packages.get(req.ID); err != nil {
			return nil, err
		}
	}
	state, err := g.loadState()
	if err != nil {
		return nil, err
	}
	c, _, _ := issuePackageGitEffective(state, req.ID)
	if c.Repository == "" {
		return nil, fmt.Errorf("link and pull a repository before importing its Markdown")
	}
	// Search results bind their cached file to the connection shown to the user.
	// A changed default must never import a different repository's same path.
	if req.ID == "" && req.Repository == "" {
		return nil, fmt.Errorf("choose a repository search result before importing")
	}
	if req.Repository != "" {
		requested, err := issuePackageGitRemote(req.Repository, req.GitBranch)
		if err != nil || requested != c {
			return nil, fmt.Errorf("the linked repository changed; refresh the search results before importing")
		}
	}
	saved := issuePackageGitActivate(state.Packages[req.ID], c)
	if !issuePackageGitSafeRelative(req.GitPath) || !strings.EqualFold(path.Ext(req.GitPath), ".md") {
		return nil, fmt.Errorf("choose a Markdown file from the pulled repository")
	}
	if _, err := issuePackageGitRemote(c.Repository, c.Branch); err != nil {
		return nil, err
	}
	checkout := g.checkout(c)
	if err = issuePackageGitCheckPath(g.packages.root, filepath.Join(".plan-git", "checkouts", filepath.Base(checkout)), true); err != nil {
		return nil, err
	}
	if err = issuePackageGitCheckPath(checkout, filepath.FromSlash(req.GitPath), true); err != nil {
		return nil, err
	}
	raw, err := issuePackageReadBounded(filepath.Join(checkout, filepath.FromSlash(req.GitPath)), 1<<20)
	if err != nil {
		return nil, err
	}
	plan, err := parseIssuePackagePlanMarkdown(raw)
	if err != nil {
		return nil, err
	}
	environment, hasEnvironment, err := parseIssuePackagePlanMarkdownEnvironment(raw)
	if err != nil {
		return nil, err
	}
	now := g.now().UTC()
	pkg := issuePackage{ID: cachelab.ID(), Revision: cachelab.ID(), Title: plan.Title, IssueURL: plan.IssueURL, IssueTitle: plan.IssueTitle, FixURL: plan.FixURL, FixTitle: plan.FixTitle, Summary: plan.Summary, Status: "planning", Cases: generatedPackageCases(plan.Cases), Sessions: []issuePackageSession{}, CreatedAt: now, UpdatedAt: now}
	pkg, err = g.packages.importPackage(pkg, nil)
	if err != nil {
		return nil, err
	}
	warnings := []string{}
	if hasEnvironment {
		if err := g.packages.rememberPlanEnvironment(pkg.ID, pkg.Revision, environment); err != nil {
			warnings = append(warnings, "Imported environment metadata could not be saved: "+err.Error())
		}
	}
	saved.LocalHistory = append([]issuePackageGitHistory{{At: now, Operation: "import", Status: "completed", Summary: "Imported " + req.GitPath + " as a new test plan"}}, saved.LocalHistory...)
	if len(saved.LocalHistory) > 100 {
		saved.LocalHistory = saved.LocalHistory[:100]
	}
	if req.ID != "" {
		state.Packages[req.ID] = saved
	}
	state.Packages[pkg.ID] = issuePackageGitSaved{Connection: c, Explicit: true, SourcePath: req.GitPath, Baselines: map[string]string{req.GitPath: issuePackageDigest(raw)}, GitHistory: saved.GitHistory, LocalHistory: []issuePackageGitHistory{{At: now, Operation: "import", Status: "completed", Summary: "Imported from " + req.GitPath}}}
	if err = g.saveState(state); err != nil {
		warnings = append(warnings, "The plan was imported, but repository history could not be updated.")
	}
	return map[string]any{"package": pkg, "library": g.packages.packageLibrarySnapshot(), "message": "Markdown imported as a new test plan", "warning": strings.Join(warnings, " ")}, nil
}

func issuePackageGitParseHistory(raw, web string) []issuePackageGitCommit {
	out := []issuePackageGitCommit{}
	parts := strings.Split(raw, "\x00")
	for i := 0; i+2 < len(parts) && len(out) < 20; i += 3 {
		sha := strings.TrimSpace(parts[i])
		if !issuePackageGitSHA.MatchString(sha) {
			continue
		}
		subject := strings.TrimSpace(parts[i+2])
		if len(subject) > 1000 {
			subject = subject[:1000]
		}
		out = append(out, issuePackageGitCommit{Commit: sha, At: strings.TrimSpace(parts[i+1]), Subject: subject, URL: web + "/commit/" + sha})
	}
	return out
}
func issuePackageGitSafeRelative(value string) bool {
	if value == "" || len(value) > 2048 || strings.ContainsAny(value, "\\\x00\r\n") || path.IsAbs(value) || path.Clean(value) != value {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == "." || part == ".." || part == ".git" || strings.HasPrefix(part, "-") {
			return false
		}
	}
	return true
}
func issuePackageGitCheckPath(base, relative string, required bool) error {
	if !issuePackageGitSafeRelative(filepath.ToSlash(relative)) {
		return fmt.Errorf("unsafe managed repository path")
	}
	current := base
	if info, err := os.Lstat(current); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("managed repository directory is unavailable or is a link")
	}
	pieces := strings.Split(relative, string(filepath.Separator))
	for i, part := range pieces {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) && !required {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("repository paths cannot pass through symbolic links")
		}
		if i < len(pieces)-1 && !info.IsDir() {
			return fmt.Errorf("repository parent is not a directory")
		}
	}
	return nil
}
func issuePackageGitSafeDirectory(base, relative string) error {
	if err := issuePackageGitCheckPath(base, filepath.FromSlash(relative), false); err != nil {
		return err
	}
	return os.MkdirAll(filepath.Join(base, filepath.FromSlash(relative)), 0700)
}
func issuePackageGitWriteFiles(checkout string, files map[string][]byte) error {
	for name, raw := range files {
		if !issuePackageGitSafeRelative(name) || !strings.EqualFold(path.Ext(name), ".md") {
			return fmt.Errorf("only reviewed Markdown files can be written")
		}
		if err := issuePackageGitCheckPath(checkout, filepath.FromSlash(name), false); err != nil {
			return err
		}
		if parent := path.Dir(name); parent != "." {
			if err := issuePackageGitSafeDirectory(checkout, parent); err != nil {
				return err
			}
		}
		if err := writePrivateConfigAtomically(filepath.Join(checkout, filepath.FromSlash(name)), raw); err != nil {
			return err
		}
	}
	return nil
}
func issuePackageGitMarkdownFiles(checkout string) ([]issuePackageGitFile, error) {
	out := []issuePackageGitFile{}
	visited, total := 0, 0
	info, err := os.Lstat(checkout)
	if err != nil {
		return out, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("managed checkout is not a regular directory")
	}
	err = filepath.WalkDir(checkout, func(file string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		visited++
		if visited > 20000 {
			return fmt.Errorf("repository has too many files to list")
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(d.Name()), ".md") {
			return nil
		}
		relative, err := filepath.Rel(checkout, file)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if !issuePackageGitSafeRelative(relative) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > 1<<20 {
			return nil
		}
		total += int(info.Size())
		if len(out) >= 2000 || total > 16<<20 {
			return fmt.Errorf("repository Markdown exceeds the listing limit")
		}
		out = append(out, issuePackageGitFile{Path: relative, Bytes: int(info.Size())})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, err
}

type issuePackageGitOutput struct {
	mu   sync.Mutex
	data []byte
}

func (w *issuePackageGitOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	remaining := (64 << 10) - len(w.data)
	if remaining > len(p) {
		remaining = len(p)
	}
	if remaining > 0 {
		w.data = append(w.data, p[:remaining]...)
	}
	return len(p), nil
}
func runIssuePackageGit(ctx context.Context, dir string, args []string) (issuePackageGitRunResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	// Do not inherit a caller's GIT_DIR, index, trace sink, or injected config.
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if !strings.HasPrefix(key, "GIT_") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull, "GIT_CEILING_DIRECTORIES="+dir, "GIT_SSH_COMMAND=ssh -o BatchMode=yes -o StrictHostKeyChecking=yes")
	output := &issuePackageGitOutput{}
	cmd.Stdout = output
	cmd.Stderr = output
	cmd.WaitDelay = 3 * time.Second
	err := cmd.Run()
	result := issuePackageGitRunResult{Output: string(output.data)}
	if ctx.Err() != nil {
		return result, fmt.Errorf("repository command timed out or was cancelled")
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("Git could not start; check that Git is installed and local credentials are configured")
	}
	return result, nil
}

// Read only identity values from ordinary local config. Includes, aliases,
// filters, URL rewrites, executable helpers and conditional config are ignored.
func issuePackageGitIdentityArgs() []string {
	values := map[string]string{}
	home, err := os.UserHomeDir()
	if err == nil {
		configRoot := os.Getenv("XDG_CONFIG_HOME")
		if configRoot == "" {
			configRoot = filepath.Join(home, ".config")
		}
		for _, file := range []string{filepath.Join(configRoot, "git", "config"), filepath.Join(home, ".gitconfig")} {
			raw, err := issuePackageReadBounded(file, 256<<10)
			if err != nil {
				continue
			}
			section := ""
			scanner := bufio.NewScanner(bytes.NewReader(raw))
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if strings.HasPrefix(line, "[") {
					section = strings.ToLower(line)
					continue
				}
				if section != "[user]" {
					continue
				}
				key, value, ok := strings.Cut(line, "=")
				key = strings.ToLower(strings.TrimSpace(key))
				if !ok || (key != "name" && key != "email") {
					continue
				}
				value = strings.TrimSpace(value)
				if strings.HasPrefix(value, "\"") {
					decoded, err := strconv.Unquote(value)
					if err != nil {
						continue
					}
					value = decoded
				} else {
					for _, marker := range []string{" #", " ;"} {
						if i := strings.Index(value, marker); i >= 0 {
							value = strings.TrimSpace(value[:i])
						}
					}
				}
				if packageText(value, 240, true) && !strings.ContainsAny(value, "\r\n") {
					values[key] = value
				}
			}
		}
	}
	args := []string{}
	for _, key := range []string{"name", "email"} {
		if values[key] != "" {
			args = append(args, "-c", "user."+key+"="+values[key])
		}
	}
	switch runtime.GOOS {
	case "darwin":
		args = append(args, "-c", "credential.helper=osxkeychain")
	case "windows":
		args = append(args, "-c", "credential.helper=manager")
	default:
		args = append(args, "-c", "credential.helper=cache")
	}
	if gh, err := exec.LookPath("gh"); err == nil {
		if absolute, err := filepath.Abs(gh); err == nil {
			args = append(args, "-c", "credential.helper=!"+issuePackageGitQuote(absolute)+" auth git-credential")
		}
	}
	return args
}

// Only clone-created configuration is allowed inside this managed checkout.
// In particular .gitattributes cannot activate a configured filter executable.
func issuePackageGitCheckConfig(checkout string) error {
	gitDir := filepath.Join(checkout, ".git")
	info, err := os.Lstat(gitDir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("managed Git metadata is unavailable or is a link")
	}
	raw, err := issuePackageReadBounded(filepath.Join(gitDir, "config"), 128<<10)
	if err != nil {
		return fmt.Errorf("managed Git configuration is unavailable")
	}
	section := ""
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			section = strings.ToLower(line)
			if section != "[core]" && section != "[remote \"origin\"]" && !strings.HasPrefix(section, "[branch \"") {
				return fmt.Errorf("managed checkout contains unsupported Git configuration; no repository command was run")
			}
			continue
		}
		key, _, ok := strings.Cut(line, "=")
		key = strings.ToLower(strings.TrimSpace(key))
		if !ok {
			return fmt.Errorf("managed checkout contains unsupported Git configuration")
		}
		allowed := false
		switch {
		case section == "[core]":
			allowed = packageEnum(key, "repositoryformatversion", "filemode", "bare", "logallrefupdates", "ignorecase", "precomposeunicode")
		case section == "[remote \"origin\"]":
			allowed = packageEnum(key, "url", "fetch")
		case strings.HasPrefix(section, "[branch \""):
			allowed = packageEnum(key, "remote", "merge")
		}
		if !allowed {
			return fmt.Errorf("managed checkout contains unsupported Git configuration; no repository command was run")
		}
	}
	if scanner.Err() != nil {
		return fmt.Errorf("managed Git configuration exceeds its line limit")
	}
	return nil
}

func issuePackageGitCheckBaseline(checkout string, desired map[string][]byte, baselines, pending map[string]string) error {
	for name, content := range desired {
		if err := issuePackageGitCheckPath(checkout, filepath.FromSlash(name), false); err != nil {
			return err
		}
		current, err := issuePackageReadBounded(filepath.Join(checkout, filepath.FromSlash(name)), 8<<20)
		exists := err == nil
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		currentDigest := ""
		if exists {
			currentDigest = issuePackageDigest(current)
		}
		// Identical content needs no overwrite and is safe even on a first save.
		if exists && currentDigest == issuePackageDigest(content) {
			continue
		}
		if exists && pending[name] != "" && currentDigest == pending[name] {
			continue
		}
		baseline, known := baselines[name]
		if (!known && exists) || (known && currentDigest != baseline) {
			return fmt.Errorf("repository Markdown changed at %s; pull and import the current file before reviewing a save", name)
		}
	}
	return nil
}

func issuePackageGitCheckPending(checkout, porcelain string, pending map[string]string) error {
	for _, record := range strings.Split(porcelain, "\x00") {
		if record == "" {
			continue
		}
		if len(record) < 4 || record[2] != ' ' || strings.ContainsAny(record[:2], "RCDU!") {
			return fmt.Errorf("unowned checkout changes")
		}
		name := record[3:]
		digest, owned := pending[name]
		if !owned || !issuePackageGitSafeRelative(name) {
			return fmt.Errorf("unowned checkout changes")
		}
		if err := issuePackageGitCheckPath(checkout, filepath.FromSlash(name), true); err != nil {
			return err
		}
		raw, err := issuePackageReadBounded(filepath.Join(checkout, filepath.FromSlash(name)), 8<<20)
		if err != nil || issuePackageDigest(raw) != digest {
			return fmt.Errorf("pending repository writing changed outside Runway")
		}
	}
	return nil
}
