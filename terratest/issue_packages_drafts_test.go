package test

import (
	"errors"
	"github.com/brudnak/ha-rancher-rke2/internal/cachelab"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func packageTestDraft(t *testing.T, s *issuePackageService, req issuePackageRequest) issuePackageDraftResponse {
	t.Helper()
	result, err := s.handleWritingDraft(req)
	if err != nil {
		t.Fatal(err)
	}
	return result.(issuePackageDraftResponse)
}

func TestIssuePackageDraftRecoversIncompleteWritingWithoutChangingCheckpoint(t *testing.T) {
	s := packageTestService(t)
	pkg := packageTestPlan(t, s)
	initial := packageTestDraft(t, s, issuePackageRequest{Action: "draft-read", ID: pkg.ID})
	plan := issuePackageEditablePlan(cloneIssuePackage(pkg))
	plan.Cases[0].Title = ""
	plan.Cases[0].Steps[0].Instruction = ""
	plan.Cases[0].AutomationURL = "https://incomplete-"
	plan.Notes = "Unfinished investigation notes"
	draft := issuePackageWritingDraft{BasePlanFingerprint: initial.CurrentPlanFingerprint, Plan: &plan}
	saved := packageTestDraft(t, s, issuePackageRequest{Action: "draft-write", ID: pkg.ID, Draft: &draft})
	if saved.Draft.Revision == "" || saved.Draft.UpdatedAt == nil {
		t.Fatal("draft missing durable revision or time")
	}
	preserved, _ := s.get(pkg.ID)
	if preserved.Revision != pkg.Revision || preserved.Notes != "" || preserved.Cases[0].Title == "" {
		t.Fatal("draft changed the saved plan")
	}
	if err := validateIssuePackageCases(plan.Cases); err == nil {
		t.Fatal("incomplete draft unexpectedly accepted as a saved case")
	}
	restarted, err := newIssuePackageService(s.root)
	if err != nil {
		t.Fatal(err)
	}
	recovered := packageTestDraft(t, restarted, issuePackageRequest{Action: "draft-read", ID: pkg.ID})
	if recovered.Draft.Plan.Notes != plan.Notes || recovered.Draft.Plan.Cases[0].Steps[0].Instruction != "" || recovered.Draft.Revision != saved.Draft.Revision {
		t.Fatal("restart lost unfinished writing")
	}
	info, err := os.Stat(filepath.Join(s.root, pkg.ID, "draft.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("draft file is not private")
	}
}

func TestIssuePackageDraftClearRejectsLateWriteAndCompetingViews(t *testing.T) {
	s := packageTestService(t)
	pkg := packageTestPlan(t, s)
	initial := packageTestDraft(t, s, issuePackageRequest{Action: "draft-read", ID: pkg.ID})
	plan := issuePackageEditablePlan(pkg)
	plan.Notes = "first view"
	draft := issuePackageWritingDraft{BasePlanFingerprint: initial.CurrentPlanFingerprint, Plan: &plan}
	saved := packageTestDraft(t, s, issuePackageRequest{Action: "draft-write", ID: pkg.ID, Draft: &draft})
	if _, err := s.handleWritingDraft(issuePackageRequest{Action: "draft-write", ID: pkg.ID, Draft: &draft}); !errors.Is(err, errIssuePackageConflict) {
		t.Fatalf("competing empty revision was accepted: %v", err)
	}
	cleared := packageTestDraft(t, s, issuePackageRequest{Action: "draft-clear", ID: pkg.ID, DraftRevision: saved.Draft.Revision})
	if cleared.Draft.Revision == saved.Draft.Revision || cleared.Draft.Plan != nil || len(cleared.Draft.Observations) != 0 {
		t.Fatal("clear did not create a distinct empty tombstone")
	}
	for _, revision := range []string{"", saved.Draft.Revision} {
		if _, err := s.handleWritingDraft(issuePackageRequest{Action: "draft-write", ID: pkg.ID, DraftRevision: revision, Draft: &draft}); !errors.Is(err, errIssuePackageConflict) {
			t.Fatalf("late autosave resurrected cleared content: %v", err)
		}
	}
	after := packageTestDraft(t, s, issuePackageRequest{Action: "draft-read", ID: pkg.ID})
	if after.Draft.Plan != nil || after.Draft.Revision != cleared.Draft.Revision {
		t.Fatal("rejected requests altered cleared draft")
	}
}

func TestIssuePackageDraftBindsSavedPlanNotSessionRevision(t *testing.T) {
	s := packageTestService(t)
	pkg := packageTestPlan(t, s)
	initial := packageTestDraft(t, s, issuePackageRequest{Action: "draft-read", ID: pkg.ID})
	plan := issuePackageEditablePlan(pkg)
	plan.Notes = "work in progress"
	draft := issuePackageWritingDraft{BasePlanFingerprint: initial.CurrentPlanFingerprint, Plan: &plan}
	packageTestDraft(t, s, issuePackageRequest{Action: "draft-write", ID: pkg.ID, Draft: &draft})
	pkg = packageTestStart(t, s, pkg)
	read := packageTestDraft(t, s, issuePackageRequest{Action: "draft-read", ID: pkg.ID})
	if read.PlanChanged || read.CurrentPlanFingerprint != initial.CurrentPlanFingerprint {
		t.Fatal("session-only changes made the saved plan stale")
	}
	pkg = packageTestMutation(t, s, issuePackageRequest{Action: "update", ID: pkg.ID, Revision: pkg.Revision, Title: pkg.Title, Status: pkg.Status, Notes: "another view changed the plan", Cases: pkg.Cases}, nil, nil, nil)
	read = packageTestDraft(t, s, issuePackageRequest{Action: "draft-read", ID: pkg.ID})
	if !read.PlanChanged || read.Draft.Plan.Notes != "work in progress" {
		t.Fatal("plan conflict was missed or recovered text overwritten")
	}
}

func TestIssuePackageDraftObservationConflictNeverLosesText(t *testing.T) {
	s := packageTestService(t)
	pkg := packageTestStart(t, s, packageTestPlan(t, s))
	session := pkg.Sessions[0]
	initial := packageTestDraft(t, s, issuePackageRequest{Action: "draft-read", ID: pkg.ID})
	draft := issuePackageWritingDraft{BasePlanFingerprint: initial.CurrentPlanFingerprint, Observations: []issuePackageObservationDraft{{SessionID: session.ID, CaseID: session.Cases[0].ID, Notes: "Unfinished finding"}}}
	saved := packageTestDraft(t, s, issuePackageRequest{Action: "draft-write", ID: pkg.ID, Draft: &draft})
	pkg = packageTestMutation(t, s, issuePackageRequest{Action: "case-result", ID: pkg.ID, Revision: pkg.Revision, SessionID: session.ID, CaseID: session.Cases[0].ID, Outcome: "failed", Notes: "Another window saved this"}, nil, nil, nil)
	read := packageTestDraft(t, s, issuePackageRequest{Action: "draft-read", ID: pkg.ID})
	if len(read.ObservationConflicts) != 1 || !strings.Contains(read.ObservationConflicts[0].Reason, "another view") || read.Draft.Observations[0].Notes != "Unfinished finding" {
		t.Fatal("observation conflict was not preserved")
	}
	pkg = packageTestMutation(t, s, issuePackageRequest{Action: "finish-session", ID: pkg.ID, Revision: pkg.Revision, SessionID: session.ID, Finding: "inconclusive"}, nil, nil, nil)
	read = packageTestDraft(t, s, issuePackageRequest{Action: "draft-read", ID: pkg.ID})
	if len(read.ObservationConflicts) != 1 || !strings.Contains(read.ObservationConflicts[0].Reason, "preserved") || read.Draft.Revision != saved.Draft.Revision {
		t.Fatal("closing the session lost draft text or read mutated its revision")
	}
	draft.Observations[0].Notes = "Another window saved this"
	read = packageTestDraft(t, s, issuePackageRequest{Action: "draft-write", ID: pkg.ID, DraftRevision: saved.Draft.Revision, Draft: &draft})
	if len(read.ObservationConflicts) != 0 || read.Draft.Observations[0].Notes != "Another window saved this" {
		t.Fatal("already checkpointed text should not be flagged as a recovery conflict")
	}
}

func TestIssuePackageDraftRejectsUnsafeFilesAndInvalidIdentity(t *testing.T) {
	s := packageTestService(t)
	pkg := packageTestPlan(t, s)
	initial := packageTestDraft(t, s, issuePackageRequest{Action: "draft-read", ID: pkg.ID})
	plan := issuePackageEditablePlan(cloneIssuePackage(pkg))
	plan.Cases[0].ID = "../escape"
	draft := issuePackageWritingDraft{BasePlanFingerprint: initial.CurrentPlanFingerprint, Plan: &plan}
	if _, err := s.handleWritingDraft(issuePackageRequest{Action: "draft-write", ID: pkg.ID, Draft: &draft}); err == nil {
		t.Fatal("invalid case identity accepted")
	}
	path := filepath.Join(s.root, pkg.ID, "draft.json")
	corrupt := []byte(`{"version":1,"version":1}`)
	if err := os.WriteFile(path, corrupt, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.handleWritingDraft(issuePackageRequest{Action: "draft-clear", ID: pkg.ID}); err == nil {
		t.Fatal("corrupt existing file overwritten")
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != string(corrupt) {
		t.Fatal("corrupt draft was not preserved")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(target, []byte("private outside file"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := s.handleWritingDraft(issuePackageRequest{Action: "draft-read", ID: pkg.ID}); err == nil {
		t.Fatal("draft followed a symbolic link")
	}
}

func TestIssuePackageDraftRecoversTitleListAndEnvironment(t *testing.T) {
	s := packageTestService(t)
	pkg := packageTestPlan(t, s)
	initial := packageTestDraft(t, s, issuePackageRequest{Action: "draft-read", ID: pkg.ID})
	draft := issuePackageWritingDraft{BasePlanFingerprint: initial.CurrentPlanFingerprint, TitleLines: "1. First title\n2. Still thinking…", PlanClusterID: "selected-rancher"}
	saved := packageTestDraft(t, s, issuePackageRequest{Action: "draft-write", ID: pkg.ID, Draft: &draft})
	restarted, err := newIssuePackageService(s.root)
	if err != nil {
		t.Fatal(err)
	}
	recovered := packageTestDraft(t, restarted, issuePackageRequest{Action: "draft-read", ID: pkg.ID})
	if recovered.Draft.TitleLines != draft.TitleLines || recovered.Draft.PlanClusterID != draft.PlanClusterID || recovered.PlanChanged {
		t.Fatal("title-only writing or environment selection did not survive restart")
	}
	checkpoint, _ := restarted.get(pkg.ID)
	if checkpoint.Revision != pkg.Revision || len(checkpoint.Cases) != len(pkg.Cases) {
		t.Fatal("unfinished titles changed the saved plan")
	}
	cleared := packageTestDraft(t, restarted, issuePackageRequest{Action: "draft-clear", ID: pkg.ID, DraftRevision: saved.Draft.Revision})
	if cleared.Draft.TitleLines != "" || cleared.Draft.PlanClusterID != "" {
		t.Fatal("clear retained title writing")
	}
	draft.TitleLines = strings.Repeat("x", 128001)
	if _, err := restarted.handleWritingDraft(issuePackageRequest{Action: "draft-write", ID: pkg.ID, DraftRevision: cleared.Draft.Revision, Draft: &draft}); err == nil {
		t.Fatal("accepted unbounded title list")
	}
}

func TestIssuePackageDraftRecoversSessionCaseOriginsAfterSessionChanges(t *testing.T) {
	s := packageTestService(t)
	pkg := packageTestStart(t, s, packageTestPlan(t, s))
	initial := packageTestDraft(t, s, issuePackageRequest{Action: "draft-read", ID: pkg.ID})
	if initial.Draft.SessionCaseEdits == nil {
		t.Fatal("a new draft did not provide an empty session-edit list")
	}
	plan := issuePackageEditablePlan(cloneIssuePackage(pkg))
	plan.Cases[0].Expected = "Unfinished session expectation"
	origin := issuePackageSessionCaseDraft{SessionID: pkg.Sessions[0].ID, CaseID: pkg.Cases[0].ID}
	draft := issuePackageWritingDraft{BasePlanFingerprint: initial.CurrentPlanFingerprint, Plan: &plan, SessionCaseEdits: []issuePackageSessionCaseDraft{origin}}
	saved := packageTestDraft(t, s, issuePackageRequest{Action: "draft-write", ID: pkg.ID, Draft: &draft})
	pkg = packageTestMutation(t, s, issuePackageRequest{Action: "finish-session", ID: pkg.ID, Revision: pkg.Revision, SessionID: origin.SessionID, Finding: "inconclusive"}, nil, nil, nil)
	pkg = packageTestMutation(t, s, issuePackageRequest{Action: "delete-session", ID: pkg.ID, Revision: pkg.Revision, SessionID: origin.SessionID, Confirm: "confirm"}, nil, nil, nil)
	restarted, err := newIssuePackageService(s.root)
	if err != nil {
		t.Fatal(err)
	}
	recovered := packageTestDraft(t, restarted, issuePackageRequest{Action: "draft-read", ID: pkg.ID})
	if !reflect.DeepEqual(recovered.Draft.SessionCaseEdits, []issuePackageSessionCaseDraft{origin}) || recovered.Draft.Plan.Cases[0].Expected != plan.Cases[0].Expected || recovered.Draft.Revision != saved.Draft.Revision {
		t.Fatal("session changes or restart lost the origin of unfinished writing")
	}
	checkpoint, _ := restarted.get(pkg.ID)
	if checkpoint.Cases[0].Expected == plan.Cases[0].Expected {
		t.Fatal("recovering session writing silently changed the saved plan")
	}
	cleared := packageTestDraft(t, restarted, issuePackageRequest{Action: "draft-clear", ID: pkg.ID, DraftRevision: saved.Draft.Revision})
	if cleared.Draft.SessionCaseEdits == nil || len(cleared.Draft.SessionCaseEdits) != 0 {
		t.Fatal("clearing writing retained a session-case origin")
	}
}

func TestIssuePackageDraftValidatesSessionCaseOriginsWithoutRequiringLiveSessions(t *testing.T) {
	s := packageTestService(t)
	pkg := packageTestPlan(t, s)
	initial := packageTestDraft(t, s, issuePackageRequest{Action: "draft-read", ID: pkg.ID})
	origin := issuePackageSessionCaseDraft{SessionID: cachelab.ID(), CaseID: cachelab.ID()}
	draft := issuePackageWritingDraft{BasePlanFingerprint: initial.CurrentPlanFingerprint, TitleLines: "1. Keep this writing", SessionCaseEdits: []issuePackageSessionCaseDraft{origin}}
	saved := packageTestDraft(t, s, issuePackageRequest{Action: "draft-write", ID: pkg.ID, Draft: &draft})
	for _, origins := range [][]issuePackageSessionCaseDraft{
		{{SessionID: "../invalid", CaseID: origin.CaseID}},
		{{SessionID: origin.SessionID, CaseID: "../invalid"}},
		{origin, origin},
		{origin, {SessionID: cachelab.ID(), CaseID: origin.CaseID}},
	} {
		draft.SessionCaseEdits = origins
		if _, err := s.handleWritingDraft(issuePackageRequest{Action: "draft-write", ID: pkg.ID, DraftRevision: saved.Draft.Revision, Draft: &draft}); err == nil {
			t.Fatal("invalid or ambiguous session-case origins were accepted")
		}
	}
	draft.SessionCaseEdits = nil
	for range 250 {
		draft.SessionCaseEdits = append(draft.SessionCaseEdits, issuePackageSessionCaseDraft{SessionID: cachelab.ID(), CaseID: cachelab.ID()})
	}
	if err := validateIssuePackageWritingDraft(draft); err != nil {
		t.Fatalf("the maximum valid origin list was rejected: %v", err)
	}
	draft.SessionCaseEdits = append(draft.SessionCaseEdits, issuePackageSessionCaseDraft{SessionID: cachelab.ID(), CaseID: cachelab.ID()})
	if err := validateIssuePackageWritingDraft(draft); err == nil {
		t.Fatal("unbounded session-case origins were accepted")
	}
	recovered := packageTestDraft(t, s, issuePackageRequest{Action: "draft-read", ID: pkg.ID})
	if recovered.Draft.Revision != saved.Draft.Revision || recovered.Draft.TitleLines != saved.Draft.TitleLines || !reflect.DeepEqual(recovered.Draft.SessionCaseEdits, saved.Draft.SessionCaseEdits) {
		t.Fatal("invalid origin metadata overwrote durable writing")
	}
}
