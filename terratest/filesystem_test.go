package test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAutomationOutputDirUsesGitHubWorkspace(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("GITHUB_WORKSPACE", workspace)

	if got, want := automationOutputDir(), filepath.Join(workspace, "automation-output"); got != want {
		t.Fatalf("automationOutputDir() = %q, want %q", got, want)
	}
}

func TestAutomationOutputDirFallsBackToPackageDirectory(t *testing.T) {
	t.Setenv("GITHUB_WORKSPACE", "")

	if got, want := automationOutputDir(), "automation-output"; got != want {
		t.Fatalf("automationOutputDir() = %q, want %q", got, want)
	}
}

func TestCleanupAutomationOutputRemovesWorkspaceFolder(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("GITHUB_WORKSPACE", workspace)

	outputDir := automationOutputDir()
	if err := os.MkdirAll(filepath.Join(outputDir, "control-panel"), 0o755); err != nil {
		t.Fatalf("failed to create automation output dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "control-panel", "stale.yaml"), []byte("stale"), 0o600); err != nil {
		t.Fatalf("failed to write stale kubeconfig: %v", err)
	}

	cleanupAutomationOutput()

	if _, err := os.Stat(outputDir); !os.IsNotExist(err) {
		t.Fatalf("expected automation output dir to be removed, stat err=%v", err)
	}
}

func TestCleanupAutomationOutputForPanelRunPreservesControlPanelState(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("GITHUB_WORKSPACE", workspace)
	t.Setenv(runIDEnv, "ABC12345")

	outputDir := automationOutputDir()
	runDir := filepath.Join(outputDir, "runs", "abc12345")
	if err := os.MkdirAll(filepath.Join(runDir, "terraform"), 0o755); err != nil {
		t.Fatalf("failed to create run dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(outputDir, "control-panel"), 0o755); err != nil {
		t.Fatalf("failed to create control panel dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "control-panel", "lifecycle-state.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("failed to write lifecycle state: %v", err)
	}

	cleanupAutomationOutput()

	if _, err := os.Stat(runDir); !os.IsNotExist(err) {
		t.Fatalf("expected run dir to be removed, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "control-panel", "lifecycle-state.json")); err != nil {
		t.Fatalf("expected control panel state to remain: %v", err)
	}
}

func TestCleanupBootstrapTerraformLocalFilesRemovesOnlyLocalWorkingFiles(t *testing.T) {
	tempDir := t.TempDir()
	bootstrapDir := filepath.Join(tempDir, "bootstrap", "terraform-state")
	if err := os.MkdirAll(filepath.Join(bootstrapDir, ".terraform"), 0o755); err != nil {
		t.Fatalf("failed to create .terraform dir: %v", err)
	}
	for _, name := range []string{".terraform.lock.hcl", "terraform.tfstate", "terraform.tfstate.backup", "terraform.tfvars", "tfplan", "backend.env", "main.tf"} {
		if err := os.WriteFile(filepath.Join(bootstrapDir, name), []byte(name), 0o600); err != nil {
			t.Fatalf("failed to write %s: %v", name, err)
		}
	}

	terratestDir := filepath.Join(tempDir, "terratest")
	if err := os.MkdirAll(terratestDir, 0o755); err != nil {
		t.Fatalf("failed to create terratest dir: %v", err)
	}
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get original dir: %v", err)
	}
	if err := os.Chdir(terratestDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Fatalf("failed to restore original dir: %v", err)
		}
	})

	cleanupBootstrapTerraformLocalFiles()

	for _, name := range []string{".terraform.lock.hcl", "terraform.tfstate", "terraform.tfstate.backup", "terraform.tfvars", "tfplan", "backend.env"} {
		if _, err := os.Stat(filepath.Join(bootstrapDir, name)); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed, stat err=%v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(bootstrapDir, ".terraform")); !os.IsNotExist(err) {
		t.Fatalf("expected .terraform dir to be removed, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(bootstrapDir, "main.tf")); err != nil {
		t.Fatalf("expected main.tf to remain: %v", err)
	}
}

func TestCleanupTerraformNonStateFilesPreservesLocalState(t *testing.T) {
	tempDir := t.TempDir()
	terraformDir := filepath.Join(tempDir, "modules", "aws")
	if err := os.MkdirAll(filepath.Join(terraformDir, ".terraform"), 0o755); err != nil {
		t.Fatalf("failed to create .terraform dir: %v", err)
	}
	for _, name := range []string{".terraform.lock.hcl", "backend.tf", "terraform.tfvars", "terraform.tfstate", "terraform.tfstate.backup"} {
		if err := os.WriteFile(filepath.Join(terraformDir, name), []byte(name), 0o600); err != nil {
			t.Fatalf("failed to write %s: %v", name, err)
		}
	}

	terratestDir := filepath.Join(tempDir, "terratest")
	if err := os.MkdirAll(terratestDir, 0o755); err != nil {
		t.Fatalf("failed to create terratest dir: %v", err)
	}
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get original dir: %v", err)
	}
	if err := os.Chdir(terratestDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Fatalf("failed to restore original dir: %v", err)
		}
	})

	cleanupTerraformNonStateFiles()

	for _, name := range []string{".terraform.lock.hcl", "backend.tf", "terraform.tfvars"} {
		if _, err := os.Stat(filepath.Join(terraformDir, name)); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed, stat err=%v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(terraformDir, ".terraform")); !os.IsNotExist(err) {
		t.Fatalf("expected .terraform dir to be removed, stat err=%v", err)
	}
	for _, name := range []string{"terraform.tfstate", "terraform.tfstate.backup"} {
		if _, err := os.Stat(filepath.Join(terraformDir, name)); err != nil {
			t.Fatalf("expected %s to remain: %v", name, err)
		}
	}
}

func TestCreateInstallScriptFailsFastAndCreatesNamespaceIdempotently(t *testing.T) {
	tempDir := t.TempDir()
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get original dir: %v", err)
	}
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("failed to chdir to temp dir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Fatalf("failed to restore original dir: %v", err)
		}
	})

	CreateInstallScript("helm install rancher rancher-latest/rancher", "high-availability-1")

	scriptPath := filepath.Join(tempDir, "high-availability-1", "install.sh")
	data, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("failed to read generated install script: %v", err)
	}
	if info, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("failed to stat generated install script: %v", err)
	} else if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("expected install script mode 0700, got %v", got)
	}
	script := string(data)

	for _, want := range []string{
		"set -euo pipefail",
		"Waiting for RKE2 ingress admission webhook...",
		"rke2-ingress-nginx-controller-admission",
		"kubectl create namespace cattle-system --dry-run=client -o yaml | kubectl apply -f -",
		"helm install rancher rancher-latest/rancher",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("generated install script missing %q:\n%s", want, script)
		}
	}
}

func TestHAInstanceDirUsesOptionalOutputRoot(t *testing.T) {
	if got := haInstanceDir(2); got != "high-availability-2" {
		t.Fatalf("expected default HA dir, got %q", got)
	}

	root := filepath.Join(t.TempDir(), "runs", "abc12345", "ha")
	t.Setenv(haOutputRootEnv, root)
	if got, want := haInstanceDir(2), filepath.Join(root, "high-availability-2"); got != want {
		t.Fatalf("expected rooted HA dir %q, got %q", want, got)
	}
}

func TestRancherTestsHostRemovesURLScheme(t *testing.T) {
	tests := map[string]string{
		"gha.example.test":          "gha.example.test",
		"https://gha.example.test":  "gha.example.test",
		"https://gha.example.test/": "gha.example.test",
		"http://gha.example.test":   "gha.example.test",
	}

	for input, want := range tests {
		if got := rancherTestsHost(input); got != want {
			t.Fatalf("rancherTestsHost(%q) = %q, want %q", input, got, want)
		}
	}
}
