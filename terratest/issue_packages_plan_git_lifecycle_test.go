package test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func packageGitLink(t *testing.T, g *issuePackageGitService, id, repository, branch string) map[string]any {
	t.Helper()
	out, err := g.link(issuePackageRequest{ID: id, Repository: repository, GitBranch: branch})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func packageGitLifecycleReview(t *testing.T, g *issuePackageGitService, pkg issuePackage, kind, repository string) issuePackageGitPreview {
	t.Helper()
	p, err := g.preview(issuePackageRequest{ID: pkg.ID, Revision: pkg.Revision, Kind: kind, Repository: repository, GitBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestIssuePackageGitDefaultLinkPersistsWithoutCommandsAndProtectsOverrides(t *testing.T) {
	g, first := packageGitTestService(t)
	second := packageTestPlan(t, g.packages)
	imported := packageTestPlan(t, g.packages)
	initial, err := g.status(first.ID)
	if err != nil || initial["connection"] != nil || initial["connectionScope"] != "none" || initial["linkScope"] != "default" {
		t.Fatal("new plans did not begin with an unlinked default")
	}
	linked := packageGitLink(t, g, first.ID, "owner/test-plans", "main")
	if linked["connectionScope"] != "default" || linked["checkStatus"] != "not-checked" {
		t.Fatal("link claimed verified access or incorrect scope")
	}
	state, _ := g.loadState()
	defaultConnection := state.DefaultConnection
	otherConnection, _ := issuePackageGitRemote("owner/imported-plans", "main")
	state.Packages[imported.ID] = issuePackageGitSaved{Connection: otherConnection, Explicit: true, SourcePath: "v2.14/imported.md", Baselines: map[string]string{"v2.14/imported.md": "baseline"}, Pending: map[string]string{"v2.14/imported.md": "pending"}, PendingHead: strings.Repeat("a", 40)}
	if err := g.saveState(state); err != nil {
		t.Fatal(err)
	}
	// A service restart reads the same default without invoking any command.
	restarted := &issuePackageGitService{packages: g.packages, runner: g.runner, reviews: map[string]issuePackageGitPreview{}, now: time.Now}
	status, err := restarted.status(second.ID)
	if err != nil || status["connection"] != defaultConnection || status["connectionScope"] != "default" {
		t.Fatal("another plan did not inherit the remembered default")
	}
	firstReview := packageGitLifecycleReview(t, g, first, "pull", "owner/test-plans")
	secondReview := packageGitLifecycleReview(t, g, second, "check", "owner/test-plans")
	importReview := packageGitLifecycleReview(t, g, imported, "check", "owner/imported-plans")
	if _, err := g.link(issuePackageRequest{ID: first.ID, Repository: "https://token@github.com/owner/wrong"}); err == nil {
		t.Fatal("invalid relink accepted")
	}
	if len(g.reviews) != 3 {
		t.Fatal("invalid relink disturbed active reviews")
	}
	packageGitLink(t, g, second.ID, "owner/replacement", "main")
	if _, ok := g.reviews[firstReview.ReviewToken]; ok {
		t.Fatal("default relink retained first plan approval")
	}
	if _, ok := g.reviews[secondReview.ReviewToken]; ok {
		t.Fatal("default relink retained another inherited approval")
	}
	if _, ok := g.reviews[importReview.ReviewToken]; !ok {
		t.Fatal("default relink revoked an unrelated imported override")
	}
	state, _ = g.loadState()
	if got := state.Packages[imported.ID]; got.Connection != otherConnection || got.SourcePath != "v2.14/imported.md" || got.PendingHead == "" {
		t.Fatal("default relink changed imported source recovery")
	}
	if _, err := g.unlink(first.ID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{first.ID, second.ID, packageTestPlan(t, g.packages).ID} {
		status, err := g.status(id)
		if err != nil || status["connection"] != nil || status["linkScope"] != "default" {
			t.Fatal("default unlink failed to unlink ordinary/new plans")
		}
	}
	status, _ = g.status(imported.ID)
	if status["connection"] != otherConnection || status["linkScope"] != "plan" {
		t.Fatal("default unlink removed explicit source link")
	}
	if _, err := os.Stat(filepath.Join(g.storageRoot(), "checkouts")); !os.IsNotExist(err) {
		t.Fatal("local linking or previews created checkout files")
	}
}

func TestIssuePackageGitImportedUnlinkRetainsRecoveryWithoutDefaultFallback(t *testing.T) {
	g, pkg := packageGitTestService(t)
	packageGitLink(t, g, pkg.ID, "owner/test-plans", "main")
	state, _ := g.loadState()
	c, _ := issuePackageGitRemote("owner/source", "main")
	state.Packages[pkg.ID] = issuePackageGitSaved{Connection: c, Explicit: true, SourcePath: "cases/old.md", Baselines: map[string]string{"cases/old.md": "old"}, Pending: map[string]string{"cases/old.md": "pending"}, PendingHead: strings.Repeat("b", 40), GitHistory: []issuePackageGitCommit{{Commit: strings.Repeat("b", 40), URL: c.WebURL + "/commit/" + strings.Repeat("b", 40)}}}
	if err := g.saveState(state); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(g.checkout(c), 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(g.checkout(c), "preserved.md")
	if err := os.WriteFile(marker, []byte("preserve me"), 0600); err != nil {
		t.Fatal(err)
	}
	p := packageGitLifecycleReview(t, g, pkg, "check", "owner/source")
	status, err := g.unlink(pkg.ID)
	if err != nil || status["connection"] != nil || status["connectionScope"] != "none" || status["linkScope"] != "plan" {
		t.Fatal("imported unlink unexpectedly inherited default")
	}
	if _, ok := g.reviews[p.ReviewToken]; ok {
		t.Fatal("unlinked source retained approval")
	}
	if _, err := g.importMarkdown(issuePackageRequest{ID: pkg.ID, GitPath: "preserved.md"}); err == nil {
		t.Fatal("unlinked checkout remained importable")
	}
	if raw, _ := os.ReadFile(marker); string(raw) != "preserve me" || len(status["gitHistory"].([]issuePackageGitCommit)) != 1 {
		t.Fatal("unlink deleted files or history")
	}
	packageGitLink(t, g, pkg.ID, "https://github.com/owner/source.git", "main")
	state, _ = g.loadState()
	restored := state.Packages[pkg.ID]
	if restored.SourcePath != "cases/old.md" || restored.Pending["cases/old.md"] != "pending" || restored.PendingHead == "" || restored.Disabled {
		t.Fatal("relink did not restore exact source recovery")
	}
	packageGitLink(t, g, pkg.ID, "owner/source", "main")
	state, _ = g.loadState()
	if !reflect.DeepEqual(state.Packages[pkg.ID].Baselines, restored.Baselines) || state.Packages[pkg.ID].SourcePath != restored.SourcePath {
		t.Fatal("same canonical relink erased source metadata")
	}
	packageGitLink(t, g, pkg.ID, "owner/other-source", "main")
	state, _ = g.loadState()
	if got := state.Packages[pkg.ID]; got.SourcePath != "" || len(got.Pending) != 0 || len(got.Baselines) != 0 {
		t.Fatal("relink leaked source metadata into a different repository")
	}
	if state.DefaultConnection.Repository != "https://github.com/owner/test-plans.git" {
		t.Fatal("imported relink changed global default")
	}
}

func TestIssuePackageGitConnectionCheckRequiresReviewAndSharesOnlyReadStatus(t *testing.T) {
	g, pkg := packageGitTestService(t)
	second := packageTestPlan(t, g.packages)
	packageGitLink(t, g, pkg.ID, "owner/test-plans", "main")
	p := packageGitLifecycleReview(t, g, pkg, "check", "owner/test-plans")
	if len(p.Commands) != 1 || len(p.Files) != 0 || p.CheckoutPath != "" || !strings.Contains(p.Commands[0].Display, "ls-remote --exit-code --heads https://github.com/owner/test-plans.git refs/heads/main") {
		t.Fatal("connection review included extra actions or wrong remote/ref")
	}
	fake := &packageGitFake{t: t, preview: p}
	g.runner = fake.run
	if _, err := g.execute(context.Background(), issuePackageRequest{ID: pkg.ID, ReviewToken: p.ReviewToken}); err == nil || len(fake.calls) != 0 {
		t.Fatal("connection check bypassed approval")
	}
	status, err := packageGitExecute(g, p)
	if err != nil || status["checkStatus"] != "readable" || status["checkedAt"] == nil || !strings.Contains(status["checkMessage"].(string), "push has not been tested") {
		t.Fatalf("read access was not clearly recorded: %v %v", status, err)
	}
	if !reflect.DeepEqual(fake.calls, []string{"check"}) {
		t.Fatal("connection test ran more than ls-remote")
	}
	status, _ = g.status(second.ID)
	if status["checkStatus"] != "readable" {
		t.Fatal("another ordinary plan lost shared read check")
	}
	if _, err := os.Stat(filepath.Join(g.storageRoot(), "checkouts")); !os.IsNotExist(err) {
		t.Fatal("connection check created a checkout")
	}
	packageGitLink(t, g, second.ID, "owner/new-repo", "main")
	status, _ = g.status(pkg.ID)
	if status["checkStatus"] != "not-checked" || status["checkedAt"] != nil {
		t.Fatal("relink inherited another repository's verified status")
	}
}

func TestIssuePackageGitConnectionFailuresAreRecordedAndMissingBranchExplained(t *testing.T) {
	for _, failure := range []string{"missing", "auth", "malformed", "wrong-ref", "duplicate", "invalid-sha-length"} {
		t.Run(failure, func(t *testing.T) {
			g, pkg := packageGitTestService(t)
			packageGitLink(t, g, pkg.ID, "owner/test-plans", "main")
			p := packageGitLifecycleReview(t, g, pkg, "check", "owner/test-plans")
			calls := 0
			g.runner = func(_ context.Context, dir string, args []string) (issuePackageGitRunResult, error) {
				calls++
				if dir != p.Commands[0].Directory || !reflect.DeepEqual(args, p.Commands[0].Args) {
					t.Fatal("unreviewed command executed")
				}
				switch failure {
				case "missing":
					return issuePackageGitRunResult{ExitCode: 2}, nil
				case "auth":
					return issuePackageGitRunResult{ExitCode: 128, Output: "Authentication failed"}, nil
				case "wrong-ref":
					return issuePackageGitRunResult{Output: strings.Repeat("a", 40) + "\trefs/heads/other\n"}, nil
				case "invalid-sha-length":
					return issuePackageGitRunResult{Output: strings.Repeat("a", 42) + "\trefs/heads/main\n"}, nil
				case "duplicate":
					return issuePackageGitRunResult{Output: strings.Repeat(strings.Repeat("a", 40)+"\trefs/heads/main\n", 2)}, nil
				}
				return issuePackageGitRunResult{Output: "not-a-sha\trefs/heads/main\n"}, nil
			}
			if _, err := packageGitExecute(g, p); err == nil || (failure == "missing" && !strings.Contains(err.Error(), "README")) {
				t.Fatalf("failed check was not explained: %v", err)
			}
			status, err := g.status(pkg.ID)
			if err != nil || status["checkStatus"] != "failed" || status["checkedAt"] == nil || calls != 1 {
				t.Fatal("failed read check was not preserved")
			}
			if _, err := packageGitExecute(g, p); err == nil || calls != 1 {
				t.Fatal("failed check reused approval")
			}
		})
	}
}

func TestIssuePackageGitCannotReviewAnotherRepositoryOrExecuteStaleLink(t *testing.T) {
	g, pkg := packageGitTestService(t)
	packageGitLink(t, g, pkg.ID, "owner/test-plans", "main")
	for _, kind := range []string{"save", "pull", "check"} {
		if _, err := g.preview(issuePackageRequest{ID: pkg.ID, Kind: kind, Repository: "owner/unlinked"}); err == nil {
			t.Fatalf("%s reviewed an unlinked repository", kind)
		}
		p := packageGitLifecycleReview(t, g, pkg, kind, "owner/test-plans")
		packageGitLink(t, g, pkg.ID, "owner/replacement", "main")
		// Even an old token accidentally retained in memory is bound to its link.
		g.reviews[p.ReviewToken] = p
		if _, err := packageGitExecute(g, p); err == nil {
			t.Fatalf("%s executed after its link changed", kind)
		}
		packageGitLink(t, g, pkg.ID, "owner/test-plans", "main")
	}
}

func TestIssuePackageGitGlobalCachedImportBindsDefaultAndKeepsSource(t *testing.T) {
	g, pkg := packageGitTestService(t)
	packageGitLink(t, g, pkg.ID, "owner/test-plans", "main")
	state, _ := g.loadState()
	c := state.DefaultConnection
	if err := os.MkdirAll(g.checkout(c), 0700); err != nil {
		t.Fatal(err)
	}
	if err := issuePackageGitWriteFiles(g.checkout(c), map[string][]byte{"v2.14/123.md": []byte("1. Test imported login\n2. Test visibility\n")}); err != nil {
		t.Fatal(err)
	}
	for _, repository := range []string{"", "owner/wrong"} {
		if _, err := g.importMarkdown(issuePackageRequest{GitPath: "v2.14/123.md", Repository: repository, GitBranch: "main"}); err == nil {
			t.Fatal("unbound global result imported")
		}
	}
	out, err := g.importMarkdown(issuePackageRequest{GitPath: "v2.14/123.md", Repository: c.Repository, GitBranch: c.Branch})
	if err != nil {
		t.Fatal(err)
	}
	imported := out["package"].(issuePackage)
	state, _ = g.loadState()
	if _, exists := state.Packages[""]; exists {
		t.Fatal("global import created an empty package association")
	}
	saved := state.Packages[imported.ID]
	if !saved.Explicit || saved.SourcePath != "v2.14/123.md" || saved.Connection != c || len(imported.Cases) != 2 {
		t.Fatal("global import lost its exact source")
	}
	packageGitLink(t, g, pkg.ID, "owner/replacement", "main")
	if _, err := g.importMarkdown(issuePackageRequest{GitPath: "v2.14/123.md", Repository: c.Repository, GitBranch: c.Branch}); err == nil {
		t.Fatal("stale result imported from a changed default")
	}
}
