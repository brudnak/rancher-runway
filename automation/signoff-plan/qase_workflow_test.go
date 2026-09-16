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
	WorkflowDispatch qaseWorkflowDispatchTrigger `yaml:"workflow_dispatch"`
}

type qaseWorkflowDispatchTrigger struct {
	Inputs map[string]qaseWorkflowInput `yaml:"inputs"`
}

type qaseWorkflowInput struct {
	Required bool   `yaml:"required"`
	Type     string `yaml:"type"`
}

type qaseJob struct {
	If          string            `yaml:"if"`
	Needs       string            `yaml:"needs"`
	RunsOn      string            `yaml:"runs-on"`
	Environment string            `yaml:"environment"`
	Permissions map[string]string `yaml:"permissions"`
	Env         map[string]string `yaml:"env"`
	Steps       []qaseStep        `yaml:"steps"`
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
	checkout := qaseStepByName(t, job, "Checkout repository")
	if got := qaseStringValue(checkout.With["ref"]); got != "${{ github.sha }}" {
		t.Errorf("source lane checkout ref = %q, want immutable workflow SHA", got)
	}
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
	if got := qaseStringValue(upload.With["name"]); got != "qase-report-${{ github.run_id }}-${{ github.run_attempt }}" {
		t.Errorf("Qase artifact name = %q, want workflow-run-attempt-scoped name", got)
	}
	if got := qaseStringValue(upload.With["retention-days"]); got != "1" {
		t.Errorf("Qase artifact retention-days = %q, want 1", got)
	}

	dispatchJob := qaseWorkflowJob(t, workflow, "dispatch-qase-report")
	if dispatchJob.Needs != "run-lane" {
		t.Fatalf("Qase dispatch needs = %q, want run-lane", dispatchJob.Needs)
	}
	condition := compactQaseExpression(dispatchJob.If)
	for _, required := range []string{
		"needs.run-lane.result == 'success'",
		"inputs.run_rancher_tests == true",
	} {
		if !strings.Contains(condition, required) {
			t.Errorf("Qase dispatch condition omits %q: %q", required, dispatchJob.If)
		}
	}
	if got := dispatchJob.Permissions["actions"]; got != "write" {
		t.Errorf("Qase dispatch actions permission = %q, want write", got)
	}
	if dispatchJob.Environment != "" {
		t.Fatalf("Qase dispatch unexpectedly enters environment %q", dispatchJob.Environment)
	}
	dispatch := qaseStepByName(t, dispatchJob, "Dispatch verified Qase reporting")
	if got := dispatch.Env["GH_TOKEN"]; got != "${{ github.token }}" {
		t.Fatalf("Qase dispatch GH_TOKEN = %q, want github.token", got)
	}
	for _, marker := range []string{
		"return_run_details: true",
		"source_run_id: $source_run_id",
		"source_run_attempt: $source_run_attempt",
		"source_head_sha: $source_head_sha",
		"actions/workflows/report-successful-signoff-to-qase.yml/dispatches",
		".workflow_run_id",
		".html_url",
	} {
		if !strings.Contains(dispatch.Run, marker) {
			t.Errorf("Qase dispatch step omits %q", marker)
		}
	}
	if strings.Contains(dispatch.Run, "actions/runs/$SOURCE_RUN_ID") || strings.Contains(dispatch.Run, "sleep ") {
		t.Fatal("source workflow must dispatch and finish instead of waiting for its reporter")
	}
}

func TestQaseReportWorkflowVerifiesACompletedSuccessfulLocalSource(t *testing.T) {
	raw := readWorkflowSource(t, qaseReportWorkflowName)
	workflow := readQaseWorkflowContract(t, qaseReportWorkflowName)
	if strings.Contains(raw, "workflow_run:") {
		t.Fatal("Qase reporter retains the GITHUB_TOKEN-suppressed workflow_run trigger")
	}
	inputs := workflow.On.WorkflowDispatch.Inputs
	if len(inputs) != 3 {
		t.Fatalf("workflow_dispatch input count = %d, want 3", len(inputs))
	}
	for _, name := range []string{"source_run_id", "source_run_attempt", "source_head_sha"} {
		input, ok := inputs[name]
		if !ok {
			t.Errorf("workflow_dispatch input %q is missing", name)
			continue
		}
		if !input.Required || input.Type != "string" {
			t.Errorf("workflow_dispatch input %q = %#v, want required string", name, input)
		}
	}

	validate := qaseWorkflowJob(t, workflow, "validate-source")
	if validate.Environment != "" {
		t.Fatalf("source validation unexpectedly enters environment %q", validate.Environment)
	}
	if got := validate.Permissions["actions"]; got != "read" {
		t.Errorf("source validation actions permission = %q, want read", got)
	}
	validation := qaseStepByName(t, validate, "Wait for and validate source conclusion")
	for _, required := range []string{
		`[ "$REPORT_ACTOR" != "github-actions[bot]" ]`,
		"actions/runs/$REPORT_RUN_ID/attempts/$previous_attempt",
		`[ "$previous_conclusion" = "success" ]`,
		"actions/runs/$SOURCE_RUN_ID",
		`[ "$actual_id" != "$SOURCE_RUN_ID" ]`,
		`[ "$actual_attempt" != "$SOURCE_RUN_ATTEMPT" ]`,
		`[ "$actual_sha" != "$SOURCE_HEAD_SHA" ]`,
		`[ "$actual_repository" != "$GITHUB_REPOSITORY" ]`,
		`[ "$actual_head_repository" != "$GITHUB_REPOSITORY" ]`,
		`[ "$actual_event" != "workflow_dispatch" ]`,
		`[ "$actual_path" != ".github/workflows/run-rancher-signoff-lane.yml" ]`,
		`[ "$actual_conclusion" != "success" ]`,
	} {
		if !strings.Contains(validation.Run, required) {
			t.Errorf("source validation omits %q", required)
		}
	}

	report := qaseWorkflowJob(t, workflow, "report")
	if report.Needs != "validate-source" {
		t.Fatalf("Qase report needs = %q, want validate-source", report.Needs)
	}
}

func TestQaseReportWorkflowPinsReporterAndKeepsSecretAtFinalBoundary(t *testing.T) {
	raw := readWorkflowSource(t, qaseReportWorkflowName)
	workflow := readQaseWorkflowContract(t, qaseReportWorkflowName)
	job := qaseWorkflowJob(t, workflow, "report")
	if job.Environment != "rancher-signoff" {
		t.Fatalf("Qase report environment = %q, want rancher-signoff", job.Environment)
	}
	if got := job.Permissions["actions"]; got != "read" {
		t.Errorf("Qase report actions permission = %q, want read", got)
	}

	checkout := qaseStepByName(t, job, "Checkout repository")
	if got := qaseStringValue(checkout.With["ref"]); got != "${{ inputs.source_head_sha }}" {
		t.Errorf("Qase checkout ref = %q, want verified source SHA", got)
	}
	download := qaseStepByName(t, job, "Download Qase report input")
	assertImmutableQaseAction(t, download.Uses, "actions/download-artifact")
	if got := qaseStringValue(download.With["name"]); got != "qase-report-${{ inputs.source_run_id }}-${{ inputs.source_run_attempt }}" {
		t.Errorf("downloaded Qase artifact name = %q, want source run and attempt", got)
	}
	if got := qaseStringValue(download.With["run-id"]); got != "${{ inputs.source_run_id }}" {
		t.Errorf("downloaded Qase artifact run-id = %q, want validated source run", got)
	}
	setupGo := qaseStepByName(t, job, "Set up Go")
	if got := qaseStringValue(setupGo.With["cache"]); got != "false" {
		t.Errorf("Qase setup-go cache = %q, want false", got)
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
		t.Fatalf("final Qase step token env = %q, want environment secret", got)
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
	job := qaseWorkflowJob(t, workflow, "report")

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
	if got := report.Env["BUILD_URL"]; got != "${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ inputs.source_run_id }}" {
		t.Errorf("BUILD_URL = %q, want validated source workflow URL", got)
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
