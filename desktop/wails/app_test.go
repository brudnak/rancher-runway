package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	harancher "github.com/brudnak/ha-rancher-rke2/terratest"
)

func TestShouldImportShellEnv(t *testing.T) {
	for _, key := range []string{
		"PATH",
		"HOME",
		"KUBECONFIG",
		"AWS_PROFILE",
		"DOCKERHUB_USERNAME",
		"LINODE_TOKEN",
		"RANCHER_TOKEN",
		"TF_VAR_region",
		"TERRAFORM_CONFIG",
	} {
		t.Run(key, func(t *testing.T) {
			if !shouldImportShellEnv(key) {
				t.Fatalf("expected %s to be imported", key)
			}
		})
	}

	for _, key := range []string{"", "SHELL", "SSH_AUTH_SOCK", "NPM_TOKEN"} {
		t.Run("skip_"+key, func(t *testing.T) {
			if shouldImportShellEnv(key) {
				t.Fatalf("expected %s to be skipped", key)
			}
		})
	}
}

func TestHardenDesktopPathDeduplicatesAndKeepsExistingEntries(t *testing.T) {
	customBin := filepath.Join(t.TempDir(), "bin")
	t.Setenv("PATH", strings.Join([]string{customBin, "/usr/bin", customBin}, string(os.PathListSeparator)))

	hardenDesktopPath()

	parts := filepath.SplitList(os.Getenv("PATH"))
	seen := map[string]bool{}
	for _, part := range parts {
		if seen[part] {
			t.Fatalf("PATH contains duplicate entry %q in %q", part, os.Getenv("PATH"))
		}
		seen[part] = true
	}
	if !seen[customBin] {
		t.Fatalf("PATH did not keep existing custom entry %q in %q", customBin, os.Getenv("PATH"))
	}
	if parts[0] != "/opt/homebrew/bin" {
		t.Fatalf("expected Homebrew path to be preferred first, got %q from %q", parts[0], os.Getenv("PATH"))
	}
}

func TestLifecycleCloseBlockedDialog(t *testing.T) {
	tests := []struct {
		operation   string
		wantTitle   string
		wantMessage string
	}{
		{operation: "setup", wantTitle: "Setup is still running", wantMessage: "creating a run slot"},
		{operation: "cleanup", wantTitle: "Cleanup is still running", wantMessage: "cleaning up infrastructure"},
		{operation: "readiness", wantTitle: "Readiness checks are still running", wantMessage: "checking cluster readiness"},
		{operation: "", wantTitle: "Lifecycle operation is still running", wantMessage: "setup, slot creation, readiness, or cleanup"},
	}

	for _, tc := range tests {
		t.Run(tc.operation, func(t *testing.T) {
			title, message := lifecycleCloseBlockedDialog(tc.operation)
			if title != tc.wantTitle {
				t.Fatalf("title = %q, want %q", title, tc.wantTitle)
			}
			if !strings.Contains(message, tc.wantMessage) {
				t.Fatalf("message = %q, want it to contain %q", message, tc.wantMessage)
			}
		})
	}
}

func TestGPUCloseWarningDialog(t *testing.T) {
	tests := []struct {
		name        string
		summary     harancher.GPUInfrastructureSummary
		wantMessage string
	}{
		{
			name:        "single gpu",
			summary:     harancher.GPUInfrastructureSummary{Active: true, Count: 1},
			wantMessage: "One GPU worker node appears to be deployed",
		},
		{
			name:        "multiple gpus",
			summary:     harancher.GPUInfrastructureSummary{Active: true, Count: 2},
			wantMessage: "2 GPU worker nodes appear to be deployed",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			title, message := gpuCloseWarningDialog(tc.summary)
			if title != "GPU infrastructure is active" {
				t.Fatalf("title = %q", title)
			}
			if !strings.Contains(message, tc.wantMessage) {
				t.Fatalf("message = %q, want it to contain %q", message, tc.wantMessage)
			}
			if !strings.Contains(message, "Destroy page") {
				t.Fatalf("message = %q, want cleanup guidance", message)
			}
		})
	}
}
