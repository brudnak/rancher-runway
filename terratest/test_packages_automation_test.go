package test

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

func packageAutomationFixture(t *testing.T) (*testPackageService, testPackage, testPackageRunLink) {
	s := packageTestService(t)
	pkg := packageTestPlan(t, s)
	pkg.Cases[0].Selection = []string{"validation/projects/TestSuite/TestMember"}
	pkg.Cases[0].AutomationSource = &testPackageAutomationSource{Name: "Project regression", SHA: strings.Repeat("a", 40), Ref: "main", Tags: "validation", Timeout: 30}
	pkg.Cases[0].Automation = "automated"
	pkg = packageTestMutation(t, s, testPackageRequest{Action: "update", ID: pkg.ID, Revision: pkg.Revision, Title: pkg.Title, Status: pkg.Status, Cases: pkg.Cases}, nil, nil, nil)
	pkg = packageTestStart(t, s, pkg)
	link := testPackageRunLink{PackageID: pkg.ID, SessionID: pkg.Sessions[0].ID, CaseID: pkg.Cases[0].ID}
	return s, pkg, link
}
func TestTestPackageAutomationFrozenSourceAndPrepare(t *testing.T) {
	s, pkg, link := packageAutomationFixture(t)
	source := pkg.Sessions[0].Cases[0].AutomationSource.SHA
	pkg.Cases[0].AutomationSource.SHA = strings.Repeat("b", 40)
	pkg = packageTestMutation(t, s, testPackageRequest{Action: "update", ID: pkg.ID, Revision: pkg.Revision, Title: pkg.Title, Status: pkg.Status, Cases: pkg.Cases}, nil, nil, nil)
	result, err := s.preparePackageAutomation(testPackageRequest{ID: pkg.ID, SessionID: link.SessionID, CaseID: link.CaseID})
	if err != nil {
		t.Fatal(err)
	}
	if result.(map[string]any)["source"].(*testPackageAutomationSource).SHA != source {
		t.Fatal("frozen source changed")
	}
	bundle, err := s.buildBundle(pkg.ID, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	copy := packageTestService(t)
	imported, err := copy.importPackage(bundle.Package, nil)
	if err != nil {
		t.Fatal(err)
	}
	if imported.Sessions[0].Cases[0].AutomationSource.SHA != source {
		t.Fatal("lost frozen automation in round trip")
	}
}
func TestTestPackageAutomationPreservesIdempotentEvidenceWithoutInferringOutcome(t *testing.T) {
	s, pkg, link := packageAutomationFixture(t)
	run := testLabRun{ID: cacheLabID(), PackageLink: &link, ClusterID: "cluster-a", SHA: strings.Repeat("a", 40), Status: "failed", FinishedAt: time.Now()}
	meta, _ := json.Marshal(testPackageTestRunMetadata{ID: run.ID, SHA: run.SHA, Status: run.Status})
	raw := []byte("failure evidence")
	proof := testPackageEvidence{ID: cacheLabID(), Kind: "test-run", Name: "Project regression", SourceID: run.ID, ClusterID: run.ClusterID, CaseID: link.CaseID, CapturedAt: run.FinishedAt, AttachedAt: time.Now(), Metadata: meta, Artifact: packageArtifact(cacheLabID()+".log", "text/plain", raw)}
	for i := 0; i < 2; i++ {
		if err := s.preserveAutomationEvidence(run, proof, raw); err != nil {
			t.Fatal(err)
		}
	}
	pkg, _ = s.get(pkg.ID)
	session := pkg.Sessions[0]
	if len(session.Evidence) != 1 || session.Results[0].Outcome != "not-run" || session.Finding != "inconclusive" {
		t.Fatal("duplicate evidence or inferred findings")
	}
	s.pendingAutomation[run.ID] = link
	if _, err := s.mutate(testPackageRequest{Action: "finish-session", ID: pkg.ID, Revision: pkg.Revision, SessionID: link.SessionID, Finding: "inconclusive"}, nil, nil, nil); err == nil {
		t.Fatal("finished while linked run pending")
	}
	delete(s.pendingAutomation, run.ID)
	run.ID = cacheLabID()
	run.ClusterID = "another-cluster"
	if err := s.preserveAutomationEvidence(run, proof, raw); err == nil {
		t.Fatal("different cluster accepted")
	}
}
func TestTestPackageAutomationRejectsChangedRunBeforeExecution(t *testing.T) {
	s, pkg, link := packageAutomationFixture(t)
	pkg.Sessions[0].Environment.URL = "https://rancher.example.test"
	if err := s.saveLocked(pkg); err != nil {
		t.Fatal(err)
	}
	panel := &localControlPanel{testPackages: s}
	lab := &testLabService{}
	req := testLabRequest{PackageLink: &link, ClusterID: "cluster-a", Config: "rancher:\n  host: another.example.test\n  adminToken: token-abc:password\n", SHA: pkg.Cases[0].AutomationSource.SHA, Selection: pkg.Cases[0].Selection, Tags: "validation", Timeout: 30}
	if _, err := panel.startPackageLinkedRun(lab, req); err == nil || !strings.Contains(err.Error(), "target") {
		t.Fatal("different target was not rejected", err)
	}
	req.Config = strings.ReplaceAll(req.Config, "another.example.test", "rancher.example.test")
	req.SHA = strings.Repeat("b", 40)
	if _, err := panel.startPackageLinkedRun(lab, req); err == nil || !strings.Contains(err.Error(), "frozen") {
		t.Fatal("different source not rejected", err)
	}
}

func TestTestPackageAutomationManualTargetAllowsResolvedCluster(t *testing.T) {
	s, pkg, link := packageAutomationFixture(t)
	pkg.Sessions[0].Environment.ClusterID = ""
	pkg.Sessions[0].Environment.URL = "https://rancher.example.test"
	if err := s.saveLocked(pkg); err != nil {
		t.Fatal(err)
	}
	run := testLabRun{ID: cacheLabID(), PackageLink: &link, ClusterID: "external-resolved", Host: "rancher.example.test", Status: "passed", FinishedAt: time.Now()}
	e := testPackageEvidence{ID: cacheLabID(), Kind: "test-run", Name: "Passed fixture", SourceID: run.ID, ClusterID: run.ClusterID, CaseID: link.CaseID, CapturedAt: run.FinishedAt, AttachedAt: time.Now(), Metadata: json.RawMessage(`{}`)}
	if err := s.preserveAutomationEvidence(run, e, nil); err != nil {
		t.Fatal("manual target lost evidence after cluster resolution", err)
	}
}
func TestTestPackageAutomationCallbackCopiesLogAndRecoversAfterRestart(t *testing.T) {
	s, pkg, link := packageAutomationFixture(t)
	lab := testLabFixture(t)
	run := testLabRun{ID: cacheLabID(), Name: "Regression", PackageLink: &link, ClusterID: "cluster-a", SHA: strings.Repeat("a", 40), Ref: "main", Status: "passed", StartedAt: time.Now().Add(-time.Minute), FinishedAt: time.Now(), Selection: pkg.Cases[0].Selection, Results: []testLabResult{{Name: "TestMember", Status: "pass"}}}
	lab.library.Runs = []testLabRun{run}
	lab.logs[run.ID] = "fixture log"
	if err := lab.persistLocked(); err != nil {
		t.Fatal(err)
	}
	panel := &localControlPanel{testPackages: s, testLab: lab}
	panel.preservePackageRun(run)
	if !lab.library.Runs[0].PackageEvidenceSaved || lab.library.Runs[0].PackageEvidenceError != "" {
		t.Fatal("callback did not preserve evidence", lab.library.Runs[0].PackageEvidenceError)
	}
	saved, _ := s.get(pkg.ID)
	if len(saved.Sessions[0].Evidence) != 1 || saved.Sessions[0].Evidence[0].Artifact == nil {
		t.Fatal("missing copied result/log")
	}
	// Simulate a crash after the package write but before marking Test Lab as saved.
	lab.library.Runs[0].PackageEvidenceSaved = false
	panel.syncPackageRuns()
	saved, _ = s.get(pkg.ID)
	if len(saved.Sessions[0].Evidence) != 1 || !lab.library.Runs[0].PackageEvidenceSaved {
		t.Fatal("retry duplicated evidence or failed to restore saved state")
	}
}

func TestTestPackageAutomationConcurrentRetriesDoNotDuplicateEvidence(t *testing.T) {
	s, pkg, link := packageAutomationFixture(t)
	run := testLabRun{ID: cacheLabID(), PackageLink: &link, ClusterID: "cluster-a", Status: "passed", FinishedAt: time.Now()}
	proof := testPackageEvidence{ID: cacheLabID(), Kind: "test-run", Name: "Concurrent result", SourceID: run.ID, CaseID: link.CaseID, ClusterID: run.ClusterID, CapturedAt: run.FinishedAt, AttachedAt: time.Now(), Metadata: json.RawMessage(`{}`)}
	var group sync.WaitGroup
	failures := make(chan error, 8)
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() { defer group.Done(); failures <- s.preserveAutomationEvidence(run, proof, nil) }()
	}
	group.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	saved, _ := s.get(pkg.ID)
	if len(saved.Sessions[0].Evidence) != 1 {
		t.Fatal("concurrent retries duplicated evidence")
	}
}
