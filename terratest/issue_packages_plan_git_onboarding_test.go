package test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func packageGitOnboardingService(t *testing.T, s *issuePackageService) *issuePackageGitService {
	t.Helper()
	return &issuePackageGitService{packages: s, reviews: map[string]issuePackageGitPreview{}, now: time.Now, runner: func(context.Context, string, []string) (issuePackageGitRunResult, error) {
		t.Fatal("onboarding invoked a repository command")
		return issuePackageGitRunResult{}, nil
	}}
}

func TestIssuePackageGitOnboardingBeforeAnyPlanIsReadOnlyAndSkipIsTemporary(t *testing.T) {
	g := packageGitOnboardingService(t, packageTestService(t))
	for range 2 {
		status, err := g.onboarding("")
		if err != nil || status["required"] != true || status["dismissed"] != false || status["connection"] != nil || status["connectionScope"] != "none" || status["linkScope"] != "default" {
			t.Fatalf("unexpected first-time onboarding: %v %v", status, err)
		}
	}
	if _, err := g.dismissOnboarding(issuePackageRequest{}); err == nil {
		t.Fatal("Skip silently became a permanent preference")
	}
	if _, err := os.Stat(g.storageRoot()); !os.IsNotExist(err) {
		t.Fatal("reading onboarding or skipping created repository settings")
	}
	status, err := g.status("")
	if err != nil || status["connection"] != nil || status["connectionScope"] != "none" {
		t.Fatal("global status required a plan")
	}
}

func TestIssuePackageGitOnboardingDismissalSurvivesRestartLinkAndUnlink(t *testing.T) {
	s := packageTestService(t)
	g := packageGitOnboardingService(t, s)
	status, err := g.dismissOnboarding(issuePackageRequest{DontAskAgain: true})
	if err != nil || status["required"] != false || status["dismissed"] != true {
		t.Fatalf("don't-ask preference not saved: %v %v", status, err)
	}
	restored, err := newIssuePackageService(s.root)
	if err != nil {
		t.Fatal(err)
	}
	g = packageGitOnboardingService(t, restored)
	status, err = g.onboarding("")
	if err != nil || status["required"] != false || status["dismissed"] != true {
		t.Fatal("restart forgot don't-ask preference")
	}
	// Suppression affects only onboarding; users can still link manually.
	packageGitLink(t, g, "", "owner/test-plans", "main")
	if _, err := g.unlink(""); err != nil {
		t.Fatal(err)
	}
	pkg := packageTestPlan(t, restored)
	status, err = g.onboarding(pkg.ID)
	if err != nil || status["required"] != false || status["dismissed"] != true || status["connection"] != nil {
		t.Fatal("link/unlink or a new plan reset don't-ask preference")
	}
	state, err := g.loadState()
	if err != nil || len(state.Packages) != 0 {
		t.Fatal("global setup created a fake empty-ID package association")
	}
	if _, err := os.Stat(filepath.Join(g.storageRoot(), "checkouts")); !os.IsNotExist(err) {
		t.Fatal("local preference setup created checkout files")
	}
}

func TestIssuePackageGitGlobalOnboardingLinkInheritsAndPreservesSourceRecovery(t *testing.T) {
	g := packageGitOnboardingService(t, packageTestService(t))
	linked := packageGitLink(t, g, "", "owner/test-plans", "main")
	if linked["connectionScope"] != "default" || linked["checkStatus"] != "not-checked" {
		t.Fatal("global link required a plan or claimed network verification")
	}
	pkg := packageTestPlan(t, g.packages)
	status, err := g.onboarding(pkg.ID)
	if err != nil || status["required"] != false || status["dismissed"] != false || status["connectionScope"] != "default" {
		t.Fatal("new plan did not inherit onboarding connection")
	}
	state, _ := g.loadState()
	oldDefault := state.DefaultConnection
	state.Packages[pkg.ID] = issuePackageGitSaved{Connection: oldDefault, Baselines: map[string]string{"saved.md": "base"}, Pending: map[string]string{"saved.md": "pending"}, PendingHead: strings.Repeat("b", 40)}
	imported := packageTestPlan(t, g.packages)
	other, _ := issuePackageGitRemote("owner/imported", "main")
	state.Packages[imported.ID] = issuePackageGitSaved{Connection: other, Explicit: true, SourcePath: "existing.md", Pending: map[string]string{"existing.md": "source-pending"}}
	if err := g.saveState(state); err != nil {
		t.Fatal(err)
	}
	inheritedReview := packageGitLifecycleReview(t, g, pkg, "check", oldDefault.Repository)
	explicitReview := packageGitLifecycleReview(t, g, imported, "check", other.Repository)
	packageGitLink(t, g, "", "owner/replacement", "main")
	state, _ = g.loadState()
	archived := state.Packages[pkg.ID].Previous[issuePackageGitConnectionKey(oldDefault)]
	if archived.Baselines["saved.md"] != "base" || archived.Pending["saved.md"] != "pending" || archived.PendingHead == "" || state.Packages[imported.ID].SourcePath != "existing.md" {
		t.Fatal("global onboarding relink erased prior recovery or imported source")
	}
	if _, exists := g.reviews[inheritedReview.ReviewToken]; exists {
		t.Fatal("global onboarding relink retained inherited approval")
	}
	if _, exists := g.reviews[explicitReview.ReviewToken]; !exists {
		t.Fatal("global onboarding relink revoked imported approval")
	}
	if _, exists := state.Packages[""]; exists {
		t.Fatal("global link wrote an empty package entry")
	}
	if _, err := g.unlink(""); err != nil {
		t.Fatal(err)
	}
	status, err = g.onboarding(pkg.ID)
	if err != nil || status["required"] != true {
		t.Fatal("unlink without don't-ask did not restore onboarding")
	}
	status, err = g.onboarding(imported.ID)
	if err != nil || status["required"] != false || status["connectionScope"] != "plan" {
		t.Fatal("an explicit imported link did not suppress onboarding")
	}
	if _, err := g.unlink(imported.ID); err != nil {
		t.Fatal(err)
	}
	status, err = g.onboarding(imported.ID)
	if err != nil || status["required"] != true || status["linkScope"] != "plan" {
		t.Fatal("an unlinked imported plan lost its own link scope")
	}
	packageGitLink(t, g, "", "owner/shared", "main")
	status, err = g.onboarding(imported.ID)
	if err != nil || status["required"] != false || status["connection"] != nil || status["linkScope"] != "plan" {
		t.Fatal("global connection should suppress prompts without changing explicit unlink")
	}
}

func TestIssuePackageGitOnboardingHTTPRoutesAndUnknownPlanAreSafe(t *testing.T) {
	p := clusterWorkspaceTestPanel(t)
	p.issuePackages = packageTestService(t)
	g := packageGitOnboardingService(t, p.issuePackages)
	issuePackageGitServices.Store(p.issuePackages, g)
	t.Cleanup(func() { issuePackageGitServices.Delete(p.issuePackages) })
	if _, err := g.dismissOnboarding(issuePackageRequest{ID: "missing", DontAskAgain: true}); err == nil {
		t.Fatal("invalid package request changed global preference")
	}
	for _, req := range []issuePackageRequest{{Action: "plan-git-onboarding"}, {Action: "plan-git-onboarding-dismiss", DontAskAgain: true}, {Action: "plan-git-link", Repository: "owner/plans", GitBranch: "main"}} {
		body, _ := json.Marshal(req)
		r := httptest.NewRequest("POST", "http://localhost/api/issue-packages", strings.NewReader(string(body)))
		r.RemoteAddr = "127.0.0.1:1"
		r.Header.Set("Origin", "http://localhost")
		w := httptest.NewRecorder()
		p.handleIssuePackages(w, r)
		if w.Code != 200 {
			t.Fatalf("%s failed: %d %s", req.Action, w.Code, w.Body.String())
		}
		var result map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if req.Action == "plan-git-onboarding-dismiss" && (result["dismissed"] != true || result["required"] != false) {
			t.Fatal("HTTP dismissal did not persist preference")
		}
	}
}
