package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLaneWorkflowReadsSuiteEnvFromProvisionedLocation(t *testing.T) {
	workflow := readActionsWorkflow(t, "run-rancher-signoff-lane.yml")
	script := workflowStepScript(t, workflow, "run-lane", "Run Rancher tests")
	start := strings.Index(script, "env_file=")
	end := strings.Index(script, `echo "::add-mask::$RANCHER_ADMIN_TOKEN"`)
	if start < 0 || end <= start {
		t.Fatal("cannot locate suite environment selection and loading")
	}
	script = "set -euo pipefail\n" + script[start:end] + `printf '%s' "$CLUSTER_NAME"`
	for _, test := range []struct {
		name        string
		lane        string
		path        string
		wantFailure bool
	}{
		{"fresh install", "webhook-fresh-install", "runs/35229852749/downstream/downstream-ha-1.env", false},
		{"upgrade", "webhook-upgrade", "runs/35229852749/downstream/downstream-ha-1.env", false},
		{"candidate", "webhook-candidate-on-previous", "runs/35229852749/downstream/downstream-ha-1.env", false},
		{"framework", "framework-regression", "local-suite-ha-1.env", false},
		{"reject legacy downstream file", "webhook-fresh-install", "downstream-ha-1.env", true},
		{"reject another run", "webhook-fresh-install", "runs/other-run/downstream/downstream-ha-1.env", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			path := filepath.Join(workspace, "automation-output", test.path)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("CLUSTER_NAME=expected-cluster\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			command := exec.Command("bash", "-c", strings.ReplaceAll(script, "${{ inputs.lane }}", test.lane))
			command.Dir = workspace
			command.Env = append(os.Environ(), "HA_RANCHER_RUN_ID=35229852749", "RANCHER_RELEASE_MAJOR=2", "RANCHER_RELEASE_MINOR=11")
			output, err := command.CombinedOutput()
			if (err != nil) != test.wantFailure {
				t.Fatalf("error = %v, want failure %v; output: %s", err, test.wantFailure, output)
			}
			if !test.wantFailure && string(output) != "expected-cluster" {
				t.Fatalf("suite environment was not loaded: %s", output)
			}
		})
	}
}
