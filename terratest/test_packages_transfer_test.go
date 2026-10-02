package test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/brudnak/ha-rancher-rke2/internal/cachelab"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testPackageTransferFixture(t *testing.T) (*localControlPanel, *testPackageService, testPackage) {
	t.Helper()
	s, err := newTestPackageService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.exportRoot = t.TempDir()
	now := time.Now().UTC().Truncate(time.Second)
	c := testPackageCase{ID: cachelab.ID(), Title: "Expected state survives", Expected: "Saved value remains", Automation: "manual", Steps: []testPackageStep{{ID: cachelab.ID(), Instruction: "Change the setting", Expected: "The setting is saved"}}, Selection: []string{}}
	artifact := []byte("Saved local test result\n")
	meta, _ := json.Marshal(testPackageTestRunMetadata{ID: cachelab.ID(), Name: "Previous execution", Status: "passed", Results: []testLabResult{{Package: "validation/vai", Name: "TestCase", Status: "pass", Elapsed: 1}}})
	e := testPackageEvidence{ID: cachelab.ID(), Kind: "test-run", Name: "Related automated result", Description: "Pre-existing execution", SourceID: cachelab.ID(), CapturedAt: now, AttachedAt: now, Metadata: meta, Artifact: &testPackageArtifact{Name: cachelab.ID() + ".log", MediaType: "text/plain", Bytes: int64(len(artifact)), SHA256: testPackageDigest(artifact)}}
	pkg := testPackage{ID: cachelab.ID(), Revision: cachelab.ID(), Title: "Issue investigation", Summary: "Reproduce and validate the behavior", Status: "validating", Notes: "private-note-marker", Cases: []testPackageCase{c}, CreatedAt: now, UpdatedAt: now, Sessions: []testPackageSession{{ID: cachelab.ID(), Name: "Reproduction attempt", Purpose: "reproduction", Status: "active", Finding: "inconclusive", StartedAt: now, Environment: testPackageEnvironment{ClusterID: "stable-cluster", ClusterName: "Staging", URL: "https://private-target.example.test", RancherVersion: "v2.15.2", Images: []string{}, Source: "recorded", RecordedAt: now}, Cases: []testPackageCase{c}, Results: []testPackageResult{{CaseID: c.ID, Outcome: "not-run", Steps: []testPackageStepResult{{StepID: c.Steps[0].ID, Done: true, At: now}}}}, Evidence: []testPackageEvidence{e}}}}
	path, err := s.artifactPath(pkg.ID, e.Artifact.Name)
	if err != nil {
		t.Fatal(err)
	}
	if err = writePrivateConfigAtomically(path, artifact); err != nil {
		t.Fatal(err)
	}
	if err = s.saveLocked(pkg); err != nil {
		t.Fatal(err)
	}
	return &localControlPanel{token: "package-review-secret"}, s, pkg
}

func TestTestPackagePodLogsReportAndBundle(t *testing.T) {
	_, s, pkg := testPackageTransferFixture(t)
	now := time.Now().UTC()
	evidence := &pkg.Sessions[0].Evidence[0]
	evidence.Kind = "pod-logs"
	evidence.Name = "Previous Rancher container logs"
	metadata := testPackageLogMetadata{Component: "rancher", Namespace: "cattle-system", Pod: "rancher-abc", PodUID: "pod-uid", Container: "rancher", Image: "rancher/rancher:v2.15.2", ImageObservation: "current-container", StartedAt: now, CompletedAt: now, SinceTime: now.Add(-time.Minute), SinceSeconds: 60, TailLines: 1000, Previous: true, Timestamps: true, LimitBytes: testPackageLogMaxBytes, ReceivedBytes: evidence.Artifact.Bytes, Method: "kubernetes-pod-logs"}
	evidence.Metadata, _ = json.Marshal(metadata)
	s.mu.Lock()
	err := s.saveLocked(pkg)
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	report, err := testPackageMarkdown(pkg, "", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"cattle-system/rancher-abc", "Last 60 seconds", "Up to 1000 lines", "Current container image", "previous container instance", "not a continuous recording"} {
		if !strings.Contains(report, expected) {
			t.Fatalf("pod provenance omitted %s", expected)
		}
	}
	bundle, err := s.buildBundle(pkg.ID, false, []string{evidence.Artifact.Name})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(bundle)
	var restored testPackageBundle
	if err = json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	artifacts, _, err := validateTestPackageBundle(&restored)
	if err != nil || len(artifacts) != 1 {
		t.Fatalf("portable pod snapshot rejected %v", err)
	}
	if restored.Package.Sessions[0].Evidence[0].Kind != "pod-logs" {
		t.Fatal("pod log evidence changed kind")
	}
}

func TestTestPackageTransferRoundTripAndReviewBinding(t *testing.T) {
	p, s, pkg := testPackageTransferFixture(t)
	pkg.IssueURL = "https://github.com/rancher/rancher/issues/123"
	pkg.FixURL = "https://github.com/rancher/rancher/pull/500"
	pkg.FixTitle = "rancher/rancher#500 · Invalidate cached project lists"
	if err := s.saveLocked(pkg); err != nil {
		t.Fatal(err)
	}
	bundle, err := s.buildBundle(pkg.ID, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Package.Notes != "" || len(bundle.Artifacts) != 0 || !bundle.Package.Sessions[0].Evidence[0].Artifact.Omitted {
		t.Fatal("default export exposed private notes or artifact bytes")
	}
	preview, err := p.previewTestPackageImport(&bundle)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Artifacts != 0 || len(preview.Warnings) < 2 {
		t.Fatal("preview hid active attempt or omitted evidence")
	}
	if preview.IssueURL != bundle.Package.IssueURL || preview.FixURL != bundle.Package.FixURL || preview.FixTitle != bundle.Package.FixTitle {
		t.Fatal("preview did not name the package's issue and fix links")
	}
	changed := bundle
	changed.Package = cloneTestPackage(bundle.Package)
	changed.Package.Title = "Different payload"
	if _, err = p.handleTestPackageTransfer(context.Background(), s, testPackageRequest{Action: "import", Bundle: &changed, ReviewToken: preview.ReviewToken, Confirm: typedConfirmationPhrase}); err == nil {
		t.Fatal("review accepted changed input")
	}
	if _, err = p.handleTestPackageTransfer(context.Background(), s, testPackageRequest{Action: "import", Bundle: &bundle, ReviewToken: preview.ReviewToken}); err == nil {
		t.Fatal("import accepted missing confirmation")
	}
	value, err := p.handleTestPackageTransfer(context.Background(), s, testPackageRequest{Action: "import", Bundle: &bundle, ReviewToken: preview.ReviewToken, Confirm: typedConfirmationPhrase})
	if err != nil {
		t.Fatal(err)
	}
	imported := value.(map[string]any)["package"].(testPackage)
	if imported.ID == pkg.ID || imported.OriginID != pkg.ID || imported.Sessions[0].ID != pkg.Sessions[0].ID || imported.Sessions[0].Status != "completed" || imported.Sessions[0].Finding != "inconclusive" || imported.Sessions[0].Environment.Source != "imported" {
		t.Fatal("copy import did not preserve independent lineage/history")
	}
	original, _ := s.get(pkg.ID)
	if original.Revision != pkg.Revision || original.Sessions[0].Status != "active" {
		t.Fatal("import modified existing package")
	}
	withFiles, err := s.buildBundle(pkg.ID, true, []string{pkg.Sessions[0].Evidence[0].Artifact.Name})
	if err != nil {
		t.Fatal(err)
	}
	if withFiles.Package.Notes != pkg.Notes || len(withFiles.Artifacts) != 1 || withFiles.Package.Sessions[0].Evidence[0].Artifact.Omitted {
		t.Fatal("explicit private evidence selection lost content")
	}
	data, _, err := validateTestPackageBundle(&withFiles)
	if err != nil {
		t.Fatal(err)
	}
	copy, err := s.importPackage(withFiles.Package, data)
	if err != nil {
		t.Fatal(err)
	}
	path, _ := s.artifactPath(copy.ID, withFiles.Artifacts[0].Name)
	raw, err := os.ReadFile(path)
	if err != nil || testPackageDigest(raw) != withFiles.Artifacts[0].SHA256 {
		t.Fatal("owned artifact round trip failed")
	}
	if !strings.Contains(copy.Sessions[0].Conclusion, "not resumed") {
		t.Fatal("import suggested execution resumed")
	}
}

func TestTestPackageExportSanitizesImportedSessionAndBaselineHelmCommands(t *testing.T) {
	s := packageTestService(t)
	pkg := packageTestCandidate(t, s, packageTestCompletedBaseline(t, s))
	baselineCommand := "helm install rancher rancher/rancher --version 2.15.2 --set bootstrapPassword=baseline-secret,hostname=private-baseline.example,replicas=3 --values /Users/private/baseline.yml"
	candidateCommand := "helm upgrade --install rancher rancher/rancher --version 2.15.3 --set bootstrapPassword=candidate-secret,replicas=3 --kubeconfig /Users/private/candidate-kubeconfig --password registry-secret"
	pkg.Sessions[0].Environment.HelmCommand = baselineCommand
	pkg.Sessions[1].Environment.HelmCommand = candidateCommand
	pkg.Sessions[1].Baseline.Environment.HelmCommand = baselineCommand
	// Exercise the actual strict portable import path with an older third-party
	// package that did not redact its captured Helm commands.
	incoming := testPackageBundle{Format: testPackageBundleFormat, Version: testPackageBundleVersion, ExportedAt: time.Now().UTC(), Package: pkg, Artifacts: []testPackageBundledArtifact{}}
	raw, err := json.Marshal(incoming)
	if err != nil {
		t.Fatal(err)
	}
	var decoded testPackageBundle
	if err = decodeTestPackage(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	artifacts, _, err := validateTestPackageBundle(&decoded)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := s.importPackage(decoded.Package, artifacts)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := s.buildBundle(imported.ID, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"baseline-secret", "candidate-secret", "registry-secret", "private-baseline.example", "/Users/private/"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("export retained sensitive Helm value %q", secret)
		}
	}
	for _, command := range []string{bundle.Package.Sessions[0].Environment.HelmCommand, bundle.Package.Sessions[1].Environment.HelmCommand, bundle.Package.Sessions[1].Baseline.Environment.HelmCommand} {
		if !strings.Contains(command, "replicas=3") || !strings.Contains(command, "--version") || !strings.Contains(command, "<local-file>") || !strings.Contains(command, "<redacted>") {
			t.Fatalf("export lost reproducible command structure: %s", command)
		}
		if sanitizeTestPackageHelmCommand(command) != command {
			t.Fatal("repeated export changed a sanitized command")
		}
	}
	saved, err := s.get(imported.ID)
	if err != nil || saved.Sessions[1].Environment.HelmCommand != candidateCommand || saved.Sessions[1].Baseline.Environment.HelmCommand != baselineCommand || saved.Revision != imported.Revision {
		t.Fatalf("export modified preserved local history: %v", err)
	}
}

func TestTestPackageBundleStrictValidation(t *testing.T) {
	_, s, pkg := testPackageTransferFixture(t)
	bundle, err := s.buildBundle(pkg.ID, false, []string{pkg.Sessions[0].Evidence[0].Artifact.Name})
	if err != nil {
		t.Fatal(err)
	}
	original, _ := json.Marshal(bundle)
	for name, change := range map[string]func(*testPackageBundle){
		"null metadata": func(b *testPackageBundle) { b.Package.Sessions[0].Evidence[0].Metadata = json.RawMessage(`null`) },
		"version":       func(b *testPackageBundle) { b.Version = 2 },
		"format":        func(b *testPackageBundle) { b.Format = "other" },
		"traversal":     func(b *testPackageBundle) { b.Artifacts[0].Name = "../outside.log" },
		"duplicate":     func(b *testPackageBundle) { b.Artifacts = append(b.Artifacts, b.Artifacts[0]) },
		"checksum": func(b *testPackageBundle) {
			b.Artifacts[0].Data = base64.StdEncoding.EncodeToString([]byte("tampered"))
		},
		"credentials": func(b *testPackageBundle) {
			b.Package.Sessions[0].Evidence[0].Metadata = json.RawMessage(`{"adminToken":"not-allowed"}`)
		},
		"hidden path": func(b *testPackageBundle) {
			b.Package.Sessions[0].Evidence[0].Metadata = json.RawMessage(`{"kubeconfig":"/private/not-allowed"}`)
		},
		"nested execution": func(b *testPackageBundle) {
			b.Package.Sessions[0].Evidence[0].Metadata = json.RawMessage(`{"results":[{"command":"rm"}]}`)
		},
	} {
		t.Run(name, func(t *testing.T) {
			var copy testPackageBundle
			if err := json.Unmarshal(original, &copy); err != nil {
				t.Fatal(err)
			}
			change(&copy)
			if _, _, err := validateTestPackageBundle(&copy); err == nil {
				t.Fatal("invalid portable bundle accepted")
			}
		})
	}
	for _, raw := range [][]byte{[]byte(`{"format":"rancher-runway/test-package","format":"other"}`), []byte(strings.Replace(string(original), `"format":`, `"unknown":true,"format":`, 1)), append(append([]byte{}, original...), []byte(` {}`)...)} {
		var decoded testPackageBundle
		if err := json.Unmarshal(raw, &decoded); err == nil {
			t.Fatal("ambiguous/unknown JSON accepted")
		}
	}
	if _, err := s.buildBundle(pkg.ID, false, []string{"../../secrets"}); err == nil {
		t.Fatal("export read arbitrary path")
	}
	artifact := pkg.Sessions[0].Evidence[0].Artifact
	path, _ := s.artifactPath(pkg.ID, artifact.Name)
	if err := os.WriteFile(path, []byte("changed-on-disk"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.buildBundle(pkg.ID, false, []string{artifact.Name}); err == nil {
		t.Fatal("changed evidence exported")
	}
}

func TestTestPackageReportsDoNotInferPassOrLeakPrivateFields(t *testing.T) {
	p, s, pkg := testPackageTransferFixture(t)
	pkg.Title = "<script>unsafe</script> ![remote](https://tracker.test/image)"
	pkg.Sessions[0].Results[0].Notes = "Case remains unexecuted despite a checked step."
	if err := s.saveLocked(pkg); err != nil {
		t.Fatal(err)
	}
	markdown, err := testPackageMarkdown(pkg, "", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, hidden := range []string{"<script>", "![remote]", "private-note-marker", "private-target.example.test", s.root, pkg.Sessions[0].Evidence[0].Artifact.Name} {
		if strings.Contains(markdown, hidden) {
			t.Fatalf("report exposed executable/private content %q", hidden)
		}
	}
	if !strings.Contains(markdown, "not-run") || !strings.Contains(markdown, "Markers") && !strings.Contains(markdown, "markers") || !strings.Contains(markdown, "| 0 | 0 | 0 | 0 | 1 |") {
		t.Fatal("checked steps or attached passing evidence implied a passed manual case")
	}
	if _, err := testPackageMarkdown(pkg, "missing", false); err == nil {
		t.Fatal("unknown report session accepted")
	}
	value, err := p.handleTestPackageTransfer(context.Background(), s, testPackageRequest{Action: "export", ID: pkg.ID})
	if err != nil {
		t.Fatal(err)
	}
	result := value.(map[string]any)
	for _, key := range []string{"path", "reportPath"} {
		path := result[key].(string)
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0600 || filepath.Dir(path) != s.exportRoot {
			t.Fatalf("unsafe download %s %v", path, err)
		}
	}
	reportRaw, _ := os.ReadFile(result["reportPath"].(string))
	if !strings.Contains(string(reportRaw), "omitted from this package copy") {
		t.Fatal("metadata-only export report hid omitted evidence")
	}
}

func TestTestPackageReportComparesAttemptsAndExplainsEvidence(t *testing.T) {
	_, _, pkg := testPackageTransferFixture(t)
	first := &pkg.Sessions[0]
	first.Status = "completed"
	first.FinishedAt = first.StartedAt.Add(time.Minute)
	first.Finding = "reproduced"
	first.Results[0].Outcome = "failed"
	first.Cases[0].AutomationURL = "https://github.com/rancher/tests/pull/123"
	first.Cases[0].Selection = []string{"validation/vai::TestVaiSuite/TestCase"}
	var metadata testPackageTestRunMetadata
	if err := json.Unmarshal(first.Evidence[0].Metadata, &metadata); err != nil {
		t.Fatal(err)
	}
	metadata.SHA = strings.Repeat("b", 40)
	first.Evidence[0].Metadata, _ = json.Marshal(metadata)
	first.Evidence[0].CaseID = first.Cases[0].ID
	first.Evidence[0].StepID = first.Cases[0].Steps[0].ID
	second := cloneTestPackage(pkg).Sessions[0]
	second.ID = cachelab.ID()
	second.Name = "Fix verification"
	second.Purpose = "validation"
	second.Finding = "validated"
	second.Environment.RancherVersion = "v2.16.0"
	second.Results[0].Outcome = "passed"
	snapshot := testPackageCacheMetadata{ID: cachelab.ID(), Name: "After change", SHA256: strings.Repeat("c", 64), Bytes: 4096, Tables: 7, Image: "rancher/rancher:v2.16.0"}
	raw, _ := json.Marshal(snapshot)
	second.Evidence = []testPackageEvidence{{ID: cachelab.ID(), Kind: "cache-snapshot", Name: "After change", CapturedAt: second.StartedAt, AttachedAt: second.StartedAt, Metadata: raw}}
	pkg.Sessions = append(pkg.Sessions, second)
	report, err := testPackageMarkdown(pkg, "", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"## Session comparison", "reproduced | 0 / 1 / 0 / 0 / 0", "validated | 1 / 0 / 0 / 0 / 0", "v2.16.0", metadata.SHA, "Recorded result events: 1 passed", "do not set manual case outcomes", "step 1", snapshot.SHA256, "7 tables", "rancher/rancher:v2.16.0", first.Cases[0].AutomationURL, "Referenced Test Lab selections", "**Step 1:**", "*Marked "} {
		if !strings.Contains(report, want) {
			t.Fatalf("report omitted %q", want)
		}
	}
	if strings.Index(report, "## Session comparison") > strings.Index(report, "## Reproduction attempt") {
		t.Fatal("comparison buried below details")
	}
}
