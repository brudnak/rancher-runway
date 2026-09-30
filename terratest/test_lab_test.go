package test

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const testLabFixtureSHA = "0123456789abcdef0123456789abcdef01234567"
const testLabFixtureConfig = "rancher:\n  host: example.test\n  adminToken: token-local:verysecretvalue\n  cleanup: true\n"

func testLabFixture(t *testing.T) *testLabService {
	t.Helper()
	s, err := newTestLabService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	secrets := map[string]string{}
	s.keychain = func(action, id, value string) (string, error) {
		switch action {
		case "write":
			secrets[id] = value
		case "delete":
			delete(secrets, id)
		case "read":
			if v, ok := secrets[id]; ok {
				return v, nil
			}
			return "", fmt.Errorf("missing credential")
		}
		return "", nil
	}
	s.library.Catalog = testLabCatalog{SHA: testLabFixtureSHA, Ref: "main", Entries: []testLabEntry{{ID: "validation/example::TestSuite", Package: "validation/example", Suite: "TestSuite", Constraint: "validation && !extended"}, {ID: "validation/example::TestSuite/TestFirst", Package: "validation/example", Suite: "TestSuite", Test: "TestFirst", Constraint: "validation && !extended"}, {ID: "validation/example::TestSuite/TestSecond", Package: "validation/example", Suite: "TestSuite", Test: "TestSecond", Constraint: "validation && !extended"}}}
	return s
}
func testLabFixtureRequest() testLabRequest {
	return testLabRequest{SHA: testLabFixtureSHA, Selection: []string{"validation/example::TestSuite/TestFirst"}, Tags: "validation", Timeout: 1, Config: testLabFixtureConfig, Name: "Fixture"}
}
func TestTestLabExactSelectors(t *testing.T) {
	s := testLabFixture(t)
	req := testLabFixtureRequest()
	commands, err := s.commandsLocked(req)
	if err != nil {
		t.Fatal(err)
	}
	if commands[0].Pattern != "^TestSuite$/^(TestFirst)$" {
		t.Fatal(commands)
	}
	req.Selection = append(req.Selection, "validation/example::TestSuite/TestSecond")
	commands, err = s.commandsLocked(req)
	if err != nil || len(commands) != 1 || commands[0].Pattern != "^TestSuite$/^(TestFirst|TestSecond)$" {
		t.Fatalf("%v %v", commands, err)
	}
	req.Selection = append(req.Selection, "validation/example::TestSuite")
	commands, err = s.commandsLocked(req)
	if err != nil || len(commands) != 1 || commands[0].Pattern != "^TestSuite$" {
		t.Fatalf("%v %v", commands, err)
	}
	for _, tags := range []string{"extended", "validation,extended", "validation;echo nope", ""} {
		req.Tags = tags
		if _, err = s.commandsLocked(req); err == nil {
			t.Fatalf("accepted excluded tags %q", tags)
		}
	}
	req = testLabFixtureRequest()
	req.SHA = strings.Repeat("a", 40)
	if _, err = s.commandsLocked(req); err == nil {
		t.Fatal("accepted stale revision")
	}
	req = testLabFixtureRequest()
	req.Selection = []string{"../../outside"}
	if _, err = s.commandsLocked(req); err == nil {
		t.Fatal("accepted noncatalog selection")
	}
}
func TestTestLabDiscoveryDoesNotExecute(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "validation", "example")
	os.MkdirAll(dir, 0700)
	source := `//go:build validation
package example
import("testing";"github.com/stretchr/testify/suite")
type Example struct{suite.Suite}
func init(){panic("must never run during discovery")}
func(e *Example) TestFirst(){}
func(e *Example) SetupSuite(){}
func TestExample(t *testing.T){suite.Run(t,new(Example))}
func TestOther(t *testing.T){}
`
	if err := os.WriteFile(filepath.Join(dir, "example_test.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	entries, err := testLabDiscover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatal(entries)
	}
	found := false
	for _, e := range entries {
		if e.ID == "validation/example::TestExample/TestFirst" {
			found = true
			if e.Constraint != "validation" || e.Line == 0 {
				t.Fatal(e)
			}
		}
	}
	if !found {
		t.Fatal(entries)
	}
}
func testLabArchive(t *testing.T, file string, files map[string]string) {
	t.Helper()
	f, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	w := tar.NewWriter(gz)
	for name, body := range files {
		if err = w.WriteHeader(&tar.Header{Name: "tests-fixture/" + name, Mode: 0600, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	w.Close()
	gz.Close()
	f.Close()
}
func TestTestLabArchiveBoundaries(t *testing.T) {
	for _, name := range []string{"../escape", "/absolute", "safe/../../escape", "safe\\escape"} {
		t.Run(name, func(t *testing.T) {
			f := filepath.Join(t.TempDir(), "archive.tar.gz")
			testLabArchive(t, f, map[string]string{name: "content"})
			if err := testLabExtract(f, t.TempDir()); err == nil {
				t.Fatalf("accepted %q", name)
			}
		})
	}
}
func TestTestLabSavedConfigAndRestart(t *testing.T) {
	s := testLabFixture(t)
	req := testLabFixtureRequest()
	req.Action = "save-plan"
	req.Remember = true
	value, err := s.mutate(req)
	if err != nil {
		t.Fatal(err)
	}
	plan := value.(testLabPlan)
	data, err := os.ReadFile(filepath.Join(s.root, "library.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "verysecretvalue") || strings.Contains(string(data), "adminToken") {
		t.Fatal("configuration leaked into manifest")
	}
	value, err = s.mutate(testLabRequest{Action: "load-plan", ID: plan.ID})
	if err != nil || value.(map[string]any)["config"] != testLabFixtureConfig {
		t.Fatalf("%v %v", value, err)
	}
	if _, err = s.mutate(testLabRequest{Action: "delete-plan", ID: plan.ID}); err == nil {
		t.Fatal("deleted without confirmation")
	}
	id := cacheLabID()
	s.library.Runs = []testLabRun{{ID: id, Status: "running"}}
	s.persistLocked()
	work := filepath.Join(s.root, "work-"+id)
	os.MkdirAll(work, 0700)
	os.WriteFile(filepath.Join(work, "cattle-config.yml"), []byte(testLabFixtureConfig), 0600)
	restored, err := newTestLabService(s.root)
	if err != nil {
		t.Fatal(err)
	}
	if restored.library.Runs[0].Status != "interrupted" {
		t.Fatal("run not marked interrupted")
	}
	if _, err = os.Stat(work); !os.IsNotExist(err) {
		t.Fatal("interrupted configuration not removed")
	}
}
func TestTestLabRedactionAcrossWrites(t *testing.T) {
	s := testLabFixture(t)
	s.library.Runs = []testLabRun{{ID: "run", Results: []testLabResult{}}}
	_, secrets, err := testLabConfig(testLabFixtureConfig)
	if err != nil {
		t.Fatal(err)
	}
	out := &testLabOutput{s: s, id: "run", secrets: secrets, seen: map[string]bool{}}
	_, _ = out.Write([]byte("token-local:very"))
	_, _ = out.Write([]byte("secretvalue\n"))
	event := map[string]any{"Action": "output", "Output": "Authorization: token-local:verysecretvalue\n"}
	data, _ := json.Marshal(event)
	out.Write(append(data, '\n'))
	out.flush()
	if strings.Contains(s.logs["run"], "verysecretvalue") || !strings.Contains(s.logs["run"], "[redacted]") {
		t.Fatal(s.logs)
	}
	for _, raw := range []string{testLabFixtureConfig + "---\nother: document\n", "- list\n", strings.Replace(testLabFixtureConfig, "example.test", "https://example.test", 1)} {
		if _, _, err = testLabConfig(raw); err == nil {
			t.Fatal("accepted invalid config")
		}
	}
}

type testLabRoundTrip func(*http.Request) (*http.Response, error)

func (f testLabRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestTestLabGitHubDevicePollingAndDisconnect(t *testing.T) {
	s := testLabFixture(t)
	s.library.GitHub.ClientID = "Iv1.fixture"
	calls := 0
	s.client = &http.Client{Transport: testLabRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		body := `{"error":"slow_down"}`
		if strings.HasSuffix(r.URL.Path, "device/code") {
			body = `{"device_code":"secret-device","user_code":"ABCD-1234","verification_uri":"https://github.com/login/device","expires_in":900,"interval":5}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})}
	value, err := s.githubAction(context.Background(), testLabRequest{Action: "github-start"})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(value)
	if strings.Contains(string(data), "secret-device") {
		t.Fatal("device secret exposed")
	}
	s.githubAction(context.Background(), testLabRequest{Action: "github-poll"})
	if calls != 1 {
		t.Fatal("polled before interval")
	}
	s.device.NextPoll = time.Now().Add(-time.Second)
	s.githubAction(context.Background(), testLabRequest{Action: "github-poll"})
	if s.device.Interval != 10 {
		t.Fatal("slow_down not respected")
	}
	s.library.GitHub.Login = "fixture"
	s.library.GitHub.Repository = "fixture/private-tests"
	s.library.GitHub.CreatedRepository = true
	s.credential.AccessToken = "fake"
	if _, err = s.githubAction(context.Background(), testLabRequest{Action: "github-disconnect"}); err == nil {
		t.Fatal("disconnected without confirmation")
	}
	before := calls
	if _, err = s.githubAction(context.Background(), testLabRequest{Action: "github-disconnect", Confirm: typedConfirmationPhrase}); err != nil {
		t.Fatal(err)
	}
	if calls != before {
		t.Fatal("disconnect mutated GitHub")
	}
	if s.library.GitHub.Login != "" || s.credential.AccessToken != "" {
		t.Fatal("credentials retained")
	}
	if _, err = s.githubAction(context.Background(), testLabRequest{Action: "github-delete", Repository: "fixture/private-tests"}); err == nil {
		t.Fatal("repository deletion supported")
	}
}
func TestTestLabRepositoryAccessIsNotDeletion(t *testing.T) {
	s := testLabFixture(t)
	s.credential.AccessToken = "fake"
	s.library.GitHub.Repository = "fixture/private-tests"
	s.client = &http.Client{Transport: testLabRoundTrip(func(r *http.Request) (*http.Response, error) {
		code := 404
		body := `{}`
		if r.URL.Path == "/user" {
			code = 200
			body = `{"login":"fixture"}`
		}
		if r.Method != "GET" {
			t.Fatalf("unexpected mutation %s", r.Method)
		}
		return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})}
	_, err := s.githubAction(context.Background(), testLabRequest{Action: "github-status"})
	if err != nil {
		t.Fatal(err)
	}
	if s.library.GitHub.RepositoryStatus != "Repository unavailable" {
		t.Fatal(s.library.GitHub)
	}
}

// Optional read-only check against a previously downloaded public archive. This
// does not fetch the network and never executes any rancher/tests code.
func TestTestLabUpstreamArchive(t *testing.T) {
	archive := os.Getenv("RUNWAY_TEST_CATALOG_FIXTURE")
	if archive == "" {
		t.Skip("no public archive fixture supplied")
	}
	dir := t.TempDir()
	if err := testLabExtract(archive, dir); err != nil {
		t.Fatal(err)
	}
	entries, err := testLabDiscover(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if e.ID == "validation/configmaps::TestConfigMapTestSuite/TestSteveGeneratedFields" {
			found = true
			content, err := testLabReadSourceFile(archive, e.File)
			if err != nil || !strings.Contains(content, e.Test+"(") {
				t.Fatalf("could not read the catalog's source file %s: %v", e.File, err)
			}
		}
	}
	if !found || len(entries) < 500 {
		t.Fatalf("incomplete discovery: %d entries, known method %v", len(entries), found)
	}
	t.Logf("Indexed %d catalog entries from public source without executing tests", len(entries))
	index, err := testLabIndexSource(dir, testLabFixtureSHA)
	if err != nil {
		t.Fatal(err)
	}
	result := index.check(testLabFixtureConfig+"tenantRanchers:\n  clients: []\n", map[string]bool{"validation/hostedtenant/rbac": true})
	found = false
	for _, finding := range result.Findings {
		if finding.Path == "tenantRanchers.clients" && strings.HasSuffix(finding.File, "hosted_tenant_rbac_test.go") {
			found = true
		}
	}
	if !found || len(index.Docs) < 50 {
		t.Fatalf("incomplete source workspace: %d guides, assertion found %v", len(index.Docs), found)
	}
	t.Logf("Read %d validation READMEs and detected the upstream hosted-tenant config assertion", len(index.Docs))

	if out := os.Getenv("RUNWAY_TEST_CATALOG_OUTPUT"); out != "" {
		data, _ := json.Marshal(testLabCatalog{Ref: "main", SHA: "02e6d36fe92251f46d1bfc3cdb513df4318cb98b", Entries: entries, GoVersion: "1.26.0", FetchedAt: time.Now()})
		if err = os.WriteFile(out, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func testLabWaitRun(t *testing.T, s *testLabService, id string) testLabRun {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		run := *s.runLocked(id)
		s.mu.Unlock()
		if run.Status != "running" {
			return run
		}
		time.Sleep(50 * time.Millisecond)
	}
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
	t.Fatal("local fixture timed out")
	return testLabRun{}
}

func TestTestLabLocalRunnerIsolationAndNoMatch(t *testing.T) {
	if testing.Short() {
		t.Skip("local Go subprocess fixture")
	}
	s := testLabFixture(t)
	archive := filepath.Join(s.root, testLabFixtureSHA+".tar.gz")
	source := `package example
import("os";"testing";"fmt")
func TestSuite(t *testing.T){
 t.Run("TestFirst",func(t *testing.T){
  if os.Getenv("RUNWAY_TEST_PARENT_SECRET")!=""{t.Fatal("inherited parent secret")}
  b,err:=os.ReadFile(os.Getenv("CATTLE_TEST_CONFIG"));if err!=nil{t.Fatal(err)}
  if len(b)==0{t.Fatal("missing config")};fmt.Println("token-local:verysecretvalue")
 })
 t.Run("TestSecond",func(t *testing.T){t.Fatal("unselected method executed")})
}
`
	files := map[string]string{"go.mod": "module github.com/rancher/tests\n\ngo 1.26.0\n", "validation/example/example_test.go": source}
	if runtime.GOOS == "darwin" {
		// Exercise the same native headers/runtime that Rancher's dependencies
		// need. Pure Go fixtures miss SDK failures in the isolated environment.
		files["validation/example/native.go"] = `package example
/*
#include <stdlib.h>
#include <errno.h>
#include <pthread.h>
static int nativeValue(void) {
 errno = 0;
 pthread_t self = pthread_self();
 (void) self;
 return abs(-7);
}
*/
import "C"
func nativeValue() int { return int(C.nativeValue()) }
`
		files["validation/example/example_test.go"] = strings.Replace(source, `fmt.Println("token-local:verysecretvalue")`, `fmt.Println("token-local:verysecretvalue")
  if nativeValue()!=7 { t.Fatal("native code returned the wrong result") }
  if os.Getenv("SDKROOT")=="" { t.Fatal("missing SDK") }
  if os.Getenv("CGO_CFLAGS")!="" { t.Fatal("inherited compiler flags") }`, 1)
		// The runner must discover its SDK, not depend on launch-shell settings.
		t.Setenv("SDKROOT", filepath.Join(t.TempDir(), "missing-sdk"))
		t.Setenv("CC", "/missing/host/compiler")
		t.Setenv("CXX", "/missing/host/compiler++")
		t.Setenv("CGO_CFLAGS", "-invalid-host-flag")
	}
	testLabArchive(t, archive, files)
	t.Setenv("RUNWAY_TEST_PARENT_SECRET", "fixture-sensitive-value")
	req := testLabFixtureRequest()
	if _, err := s.startRun(req); err == nil {
		t.Fatal("executed without review confirmation")
	}
	req.Confirm = typedConfirmationPhrase
	value, err := s.startRun(req)
	if err != nil {
		t.Fatal(err)
	}
	id := value.(testLabRun).ID
	run := testLabWaitRun(t, s, id)
	if run.Status != "passed" {
		t.Fatalf("%+v\n%s", run, s.logs[id])
	}
	if strings.Contains(s.logs[id], "verysecretvalue") {
		t.Fatal("credential appeared in persisted output")
	}
	if _, err = os.Stat(filepath.Join(s.root, "work-"+id)); !os.IsNotExist(err) {
		t.Fatal("ephemeral config was retained")
	}
	s.mu.Lock()
	s.library.Catalog.Entries = append(s.library.Catalog.Entries, testLabEntry{ID: "validation/example::TestMissing", Package: "validation/example", Suite: "TestMissing"})
	s.mu.Unlock()
	req.Selection = []string{"validation/example::TestMissing"}
	value, err = s.startRun(req)
	if err != nil {
		t.Fatal(err)
	}
	run = testLabWaitRun(t, s, value.(testLabRun).ID)
	if run.Status != "failed" || !strings.Contains(run.Error, "did not execute") {
		t.Fatalf("empty selection counted as pass: %+v", run)
	}
}

func TestTestLabLocalRunnerMissingSDK(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS SDK preflight")
	}
	t.Setenv("DEVELOPER_DIR", filepath.Join(t.TempDir(), "missing-xcode"))
	s := testLabFixture(t)
	testLabArchive(t, filepath.Join(s.root, testLabFixtureSHA+".tar.gz"), map[string]string{
		"go.mod":                             "module github.com/rancher/tests\n\ngo 1.26.0\n",
		"validation/example/example_test.go": "package example\nimport \"testing\"\nfunc TestSuite(t *testing.T) { t.Fatal(\"must not execute\") }\n",
	})
	req := testLabFixtureRequest()
	req.Confirm = typedConfirmationPhrase
	value, err := s.startRun(req)
	if err != nil {
		t.Fatal(err)
	}
	id := value.(testLabRun).ID
	run := testLabWaitRun(t, s, id)
	if run.Status != "failed" || !strings.Contains(run.Error, "SDK") || !strings.Contains(run.Error, "xcode-select") || !strings.Contains(run.Error, "No tests ran") {
		t.Fatalf("missing SDK did not produce actionable guidance: %+v", run)
	}
	if len(run.Results) != 0 || strings.Contains(s.logs[id], "[runway] validation/example") {
		t.Fatalf("started Go despite failed toolchain check: %s", s.logs[id])
	}
	if _, err := os.Stat(filepath.Join(s.root, "work-"+id)); !os.IsNotExist(err) {
		t.Fatal("ephemeral configuration was retained after preflight failure")
	}
}

func TestTestLabRuntimeDataExcludedFromSource(t *testing.T) {
	goPath, err := resolveLocalToolPath("go")
	if err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	root := filepath.Join(repo, "terratest", "automation-output", "control-panel", "test-lab")
	cache := filepath.Join(root, "go-modules", "example.invalid", "legacy@v1.0.0", "source.go")
	if err := os.MkdirAll(filepath.Dir(cache), 0700); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{
		filepath.Join(repo, "go.mod"):  "module example.invalid/runway\n\ngo 1.20\n",
		filepath.Join(repo, "main.go"): "package runway\n",
		cache:                          "package legacy\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Existing downloads must remain usable, including dependencies with no
	// go.mod of their own. Initializing/reopening Test Lab isolates that data.
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := newTestLabService(root); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(goPath, "list", "-buildvcs=false", "./...")
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "GOFLAGS=-buildvcs=false", "GOWORK=off", "GOENV=off", "GOTOOLCHAIN=local", "GOPROXY=off", "GOVCS=*:off")
		output, err := cmd.CombinedOutput()
		if err != nil || strings.TrimSpace(string(output)) != "example.invalid/runway" {
			t.Fatalf("runtime data entered Go package discovery: %v\n%s", err, output)
		}
		if content, err := os.ReadFile(cache); err != nil || string(content) != "package legacy\n" {
			t.Fatal("existing cache was modified")
		}
	}
}

func TestTestLabAPIProtectsLocalMetadata(t *testing.T) {
	s := testLabFixture(t)
	p := &localControlPanel{testLab: s, token: "test-panel-token"}
	for _, action := range []string{"logs", "config-library", "config-load", "config-save", "config-delete", "folder-delete", "config-export", "config-import", "config-import-preview", "source-docs", "source-file", "preflight"} {
		for _, method := range []string{"GET", "POST"} {
			body, _ := json.Marshal(testLabRequest{Action: action, ID: "fixture"})
			req := httptest.NewRequest(method, "http://localhost/api/test-lab", strings.NewReader(string(body)))
			req.Header.Set("Origin", "https://untrusted.example")
			w := httptest.NewRecorder()
			p.handleTestLab(w, req)
			if w.Code != 401 {
				t.Fatalf("%s %s accepted unauthenticated origin: %d", method, action, w.Code)
			}
		}
	}
	req := httptest.NewRequest("GET", "http://localhost/api/test-lab", nil)
	req.Header.Set("X-Control-Panel-Token", "test-panel-token")
	w := httptest.NewRecorder()
	p.handleTestLab(w, req)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("read failed: %d %s", w.Code, w.Body.String())
	}
}
func TestTestLabCreateRepositoryIsPrivateAndExplicit(t *testing.T) {
	s := testLabFixture(t)
	s.library.GitHub.Login = "fixture"
	s.credential.AccessToken = "fake"
	calls := 0
	s.client = &http.Client{Transport: testLabRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/user/repos" {
			t.Fatalf("unexpected mutation %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil || body["private"] != true {
			t.Fatal("repository was not forced private")
		}
		return &http.Response{StatusCode: 201, Body: io.NopCloser(strings.NewReader(`{"full_name":"fixture/test-runner","private":true}`)), Header: http.Header{}}, nil
	})}
	req := testLabRequest{Action: "github-create", Name: "test-runner"}
	if _, err := s.githubAction(context.Background(), req); err == nil || calls != 0 {
		t.Fatal("created without explicit confirmation")
	}
	req.Confirm = typedConfirmationPhrase
	if _, err := s.githubAction(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if !s.library.GitHub.CreatedRepository {
		t.Fatal("dedicated repository origin not recorded")
	}
}
