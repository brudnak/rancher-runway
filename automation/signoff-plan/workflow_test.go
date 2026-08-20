package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type actionsWorkflow struct {
	Jobs map[string]struct {
		Env   map[string]string `yaml:"env"`
		Steps []struct {
			Name string `yaml:"name"`
			Run  string `yaml:"run"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

func TestSignoffWorkflowDispatchesResolvedHeadAndKeepsAliasDedupe(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq is required to exercise the workflow's dispatch filter")
	}
	workflow := readActionsWorkflow(t, "signoff-plan.yml")
	queueScript := workflowStepScript(t, workflow, "plan", "Build dispatch queue")
	sha := "abcdef0123456789abcdef0123456789abcdef01"

	tests := []struct {
		name       string
		requested  string
		resolved   string
		runs       []map[string]interface{}
		wantQueued bool
	}{
		{
			name:       "plain head moves after an older success",
			requested:  "head",
			resolved:   "v2.16-" + sha + "-head",
			runs:       []map[string]interface{}{successfulWorkflowRun("Run head / framework-regression")},
			wantQueued: true,
		},
		{
			name:       "minor head moves after an older success",
			requested:  "v2.14-head",
			resolved:   "v2.14-" + sha + "-head",
			runs:       []map[string]interface{}{successfulWorkflowRun("Run v2.14-head / framework-regression")},
			wantQueued: true,
		},
		{
			name:      "active mutable head remains suppressed",
			requested: "v2.14-head",
			resolved:  "v2.14-" + sha + "-head",
			runs: []map[string]interface{}{
				{"databaseId": 22, "status": "in_progress", "conclusion": "", "displayTitle": "Run v2.14-head / framework-regression", "event": "workflow_dispatch", "headBranch": "main"},
			},
			wantQueued: false,
		},
		{
			name:       "immutable community head success is reused",
			requested:  "v2.15-abcdef0-head",
			resolved:   "v2.15-abcdef0-head",
			runs:       []map[string]interface{}{successfulWorkflowRun("Run v2.15-abcdef0-head / framework-regression")},
			wantQueued: false,
		},
		{
			name:       "immutable Prime head success is reused",
			requested:  "v2.14.5-abcdef0-head",
			resolved:   "v2.14.5-abcdef0-head",
			runs:       []map[string]interface{}{successfulWorkflowRun("Run v2.14.5-abcdef0-head / framework-regression")},
			wantQueued: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw, queued := runDispatchQueueFixture(t, queueScript, test.requested, test.resolved, test.runs)
			if len(raw) != 1 {
				t.Fatalf("raw queue length = %d, want 1", len(raw))
			}
			if raw[0]["rancher_version"] != test.requested || raw[0]["resolved_rancher_version"] != test.resolved {
				t.Fatalf("dispatch identity = %#v", raw[0])
			}
			if got := len(queued) == 1; got != test.wantQueued {
				t.Fatalf("queued = %t, want %t; queue=%#v", got, test.wantQueued, queued)
			}
		})
	}
}

func TestLaneWorkflowUsesParentResolvedTargetWithoutLosingRequestedAlias(t *testing.T) {
	workflow := readActionsWorkflow(t, "run-rancher-signoff-lane.yml")
	job, ok := workflow.Jobs["run-lane"]
	if !ok {
		t.Fatal("run-lane job not found")
	}
	if got := job.Env["REQUESTED_RANCHER_VERSION"]; got != "${{ inputs.rancher_version }}" {
		t.Fatalf("REQUESTED_RANCHER_VERSION = %q", got)
	}
	if got := job.Env["TARGET_RANCHER_VERSION"]; got != "${{ inputs.resolved_rancher_version || inputs.rancher_version }}" {
		t.Fatalf("TARGET_RANCHER_VERSION = %q", got)
	}
	planScript := workflowStepScript(t, workflow, "run-lane", "Generate sign-off plan")
	if !strings.Contains(planScript, `"-rancher-version" "$TARGET_RANCHER_VERSION"`) {
		t.Fatal("lane planner is not invoked with the resolved target environment variable")
	}
	receiptScript := workflowStepScript(t, workflow, "run-lane", "Write lane receipt")
	for _, field := range []string{
		"install_rancher_distro",
		"upgrade_to_rancher_distro",
		"requested_distro",
		"resolved_distro",
		"chart_repo_alias",
		"chart_version",
	} {
		if !strings.Contains(receiptScript, field) {
			t.Fatalf("lane receipt does not retain %s", field)
		}
	}
}

func readActionsWorkflow(t *testing.T, name string) actionsWorkflow {
	t.Helper()
	path := filepath.Join("..", "..", ".github", "workflows", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var workflow actionsWorkflow
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	return workflow
}

func workflowStepScript(t *testing.T, workflow actionsWorkflow, jobName, stepName string) string {
	t.Helper()
	job, ok := workflow.Jobs[jobName]
	if !ok {
		t.Fatalf("job %q not found", jobName)
	}
	for _, step := range job.Steps {
		if step.Name == stepName {
			return step.Run
		}
	}
	t.Fatalf("step %q not found in job %q", stepName, jobName)
	return ""
}

func successfulWorkflowRun(title string) map[string]interface{} {
	return map[string]interface{}{
		"databaseId":   11,
		"status":       "completed",
		"conclusion":   "success",
		"displayTitle": title,
		"event":        "workflow_dispatch",
		"headBranch":   "main",
		"createdAt":    "2026-08-20T12:00:00Z",
		"url":          "https://example.invalid/run/11",
	}
}

func runDispatchQueueFixture(t *testing.T, script, requested, resolved string, runs []map[string]interface{}) ([]map[string]interface{}, []map[string]interface{}) {
	t.Helper()
	workdir := t.TempDir()
	plan := map[string]interface{}{
		"target_version":          requested,
		"resolved_target_version": resolved,
		"previous_version":        "v2.14.4",
		"webhook_image":           "stgregistry.suse.com/rancher/rancher-webhook:v0.10.10-rc.3",
		"signing_policy_input":    "auto",
		"lanes": []map[string]interface{}{
			{"name": "framework-regression"},
		},
	}
	writeJSONFixture(t, filepath.Join(workdir, "signoff-plan.json"), plan)
	runsJSON, err := json.Marshal(runs)
	if err != nil {
		t.Fatal(err)
	}

	binDir := filepath.Join(workdir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ghPath := filepath.Join(binDir, "gh")
	if err := os.WriteFile(ghPath, []byte("#!/bin/sh\nprintf '%s\\n' \"$GH_RUNS_JSON\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("bash", "-c", script)
	command.Dir = workdir
	command.Env = append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GH_RUNS_JSON="+string(runsJSON),
		"DISPATCH_REQUESTED=false",
		"MAX_PARALLEL_LANES=4",
		"RERUN_SUCCESSFUL_LANES=false",
		"RKE2_SERVER_COUNT=3",
		"IGNORE_ACTIVE_RUNNER_ID=",
		"REF_NAME=main",
		"GH_TOKEN=test-token",
		"GITHUB_OUTPUT="+filepath.Join(workdir, "github-output"),
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("dispatch queue script failed: %v\n%s", err, output)
	}

	var raw []map[string]interface{}
	readJSONFixture(t, filepath.Join(workdir, "automation-output", "signoff-dispatch-queue-raw.json"), &raw)
	var queued []map[string]interface{}
	readJSONFixture(t, filepath.Join(workdir, "automation-output", "signoff-dispatch-queue.json"), &queued)
	return raw, queued
}

func writeJSONFixture(t *testing.T, path string, value interface{}) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readJSONFixture(t *testing.T, path string, target interface{}) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatal(err)
	}
}
