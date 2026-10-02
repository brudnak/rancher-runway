package test

import (
	"errors"
	"os"
	"path/filepath"
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
