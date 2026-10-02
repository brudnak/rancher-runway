package test

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/brudnak/ha-rancher-rke2/internal/cachelab"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func packageTestService(t *testing.T) *issuePackageService {
	t.Helper()
	s, err := newIssuePackageService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func packageTestMutation(t *testing.T, s *issuePackageService, req issuePackageRequest, e *issuePackageEnvironment, proof *issuePackageEvidence, raw []byte) issuePackage {
	t.Helper()
	result, err := s.mutate(req, e, proof, raw)
	if err != nil {
		t.Fatal(err)
	}
	return result.(map[string]any)["package"].(issuePackage)
}
func packageTestPlan(t *testing.T, s *issuePackageService) issuePackage {
	t.Helper()
	pkg := packageTestMutation(t, s, issuePackageRequest{Action: "create", Title: "Project visibility", IssueURL: "https://github.com/rancher/rancher/issues/123"}, nil, nil, nil)
	return packageTestMutation(t, s, issuePackageRequest{Action: "update", ID: pkg.ID, Revision: pkg.Revision, Title: pkg.Title, Status: "planning", Cases: []issuePackageCase{{Title: "Project member sees the resource", Steps: []issuePackageStep{{Instruction: "Open the project", Expected: "Its resources are visible"}}}}}, nil, nil, nil)
}
func packageTestStart(t *testing.T, s *issuePackageService, pkg issuePackage) issuePackage {
	t.Helper()
	e := issuePackageEnvironment{ClusterID: "cluster-a", ClusterName: "Baseline", RancherVersion: "v2.15.2", Source: "manual", RecordedAt: time.Now().UTC(), Images: []string{"rancher/rancher:v2.15.2"}}
	return packageTestMutation(t, s, issuePackageRequest{Action: "start-session", ID: pkg.ID, Revision: pkg.Revision, Name: "Before the fix", Purpose: "reproduction"}, &e, nil, nil)
}
func TestIssuePackageFrozenSessionsAndOptimisticRevision(t *testing.T) {
	s := packageTestService(t)
	pkg := packageTestPlan(t, s)
	original := pkg
	pkg = packageTestStart(t, s, pkg)
	session := pkg.Sessions[0]
	c := session.Cases[0]
	step := c.Steps[0]
	if _, err := s.mutate(issuePackageRequest{Action: "update", ID: pkg.ID, Revision: original.Revision}, nil, nil, nil); !errors.Is(err, errIssuePackageConflict) {
		t.Fatalf("stale write accepted: %v", err)
	}
	pkg.Cases[0].Steps[0].Instruction = "A different future plan"
	pkg = packageTestMutation(t, s, issuePackageRequest{Action: "update", ID: pkg.ID, Revision: pkg.Revision, Title: pkg.Title, Status: "reproducing", Cases: pkg.Cases}, nil, nil, nil)
	if pkg.Sessions[0].Cases[0].Steps[0].Instruction != step.Instruction {
		t.Fatal("plan edit rewrote frozen history")
	}
	before := time.Now().UTC()
	pkg = packageTestMutation(t, s, issuePackageRequest{Action: "mark-step", ID: pkg.ID, Revision: pkg.Revision, SessionID: session.ID, CaseID: c.ID, StepID: step.ID, Done: true}, nil, nil, nil)
	result := pkg.Sessions[0].Results[0]
	if result.Outcome != "not-run" || !result.Steps[0].Done || result.Steps[0].At.Before(before) {
		t.Fatal("step marker inferred pass or was not stamped by server")
	}
	pkg = packageTestMutation(t, s, issuePackageRequest{Action: "case-result", ID: pkg.ID, Revision: pkg.Revision, SessionID: session.ID, CaseID: c.ID, Outcome: "failed", Notes: "Reproduced the original symptom"}, nil, nil, nil)
	pkg = packageTestMutation(t, s, issuePackageRequest{Action: "finish-session", ID: pkg.ID, Revision: pkg.Revision, SessionID: session.ID, Finding: "reproduced", Conclusion: "Original issue reproduced"}, nil, nil, nil)
	for _, action := range []string{"mark-step", "case-result", "attach-evidence", "remove-evidence", "finish-session"} {
		_, err := s.mutate(issuePackageRequest{Action: action, ID: pkg.ID, Revision: pkg.Revision, SessionID: session.ID}, nil, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "preserved") {
			t.Fatalf("completed history changed via %s: %v", action, err)
		}
	}
	restored, err := newIssuePackageService(s.root)
	if err != nil {
		t.Fatal(err)
	}
	saved, _ := restored.get(pkg.ID)
	if saved.Sessions[0].Results[0].Outcome != "failed" || saved.Sessions[0].Finding != "reproduced" {
		t.Fatal("restart lost recorded result")
	}
	info, err := os.Stat(filepath.Join(s.root, pkg.ID, "package.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("manifest was not private")
	}
}
func TestIssuePackageEvidenceSurvivesLabDeletionAndImportsAsCopy(t *testing.T) {
	p := clusterWorkspaceTestPanel(t)
	s := packageTestService(t)
	pkg := packageTestStart(t, s, packageTestPlan(t, s))
	lab, err := newTestLabService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p.testLab = lab
	run := testLabRun{ID: cachelab.ID(), ClusterID: "cluster-a", Name: "VAI regression", Status: "failed", StartedAt: time.Now().Add(-time.Minute), FinishedAt: time.Now(), Results: []testLabResult{{Name: "TestVAI", Status: "fail"}}}
	lab.library.Runs = []testLabRun{run}
	path := filepath.Join(lab.root, run.ID+".log")
	if err = os.WriteFile(path, []byte("failure token-abc:secret123\n"), 0600); err != nil {
		t.Fatal(err)
	}
	proof, raw, err := p.issuePackageEvidence(issuePackageRequest{Kind: "test-run", SourceID: run.ID})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret123") {
		t.Fatal("recognizable token copied")
	}
	pkg = packageTestMutation(t, s, issuePackageRequest{Action: "attach-evidence", ID: pkg.ID, Revision: pkg.Revision, SessionID: pkg.Sessions[0].ID}, nil, &proof, raw)
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	artifactPath, err := s.artifactPath(pkg.ID, proof.Artifact.Name)
	if err != nil {
		t.Fatal(err)
	}
	preserved, err := os.ReadFile(artifactPath)
	if err != nil || !bytes.Equal(preserved, raw) {
		t.Fatal("source deletion removed package evidence")
	}
	read, err := s.readEvidence(issuePackageRequest{ID: pkg.ID, SessionID: pkg.Sessions[0].ID, SourceID: proof.ID})
	if err != nil || read.(map[string]any)["text"] != string(raw) {
		t.Fatalf("preserved reader failed after source cleanup: %v", err)
	}
	imported, err := s.importPackage(pkg, map[string][]byte{proof.Artifact.Name: raw})
	if err != nil {
		t.Fatal(err)
	}
	if imported.ID == pkg.ID || imported.OriginID != pkg.ID || imported.Sessions[0].Status != "completed" || imported.Sessions[0].Finding != "inconclusive" || imported.Sessions[0].Environment.Source != "imported" {
		t.Fatal("import resumed work or lost provenance")
	}
	if _, err = s.importPackage(pkg, map[string][]byte{proof.Artifact.Name: []byte("tampered")}); err == nil {
		t.Fatal("tampered artifact imported")
	}
	metadataOnly, err := s.importPackage(pkg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !metadataOnly.Sessions[0].Evidence[0].Artifact.Omitted {
		t.Fatal("omitted file was presented as available")
	}
	if _, err := s.readEvidence(issuePackageRequest{ID: metadataOnly.ID, SessionID: metadataOnly.Sessions[0].ID, SourceID: proof.ID}); err == nil {
		t.Fatal("omitted artifact read succeeded")
	}
	missingPath, _ := s.artifactPath(metadataOnly.ID, proof.Artifact.Name)
	if _, err = os.Stat(missingPath); !os.IsNotExist(err) {
		t.Fatal("metadata-only import invented artifact bytes")
	}
}
func TestIssuePackageStrictValidationAndFailurePreserveData(t *testing.T) {
	s := packageTestService(t)
	pkg := packageTestStart(t, s, packageTestPlan(t, s))
	oldRevision := pkg.Revision
	invalid := cloneIssuePackage(pkg)
	invalid.Sessions[0].Results[0].Steps[0].StepID = cachelab.ID()
	if validateIssuePackage(invalid) == nil {
		t.Fatal("foreign step accepted")
	}
	badCase := pkg.Cases[0]
	if _, err := s.mutate(issuePackageRequest{Action: "update", ID: pkg.ID, Revision: pkg.Revision, Title: pkg.Title, Status: "planning", Cases: []issuePackageCase{badCase, badCase}}, nil, nil, nil); err == nil {
		t.Fatal("duplicate cases accepted")
	}
	if _, err := s.artifactPath(pkg.ID, "../../escape.db"); err == nil {
		t.Fatal("artifact traversal accepted")
	}
	if _, err := s.mutate(issuePackageRequest{Action: "delete", ID: pkg.ID, Revision: pkg.Revision, Confirm: "DELETE"}, nil, nil, nil); err == nil {
		t.Fatal("old confirmation accepted")
	}
	saved, _ := s.get(pkg.ID)
	if saved.Revision != oldRevision {
		t.Fatal("failed operation mutated package")
	}
	// Private writes must roll back memory if the manifest cannot be replaced.
	manifest := filepath.Join(s.root, pkg.ID, "package.json")
	if err := os.Remove(manifest); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(manifest, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := s.mutate(issuePackageRequest{Action: "update", ID: pkg.ID, Revision: pkg.Revision, Title: "Should not stick", Status: "planning", Cases: pkg.Cases}, nil, nil, nil); err == nil {
		t.Fatal("write failure not reported")
	}
	saved, _ = s.get(pkg.ID)
	if saved.Title != pkg.Title || saved.Revision != pkg.Revision {
		t.Fatal("failed persist changed in-memory plan")
	}
}
func TestIssuePackageCorruptDataAndPrivateAPI(t *testing.T) {
	s := packageTestService(t)
	pkg := packageTestPlan(t, s)
	path := filepath.Join(s.root, pkg.ID, "package.json")
	if err := os.WriteFile(path, []byte(`{"unexpected":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := newIssuePackageService(s.root); err == nil {
		t.Fatal("corrupt library was silently accepted")
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != `{"unexpected":true}` {
		t.Fatal("corrupt source modified")
	}
	p := clusterWorkspaceTestPanel(t)
	p.issuePackages = packageTestService(t)
	for _, method := range []string{"GET", "POST"} {
		r := httptest.NewRequest(method, "http://localhost/api/issue-packages", strings.NewReader(`{"action":"create","title":"Forbidden"}`))
		r.RemoteAddr = "127.0.0.1:1"
		w := httptest.NewRecorder()
		p.handleIssuePackages(w, r)
		if w.Code != 401 {
			t.Fatalf("same-origin requirement bypassed: %s %d", method, w.Code)
		}
	}
	r := httptest.NewRequest("POST", "http://localhost/api/issue-packages", strings.NewReader(`{"action":"create","title":"Authorized"}`))
	r.RemoteAddr = "127.0.0.1:1"
	r.Header.Set("Origin", "http://localhost")
	w := httptest.NewRecorder()
	p.handleIssuePackages(w, r)
	if w.Code != 200 {
		t.Fatalf("authorized create failed %d %s", w.Code, w.Body.String())
	}
	var out struct {
		Package issuePackage `json:"package"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.Package.Title != "Authorized" {
		t.Fatal("unexpected create response")
	}
	r = httptest.NewRequest("POST", "http://localhost/api/issue-packages", strings.NewReader(`{"action":"create","title":"bad","sessions":[]}`))
	r.RemoteAddr = "127.0.0.1:1"
	r.Header.Set("Origin", "http://localhost")
	w = httptest.NewRecorder()
	p.handleIssuePackages(w, r)
	if w.Code != 400 {
		t.Fatal("client injected session history")
	}
}
func TestIssuePackageEnvironmentAndCacheProvenance(t *testing.T) {
	p := clusterWorkspaceTestPanel(t)
	p.rememberClusterSnapshot([]clusterView{{ID: "cluster-a", Name: "Observed cluster", Version: "v2.15.2", RancherURL: "https://rancher.test", Pods: []podView{{Images: []containerImageView{{Image: "rancher/rancher:v2.15.2", ImageID: "sha256:observed"}}}}}})
	e, err := p.issuePackageEnvironment(issuePackageEnvironment{ClusterID: "cluster-a", ClusterName: "Untrusted name", RancherVersion: "made-up", URL: "https://wrong.test", KubernetesVersion: "v1.35.1"})
	if err != nil {
		t.Fatal(err)
	}
	if e.ClusterName != "Observed cluster" || e.RancherVersion != "v2.15.2" || e.URL != "https://rancher.test" || len(e.Images) != 2 || e.Source != "recorded" {
		t.Fatalf("client spoofed environment: %+v", e)
	}
	s := packageTestService(t)
	pkg := packageTestStart(t, s, packageTestPlan(t, s))
	proof := issuePackageEvidence{ID: cachelab.ID(), Kind: "note", Name: "Other cluster", ClusterID: "wrong-cluster", Metadata: json.RawMessage(`{}`), CapturedAt: time.Now(), AttachedAt: time.Now()}
	if _, err = s.mutate(issuePackageRequest{Action: "attach-evidence", ID: pkg.ID, Revision: pkg.Revision, SessionID: pkg.Sessions[0].ID}, nil, &proof, nil); err == nil {
		t.Fatal("cross-cluster evidence accepted")
	}
	cache, err := cachelab.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p.cacheLab = cache
	workspace := cachelab.Workspace{ID: cachelab.ID(), ClusterID: "cluster-a"}
	snapshot := cachelab.Snapshot{ID: cachelab.ID(), Workspace: workspace.ID, Name: "Baseline", CreatedAt: time.Now(), Bytes: 64, Tables: 2}
	cache = cacheTestRewrite(t, cache, func(lib *cachelab.Library) {
		lib.Workspaces = []cachelab.Workspace{workspace}
		lib.Snapshots = []cachelab.Snapshot{snapshot}
	})
	p.cacheLab = cache
	proof, raw, err := p.issuePackageEvidence(issuePackageRequest{Kind: "cache-snapshot", SourceID: snapshot.ID, WorkspaceID: workspace.ID})
	if err != nil || proof.Artifact != nil || len(raw) != 0 || proof.ClusterID != "cluster-a" {
		t.Fatal("metadata-only snapshot failed or read database")
	}
	dbpath := filepath.Join(cache.Root(), workspace.ID, snapshot.ID+".db")
	if err = os.MkdirAll(filepath.Dir(dbpath), 0700); err != nil {
		t.Fatal(err)
	}
	db := append([]byte("SQLite format 3\x00"), make([]byte, 48)...)
	if err = os.WriteFile(dbpath, db, 0600); err != nil {
		t.Fatal(err)
	}
	proof, raw, err = p.issuePackageEvidence(issuePackageRequest{Kind: "cache-snapshot", SourceID: snapshot.ID, WorkspaceID: workspace.ID, IncludeDatabase: true})
	if err != nil || proof.Artifact == nil || !bytes.Equal(raw, db) {
		t.Fatalf("explicit DB copy failed %v", err)
	}
	f, err := os.OpenFile(dbpath, os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(issuePackageArtifactLimit + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, _, err = p.issuePackageEvidence(issuePackageRequest{Kind: "cache-snapshot", SourceID: snapshot.ID, WorkspaceID: workspace.ID, IncludeDatabase: true}); err == nil {
		t.Fatal("oversize database copied")
	}
}

func TestIssuePackageReadBoundsAndStrictMetadata(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "log")
	if err := os.WriteFile(path, []byte("12345"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := issuePackageReadBounded(path, 4); err == nil {
		t.Fatal("oversized file read")
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := issuePackageReadBounded(link, 10); err == nil {
		t.Fatal("symlink read")
	}
	s := packageTestService(t)
	pkg := packageTestStart(t, s, packageTestPlan(t, s))
	proof := issuePackageEvidence{ID: cachelab.ID(), Kind: "note", Name: "Hidden metadata", Description: "An observation", CapturedAt: time.Now(), AttachedAt: time.Now(), Metadata: json.RawMessage(`{"token":"secret"}`)}
	if _, err := s.mutate(issuePackageRequest{Action: "attach-evidence", ID: pkg.ID, Revision: pkg.Revision, SessionID: pkg.Sessions[0].ID}, nil, &proof, nil); err == nil {
		t.Fatal("unknown credential-shaped metadata accepted")
	}
	if err := validateIssuePackageEnvironment(issuePackageEnvironment{Source: "manual", RecordedAt: time.Now(), URL: "https://rancher.test/#token=secret"}); err == nil {
		t.Fatal("environment fragment accepted")
	}
	var req issuePackageRequest
	if err := decodeIssuePackage([]byte(`{"action":"create","action":"delete"}`), &req); err == nil {
		t.Fatal("duplicate request keys accepted")
	}
}

func TestIssuePackageEnvironmentDistinguishesComponentVersions(t *testing.T) {
	p := clusterWorkspaceTestPanel(t)
	p.rememberClusterSnapshot([]clusterView{
		{ID: "k3d-record", Name: "Local K3D", Role: "k3d", Version: "v1.35.2-k3s1"},
		{ID: "steve-record", Name: "Local Steve", Role: "steve", Version: "v0.10.6"},
		{ID: "run-abc-ha-1-downstream-sample", Name: "Downstream", Type: "downstream", Version: "v1.35.2+rke2r1", RancherURL: "https://management.test"},
	})
	for _, in := range []struct{ id, k8s string }{{"k3d-record", "v1.35.2-k3s1"}, {"run-abc-ha-1-downstream-sample", "v1.35.2+rke2r1"}} {
		env, err := p.issuePackageEnvironment(issuePackageEnvironment{ClusterID: in.id, Images: []string{"manually-recorded@sha256:abc"}})
		if err != nil {
			t.Fatal(err)
		}
		if env.RancherVersion != "" || env.KubernetesVersion != in.k8s || len(env.Images) != 1 {
			t.Fatalf("component mislabeled or manual image lost: %+v", env)
		}
	}
	env, err := p.issuePackageEnvironment(issuePackageEnvironment{ClusterID: "steve-record", KubernetesVersion: "v1.35.2", Configuration: "SQL cache enabled"})
	if err != nil {
		t.Fatal(err)
	}
	if env.RancherVersion != "" || !strings.Contains(env.Configuration, "Steve reference (local record): v0.10.6") || env.KubernetesVersion != "v1.35.2" {
		t.Fatalf("Steve ref mislabeled as Rancher: %+v", env)
	}
	// Retained downstream history has no current snapshot, but its stable local
	// identity still distinguishes the recorded Kubernetes version.
	p.mu.Lock()
	p.clusterSnapshot = map[string]clusterView{}
	p.mu.Unlock()
	env, err = p.issuePackageEnvironment(issuePackageEnvironment{ClusterID: "run-abc-ha-1-downstream-sample"})
	if err != nil || env.RancherVersion != "" || env.KubernetesVersion != "v1.35.2+rke2r1" {
		t.Fatalf("retained downstream version mislabeled: %+v %v", env, err)
	}
}
