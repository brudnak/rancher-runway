package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	harancher "github.com/brudnak/ha-rancher-rke2/terratest"
)

func TestWalkToRepoRootFindsCurrentRepository(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to read cwd: %v", err)
	}

	repoRoot, err := walkToRepoRoot(cwd)
	if err != nil {
		t.Fatalf("walkToRepoRoot failed: %v", err)
	}

	if !fileExists(filepath.Join(repoRoot, "go.mod")) {
		t.Fatalf("expected repo root to contain go.mod, got %s", repoRoot)
	}
	if !dirExists(filepath.Join(repoRoot, "terratest")) {
		t.Fatalf("expected repo root to contain terratest, got %s", repoRoot)
	}
}

func TestUsageListsLifecycleCommands(t *testing.T) {
	usage := usageText()

	for _, command := range []string{"status", "setup", "wait-ready", "panel", "cleanup", "provision-downstream", "delete-downstream", "upgrade"} {
		if !strings.Contains(usage, command) {
			t.Fatalf("expected usage to include %q:\n%s", command, usage)
		}
	}
}

func TestClusterStateLabel(t *testing.T) {
	tests := []struct {
		name    string
		cluster harancher.LocalWorkspaceCluster
		want    string
	}{
		{name: "reachable", cluster: harancher.LocalWorkspaceCluster{Reachable: true}, want: "reachable"},
		{name: "provisioning", cluster: harancher.LocalWorkspaceCluster{Provisioning: true}, want: "provisioning"},
		{name: "available", cluster: harancher.LocalWorkspaceCluster{Available: true}, want: "unavailable"},
		{name: "missing", cluster: harancher.LocalWorkspaceCluster{}, want: "missing"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := clusterStateLabel(tc.cluster); got != tc.want {
				t.Fatalf("clusterStateLabel() = %q, want %q", got, tc.want)
			}
		})
	}
}
