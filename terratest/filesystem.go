package test

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
)

const haOutputRootEnv = "HA_RANCHER_HA_OUTPUT_ROOT"
const runIDEnv = "HA_RANCHER_RUN_ID"
const panelNonInteractiveEnv = "HA_RANCHER_PANEL_NONINTERACTIVE"
const terraformStatePathEnv = "HA_RANCHER_TF_STATE_PATH"
const terraformDataDirEnv = "HA_RANCHER_TF_DATA_DIR"
const terraformModuleDirEnv = "HA_RANCHER_TF_MODULE_DIR"

func haInstanceDir(instanceNum int) string {
	name := fmt.Sprintf("high-availability-%d", instanceNum)
	if root := strings.TrimSpace(os.Getenv(haOutputRootEnv)); root != "" {
		return filepath.Join(root, name)
	}
	return name
}

func absoluteFromWorkingDir(path string) (string, error) {
	if filepath.IsAbs(path) {
		return path, nil
	}
	currentDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}
	return filepath.Join(currentDir, path), nil
}

func terraformModuleDir() string {
	if dir := strings.TrimSpace(os.Getenv(terraformModuleDirEnv)); dir != "" {
		return dir
	}
	if isLinodeDockerDeployment() {
		return "../modules/linode-docker-cattle"
	}
	return "../modules/aws"
}

func cleanupHAInstance(instanceNum int) {
	haDir := haInstanceDir(instanceNum)

	filesToRemove := []string{
		filepath.Join(haDir, "install.sh"),
		filepath.Join(haDir, "kube_config.yaml"),
		filepath.Join(haDir, "kube_config_lb.yaml"),
	}

	for _, file := range filesToRemove {
		RemoveFile(file)
	}

	RemoveFolder(haDir)
}

func cleanupTerraformFiles() {
	files := []string{
		"../modules/aws/.terraform.lock.hcl",
		"../modules/aws/backend.tf",
		"../modules/aws/terraform.tfstate",
		"../modules/aws/terraform.tfstate.backup",
		"../modules/aws/terraform.tfvars",
	}

	for _, file := range files {
		RemoveFile(file)
	}

	RemoveFolder("../modules/aws/.terraform")
}

func cleanupTerraformNonStateFiles() {
	files := []string{
		"../modules/aws/.terraform.lock.hcl",
		"../modules/aws/backend.tf",
		"../modules/aws/terraform.tfvars",
	}

	for _, file := range files {
		RemoveFile(file)
	}

	RemoveFolder("../modules/aws/.terraform")
}

func cleanupBootstrapTerraformLocalFiles() {
	files := []string{
		"../bootstrap/terraform-state/.terraform.lock.hcl",
		"../bootstrap/terraform-state/terraform.tfstate",
		"../bootstrap/terraform-state/terraform.tfstate.backup",
		"../bootstrap/terraform-state/terraform.tfvars",
		"../bootstrap/terraform-state/tfplan",
		"../bootstrap/terraform-state/backend.env",
	}

	for _, file := range files {
		RemoveFile(file)
	}

	RemoveFolder("../bootstrap/terraform-state/.terraform")
}

func cleanupAutomationOutput() {
	if runID := safeRunPathSegment(os.Getenv(runIDEnv)); runID != "" && runID != "unknown" {
		RemoveFolder(filepath.Join(automationOutputDir(), "runs", runID))
		return
	}
	RemoveFolder(automationOutputDir())
}

func automationOutputDir() string {
	if workspace := strings.TrimSpace(os.Getenv("RANCHER_RUNWAY_WORKSPACE")); workspace != "" {
		return filepath.Join(workspace, "automation-output")
	}
	if workspace := strings.TrimSpace(os.Getenv("GITHUB_WORKSPACE")); workspace != "" {
		return filepath.Join(workspace, "automation-output")
	}
	return "automation-output"
}

func automationOutputPath(name string) string {
	return filepath.Join(automationOutputDir(), name)
}

func CreateInstallScript(helmCommand, haDir, ingressDaemonSetName string) {
	installScript := fmt.Sprintf(`#!/bin/bash
set -euo pipefail

# First make sure we're using the right kubeconfig
if [ ! -f "kube_config.yaml" ]; then
  echo "ERROR: kube_config.yaml not found. Make sure you're in the right directory."
  exit 1
fi

# Export KUBECONFIG to point to our kubeconfig file
export KUBECONFIG="$(pwd)/kube_config.yaml"

# Verify kubectl can connect to the cluster
echo "Verifying connection to Kubernetes cluster..."
if ! kubectl cluster-info; then
  echo "ERROR: Unable to connect to Kubernetes cluster. Check your kubeconfig."
  exit 1
fi

echo "Creating namespace..."
kubectl create namespace cattle-system --dry-run=client -o yaml | kubectl apply -f -

ingress_daemonset=%s

collect_ingress_diagnostics() {
  kubectl --request-timeout=20s get ingressclasses.networking.k8s.io -o wide || true
  kubectl --request-timeout=20s get pods -A -o wide || true
  kubectl --request-timeout=20s get services,endpointslices.discovery.k8s.io -A -o wide || true
  kubectl --request-timeout=20s get validatingwebhookconfigurations.admissionregistration.k8s.io -o wide || true
  kubectl --request-timeout=20s get mutatingwebhookconfigurations.admissionregistration.k8s.io -o wide || true
  kubectl --request-timeout=20s -n kube-system describe "daemonset/${ingress_daemonset}" || true
  kubectl --request-timeout=20s get events -A --sort-by=.metadata.creationTimestamp || true
}

echo "Waiting for the configured RKE2 ingress controller..."
if ! kubectl -n kube-system wait --for=create "daemonset/${ingress_daemonset}" --timeout=10m; then
  echo "ERROR: Timed out waiting for RKE2 ingress DaemonSet ${ingress_daemonset} to be created." >&2
  collect_ingress_diagnostics
  exit 1
fi
if ! kubectl -n kube-system rollout status "daemonset/${ingress_daemonset}" --timeout=10m; then
  echo "ERROR: RKE2 ingress DaemonSet ${ingress_daemonset} did not roll out." >&2
  collect_ingress_diagnostics
  exit 1
fi

daemonset_status=""
if ! daemonset_status="$(kubectl -n kube-system get "daemonset/${ingress_daemonset}" --request-timeout=20s -o jsonpath='{.status.desiredNumberScheduled}{" "}{.status.numberAvailable}')"; then
  echo "ERROR: Unable to verify RKE2 ingress DaemonSet ${ingress_daemonset}." >&2
  collect_ingress_diagnostics
  exit 1
fi
read -r desired_ingress_pods available_ingress_pods <<<"${daemonset_status}"
if [[ ! "${desired_ingress_pods}" =~ ^[0-9]+$ || ! "${available_ingress_pods}" =~ ^[0-9]+$ ]]; then
  echo "ERROR: RKE2 ingress DaemonSet ${ingress_daemonset} returned an invalid status: ${daemonset_status}." >&2
  collect_ingress_diagnostics
  exit 1
fi
if (( desired_ingress_pods < 1 || available_ingress_pods < desired_ingress_pods )); then
  echo "ERROR: RKE2 ingress DaemonSet ${ingress_daemonset} has ${available_ingress_pods}/${desired_ingress_pods} available pods." >&2
  collect_ingress_diagnostics
  exit 1
fi

echo "Waiting for Kubernetes to accept Ingress resources..."
ingress_check_output=""
# Twenty attempts at a 20-second request timeout plus retry delays stays bounded to about ten minutes.
for ((attempt=1; attempt<=20; attempt++)); do
  if ingress_check_output="$(kubectl create --request-timeout=20s --dry-run=server -f - 2>&1 <<'EOF'
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  generateName: rancher-runway-ingress-readiness-
  namespace: cattle-system
spec:
  rules:
    - host: rancher-runway-readiness.invalid
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: rancher-runway-ingress-readiness
                port:
                  number: 80
EOF
)"; then
    echo "Kubernetes Ingress API and admission webhooks are ready."
    break
  fi
  if [ "${attempt}" -eq 20 ]; then
    echo "ERROR: Timed out waiting for Kubernetes to accept Ingress resources." >&2
    echo "Last server-side Ingress dry-run error:" >&2
    printf '%%s\n' "${ingress_check_output}" >&2
    collect_ingress_diagnostics
    exit 1
  fi
  echo "Ingress admission is not ready (attempt ${attempt}/20); retrying in 10 seconds..."
  sleep 10
done

helm repo update

echo "Installing Rancher..."
%s

echo "Rancher installation complete!"`, shellSingleQuote(ingressDaemonSetName), helmCommand)

	absHADir, err := absoluteFromWorkingDir(haDir)
	if err != nil {
		log.Printf("%v", err)
		return
	}
	if _, err := os.Stat(absHADir); os.IsNotExist(err) {
		if mkdirErr := os.MkdirAll(absHADir, 0o700); mkdirErr != nil {
			log.Printf("Failed to create directory %s: %v", absHADir, mkdirErr)
			return
		}
		log.Printf("Created directory %s", absHADir)
	}

	absInstallScriptPath := filepath.Join(absHADir, "install.sh")
	if err := os.WriteFile(absInstallScriptPath, []byte(installScript), 0o700); err != nil {
		log.Printf("Failed to write file %s: %v", absInstallScriptPath, err)
	}
}

func CheckIPAddress(ip string) string {
	if net.ParseIP(ip) == nil {
		return "invalid"
	}
	return "valid"
}

func RemoveFile(filePath string) {
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		log.Printf("Failed to remove file %s: %v", filePath, err)
	}
}

func CreateDir(path string) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(path, os.ModePerm); err != nil {
			log.Printf("Failed to create directory %s: %v", path, err)
		}
	}
}

func RemoveFolder(folderPath string) {
	if err := os.RemoveAll(folderPath); err != nil {
		log.Printf("Failed to remove folder %s: %v", folderPath, err)
	}
}
