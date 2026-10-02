package test

import (
	"fmt"
	"path/filepath"
	"strings"
)

func (p *localControlPanel) enrichRunRecord(record panelRunRecord) panelRunRecord {
	record = p.normalizeRunRecordArtifactPaths(record)
	record.RunFolderPath = runFolderPathForRecord(record)
	record.RunFolderExists = record.RunFolderPath != "" && pathExists(record.RunFolderPath)
	return record
}

func (p *localControlPanel) normalizeRunRecordArtifactPaths(record panelRunRecord) panelRunRecord {
	runID := safeRunPathSegment(record.RunID)
	record.TerraformModuleDir = p.remapRunArtifactPath(record.TerraformModuleDir, runID)
	record.TerraformStatePath = p.remapRunArtifactPath(record.TerraformStatePath, runID)
	record.TerraformDataDir = p.remapRunArtifactPath(record.TerraformDataDir, runID)
	record.HAOutputRoot = p.remapRunArtifactPath(record.HAOutputRoot, runID)
	record.RunFolderPath = p.remapRunArtifactPath(record.RunFolderPath, runID)
	if strings.HasPrefix(record.TerraformBackend, "local (") && strings.HasSuffix(record.TerraformBackend, ")") {
		backendPath := strings.TrimSuffix(strings.TrimPrefix(record.TerraformBackend, "local ("), ")")
		record.TerraformBackend = "local (" + p.remapRunArtifactPath(backendPath, runID) + ")"
	}
	return record
}

func (p *localControlPanel) remapRunArtifactPath(rawPath string, runID string) string {
	path := strings.TrimSpace(rawPath)
	if path == "" || runID == "" {
		return path
	}

	cleanPath := filepath.Clean(path)
	if !filepath.IsAbs(cleanPath) || pathExists(cleanPath) {
		return cleanPath
	}

	parts := strings.Split(filepath.ToSlash(cleanPath), "/")
	for i := 0; i+2 < len(parts); i++ {
		if parts[i] != "automation-output" || parts[i+1] != "runs" || parts[i+2] != runID {
			continue
		}
		suffix := filepath.FromSlash(strings.Join(parts[i+3:], "/"))
		return filepath.Join(p.testDir, "automation-output", "runs", runID, suffix)
	}
	return cleanPath
}

func runFolderPathForRecord(record panelRunRecord) string {
	if path := strings.TrimSpace(record.TerraformModuleDir); path != "" {
		return strings.TrimSuffix(filepath.Clean(path), string(filepath.Separator)+filepath.Join("terraform", "module"))
	}
	if path := strings.TrimSpace(record.TerraformStatePath); path != "" {
		return strings.TrimSuffix(filepath.Clean(path), string(filepath.Separator)+filepath.Join("terraform", "terraform.tfstate"))
	}
	if path := strings.TrimSpace(record.HAOutputRoot); path != "" {
		return strings.TrimSuffix(filepath.Clean(path), string(filepath.Separator)+"ha")
	}
	return ""
}

func (p *localControlPanel) currentHAOutputRoot() string {
	record, ok := p.readCurrentRunRecord()
	if !ok {
		return ""
	}
	return strings.TrimSpace(record.HAOutputRoot)
}

func (p *localControlPanel) haOutputRootForRun(runID string) string {
	return filepath.Join(p.testDir, "automation-output", "runs", safeRunPathSegment(runID), "ha")
}

func (p *localControlPanel) terraformStatePathForRun(runID string) string {
	return filepath.Join(p.testDir, "automation-output", "runs", safeRunPathSegment(runID), "terraform", "terraform.tfstate")
}

func (p *localControlPanel) terraformDataDirForRun(runID string) string {
	return filepath.Join(p.testDir, "automation-output", "runs", safeRunPathSegment(runID), "terraform", ".terraform")
}

func (p *localControlPanel) terraformModuleDirForRun(runID string) string {
	return filepath.Join(p.testDir, "automation-output", "runs", safeRunPathSegment(runID), "terraform", "module")
}

func (p *localControlPanel) haInstanceDir(instanceNum int) string {
	if root := p.currentHAOutputRoot(); root != "" {
		return filepath.Join(root, fmt.Sprintf("high-availability-%d", instanceNum))
	}
	return filepath.Join(p.testDir, fmt.Sprintf("high-availability-%d", instanceNum))
}

func (p *localControlPanel) haInstanceDirForRun(record panelRunRecord, instanceNum int) string {
	if root := strings.TrimSpace(record.HAOutputRoot); root != "" {
		return filepath.Join(root, fmt.Sprintf("high-availability-%d", instanceNum))
	}
	return p.haInstanceDir(instanceNum)
}

func (p *localControlPanel) hostedTenantInstanceDirForRun(record panelRunRecord, instanceNum int) string {
	root := strings.TrimSpace(record.HAOutputRoot)
	if root == "" {
		root = p.currentHAOutputRoot()
	}
	name := "host-rancher"
	if instanceNum > 1 {
		name = fmt.Sprintf("tenant-%d-rancher", instanceNum-1)
	}
	if root == "" {
		return name
	}
	return filepath.Join(root, name)
}

func runScopedClusterName(runID string, name string) string {
	runID = safeRunPathSegment(runID)
	if runID == "" || runID == "unknown" {
		return name
	}
	return fmt.Sprintf("%s (%s)", name, runID)
}

func runScopedDownloadName(runID string, name string) string {
	runID = safeRunPathSegment(runID)
	if runID == "" || runID == "unknown" {
		return name
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	return fmt.Sprintf("%s-%s%s", base, runID, ext)
}

func safeRunPathSegment(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	cleaned := strings.Trim(b.String(), "-_")
	if cleaned == "" {
		return "unknown"
	}
	return cleaned
}
