package test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func indexedBuildReport(line, tag string) string {
	return fmt.Sprintf(`<!-- meta
version: %s
webhook: v0.12.1-rc.17
rancher_date: 2026-11-17T09:30:17Z
rancher_date_source: image
rancher_image: stgregistry.suse.com/rancher/rancher-server:%s
build_stream: prime-head
generated: 2026-11-17T10:16:52Z
-->
# Report for %s
`, tag, tag, line)
}

func TestInteractiveSetupBuildIndexBoundedAndCached(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	fail := false
	index := setupBuildIndex{client: &http.Client{Transport: rancherPlanRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		calls++
		failed := fail
		mu.Unlock()
		if failed {
			return nil, fmt.Errorf("offline")
		}
		var body string
		if req.URL.Path == "/brudnak/webhook-ranch-hand/main/README.md" {
			body = "### Prime heads\n"
			for _, minor := range []int{9, 16, 17, 15, 14, 13, 12} {
				body += fmt.Sprintf("| v2.%d | [open](reports/v2.%d/v2.%d.0-abcdef0-head.md) |\n", minor, minor, minor)
			}
			body += "### Alphas\n[ignore](reports/v2.99/v2.99.0-abcdef0-head.md)\n"
		} else {
			path := strings.TrimPrefix(req.URL.Path, "/brudnak/webhook-ranch-hand/main/")
			parts := setupReportPath.FindStringSubmatch(path)
			if parts == nil {
				t.Errorf("unexpected fetch: %s", req.URL)
				return nil, fmt.Errorf("bad path")
			}
			body = indexedBuildReport(parts[1], parts[2])
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}}
	got, err := index.latest(t.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Builds) != 7 || calls != 8 || got.Builds[0].Line != "2.17" || got.Builds[1].Line != "2.16" || got.Stale {
		t.Fatalf("wanted seven ordered lines in eight small requests: %+v, calls=%d", got, calls)
	}
	_, err = index.latest(t.Context(), false)
	if err != nil || calls != 8 {
		t.Fatalf("reopening fetched again: %v, calls=%d", err, calls)
	}
	mu.Lock()
	fail = true
	mu.Unlock()
	saved, err := index.latest(t.Context(), true)
	if err != nil || !saved.Stale || len(saved.Builds) != 7 {
		t.Fatalf("lost saved index during outage: %+v, %v", saved, err)
	}
}

func TestInteractiveSetupBuildIndexHistoryDoesNotInspectImages(t *testing.T) {
	calls := 0
	index := setupBuildIndex{client: &http.Client{Transport: rancherPlanRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if !strings.HasSuffix(req.URL.Path, "/reports/v2.17/README.md") {
			t.Fatalf("unexpected fetch %s", req.URL)
		}
		body := ""
		for i := 0; i < 240; i++ {
			body += fmt.Sprintf("| `v2.17.0-%07x-head` | `prime-head` | 2026-11-17 | Image built | Clean | `v0.12.1` | - | 2026-11-17 | [open](tag.md) |\n", i)
		}
		body += "| `v2.17.0-fffffff-head` | `prime-head` | 2026-09-01 | Image built | Clean | `v0.12.0` | - | 2026-11-17 | [open](tag.md) |\n"
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}}
	got, err := index.history(t.Context(), "2.17", false)
	if err != nil {
		t.Fatal(err)
	}
	page := filterSetupBuildIndex(got, 30, 0, 20, time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC))
	if calls != 1 || page.Total != 240 || len(page.Builds) != 20 || !page.More || page.NextOffset != 20 {
		t.Fatalf("history should use one index request, exclude old builds, and page results: %+v, calls=%d", page, calls)
	}
	if page.Builds[0].Reference != "" || page.Builds[0].DatePrecision != "day" {
		t.Fatal("history must not invent an image repository or a precise build time")
	}
	_, err = index.history(t.Context(), "2.17", false)
	if err != nil || calls != 1 {
		t.Fatal("history pagination should reuse the downloaded index")
	}
}

func TestInteractiveSetupBuildIndexOtherBuilds(t *testing.T) {
	const head = "v2.17.0-0000002-head"
	const source = "reports/v2.17/README.md"
	// Mixed dates, equal-day builds, and a head in the middle of the source.
	var body strings.Builder
	for i, day := range []string{"15", "17", "18", "17", "16", "13", "14"} {
		fmt.Fprintf(&body, "| `v2.17.0-%07x-head` | `prime-head` | 2026-11-%s | Image built | Clean | `v0.12.1` | - | 2026-11-18 | [open](tag.md) |\n", i, day)
	}
	s := &interactiveServer{buildIndex: setupBuildIndex{cache: map[string]setupIndexCacheEntry{
		source: {data: []byte(body.String()), at: time.Now()},
	}}}
	for _, page := range []struct {
		offset string
		want   []int
		more   bool
	}{{"0", []int{1, 3, 4, 0, 6}, true}, {"5", []int{5}, false}} {
		r := httptest.NewRequest("GET", "/?line=2.17&days=0&q=v0.12&limit=5&exclude="+head+"&offset="+page.offset, nil)
		w := httptest.NewRecorder()
		s.handleSetupBuildIndex(w, r)
		var got setupBuildIndexResponse
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || w.Code != 200 {
			t.Fatalf("history page %s: %s", page.offset, w.Body.String())
		}
		if got.More != page.more || got.Total != 6 || len(got.Builds) != len(page.want) {
			t.Fatalf("unexpected page: %+v", got)
		}
		for i, n := range page.want {
			if got.Builds[i].Version != fmt.Sprintf("v2.17.0-%07x-head", n) {
				t.Fatalf("unexpected ordering: %+v", got.Builds)
			}
		}
	}
}

func TestInteractiveSetupBuildIndexReportValidation(t *testing.T) {
	path := "reports/v2.17/v2.17.0-abcdef0-head.md"
	valid := indexedBuildReport("2.17", "v2.17.0-abcdef0-head")
	build, err := parseSetupBuildReport(path, []byte(valid))
	if err != nil || build.Created != "2026-11-17T09:30:17Z" || build.Webhook != "v0.12.1-rc.17" {
		t.Fatalf("invalid metadata: %+v, %v", build, err)
	}
	for _, invalid := range []string{
		strings.ReplaceAll(valid, "stgregistry.suse.com", "untrusted.example"),
		strings.ReplaceAll(valid, "rancher_date_source: image", "rancher_date_source: source"),
		strings.ReplaceAll(valid, "version: v2.17.0-abcdef0-head", "version: v2.16.0-abcdef0-head"),
		strings.ReplaceAll(valid, "2026-11-17T09:30:17Z", "not-a-date"),
	} {
		if _, err := parseSetupBuildReport(path, []byte(invalid)); err == nil {
			t.Fatal("accepted unverified report identity or date")
		}
	}
	if _, err := parseSetupBuildReport("reports/v2.16/v2.17.0-abcdef0-head.md", []byte(valid)); err == nil {
		t.Fatal("accepted a report from another release line")
	}
	index := setupBuildIndex{}
	if _, _, err := index.report(context.Background(), "../../secrets", false); err == nil {
		t.Fatal("accepted arbitrary report path")
	}
}

func TestInteractiveSetupBuildIndexRouteValidatesBeforeNetwork(t *testing.T) {
	mux := http.NewServeMux()
	(&interactiveServer{token: "fixture"}).registerHandlersAt(mux, nil, "/setup-editor")
	for _, query := range []string{"catalog=1&days=-1", "catalog=1&offset=-1", "catalog=1&line=../../README", "catalog=1&days=all", "catalog=1&limit=21", "catalog=1&limit=0", "report=../../README"} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest("GET", "/setup-editor/api/rancher-builds?token=fixture&"+query, nil))
		if response.Code != http.StatusBadRequest {
			t.Errorf("%s returned %d", query, response.Code)
		}
	}
}
