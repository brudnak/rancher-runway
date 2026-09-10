package main

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const (
	qaseReportWorkflowName = "report-successful-signoff-to-qase.yml"
	qaseReporterCommit     = "7ac7356ad7db8d793dc14c7a2a4130772bf833b4"
)

type qaseWorkflowContract struct {
	Name string               `yaml:"name"`
	On   qaseWorkflowTriggers `yaml:"on"`
	Env  map[string]string    `yaml:"env"`
	Jobs map[string]qaseJob   `yaml:"jobs"`
}

type qaseWorkflowTriggers struct {
	WorkflowRun qaseWorkflowRunTrigger `yaml:"workflow_run"`
}

type qaseWorkflowRunTrigger struct {
	Workflows []string `yaml:"workflows"`
	Types     []string `yaml:"types"`
}

type qaseJob struct {
	If    string            `yaml:"if"`
	Env   map[string]string `yaml:"env"`
	Steps []qaseStep        `yaml:"steps"`
}

type qaseStep struct {
	Name string                 `yaml:"name"`
	ID   string                 `yaml:"id"`
	If   string                 `yaml:"if"`
	Uses string                 `yaml:"uses"`
	Env  map[string]string      `yaml:"env"`
	With map[string]interface{} `yaml:"with"`
	Run  string                 `yaml:"run"`
}

func TestLaneWorkflowPublishesQaseInputOnlyAfterSuccess(t *testing.T) {
	const workflowName = "run-rancher-signoff-lane.yml"
	raw := readWorkflowSource(t, workflowName)
	if strings.Contains(raw, "QASE_AUTOMATION_TOKEN") {
		t.Fatal("source lane workflow must never receive the Qase API token")
	}

	workflow := readQaseWorkflowContract(t, workflowName)
	job := qaseWorkflowJob(t, workflow, "run-lane")
	runTests := qaseStepByName(t, job, "Run Rancher tests")
	if !strings.Contains(runTests.Run, `"$go_bin/gotestsum"`) {
		t.Fatal("Run Rancher tests no longer invokes gotestsum through the isolated tool path")
	}
	if !strings.Contains(runTests.Run, `--jsonfile "$go_json"`) {
		t.Fatal("Run Rancher tests must preserve gotestsum JSON for deferred Qase reporting")
	}

	if len(job.Steps) < 2 {
		t.Fatalf("run-lane has %d steps, want at least two", len(job.Steps))
	}
	prepare := job.Steps[len(job.Steps)-2]
	upload := job.Steps[len(job.Steps)-1]
	if prepare.Name != "Prepare Qase report input" {
		t.Fatalf("penultimate run-lane step = %q, want Prepare Qase report input", prepare.Name)
	}
	if upload.Name != "Upload Qase report input" {
		t.Fatalf("final run-lane step = %q, want Upload Qase report input", upload.Name)
	}
	if !strings.Contains(compactQaseExpression(prepare.If), "success()") {
		t.Fatalf("Qase input preparation is not success-gated: %q", prepare.If)
	}
	if !strings.Contains(compactQaseExpression(upload.If), "steps.prepare_qase_report.outcome == 'success'") {
		t.Fatalf("Qase input upload is not gated on successful preparation: %q", upload.If)
	}
	for _, marker := range []string{
		`"$RUNNER_TEMP/qase-report-input"`,
		"-mode prepare",
		"-results-manifest automation-output/rancher-test-results.json",
		`-output-dir "$RUNNER_TEMP/qase-report-input"`,
	} {
		if !strings.Contains(prepare.Run, marker) {
			t.Errorf("Qase input preparation omits %q", marker)
		}
	}
	assertImmutableQaseAction(t, upload.Uses, "actions/upload-artifact")
	if got := qaseStringValue(upload.With["name"]); got != "qase-report-${{ github.run_id }}" {
		t.Errorf("Qase artifact name = %q, want workflow-run-scoped name", got)
	}
	if got := qaseStringValue(upload.With["retention-days"]); got != "1" {
		t.Errorf("Qase artifact retention-days = %q, want 1", got)
	}
}

func TestQaseReportWorkflowRunsOnlyForSuccessfulLocalDispatches(t *testing.T) {
	workflow := readQaseWorkflowContract(t, qaseReportWorkflowName)
	trigger := workflow.On.WorkflowRun
	if len(trigger.Workflows) != 1 || trigger.Workflows[0] != "Run Rancher Sign-Off Lane" {
		t.Fatalf("workflow_run workflows = %#v, want only Run Rancher Sign-Off Lane", trigger.Workflows)
	}
	if len(trigger.Types) != 1 || trigger.Types[0] != "completed" {
		t.Fatalf("workflow_run types = %#v, want only completed", trigger.Types)
	}

	_, job := onlyQaseWorkflowJob(t, workflow)
	condition := compactQaseExpression(job.If)
	for _, required := range []string{
		"github.event.workflow_run.conclusion == 'success'",
		"github.event.workflow_run.event == 'workflow_dispatch'",
		"github.event.workflow_run.head_repository.full_name == github.repository",
	} {
		if !strings.Contains(condition, required) {
			t.Errorf("Qase report job condition omits %q: %q", required, job.If)
		}
	}
}

func TestQaseReportWorkflowPinsReporterAndKeepsSecretAtFinalBoundary(t *testing.T) {
	raw := readWorkflowSource(t, qaseReportWorkflowName)
	workflow := readQaseWorkflowContract(t, qaseReportWorkflowName)
	_, job := onlyQaseWorkflowJob(t, workflow)

	download := qaseStepByName(t, job, "Download Qase report input")
	assertImmutableQaseAction(t, download.Uses, "actions/download-artifact")
	if got := qaseStringValue(download.With["name"]); got != "qase-report-${{ github.event.workflow_run.id }}" {
		t.Errorf("downloaded Qase artifact name = %q, want source workflow run ID", got)
	}

	downloadReporter := qaseStepByName(t, job, "Download pinned reporter-v2 source")
	if got := downloadReporter.Env["RANCHER_TESTS_REPORTER_COMMIT"]; got != qaseReporterCommit {
		t.Fatalf("Qase reporter source commit = %q, want %s", got, qaseReporterCommit)
	}
	if !strings.Contains(downloadReporter.Run, "https://codeload.github.com/rancher/tests/tar.gz/$RANCHER_TESTS_REPORTER_COMMIT") {
		t.Fatal("reporter download URL does not consume the pinned Rancher tests commit")
	}
	buildReporter := qaseStepByName(t, job, "Build reporter-v2")
	for _, step := range []qaseStep{downloadReporter, buildReporter} {
		stepSource := strings.ToLower(fmt.Sprintf("%#v", step))
		for _, forbidden := range []string{"rancher_tests_ref", "rancher-tests-ref", "input_rancher_tests_ref"} {
			if strings.Contains(stepSource, forbidden) {
				t.Errorf("reporter source/build step %q depends on user-selectable %s", step.Name, forbidden)
			}
		}
	}
	for _, step := range job.Steps {
		if strings.HasPrefix(step.Uses, "actions/") {
			assertImmutableQaseAction(t, step.Uses, strings.SplitN(step.Uses, "@", 2)[0])
		}
	}

	if len(job.Steps) == 0 {
		t.Fatal("Qase report job has no steps")
	}
	finalIndex := len(job.Steps) - 1
	finalStep := job.Steps[finalIndex]
	if finalStep.Name != "Report successful run to Qase" {
		t.Fatalf("final Qase report step = %q, want Report successful run to Qase", finalStep.Name)
	}
	secretExpression := "${{ secrets.QASE_AUTOMATION_TOKEN }}"
	if got := finalStep.Env["QASE_AUTOMATION_TOKEN"]; got != secretExpression {
		t.Fatalf("final Qase step token env = %q, want repository secret", got)
	}
	if got := strings.Count(raw, secretExpression); got != 1 {
		t.Fatalf("Qase token secret reference count = %d, want exactly 1", got)
	}
	if qaseStringMapContains(workflow.Env, secretExpression) {
		t.Fatal("Qase token secret must not be workflow-scoped")
	}
	if qaseStringMapContains(job.Env, secretExpression) {
		t.Fatal("Qase token secret must not be job-scoped")
	}
	if strings.Contains(job.If, "QASE_AUTOMATION_TOKEN") {
		t.Fatal("Qase token must not be referenced from the job condition")
	}
	for index, step := range job.Steps {
		if index != finalIndex && (qaseStepContains(step, secretExpression) || qaseStepContains(step, "QASE_AUTOMATION_TOKEN")) {
			t.Errorf("Qase token reaches non-final step %q", step.Name)
		}
	}
}

func TestQaseReportWorkflowVerifiesInputAndFailsClosedOnReporterProblems(t *testing.T) {
	workflow := readQaseWorkflowContract(t, qaseReportWorkflowName)
	_, job := onlyQaseWorkflowJob(t, workflow)

	buildTools := qaseStepByName(t, job, "Build Qase report tools")
	if !strings.Contains(buildTools.Run, `go build -o "$RUNNER_TEMP/qase-tools/qase-report-input" ./automation/qase-report-input`) {
		t.Error("Qase helper is not built from the trusted workflow checkout")
	}

	verify := qaseStepByName(t, job, "Validate Qase report input")
	if verify.ID != "qase_input" {
		t.Errorf("Qase validation step id = %q, want qase_input", verify.ID)
	}
	for _, marker := range []string{
		`"$RUNNER_TEMP/qase-tools/qase-report-input"`,
		"-mode verify",
		`-input-dir "$RUNNER_TEMP/qase-report-input"`,
		`-reporter-root "$RUNNER_TEMP/tests"`,
		`-github-output "$GITHUB_OUTPUT"`,
	} {
		if !strings.Contains(verify.Run, marker) {
			t.Errorf("Qase validation step omits %q", marker)
		}
	}

	report := qaseStepByName(t, job, "Report successful run to Qase")
	if !strings.Contains(compactQaseExpression(report.If), "steps.qase_input.outputs.enabled == 'true'") {
		t.Errorf("final Qase step does not honor helper enabled output: %q", report.If)
	}
	if got := report.Env["EXPECTED_RESULT_COUNT"]; got != "${{ steps.qase_input.outputs.result_count }}" {
		t.Errorf("EXPECTED_RESULT_COUNT = %q, want verified helper result count", got)
	}
	for _, marker := range []string{
		"reporter.log",
		"reporter_status",
		`if [ "$reporter_status" -ne 0 ]`,
		"level=(warning|error|fatal)",
		"Failed to read file: open /app/image-report/image-report.txt",
		`if [ -n "$unexpected_diagnostics" ]`,
		"grep -cF 'Updating run with '",
		"EXPECTED_RESULT_COUNT",
		`if [ "$reported_count" -ne "$EXPECTED_RESULT_COUNT" ]`,
	} {
		if !strings.Contains(report.Run, marker) {
			t.Errorf("final Qase step omits reporter failure/count check %q", marker)
		}
	}
}

func readQaseWorkflowContract(t *testing.T, name string) qaseWorkflowContract {
	t.Helper()
	var workflow qaseWorkflowContract
	if err := yaml.Unmarshal([]byte(readWorkflowSource(t, name)), &workflow); err != nil {
		t.Fatalf("parse workflow %s: %v", name, err)
	}
	return workflow
}

func qaseWorkflowJob(t *testing.T, workflow qaseWorkflowContract, name string) qaseJob {
	t.Helper()
	job, ok := workflow.Jobs[name]
	if !ok {
		t.Fatalf("job %q not found", name)
	}
	return job
}

func onlyQaseWorkflowJob(t *testing.T, workflow qaseWorkflowContract) (string, qaseJob) {
	t.Helper()
	if len(workflow.Jobs) != 1 {
		t.Fatalf("workflow %q has %d jobs, want exactly 1", workflow.Name, len(workflow.Jobs))
	}
	for name, job := range workflow.Jobs {
		return name, job
	}
	panic("unreachable")
}

func qaseStepByName(t *testing.T, job qaseJob, name string) qaseStep {
	t.Helper()
	for _, step := range job.Steps {
		if step.Name == name {
			return step
		}
	}
	t.Fatalf("step %q not found", name)
	return qaseStep{}
}

func compactQaseExpression(value string) string {
	value = strings.ReplaceAll(value, "${{", "")
	value = strings.ReplaceAll(value, "}}", "")
	return strings.Join(strings.Fields(value), " ")
}

func assertImmutableQaseAction(t *testing.T, uses, action string) {
	t.Helper()
	pattern := regexp.MustCompile(`^` + regexp.QuoteMeta(action) + `@[0-9a-f]{40}$`)
	if !pattern.MatchString(uses) {
		t.Errorf("action use %q is not an immutable SHA pin for %s", uses, action)
	}
}

func qaseStringValue(value interface{}) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func qaseStringMapContains(values map[string]string, target string) bool {
	for key, value := range values {
		if strings.Contains(key, target) || strings.Contains(value, target) {
			return true
		}
	}
	return false
}

func qaseStepContains(step qaseStep, target string) bool {
	if strings.Contains(step.Name, target) || strings.Contains(step.ID, target) ||
		strings.Contains(step.If, target) || strings.Contains(step.Uses, target) ||
		strings.Contains(step.Run, target) || qaseStringMapContains(step.Env, target) {
		return true
	}
	for key, value := range step.With {
		if strings.Contains(key, target) || strings.Contains(qaseStringValue(value), target) {
			return true
		}
	}
	return false
}
