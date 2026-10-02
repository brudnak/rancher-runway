package test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (p *localControlPanel) prepareTerraformModuleForRun(runID string) error {
	sourceDir := p.terraformSourceModuleDir()
	targetDir := p.terraformModuleDirForRun(runID)
	if err := os.RemoveAll(targetDir); err != nil {
		return fmt.Errorf("failed to clear run Terraform module %s: %w", targetDir, err)
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("failed to create run Terraform module %s: %w", targetDir, err)
	}

	return filepath.WalkDir(sourceDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if shouldSkipTerraformModuleCopy(rel, entry) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		target := filepath.Join(targetDir, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to copy symlink from Terraform module: %s", path)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
}

func (p *localControlPanel) terraformSourceModuleDir() string {
	if isLinodeDockerDeployment() {
		return filepath.Join(p.repoRoot, "modules", "linode-docker-cattle")
	}
	return filepath.Join(p.repoRoot, "modules", "aws")
}

func shouldSkipTerraformModuleCopy(rel string, entry os.DirEntry) bool {
	name := entry.Name()
	if entry.IsDir() && name == ".terraform" {
		return true
	}
	switch name {
	case ".terraform.lock.hcl", "backend.tf", "terraform.tfvars", "terraform.tfstate", "terraform.tfstate.backup":
		return true
	}
	return strings.HasSuffix(name, ".tfstate") || strings.HasPrefix(name, ".terraform.")
}
