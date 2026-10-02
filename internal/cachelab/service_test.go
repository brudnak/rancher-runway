package cachelab

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/binary"

	"fmt"
	"io"

	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func cacheTestService(t *testing.T) (*Service, Workspace) {
	t.Helper()
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.SaveWorkspace(context.Background(), Request{Profile: Workspace{Kind: "import", Name: "Test workspace"}})
	if err != nil {
		t.Fatal(err)
	}
	w := result.(Workspace)
	if err = os.MkdirAll(filepath.Join(s.root, w.ID), 0700); err != nil {
		t.Fatal(err)
	}
	return s, w
}
func cacheTestDB(t *testing.T, statements ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range statements {
		if _, err = db.Exec(statement); err != nil {
			db.Close()
			t.Fatalf("%s: %v", statement, err)
		}
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}
func cacheTestSnapshot(t *testing.T, s *Service, w Workspace, source, name string) Snapshot {
	t.Helper()
	path := filepath.Join(s.root, w.ID, ID()+".partial")
	if err := Vacuum(context.Background(), source, path); err != nil {
		t.Fatal(err)
	}
	record, err := s.AddSnapshot(context.Background(), w.ID, path, Snapshot{Name: name, Source: w.URL, Method: "VACUUM INTO · fixture"})
	if err != nil {
		t.Fatal(err)
	}
	return record
}
func TestCacheLabVacuumIncludesCommittedWALAndPreservesSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "live.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range []string{"PRAGMA journal_mode=WAL", "PRAGMA wal_autocheckpoint=0", "CREATE TABLE objects (id TEXT PRIMARY KEY, data TEXT)", `INSERT INTO objects VALUES ('a','{"spec":{"replicas":3}}')`} {
		if _, err = db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	copy := filepath.Join(t.TempDir(), "snapshot.db")
	if err = Vacuum(context.Background(), path, copy); err != nil {
		t.Fatal(err)
	}
	out, err := cacheLabOpen(copy)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	var count int
	if err = out.QueryRow("SELECT count(*) FROM objects").Scan(&count); err != nil || count != 1 {
		t.Fatalf("WAL commit missing: %d %v", count, err)
	}
	if _, err = out.Exec("DELETE FROM objects"); err == nil {
		t.Fatal("snapshot was writable")
	}
	if _, err = out.Exec("PRAGMA query_only=0; DELETE FROM objects"); err == nil {
		t.Fatal("source read-only open was bypassed")
	}
	if err = db.QueryRow("SELECT count(*) FROM objects").Scan(&count); err != nil || count != 1 {
		t.Fatal("live source changed")
	}
}
func TestCacheLabLibraryRoundTripAndSecretSeparation(t *testing.T) {
	s, w := cacheTestService(t)
	snap := cacheTestSnapshot(t, s, w, cacheTestDB(t, "CREATE TABLE records(id INTEGER PRIMARY KEY)", "INSERT INTO records VALUES(1)"), "Before")
	for _, req := range []Request{{Action: "folder", Workspace: w.ID, Name: "Provisioning"}, {Action: "snapshot", Workspace: w.ID, Snapshot: snap.ID, Name: "Baseline", Notes: "Waiting for agents", Folder: "Provisioning", Favorite: true}, {Action: "view", Workspace: w.ID, View: cacheLabView{Snapshot: snap.ID, Baseline: snap.ID, Mode: "sql", SQL: "SELECT * FROM records"}}, {Action: "save-query", Workspace: w.ID, Name: "All records", SQL: "SELECT * FROM records"}} {
		if _, err := s.Mutate(req); err != nil {
			t.Fatal(err)
		}
	}
	result, err := s.SaveWorkspace(context.Background(), Request{Profile: Workspace{Kind: "rancher", URL: "https://RANCHER.example.test/"}, Token: "token-private:do-not-persist"})
	if err != nil {
		t.Fatal(err)
	}
	live := result.(Workspace)
	if live.Name != "rancher.example.test" {
		t.Fatal(live.Name)
	}
	_, args, cleanup, err := s.connection(live.ID)
	if err != nil {
		t.Fatal(err)
	}
	config, _ := os.ReadFile(args[1])
	if !bytes.Contains(config, []byte("token-private")) {
		t.Fatal("temporary connection missing token")
	}
	info, _ := os.Stat(args[1])
	if info.Mode().Perm() != 0600 {
		t.Fatal("credentials have unsafe permissions")
	}
	cleanup()
	if _, err = os.Stat(args[1]); !os.IsNotExist(err) {
		t.Fatal("temporary credential survived cleanup")
	}
	manifest, _ := os.ReadFile(filepath.Join(s.root, "library.json"))
	if bytes.Contains(manifest, []byte("token-private")) {
		t.Fatal("credential leaked into library")
	}
	reopened, err := New(s.root)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.library.Snapshots[0].Notes != "Waiting for agents" || !reopened.library.Snapshots[0].Favorite {
		t.Fatal("snapshot state lost")
	}
	saved, _ := reopened.workspaceLocked(w.ID)
	if saved.View.SQL == "" || len(saved.Queries) != 1 || len(saved.Folders) != 1 {
		t.Fatal("workspace state lost")
	}
	disconnected, _ := reopened.workspaceLocked(live.ID)
	if disconnected.Connected {
		t.Fatal("session token claimed to survive restart")
	}
}
func TestCacheLabQueryBoundariesAndExactIntegers(t *testing.T) {
	s, w := cacheTestService(t)
	path := cacheTestDB(t, `CREATE TABLE "odd;table" (id INTEGER PRIMARY KEY, value TEXT)`, `INSERT INTO "odd;table" VALUES (9223372036854775807, 'hello; world')`)
	snap := cacheTestSnapshot(t, s, w, path, "SQL")
	for _, query := range []string{`SELECT * FROM "odd;table";`, `WITH values_cte AS (SELECT ';' AS value) SELECT * FROM values_cte`, `SELECT '--not a comment' AS value /* ; */`, `SELECT '[test]' -- harmless comment`} {
		if _, err := s.Inspect(context.Background(), Request{Action: "query", Snapshot: snap.ID, SQL: query}); err != nil {
			t.Fatalf("valid query %q: %v", query, err)
		}
	}
	for _, query := range []string{`DELETE FROM "odd;table"`, `SELECT 1; DELETE FROM "odd;table"`, `PRAGMA query_only=OFF`, `ATTACH DATABASE '/tmp/escaped.db' AS other`, `SELECT 1); VACUUM INTO '/tmp/escaped.db'; --`, `SELECT 1 /*`, `SELECT 'unterminated`} {
		if _, err := s.Inspect(context.Background(), Request{Action: "query", Snapshot: snap.ID, SQL: query}); err == nil {
			t.Fatalf("unsafe query accepted: %s", query)
		}
	}
	result, err := s.Inspect(context.Background(), Request{Action: "rows", Snapshot: snap.ID, Table: "odd;table"})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.(cacheLabRows).Rows[0][0]; got != "9223372036854775807" {
		t.Fatalf("integer precision lost: %#v", got)
	}
	if _, err = s.Inspect(context.Background(), Request{Action: "rows", Snapshot: snap.ID, Table: "odd;table", Sort: "id; DELETE"}); err == nil {
		t.Fatal("unknown sort accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = s.Inspect(ctx, Request{Action: "query", Snapshot: snap.ID, SQL: "SELECT 1"}); err == nil {
		t.Fatal("canceled query succeeded")
	}
}
func TestCacheLabCompareLogicalRecordsJSONAndNoise(t *testing.T) {
	s, w := cacheTestService(t)
	a := cacheTestSnapshot(t, s, w, cacheTestDB(t, `CREATE TABLE objects(id TEXT PRIMARY KEY, data TEXT)`, `INSERT INTO objects VALUES ('same','{"metadata":{"resourceVersion":"1"},"spec":{"replicas":1}}'),('changed','{"spec":{"replicas":1}}'),('gone','{}')`), "Before")
	b := cacheTestSnapshot(t, s, w, cacheTestDB(t, `CREATE TABLE objects(id TEXT PRIMARY KEY, data TEXT)`, `INSERT INTO objects VALUES ('same','{"spec":{"replicas":1},"metadata":{"resourceVersion":"2"}}'),('changed','{"spec":{"replicas":3}}'),('new','{}')`, `CREATE TABLE extra(id INTEGER PRIMARY KEY)`), "After")
	for _, ignore := range []bool{false, true} {
		value, err := s.Inspect(context.Background(), Request{Action: "diff", Snapshot: a.ID, Other: b.ID, Table: "objects", IgnoreVolatile: ignore})
		if err != nil {
			t.Fatal(err)
		}
		diff := value.(cacheLabDiff)
		want := 2
		if ignore {
			want = 1
		}
		if diff.Added != 1 || diff.Removed != 1 || diff.Changed != want {
			t.Fatalf("incorrect diff: %+v", diff)
		}
		found := false
		for _, c := range diff.Changes {
			for _, f := range c.Fields {
				found = found || f.Path == "/data/spec/replicas"
			}
		}
		if !found {
			t.Fatal("missing JSON field path")
		}
	}
	value, err := s.Inspect(context.Background(), Request{Action: "diff", Snapshot: a.ID, Other: b.ID, Table: "extra"})
	if err != nil {
		t.Fatal(err)
	}
	if value.(cacheLabDiff).AfterSQL == "" {
		t.Fatal("schema addition missing")
	}
	if cacheLabJSON(cacheLabCanonical(`{"a":1} trailing`, true)) != `"{\"a\":1} trailing"` {
		t.Fatal("invalid JSON was silently normalized")
	}
}
func TestCacheLabUnkeyedComparisonPreservesDuplicates(t *testing.T) {
	s, w := cacheTestService(t)
	a := cacheTestSnapshot(t, s, w, cacheTestDB(t, `CREATE TABLE things(name TEXT,value TEXT)`, `INSERT INTO things VALUES('same','a'),('same','a'),('other','b')`), "Before")
	b := cacheTestSnapshot(t, s, w, cacheTestDB(t, `CREATE TABLE things(name TEXT,value TEXT)`, `INSERT INTO things VALUES('same','a'),('other','b'),('new','c')`), "After")
	value, err := s.Inspect(context.Background(), Request{Action: "diff", Snapshot: a.ID, Other: b.ID, Table: "things"})
	if err != nil {
		t.Fatal(err)
	}
	diff := value.(cacheLabDiff)
	if diff.Added != 1 || diff.Removed != 1 || diff.Unchanged != 2 || len(diff.Warnings) == 0 {
		t.Fatalf("incorrect duplicate comparison: %+v", diff)
	}
}
func TestCacheLabCleanupConfirmsAndStaysWithinWorkspace(t *testing.T) {
	s, w := cacheTestService(t)
	source := cacheTestDB(t, "CREATE TABLE records(id INTEGER)")
	a := cacheTestSnapshot(t, s, w, source, "one")
	other, err := s.SaveWorkspace(context.Background(), Request{Profile: Workspace{Kind: "import", Name: "Other"}})
	if err != nil {
		t.Fatal(err)
	}
	w2 := other.(Workspace)
	os.MkdirAll(filepath.Join(s.root, w2.ID), 0700)
	b := cacheTestSnapshot(t, s, w2, source, "two")
	if _, err = s.Mutate(Request{Action: "delete-workspace", Workspace: w.ID, Confirm: "wrong"}); err == nil {
		t.Fatal("cleanup did not require confirmation")
	}
	if _, err = s.Mutate(Request{Action: "delete-snapshot", Workspace: w.ID, Snapshot: b.ID, Confirm: typedConfirmationPhrase}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.snapshotLocked(b.ID); err != nil {
		t.Fatal("deleted another workspace snapshot")
	}
	if _, err = s.Mutate(Request{Action: "delete-workspace", Workspace: w.ID, Confirm: typedConfirmationPhrase}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.snapshotLocked(a.ID); err == nil {
		t.Fatal("deleted snapshot remains")
	}
	if _, _, err = s.snapshotLocked(b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(source); err != nil {
		t.Fatal("original source was removed")
	}
	reopened, err := New(s.root)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.library.Snapshots) != 1 || len(reopened.library.Workspaces) != 1 {
		t.Fatal("cleanup did not persist")
	}
}
func TestCacheLabRecoversInterruptedCaptureAndDelete(t *testing.T) {
	s, w := cacheTestService(t)
	snap := cacheTestSnapshot(t, s, w, cacheTestDB(t, "CREATE TABLE x(a TEXT)"), "keep")
	_, path, _ := s.snapshotLocked(snap.ID)
	if err := os.Rename(path, path+".delete"); err != nil {
		t.Fatal(err)
	}
	partial := filepath.Join(s.root, w.ID, ID()+".partial")
	os.WriteFile(partial, []byte("incomplete"), 0600)
	s.library.Job = Job{Running: true, Workspace: w.ID}
	s.persistLocked()
	reopened, err := New(s.root)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.library.Job.Running || reopened.library.Job.Error == "" {
		t.Fatal("interrupted capture claimed success")
	}
	if _, _, err = reopened.snapshotLocked(snap.ID); err != nil {
		t.Fatal("interrupted cleanup did not roll back")
	}
	if _, err = os.Stat(partial); !os.IsNotExist(err) {
		t.Fatal("partial file survived restart")
	}
}

func TestCacheLabCaptureUsesHelperAndVerifiesPod(t *testing.T) {
	s, _ := cacheTestService(t)
	header := make([]byte, 64)
	copy(header, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1})
	binary.LittleEndian.PutUint16(header[16:], 2)
	binary.LittleEndian.PutUint16(header[18:], 62)
	binary.LittleEndian.PutUint32(header[20:], 1)
	binary.LittleEndian.PutUint16(header[52:], 64)
	helper := filepath.Join(t.TempDir(), "helper")
	os.WriteFile(helper, header, 0600)
	saved, err := s.SaveWorkspace(context.Background(), Request{Profile: Workspace{Kind: "rancher", URL: "https://rancher.example.test", HelperPath: helper, Pod: "rancher-0"}, Token: "test:secret"})
	if err != nil {
		t.Fatal(err)
	}
	w := saved.(Workspace)
	os.MkdirAll(filepath.Join(s.root, w.ID), 0700)
	source := cacheTestDB(t, "CREATE TABLE objects(id TEXT PRIMARY KEY)", "INSERT INTO objects VALUES('sample')")
	content, _ := os.ReadFile(source)
	podReads := 0
	cleaned := false
	s.runner = func(ctx context.Context, args []string, in io.Reader, out io.Writer) error {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "secret") {
			t.Fatal("token leaked into command args")
		}
		switch {
		case strings.Contains(joined, "get pods"):
			podReads++
			_, err := fmt.Fprint(out, `{"items":[{"metadata":{"name":"rancher-0","uid":"stable-uid"},"spec":{"containers":[{"name":"rancher","image":"rancher/rancher:v2.15.2"}]},"status":{"phase":"Running","containerStatuses":[{"name":"rancher","ready":true,"restartCount":0}]}}]}`)
			return err
		case strings.Contains(joined, "uname -m"):
			_, err := fmt.Fprint(out, "x86_64\n")
			return err
		case strings.Contains(joined, "runway-stage"):
			received, _ := io.ReadAll(in)
			if !bytes.Equal(received, header) {
				t.Fatal("helper upload wrong")
			}
			return nil
		case strings.Contains(joined, "runway-capture"):
			if !strings.Contains(joined, "/tmp/vai-snapshot.db") || !strings.Contains(joined, "mkdir \"$lock\"") {
				t.Fatal("snapshot isolation missing")
			}
			_, err := fmt.Fprint(out, base64.StdEncoding.EncodeToString(content))
			return err
		case strings.Contains(joined, "rm -f"):
			cleaned = true
			return nil
		}
		return fmt.Errorf("unexpected command")
	}
	result, err := s.capture(context.Background(), Request{Workspace: w.ID, Name: "Live fixture"}, ID())
	if err != nil {
		t.Fatal(err)
	}
	if result.PodUID != "stable-uid" || result.Tables != 1 || podReads != 2 || !cleaned {
		t.Fatalf("capture not verified: %+v", result)
	}
}
func TestCacheLabKubeconfigPinsContextAndSource(t *testing.T) {
	s, _ := cacheTestService(t)
	config := filepath.Join(t.TempDir(), "config")
	os.WriteFile(config, []byte("test fixture"), 0600)
	s.runner = func(ctx context.Context, args []string, in io.Reader, out io.Writer) error {
		if !strings.Contains(strings.Join(args, " "), "--context test-context config view --minify") {
			t.Fatal(args)
		}
		_, err := fmt.Fprint(out, `{"current-context":"test-context","clusters":[{"cluster":{"server":"https://rancher.example.test/k8s/clusters/local"}}]}`)
		return err
	}
	result, err := s.SaveWorkspace(context.Background(), Request{Profile: Workspace{Kind: "kubeconfig", Kubeconfig: config, Context: "test-context"}})
	if err != nil {
		t.Fatal(err)
	}
	w := result.(Workspace)
	if w.Name != "rancher.example.test" {
		t.Fatal(w)
	}
	_, args, cleanup, err := s.connection(w.ID)
	defer cleanup()
	if err != nil || args[3] != "test-context" {
		t.Fatalf("context not pinned: %v %v", args, err)
	}
}
func TestCacheLabCaptureLockAndTimeout(t *testing.T) {
	s, w := cacheTestService(t)
	ctx, _, err := s.BeginJob(w.ID, "Testing")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.BeginJob(w.ID, "Another"); err == nil {
		t.Fatal("concurrent capture accepted")
	}
	if _, err = s.Mutate(Request{Action: "delete-workspace", Workspace: w.ID, Confirm: typedConfirmationPhrase}); err == nil {
		t.Fatal("deleted workspace during capture")
	}
	if _, err = s.Mutate(Request{Action: "cancel"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("capture was not canceled")
	}
	s.FinishJob(Snapshot{}, cacheLabContextError(ctx.Err()))
	if s.library.Job.Error == "" || s.library.Job.Snapshot != "" {
		t.Fatal(s.library.Job)
	}
}

func TestCacheLabDiffStreamsBeyondPreviewRowLimit(t *testing.T) {
	s, w := cacheTestService(t)
	source := cacheTestDB(t, "CREATE TABLE objects(id INTEGER PRIMARY KEY,value TEXT)", "WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<51000) INSERT INTO objects SELECT x,'before' FROM n")
	a := cacheTestSnapshot(t, s, w, source, "Before")
	db, _ := sql.Open("sqlite", source)
	_, err := db.Exec("UPDATE objects SET value='after' WHERE id=51000")
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	b := cacheTestSnapshot(t, s, w, source, "After")
	value, err := s.Inspect(context.Background(), Request{Action: "diff", Snapshot: a.ID, Other: b.ID, Table: "objects"})
	if err != nil {
		t.Fatal(err)
	}
	diff := value.(cacheLabDiff)
	if diff.Changed != 1 || diff.Unchanged != 50999 || diff.BeforeRows != 51000 {
		t.Fatalf("large table was incompletely compared: %+v", diff)
	}
}

func TestCacheLabKubeconfigChangedServerIsRejected(t *testing.T) {
	s, _ := cacheTestService(t)
	s.runner = func(ctx context.Context, args []string, in io.Reader, out io.Writer) error {
		_, err := fmt.Fprint(out, `{"clusters":[{"cluster":{"server":"https://another.example.test"}}]}`)
		return err
	}
	_, err := s.pods(context.Background(), Workspace{Kind: "kubeconfig", URL: "https://original.example.test"}, []string{"--kubeconfig", "unused"})
	if err == nil || !strings.Contains(err.Error(), "different server") {
		t.Fatalf("source mismatch accepted: %v", err)
	}
}
func TestCacheLabFolderRenameAndViewPersistence(t *testing.T) {
	s, w := cacheTestService(t)
	snapshot := cacheTestSnapshot(t, s, w, cacheTestDB(t, "CREATE TABLE sample(id TEXT)"), "Snapshot")
	for _, req := range []Request{{Action: "folder", Workspace: w.ID, Name: "Before"}, {Action: "snapshot", Workspace: w.ID, Snapshot: snapshot.ID, Folder: "Before"}, {Action: "view", Workspace: w.ID, View: cacheLabView{Snapshot: snapshot.ID, Folder: "Before", CompareTable: "sample"}}, {Action: "rename-folder", Workspace: w.ID, Folder: "Before", Name: "Investigation"}} {
		if _, err := s.Mutate(req); err != nil {
			t.Fatal(err)
		}
	}
	reopened, err := New(s.root)
	if err != nil {
		t.Fatal(err)
	}
	saved, _ := reopened.workspaceLocked(w.ID)
	if saved.View.CompareTable != "sample" || saved.View.Folder != "Investigation" || reopened.library.Snapshots[0].Folder != "Investigation" {
		t.Fatal("renamed folder or comparison state lost")
	}
	if _, err := s.Mutate(Request{Action: "delete-folder", Workspace: w.ID, Folder: "Investigation", Confirm: typedConfirmationPhrase}); err != nil {
		t.Fatal(err)
	}
	if s.library.Snapshots[0].Folder != "" {
		t.Fatal("snapshot was not moved to Unfiled")
	}
	if _, _, err = s.snapshotLocked(snapshot.ID); err != nil {
		t.Fatal("folder removal deleted the file")
	}
	if strings.ContainsAny(FileLabel("https://rancher.example.com/../../notes\n"), "/\\\n") {
		t.Fatal("unsafe export filename")
	}
}
