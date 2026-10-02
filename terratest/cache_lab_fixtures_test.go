package test

import (
	"encoding/json"
	"github.com/brudnak/ha-rancher-rke2/internal/cachelab"

	"context"
	"database/sql"

	"os"
	"path/filepath"

	"testing"
)

func cacheTestService(t *testing.T) (*cachelab.Service, cachelab.Workspace) {
	t.Helper()
	s, err := cachelab.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.SaveWorkspace(context.Background(), cachelab.Request{Profile: cachelab.Workspace{Kind: "import", Name: "Test workspace"}})
	if err != nil {
		t.Fatal(err)
	}
	w := result.(cachelab.Workspace)
	if err = os.MkdirAll(filepath.Join(s.Root(), w.ID), 0700); err != nil {
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

func cacheTestSnapshot(t *testing.T, s *cachelab.Service, w cachelab.Workspace, source, name string) cachelab.Snapshot {
	t.Helper()
	path := filepath.Join(s.Root(), w.ID, cachelab.ID()+".partial")
	if err := cachelab.Vacuum(context.Background(), source, path); err != nil {
		t.Fatal(err)
	}
	record, err := s.AddSnapshot(context.Background(), w.ID, path, cachelab.Snapshot{Name: name, Source: w.URL, Method: "VACUUM INTO · fixture"})
	if err != nil {
		t.Fatal(err)
	}
	return record
}

// Rewrite persisted fixtures before use; integration tests never access service internals.
func cacheTestRewrite(t *testing.T, s *cachelab.Service, edit func(*cachelab.Library)) *cachelab.Service {
	t.Helper()
	lib := s.Library()
	edit(&lib)
	raw, err := json.Marshal(lib)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(s.Root(), "library.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	next, err := cachelab.New(s.Root(), cachelab.Options{ResolveCluster: func(id, host, path, ctx string) (string, error) { return id, nil }})
	if err != nil {
		t.Fatal(err)
	}
	return next
}
func cacheTestBegin(t *testing.T, s *cachelab.Service, workspace string) {
	t.Helper()
	if _, _, err := s.BeginJob(workspace, "Test capture"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.FinishJob(cachelab.Snapshot{}, context.Canceled) })
}
func cacheTestWorkspace(t *testing.T, s *cachelab.Service, id string) cachelab.Workspace {
	t.Helper()
	for _, w := range s.Library().Workspaces {
		if w.ID == id {
			return w
		}
	}
	t.Fatal("workspace missing")
	return cachelab.Workspace{}
}
