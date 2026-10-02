package test

import (
	"log"
	"path/filepath"

	"github.com/brudnak/ha-rancher-rke2/internal/workspace"
)

func workspaceLayout() workspace.Layout     { return workspace.Layout{OutputDir: automationOutputDir()} }
func durableDataDir() string                { return workspaceLayout().DataDir() }
func migrateDurableData() error             { return workspaceLayout().Migrate() }
func migrateDurableEntry(name string) error { return workspaceLayout().MigrateEntry(name) }
func durableDataPath(name string) string {
	if err := migrateDurableEntry(name); err != nil {
		log.Printf("[workspace] Retained data migration deferred: %v", err)
		return filepath.Join(automationOutputDir(), "control-panel", name)
	}
	return filepath.Join(durableDataDir(), name)
}
