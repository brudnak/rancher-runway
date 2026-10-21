package test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func packageSearchSave(t *testing.T, s *issuePackageService, pkg issuePackage) {
	t.Helper()
	s.mu.Lock()
	err := s.saveLocked(pkg)
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
}
func packageSearch(t *testing.T, s *issuePackageService, query string) issuePackageSearchResponse {
	t.Helper()
	result, err := s.searchPlans(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func TestIssuePackagePlanSearchArchivedPlansAndCompletedSessions(t *testing.T) {
	s := packageTestService(t)
	pkg := packageTestStart(t, s, packageTestPlan(t, s))
	pkg = packageTestMutation(t, s, issuePackageRequest{Action: "case-result", ID: pkg.ID, Revision: pkg.Revision, SessionID: pkg.Sessions[0].ID, CaseID: pkg.Cases[0].ID, Outcome: "passed", Notes: "Result SQL-cache-notes remains useful"}, nil, nil, nil)
	pkg = packageTestMutation(t, s, issuePackageRequest{Action: "finish-session", ID: pkg.ID, Revision: pkg.Revision, SessionID: pkg.Sessions[0].ID, Finding: "validated", Conclusion: "Conclusion SQL-cache-conclusion"}, nil, nil, nil)
	pkg.Status = "archived"
	pkg.Title = "SQL-cache-title regression"
	pkg.Summary = "SQL-cache-summary"
	pkg.Notes = "SQL-cache-plan-notes"
	pkg.Cases[0].Title = "SQL-cache-case"
	pkg.Cases[0].Preconditions = "SQL-cache-preconditions"
	pkg.Cases[0].Expected = "SQL-cache-expected"
	pkg.Cases[0].Steps[0].Instruction = "SQL-cache-step"
	pkg.Cases[0].Steps[0].Expected = "SQL-cache-step-expected"
	pkg.Sessions[0].Cases[0].Steps[0].Instruction = "SQL-cache-frozen-step"
	packageSearchSave(t, s, pkg)
	for _, tc := range []struct{ query, kind, field string }{
		{"sql-cache-title", "plan", "Plan title"}, {"sql-cache-summary", "plan", "Summary"}, {"sql-cache-plan-notes", "plan", "Plan notes"},
		{"sql-cache-case", "case", "Case title"}, {"sql-cache-preconditions", "case", "Preconditions"}, {"sql-cache-expected", "case", "Expected outcome"},
		{"sql-cache-step", "case", "Step 1"}, {"sql-cache-step-expected", "case", "Step 1 expected outcome"},
		{"sql-cache-frozen-step", "session", "Step 1"}, {"sql-cache-notes", "session", "Session notes"}, {"sql-cache-conclusion", "session", "Conclusion"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			found := false
			for _, m := range packageSearch(t, s, tc.query).Matches {
				if m.Kind != tc.kind || m.Field != tc.field {
					continue
				}
				found = true
				if m.PackageID != pkg.ID || m.Status != "archived" || !m.Archived || m.ID == "" || !strings.Contains(strings.ToLower(m.Snippet), tc.query) {
					t.Fatalf("invalid match %+v", m)
				}
				if m.Kind == "session" && m.SessionID != pkg.Sessions[0].ID {
					t.Fatal("session match lost its frozen snapshot")
				}
			}
			if !found {
				t.Fatalf("missing %s %s", tc.kind, tc.field)
			}
		})
	}
}
func TestIssuePackagePlanSearchHistoricalMarkdownAndUntrustedFiles(t *testing.T) {
	s := packageTestService(t)
	pkg := packageTestPlan(t, s)
	pkg.Summary = "A retired SQL-cache-previous behavior"
	packageSearchSave(t, s, pkg)
	pkg.Summary = "Current behavior"
	packageSearchSave(t, s, pkg)
	result := packageSearch(t, s, "sql-cache-previous")
	if len(result.Matches) != 1 || result.Matches[0].Kind != "history" || result.Matches[0].SourceID == "" || result.Matches[0].Path == "" {
		t.Fatalf("historical match unavailable: %+v", result)
	}
	history := result.Matches[0]
	if err := os.WriteFile(history.Path, []byte("SQL-cache-edited-history"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := packageSearch(t, s, "sql-cache-edited-history"); len(got.Matches) != 0 || len(got.Warnings) == 0 {
		t.Fatalf("modified history trusted: %+v", got)
	}
	outside := filepath.Join(t.TempDir(), "outside.md")
	content := []byte("SQL-cache-link-outside")
	if err := os.WriteFile(outside, content, 0600); err != nil {
		t.Fatal(err)
	}
	directory, err := s.planHistoryDirectory(pkg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(directory, issuePackagePlanDigest(content)+".md")); err != nil {
		t.Fatal(err)
	}
	if got := packageSearch(t, s, "sql-cache-link-outside"); len(got.Matches) != 0 {
		t.Fatal("search followed snapshot link")
	}
}
func TestIssuePackagePlanSearchCachedRepositoriesWithoutCommands(t *testing.T) {
	s := packageTestService(t)
	pkg := packageTestPlan(t, s)
	shared, _ := issuePackageGitRemote("owner/shared", "main")
	private, _ := issuePackageGitRemote("owner/private", "test-plans")
	g := &issuePackageGitService{packages: s, runner: func(context.Context, string, []string) (issuePackageGitRunResult, error) {
		t.Fatal("search launched a Git command")
		return issuePackageGitRunResult{}, nil
	}, reviews: map[string]issuePackageGitPreview{}, now: time.Now}
	issuePackageGitServices.Store(s, g)
	t.Cleanup(func() { issuePackageGitServices.Delete(s) })
	state := issuePackageGitState{Version: 1, DefaultConnection: shared, Packages: map[string]issuePackageGitSaved{pkg.ID: {Connection: private, Explicit: true}}}
	if err := g.saveState(state); err != nil {
		t.Fatal(err)
	}
	for _, c := range []issuePackageGitConnection{shared, private} {
		if err := os.MkdirAll(filepath.Join(g.checkout(c), "milestone"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(g.checkout(c), "milestone", "issue-123.md"), []byte("# A cached plan\n\n1. SQL-cache-repository"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(g.checkout(shared), ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(g.checkout(shared), ".git", "private.md"), []byte("SQL-cache-hidden"), 0600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("SQL-cache-outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(g.checkout(shared), "outside.md")); err != nil {
		t.Fatal(err)
	}
	result := packageSearch(t, s, "sql-cache-repository")
	if len(result.Matches) != 2 {
		t.Fatalf("missing cached repository matches: %+v", result)
	}
	for _, m := range result.Matches {
		if m.Kind != "repository" || m.GitPath != "milestone/issue-123.md" || m.Title != "A cached plan" {
			t.Fatalf("bad cached result: %+v", m)
		}
		if m.Repository == shared.Repository && (m.PackageID != "" || m.GitBranch != "main") {
			t.Fatal("default repo lost empty-ID import guard")
		}
		if m.Repository == private.Repository && (m.PackageID != pkg.ID || m.GitBranch != "test-plans") {
			t.Fatal("private repo lost associated plan")
		}
	}
	for _, query := range []string{"sql-cache-hidden", "sql-cache-outside"} {
		if len(packageSearch(t, s, query).Matches) != 0 {
			t.Fatalf("searched excluded content: %s", query)
		}
	}
	// A whole managed parent linked outside the store must also be rejected.
	if err := os.RemoveAll(filepath.Join(g.storageRoot(), "checkouts")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(g.storageRoot(), "checkouts")); err != nil {
		t.Fatal(err)
	}
	if got := packageSearch(t, s, "sql-cache-repository"); len(got.Matches) != 0 || len(got.Warnings) == 0 {
		t.Fatalf("unsafe checkout not skipped: %+v", got)
	}
}
func TestIssuePackagePlanSearchLiteralUnicodeLimitsAndSecrets(t *testing.T) {
	s := packageTestService(t)
	pkg := packageTestPlan(t, s)
	pkg.Notes = "A literal [.*] matches itself.\nPrefix İΣ SQL-cache-café suffix\npassword=credential-only-marker\nAuthorization: Bearer auth-only-marker"
	pkg.Summary = "An adjacent sql-cache-known password=private-secret"
	pkg.Sessions = []issuePackageSession{}
	packageSearchSave(t, s, pkg)
	if got := packageSearch(t, s, "[.*]"); len(got.Matches) != 1 || got.Matches[0].Field != "Plan notes" {
		t.Fatalf("query was interpreted as expression: %+v", got)
	}
	if got := packageSearch(t, s, "SQL-CACHE-CAFÉ"); len(got.Matches) != 1 || !strings.Contains(got.Matches[0].Snippet, "SQL-cache-café") {
		t.Fatalf("Unicode snippet changed: %+v", got)
	}
	for _, query := range []string{"credential-only-marker", "auth-only-marker", "private-secret"} {
		if got := packageSearch(t, s, query); len(got.Matches) != 0 {
			t.Fatalf("credential was searchable: %+v", got)
		}
	}
	for _, m := range packageSearch(t, s, "sql-cache-known").Matches {
		if strings.Contains(m.Snippet, "private-secret") {
			t.Fatal("snippet exposed adjacent credential")
		}
	}
	if result := packageSearch(t, s, "   "); result.Matches == nil || len(result.Matches) != 0 {
		t.Fatal("empty search should return empty array")
	}
	if _, err := s.searchPlans(context.Background(), strings.Repeat("x", 201)); err == nil {
		t.Fatal("oversized query accepted")
	}
	if _, err := s.searchPlans(context.Background(), "bad\x00query"); err == nil {
		t.Fatal("NUL query accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.searchPlans(ctx, "query"); err == nil {
		t.Fatal("cancelled search ignored")
	}
	search := issuePackageSearch{ctx: context.Background(), needle: "match", out: issuePackageSearchResponse{Matches: []issuePackageSearchMatch{}}}
	for i := 0; i < issuePackageSearchMatchLimit+2; i++ {
		search.add(issuePackageSearchMatch{}, "field", "match")
	}
	if len(search.out.Matches) != issuePackageSearchMatchLimit || !search.out.Truncated {
		t.Fatal("match limit not enforced")
	}
	search = issuePackageSearch{ctx: context.Background(), bytes: issuePackageSearchByteLimit}
	if search.reserve(1) || !search.out.Truncated {
		t.Fatal("reading budget not enforced")
	}
}
func TestIssuePackagePlanSearchExcludesEvidenceAndEnvironment(t *testing.T) {
	s := packageTestService(t)
	pkg := packageTestStart(t, s, packageTestPlan(t, s))
	// Directly replace the immutable stored test snapshot so metadata omitted by
	// the search can be checked independently from evidence validation rules.
	pkg.Sessions[0].Environment.Configuration = "sql-cache-environment-secret"
	pkg.Sessions[0].Evidence = []issuePackageEvidence{{Description: "sql-cache-evidence-secret", Metadata: json.RawMessage(`{"token":"sql-cache-evidence-secret"}`)}}
	s.mu.Lock()
	s.packages[pkg.ID] = pkg
	s.mu.Unlock()
	for _, query := range []string{"sql-cache-environment-secret", "sql-cache-evidence-secret"} {
		if got := packageSearch(t, s, query); len(got.Matches) != 0 {
			t.Fatalf("searched private connection/evidence metadata: %+v", got)
		}
	}
}
func TestIssuePackagePlanSearchHTTP(t *testing.T) {
	p := clusterWorkspaceTestPanel(t)
	p.issuePackages = packageTestService(t)
	pkg := packageTestPlan(t, p.issuePackages)
	body, _ := json.Marshal(issuePackageRequest{Action: "plan-search", Query: "Project visibility"})
	r := httptest.NewRequest("POST", "http://localhost/api/issue-packages", strings.NewReader(string(body)))
	r.RemoteAddr = "127.0.0.1:1"
	r.Header.Set("Origin", "http://localhost")
	w := httptest.NewRecorder()
	p.handleIssuePackages(w, r)
	if w.Code != 200 {
		t.Fatalf("search endpoint failed: %d %s", w.Code, w.Body.String())
	}
	var response issuePackageSearchResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || len(response.Matches) == 0 || response.Matches[0].PackageID != pkg.ID {
		t.Fatalf("invalid response: %s", w.Body.String())
	}
	stored, _ := p.issuePackages.get(pkg.ID)
	if stored.Revision != pkg.Revision {
		t.Fatal("search changed saved plan")
	}
}

func TestIssuePackagePlanSearchBaselineSurvivesDeletedSource(t *testing.T) {
	s := packageTestService(t)
	pkg := packageTestCandidate(t, s, packageTestCompletedBaseline(t, s))
	sourceID, ownerID := pkg.Sessions[0].ID, pkg.Sessions[1].ID
	baseline := pkg.Sessions[1].Baseline
	baseline.Conclusion = "sql-cache-baseline-conclusion"
	baseline.Cases[0].Steps[0].Instruction = "sql-cache-baseline-step"
	baseline.Results[0].Notes = "sql-cache-baseline-notes"
	baseline.Environment.Configuration = "sql-cache-baseline-private-configuration"
	baseline.Evidence[0].Name = "sql-cache-baseline-private-evidence"
	packageSearchSave(t, s, pkg)
	pkg = packageTestMutation(t, s, issuePackageRequest{Action: "delete-session", ID: pkg.ID, Revision: pkg.Revision, SessionID: sourceID, Confirm: "confirm"}, nil, nil, nil)
	for _, query := range []string{"sql-cache-baseline-conclusion", "sql-cache-baseline-step", "sql-cache-baseline-notes"} {
		got := packageSearch(t, s, query)
		if len(got.Matches) != 1 {
			t.Fatalf("baseline text not found: %+v", got)
		}
		m := got.Matches[0]
		if m.Kind != "session" || m.SessionID != ownerID || m.SourceID != sourceID || !strings.HasPrefix(m.Field, "Baseline ") {
			t.Fatalf("baseline match points at a deleted session: %+v", m)
		}
	}
	for _, query := range []string{"sql-cache-baseline-private-configuration", "sql-cache-baseline-private-evidence"} {
		if got := packageSearch(t, s, query); len(got.Matches) != 0 {
			t.Fatalf("baseline private metadata searched: %+v", got)
		}
	}
}
