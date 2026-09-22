package test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func radarConfig() issueRadarConfig {
	return issueRadarConfig{Repo: "rancher/rancher", Label: "area/frameworks", Users: []string{"alice", "bob"}}
}

func radarResponse(t *testing.T, value any) []byte {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return append([]byte("HTTP/2.0 200 OK\r\nContent-Type: application/json\r\n\r\n"), payload...)
}

func radarRunner(t *testing.T, callback func(*url.URL) ([]byte, error)) imageLookupCommandRunner {
	t.Helper()
	return func(ctx context.Context, command string, args, env []string, limit int64) ([]byte, error) {
		if command != "gh" || len(args) < 12 || args[0] != "api" {
			t.Fatalf("unexpected command: %s %v", command, args)
		}
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "--hostname github.com --method GET") || limit != 2<<20 {
			t.Fatalf("request must be fixed-host, read-only and bounded: %v", args)
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("GitHub call requires a timeout")
		}
		for _, value := range env {
			if strings.HasPrefix(value, "GH_DEBUG=") {
				t.Fatal("debug output must be disabled")
			}
		}
		endpoint := args[len(args)-3]
		parsed, err := url.Parse(endpoint)
		if err != nil {
			t.Fatal(err)
		}
		return callback(parsed)
	}
}

func TestIssueRadarNormalizesScope(t *testing.T) {
	config := radarConfig()
	config.Repo = " rancher/rancher "
	config.Label = " area/frameworks, kind/bug, AREA/frameworks "
	config.Users = []string{" @ALICE ", "Bob", "alice", ""}
	got, err := normalizeIssueRadarConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	if got.Repo != "rancher/rancher" || got.Label != "area/frameworks,kind/bug" || !reflect.DeepEqual(got.Users, []string{"alice", "bob"}) {
		t.Fatalf("unexpected normalized config: %+v", got)
	}
	for _, mutate := range []func(*issueRadarConfig){
		func(c *issueRadarConfig) { c.Repo = "https://evil.invalid/repo" },
		func(c *issueRadarConfig) { c.Repo = "owner/../repo" },
		func(c *issueRadarConfig) { c.Repo = "owner/repo?other=yes" },
		func(c *issueRadarConfig) { c.Label = "" },
		func(c *issueRadarConfig) { c.Label = "label\nnext" },
		func(c *issueRadarConfig) { c.Users = nil },
		func(c *issueRadarConfig) { c.Users = []string{"user:someone"} },
		func(c *issueRadarConfig) { c.Users = []string{"bad--login"} },
		func(c *issueRadarConfig) { c.Users = []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"} },
		func(c *issueRadarConfig) { c.Milestone = "v1"; c.NoMilestone = true },
	} {
		invalid := radarConfig()
		mutate(&invalid)
		if _, err := normalizeIssueRadarConfig(invalid); err == nil {
			t.Fatalf("accepted invalid scope: %+v", invalid)
		}
	}
}

func TestIssueRadarFetchPaginatesMilestonesAndIssues(t *testing.T) {
	config := radarConfig()
	config.Milestone = "v2.14.0"
	var calls []string
	service := &issueRadarService{runCommand: radarRunner(t, func(endpoint *url.URL) ([]byte, error) {
		calls = append(calls, endpoint.String())
		page := endpoint.Query().Get("page")
		if strings.HasSuffix(endpoint.Path, "/milestones") {
			if page == "1" {
				return radarResponse(t, make([]issueRadarMilestone, 100)), nil
			}
			return radarResponse(t, []issueRadarMilestone{{Title: config.Milestone, Number: 19}}), nil
		}
		query := endpoint.Query()
		if query.Get("milestone") != "19" || query.Get("labels") != "area/frameworks" || query.Get("state") != "open" || query.Get("assignee") != "" {
			t.Fatalf("incorrect issue scope (must include unassigned): %v", query)
		}
		if page == "1" {
			items := make([]issueRadarIssue, 100)
			for i := range items {
				items[i] = issueRadarIssue{Number: i + 1, PullRequest: &struct{}{}}
			}
			items[0].PullRequest = nil
			return radarResponse(t, items), nil
		}
		return radarResponse(t, []issueRadarIssue{{Number: 1}, {Number: 101, URL: "javascript:bad"}}), nil
	})}
	issues, err := service.issues(context.Background(), config, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 4 || len(issues) != 2 || issues[1].Number != 101 {
		t.Fatalf("pagination/deduplication failed: %d calls, %+v", len(calls), issues)
	}
	if issues[1].URL != "https://github.com/rancher/rancher/issues/101" {
		t.Fatal("issue link must be canonical")
	}
}

func TestIssueRadarMilestoneScopeAndMissingTitle(t *testing.T) {
	for _, noMilestone := range []bool{false, true} {
		config := radarConfig()
		config.NoMilestone = noMilestone
		service := &issueRadarService{runCommand: radarRunner(t, func(endpoint *url.URL) ([]byte, error) {
			want := ""
			if noMilestone {
				want = "none"
			}
			if endpoint.Query().Get("milestone") != want {
				t.Fatalf("wrong milestone scope: %v", endpoint)
			}
			return radarResponse(t, []issueRadarIssue{}), nil
		})}
		if _, err := service.issues(context.Background(), config, "", 0); err != nil {
			t.Fatal(err)
		}
	}
	config := radarConfig()
	config.Milestone = "not-a-milestone"
	service := &issueRadarService{runCommand: radarRunner(t, func(endpoint *url.URL) ([]byte, error) { return radarResponse(t, []issueRadarMilestone{}), nil })}
	if _, err := service.issues(context.Background(), config, "", 0); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing milestone must not broaden scope: %v", err)
	}
}

func TestIssueRadarFailsClosedOnPaginationError(t *testing.T) {
	service := &issueRadarService{runCommand: radarRunner(t, func(endpoint *url.URL) ([]byte, error) {
		if endpoint.Query().Get("page") == "1" {
			return radarResponse(t, make([]issueRadarIssue, 100)), nil
		}
		return []byte("HTTP/2.0 403 Forbidden\r\n\r\n{\"message\":\"private-token-do-not-leak\"}"), errors.New("failed")
	})}
	items, err := service.issues(context.Background(), radarConfig(), "", 0)
	if items != nil || err == nil || strings.Contains(err.Error(), "private-token") {
		t.Fatalf("must not return partial data or raw errors: %+v %v", items, err)
	}
}

func TestIssueRadarPaginationLimitNeverReturnsAPartialSnapshot(t *testing.T) {
	calls := 0
	service := &issueRadarService{runCommand: radarRunner(t, func(endpoint *url.URL) ([]byte, error) {
		calls++
		return radarResponse(t, make([]issueRadarIssue, 100)), nil
	})}
	items, err := service.issues(context.Background(), radarConfig(), "", 0)
	if calls != 100 || items != nil || err == nil || !strings.Contains(err.Error(), "fetch limit") {
		t.Fatalf("expected a bounded failure, got %d calls, %d items, %v", calls, len(items), err)
	}
}

func TestIssueRadarCancellationStopsTheRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	service := &issueRadarService{runCommand: radarRunner(t, func(endpoint *url.URL) ([]byte, error) {
		return nil, context.Canceled
	})}
	_, err := service.issues(ctx, radarConfig(), "", 0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation must remain detectable: %v", err)
	}
}

func radarHTTPRequest(path, method string, body any) *http.Request {
	data, _ := json.Marshal(body)
	request := httptest.NewRequest(method, path, strings.NewReader(string(data)))
	request.Header.Set("X-Control-Panel-Token", "test-token")
	return request
}

func TestIssueRadarHandlersAreReadOnlyAndGuarded(t *testing.T) {
	panel := &localControlPanel{token: "test-token", issueRadar: &issueRadarService{runCommand: radarRunner(t, func(endpoint *url.URL) ([]byte, error) {
		return radarResponse(t, []issueRadarIssue{{Number: 42, Title: "Read only"}}), nil
	})}}
	handler := panel.handler()
	for _, path := range []string{"/api/issue-radar", "/api/issue-radar/milestones", "/api/issue-radar/history", "/api/issue-radar/save"} {
		for _, method := range []string{"GET", "POST"} {
			request := radarHTTPRequest(path, method, radarConfig())
			status := http.StatusMethodNotAllowed
			if method == "POST" {
				request.Header.Del("X-Control-Panel-Token")
				status = http.StatusForbidden
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != status {
				t.Fatalf("%s %s: got %d, want %d", method, path, recorder.Code, status)
			}
		}
	}
	request := radarHTTPRequest("/api/issue-radar", "POST", radarConfig())
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "Read only") || recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unexpected response: %d %s", recorder.Code, recorder.Body)
	}
	request = radarHTTPRequest("/api/issue-radar", "POST", map[string]any{"repo": "rancher/rancher", "token": "must-not-accept"})
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatal("unknown fields must be rejected")
	}
}

func TestIssueRadarHistoryKeepsFailuresDistinctAndIgnoresMilestone(t *testing.T) {
	config := radarConfig()
	config.Milestone = "v2.14.0"
	panel := &localControlPanel{token: "test-token", issueRadar: &issueRadarService{runCommand: radarRunner(t, func(endpoint *url.URL) ([]byte, error) {
		query := endpoint.Query()
		if query.Get("milestone") != "" || query.Get("state") != "closed" || query.Get("sort") != "updated" || query.Get("labels") != config.Label {
			t.Fatalf("unexpected history scope: %v", query)
		}
		if query.Get("assignee") == "bob" {
			return []byte("HTTP/2.0 422 Unprocessable Entity\r\n\r\n{}"), errors.New("failed")
		}
		items := make([]issueRadarIssue, 60)
		for i := range items {
			items[i] = issueRadarIssue{Number: i + 1}
		}
		return radarResponse(t, items), nil
	})}}
	request := radarHTTPRequest("/api/issue-radar/history", "POST", map[string]any{"config": config, "limit": 30})
	recorder := httptest.NewRecorder()
	panel.handleIssueRadarHistory(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatal(recorder.Body.String())
	}
	var result struct {
		History  map[string][]issueRadarIssue
		Warnings []string
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.History["alice"]) != 30 || len(result.Warnings) != 1 {
		t.Fatalf("incorrect history result: %+v", result)
	}
	if _, exists := result.History["bob"]; exists {
		t.Fatal("failed history must not pretend to be an empty successful result")
	}
}

func TestIssueRadarSavesLargeReportPrivatelyWithoutReplacingExistingFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	panel := &localControlPanel{token: "test-token"}
	content := "# Assignment review\n" + strings.Repeat("A long report of real issues\n", 5000)
	for i := 0; i < 2; i++ {
		recorder := httptest.NewRecorder()
		panel.handleIssueRadarSave(recorder, radarHTTPRequest("/api/issue-radar/save", "POST", map[string]string{"content": content}))
		if recorder.Code != http.StatusOK {
			t.Fatalf("save failed: %d %s", recorder.Code, recorder.Body)
		}
		var saved struct{ Filename, Path string }
		if err := json.Unmarshal(recorder.Body.Bytes(), &saved); err != nil {
			t.Fatal(err)
		}
		want := "issue-radar-report.md"
		if i > 0 {
			want = fmt.Sprintf("issue-radar-report (%d).md", i)
		}
		if saved.Filename != want || filepath.Base(saved.Path) != want {
			t.Fatalf("unexpected filename: %+v", saved)
		}
		data, err := os.ReadFile(saved.Path)
		if err != nil || string(data) != content {
			t.Fatalf("saved content mismatch: %v", err)
		}
		info, err := os.Stat(saved.Path)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("report must be private: %v", err)
		}
	}
}
