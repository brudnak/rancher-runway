package workspace_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/brudnak/ha-rancher-rke2/internal/workspace"
)

func writeFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
func requireContent(t *testing.T, path, content string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil || string(data) != content {
		t.Fatalf("%s: got %q (%v), want %q", path, data, err, content)
	}
}

func TestMigrationUsesExplicitWorkspace(t *testing.T) {
	root := t.TempDir()
	layout := workspace.Layout{OutputDir: filepath.Join(root, "automation-output")}
	old := filepath.Join(layout.OutputDir, "control-panel", "cluster-history", "event.json")
	writeFixture(t, old, `{"version":"2.15.3-head","digest":"sha256:exact"}`)
	// Only retained libraries migrate; lifecycle files stay in automation output.
	session := filepath.Join(layout.OutputDir, "control-panel", "session.json")
	writeFixture(t, session, "session")
	if err := layout.Migrate(); err != nil {
		t.Fatal(err)
	}
	if err := layout.Migrate(); err != nil {
		t.Fatal("repeat migration:", err)
	}
	requireContent(t, filepath.Join(root, "runway-data", "cluster-history", "event.json"), `{"version":"2.15.3-head","digest":"sha256:exact"}`)
	requireContent(t, session, "session")
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("legacy entry remains: %v", err)
	}
}

func TestMigrationRejectsSymlinksAndConflicts(t *testing.T) {
	for _, kind := range []string{"symlink", "conflict"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			layout := workspace.Layout{OutputDir: filepath.Join(root, "automation-output")}
			old := filepath.Join(layout.OutputDir, "control-panel", "cluster-workspaces.json")
			next := filepath.Join(layout.DataDir(), "cluster-workspaces.json")
			writeFixture(t, next, "new")
			if kind == "symlink" {
				if err := os.MkdirAll(filepath.Dir(old), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(next, old); err != nil {
					t.Fatal(err)
				}
			} else {
				writeFixture(t, old, "old")
			}
			if err := layout.Migrate(); err == nil {
				t.Fatal("unsafe migration accepted")
			}
			requireContent(t, next, "new")
			if kind == "conflict" {
				requireContent(t, old, "old")
			}
		})
	}
}

func TestDataModulePreservesExistingFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "go.mod")
	writeFixture(t, path, "module existing.local/data\n")
	if err := workspace.EnsureDataModule(root); err != nil {
		t.Fatal(err)
	}
	requireContent(t, path, "module existing.local/data\n")
}
