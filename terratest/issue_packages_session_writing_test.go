package test

import (
	"encoding/json"
	"github.com/brudnak/ha-rancher-rke2/internal/cachelab"
	"reflect"
	"strings"
	"testing"
	"time"
)

func packageTestWriteSessionCase(t *testing.T, s *issuePackageService, pkg issuePackage, sessionIndex int, c issuePackageCase) issuePackage {
	t.Helper()
	return packageTestMutation(t, s, issuePackageRequest{Action: "update-session-case", ID: pkg.ID, Revision: pkg.Revision, SessionID: pkg.Sessions[sessionIndex].ID, CaseID: c.ID, Cases: []issuePackageCase{c}}, nil, nil, nil)
}

func TestIssuePackageSessionWritingExtendsTitleOnlyPlan(t *testing.T) {
	s := packageTestService(t)
	pkg := packageTestPlan(t, s)
	pkg.Cases[0].Steps = nil
	pkg = packageTestMutation(t, s, issuePackageRequest{Action: "update", ID: pkg.ID, Revision: pkg.Revision, Title: pkg.Title, Status: pkg.Status, Cases: pkg.Cases}, nil, nil, nil)
	pkg = packageTestStart(t, s, pkg)
	c := pkg.Sessions[0].Cases[0]
	c.Preconditions = "Use a project member account"
	c.Expected = "Only permitted resources appear"
	c.Steps = []issuePackageStep{{Instruction: "Open the project", Expected: "Resources are visible"}}
	pkg = packageTestWriteSessionCase(t, s, pkg, 0, c)
	updated := pkg.Sessions[0].Cases[0]
	marker := pkg.Sessions[0].Results[0].Steps[0]
	if updated.ID != c.ID || updated.Steps[0].ID == "" || updated.Preconditions != c.Preconditions || updated.Expected != c.Expected || marker.StepID != updated.Steps[0].ID || marker.Done || !marker.At.IsZero() {
		t.Fatal("new details or unperformed step marker were not saved")
	}
	if !reflect.DeepEqual(pkg.Cases[0], updated) {
		t.Fatal("the working plan did not receive details written during testing")
	}
	reopened, err := newIssuePackageService(s.root)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := reopened.get(pkg.ID)
	if err != nil || !reflect.DeepEqual(saved.Sessions[0].Cases[0], updated) {
		t.Fatalf("session writing did not survive reopening: %v", err)
	}
}

func TestIssuePackageSessionWritingReconcilesStepsWithoutLosingObservations(t *testing.T) {
	s := packageTestService(t)
	pkg := packageTestStart(t, s, packageTestPlan(t, s))
	session := pkg.Sessions[0]
	c := session.Cases[0]
	pkg = packageTestMutation(t, s, issuePackageRequest{Action: "mark-step", ID: pkg.ID, Revision: pkg.Revision, SessionID: session.ID, CaseID: c.ID, StepID: c.Steps[0].ID, Done: true}, nil, nil, nil)
	pkg = packageTestMutation(t, s, issuePackageRequest{Action: "case-result", ID: pkg.ID, Revision: pkg.Revision, SessionID: session.ID, CaseID: c.ID, Outcome: "failed", Notes: "The resource is missing"}, nil, nil, nil)
	performed := pkg.Sessions[0].Results[0].Steps[0]
	pkg = packageTestWriteSessionCase(t, s, pkg, 0, pkg.Sessions[0].Cases[0])
	if result := pkg.Sessions[0].Results[0]; result.Outcome != "failed" || result.Steps[0] != performed {
		t.Fatal("saving unchanged details reset the existing outcome or step marker")
	}
	c = cloneIssuePackage(pkg).Sessions[0].Cases[0]
	c.Steps = append([]issuePackageStep{{ID: cachelab.ID(), Instruction: "Check the account role"}}, c.Steps...)
	pkg = packageTestWriteSessionCase(t, s, pkg, 0, c)
	result := pkg.Sessions[0].Results[0]
	if result.Outcome != "not-run" || result.Notes != "The resource is missing" || result.Steps[0].Done || !result.Steps[0].At.IsZero() || result.Steps[1] != performed {
		t.Fatal("added steps did not reset the outcome, or rewrote notes or an unchanged performed step")
	}
	pkg = packageTestMutation(t, s, issuePackageRequest{Action: "case-result", ID: pkg.ID, Revision: pkg.Revision, SessionID: session.ID, CaseID: c.ID, Outcome: "passed", Notes: result.Notes}, nil, nil, nil)
	c = cloneIssuePackage(pkg).Sessions[0].Cases[0]
	c.Steps[1].Instruction = "Open the project with a different account"
	pkg = packageTestWriteSessionCase(t, s, pkg, 0, c)
	result = pkg.Sessions[0].Results[0]
	if result.Steps[1].StepID != performed.StepID || result.Steps[1].Done || !result.Steps[1].At.IsZero() || result.Outcome != "not-run" || result.Notes != "The resource is missing" {
		t.Fatal("a changed instruction kept its old performed timestamp or lost the observation")
	}
	c = cloneIssuePackage(pkg).Sessions[0].Cases[0]
	c.Steps = c.Steps[1:]
	pkg = packageTestWriteSessionCase(t, s, pkg, 0, c)
	if len(pkg.Sessions[0].Results[0].Steps) != 1 || pkg.Sessions[0].Results[0].Steps[0].StepID != performed.StepID {
		t.Fatal("removing an unreferenced step left an orphaned result marker")
	}
}

func TestIssuePackageSessionWritingPreservesBaselineCompletedSessionsAndAutomation(t *testing.T) {
	s := packageTestService(t)
	pkg := packageTestCandidate(t, s, packageTestCompletedBaseline(t, s))
	before := cloneIssuePackage(pkg)
	c := cloneIssuePackage(pkg).Sessions[1].Cases[0]
	c.Title = "Project member sees the permitted resource"
	c.Steps = append(c.Steps, issuePackageStep{Instruction: "Check the namespace filter"})
	c.Automation = "automated"
	c.AutomationURL = "https://example.com/changed-reference"
	c.Selection = []string{"different-test"}
	pkg = packageTestWriteSessionCase(t, s, pkg, 1, c)
	if !reflect.DeepEqual(pkg.Sessions[0], before.Sessions[0]) || !reflect.DeepEqual(pkg.Sessions[1].Baseline, before.Sessions[1].Baseline) {
		t.Fatal("writing candidate details changed a completed session or comparison baseline")
	}
	updated := pkg.Sessions[1].Cases[0]
	original := before.Sessions[1].Cases[0]
	if updated.Automation != original.Automation || updated.AutomationURL != original.AutomationURL || !reflect.DeepEqual(updated.Selection, original.Selection) || !reflect.DeepEqual(updated.AutomationSource, original.AutomationSource) {
		t.Fatal("manual details changed the session's automation references")
	}
	if rows := issuePackageCompareCases(pkg.Sessions[1]); len(rows) != 1 || rows[0].Content != "Changed case — review differences" {
		t.Fatal("edited candidate content was presented as directly comparable")
	}
	_, err := s.mutate(issuePackageRequest{Action: "update-session-case", ID: pkg.ID, Revision: pkg.Revision, SessionID: pkg.Sessions[0].ID, CaseID: original.ID, Cases: []issuePackageCase{original}}, nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "preserved") {
		t.Fatalf("completed session writing was accepted: %v", err)
	}
}

func TestIssuePackageSessionWritingRejectsEvidenceLossAndInvalidCaseIdentity(t *testing.T) {
	s := packageTestService(t)
	pkg := packageTestStart(t, s, packageTestPlan(t, s))
	c := cloneIssuePackage(pkg).Sessions[0].Cases[0]
	now := time.Now().UTC()
	evidence := issuePackageEvidence{ID: cachelab.ID(), Kind: "note", Name: "Observation", CaseID: c.ID, StepID: c.Steps[0].ID, CapturedAt: now, AttachedAt: now, Metadata: json.RawMessage(`{}`)}
	pkg = packageTestMutation(t, s, issuePackageRequest{Action: "attach-evidence", ID: pkg.ID, Revision: pkg.Revision, SessionID: pkg.Sessions[0].ID}, nil, &evidence, nil)
	request := issuePackageRequest{Action: "update-session-case", ID: pkg.ID, Revision: pkg.Revision, SessionID: pkg.Sessions[0].ID, CaseID: c.ID, Cases: []issuePackageCase{c}}
	request.Cases[0].Steps = nil
	if _, err := s.mutate(request, nil, nil, nil); err == nil || !strings.Contains(err.Error(), "attached evidence") {
		t.Fatalf("an evidenced step was removed: %v", err)
	}
	request.Cases = []issuePackageCase{c, c}
	if _, err := s.mutate(request, nil, nil, nil); err == nil {
		t.Fatal("multiple cases were accepted")
	}
	request.Cases = []issuePackageCase{c}
	request.Cases[0].ID = cachelab.ID()
	if _, err := s.mutate(request, nil, nil, nil); err == nil {
		t.Fatal("replacement case identity was accepted")
	}
	request.Cases[0] = c
	request.Cases[0].Title = ""
	if _, err := s.mutate(request, nil, nil, nil); err == nil {
		t.Fatal("invalid case content was accepted")
	}
	saved, _ := s.get(pkg.ID)
	if !reflect.DeepEqual(saved, pkg) {
		t.Fatal("rejected session writing modified the saved package")
	}
}

func TestIssuePackageSessionWritingRejectsIndependentPlanChanges(t *testing.T) {
	for _, removed := range []bool{false, true} {
		t.Run(map[bool]string{false: "changed", true: "removed"}[removed], func(t *testing.T) {
			s := packageTestService(t)
			pkg := packageTestStart(t, s, packageTestPlan(t, s))
			c := cloneIssuePackage(pkg).Sessions[0].Cases[0]
			if removed {
				pkg.Cases = nil
			} else {
				pkg.Cases[0].Expected = "A separately edited expectation"
			}
			pkg = packageTestMutation(t, s, issuePackageRequest{Action: "update", ID: pkg.ID, Revision: pkg.Revision, Title: pkg.Title, Status: pkg.Status, Cases: pkg.Cases}, nil, nil, nil)
			c.Steps = append(c.Steps, issuePackageStep{Instruction: "Check another resource"})
			_, err := s.mutate(issuePackageRequest{Action: "update-session-case", ID: pkg.ID, Revision: pkg.Revision, SessionID: pkg.Sessions[0].ID, CaseID: c.ID, Cases: []issuePackageCase{c}}, nil, nil, nil)
			if err == nil || !strings.Contains(err.Error(), "test plan") {
				t.Fatalf("independent plan changes were overwritten: %v", err)
			}
			saved, _ := s.get(pkg.ID)
			if !reflect.DeepEqual(saved, pkg) {
				t.Fatal("plan conflict changed the saved package")
			}
		})
	}
}
