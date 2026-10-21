package test

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIssuePackagePlanMarkdownHTTP(t *testing.T) {
	p := clusterWorkspaceTestPanel(t)
	p.issuePackages = packageTestService(t)
	pkg := packageTestPlan(t, p.issuePackages)
	raw, err := json.Marshal(issuePackageRequest{Action: "plan-markdown", ID: pkg.ID, Revision: pkg.Revision})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "http://localhost/api/issue-packages", strings.NewReader(string(raw)))
	r.RemoteAddr = "127.0.0.1:1"
	r.Header.Set("Origin", "http://localhost")
	w := httptest.NewRecorder()
	p.handleIssuePackages(w, r)
	if w.Code != 200 {
		t.Fatalf("plan copy failed: %d %s", w.Code, w.Body.String())
	}
	var response struct {
		Markdown string `json:"markdown"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || !strings.Contains(response.Markdown, "NOT TESTED YET") {
		t.Fatalf("invalid Markdown response: %s", w.Body.String())
	}
	stored, err := p.issuePackages.get(pkg.ID)
	if err != nil || stored.Revision != pkg.Revision || len(stored.Sessions) != 0 {
		t.Fatal("copying a plan created a session or changed saved data")
	}
}

func TestIssuePackageGitHubPlanTitlesOnlyAndEscaping(t *testing.T) {
	s := packageTestService(t)
	pkg := packageTestPlan(t, s)
	pkg.Cases[0].Title = "Check [links](#fake) | <details> & `code`"
	pkg.Cases[0].Steps = nil
	pkg.Notes = "private notebook text"
	e := issuePackageEnvironment{Source: "manual", RecordedAt: time.Now().UTC(), Configuration: "</details>\n<script>bad()</script>"}
	markdown, err := issuePackageGitHubPlanMarkdown(pkg, e, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"<!-- -->\n<!-- -->", "<summary>🧪 Test Environment", "| Component | Version / Type |", "<a name=\"top\"></a>", "### 🧪 Test Cases",
		"| # | Priority | Description & Link | PASS/FAIL |", "| 1 | P0 | [Check \\[links\\](\\#fake) \\| &lt;details&gt; &amp; \\`code\\`](#test-1) | ⏸️ NOT TESTED YET |",
		"<a name=\"test-1\"></a>", "[back to top](#top)", "**Test Steps for Validation**", "**✅ Expected Outcome**", "**✅ Actual Outcome**",
		"_Add steps as you test._", "&lt;/details&gt;", "&lt;script&gt;bad()&lt;/script&gt;", "| Browser type | Not recorded |",
	} {
		if !strings.Contains(markdown, expected) {
			t.Fatalf("export missing %q:\n%s", expected, markdown)
		}
	}
	for _, absent := range []string{"private notebook text", "<script>", "✅ PASS"} {
		if strings.Contains(markdown, absent) {
			t.Fatalf("export unexpectedly includes %q", absent)
		}
	}
	if strings.Count(markdown, "<details>") != strings.Count(markdown, "</details>") {
		t.Fatal("user text broke collapsible sections")
	}
}

func TestIssuePackageGitHubPlanResultsRequireExplicitUnchangedSession(t *testing.T) {
	s := packageTestService(t)
	pkg := packageTestStart(t, s, packageTestPlan(t, s))
	session := pkg.Sessions[0]
	pkg = packageTestMutation(t, s, issuePackageRequest{Action: "case-result", ID: pkg.ID, Revision: pkg.Revision, SessionID: session.ID, CaseID: pkg.Cases[0].ID, Outcome: "passed", Notes: "Observed the correct resource"}, nil, nil, nil)
	session = pkg.Sessions[0]
	pkg.Cases[0].Preconditions = "New behavior not tested in the session"
	pkg.Cases[0].Expected = "The new behavior is correct"
	markdown, err := issuePackageGitHubPlanMarkdown(pkg, session.Environment, &session)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markdown, "✅ PASS") || strings.Contains(markdown, "Observed the correct resource") || !strings.Contains(markdown, "NOT TESTED YET") {
		t.Fatal("copied a previous result onto a changed case")
	}
	pkg.Cases = append([]issuePackageCase{}, session.Cases...)
	markdown, err = issuePackageGitHubPlanMarkdown(pkg, session.Environment, nil)
	if err != nil || strings.Contains(markdown, "✅ PASS") {
		t.Fatal("default plan unexpectedly inferred a session")
	}
	markdown, err = issuePackageGitHubPlanMarkdown(pkg, session.Environment, &session)
	if err != nil || !strings.Contains(markdown, "✅ PASS") || !strings.Contains(markdown, "Observed the correct resource") {
		t.Fatal("explicit matching session lost its result")
	}
	for _, expected := range []string{"1. Open the project", "   - Expected: Its resources are visible"} {
		if !strings.Contains(markdown, expected) {
			t.Fatalf("missing step content %q", expected)
		}
	}
}

func TestIssuePackagePlanMarkdownActionUsesSelectedEnvironment(t *testing.T) {
	p := clusterWorkspaceTestPanel(t)
	p.rememberClusterSnapshot([]clusterView{{ID: "tenant", Name: "Tenant Rancher", Type: "local", Role: "tenant", DeploymentType: deploymentTypeHostedTenantK3S, Version: "head", RancherURL: "https://private.rancher.test"}})
	s := packageTestService(t)
	pkg := packageTestPlan(t, s)
	req := issuePackageRequest{Action: "plan-markdown", ID: pkg.ID, Revision: pkg.Revision, Environment: issuePackageEnvironment{ClusterID: "tenant"}}
	result, err := p.issuePackagePlanMarkdown(s, req)
	if err != nil {
		t.Fatal(err)
	}
	data := result.(map[string]any)
	markdown := data["markdown"].(string)
	for _, expected := range []string{"Hosted tenant Rancher on K3s", "| Infrastructure provider | AWS |", "| K3s Local K8s Version | Not recorded |", "head (saved configuration)"} {
		if !strings.Contains(markdown, expected) {
			t.Fatalf("missing selected environment %q", expected)
		}
	}
	if strings.Contains(markdown, "https://private.rancher.test") || data["environment"].(issuePackageEnvironment).ClusterID != "tenant" {
		t.Fatal("selected environment was lost or its private endpoint was exported")
	}
	req.Revision = "stale"
	if _, err := p.issuePackagePlanMarkdown(s, req); !errors.Is(err, errIssuePackageConflict) {
		t.Fatal("stale plan copy was accepted")
	}
	req.Revision, req.SessionID = pkg.Revision, "missing"
	if _, err := p.issuePackagePlanMarkdown(s, req); err == nil {
		t.Fatal("missing session silently ignored")
	}
	pkg = packageTestStart(t, s, pkg)
	req.Revision, req.SessionID = pkg.Revision, pkg.Sessions[0].ID
	if _, err := p.issuePackagePlanMarkdown(s, req); err == nil {
		t.Fatal("results relabeled with a different environment")
	}
}

func TestIssuePackageInstallationClassification(t *testing.T) {
	for _, tc := range []struct {
		name, deployment, role, kind, version, installation, distribution, provider string
	}{
		{"rke2-ha", deploymentTypeHARKE2, "local", "local", "", "Rancher on RKE2 HA", "RKE2", "AWS"},
		{"hosted-tenant", deploymentTypeHostedTenantK3S, "tenant", "local", "", "Hosted tenant Rancher on K3s", "K3s", "AWS"},
		{"hosted-host", deploymentTypeHostedTenantK3S, "host", "local", "", "Host Rancher on K3s", "K3s", "AWS"},
		{"linode-docker", deploymentTypeLinodeDocker, "docker", "linode", "", "Rancher Docker installation", "", "Linode"},
		{"downstream-not-management", deploymentTypeHARKE2, "downstream", "downstream", "v1.35.3+k3s1", "", "K3s", ""},
		{"imported-rke2", "", "", "", "v1.35.3+rke2r1", "", "RKE2", ""},
		{"unknown", "", "", "", "", "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now().UTC()
			e := issuePackageEnvironment{}
			for _, detail := range [][2]string{{"Deployment", tc.deployment}, {"Cluster role", tc.role}, {"Cluster type", tc.kind}} {
				packageEnvironmentDetail(&e, detail[0], detail[1], "recorded", nil)
			}
			packageEnvironmentDetail(&e, "Kubernetes server version", tc.version, "observed", &now)
			packageEnvironmentInstallation(&e)
			got := map[string]issuePackageEnvironmentDetail{}
			for _, detail := range e.Details {
				got[detail.Label] = detail
			}
			if got["Installation type"].Value != tc.installation || got["Kubernetes distribution"].Value != tc.distribution || got["Infrastructure provider"].Value != tc.provider {
				t.Fatalf("incorrect installation classification: %+v", got)
			}
			if tc.version != "" && (got["Kubernetes distribution"].Source != "observed" || got["Kubernetes distribution"].ObservedAt == nil) {
				t.Fatal("distribution lost observation provenance")
			}
		})
	}
}

func TestIssuePackageInstallationFromSavedRegistryDoesNotUseDownstreamParent(t *testing.T) {
	p := clusterWorkspaceTestPanel(t)
	p.writeRunRecord(panelRunRecord{RunID: "rke2", DeploymentType: deploymentTypeHARKE2, CreatedAt: time.Now().UTC()})
	p.writeRunRecord(panelRunRecord{RunID: "hosted", DeploymentType: deploymentTypeHostedTenantK3S, CreatedAt: time.Now().UTC()})
	p.rememberClusterSnapshot([]clusterView{
		{ID: "run-rke2-ha-1-downstream-fleet-default-example", RunID: "rke2", Type: "downstream", Version: "v1.35.3+k3s1", Name: "Downstream"},
		{ID: "saved-tenant", RunID: "hosted", Type: "local", Role: "tenant", Version: "v2.15.2", Name: "Tenant"},
	})
	// A restarted server has registry records before rediscovering live views.
	p.rememberClusterSnapshot(nil)
	for _, tc := range []struct{ id, installation, distribution, role string }{
		{"run-rke2-ha-1-downstream-fleet-default-example", "", "K3s", "downstream"},
		{"saved-tenant", "Hosted tenant Rancher on K3s", "K3s", "tenant"},
	} {
		e, err := p.issuePackageEnvironment(issuePackageEnvironment{ClusterID: tc.id})
		if err != nil {
			t.Fatal(err)
		}
		details := map[string]issuePackageEnvironmentDetail{}
		for _, detail := range e.Details {
			details[detail.Label] = detail
		}
		if details["Installation type"].Value != tc.installation || details["Kubernetes distribution"].Value != tc.distribution || details["Cluster role"].Value != tc.role {
			t.Fatalf("saved registry misclassified %s: %+v", tc.id, details)
		}
		if tc.role == "downstream" && (details["Infrastructure provider"].Value != "" || details["Deployment"].Value != "" || e.RancherVersion != "") {
			t.Fatal("downstream inherited management installation metadata")
		}
		if details["Kubernetes distribution"].Source != "recorded" || details["Kubernetes distribution"].ObservedAt != nil {
			t.Fatal("saved distribution was presented as a live observation")
		}
	}
}
