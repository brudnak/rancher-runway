package history_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/brudnak/ha-rancher-rke2/internal/history"
)

func TestFileStoreLegacyCompatibilityAndIsolation(t *testing.T) {
	files := history.FileStore{Root: filepath.Join(t.TempDir(), "history")}
	var store history.Store = files
	stream := "cluster/../../outside"
	early := history.Event{ID: "early", At: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), Kind: "discovery", Data: json.RawMessage(`{"version":"head"}`)}
	late := early
	late.ID = "late"
	late.At = early.At.Add(time.Second)
	// Populate using the old API, then reopen through the interface.
	if err := history.WriteEvent(files.Dir(stream), late); err != nil {
		t.Fatal(err)
	}
	if err := store.Write(stream, early); err != nil {
		t.Fatal(err)
	}
	if err := store.Write("other", early); err != nil {
		t.Fatal(err)
	}
	reopened := history.FileStore{Root: files.Root}
	events, err := reopened.List(stream)
	if err != nil || len(events) != 2 || events[0].ID != "early" || events[1].ID != "late" {
		t.Fatalf("legacy history lost: %+v %v", events, err)
	}
	if filepath.Dir(files.Dir(stream)) != files.Root {
		t.Fatal("stream escaped root")
	}
	// Reads and writes cannot alias caller-owned payload memory.
	events[0].Data[0] = 'X'
	early.Data[0] = 'X'
	events, err = store.List(stream)
	if err != nil || !json.Valid(events[0].Data) {
		t.Fatalf("payload alias: %+v %v", events, err)
	}
	for i := 0; i < 2; i++ {
		if err := store.Delete(stream); err != nil {
			t.Fatal(err)
		}
	}
	events, err = store.List(stream)
	if err != nil || events == nil || len(events) != 0 {
		t.Fatalf("deleted history: %+v %v", events, err)
	}
	events, err = store.List("other")
	if err != nil || len(events) != 1 {
		t.Fatalf("another stream deleted: %+v %v", events, err)
	}
}

func TestFileStoreErrorsAndRetry(t *testing.T) {
	files := history.FileStore{Root: filepath.Join(t.TempDir(), "history")}
	var store history.Store = files
	if err := os.WriteFile(files.Root, []byte("obstruction"), 0600); err != nil {
		t.Fatal(err)
	}
	event := history.Event{ID: "retry", At: time.Now().UTC(), Kind: "test", Data: json.RawMessage(`{}`)}
	if err := store.Write("a", event); err == nil {
		t.Fatal("write error swallowed")
	}
	if _, err := store.List("a"); err == nil {
		t.Fatal("read error swallowed")
	}
	if err := store.Delete("a"); err == nil {
		t.Fatal("delete error swallowed")
	}
	if err := os.Remove(files.Root); err != nil {
		t.Fatal(err)
	}
	if err := store.Write("a", event); err != nil {
		t.Fatal(err)
	}
	// Existing timestamp/ID replacement behavior remains compatible.
	event.Kind = "updated"
	if err := store.Write("a", event); err != nil {
		t.Fatal(err)
	}
	events, err := store.List("a")
	if err != nil || len(events) != 1 || events[0].Kind != "updated" {
		t.Fatalf("retry/replacement: %+v %v", events, err)
	}
	if err := os.WriteFile(filepath.Join(files.Dir("a"), "corrupt.json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.List("a"); err == nil {
		t.Fatal("corrupt history hidden")
	}
}

// Baseline for evaluating another backend; setup is excluded from the measured read.
func BenchmarkFileStoreList(b *testing.B) {
	for _, count := range []int{100, 1000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			store := history.FileStore{Root: b.TempDir()}
			at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
			for i := 0; i < count; i++ {
				event := history.Event{ID: fmt.Sprint(i), At: at.Add(time.Duration(i) * time.Second), Kind: "deployment", Data: json.RawMessage(`{"version":"v2.15.3","digest":"sha256:example"}`)}
				if err := store.Write("cluster", event); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				events, err := store.List("cluster")
				if err != nil || len(events) != count {
					b.Fatalf("read: %d %v", len(events), err)
				}
			}
		})
	}
}
