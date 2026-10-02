package panelsession

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestPanelStateURLStripsTokenAndUsesStateEndpoint(t *testing.T) {
	got, err := StateURL("http://127.0.0.1:1234/?token=secret#frag")
	if err != nil {
		t.Fatalf("panelStateURL failed: %v", err)
	}
	if want := "http://127.0.0.1:1234/api/state"; got != want {
		t.Fatalf("StateURL() = %q, want %q", got, want)
	}
}

func TestPanelSessionHealthyUsesStateEndpoint(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	if !Healthy(server.URL + "/?token=secret") {
		t.Fatal("expected panel session to be healthy")
	}
	if gotPath != "/api/state" {
		t.Fatalf("expected health check to use /api/state, got %q", gotPath)
	}
}

func testStore(t *testing.T) (Store, Record) {
	t.Helper()
	root := t.TempDir()
	return Store{Path: filepath.Join(root, "sessions", "session.json")}, Record{PID: 42, URL: "http://127.0.0.1:1/?token=test", RepoRoot: root, SessionID: "current"}
}

func TestRecordRoundTripAndOwnership(t *testing.T) {
	store, record := testStore(t)
	if err := store.Write(record); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("permissions = %o", info.Mode().Perm())
	}
	for _, owner := range []struct {
		pid int
		id  string
	}{{41, "current"}, {42, "previous"}} {
		store.RemoveOwned(owner.pid, owner.id)
		got, ok, err := store.Read()
		if err != nil || !ok || got != record {
			t.Fatalf("another owner lost the session: %+v %v %v", got, ok, err)
		}
	}
	store.RemoveOwned(record.PID, record.SessionID)
	if _, ok, err := store.Read(); err != nil || ok {
		t.Fatalf("owned record survived: %v %v", ok, err)
	}
	store.RemoveOwned(record.PID, record.SessionID)
}

func TestInvalidRecordsAreDiscarded(t *testing.T) {
	for _, data := range []string{"broken JSON", `null`, `{}`, `{"pid":0,"url":"http://localhost","repoRoot":"/tmp"}`, `{"pid":42,"url":" ","repoRoot":"/tmp"}`, `{"pid":42,"url":"http://localhost","repoRoot":" "}`} {
		t.Run(data, func(t *testing.T) {
			store, record := testStore(t)
			if err := store.Write(record); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(store.Path, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			if _, ok, err := store.Read(); err != nil || ok {
				t.Fatalf("invalid session accepted: %v %v", ok, err)
			}
			if _, err := os.Stat(store.Path); !os.IsNotExist(err) {
				t.Fatalf("invalid record retained: %v", err)
			}
		})
	}
}

func TestReadErrorsAreNotTreatedAsMissing(t *testing.T) {
	store := Store{Path: t.TempDir()}
	if _, ok, err := store.Read(); err == nil || ok {
		t.Fatalf("directory read must fail: %v %v", ok, err)
	}
	if err := store.Write(Record{}); err == nil {
		t.Fatal("writing over directory must fail")
	}
}

func TestSessionReuseBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name           string
		alive          bool
		status         int
		otherWorkspace bool
		reuse          bool
		retained       bool
	}{
		{"healthy", true, 200, false, true, true},
		{"dead process", false, 200, false, false, false},
		{"unhealthy endpoint", true, 503, false, false, false},
		{"different workspace", false, 503, true, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.URL.Path != "/api/state" || r.URL.RawQuery != "" {
					t.Errorf("unexpected probe: %s", r.URL)
				}
				w.WriteHeader(tc.status)
			}))
			defer server.Close()
			store, record := testStore(t)
			record.URL = server.URL + "/?token=private#fragment"
			if err := store.Write(record); err != nil {
				t.Fatal(err)
			}
			root := record.RepoRoot
			if tc.otherWorkspace {
				root = t.TempDir()
			}
			probes := 0
			got, ok, err := store.ExistingURL(root, func(pid int) bool {
				probes++
				if pid != record.PID {
					t.Errorf("wrong PID: %d", pid)
				}
				return tc.alive
			})
			if err != nil || ok != tc.reuse {
				t.Fatalf("reuse: %q %v %v", got, ok, err)
			}
			if tc.reuse && got != record.URL {
				t.Fatalf("launch URL changed: %s", got)
			}
			if !tc.reuse && got != "" {
				t.Fatalf("unexpected URL: %s", got)
			}
			if tc.otherWorkspace && probes != 0 {
				t.Fatal("probed another workspace")
			}
			if (!tc.alive || tc.otherWorkspace) && requests != 0 {
				t.Fatal("unnecessary HTTP probe")
			}
			if _, ok, err := store.Read(); err != nil || ok != tc.retained {
				t.Fatalf("retained: %v %v", ok, err)
			}
		})
	}
}

func TestHealthFailures(t *testing.T) {
	if Healthy("http://%zz") {
		t.Fatal("invalid URL reported healthy")
	}
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	server.Close()
	if Healthy(server.URL) {
		t.Fatal("unreachable server reported healthy")
	}
}

func TestSamePathNormalizesWorkspace(t *testing.T) {
	root := t.TempDir()
	if !SamePath(root, root+"/child/..") {
		t.Fatal("equivalent paths differ")
	}
	if SamePath(root, filepath.Join(root, "other")) {
		t.Fatal("different paths match")
	}
}
