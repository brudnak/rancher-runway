package test

import (
	"context"
	"errors"
	"github.com/brudnak/ha-rancher-rke2/internal/cachelab"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPanelHistoryProbeDoesNotBlockDiscovery(t *testing.T) {
	p := clusterWorkspaceTestPanel(t)
	t.Cleanup(func() { p.workers.Stop(); p.workers.Wait(context.Background()) })
	blocked := make(chan struct{})
	started := make(chan struct{})
	p.historyDiscovery.snapshotWith(&p.workers, func() struct{} { close(started); <-blocked; return struct{}{} })
	<-started
	done := make(chan struct{})
	p.clusterDiscovery.snapshotWith(&p.workers, func() panelClusterState { close(done); return panelClusterState{Items: []clusterView{{ID: "fresh"}}} })
	select {
	case <-done:
	case <-time.After(time.Second):
		close(blocked)
		t.Fatal("history blocked discovery")
	}
	close(blocked)
}
func TestCacheJobShutdownWaitsForFinalRecord(t *testing.T) {
	p := clusterWorkspaceTestPanel(t)
	s, err := cachelab.New(t.TempDir(), cachelab.Options{Workers: &p.workers})
	if err != nil {
		t.Fatal(err)
	}
	value, err := s.SaveWorkspace(context.Background(), cachelab.Request{Profile: cachelab.Workspace{Name: "Saved", Kind: "import"}})
	if err != nil {
		t.Fatal(err)
	}
	id := value.(cachelab.Workspace).ID
	ctx, _, err := s.BeginJob(id, "Testing")
	if err != nil {
		t.Fatal(err)
	}
	p.workers.Stop()
	if ctx.Err() == nil {
		t.Fatal("capture did not inherit cancellation")
	}
	s.FinishJob(cachelab.Snapshot{}, ctx.Err())
	if err = p.workers.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	reopened, err := cachelab.New(s.Root())
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Library().Job.Running || reopened.Library().Job.Error == "" {
		t.Fatal("final capture state missing")
	}
}

func TestPanelShutdownTimeoutKeepsSessionUntilWorkersFinish(t *testing.T) {
	p := clusterWorkspaceTestPanel(t)
	p.server = &http.Server{}
	path := panelSessionPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("session"), 0600); err != nil {
		t.Fatal(err)
	}
	_, done, err := p.workers.Begin()
	if err != nil {
		t.Fatal(err)
	}
	server := &ControlPanelServer{panel: p}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err = server.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unexpected shutdown result: %v", err)
	}
	if _, err = os.Stat(path); err != nil {
		t.Fatal("session removed before workers finished")
	}
	done()
	if err = server.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("session not removed after drain: %v", err)
	}
}
