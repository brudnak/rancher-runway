package test

import (
	"encoding/json"
	"github.com/brudnak/ha-rancher-rke2/internal/cachelab"
	"os"
	"strings"
	"testing"
	"time"
)

func packageTestCompletedBaseline(t *testing.T, s *testPackageService) testPackage {
	t.Helper()
	pkg := packageTestStart(t, s, packageTestPlan(t, s))
	session := pkg.Sessions[0]
	c := session.Cases[0]
	pkg = packageTestMutation(t, s, testPackageRequest{Action: "mark-step", ID: pkg.ID, Revision: pkg.Revision, SessionID: session.ID, CaseID: c.ID, StepID: c.Steps[0].ID, Done: true}, nil, nil, nil)
	pkg = packageTestMutation(t, s, testPackageRequest{Action: "case-result", ID: pkg.ID, Revision: pkg.Revision, SessionID: session.ID, CaseID: c.ID, Outcome: "failed", Notes: "The resource was missing"}, nil, nil, nil)
	raw := []byte("A preserved baseline log\n")
	proof := testPackageEvidence{ID: cachelab.ID(), Kind: "test-run", Name: "Baseline test output", CaseID: c.ID, StepID: c.Steps[0].ID, CapturedAt: time.Now().UTC(), AttachedAt: time.Now().UTC(), Metadata: json.RawMessage(`{}`), Artifact: packageArtifact(cachelab.ID()+".log", "text/plain", raw)}
	pkg = packageTestMutation(t, s, testPackageRequest{Action: "attach-evidence", ID: pkg.ID, Revision: pkg.Revision, SessionID: session.ID}, nil, &proof, raw)
	return packageTestMutation(t, s, testPackageRequest{Action: "finish-session", ID: pkg.ID, Revision: pkg.Revision, SessionID: session.ID, Finding: "reproduced", Conclusion: "Resource visibility reproduced"}, nil, nil, nil)
}

func packageTestCandidate(t *testing.T, s *testPackageService, pkg testPackage) testPackage {
	t.Helper()
	environment := testPackageEnvironment{ClusterID: "candidate-cluster", ClusterName: "Fix candidate", URL: "https://private-candidate.example", RancherVersion: "v2.15.3-rc1", KubernetesVersion: "v1.35.0+k3s1", Source: "manual", RecordedAt: time.Now().UTC()}
	return packageTestMutation(t, s, testPackageRequest{Action: "start-session", ID: pkg.ID, Revision: pkg.Revision, Name: "Candidate", Purpose: "exploration", BaselineSessionID: pkg.Sessions[0].ID}, &environment, nil, nil)
}

func TestTestPackageBaselineFreezesCasesAndResetsOutcomes(t *testing.T) {
	s := packageTestService(t)
	pkg := packageTestCompletedBaseline(t, s)
	original := cloneTestPackage(pkg)
	// The plan evolves before the fix is available. Candidate still uses exactly
	// what was executed in the preserved baseline, never the newer working plan.
	pkg.Cases[0].Title = "A revised plan"
	pkg.Cases[0].Steps[0].Instruction = "New instruction"
	pkg = packageTestMutation(t, s, testPackageRequest{Action: "update", ID: pkg.ID, Revision: pkg.Revision, Title: pkg.Title, Status: "validating", Cases: pkg.Cases}, nil, nil, nil)
	pkg = packageTestCandidate(t, s, pkg)
	candidate := pkg.Sessions[1]
	if candidate.Purpose != "validation" || candidate.Baseline == nil || candidate.Baseline.SessionID != original.Sessions[0].ID {
		t.Fatal("candidate did not preserve baseline identity or validation purpose")
	}
	if testPackageCaseContent(candidate.Cases[0]) != testPackageCaseContent(original.Sessions[0].Cases[0]) || candidate.Cases[0].ID != original.Sessions[0].Cases[0].ID {
		t.Fatal("candidate used the current plan instead of the exact baseline")
	}
	result := candidate.Results[0]
	if result.Outcome != "not-run" || result.Notes != "" || result.Steps[0].Done || !result.Steps[0].At.IsZero() || len(candidate.Evidence) != 0 {
		t.Fatal("candidate inherited baseline outcomes, markers, notes, or evidence")
	}
	if candidate.Baseline.Results[0].Outcome != "failed" || candidate.Baseline.Results[0].Notes != "The resource was missing" || !candidate.Baseline.Results[0].Steps[0].Done || len(candidate.Baseline.Evidence) != 1 || !candidate.Baseline.Evidence[0].HasArtifact {
		t.Fatal("baseline observations were not preserved for comparison")
	}
	// Mutating the returned model must not rewrite the source or saved snapshot.
	candidate.Baseline.Cases[0].Steps[0].Instruction = "Caller changed the copy"
	saved, err := s.get(pkg.ID)
	if err != nil || saved.Sessions[1].Baseline.Cases[0].Steps[0].Instruction != original.Sessions[0].Cases[0].Steps[0].Instruction {
		t.Fatalf("baseline was aliased to a caller: %v", err)
	}
}

func TestTestPackageBaselineRejectsUnpreservedAndPartialSources(t *testing.T) {
	s := packageTestService(t)
	pkg := packageTestStart(t, s, packageTestPlan(t, s))
	env := pkg.Sessions[0].Environment
	request := testPackageRequest{Action: "start-session", ID: pkg.ID, Revision: pkg.Revision, Name: "Invalid candidate", Purpose: "validation", BaselineSessionID: pkg.Sessions[0].ID}
	if _, err := s.mutate(request, &env, nil, nil); err == nil || !strings.Contains(err.Error(), "finish the baseline") {
		t.Fatalf("active source was accepted: %v", err)
	}
	request.BaselineSessionID = cachelab.ID()
	if _, err := s.mutate(request, &env, nil, nil); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing source was accepted: %v", err)
	}
	request.BaselineSessionID = pkg.Sessions[0].ID
	request.CaseIDs = []string{pkg.Cases[0].ID}
	if _, err := s.mutate(request, &env, nil, nil); err == nil || !strings.Contains(err.Error(), "every frozen baseline case") {
		t.Fatalf("partial source was accepted: %v", err)
	}
	saved, _ := s.get(pkg.ID)
	if saved.Revision != pkg.Revision || len(saved.Sessions) != 1 {
		t.Fatal("failed validation changed saved history")
	}
}

func TestTestPackageBaselineSurvivesDeletionRestartAndPortableRoundTrip(t *testing.T) {
	s := packageTestService(t)
	pkg := packageTestCandidate(t, s, packageTestCompletedBaseline(t, s))
	baseline := pkg.Sessions[0]
	artifactPath, err := s.artifactPath(pkg.ID, baseline.Evidence[0].Artifact.Name)
	if err != nil {
		t.Fatal(err)
	}
	pkg = packageTestMutation(t, s, testPackageRequest{Action: "delete-session", ID: pkg.ID, Revision: pkg.Revision, SessionID: baseline.ID, Confirm: "confirm"}, nil, nil, nil)
	if _, err = os.Stat(artifactPath); !os.IsNotExist(err) {
		t.Fatal("deleting source session did not clean its exclusively owned artifact")
	}
	restored, err := newTestPackageService(s.root)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err = restored.get(pkg.ID)
	if err != nil || len(pkg.Sessions) != 1 || pkg.Sessions[0].Baseline.SessionID != baseline.ID || pkg.Sessions[0].Baseline.Results[0].Notes != "The resource was missing" {
		t.Fatalf("restart lost independent baseline: %v", err)
	}
	bundle, err := restored.buildBundle(pkg.ID, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	artifacts, _, err := validateTestPackageBundle(&bundle)
	if err != nil || len(artifacts) != 0 {
		t.Fatalf("baseline summary created an artifact dependency: %v", err)
	}
	raw, _ := json.Marshal(bundle)
	if strings.Contains(string(raw), baseline.Evidence[0].Artifact.Name) {
		t.Fatal("baseline summary leaked an owned artifact reference")
	}
	var decoded testPackageBundle
	if err = decodeTestPackage(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	imported, err := restored.importPackage(decoded.Package, artifacts)
	if err != nil {
		t.Fatal(err)
	}
	if imported.Sessions[0].Baseline.Environment.Source != "imported" || imported.Sessions[0].Baseline.Results[0].Outcome != "failed" || imported.Sessions[0].Finding != "inconclusive" {
		t.Fatal("import lost baseline provenance or resumed candidate work")
	}
	report, err := testPackageMarkdown(imported, imported.Sessions[0].ID, false)
	if err != nil || !strings.Contains(report, "Baseline → candidate") || !strings.Contains(report, "failed | not-run") || !strings.Contains(report, "1 / 0") || strings.Contains(report, "private-candidate.example") {
		t.Fatalf("portable candidate report lost matrix or disclosed target URL: %v\n%s", err, report)
	}
}

func TestTestPackageBaselineComparisonUsesIdentityAndContent(t *testing.T) {
	s := packageTestService(t)
	pkg := packageTestCandidate(t, s, packageTestCompletedBaseline(t, s))
	candidate := pkg.Sessions[1]
	rows := testPackageCompareCases(candidate)
	if len(rows) != 1 || rows[0].Content != "Same case" || rows[0].BaselineOutcome != "failed" || rows[0].CandidateOutcome != "not-run" {
		t.Fatal("exact baseline cases did not compare")
	}
	// Same identity but edited instruction is materially different.
	candidate.Cases[0].Steps[0].Expected = "A different expected behavior"
	rows = testPackageCompareCases(candidate)
	if !strings.HasPrefix(rows[0].Content, "Changed case") {
		t.Fatal("retained identity hid changed case content")
	}
	// Equal title and position cannot substitute for a missing stable identity.
	oldID := candidate.Cases[0].ID
	candidate.Cases[0].ID = cachelab.ID()
	candidate.Results[0].CaseID = candidate.Cases[0].ID
	rows = testPackageCompareCases(candidate)
	if len(rows) != 2 || rows[0].CaseID != oldID || rows[0].Content != "Missing from candidate" || rows[1].Content != "Added in candidate" {
		t.Fatal("cases were matched by title or position instead of stable identity")
	}
}

func TestTestPackageBaselineStrictValidationAndLegacyCompatibility(t *testing.T) {
	s := packageTestService(t)
	pkg := packageTestCandidate(t, s, packageTestCompletedBaseline(t, s))
	for name, mutate := range map[string]func(*testPackage){
		"foreign result":   func(p *testPackage) { p.Sessions[1].Baseline.Results[0].CaseID = cachelab.ID() },
		"foreign evidence": func(p *testPackage) { p.Sessions[1].Baseline.Evidence[0].StepID = cachelab.ID() },
		"capture before completion": func(p *testPackage) {
			p.Sessions[1].Baseline.CapturedAt = p.Sessions[1].Baseline.StartedAt.Add(-time.Hour)
		},
		"future capture":          func(p *testPackage) { p.Sessions[1].Baseline.CapturedAt = p.Sessions[1].StartedAt.Add(time.Hour) },
		"recursive identity":      func(p *testPackage) { p.Sessions[1].Baseline.SessionID = p.Sessions[1].ID },
		"wrong candidate purpose": func(p *testPackage) { p.Sessions[1].Purpose = "exploration" },
	} {
		t.Run(name, func(t *testing.T) {
			invalid := cloneTestPackage(pkg)
			mutate(&invalid)
			if validateTestPackage(invalid) == nil {
				t.Fatal("invalid baseline accepted")
			}
		})
	}
	raw, _ := json.Marshal(pkg)
	raw = []byte(strings.Replace(string(raw), `"hasArtifact":true`, `"hasArtifact":true,"artifact":{"name":"secret.log"}`, 1))
	var decoded testPackage
	if decodeTestPackage(raw, &decoded) == nil {
		t.Fatal("hidden artifact reference accepted in baseline summary")
	}
	legacy := cloneTestPackage(pkg)
	legacy.Sessions[1].Baseline = nil
	raw, _ = json.Marshal(legacy)
	if err := decodeTestPackage(raw, &decoded); err != nil || validateTestPackage(decoded) != nil {
		t.Fatalf("legacy package without baseline rejected: %v", err)
	}
}

func TestTestPackageEnvironmentComparisonIgnoresObservationClockAndArrayOrder(t *testing.T) {
	now := time.Now().UTC()
	b := testPackageEnvironment{URL: "https://baseline-private.example", ClusterName: "Same name", RancherVersion: "v2.15.2", Images: []string{"z:1", "a:1"}, RecordedAt: now, ObservedAt: &now, Details: []testPackageEnvironmentDetail{{Label: "Webhook", Value: "1.0", Source: "recorded"}, {Label: "Image", Value: "z:1", Source: "observed"}, {Label: "Image", Value: "a:1", Source: "observed"}}}
	later := now.Add(time.Hour)
	c := b
	c.URL = "https://candidate-private.example"
	c.RecordedAt, c.ObservedAt = later, &later
	c.Images = []string{"a:1", "z:1"}
	c.Details = []testPackageEnvironmentDetail{{Label: "Image", Value: "a:1", Source: "observed", ObservedAt: &later}, {Label: "Image", Value: "z:1", Source: "observed", ObservedAt: &later}, {Label: "Webhook", Value: "1.0", Source: "observed", ObservedAt: &later}}
	if differences := testPackageCompareEnvironments(b, c); len(differences) != 0 {
		t.Fatalf("clock, provenance, order, or private URL were reported as value changes: %+v", differences)
	}
	c.Details[2].Value = "1.1"
	if differences := testPackageCompareEnvironments(b, c); len(differences) != 1 || differences[0].Label != "Detail: Webhook" || differences[0].Baseline != "1.0" || differences[0].Candidate != "1.1" {
		t.Fatalf("exact webhook difference lost: %+v", differences)
	}
}
