package cachelab

import (
	"context"
	"testing"
)

func TestLibrarySnapshotDoesNotExposeMutableState(t *testing.T) {
	s, w := cacheTestService(t)
	if _, err := s.Mutate(Request{Action: "folder", Workspace: w.ID, Name: "Evidence"}); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.library.Workspaces[0].Queries = []SavedQuery{{Name: "Original", SQL: "select 1"}}
	s.library.Snapshots = []Snapshot{{ID: ID(), Workspace: w.ID, Name: "Original"}}
	s.mu.Unlock()
	view := s.Library()
	view.Workspaces[0].Name = "Changed"
	view.Workspaces[0].Folders[0] = "Changed"
	view.Workspaces[0].Queries[0].Name = "Changed"
	view.Snapshots[0].Name = "Changed"
	next := s.Library()
	if next.Workspaces[0].Name != w.Name || next.Workspaces[0].Folders[0] != "Evidence" || next.Workspaces[0].Queries[0].Name != "Original" || next.Snapshots[0].Name != "Original" {
		t.Fatal("snapshot exposes mutable service state")
	}
}
func TestConfiguredWorkerShutdownPersistsFinalCapture(t *testing.T) {
	s, w := cacheTestService(t)
	ctx, _, err := s.BeginJob(w.ID, "Testing")
	if err != nil {
		t.Fatal(err)
	}
	s.workers.Stop()
	if ctx.Err() == nil {
		t.Fatal("capture did not inherit shutdown")
	}
	s.FinishJob(Snapshot{}, ctx.Err())
	if err = s.workers.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(s.Root())
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Library().Job.Running || reopened.Library().Job.Error == "" {
		t.Fatal("capture completion was not persisted")
	}
}
