package test

import (
	"os"
	"os/exec"
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

func TestAutomationOutputDirPrefersManagedWorkspace(t *testing.T) {
	managed := filepath.Join(t.TempDir(), "managed-terratest")
	t.Setenv("RANCHER_RUNWAY_WORKSPACE", managed)
	t.Setenv("GITHUB_WORKSPACE", filepath.Join(t.TempDir(), "github"))
	if got, want := automationOutputDir(), filepath.Join(managed, "automation-output"); got != want {
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

func TestCreateInstallScriptUsesControllerNeutralIngressReadiness(t *testing.T) {
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

	CreateInstallScript("helm install rancher rancher-latest/rancher", "high-availability-1", "rke2-traefik")

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
		"kubectl create namespace cattle-system --dry-run=client -o yaml | kubectl apply -f -",
		"ingress_daemonset='rke2-traefik'",
		`if ! kubectl -n kube-system wait --for=create "daemonset/${ingress_daemonset}" --timeout=10m`,
		`if ! kubectl -n kube-system rollout status "daemonset/${ingress_daemonset}" --timeout=10m`,
		"desiredNumberScheduled",
		"available_ingress_pods",
		"describe \"daemonset/${ingress_daemonset}\"",
		"Waiting for Kubernetes to accept Ingress resources...",
		"kubectl create --request-timeout=20s --dry-run=server -f -",
		"apiVersion: networking.k8s.io/v1",
		"generateName: rancher-runway-ingress-readiness-",
		"namespace: cattle-system",
		"Last server-side Ingress dry-run error:",
		"endpointslices.discovery.k8s.io",
		"mutatingwebhookconfigurations.admissionregistration.k8s.io",
		`printf '%s\n' "${ingress_check_output}"`,
		"helm install rancher rancher-latest/rancher",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("generated install script missing %q:\n%s", want, script)
		}
	}
	for _, stale := range []string{"rke2-ingress-nginx-controller-admission", "Waiting for RKE2 ingress admission webhook"} {
		if strings.Contains(script, stale) {
			t.Fatalf("generated install script retained controller-specific readiness check %q:\n%s", stale, script)
		}
	}
	if strings.Contains(script, "%!") {
		t.Fatalf("generated install script contains a fmt formatting artifact:\n%s", script)
	}
	if namespaceIndex, readinessIndex := strings.Index(script, "kubectl create namespace cattle-system"), strings.Index(script, "kubectl create --request-timeout=20s --dry-run=server"); namespaceIndex < 0 || readinessIndex < 0 || namespaceIndex > readinessIndex {
		t.Fatalf("expected cattle-system creation before the server-side Ingress dry-run:\n%s", script)
	}
	if output, err := exec.Command("bash", "-n", scriptPath).CombinedOutput(); err != nil {
		t.Fatalf("generated install script failed bash syntax validation: %v\n%s", err, output)
	}
}

func TestCreateInstallScriptUsesSelectedNginxDaemonSet(t *testing.T) {
	haDir := filepath.Join(t.TempDir(), "high-availability-1")
	CreateInstallScript("helm install rancher rancher-latest/rancher", haDir, "rke2-ingress-nginx-controller")

	data, err := os.ReadFile(filepath.Join(haDir, "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	if !strings.Contains(script, "ingress_daemonset='rke2-ingress-nginx-controller'") {
		t.Fatalf("generated install script omitted selected nginx DaemonSet:\n%s", script)
	}
	if strings.Contains(script, "ingress_daemonset='rke2-traefik'") {
		t.Fatalf("generated nginx install script retained Traefik readiness target:\n%s", script)
	}
}

func TestInstallScriptRetriesIngressServerDryRunUnderStrictMode(t *testing.T) {
	tempDir := t.TempDir()
	haDir := filepath.Join(tempDir, "high-availability-1")
	fakeBin := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(fakeBin, 0o700); err != nil {
		t.Fatal(err)
	}

	writeExecutable := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(fakeBin, name), []byte(content), 0o700); err != nil {
			t.Fatalf("failed to write fake %s: %v", name, err)
		}
	}
	writeExecutable("kubectl", `#!/bin/bash
set -euo pipefail
case "$*" in
  "cluster-info")
    echo "Kubernetes control plane is running"
    ;;
  "create namespace cattle-system --dry-run=client -o yaml")
    printf '%s\n' 'apiVersion: v1' 'kind: Namespace' 'metadata:' '  name: cattle-system'
    ;;
  "apply -f -")
    cat >/dev/null
    ;;
  "-n kube-system wait --for=create daemonset/rke2-traefik --timeout=10m")
    echo "daemonset.apps/rke2-traefik created"
    ;;
  "-n kube-system rollout status daemonset/rke2-traefik --timeout=10m")
    echo "daemon set rke2-traefik successfully rolled out"
    ;;
  "-n kube-system get daemonset/rke2-traefik --request-timeout=20s -o jsonpath="*)
    printf '%s\n' '1 1'
    ;;
  "create --request-timeout=20s --dry-run=server -f -")
    cat >"${FAKE_INGRESS_INPUT:?}"
    attempt=0
    if [ -f "${FAKE_INGRESS_COUNT:?}" ]; then
      attempt="$(cat "${FAKE_INGRESS_COUNT}")"
    fi
    attempt=$((attempt + 1))
    printf '%s\n' "${attempt}" >"${FAKE_INGRESS_COUNT}"
    if [ "${attempt}" -eq 1 ]; then
      echo "webhook has no ready endpoints" >&2
      exit 1
    fi
    echo "ingress.networking.k8s.io/rancher-runway-ingress-readiness-dryrun created (server dry run)"
    ;;
  *)
    echo "unexpected kubectl arguments: $*" >&2
    exit 1
    ;;
esac
`)
	writeExecutable("helm", `#!/bin/bash
set -euo pipefail
printf '%s\n' "$*" >>"${FAKE_HELM_LOG:?}"
`)
	writeExecutable("sleep", `#!/bin/bash
exit 0
`)

	CreateInstallScript("helm install rancher rancher-latest/rancher", haDir, "rke2-traefik")
	if err := os.WriteFile(filepath.Join(haDir, "kube_config.yaml"), []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ingressCountPath := filepath.Join(tempDir, "ingress-count")
	ingressInputPath := filepath.Join(tempDir, "ingress.yaml")
	helmLogPath := filepath.Join(tempDir, "helm.log")
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_INGRESS_COUNT", ingressCountPath)
	t.Setenv("FAKE_INGRESS_INPUT", ingressInputPath)
	t.Setenv("FAKE_HELM_LOG", helmLogPath)

	command := exec.Command(filepath.Join(haDir, "install.sh"))
	command.Dir = haDir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated install script failed after a retry: %v\n%s", err, output)
	}
	count, err := os.ReadFile(ingressCountPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(count)) != "2" {
		t.Fatalf("expected two server-side dry-run attempts, got %q", count)
	}
	ingress, err := os.ReadFile(ingressInputPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"kind: Ingress", "namespace: cattle-system", "generateName: rancher-runway-ingress-readiness-"} {
		if !strings.Contains(string(ingress), want) {
			t.Fatalf("server-side dry-run input missing %q:\n%s", want, ingress)
		}
	}
	helmLog, err := os.ReadFile(helmLogPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"repo update", "install rancher rancher-latest/rancher"} {
		if !strings.Contains(string(helmLog), want) {
			t.Fatalf("expected Helm to run %q after readiness succeeded, got:\n%s", want, helmLog)
		}
	}
}

func TestInstallScriptRejectsZeroPodIngressDaemonSet(t *testing.T) {
	tempDir := t.TempDir()
	haDir := filepath.Join(tempDir, "high-availability-1")
	fakeBin := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(fakeBin, 0o700); err != nil {
		t.Fatal(err)
	}

	kubectlScript := `#!/bin/bash
set -euo pipefail
case "$*" in
  "cluster-info")
    exit 0
    ;;
  "create namespace cattle-system --dry-run=client -o yaml")
    printf '%s\n' 'apiVersion: v1' 'kind: Namespace' 'metadata:' '  name: cattle-system'
    ;;
  "apply -f -")
    cat >/dev/null
    ;;
  "-n kube-system wait --for=create daemonset/rke2-traefik --timeout=10m"|"-n kube-system rollout status daemonset/rke2-traefik --timeout=10m")
    exit 0
    ;;
  "-n kube-system get daemonset/rke2-traefik --request-timeout=20s -o jsonpath="*)
    printf '%s\n' '0 0'
    ;;
  "create --request-timeout=20s --dry-run=server -f -")
    : >"${FAKE_INGRESS_DRY_RUN:?}"
    exit 0
    ;;
  "--request-timeout=20s get "*|"--request-timeout=20s -n kube-system describe "*)
    printf '%s\n' "$*" >>"${FAKE_DIAGNOSTICS_LOG:?}"
    ;;
  *)
    echo "unexpected kubectl arguments: $*" >&2
    exit 1
    ;;
esac
`
	if err := os.WriteFile(filepath.Join(fakeBin, "kubectl"), []byte(kubectlScript), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fakeBin, "helm"), []byte("#!/bin/bash\n: >\"${FAKE_HELM_LOG:?}\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	CreateInstallScript("helm install rancher rancher-latest/rancher", haDir, "rke2-traefik")
	if err := os.WriteFile(filepath.Join(haDir, "kube_config.yaml"), []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	diagnosticsPath := filepath.Join(tempDir, "diagnostics.log")
	ingressDryRunPath := filepath.Join(tempDir, "ingress-dry-run")
	helmLogPath := filepath.Join(tempDir, "helm.log")
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_DIAGNOSTICS_LOG", diagnosticsPath)
	t.Setenv("FAKE_INGRESS_DRY_RUN", ingressDryRunPath)
	t.Setenv("FAKE_HELM_LOG", helmLogPath)

	command := exec.Command(filepath.Join(haDir, "install.sh"))
	command.Dir = haDir
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("expected zero-pod ingress DaemonSet to fail readiness:\n%s", output)
	}
	if !strings.Contains(string(output), "has 0/0 available pods") {
		t.Fatalf("expected zero-pod DaemonSet diagnostic, got:\n%s", output)
	}
	if _, err := os.Stat(ingressDryRunPath); !os.IsNotExist(err) {
		t.Fatalf("expected Ingress dry-run not to execute, stat err=%v", err)
	}
	if _, err := os.Stat(helmLogPath); !os.IsNotExist(err) {
		t.Fatalf("expected Helm not to run, stat err=%v", err)
	}
	diagnostics, err := os.ReadFile(diagnosticsPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"describe daemonset/rke2-traefik", "get events -A"} {
		if !strings.Contains(string(diagnostics), want) {
			t.Fatalf("zero-pod diagnostics omitted %q:\n%s", want, diagnostics)
		}
	}
}

func TestInstallScriptStopsAfterBoundedIngressAdmissionTimeout(t *testing.T) {
	tempDir := t.TempDir()
	haDir := filepath.Join(tempDir, "high-availability-1")
	fakeBin := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(fakeBin, 0o700); err != nil {
		t.Fatal(err)
	}

	kubectlScript := `#!/bin/bash
set -euo pipefail
case "$*" in
  "cluster-info")
    exit 0
    ;;
  "create namespace cattle-system --dry-run=client -o yaml")
    printf '%s\n' 'apiVersion: v1' 'kind: Namespace' 'metadata:' '  name: cattle-system'
    ;;
  "apply -f -")
    cat >/dev/null
    ;;
  "-n kube-system wait --for=create daemonset/rke2-traefik --timeout=10m"|"-n kube-system rollout status daemonset/rke2-traefik --timeout=10m")
    exit 0
    ;;
  "-n kube-system get daemonset/rke2-traefik --request-timeout=20s -o jsonpath="*)
    printf '%s\n' '1 1'
    ;;
  "create --request-timeout=20s --dry-run=server -f -")
    cat >/dev/null
    attempt=0
    if [ -f "${FAKE_INGRESS_COUNT:?}" ]; then
      attempt="$(cat "${FAKE_INGRESS_COUNT}")"
    fi
    printf '%s\n' "$((attempt + 1))" >"${FAKE_INGRESS_COUNT}"
    echo "sentinel admission webhook failure" >&2
    exit 1
    ;;
  "--request-timeout=20s get "*|"--request-timeout=20s -n kube-system describe "*)
    printf '%s\n' "$*" >>"${FAKE_KUBECTL_GET_LOG:?}"
    ;;
  *)
    echo "unexpected kubectl arguments: $*" >&2
    exit 1
    ;;
esac
`
	if err := os.WriteFile(filepath.Join(fakeBin, "kubectl"), []byte(kubectlScript), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fakeBin, "helm"), []byte("#!/bin/bash\nprintf '%s\\n' \"$*\" >>\"${FAKE_HELM_LOG:?}\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fakeBin, "sleep"), []byte("#!/bin/bash\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	CreateInstallScript("helm install rancher rancher-latest/rancher", haDir, "rke2-traefik")
	if err := os.WriteFile(filepath.Join(haDir, "kube_config.yaml"), []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ingressCountPath := filepath.Join(tempDir, "ingress-count")
	kubectlGetLogPath := filepath.Join(tempDir, "kubectl-get.log")
	helmLogPath := filepath.Join(tempDir, "helm.log")
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_INGRESS_COUNT", ingressCountPath)
	t.Setenv("FAKE_KUBECTL_GET_LOG", kubectlGetLogPath)
	t.Setenv("FAKE_HELM_LOG", helmLogPath)

	command := exec.Command(filepath.Join(haDir, "install.sh"))
	command.Dir = haDir
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("expected generated install script to fail after bounded retries:\n%s", output)
	}
	if !strings.Contains(string(output), "sentinel admission webhook failure") {
		t.Fatalf("expected last admission error in timeout diagnostics:\n%s", output)
	}
	count, err := os.ReadFile(ingressCountPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(count)) != "20" {
		t.Fatalf("expected exactly 20 bounded dry-run attempts, got %q", count)
	}
	diagnostics, err := os.ReadFile(kubectlGetLogPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"ingressclasses.networking.k8s.io",
		"endpointslices.discovery.k8s.io",
		"validatingwebhookconfigurations.admissionregistration.k8s.io",
		"mutatingwebhookconfigurations.admissionregistration.k8s.io",
	} {
		if !strings.Contains(string(diagnostics), want) {
			t.Fatalf("timeout diagnostics omitted %q:\n%s", want, diagnostics)
		}
	}
	if _, err := os.Stat(helmLogPath); !os.IsNotExist(err) {
		t.Fatalf("expected Helm not to run after ingress admission timeout, stat err=%v", err)
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
