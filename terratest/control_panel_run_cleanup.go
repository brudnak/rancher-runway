package test

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (p *localControlPanel) hasWorkspaceRunResidue() bool {
	if _, ok := p.readCurrentRunRecord(); ok {
		return true
	}
	return len(p.workspaceBlockers()) > 0
}

func (p *localControlPanel) workspaceBlockers() []string {
	var blockers []string
	if _, ok := p.readCurrentRunRecord(); ok {
		blockers = append(blockers, p.currentRunRecordPath())
	}
	entries, err := os.ReadDir(p.runRecordsDir())
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
				continue
			}
			blockers = append(blockers, filepath.Join(p.runRecordsDir(), entry.Name()))
		}
	}
	blockers = append(blockers, p.sharedWorkspaceResidueBlockers()...)
	return blockers
}

func (p *localControlPanel) sharedWorkspaceResidueBlockers() []string {
	var blockers []string
	blockers = append(blockers, existingGlobPaths(filepath.Join(p.testDir, "high-availability-*"))...)
	for _, name := range []string{
		".terraform.lock.hcl",
		"terraform.tfstate",
		"terraform.tfstate.backup",
		".terraform",
		"backend.tf",
		"terraform.tfvars",
	} {
		path := filepath.Join(p.repoRoot, "modules", "aws", name)
		if _, err := os.Stat(path); err == nil {
			blockers = append(blockers, path)
		}
	}
	return blockers
}

func (p *localControlPanel) cleanLocalArtifacts() (localArtifactCleanupResult, error) {
	if records := p.listRunRecords(); len(records) > 0 {
		return localArtifactCleanupResult{}, fmt.Errorf("destroy recorded run slots before cleaning local artifacts; this keeps Terraform destroy targets available")
	}

	var result localArtifactCleanupResult
	for _, path := range p.localArtifactCleanupTargets() {
		if removeLocalArtifactPath(path) {
			result.Removed = append(result.Removed, path)
		}
	}

	for _, dir := range []string{
		p.runRecordsDir(),
		filepath.Join(p.testDir, "automation-output", "control-panel"),
		filepath.Join(p.testDir, "automation-output"),
	} {
		removeEmptyDir(dir)
	}

	sort.Strings(result.Removed)
	return result, nil
}

func (p *localControlPanel) localArtifactCleanupTargets() []string {
	targets := p.sharedWorkspaceResidueBlockers()
	targets = append(targets,
		filepath.Join(p.testDir, "automation-output", "runs"),
		p.currentRunRecordPath(),
		filepath.Join(p.testDir, "automation-output", "control-panel", "lifecycle-state.json"),
	)
	targets = append(targets, existingGlobPaths(filepath.Join(p.testDir, "automation-output", "rancher-resolution-*.json"))...)
	targets = append(targets, existingGlobPaths(filepath.Join(p.testDir, "automation-output", "control-panel", "run-*.yaml"))...)
	return uniqueExistingPaths(targets)
}

func removeLocalArtifactPath(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	if _, err := os.Stat(path); err != nil {
		return false
	}
	if err := os.RemoveAll(path); err != nil {
		log.Printf("[control-panel] Failed to remove local artifact %s: %v", path, err)
		return false
	}
	return true
}

func uniqueExistingPaths(paths []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, path := range paths {
		path = filepath.Clean(strings.TrimSpace(path))
		if path == "." || seen[path] {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			continue
		}
		seen[path] = true
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}

func removeEmptyDir(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) > 0 {
		return
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		log.Printf("[control-panel] Failed to remove empty directory %s: %v", path, err)
	}
}

func (p *localControlPanel) sharedWorkspacePathLabels() []string {
	paths := []string{
		filepath.Join(p.repoRoot, "modules", "aws"),
		filepath.Join(p.testDir, "high-availability-*"),
		filepath.Join(p.testDir, "automation-output", "runs"),
		automationOutputDir(),
	}
	return paths
}
