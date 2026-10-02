package test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func packageTestUpdate(t *testing.T, s *issuePackageService, pkg issuePackage, fixURL, fixTitle string) issuePackage {
	t.Helper()
	return packageTestMutation(t, s, issuePackageRequest{Action: "update", ID: pkg.ID, Revision: pkg.Revision, Title: pkg.Title, IssueURL: pkg.IssueURL, IssueTitle: pkg.IssueTitle, FixURL: fixURL, FixTitle: fixTitle, Summary: pkg.Summary, Status: pkg.Status, Notes: pkg.Notes, Cases: pkg.Cases}, nil, nil, nil)
}

func packageTestFinish(t *testing.T, s *issuePackageService, pkg issuePackage, sessionID, finding string) issuePackage {
	t.Helper()
	return packageTestMutation(t, s, issuePackageRequest{Action: "finish-session", ID: pkg.ID, Revision: pkg.Revision, SessionID: sessionID, Finding: finding, Conclusion: "Recorded for the journey test."}, nil, nil, nil)
}

func TestIssuePackageFixReferenceIsValidatedAndFrozenPerSession(t *testing.T) {
	s := packageTestService(t)
	if _, err := s.mutate(issuePackageRequest{Action: "create", Title: "Unsafe fix link", FixURL: "javascript:alert(1)"}, nil, nil, nil); err == nil {
		t.Fatal("unsafe fix URL accepted on create")
	}
	pkg := packageTestPlan(t, s)
	if _, err := s.mutate(issuePackageRequest{Action: "update", ID: pkg.ID, Revision: pkg.Revision, Title: pkg.Title, Status: pkg.Status, Cases: pkg.Cases, FixURL: "https://user:secret@github.com/rancher/rancher/pull/500"}, nil, nil, nil); err == nil {
		t.Fatal("fix URL with embedded credentials accepted")
	}
	pkg = packageTestUpdate(t, s, pkg, " https://github.com/rancher/rancher/pull/500 ", "Invalidate cached project lists")
	if pkg.FixURL != "https://github.com/rancher/rancher/pull/500" || pkg.FixTitle != "Invalidate cached project lists" {
		t.Fatalf("fix reference not saved: %q %q", pkg.FixURL, pkg.FixTitle)
	}
	e := issuePackageEnvironment{ClusterName: "Candidate", RancherVersion: "v2.15.3-rc1", Source: "manual", RecordedAt: time.Now().UTC()}
	if _, err := s.mutate(issuePackageRequest{Action: "start-session", ID: pkg.ID, Revision: pkg.Revision, Name: "Bad candidate", Purpose: "validation", FixURL: "ftp://example.test/patch"}, &e, nil, nil); err == nil {
		t.Fatal("session accepted a non-HTTP fix reference")
	}
	pkg = packageTestMutation(t, s, issuePackageRequest{Action: "start-session", ID: pkg.ID, Revision: pkg.Revision, Name: "Candidate 1", Purpose: "validation", FixURL: pkg.FixURL, FixTitle: pkg.FixTitle}, &e, nil, nil)
	session := pkg.Sessions[0]
	if session.FixURL != "https://github.com/rancher/rancher/pull/500" || session.FixTitle != "Invalidate cached project lists" {
		t.Fatalf("session did not freeze the fix under test: %+v", session)
	}
	pkg = packageTestUpdate(t, s, pkg, "https://github.com/rancher/rancher/pull/501", "Second candidate")
	if pkg.FixURL != "https://github.com/rancher/rancher/pull/501" || pkg.Sessions[0].FixURL != "https://github.com/rancher/rancher/pull/500" || pkg.Sessions[0].FixTitle != "Invalidate cached project lists" {
		t.Fatal("editing the package fix reference rewrote a preserved session")
	}
	bundle, err := s.buildBundle(pkg.ID, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = validateIssuePackageBundle(&bundle); err != nil {
		t.Fatal(err)
	}
	imported, err := s.importPackage(bundle.Package, map[string][]byte{})
	if err != nil {
		t.Fatal(err)
	}
	if imported.FixURL != pkg.FixURL || imported.FixTitle != pkg.FixTitle || imported.Sessions[0].FixURL != session.FixURL || imported.Sessions[0].FixTitle != session.FixTitle {
		t.Fatal("portable bundle lost the package or session fix reference")
	}
	restarted, err := newIssuePackageService(s.root)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := restarted.get(pkg.ID)
	if err != nil || reopened.FixURL != pkg.FixURL || reopened.Sessions[0].FixURL != session.FixURL {
		t.Fatalf("fix reference did not survive reopening: %v", err)
	}
}

func TestIssuePackageDraftFingerprintIgnoresAbsentFixReference(t *testing.T) {
	s := packageTestService(t)
	pkg := packageTestPlan(t, s)
	plan := issuePackageEditablePlan(pkg)
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "fixUrl") || strings.Contains(string(raw), "fixTitle") {
		t.Fatal("an absent fix reference changed the saved plan identity for existing drafts")
	}
	before := issuePackagePlanFingerprint(plan)
	pkg = packageTestUpdate(t, s, pkg, "https://github.com/rancher/rancher/pull/500", "")
	after := issuePackagePlanFingerprint(issuePackageEditablePlan(pkg))
	if before == after {
		t.Fatal("linking a fix did not change the plan checkpoint")
	}
	initial := packageTestDraft(t, s, issuePackageRequest{Action: "draft-read", ID: pkg.ID})
	if initial.CurrentPlanFingerprint != after {
		t.Fatal("draft checkpoint disagrees with the saved plan fingerprint")
	}
	plan = issuePackageEditablePlan(pkg)
	plan.FixURL = "https://incomplete-"
	plan.FixTitle = strings.Repeat("x", 501)
	draft := issuePackageWritingDraft{BasePlanFingerprint: after, Plan: &plan}
	if _, err := s.handleWritingDraft(issuePackageRequest{Action: "draft-write", ID: pkg.ID, Draft: &draft}); err == nil {
		t.Fatal("oversized fix title accepted in a writing draft")
	}
	plan.FixTitle = "Still writing"
	saved := packageTestDraft(t, s, issuePackageRequest{Action: "draft-write", ID: pkg.ID, Draft: &draft})
	if saved.Draft.Plan == nil || saved.Draft.Plan.FixURL != "https://incomplete-" || saved.Draft.Plan.FixTitle != "Still writing" {
		t.Fatal("writing draft lost the in-progress fix reference")
	}
	if preserved, _ := s.get(pkg.ID); preserved.FixURL != "https://github.com/rancher/rancher/pull/500" {
		t.Fatal("writing draft changed the saved fix reference")
	}
}

func TestIssuePackageJourneyDoesNotValidateAReplacementFix(t *testing.T) {
	pkg := issuePackage{
		Cases:  []issuePackageCase{{Title: "Case"}},
		FixURL: "https://github.com/rancher/rancher/pull/501",
		Sessions: []issuePackageSession{
			{Name: "Original", Purpose: "reproduction", Status: "completed", Finding: "reproduced"},
			{Name: "Earlier candidate", Purpose: "validation", Status: "completed", Finding: "validated", FixURL: "https://github.com/rancher/rancher/pull/500"},
		},
	}
	journey := issuePackageJourneyFor(pkg)
	if journey.Action != "start-validation" || journey.Stages[3].State != "attention" || !strings.Contains(journey.Stages[3].Detail, "Current fix has not been validated") {
		t.Fatalf("old validation endorsed replacement fix: %+v", journey)
	}
	pkg.FixURL = "https://github.com/Rancher/rancher/pull/500/files"
	if journey := issuePackageJourneyFor(pkg); journey.Action != "share" {
		t.Fatalf("equivalent PR links must match: %+v", journey)
	}
	pkg.Sessions[1].FixURL = ""
	if journey := issuePackageJourneyFor(pkg); journey.Stages[3].State != "attention" {
		t.Fatal("a session without a recorded fix cannot validate the current link")
	}
}

func TestIssuePackageSingleSessionReportScopesProgress(t *testing.T) {
	s := packageTestService(t)
	pkg := packageTestStart(t, s, packageTestPlan(t, s))
	reproductionID := pkg.Sessions[0].ID
	pkg = packageTestFinish(t, s, pkg, reproductionID, "reproduced")
	environment := issuePackageEnvironment{ClusterName: "Candidate", Source: "manual", RecordedAt: time.Now().UTC()}
	pkg = packageTestMutation(t, s, issuePackageRequest{Action: "start-session", ID: pkg.ID, Revision: pkg.Revision, Name: "Unshared candidate", Purpose: "validation"}, &environment, nil, nil)
	validationID := pkg.Sessions[1].ID
	pkg = packageTestFinish(t, s, pkg, validationID, "validated")
	report, err := issuePackageMarkdown(pkg, reproductionID, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(report, "Unshared candidate") || strings.Contains(report, "Fix validated") || !strings.Contains(report, "| Validation | Not started |") {
		t.Fatalf("selected-session report included another session's progress:\n%s", report)
	}
}

func TestIssuePackageJourneyFollowsPlanReproductionFixValidation(t *testing.T) {
	s := packageTestService(t)
	pkg := packageTestMutation(t, s, issuePackageRequest{Action: "create", Title: "Journey"}, nil, nil, nil)
	expect := func(step string, action string, states ...string) {
		t.Helper()
		journey := issuePackageJourneyFor(pkg)
		if len(journey.Stages) != 4 || journey.Stages[0].ID != "plan" || journey.Stages[1].ID != "reproduction" || journey.Stages[2].ID != "fix" || journey.Stages[3].ID != "validation" {
			t.Fatalf("%s: unexpected stages %+v", step, journey.Stages)
		}
		for i, state := range states {
			if journey.Stages[i].State != state {
				t.Fatalf("%s: stage %s is %q (%s), want %q", step, journey.Stages[i].ID, journey.Stages[i].State, journey.Stages[i].Detail, state)
			}
		}
		if journey.Action != action || journey.Next == "" {
			t.Fatalf("%s: next action %q (%q), want %q", step, journey.Action, journey.Next, action)
		}
	}
	expect("empty package", "plan", "pending", "pending", "pending", "pending")
	pkg = packageTestMutation(t, s, issuePackageRequest{Action: "update", ID: pkg.ID, Revision: pkg.Revision, Title: pkg.Title, Status: "planning", Cases: []issuePackageCase{{Title: "Project member sees the resource", Steps: []issuePackageStep{{Instruction: "Open the project", Expected: "Visible"}}}}}, nil, nil, nil)
	expect("plan saved", "start-reproduction", "done", "pending", "pending", "pending")
	if detail := issuePackageJourneyFor(pkg).Stages[0].Detail; detail != "1 saved case" {
		t.Fatalf("plan detail %q", detail)
	}
	pkg = packageTestStart(t, s, pkg)
	first := pkg.Sessions[0].ID
	expect("reproduction active", "finish-reproduction", "done", "active", "pending", "pending")
	pkg = packageTestFinish(t, s, pkg, first, "not-reproduced")
	expect("reproduction not established", "start-reproduction", "done", "attention", "pending", "pending")
	pkg = packageTestStart(t, s, pkg)
	second := pkg.Sessions[1].ID
	pkg = packageTestFinish(t, s, pkg, second, "reproduced")
	expect("latest reproduction wins", "link-fix", "done", "done", "pending", "pending")
	exploration := issuePackageEnvironment{ClusterName: "Scratch", Source: "manual", RecordedAt: time.Now().UTC()}
	pkg = packageTestMutation(t, s, issuePackageRequest{Action: "start-session", ID: pkg.ID, Revision: pkg.Revision, Name: "Poking around", Purpose: "exploration"}, &exploration, nil, nil)
	expect("exploration does not move the journey", "link-fix", "done", "done", "pending", "pending")
	pkg = packageTestUpdate(t, s, pkg, "https://github.com/rancher/rancher/pull/500", "Invalidate cached project lists")
	expect("fix linked", "start-validation", "done", "done", "done", "pending")
	if detail := issuePackageJourneyFor(pkg).Stages[2].Detail; detail != "Invalidate cached project lists" {
		t.Fatalf("fix detail %q", detail)
	}
	candidate := issuePackageEnvironment{ClusterName: "Candidate", RancherVersion: "v2.15.3-rc1", Source: "manual", RecordedAt: time.Now().UTC()}
	pkg = packageTestMutation(t, s, issuePackageRequest{Action: "start-session", ID: pkg.ID, Revision: pkg.Revision, Name: "Candidate 1", BaselineSessionID: second, FixURL: pkg.FixURL, FixTitle: pkg.FixTitle}, &candidate, nil, nil)
	validation := pkg.Sessions[len(pkg.Sessions)-1].ID
	expect("validation active", "finish-validation", "done", "done", "done", "active")
	pkg = packageTestFinish(t, s, pkg, validation, "not-validated")
	expect("fix not validated", "start-validation", "done", "done", "done", "attention")
	pkg = packageTestMutation(t, s, issuePackageRequest{Action: "start-session", ID: pkg.ID, Revision: pkg.Revision, Name: "Candidate 2", BaselineSessionID: second, FixURL: pkg.FixURL, FixTitle: pkg.FixTitle}, &candidate, nil, nil)
	validation = pkg.Sessions[len(pkg.Sessions)-1].ID
	pkg = packageTestFinish(t, s, pkg, validation, "validated")
	expect("fix validated", "share", "done", "done", "done", "done")
	journey := issuePackageJourneyFor(pkg)
	if !strings.Contains(journey.Stages[3].Detail, "Fix validated · Candidate 2 · against baseline Before the fix") {
		t.Fatalf("validation detail %q", journey.Stages[3].Detail)
	}
	if pkg.Status != "planning" {
		t.Fatal("the journey must not change the organizational package status")
	}
	markdown, err := issuePackageMarkdown(pkg, "", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"**Fix:** https://github.com/rancher/rancher/pull/500", "**Fix title:** Invalidate cached project lists", "## Progress", "| Test plan | Done | 1 saved case |", "| Reproduction | Done | Issue reproduced · Before the fix |", "| Fix | Done | Invalidate cached project lists |", "| Validation | Done | Fix validated · Candidate 2 · against baseline Before the fix |", "**Next:** Share the report.", "do not set the package status", "**Fix under test:** Invalidate cached project lists · https://github.com/rancher/rancher/pull/500 (recorded when this session started)"} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("report omitted %q\n%s", want, markdown)
		}
	}
	if strings.Index(markdown, "## Progress") > strings.Index(markdown, "## Session comparison") {
		t.Fatal("progress summary buried below the session comparison")
	}
	single, err := issuePackageMarkdown(pkg, first, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(single, "**Fix under test:**") {
		t.Fatal("a reproduction session without a fix reference reported one")
	}
}
