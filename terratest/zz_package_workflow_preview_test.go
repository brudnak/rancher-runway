package test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestRunwayPackageWorkflowPreview(t *testing.T) {
	if os.Getenv("RUNWAY_PACKAGE_PREVIEW") != "1" {
		t.Skip("preview opt-in")
	}
	p := clusterWorkspaceTestPanel(t)
	s := packageTestService(t)
	p.issuePackages = s
	p.trustedLocalOrigin = true
	pkg := packageTestMutation(t, s, issuePackageRequest{Action: "create", Title: "Project resources after changing a role", Summary: "Preserve the original behavior, then repeat the same cases against the candidate build.", Status: "planning"}, nil, nil, nil)
	pkg = packageTestMutation(t, s, issuePackageRequest{Action: "update", ID: pkg.ID, Revision: pkg.Revision, Title: pkg.Title, Summary: pkg.Summary, Status: "validating", FixURL: "https://github.com/rancher/rancher/pull/500", FixTitle: "Invalidate cached project lists", Notes: "Confirm both direct access and cached list responses.", Cases: []issuePackageCase{
		{Title: "Project member sees permitted resources", Preconditions: "Sign in with the project member test account.", Expected: "The namespace list reflects the current role.", Steps: []issuePackageStep{{Instruction: "Open the assigned project", Expected: "The project is accessible"}, {Instruction: "List the project's namespaces after updating the role", Expected: "Only authorized namespaces appear"}}},
		{Title: "Removing access updates existing lists", Steps: []issuePackageStep{{Instruction: "Remove the project binding and refresh the list", Expected: "Previously visible namespaces are no longer returned"}}},
		{Title: "A fresh session has the same result", Steps: []issuePackageStep{{Instruction: "Sign in again with the same test account", Expected: "The list matches the current permissions"}}},
	}}, nil, nil, nil)
	baselineEnv := issuePackageEnvironment{ClusterName: "Reproduction environment", RancherVersion: "v2.15.2", KubernetesVersion: "v1.35.4+rke2r1", Source: "manual", RecordedAt: time.Now().UTC(), Images: []string{"rancher/rancher:v2.15.2"}, Configuration: "RKE2 · one server · SQL cache enabled"}
	pkg = packageTestMutation(t, s, issuePackageRequest{Action: "start-session", ID: pkg.ID, Revision: pkg.Revision, Name: "Original reproduction", Purpose: "reproduction"}, &baselineEnv, nil, nil)
	baseline := pkg.Sessions[0]
	for i, c := range baseline.Cases {
		outcome := "passed"
		notes := "The list reflected the current permissions."
		if i == 0 {
			outcome = "failed"
			notes = "The project remained accessible, but the namespace list retained the previous permissions until the session was restarted. Repeated twice using the same account."
		}
		pkg = packageTestMutation(t, s, issuePackageRequest{Action: "case-result", ID: pkg.ID, Revision: pkg.Revision, SessionID: baseline.ID, CaseID: c.ID, Outcome: outcome, Notes: notes}, nil, nil, nil)
	}
	pkg = packageTestMutation(t, s, issuePackageRequest{Action: "finish-session", ID: pkg.ID, Revision: pkg.Revision, SessionID: baseline.ID, Finding: "reproduced", Conclusion: "Original behavior reproduced in the first case; use this frozen plan for candidate validation."}, nil, nil, nil)
	candidateEnv := baselineEnv
	candidateEnv.ClusterName = "Candidate environment"
	candidateEnv.RancherVersion = "v2.15.3-rc1"
	candidateEnv.Images = []string{"rancher/rancher:v2.15.3-rc1"}
	pkg = packageTestMutation(t, s, issuePackageRequest{Action: "start-session", ID: pkg.ID, Revision: pkg.Revision, Name: "Candidate validation", Purpose: "validation", BaselineSessionID: baseline.ID, FixURL: "https://github.com/rancher/rancher/pull/500", FixTitle: "Invalidate cached project lists", FixCommit: "9f1c2b3a4d5e6f7089a1b2c3d4e5f60718293a4b"}, &candidateEnv, nil, nil)
	current := pkg.Sessions[len(pkg.Sessions)-1]
	pkg = packageTestMutation(t, s, issuePackageRequest{Action: "case-result", ID: pkg.ID, Revision: pkg.Revision, SessionID: current.ID, CaseID: current.Cases[0].ID, Outcome: "passed", Notes: "Namespace permissions changed immediately after the role update. The behavior remained consistent through repeated requests."}, nil, nil, nil)
	mux := http.NewServeMux()
	mux.HandleFunc("/", p.handleIndex)
	mux.HandleFunc("/static/", p.handleControlPanelStaticAsset)
	mux.HandleFunc("/api/issue-packages", p.handleIssuePackages)
	mux.HandleFunc("/api/cluster-workspaces", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, map[string]any{"clusters": []any{}}) })
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"runs": []any{}, "clusters": []any{}, "awsResources": []any{}, "operations": map[string]any{}, "workspace": map[string]any{"setupAllowed": true}, "panel": map[string]any{"build": map[string]any{"version": "preview"}}})
	})
	mux.HandleFunc("/api/preflight", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ready": true, "summary": "Isolated preview", "items": []any{}})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	if err := os.WriteFile("/tmp/runway-package-workflow-preview.url", []byte(server.URL), 0600); err != nil {
		t.Fatal(err)
	}
	t.Log("Isolated UI preview ready")
	deadline := time.Now().Add(30 * time.Minute)
	for time.Now().Before(deadline) {
		if _, err := os.Stat("/tmp/runway-package-workflow-preview.stop"); err == nil {
			return
		}
		time.Sleep(time.Second)
	}
}
