package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLaneWorkflowResolvesExternalGoToolchain(t *testing.T) {
	workflow := readActionsWorkflow(t, "run-rancher-signoff-lane.yml")
	script := workflowStepScript(t, workflow, "run-lane", "Run Rancher tests")
	start := strings.Index(script, "external_env=(")
	end := strings.Index(script, `results_json="automation-output/rancher-test-results.json"`)
	if start < 0 || end <= start {
		t.Fatal("cannot locate isolated external environment and toolchain preflight")
	}
	script = "set -euo pipefail\n" + script[start:end] + "\necho suites-started\n"
	goPath, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	for _, toolchainExit := range []int{0, 42} {
		t.Run(fmt.Sprintf("toolchain_exit_%d", toolchainExit), func(t *testing.T) {
			root := t.TempDir()
			testsDir := filepath.Join(root, "rancher-tests")
			binDir := filepath.Join(root, "bin")
			externalHome := filepath.Join(root, "home")
			externalTmp := filepath.Join(root, "tmp")
			externalGopath := filepath.Join(root, "gopath")
			for _, dir := range []string{testsDir, binDir, externalHome, externalTmp, externalGopath} {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			// Exercise real Go toolchain selection with a newer module requirement.
			// A PATH fixture supplies that version without any network downloads.
			if err := os.WriteFile(filepath.Join(testsDir, "go.mod"), []byte("module example.com/external-tests\n\ngo 1.99.0\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(goPath, filepath.Join(binDir, "go")); err != nil {
				t.Fatal(err)
			}
			toolchain := fmt.Sprintf(`#!/bin/sh
set -eu
[ "$#" -eq 1 ] && [ "$1" = version ]
[ -f go.mod ]
[ "$GOMODCACHE" = "$GOPATH/pkg/mod" ]
[ "$GOCACHE" = "$HOME/.cache/go-build" ]
[ "$CATTLE_TEST_CONFIG" = "$TMPDIR/cattle-config.yaml" ]
[ -z "${AWS_ACCESS_KEY_ID+x}" ]
[ -z "${GH_TOKEN+x}" ]
echo selected-go1.99.0
exit %d
`, toolchainExit)
			if err := os.WriteFile(filepath.Join(binDir, "go1.99.0"), []byte(toolchain), 0o700); err != nil {
				t.Fatal(err)
			}
			command := exec.Command("bash", "-c", script)
			command.Dir = root
			command.Env = append(os.Environ(),
				"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"tests_dir="+testsDir,
				"external_home="+externalHome,
				"external_tmp="+externalTmp,
				"external_gopath="+externalGopath,
				"config_path="+filepath.Join(externalTmp, "cattle-config.yaml"),
				"AWS_ACCESS_KEY_ID=must-not-reach-external-tests",
				"GH_TOKEN=must-not-reach-external-tests",
				"GOTOOLCHAIN=local",
			)
			output, err := command.CombinedOutput()
			if (err != nil) != (toolchainExit != 0) {
				t.Fatalf("preflight error = %v, want exit %d; output: %s", err, toolchainExit, output)
			}
			if !strings.Contains(string(output), "selected-go1.99.0") {
				t.Fatalf("preflight did not select the external module's required toolchain: %s", output)
			}
			if started := strings.Contains(string(output), "suites-started"); started != (toolchainExit == 0) {
				t.Fatalf("suites started = %v with toolchain exit %d; output: %s", started, toolchainExit, output)
			}
		})
	}
}
