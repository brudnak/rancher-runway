package test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDurableMigrationAndCleanup(t *testing.T) {
	root := t.TempDir()
	t.Setenv("RANCHER_RUNWAY_WORKSPACE", root)
	t.Setenv(runIDEnv, "")
	old := filepath.Join(automationOutputDir(), "control-panel", "cluster-history", "cluster", "event.json")
	os.MkdirAll(filepath.Dir(old), 0700)
	os.WriteFile(old, []byte(`{"digest":"sha256:exact"}`), 0600)
	if err := migrateDurableData(); err != nil {
		t.Fatal(err)
	}
	if err := migrateDurableData(); err != nil {
		t.Fatal("migration not idempotent", err)
	}
	cleanupAutomationOutput()
	next := filepath.Join(durableDataPath("cluster-history"), "cluster", "event.json")
	raw, err := os.ReadFile(next)
	if err != nil || string(raw) != `{"digest":"sha256:exact"}` {
		t.Fatalf("history lost: %s %v", raw, err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("legacy copy remained")
	}
}
func TestDurableMigrationConflictPreservesBothCopies(t *testing.T) {
	t.Setenv("RANCHER_RUNWAY_WORKSPACE", t.TempDir())
	t.Setenv(runIDEnv, "")
	old := filepath.Join(automationOutputDir(), "control-panel", "cluster-workspaces.json")
	next := filepath.Join(durableDataDir(), "cluster-workspaces.json")
	for path, data := range map[string]string{old: "old", next: "new"} {
		os.MkdirAll(filepath.Dir(path), 0700)
		os.WriteFile(path, []byte(data), 0600)
	}
	if migrateDurableData() == nil {
		t.Fatal("conflicting copies accepted")
	}
	cleanupAutomationOutput()
	for path, data := range map[string]string{old: "old", next: "new"} {
		raw, err := os.ReadFile(path)
		if err != nil || string(raw) != data {
			t.Fatalf("migration overwrote data: %s %v", path, err)
		}
	}
}
