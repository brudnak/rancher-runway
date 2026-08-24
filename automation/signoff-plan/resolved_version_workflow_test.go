package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateResolvedRancherVersion(t *testing.T) {
	const sha = "1f680e71accf728c75478ff6b728d59c9f9a7b8b"
	tests := []struct {
		name      string
		requested string
		resolved  string
		wantOK    bool
	}{
		{name: "manual patch alias leaves resolution blank", requested: "2.15.1-head", wantOK: true},
		{name: "patch alias pins exact patch", requested: "v2.15.1-head", resolved: "v2.15.1-" + sha + "-head", wantOK: true},
		{name: "normalizes v prefix and hex case", requested: "2.15.1-head", resolved: "2.15.1-1F680E7-head", wantOK: true},
		{name: "patch alias rejects minor immutable target", requested: "v2.15.1-head", resolved: "v2.15-" + sha + "-head"},
		{name: "patch alias rejects another patch", requested: "v2.15.1-head", resolved: "v2.15.2-" + sha + "-head"},
		{name: "patch alias rejects mutable resolution", requested: "v2.15.1-head", resolved: "v2.15.1-head"},
		{name: "minor alias pins same minor", requested: "v2.15-head", resolved: "v2.15-" + sha + "-head", wantOK: true},
		{name: "minor alias permits Prime patch target", requested: "v2.15-head", resolved: "v2.15.1-" + sha + "-head", wantOK: true},
		{name: "minor alias rejects another minor", requested: "v2.15-head", resolved: "v2.16-" + sha + "-head"},
		{name: "minor alias rejects mutable resolution", requested: "v2.15-head", resolved: "v2.15-head"},
		{name: "plain head pins an immutable target", requested: "head", resolved: "v2.16-" + sha + "-head", wantOK: true},
		{name: "plain head rejects mutable resolution", requested: "head", resolved: "v2.16-head"},
		{name: "immutable community target equals itself", requested: "v2.15-abcdef0-head", resolved: "2.15-ABCDEF0-head", wantOK: true},
		{name: "immutable Prime target equals itself", requested: "v2.15.1-abcdef0-head", resolved: "v2.15.1-abcdef0-head", wantOK: true},
		{name: "immutable target rejects another sha", requested: "v2.15.1-abcdef0-head", resolved: "v2.15.1-deadbee-head"},
		{name: "release target equals itself", requested: "2.15.1-rc1", resolved: "v2.15.1-rc1", wantOK: true},
		{name: "release target rejects substitution", requested: "v2.15.1-rc1", resolved: "v2.15.1-rc2"},
		{name: "rejects empty requested target", requested: "", resolved: "v2.15.1-" + sha + "-head"},
	}

	script := filepath.Join("..", "validate-resolved-rancher-version.sh")
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command := exec.Command("bash", script, test.requested, test.resolved)
			output, err := command.CombinedOutput()
			if test.wantOK && err != nil {
				t.Fatalf("validation failed: %v\n%s", err, output)
			}
			if !test.wantOK && err == nil {
				t.Fatalf("validation unexpectedly accepted requested=%q resolved=%q", test.requested, test.resolved)
			}
			if !test.wantOK && !strings.Contains(string(output), "invalid resolved Rancher target:") {
				t.Fatalf("rejection did not explain the validation failure: %q", output)
			}
		})
	}
}

func TestLaneWorkflowValidatesResolvedTargetBeforeCredentials(t *testing.T) {
	workflow := readActionsWorkflow(t, "run-rancher-signoff-lane.yml")
	job, ok := workflow.Jobs["run-lane"]
	if !ok {
		t.Fatal("run-lane job not found")
	}

	validationIndex := -1
	credentialsIndex := -1
	for index, step := range job.Steps {
		switch step.Name {
		case "Validate resolved Rancher target":
			validationIndex = index
			if got := step.Env["RESOLVED_RANCHER_VERSION"]; got != "${{ inputs.resolved_rancher_version || '' }}" {
				t.Fatalf("RESOLVED_RANCHER_VERSION = %q", got)
			}
			for _, expected := range []string{
				"automation/validate-resolved-rancher-version.sh",
				`"$REQUESTED_RANCHER_VERSION"`,
				`"$RESOLVED_RANCHER_VERSION"`,
			} {
				if !strings.Contains(step.Run, expected) {
					t.Fatalf("validation step does not include %q", expected)
				}
			}
		case "Configure AWS credentials":
			credentialsIndex = index
		}
	}
	if validationIndex < 0 {
		t.Fatal("resolved target validation step not found")
	}
	if credentialsIndex < 0 {
		t.Fatal("AWS credentials step not found")
	}
	if validationIndex >= credentialsIndex {
		t.Fatalf("resolved target validation step index %d must precede credentials step index %d", validationIndex, credentialsIndex)
	}

	path := filepath.Join("..", "validate-resolved-rancher-version.sh")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("validation script is unavailable: %v", err)
	}
}
