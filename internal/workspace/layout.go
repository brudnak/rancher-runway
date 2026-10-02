// Package workspace defines retained-data locations and safe legacy migrations.
package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Durable libraries are siblings of disposable automation output. Run cleanup
// must never own this directory. Names are fixed by callers, never HTTP input.
var durableDataNames = []string{"cluster-workspaces.json", "cluster-history", "rancher-operations", "issue-packages", "test-lab", "cache-lab"}
var durableMigrationMu sync.Mutex

// Layout locates retained libraries relative to an explicit automation output path.
// It does not read environment variables or application configuration.
type Layout struct{ OutputDir string }

func (l Layout) DataDir() string { return filepath.Join(filepath.Dir(l.OutputDir), "runway-data") }

// Migrate moves known retained libraries; completed moves can be retried safely.
func (l Layout) Migrate() error {
	if err := os.MkdirAll(l.DataDir(), 0700); err != nil {
		return err
	}
	if err := EnsureDataModule(l.DataDir()); err != nil {
		return err
	}
	for _, name := range durableDataNames {
		if err := l.MigrateEntry(name); err != nil {
			return err
		}
	}
	return nil
}

// MigrateEntry moves a trusted library name without overwriting either copy.
// The name must come from application code, not user input.
func (l Layout) MigrateEntry(name string) error {
	durableMigrationMu.Lock()
	defer durableMigrationMu.Unlock()
	old := filepath.Join(l.OutputDir, "control-panel", name)
	info, err := os.Lstat(old)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("retained data migration refuses a symlink: %s", old)
	}
	next := filepath.Join(l.DataDir(), name)
	if _, err = os.Lstat(next); err == nil {
		return fmt.Errorf("retained data exists at both %s and %s; neither copy was changed", old, next)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err = os.MkdirAll(l.DataDir(), 0700); err != nil {
		return err
	}
	// Atomic rename on the same workspace filesystem preserves every byte and ID.
	// No copy/delete window, and completed entries need no migration marker.
	if err = EnsureDataModule(l.DataDir()); err != nil {
		return err
	}
	if err = os.Rename(old, next); err != nil {
		return fmt.Errorf("move retained data %s: %w", name, err)
	}
	return nil
}

// EnsureDataModule excludes runtime data from Go source discovery and preserves
// an existing module file. The root directory must already exist.
func EnsureDataModule(root string) error {
	f, err := os.OpenFile(filepath.Join(root, "go.mod"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if os.IsExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	_, writeErr := f.WriteString("// Generated runtime data; excluded from Runway source discovery.\nmodule rancher-runway.local/runtime-data\n")
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}
