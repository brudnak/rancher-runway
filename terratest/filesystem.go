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
	if workspace := strings.TrimSpace(os.Getenv("GITHUB_WORKSPACE")); workspace != "" {
		return filepath.Join(workspace, "automation-output")
	}
	return "automation-output"
}

func automationOutputPath(name string) string {
	return filepath.Join(automationOutputDir(), name)
}

func CreateInstallScript(helmCommand, haDir string) {
	installScript := fmt.Sprintf(`#!/bin/bash
set -euo pipefail

# First make sure we're using the right kubeconfig
if [ ! -f "kube_config.yaml" ]; then
  echo "ERROR: kube_config.yaml not found. Make sure you're in the right directory."
  exit 1
fi

# Export KUBECONFIG to point to our kubeconfig file
export KUBECONFIG=$(pwd)/kube_config.yaml

# Verify kubectl can connect to the cluster
echo "Verifying connection to Kubernetes cluster..."
kubectl cluster-info
if [ $? -ne 0 ]; then
  echo "ERROR: Unable to connect to Kubernetes cluster. Check your kubeconfig."
  exit 1
fi

echo "Waiting for RKE2 ingress admission webhook..."
for attempt in $(seq 1 60); do
  admission_endpoint="$(kubectl -n kube-system get endpoints rke2-ingress-nginx-controller-admission -o jsonpath='{.subsets[0].addresses[0].ip}' 2>/dev/null || true)"
  if [ -n "${admission_endpoint}" ]; then
    echo "RKE2 ingress admission webhook is ready."
    break
  fi
  if [ "${attempt}" -eq 60 ]; then
    echo "ERROR: Timed out waiting for RKE2 ingress admission webhook endpoints."
    kubectl -n kube-system get pods -o wide || true
    kubectl -n kube-system get endpoints rke2-ingress-nginx-controller-admission -o yaml || true
    exit 1
  fi
  sleep 10
done

helm repo update

echo "Creating namespace..."
kubectl create namespace cattle-system --dry-run=client -o yaml | kubectl apply -f -

echo "Installing Rancher..."
%s

echo "Rancher installation complete!"`, helmCommand)

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
