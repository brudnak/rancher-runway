package test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// Temporary opt-in verification: the real cluster is read only; all package
// writes use testing's private temporary directory, never the user's library.
func TestRunwayPackageLiveReadOnlyVerification(t *testing.T) {
	path := os.Getenv("RUNWAY_LIVE_REGISTRY")
	if path == "" {
		t.Skip("explicit live registry required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest clusterWorkspaceManifest
	if err = json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	var record clusterWorkspaceRecord
	for _, item := range manifest.Clusters {
		if item.ID == "run-c3634dde-ha-1-local" {
			record = item
		}
	}
	if record.ID == "" {
		t.Fatal("previously registered Rancher is unavailable")
	}
	p := clusterWorkspaceTestPanel(t)
	p.clusterWorkspaces = map[string]clusterWorkspaceRecord{record.ID: record}
	p.clusterSnapshot[record.ID] = clusterView{ID: record.ID, Name: record.Name, Role: "local", Type: "local", RancherURL: record.URL, KubeconfigPath: record.Kubeconfig}
	s := packageTestService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Second)
	defer cancel()
	env, err := p.refreshIssuePackageEnvironment(ctx, issuePackageEnvironment{ClusterID: record.ID})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Environment read: Rancher=%q Kubernetes=%q observed details=%d images=%d warnings=%d", env.RancherVersion, env.KubernetesVersion, len(env.Details), len(env.Images), len(env.Warnings))
	if env.ObservedAt == nil {
		t.Fatal("live environment has no successful observations")
	}
	pkg := packageTestMutation(t, s, issuePackageRequest{Action: "create", Title: "Read-only capture verification"}, nil, nil, nil)
	pkg = packageTestMutation(t, s, issuePackageRequest{Action: "update", ID: pkg.ID, Revision: pkg.Revision, Title: pkg.Title, Status: "planning", Cases: []issuePackageCase{{Title: "Preserve a bounded log snapshot", Steps: []issuePackageStep{{Instruction: "Read recent Rancher and webhook logs", Expected: "Capture metadata and private evidence survive reopening"}}}}}, nil, nil, nil)
	pkg = packageTestMutation(t, s, issuePackageRequest{Action: "start-session", ID: pkg.ID, Revision: pkg.Revision, Name: "Read-only evidence verification", Purpose: "exploration"}, &env, nil, nil)
	session := pkg.Sessions[0]
	value, err := p.handleIssuePackageLogs(ctx, s, issuePackageRequest{Action: "log-targets", ID: pkg.ID, Revision: pkg.Revision, SessionID: session.ID})
	if err != nil {
		t.Fatal(err)
	}
	targets := value.(map[string]any)["targets"].([]issuePackageLogTarget)
	seen := map[string]bool{}
	var artifacts []string
	for _, target := range targets {
		if seen[target.Component] {
			continue
		}
		value, err = p.handleIssuePackageLogs(ctx, s, issuePackageRequest{Action: "capture-logs", ID: pkg.ID, Revision: pkg.Revision, SessionID: session.ID, CaseID: session.Cases[0].ID, StepID: session.Cases[0].Steps[0].ID, Pod: target.Pod, PodUID: target.PodUID, Container: target.Container, TailLines: 30, SinceSeconds: 300})
		if err != nil {
			t.Fatal(err)
		}
		pkg = value.(map[string]any)["package"].(issuePackage)
		proof := pkg.Sessions[0].Evidence[len(pkg.Sessions[0].Evidence)-1]
		artifacts = append(artifacts, proof.Artifact.Name)
		seen[target.Component] = true
		t.Logf("Captured %s: %d bytes; pod identity and scoped attachment verified", target.Component, proof.Artifact.Bytes)
	}
	if !seen["rancher"] || !seen["webhook"] {
		t.Fatal("both Rancher and webhook capture are required for this verification")
	}
	reopened, err := newIssuePackageService(s.root)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := reopened.buildBundle(pkg.ID, false, artifacts)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = validateIssuePackageBundle(&bundle); err != nil {
		t.Fatal(err)
	}
	t.Log("Private persistence and portable evidence bundle validation passed; no Rancher workloads changed")
}
