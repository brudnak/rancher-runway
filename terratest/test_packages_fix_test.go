package test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestTestPackagePullRequestLinksAreParsedStrictly(t *testing.T) {
	for raw, want := range map[string]string{
		"https://github.com/rancher/rancher/pull/500":             "https://github.com/rancher/rancher/pull/500",
		" https://github.com/rancher/rancher/pull/500/ ":          "https://github.com/rancher/rancher/pull/500",
		"https://github.com/rancher/rancher/pull/500/files":       "https://github.com/rancher/rancher/pull/500",
		"https://github.com/rancher/rancher/pull/500/commits":     "https://github.com/rancher/rancher/pull/500",
		"https://github.com/rancher/rancher/issues/500":           "",
		"http://github.com/rancher/rancher/pull/500":              "",
		"https://github.com/rancher/rancher/pull/0":               "",
		"https://github.com/rancher/rancher/pull/500?diff=1":      "",
		"https://github.com/rancher/rancher/pull/500#top":         "",
		"https://user:secret@github.com/rancher/rancher/pull/500": "",
		"https://github.evil.test/rancher/rancher/pull/500":       "",
		"https://github.com/rancher/rancher/pull/500/../501":      "",
		"https://github.com/-bad/rancher/pull/500":                "",
	} {
		reference, ok := parseTestPackagePullRequest(raw)
		if ok != (want != "") || (ok && reference.URL() != want) {
			t.Fatalf("%q parsed as %v %+v, want %q", raw, ok, reference, want)
		}
	}
	for value, ok := range map[string]bool{"": true, "abc1234": true, strings.Repeat("a", 40): true, "ABC1234": false, "abc123": false, strings.Repeat("a", 41): false, "xyz1234": false} {
		if packageCommitSHA(value) != ok {
			t.Fatalf("commit %q accepted=%v", value, !ok)
		}
	}
}

func TestTestPackageFixIssuesFollowClosingKeywords(t *testing.T) {
	body := "Fixes #12 and fixes #12 again.\nResolves rancher/webhook#7, closes: https://github.com/rancher/dashboard/issues/99\nSee #13 (not a closing keyword). fixed https://github.com/a/b/issues/1?ref=x"
	got := testPackageFixIssues("rancher", "rancher", body)
	want := []string{"https://github.com/rancher/rancher/issues/12", "https://github.com/rancher/webhook/issues/7", "https://github.com/rancher/dashboard/issues/99", "https://github.com/a/b/issues/1"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("issues %v, want %v", got, want)
	}
	if len(testPackageFixIssues("o", "r", strings.Repeat("fixes #1 ", 8000)+"fixes #2")) != 1 {
		t.Fatal("scan was not bounded to the leading description")
	}
	if len(testPackageFixIssues("o", "r", "")) != 0 {
		t.Fatal("empty body produced issues")
	}
}

type packageFixMock struct {
	pull     testPackageGitHubPull
	status   int
	requests []string
}

func testPackageFixMockPanel(t *testing.T, connected bool) (*localControlPanel, *packageFixMock) {
	t.Helper()
	service, err := newTestLabService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service.library.GitHub = testLabGitHub{Connected: connected}
	service.credential = testLabCredential{AccessToken: "mock-fix-token"}
	merged := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	mock := &packageFixMock{status: 200, pull: testPackageGitHubPull{Title: "  Invalidate cached\nproject lists  ", State: "closed", Merged: true, MergedAt: &merged, MergeCommitSHA: strings.Repeat("c", 40), Body: "Fixes #123\n\nAlso fixes rancher/webhook#7"}}
	mock.pull.Head.SHA = strings.ToUpper(strings.Repeat("a", 40))
	mock.pull.Head.Ref = "fix/project-cache"
	mock.pull.Base.Ref = "main"
	service.client = &http.Client{Transport: packageArchiveTransport(func(req *http.Request) (*http.Response, error) {
		mock.requests = append(mock.requests, req.Method+" "+req.URL.String())
		if req.URL.Scheme != "https" || req.URL.Host != "api.github.com" || req.Header.Get("Authorization") != "Bearer mock-fix-token" || req.Method != http.MethodGet {
			t.Fatalf("lookup escaped the trusted read-only API: %s %s", req.Method, req.URL)
		}
		raw, _ := json.Marshal(mock.pull)
		return &http.Response{StatusCode: mock.status, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(raw))}, nil
	})}
	return &localControlPanel{testLab: service}, mock
}

func TestTestPackageFixLookupIsExplicitReadOnlyAndBounded(t *testing.T) {
	p, mock := testPackageFixMockPanel(t, true)
	if _, err := p.testPackageFixLookup(context.Background(), testPackageRequest{FixURL: "https://github.com/rancher/rancher/issues/500"}); err == nil || len(mock.requests) != 0 {
		t.Fatal("non pull request link reached GitHub")
	}
	value, err := p.testPackageFixLookup(context.Background(), testPackageRequest{FixURL: "https://github.com/rancher/rancher/pull/500/files"})
	if err != nil {
		t.Fatal(err)
	}
	lookup := value.(map[string]any)["fix"].(testPackageFixLookup)
	if len(mock.requests) != 1 || mock.requests[0] != "GET https://api.github.com/repos/rancher/rancher/pulls/500" {
		t.Fatalf("requests %v", mock.requests)
	}
	if lookup.URL != "https://github.com/rancher/rancher/pull/500" || lookup.Repository != "rancher/rancher" || lookup.Number != 500 || lookup.Title != "Invalidate cached project lists" || lookup.State != "merged" || lookup.HeadSHA != strings.Repeat("a", 40) || lookup.MergeSHA != strings.Repeat("c", 40) || lookup.HeadRef != "fix/project-cache" || lookup.BaseRef != "main" || lookup.MergedAt == nil || lookup.FetchedAt.IsZero() {
		t.Fatalf("lookup %+v", lookup)
	}
	if lookup.Label != "rancher/rancher#500 · Invalidate cached project lists" || strings.Join(lookup.Issues, ",") != "https://github.com/rancher/rancher/issues/123,https://github.com/rancher/webhook/issues/7" {
		t.Fatalf("label %q issues %v", lookup.Label, lookup.Issues)
	}
	raw, _ := json.Marshal(lookup)
	if strings.Contains(string(raw), "Also fixes") || strings.Contains(string(raw), "mock-fix-token") {
		t.Fatal("lookup response carried the description or credentials")
	}
	mock.pull.Merged = false
	mock.pull.State = "open"
	mock.pull.Draft = true
	mock.pull.Title = strings.Repeat("t", 300)
	value, err = p.testPackageFixLookup(context.Background(), testPackageRequest{FixURL: "https://github.com/rancher/rancher/pull/500"})
	if err != nil {
		t.Fatal(err)
	}
	lookup = value.(map[string]any)["fix"].(testPackageFixLookup)
	if lookup.State != "open" || !lookup.Draft || lookup.MergeSHA != "" || lookup.MergedAt != nil || len(lookup.Title) != 240 || len(lookup.Label) > 500 {
		t.Fatalf("open draft lookup %+v", lookup)
	}
	mock.pull.Head.SHA = "not-a-commit"
	if _, err = p.testPackageFixLookup(context.Background(), testPackageRequest{FixURL: "https://github.com/rancher/rancher/pull/500"}); err == nil {
		t.Fatal("unexpected head commit accepted")
	}
	mock.pull.Head.SHA = strings.Repeat("a", 40)
	mock.status = 404
	if _, err = p.testPackageFixLookup(context.Background(), testPackageRequest{FixURL: "https://github.com/rancher/rancher/pull/500"}); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("404 not explained: %v", err)
	}
	disconnected, offline := testPackageFixMockPanel(t, false)
	if _, err = disconnected.testPackageFixLookup(context.Background(), testPackageRequest{FixURL: "https://github.com/rancher/rancher/pull/500"}); err == nil || !strings.Contains(err.Error(), "connect GitHub") || len(offline.requests) != 0 {
		t.Fatalf("disconnected lookup contacted GitHub or hid the reason: %v", err)
	}
}

func TestTestPackageSessionFreezesCommitUnderTest(t *testing.T) {
	s := packageTestService(t)
	pkg := packageTestUpdate(t, s, packageTestPlan(t, s), "https://github.com/rancher/rancher/pull/500", "rancher/rancher#500 · Invalidate cached project lists")
	e := testPackageEnvironment{ClusterName: "Candidate", RancherVersion: "v2.15.3-rc1", Source: "manual", RecordedAt: time.Now().UTC()}
	if _, err := s.mutate(testPackageRequest{Action: "start-session", ID: pkg.ID, Revision: pkg.Revision, Name: "Bad commit", Purpose: "validation", FixURL: pkg.FixURL, FixCommit: "release/v2.15"}, &e, nil, nil); err == nil {
		t.Fatal("non-commit value accepted as the commit under test")
	}
	pkg = packageTestMutation(t, s, testPackageRequest{Action: "start-session", ID: pkg.ID, Revision: pkg.Revision, Name: "Candidate 1", Purpose: "validation", FixURL: pkg.FixURL, FixTitle: pkg.FixTitle, FixCommit: " " + strings.ToUpper(strings.Repeat("a", 40)) + " "}, &e, nil, nil)
	session := pkg.Sessions[0]
	if session.FixCommit != strings.Repeat("a", 40) {
		t.Fatalf("commit not normalized: %q", session.FixCommit)
	}
	if detail := testPackageJourneyFor(pkg).Stages[3].Detail; detail != "In progress · Candidate 1 · commit aaaaaaaaaa" {
		t.Fatalf("journey detail %q", detail)
	}
	markdown, err := testPackageMarkdown(pkg, session.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markdown, "**Fix under test:** rancher/rancher\\#500 · Invalidate cached project lists · https://github.com/rancher/rancher/pull/500 · commit "+strings.Repeat("a", 40)+" (recorded when this session started)") {
		t.Fatalf("report omitted the commit under test:\n%s", markdown)
	}
	bundle, err := s.buildBundle(pkg.ID, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	bundle.Package.Sessions[0].FixCommit = "zz"
	if _, _, err = validateTestPackageBundle(&bundle); err == nil {
		t.Fatal("bundle with an invalid commit accepted")
	}
	bundle.Package.Sessions[0].FixCommit = session.FixCommit
	imported, err := s.importPackage(bundle.Package, map[string][]byte{})
	if err != nil || imported.Sessions[0].FixCommit != session.FixCommit {
		t.Fatalf("portable bundle lost the commit under test: %v", err)
	}
}
