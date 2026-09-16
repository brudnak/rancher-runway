package main

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type regressionPlannerWorkflow struct {
	Name        string            `yaml:"name"`
	Permissions map[string]string `yaml:"permissions"`
	Jobs        map[string]struct {
		Uses        string                 `yaml:"uses"`
		With        map[string]interface{} `yaml:"with"`
		Secrets     string                 `yaml:"secrets"`
		Permissions map[string]string      `yaml:"permissions"`
	} `yaml:"jobs"`
}

func TestRegressionPlannerWrapsSharedPlannerWithFixedLane(t *testing.T) {
	raw := readWorkflowSource(t, "plan-rancher-regression.yml")
	var workflow regressionPlannerWorkflow
	if err := yaml.Unmarshal([]byte(raw), &workflow); err != nil {
		t.Fatalf("parse regression planner workflow: %v", err)
	}
	if workflow.Name != "Plan Rancher Framework Regression" {
		t.Fatalf("workflow name = %q", workflow.Name)
	}
	if got := workflow.Permissions["actions"]; got != "write" {
		t.Fatalf("actions permission = %q, want write", got)
	}
	if got := workflow.Permissions["contents"]; got != "read" {
		t.Fatalf("contents permission = %q, want read", got)
	}
	if len(workflow.Jobs) != 1 {
		t.Fatalf("job count = %d, want 1", len(workflow.Jobs))
	}
	job, ok := workflow.Jobs["plan-regression"]
	if !ok {
		t.Fatal("plan-regression job not found")
	}
	if job.Uses != "./.github/workflows/signoff-plan.yml" {
		t.Fatalf("reusable workflow = %q", job.Uses)
	}
	if got := job.Permissions["actions"]; got != "write" {
		t.Fatalf("reusable caller actions permission = %q, want write", got)
	}
	if got := job.Permissions["contents"]; got != "read" {
		t.Fatalf("reusable caller contents permission = %q, want read", got)
	}
	if got := qaseStringValue(job.With["lane_filter"]); got != "framework-regression" {
		t.Fatalf("lane_filter = %q, want framework-regression", got)
	}
	if got := qaseStringValue(job.With["dispatch_runs"]); got != "${{ inputs.dispatch_runs }}" {
		t.Fatalf("dispatch_runs = %q", got)
	}
	if job.Secrets != "inherit" {
		t.Fatalf("secrets forwarding = %q, want inherit", job.Secrets)
	}
	if strings.Contains(raw, "run-rancher-signoff-lane.yml") || strings.Contains(raw, "gh workflow run") {
		t.Fatal("regression entry point bypasses the shared planner")
	}
}

func TestSharedPlannerExposesOnlyAReusableRegressionFilter(t *testing.T) {
	raw := readWorkflowSource(t, "signoff-plan.yml")
	for _, marker := range []string{
		"workflow_call:",
		"lane_filter:",
		`LANE_FILTER: ${{ inputs.lane_filter || '' }}`,
		`select((env.LANE_FILTER == "") or (.name == env.LANE_FILTER))`,
	} {
		if !strings.Contains(raw, marker) {
			t.Errorf("shared planner omits %q", marker)
		}
	}
}
