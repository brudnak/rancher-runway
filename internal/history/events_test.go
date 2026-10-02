package history_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/brudnak/ha-rancher-rke2/internal/history"
)

func TestEventRoundTripPreservesPayloadAndChronology(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "observations")
	at := time.Date(2026, 10, 1, 12, 0, 0, 123, time.UTC)
	early := history.Event{ID: "early", At: at, Kind: "installed", Data: json.RawMessage(`{"version":"2.15.3-head","digest":"sha256:exact"}`)}
	late := history.Event{ID: "late", At: at.Add(time.Second), Kind: "upgrade", Data: json.RawMessage(`{"from":"2.15.3-head","to":"head"}`)}
	for _, event := range []history.Event{late, early} {
		if err := history.WriteEvent(dir, event); err != nil {
			t.Fatal(err)
		}
	}
	events, err := history.ReadEvents(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events", len(events))
	}
	for i, want := range []history.Event{early, late} {
		got := events[i]
		if got.ID != want.ID || !got.At.Equal(want.At) || got.Kind != want.Kind {
			t.Fatalf("event metadata changed: %+v", got)
		}
		var actual, expected any
		if err := json.Unmarshal(got.Data, &actual); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(want.Data, &expected); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(actual, expected) {
			t.Fatalf("payload changed: %s", got.Data)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".event-") {
			t.Fatalf("temporary file leaked: %s", entry.Name())
		}
		info, err := entry.Info()
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("event permissions: %v", info.Mode())
		}
	}
}

func TestReadEventsEmptyIncompleteAndCorrupt(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "observations")
	events, err := history.ReadEvents(dir)
	if err != nil || events == nil || len(events) != 0 {
		t.Fatalf("missing directory: %v %v", events, err)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".event-incomplete"), []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "nested.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if events, err = history.ReadEvents(dir); err != nil || len(events) != 0 {
		t.Fatalf("incomplete writes should be ignored: %v %v", events, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "corrupt.json"), []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = history.ReadEvents(dir); err == nil || !strings.Contains(err.Error(), "history event could not be read") {
		t.Fatalf("corruption not reported: %v", err)
	}
}

func TestFailedWriteCanRetry(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "observations")
	if err := os.WriteFile(dir, []byte("obstruction"), 0600); err != nil {
		t.Fatal(err)
	}
	event := history.Event{ID: "retry", At: time.Now().UTC(), Kind: "installed", Data: json.RawMessage(`{"version":"head"}`)}
	if err := history.WriteEvent(dir, event); err == nil {
		t.Fatal("write unexpectedly succeeded")
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := history.WriteEvent(dir, event); err != nil {
		t.Fatal(err)
	}
	events, err := history.ReadEvents(dir)
	if err != nil || len(events) != 1 || events[0].ID != event.ID {
		t.Fatalf("retry lost observation: %v %v", events, err)
	}
}
