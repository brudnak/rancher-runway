package test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/cachelab"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Imported plans keep an explicit source association. Ordinary plans inherit
// the remembered default even before they have any repository history.
func issuePackageGitExplicit(saved issuePackageGitSaved) bool {
	return saved.Explicit || saved.SourcePath != "" || saved.Disabled
}
func issuePackageGitEffective(state issuePackageGitState, id string) (issuePackageGitConnection, string, string) {
	saved := state.Packages[id]
	if issuePackageGitExplicit(saved) {
		if saved.Disabled || saved.Connection.Repository == "" {
			return issuePackageGitConnection{}, "none", "plan"
		}
		return saved.Connection, "plan", "plan"
	}
	if state.DefaultConnection.Repository == "" {
		return issuePackageGitConnection{}, "none", "default"
	}
	return state.DefaultConnection, "default", "default"
}

func issuePackageGitConnectionKey(c issuePackageGitConnection) string {
	return issuePackageDigest([]byte(c.Repository + "\n" + c.Branch))
}

// Detached connection metadata is retained privately so returning to the exact
// same repository can recover an interrupted approved save without discarding
// files. Active metadata never leaks from one repository/branch into another.
func issuePackageGitActivate(saved issuePackageGitSaved, connection issuePackageGitConnection) issuePackageGitSaved {
	if saved.Connection == connection {
		return saved
	}
	previous := saved.Previous
	if previous == nil {
		previous = map[string]issuePackageGitSaved{}
	}
	if saved.Connection.Repository != "" {
		snapshot := saved
		snapshot.Previous = nil
		snapshot.LocalHistory = nil
		previous[issuePackageGitConnectionKey(saved.Connection)] = snapshot
	}
	active := issuePackageGitSaved{}
	if connection.Repository != "" {
		if remembered, ok := previous[issuePackageGitConnectionKey(connection)]; ok {
			active = remembered
			delete(previous, issuePackageGitConnectionKey(connection))
		}
	}
	active.Explicit = issuePackageGitExplicit(saved)
	active.Disabled = saved.Disabled
	active.Connection = connection
	active.LocalHistory = saved.LocalHistory
	active.Previous = previous
	return active
}
func issuePackageGitAllHistory(saved issuePackageGitSaved) []issuePackageGitCommit {
	history := append([]issuePackageGitCommit{}, saved.GitHistory...)
	for _, previous := range saved.Previous {
		history = append(history, previous.GitHistory...)
	}
	sort.SliceStable(history, func(i, j int) bool { return history[i].At > history[j].At })
	out := []issuePackageGitCommit{}
	seen := map[string]bool{}
	for _, commit := range history {
		if !seen[commit.URL] {
			out = append(out, commit)
			seen[commit.URL] = true
		}
		if len(out) >= 100 {
			break
		}
	}
	return out
}
func (g *issuePackageGitService) invalidateReviews(id string) {
	for token, review := range g.reviews {
		if review.packageID == id {
			delete(g.reviews, token)
		}
	}
}
func issuePackageGitRecord(saved *issuePackageGitSaved, entry issuePackageGitHistory) {
	saved.LocalHistory = append([]issuePackageGitHistory{entry}, saved.LocalHistory...)
	if len(saved.LocalHistory) > 100 {
		saved.LocalHistory = saved.LocalHistory[:100]
	}
}

// Default changes revoke only inherited approvals; imported source overrides
// remain independent. Call after persistence so failed local writes keep reviews.
func (g *issuePackageGitService) invalidateDefaultReviews(state issuePackageGitState) {
	for token, review := range g.reviews {
		if !issuePackageGitExplicit(state.Packages[review.packageID]) {
			delete(g.reviews, token)
		}
	}
}
func issuePackageGitSetDefault(state *issuePackageGitState, connection issuePackageGitConnection) {
	if state.DefaultConnection != connection {
		state.DefaultCheckedAt = nil
		state.DefaultCheckStatus = ""
		state.DefaultCheckMessage = ""
	}
	state.DefaultConnection = connection
	for id, saved := range state.Packages {
		if !issuePackageGitExplicit(saved) {
			state.Packages[id] = issuePackageGitActivate(saved, connection)
		}
	}
}
func (g *issuePackageGitService) link(req issuePackageRequest) (map[string]any, error) {
	if req.ID != "" {
		if _, err := g.packages.get(req.ID); err != nil {
			return nil, err
		}
	}
	connection, err := issuePackageGitRemote(req.Repository, req.GitBranch)
	if err != nil {
		return nil, err
	}
	state, err := g.loadState()
	if err != nil {
		return nil, err
	}
	before, _, scope := issuePackageGitEffective(state, req.ID)
	if scope == "default" {
		issuePackageGitSetDefault(&state, connection)
	}
	saved := issuePackageGitActivate(state.Packages[req.ID], connection)
	saved.Disabled = false
	operation := "link"
	if before.Repository != "" && before != connection {
		operation = "relink"
	}
	summary := "Linked " + connection.WebURL + " on " + connection.Branch
	if scope == "default" {
		summary += " as the default for test plans"
	}
	issuePackageGitRecord(&saved, issuePackageGitHistory{At: g.now().UTC(), Operation: operation, Status: "completed", Summary: summary + ". No repository command ran."})
	if req.ID != "" {
		state.Packages[req.ID] = saved
	}
	if err = g.saveState(state); err != nil {
		return nil, err
	}
	if scope == "default" {
		g.invalidateDefaultReviews(state)
	} else {
		g.invalidateReviews(req.ID)
	}
	out, err := g.status(req.ID)
	if err == nil {
		out["message"] = "Repository linked locally. Test the connection to check read access, or review a pull to load its test plans."
	}
	return out, err
}
func (g *issuePackageGitService) unlink(id string) (map[string]any, error) {
	if id != "" {
		if _, err := g.packages.get(id); err != nil {
			return nil, err
		}
	}
	state, err := g.loadState()
	if err != nil {
		return nil, err
	}
	_, _, scope := issuePackageGitEffective(state, id)
	if scope == "default" {
		issuePackageGitSetDefault(&state, issuePackageGitConnection{})
	}
	saved := issuePackageGitActivate(state.Packages[id], issuePackageGitConnection{})
	if scope == "plan" {
		saved.Explicit = true
		saved.Disabled = true
	}
	issuePackageGitRecord(&saved, issuePackageGitHistory{At: g.now().UTC(), Operation: "unlink", Status: "completed", Summary: "Repository unlinked. Local plans, checkout files, history, and the remote repository were preserved."})
	if id != "" {
		state.Packages[id] = saved
	}
	if err = g.saveState(state); err != nil {
		return nil, err
	}
	if scope == "default" {
		g.invalidateDefaultReviews(state)
	} else {
		g.invalidateReviews(id)
	}
	out, err := g.status(id)
	if err == nil {
		out["message"] = "Repository unlinked. Your local plans and history are still available."
	}
	return out, err
}
func (g *issuePackageGitService) previewCheck(pkg issuePackage, connection issuePackageGitConnection) (issuePackageGitPreview, error) {
	state, err := g.loadState()
	if err != nil {
		return issuePackageGitPreview{}, err
	}
	linked, _, _ := issuePackageGitEffective(state, pkg.ID)
	if linked != connection {
		return issuePackageGitPreview{}, fmt.Errorf("link this repository and branch before testing the connection")
	}
	review := issuePackageGitPreview{ReviewToken: cachelab.ID(), Kind: "check", Repository: connection.Repository, Branch: connection.Branch, WebURL: connection.WebURL, ExpiresAt: g.now().Add(10 * time.Minute).UTC(), Files: []issuePackageGitFile{}, Warnings: []string{"Checks read access to this repository branch. It does not verify permission to push, change visibility, clone files, or modify the repository."}, packageID: pkg.ID, revision: pkg.Revision}
	review.Commands = []issuePackageGitCommand{g.command(filepath.Join(g.storageRoot(), "connection-check"), "check", "ls-remote", "--exit-code", "--heads", connection.Repository, "refs/heads/"+connection.Branch)}
	raw, _ := json.Marshal(review)
	review.Digest = issuePackageDigest(raw)
	for token, previous := range g.reviews {
		if previous.ExpiresAt.Before(g.now()) {
			delete(g.reviews, token)
		}
	}
	if len(g.reviews) >= 100 {
		return issuePackageGitPreview{}, fmt.Errorf("too many pending repository reviews; let an earlier review expire")
	}
	g.reviews[review.ReviewToken] = review
	return review, nil
}
func (g *issuePackageGitService) executeCheck(ctx context.Context, review issuePackageGitPreview) (map[string]any, error) {
	if _, err := g.packages.get(review.packageID); err != nil {
		return nil, err
	}
	state, err := g.loadState()
	if err != nil {
		return nil, err
	}
	connection := issuePackageGitConnection{Repository: review.Repository, Branch: review.Branch, WebURL: review.WebURL}
	linked, scope, _ := issuePackageGitEffective(state, review.packageID)
	if linked != connection {
		return nil, fmt.Errorf("the linked repository changed after review; test the current connection instead")
	}
	for _, relative := range []string{".plan-git/connection-check", ".plan-git/disabled-hooks"} {
		if err := issuePackageGitSafeDirectory(g.packages.root, relative); err != nil {
			return nil, err
		}
	}
	if entries, err := os.ReadDir(filepath.Join(g.storageRoot(), "disabled-hooks")); err != nil || len(entries) != 0 {
		return nil, fmt.Errorf("disabled hooks directory must remain empty")
	}
	// An ls-remote check uses an explicitly non-repository directory. The runner
	// ceiling also prevents discovering any user's worktree in parent folders.
	if _, err := os.Lstat(filepath.Join(g.storageRoot(), "connection-check", ".git")); !os.IsNotExist(err) {
		return nil, fmt.Errorf("connection check directory must not contain Git metadata")
	}
	saved := issuePackageGitActivate(state.Packages[review.packageID], connection)
	command := review.Commands[0]
	result, runErr := g.runner(ctx, command.Directory, command.Args)
	message := "Read access confirmed for " + review.Branch + ". Permission to push has not been tested."
	if runErr != nil {
		message = "Connection check could not finish: " + runErr.Error()
	} else if result.ExitCode == 2 {
		runErr = fmt.Errorf("the branch %s was not found; check the branch name or initialize the repository with a README", review.Branch)
		message = runErr.Error()
	} else if result.ExitCode != 0 {
		safe, _ := redactIssuePackageLog([]byte(result.Output), nil)
		if len(safe) > 2000 {
			safe = safe[:2000]
		}
		runErr = fmt.Errorf("read access could not be confirmed (exit %d). Check the repository, branch, and local GitHub CLI, keychain, or SSH credentials. %s", result.ExitCode, strings.TrimSpace(string(safe)))
		message = runErr.Error()
	} else if !issuePackageGitBranchAdvertised(result.Output, review.Branch) {
		runErr = fmt.Errorf("GitHub did not return the exact requested branch; check its name and try again")
		message = runErr.Error()
	}
	now := g.now().UTC()
	saved.CheckedAt = &now
	saved.CheckStatus = "readable"
	if runErr != nil {
		saved.CheckStatus = "failed"
	}
	saved.CheckMessage = message
	if scope == "default" {
		state.DefaultCheckedAt = saved.CheckedAt
		state.DefaultCheckStatus = saved.CheckStatus
		state.DefaultCheckMessage = saved.CheckMessage
	}
	status := "completed"
	if runErr != nil {
		status = "failed"
	}
	issuePackageGitRecord(&saved, issuePackageGitHistory{At: now, Operation: "check", Status: status, Summary: message})
	state.Packages[review.packageID] = saved
	if err = g.saveState(state); err != nil {
		return nil, fmt.Errorf("connection check finished, but its status could not be saved: %w", err)
	}
	if runErr != nil {
		return nil, runErr
	}
	out, err := g.status(review.packageID)
	if err == nil {
		out["message"] = message
	}
	return out, err
}
func issuePackageGitBranchAdvertised(output, branch string) bool {
	matched := false
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		fields := strings.Split(strings.TrimSpace(line), "\t")
		if len(fields) != 2 || !issuePackageGitSHA.MatchString(fields[0]) || fields[1] != "refs/heads/"+branch || matched {
			return false
		}
		matched = true
	}
	return matched
}
