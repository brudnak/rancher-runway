package test

import (
	"errors"
	"testing"

	"github.com/brudnak/ha-rancher-rke2/internal/history"
)

// Inject failures at the storage boundary without depending on permissions or disks.
type failingHistoryStore struct {
	history.Store
	writeErr, listErr, deleteErr error
	writes, deletes              int
}

func (s *failingHistoryStore) Write(id string, event history.Event) error {
	s.writes++
	if s.writeErr != nil {
		return s.writeErr
	}
	return s.Store.Write(id, event)
}
func (s *failingHistoryStore) List(id string) ([]history.Event, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.Store.List(id)
}
func (s *failingHistoryStore) Delete(id string) error {
	s.deletes++
	if s.deleteErr != nil {
		return s.deleteErr
	}
	return s.Store.Delete(id)
}

func TestClusterHistoryStorageFailuresRemainRetryable(t *testing.T) {
	p := clusterWorkspaceTestPanel(t)
	if err := p.mutateClusterHistory(clusterHistoryRequest{Action: "create", Name: "Stored history"}); err != nil {
		t.Fatal(err)
	}
	rows, err := p.labClusterCandidates()
	if err != nil || len(rows) != 1 {
		t.Fatalf("clusters: %+v %v", rows, err)
	}
	id := rows[0].ID
	failure := errors.New("storage unavailable")
	store := &failingHistoryStore{Store: history.FileStore{Root: t.TempDir()}, writeErr: failure}
	p.historyStore = store
	payload := map[string]string{"version": "v2.15.3"}
	if err := p.saveClusterHistory(id, "test", payload); !errors.Is(err, failure) {
		t.Fatalf("write failure lost: %v", err)
	}
	store.writeErr = nil
	if err := p.saveClusterHistory(id, "test", payload); err != nil {
		t.Fatal(err)
	}
	if err := p.saveClusterHistory(id, "test", payload); err != nil {
		t.Fatal(err)
	}
	if store.writes != 2 {
		t.Fatalf("failed write was deduplicated or successful write repeated: %d", store.writes)
	}
	store.listErr = failure
	if _, err := p.readClusterHistory(id); !errors.Is(err, failure) {
		t.Fatalf("read failure lost: %v", err)
	}
	store.listErr = nil
	events, err := p.readClusterHistory(id)
	if err != nil || len(events) != 1 {
		t.Fatalf("retry lost observation: %+v %v", events, err)
	}
	if err := p.mutateClusterHistory(clusterHistoryRequest{Action: "delete", ID: id}); err != nil {
		t.Fatal(err)
	}
	purge := clusterHistoryRequest{Action: "purge", ID: id, ConfirmID: id, ConfirmPhrase: "DELETE PERMANENTLY"}
	store.deleteErr = failure
	if err := p.mutateClusterHistory(purge); !errors.Is(err, failure) {
		t.Fatalf("delete failure lost: %v", err)
	}
	if p.clusterWorkspaces[id].Purged {
		t.Fatal("failed purge marked complete")
	}
	events, err = p.readClusterHistory(id)
	if err != nil || len(events) != 1 {
		t.Fatalf("failed delete lost history: %+v %v", events, err)
	}
	store.deleteErr = nil
	if err := p.mutateClusterHistory(purge); err != nil {
		t.Fatal(err)
	}
	if store.deletes != 2 {
		t.Fatalf("purge did not retry: %d", store.deletes)
	}
	if err := p.saveClusterHistory(id, "late", payload); err != nil {
		t.Fatal(err)
	}
	events, err = p.readClusterHistory(id)
	if err != nil || len(events) != 0 {
		t.Fatalf("late observation recreated purged history: %+v %v", events, err)
	}
}
