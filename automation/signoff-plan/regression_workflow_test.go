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
		RunsOn      string            `yaml:"runs-on"`
		Uses        string            `yaml:"uses"`
		Permissions map[string]string `yaml:"permissions"`
		Steps       []regressionStep  `yaml:"steps"`
	} `yaml:"jobs"`
}

type regressionStep struct {
	Name string            `yaml:"name"`
	ID   string            `yaml:"id"`
	Env  map[string]string `yaml:"env"`
	Run  string            `yaml:"run"`
}

func TestRegressionPlannerDispatchesSharedPlannerFromOrdinaryJob(t *testing.T) {
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
	if job.RunsOn != "ubuntu-latest" {
		t.Fatalf("runner = %q, want ubuntu-latest", job.RunsOn)
	}
	if job.Uses != "" {
		t.Fatalf("regression launcher still uses reusable workflow %q", job.Uses)
	}
	if got := job.Permissions["actions"]; got != "write" {
		t.Fatalf("launcher actions permission = %q, want write", got)
	}
	if got := job.Permissions["contents"]; got != "read" {
		t.Fatalf("launcher contents permission = %q, want read", got)
	}
	if len(job.Steps) != 3 {
		t.Fatalf("step count = %d, want 3", len(job.Steps))
	}
	dispatch := regressionStepByName(t, job.Steps, "Dispatch regression-only planner")
	if dispatch.ID != "dispatch" {
		t.Fatalf("dispatch step id = %q, want dispatch", dispatch.ID)
	}
	for _, marker := range []string{
		`return_run_details: true`,
		`lane_filter: "framework-regression"`,
		`actions/workflows/signoff-plan.yml/dispatches`,
		`X-GitHub-Api-Version: 2026-03-10`,
		`.workflow_run_id`,
		`.html_url`,
	} {
		if !strings.Contains(dispatch.Run, marker) {
			t.Errorf("dispatch step omits %q", marker)
		}
	}
	wait := regressionStepByName(t, job.Steps, "Wait for regression-only planner")
	for _, marker := range []string{
		`actions/runs/$PLAN_RUN_ID`,
		`if [ "$conclusion" != "success" ]`,
	} {
		if !strings.Contains(wait.Run, marker) {
			t.Errorf("wait step omits %q", marker)
		}
	}
	for _, forbidden := range []string{
		`uses: ./.github/workflows/signoff-plan.yml`,
		`secrets: inherit`,
		`run-rancher-signoff-lane.yml`,
	} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("regression launcher contains forbidden reusable/direct-dispatch marker %q", forbidden)
		}
	}
}

func TestSharedPlannerExposesOnlyADispatchRegressionFilter(t *testing.T) {
	raw := readWorkflowSource(t, "signoff-plan.yml")
	for _, marker := range []string{
		"workflow_dispatch:",
		"lane_filter:",
		`LANE_FILTER: ${{ inputs.lane_filter || '' }}`,
		`select((env.LANE_FILTER == "") or (.name == env.LANE_FILTER))`,
	} {
		if !strings.Contains(raw, marker) {
			t.Errorf("shared planner omits %q", marker)
		}
	}
	if strings.Contains(raw, "workflow_call:") {
		t.Fatal("shared planner retains the failing reusable-workflow entry point")
	}
}

func regressionStepByName(t *testing.T, steps []regressionStep, name string) regressionStep {
	t.Helper()
	for _, step := range steps {
		if step.Name == name {
			return step
		}
	}
	t.Fatalf("step %q not found", name)
	return regressionStep{}
}
