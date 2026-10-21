package test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Every repository command in these tests goes through this fake. No test
// starts Git, including discovery/version commands or local repository setup.
type packageGitFake struct {
	t       *testing.T
	preview issuePackageGitPreview
	calls   []string
	fail    string
	staged  string
	status  string
	onStep  func(string)
	changed bool
}

func (f *packageGitFake) run(_ context.Context, dir string, args []string) (issuePackageGitRunResult, error) {
	step := ""
	for _, command := range f.preview.Commands {
		if command.Directory == dir && reflect.DeepEqual(command.Args, args) {
			step = command.step
			break
		}
	}
	if step == "" {
		f.t.Fatal("runner received a command outside the approved preview")
	}
	f.calls = append(f.calls, step)
	if step == "clone" {
		if err := os.MkdirAll(filepath.Join(f.preview.CheckoutPath, ".git"), 0700); err != nil {
			f.t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(f.preview.CheckoutPath, ".git", "config"), []byte("[core]\nrepositoryformatversion = 0\nbare = false\n[remote \"origin\"]\nurl = "+f.preview.Repository+"\nfetch = +refs/heads/*:refs/remotes/origin/*\n[branch \""+f.preview.Branch+"\"]\nremote = origin\nmerge = refs/heads/"+f.preview.Branch+"\n"), 0600); err != nil {
			f.t.Fatal(err)
		}
	}
	if f.onStep != nil {
		f.onStep(step)
	}
	if step == f.fail {
		return issuePackageGitRunResult{Output: "simulated conflict", ExitCode: 1}, nil
	}
	switch step {
	case "check":
		return issuePackageGitRunResult{Output: strings.Repeat("a", 40) + "\trefs/heads/" + f.preview.Branch + "\n"}, nil
	case "clean":
		return issuePackageGitRunResult{Output: f.status}, nil
	case "remote":
		return issuePackageGitRunResult{Output: f.preview.Repository + "\n"}, nil
	case "branch":
		return issuePackageGitRunResult{Output: f.preview.Branch + "\n"}, nil
	case "staged":
		if f.staged != "" {
			return issuePackageGitRunResult{Output: f.staged}, nil
		}
		names := []string{}
		for _, file := range f.preview.Files {
			names = append(names, file.Path)
		}
		return issuePackageGitRunResult{Output: strings.Join(names, "\x00") + "\x00"}, nil
	case "changed":
		if f.changed {
			return issuePackageGitRunResult{ExitCode: 1}, nil
		}
	case "head", "local-head":
		return issuePackageGitRunResult{Output: strings.Repeat("a", 40) + "\n"}, nil
	case "history":
		return issuePackageGitRunResult{Output: strings.Repeat("a", 40) + "\x002026-10-20T12:00:00Z\x00Reviewed plan\x00\n"}, nil
	}
	return issuePackageGitRunResult{}, nil
}
func packageGitTestService(t *testing.T) (*issuePackageGitService, issuePackage) {
	s := packageTestService(t)
	pkg := packageTestPlan(t, s)
	return &issuePackageGitService{packages: s, reviews: map[string]issuePackageGitPreview{}, now: time.Now, runner: func(context.Context, string, []string) (issuePackageGitRunResult, error) {
		t.Fatal("runner used before a fake approved operation was installed")
		return issuePackageGitRunResult{}, nil
	}}, pkg
}
func packageGitReview(t *testing.T, g *issuePackageGitService, pkg issuePackage, kind string) (issuePackageGitPreview, *packageGitFake) {
	t.Helper()
	state, err := g.loadState()
	if err != nil {
		t.Fatal(err)
	}
	connection, _, _ := issuePackageGitEffective(state, pkg.ID)
	wanted, _ := issuePackageGitRemote("owner/test-plans", "main")
	if connection != wanted {
		if _, err := g.link(issuePackageRequest{ID: pkg.ID, Repository: wanted.Repository, GitBranch: wanted.Branch}); err != nil {
			t.Fatal(err)
		}
	}
	p, err := g.preview(issuePackageRequest{ID: pkg.ID, Revision: pkg.Revision, Kind: kind, Repository: "owner/test-plans", GitBranch: "main", GitMessage: "Document role checks"})
	if err != nil {
		t.Fatal(err)
	}
	fake := &packageGitFake{t: t, preview: p, changed: true}
	g.runner = fake.run
	return p, fake
}
func packageGitExecute(g *issuePackageGitService, p issuePackageGitPreview) (map[string]any, error) {
	return g.execute(context.Background(), issuePackageRequest{ID: p.packageID, ReviewToken: p.ReviewToken, Confirm: "execute"})
}
func packageGitCalled(calls []string, step string) bool {
	for _, item := range calls {
		if item == step {
			return true
		}
	}
	return false
}

func TestIssuePackageGitRemoteValidation(t *testing.T) {
	for _, raw := range []string{"owner/repo", "https://github.com/owner/repo.git", "git@github.com:owner/repo.git", "ssh://git@github.com/owner/repo.git"} {
		if _, err := issuePackageGitRemote(raw, "release/2.14"); err != nil {
			t.Fatalf("valid remote rejected %q: %v", raw, err)
		}
	}
	for _, raw := range []string{"https://token@github.com/owner/repo", "https://github.com.evil.test/owner/repo", "https://github.com:443/owner/repo", "file:///tmp/repo", "ext::sh -c bad", "ssh://git:secret@github.com/owner/repo", "https://github.com/owner/repo?token=secret", "owner/../../repo", "-owner/repo", "git@evil.test:owner/repo.git"} {
		if _, err := issuePackageGitRemote(raw, "main"); err == nil {
			t.Fatalf("unsafe remote accepted %q", raw)
		}
	}
	for _, branch := range []string{"--upload-pack=bad", "a..b", "x.lock", "a/.hidden", "bad branch", "a//b"} {
		if _, err := issuePackageGitRemote("owner/repo", branch); err == nil {
			t.Fatalf("unsafe branch accepted %q", branch)
		}
	}
}
func TestIssuePackageGitPreviewIsLocalAndReviewIsRequired(t *testing.T) {
	g, pkg := packageGitTestService(t)
	p, fake := packageGitReview(t, g, pkg, "save")
	if len(fake.calls) != 0 || p.Digest == "" || len(p.Files) != 1 || !strings.HasSuffix(p.Files[0].Path, ".md") {
		t.Fatal("preview executed commands or included non-Markdown files")
	}
	raw, _ := json.Marshal(p.Files[0])
	if !strings.Contains(string(raw), `"bytes":`) {
		t.Fatal("file size JSON field changed")
	}
	for _, command := range p.Commands {
		if !strings.Contains(command.Display, "core.hooksPath=") || !strings.Contains(command.Display, "protocol.file.allow=never") {
			t.Fatal("review omits command safety arguments")
		}
		if strings.Contains(command.Display, "plan.json") {
			t.Fatal("portable metadata was staged")
		}
	}
	if _, err := g.execute(context.Background(), issuePackageRequest{ID: pkg.ID, ReviewToken: p.ReviewToken}); err == nil {
		t.Fatal("approval was optional")
	}
	if len(fake.calls) != 0 {
		t.Fatal("unapproved Git ran")
	}
	if _, err := packageGitExecute(g, p); err != nil {
		t.Fatal(err)
	}
	if !packageGitCalled(fake.calls, "pull") || !packageGitCalled(fake.calls, "commit") || !packageGitCalled(fake.calls, "push") {
		t.Fatalf("incomplete save: %v", fake.calls)
	}
	if _, err := packageGitExecute(g, p); err == nil {
		t.Fatal("review token reused")
	}
	content, err := os.ReadFile(filepath.Join(p.CheckoutPath, filepath.FromSlash(p.Files[0].Path)))
	if err != nil || !strings.Contains(string(content), pkg.Cases[0].Title) {
		t.Fatal("reviewed Markdown not written")
	}
	status, err := g.status(pkg.ID)
	if err != nil {
		t.Fatal(err)
	}
	history := status["gitHistory"].([]issuePackageGitCommit)
	if len(history) != 1 || history[0].URL != "https://github.com/owner/test-plans/commit/"+strings.Repeat("a", 40) {
		t.Fatal("GitHub history not linked")
	}
}
func TestIssuePackageGitChangedPlanAndExpiredReviewsNeverRun(t *testing.T) {
	g, pkg := packageGitTestService(t)
	p, fake := packageGitReview(t, g, pkg, "save")
	pkg = packageTestMutation(t, g.packages, issuePackageRequest{Action: "update", ID: pkg.ID, Revision: pkg.Revision, Title: "Changed after review", Status: pkg.Status, Cases: pkg.Cases}, nil, nil, nil)
	if _, err := packageGitExecute(g, p); err == nil || len(fake.calls) != 0 {
		t.Fatal("changed plan approval executed")
	}
	p, fake = packageGitReview(t, g, pkg, "pull")
	g.now = func() time.Time { return p.ExpiresAt.Add(time.Second) }
	if _, err := packageGitExecute(g, p); err == nil || len(fake.calls) != 0 {
		t.Fatal("expired approval executed")
	}
}
func TestIssuePackageGitPullConflictAndUnreviewedStagingStopPush(t *testing.T) {
	for _, kind := range []string{"pull-conflict", "extra-staged-file"} {
		t.Run(kind, func(t *testing.T) {
			g, pkg := packageGitTestService(t)
			p, fake := packageGitReview(t, g, pkg, "save")
			if kind == "pull-conflict" {
				fake.fail = "pull"
			} else {
				fake.staged = "private-notes.txt\x00"
			}
			if _, err := packageGitExecute(g, p); err == nil {
				t.Fatal("unsafe operation completed")
			}
			if packageGitCalled(fake.calls, "commit") || packageGitCalled(fake.calls, "push") {
				t.Fatalf("continued after failure: %v", fake.calls)
			}
		})
	}
}
func TestIssuePackageGitNoChangeSkipsCommitButCanRetryPush(t *testing.T) {
	g, pkg := packageGitTestService(t)
	p, fake := packageGitReview(t, g, pkg, "save")
	fake.changed = false
	if _, err := packageGitExecute(g, p); err != nil {
		t.Fatal(err)
	}
	if packageGitCalled(fake.calls, "commit") || !packageGitCalled(fake.calls, "push") {
		t.Fatal("no-change save did not skip commit and allow push retry")
	}
}
func TestIssuePackageGitRefusesHooksFiltersAndSymlinkTargets(t *testing.T) {
	for _, attack := range []string{"filter-config", "symlink-target"} {
		t.Run(attack, func(t *testing.T) {
			g, pkg := packageGitTestService(t)
			p, fake := packageGitReview(t, g, pkg, "save")
			fake.onStep = func(step string) {
				if step != "clone" {
					return
				}
				if attack == "filter-config" {
					file := filepath.Join(p.CheckoutPath, ".git", "config")
					f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0600)
					if err != nil {
						t.Fatal(err)
					}
					_, _ = f.WriteString("[filter \"bad\"]\nsmudge = arbitrary-command\n")
					_ = f.Close()
				} else {
					first := strings.Split(p.Files[0].Path, "/")[0]
					if err := os.Symlink(t.TempDir(), filepath.Join(p.CheckoutPath, first)); err != nil {
						t.Fatal(err)
					}
				}
			}
			if _, err := packageGitExecute(g, p); err == nil {
				t.Fatal("unsafe checkout accepted")
			}
			if packageGitCalled(fake.calls, "add") || packageGitCalled(fake.calls, "commit") || packageGitCalled(fake.calls, "push") {
				t.Fatalf("unsafe checkout wrote Git data: %v", fake.calls)
			}
		})
	}
}
func TestIssuePackageGitRemoteEditsAndFirstSaveCollisionsArePreserved(t *testing.T) {
	for _, known := range []bool{false, true} {
		t.Run(fmt.Sprint(known), func(t *testing.T) {
			g, pkg := packageGitTestService(t)
			p, fake := packageGitReview(t, g, pkg, "save")
			if known {
				if _, err := packageGitExecute(g, p); err != nil {
					t.Fatal(err)
				}
				p, fake = packageGitReview(t, g, pkg, "save")
			}
			remote := []byte("Someone else updated this file")
			fake.onStep = func(step string) {
				if step == "pull" {
					if err := issuePackageGitWriteFiles(p.CheckoutPath, map[string][]byte{p.Files[0].Path: remote}); err != nil {
						t.Fatal(err)
					}
				}
			}
			if _, err := packageGitExecute(g, p); err == nil || !strings.Contains(err.Error(), "changed at") {
				t.Fatalf("remote collision not rejected: %v", err)
			}
			if packageGitCalled(fake.calls, "add") || packageGitCalled(fake.calls, "push") {
				t.Fatal("remote edit overwritten")
			}
			content, _ := os.ReadFile(filepath.Join(p.CheckoutPath, filepath.FromSlash(p.Files[0].Path)))
			if string(content) != string(remote) {
				t.Fatal("remote file was modified")
			}
		})
	}
}
func TestIssuePackageGitImportLinksExistingMarkdownAndSavesBackToItsPath(t *testing.T) {
	g, pkg := packageGitTestService(t)
	folder, err := g.packages.preparePlanFolder(pkg.ID)
	if err != nil {
		t.Fatal(err)
	}
	p, fake := packageGitReview(t, g, pkg, "pull")
	source := "v2.14.0/12345-existing-plan.md"
	fake.onStep = func(step string) {
		if step == "pull" {
			if err := issuePackageGitWriteFiles(p.CheckoutPath, map[string][]byte{source: folder.Contents["test-plan.md"]}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := packageGitExecute(g, p); err != nil {
		t.Fatal(err)
	}
	count := len(fake.calls)
	result, err := g.importMarkdown(issuePackageRequest{ID: pkg.ID, GitPath: source})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.calls) != count {
		t.Fatal("import invoked Git")
	}
	imported := result["package"].(issuePackage)
	if imported.ID == pkg.ID || len(imported.Cases) != len(pkg.Cases) {
		t.Fatal("import did not create an independent case plan")
	}
	review, err := g.preview(issuePackageRequest{ID: imported.ID, Revision: imported.Revision, Kind: "save", Repository: p.Repository, GitBranch: p.Branch})
	if err != nil {
		t.Fatal(err)
	}
	if len(review.Files) != 1 || review.Files[0].Path != source {
		t.Fatalf("import lost original repository path: %#v", review.Files)
	}
	state, err := g.loadState()
	if err != nil || state.Packages[imported.ID].Baselines[source] == "" {
		t.Fatal("import lost remote content baseline")
	}
}
func TestIssuePackageGitBoundedOutputAndTraversal(t *testing.T) {
	output := &issuePackageGitOutput{}
	raw := []byte(strings.Repeat("x", 100000))
	n, err := output.Write(raw)
	if err != nil || n != len(raw) || len(output.data) != 64<<10 {
		t.Fatal("command output was not bounded")
	}
	for _, name := range []string{"../outside.md", "/absolute.md", "a/../../outside.md", ".git/config", "a\\b.md", "-option.md"} {
		if issuePackageGitSafeRelative(name) {
			t.Fatalf("unsafe path accepted %q", name)
		}
	}
}

func TestIssuePackageGitFailedCommitAndPushCanRetryOnlyOwnedWriting(t *testing.T) {
	for _, failure := range []string{"commit", "push"} {
		t.Run(failure, func(t *testing.T) {
			g, pkg := packageGitTestService(t)
			p, fake := packageGitReview(t, g, pkg, "save")
			fake.fail = failure
			if _, err := packageGitExecute(g, p); err == nil {
				t.Fatal("simulated failure succeeded")
			}
			state, err := g.loadState()
			if err != nil || state.Packages[pkg.ID].Pending[p.Files[0].Path] == "" {
				t.Fatal("failed attempt did not retain recovery digest")
			}
			pkg.Cases[0].Expected = "A more precise expected behavior"
			pkg = packageTestMutation(t, g.packages, issuePackageRequest{Action: "update", ID: pkg.ID, Revision: pkg.Revision, Title: pkg.Title, Status: pkg.Status, Cases: pkg.Cases}, nil, nil, nil)
			retry, retryFake := packageGitReview(t, g, pkg, "save")
			if failure == "commit" {
				retryFake.status = "A  " + p.Files[0].Path + "\x00"
			}
			if _, err := packageGitExecute(g, retry); err != nil {
				t.Fatalf("owned retry failed: %v", err)
			}
			if !packageGitCalled(retryFake.calls, "push") {
				t.Fatal("retry never pushed")
			}
			state, err = g.loadState()
			if err != nil || len(state.Packages[pkg.ID].Pending) != 0 {
				t.Fatal("successful retry did not clear pending recovery")
			}
		})
	}
}
func TestIssuePackageGitRetryRejectsOutsideChanges(t *testing.T) {
	g, pkg := packageGitTestService(t)
	p, fake := packageGitReview(t, g, pkg, "save")
	fake.fail = "commit"
	if _, err := packageGitExecute(g, p); err == nil {
		t.Fatal("simulated commit failure succeeded")
	}
	if err := os.WriteFile(filepath.Join(p.CheckoutPath, filepath.FromSlash(p.Files[0].Path)), []byte("outside edit"), 0600); err != nil {
		t.Fatal(err)
	}
	retry, retryFake := packageGitReview(t, g, pkg, "save")
	retryFake.status = "AM " + p.Files[0].Path + "\x00"
	if _, err := packageGitExecute(g, retry); err == nil || packageGitCalled(retryFake.calls, "pull") {
		t.Fatal("modified pending file was treated as safe recovery")
	}
}
func TestIssuePackageGitFolderWarningCannotUploadExternalEdits(t *testing.T) {
	g, pkg := packageGitTestService(t)
	if _, err := g.link(issuePackageRequest{ID: pkg.ID, Repository: "owner/test-plans"}); err != nil {
		t.Fatal(err)
	}
	folder, err := g.packages.preparePlanFolder(pkg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder.Path, "test-plan.md"), []byte("externally edited Markdown"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := g.preview(issuePackageRequest{ID: pkg.ID, Revision: pkg.Revision, Kind: "save", Repository: "owner/test-plans"}); err == nil || !strings.Contains(err.Error(), "local Markdown has edits") {
		t.Fatalf("external edit silently uploaded: %v", err)
	}
}
