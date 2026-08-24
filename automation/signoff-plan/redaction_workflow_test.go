package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type scopedActionsWorkflow struct {
	Jobs map[string]scopedActionsJob `yaml:"jobs"`
}

type scopedActionsJob struct {
	Env   map[string]string   `yaml:"env"`
	Steps []scopedActionsStep `yaml:"steps"`
}

type scopedActionsStep struct {
	Name string                 `yaml:"name"`
	ID   string                 `yaml:"id"`
	If   string                 `yaml:"if"`
	Env  map[string]string      `yaml:"env"`
	With map[string]interface{} `yaml:"with"`
	Run  string                 `yaml:"run"`
}

func TestLaneWorkflowProtectsLogSensitiveConfiguration(t *testing.T) {
	workflow := readActionsWorkflow(t, "run-rancher-signoff-lane.yml")
	job, ok := workflow.Jobs["run-lane"]
	if !ok {
		t.Fatal("run-lane job not found")
	}

	protected := []string{
		"AWS_AUTOMATION_ROLE_ARN",
		"TF_STATE_BUCKET",
		"TF_STATE_LOCK_TABLE",
		"TF_STATE_REGION",
		"AWS_REGION",
		"AWS_VPC",
		"AWS_SUBNET_A",
		"AWS_SUBNET_B",
		"AWS_SUBNET_C",
		"AWS_AMI",
		"AWS_PREFIX",
		"AWS_SUBNET_ID",
		"AWS_SECURITY_GROUP_ID",
		"AWS_PEM_KEY_NAME",
		"AWS_ROUTE53_FQDN",
		"OWNER_FIRST_NAME",
		"OWNER_LAST_NAME",
		"RANCHER_BOOTSTRAP_PASSWORD",
		"LINODE_TOKEN",
		"DOCKERHUB_USERNAME",
		"DOCKERHUB_PASSWORD",
	}
	assertProtectedValuesNotAtJobScope(t, job.Env, protected...)
	raw := readWorkflowSource(t, "run-rancher-signoff-lane.yml")
	for _, name := range protected {
		if strings.Contains(raw, "${{ vars."+name+" }}") {
			t.Errorf("workflow retains an unmasked vars.%s fallback", name)
		}
	}

	required := []string{
		"TF_STATE_BUCKET",
		"TF_STATE_LOCK_TABLE",
		"TF_STATE_REGION",
		"AWS_REGION",
		"AWS_VPC",
		"AWS_SUBNET_A",
		"AWS_SUBNET_B",
		"AWS_SUBNET_C",
		"AWS_AMI",
		"AWS_SUBNET_ID",
		"AWS_SECURITY_GROUP_ID",
		"AWS_PEM_KEY_NAME",
		"AWS_ROUTE53_FQDN",
		"OWNER_FIRST_NAME",
		"OWNER_LAST_NAME",
		"RANCHER_BOOTSTRAP_PASSWORD",
	}
	assertStepEnvSecrets(t, workflow, "run-lane", "Validate protected configuration", required...)
	assertStepEnvSecrets(t, workflow, "run-lane", "Generate sign-off plan", "AWS_PREFIX")
	assertStepEnvSecrets(t, workflow, "run-lane", "Render lane config",
		"RANCHER_BOOTSTRAP_PASSWORD",
		"AWS_REGION",
		"AWS_PREFIX",
		"AWS_VPC",
		"AWS_SUBNET_A",
		"AWS_SUBNET_B",
		"AWS_SUBNET_C",
		"AWS_AMI",
		"AWS_SUBNET_ID",
		"AWS_SECURITY_GROUP_ID",
		"AWS_PEM_KEY_NAME",
		"AWS_ROUTE53_FQDN",
		"OWNER_FIRST_NAME",
		"OWNER_LAST_NAME",
	)
	assertSecretsOnlyReachSteps(t, "run-rancher-signoff-lane.yml", "run-lane", laneSecretStepAllowlist())

	validation := workflowStepScript(t, workflow, "run-lane", "Validate protected configuration")
	for _, name := range required {
		if !strings.Contains(validation, name) {
			t.Errorf("protected configuration validation omits %s", name)
		}
	}
	if strings.Contains(validation, "\n            AWS_PREFIX\n") {
		t.Error("optional AWS_PREFIX is incorrectly required")
	}
	if got := strings.Count(raw, "mask-aws-account-id: true"); got != 2 {
		t.Errorf("AWS account-id masks = %d, want 2", got)
	}
}

func TestLaneWorkflowMasksDerivedValuesAndSuppressesAPIResponses(t *testing.T) {
	workflow := readActionsWorkflow(t, "run-rancher-signoff-lane.yml")
	render := workflowStepScript(t, workflow, "run-lane", "Render lane config")
	maskAt := strings.Index(render, `echo "::add-mask::$value"`)
	exportAt := strings.Index(render, `cat lane.env >> "$GITHUB_ENV"`)
	if maskAt < 0 || exportAt < 0 || maskAt >= exportAt {
		t.Fatalf("derived values must be masked before lane.env enters GITHUB_ENV:\n%s", render)
	}
	for _, name := range []string{"TF_STATE_KEY", "SIGNOFF_AWS_PREFIX"} {
		if !strings.Contains(render, name) {
			t.Errorf("derived mask omits %s", name)
		}
	}

	generate := workflowStepScript(t, workflow, "run-lane", "Generate sign-off plan")
	if !strings.Contains(generate, `"$RUNNER_TEMP/signoff-plan" "${args[@]}" > /dev/null`) {
		t.Error("lane planner still writes its raw plan to the workflow log")
	}
	testsScript := workflowStepScript(t, workflow, "run-lane", "Run Rancher tests")
	for _, setting := range []string{
		`-X PUT "https://${RANCHER_HOST}/v3/settings/server-url"`,
		`-X PUT "https://${RANCHER_HOST}/v3/settings/shell-image"`,
	} {
		settingAt := strings.Index(testsScript, setting)
		if settingAt < 0 {
			t.Fatalf("Rancher setting update %s not found", setting)
		}
		tail := testsScript[settingAt:]
		if outputAt := strings.Index(tail, "--output /dev/null"); outputAt < 0 || outputAt > 500 {
			t.Errorf("Rancher setting update %s does not suppress its response body", setting)
		}
	}

	validate := workflowStepScript(t, workflow, "run-lane", "Validate generated config")
	summary := workflowStepScript(t, workflow, "run-lane", "Write step summary")
	if strings.Contains(validate, "Terraform state key") || strings.Contains(validate, `echo "${TF_STATE_KEY`) {
		t.Error("validation step still prints the Terraform state key")
	}
	if strings.Contains(summary, "Terraform state key") || strings.Contains(summary, "TF_STATE_KEY") {
		t.Error("summary step still prints the Terraform state key")
	}
	receipt := workflowStepScript(t, workflow, "run-lane", "Write lane receipt")
	for _, field := range []string{"terraform_state_key", "aws_prefix"} {
		if strings.Contains(receipt, field) {
			t.Errorf("public lane receipt retains %s", field)
		}
	}
}

func TestLaneWorkflowIsolatesExternalRancherTests(t *testing.T) {
	workflow := readActionsWorkflow(t, "run-rancher-signoff-lane.yml")
	scoped := readScopedActionsWorkflow(t, "run-rancher-signoff-lane.yml")
	job, ok := scoped.Jobs["run-lane"]
	if !ok {
		t.Fatal("run-lane job not found")
	}

	_, checkout := scopedStepByName(t, job, "Checkout repository")
	if got := stringValue(checkout.With["persist-credentials"]); got != "false" {
		t.Errorf("Checkout repository persist-credentials = %q, want false", got)
	}
	if value, exists := job.Env["GH_TOKEN"]; exists {
		t.Errorf("GH_TOKEN remains job-scoped as %q", value)
	}
	githubTokenExpression := "${{ github.token }}"
	for _, step := range job.Steps {
		if scopedStepContains(step, githubTokenExpression) && step.Name != "Generate sign-off plan" {
			t.Errorf("github.token reaches unexpected step %q", step.Name)
		}
	}
	_, generatePlan := scopedStepByName(t, job, "Generate sign-off plan")
	if got := generatePlan.Env["GH_TOKEN"]; got != githubTokenExpression {
		t.Errorf("Generate sign-off plan GH_TOKEN = %q, want %q", got, githubTokenExpression)
	}

	buildAt := workflowStepIndex(t, workflow, "run-lane", "Build automation tools")
	renderAt := workflowStepIndex(t, workflow, "run-lane", "Render lane config")
	configureAt := workflowStepIndex(t, workflow, "run-lane", "Configure AWS credentials")
	setupAt := workflowStepIndex(t, workflow, "run-lane", "Run lane setup")
	if !(buildAt < renderAt && renderAt < configureAt) {
		t.Errorf("trusted preparation order is build=%d render=%d configure=%d", buildAt, renderAt, configureAt)
	}
	if configureAt+1 != setupAt {
		t.Errorf("initial AWS credential step index = %d, want immediately before setup at %d", configureAt, setupAt)
	}

	boundaryAt, boundary := scopedStepByID(t, job, "external_test_boundary")
	testsAt, externalTests := scopedStepByID(t, job, "rancher_tests")
	restoreAt, restore := scopedStepByID(t, job, "restore_trusted_config")
	refreshAt, _ := scopedStepByName(t, job, "Refresh AWS credentials before cleanup")
	deleteAt, deleteDownstream := scopedStepByID(t, job, "delete_downstream")
	cleanupAt, _ := scopedStepByID(t, job, "cleanup")
	if !(boundaryAt < testsAt && testsAt < restoreAt && restoreAt < refreshAt && refreshAt < deleteAt && deleteAt < cleanupAt) {
		t.Errorf("external-test boundary order is boundary=%d tests=%d restore=%d refresh=%d delete=%d cleanup=%d",
			boundaryAt, testsAt, restoreAt, refreshAt, deleteAt, cleanupAt)
	}
	if !strings.Contains(externalTests.If, "steps.external_test_boundary.outcome == 'success'") {
		t.Errorf("external tests are not gated on a successful boundary: %q", externalTests.If)
	}
	if strings.Contains(externalTests.Run, "run-with-cancel-cleanup.sh") {
		t.Error("external tests invoke cleanup before trusted configuration restoration")
	}
	if !strings.Contains(externalTests.Run, `env -i "${external_env[@]}" "$go_bin/gotestsum"`) {
		t.Error("external tests do not invoke gotestsum through the allowlisted environment")
	}
	for _, marker := range []string{
		`external_home="$RUNNER_TEMP/rancher-tests-home"`,
		`external_tmp="$RUNNER_TEMP/rancher-tests-tmp"`,
		`external_gopath="$RUNNER_TEMP/rancher-tests-gopath"`,
		`chmod 700 "$external_home" "$external_tmp" "$external_gopath"`,
	} {
		if !strings.Contains(externalTests.Run, marker) {
			t.Errorf("external tests do not create an isolated runtime with %q", marker)
		}
	}
	externalEnv := shellArrayBody(t, externalTests.Run, "external_env")
	for _, assignment := range []string{
		`"HOME=$external_home"`,
		`"TMPDIR=$external_tmp"`,
		`"GOPATH=$external_gopath"`,
	} {
		if !strings.Contains(externalEnv, assignment) {
			t.Errorf("external_env omits isolated assignment %s", assignment)
		}
	}
	for _, forbidden := range []string{
		"ACTIONS_",
		"AWS_",
		"GH_",
		"GITHUB_",
		"LINODE_",
		"DOCKER",
		"TF_",
		"OWNER_",
		"RANCHER_BOOTSTRAP_PASSWORD",
		"SIGNOFF_AWS_PREFIX",
	} {
		if strings.Contains(strings.ToUpper(externalEnv), forbidden) {
			t.Errorf("external_env contains protected name or prefix %q:\n%s", forbidden, externalEnv)
		}
	}
	if !strings.Contains(restore.If, "always()") || !strings.Contains(restore.If, "steps.render_config.outcome == 'success'") {
		t.Errorf("trusted configuration restore is not an unconditional post-test guard: %q", restore.If)
	}

	for _, name := range []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", "AWS_REGION"} {
		if !strings.Contains(boundary.Run, name) {
			t.Errorf("external-test boundary does not clear %s", name)
		}
		if value, exists := externalTests.Env[name]; !exists || value != "" {
			t.Errorf("external tests env %s = %q (present=%t), want an explicit empty override", name, value, exists)
		}
	}
	if !strings.Contains(boundary.Run, `>> "$GITHUB_ENV"`) {
		t.Error("external-test boundary does not persist cleared credentials through GITHUB_ENV")
	}
	for _, path := range []string{
		`$GITHUB_WORKSPACE/tool-config.yml`,
		`$GITHUB_WORKSPACE/signoff-plan.json`,
		`$GITHUB_WORKSPACE/lane.env`,
		`$HA_RANCHER_TF_MODULE_DIR/backend.tf`,
		`$HA_RANCHER_TF_MODULE_DIR/terraform.tfvars`,
		`$HA_RANCHER_TF_MODULE_DIR/terraform.tfstate`,
		`$HA_RANCHER_TF_MODULE_DIR/terraform.tfstate.backup`,
		`$HA_RANCHER_TF_DATA_DIR/terraform.tfstate`,
		`$HA_RANCHER_TF_DATA_DIR/terraform.tfstate.backup`,
	} {
		if !strings.Contains(boundary.Run, path) {
			t.Errorf("external-test boundary does not remove %s", path)
		}
	}
	if !strings.Contains(boundary.Run, `rm -f -- "${sensitive_files[@]}"`) || !strings.Contains(boundary.Run, `if [ -e "$path" ]`) {
		t.Error("external-test boundary does not remove and verify every sensitive file")
	}

	render := workflowStepScript(t, workflow, "run-lane", "Render lane config")
	for _, marker := range []string{
		`trusted_config_dir="$RUNNER_TEMP/trusted-lane-config"`,
		`install -d -m 0700 "$trusted_config_dir"`,
		`install -m 0600 signoff-plan.json "$trusted_config_dir/signoff-plan.json"`,
		`install -m 0600 tool-config.yml "$trusted_config_dir/tool-config.yml"`,
		`install -m 0600 lane.env "$trusted_config_dir/lane.env"`,
	} {
		if !strings.Contains(render, marker) {
			t.Errorf("Render lane config does not create the trusted file stash with %q", marker)
		}
	}
	for _, marker := range []string{
		`$trusted_config_dir/signoff-plan.json`,
		`$GITHUB_WORKSPACE/signoff-plan.json`,
		`$trusted_config_dir/tool-config.yml`,
		`$GITHUB_WORKSPACE/tool-config.yml`,
		`$trusted_config_dir/lane.env`,
		`$restored_lane_env`,
		`rmdir -- "$trusted_config_dir"`,
	} {
		if !strings.Contains(restore.Run, marker) {
			t.Errorf("trusted restore does not handle %q", marker)
		}
	}
	for name, value := range restore.Env {
		if !strings.HasSuffix(name, "_SHA256") || !strings.Contains(value, "_sha256") {
			t.Errorf("restore env contains non-digest payload %s=%q", name, value)
		}
	}
	raw := strings.ToLower(readWorkflowSource(t, "run-rancher-signoff-lane.yml"))
	for _, forbidden := range []string{"_b64", "base64 --decode", "base64 -w"} {
		if strings.Contains(raw, forbidden) {
			t.Errorf("workflow sends trusted configuration through an encoded payload (%s)", forbidden)
		}
	}
	if stringMapContains(deleteDownstream.Env, "${{ secrets.LINODE_TOKEN }}") {
		t.Error("downstream deletion unnecessarily receives LINODE_TOKEN")
	}

	clearAfterAt, clearAfter := scopedStepByName(t, job, "Clear AWS credentials after cleanup")
	reportAt, _ := scopedStepByName(t, job, "Render sign-off report")
	receiptAt, _ := scopedStepByName(t, job, "Write lane receipt")
	uploadAt, _ := scopedStepByName(t, job, "Upload lane receipt")
	if !(cleanupAt < clearAfterAt && clearAfterAt < reportAt && clearAfterAt < receiptAt && clearAfterAt < uploadAt) {
		t.Errorf("post-cleanup credential boundary order is cleanup=%d clear=%d report=%d receipt=%d upload=%d",
			cleanupAt, clearAfterAt, reportAt, receiptAt, uploadAt)
	}
	if !strings.Contains(clearAfter.If, "always()") {
		t.Errorf("post-cleanup credential clearing is not unconditional: %q", clearAfter.If)
	}
	for _, name := range []string{
		"AWS_ACCESS_KEY_ID",
		"AWS_SECRET_ACCESS_KEY",
		"AWS_SESSION_TOKEN",
		"AWS_SECURITY_TOKEN",
		"AWS_REGION",
		"AWS_DEFAULT_REGION",
	} {
		if !strings.Contains(clearAfter.Run, `echo "`+name+`="`) {
			t.Errorf("post-cleanup credential boundary does not blank %s", name)
		}
	}
	if !strings.Contains(clearAfter.Run, `>> "$GITHUB_ENV"`) {
		t.Error("post-cleanup credential boundary does not persist cleared values through GITHUB_ENV")
	}
}

func TestPlanWorkflowRedactsInternalRoutingFields(t *testing.T) {
	workflow := readActionsWorkflow(t, "signoff-plan.yml")
	job, ok := workflow.Jobs["plan"]
	if !ok {
		t.Fatal("plan job not found")
	}
	assertProtectedValuesNotAtJobScope(t, job.Env, "AWS_PREFIX")
	assertStepEnvSecrets(t, workflow, "plan", "Generate sign-off plan", "AWS_PREFIX")
	assertSecretsOnlyReachSteps(t, "signoff-plan.yml", "plan", map[string][]string{
		"AWS_PREFIX": {"Generate sign-off plan"},
	})
	generate := workflowStepScript(t, workflow, "plan", "Generate sign-off plan")
	if !strings.Contains(generate, `"$RUNNER_TEMP/signoff-plan" "${args[@]}" > /dev/null`) {
		t.Error("planner still writes its raw plan to the workflow log")
	}
	for _, stepName := range []string{"Write step summary", "Write plan receipt"} {
		script := workflowStepScript(t, workflow, "plan", stepName)
		if !strings.Contains(script, ".github/scripts/redact-signoff-plan.jq") {
			t.Errorf("%s does not apply the public plan redaction filter", stepName)
		}
	}

	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq is required to exercise the public plan redaction filter")
	}
	fixture := `{
		"target_version":"v2.15.1-head",
		"run_id":"123",
		"state_key_root":"root",
		"plans":[{"aws_prefix":"gha-owner-123","lanes":[{"terraform_state_key":"private/key.tfstate","name":"framework-regression"}]}]
	}`
	filterPath := filepath.Join("..", "..", ".github", "scripts", "redact-signoff-plan.jq")
	command := exec.Command("jq", "-c", "-f", filterPath)
	command.Stdin = strings.NewReader(fixture)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("redaction filter failed: %v\n%s", err, output)
	}
	var redacted any
	if err := json.Unmarshal(output, &redacted); err != nil {
		t.Fatalf("parse redacted fixture: %v", err)
	}
	for _, key := range []string{"terraform_state_key", "state_key_root", "aws_prefix", "run_id"} {
		if containsJSONKey(redacted, key) {
			t.Errorf("redacted fixture retains %s: %s", key, output)
		}
	}
	if !strings.Contains(string(output), "framework-regression") || !strings.Contains(string(output), "v2.15.1-head") {
		t.Fatalf("redaction removed public audit fields: %s", output)
	}
}

func TestBootstrapWorkflowUsesProtectedBackendConfiguration(t *testing.T) {
	workflow := readActionsWorkflow(t, "bootstrap-terraform-state.yml")
	job, ok := workflow.Jobs["bootstrap"]
	if !ok {
		t.Fatal("bootstrap job not found")
	}
	protected := []string{"AWS_BOOTSTRAP_ROLE_ARN", "AWS_REGION", "TF_STATE_BUCKET", "TF_STATE_LOCK_TABLE"}
	assertProtectedValuesNotAtJobScope(t, job.Env, protected...)
	assertStepEnvSecrets(t, workflow, "bootstrap", "Validate protected configuration", protected...)
	assertStepEnvSecrets(t, workflow, "bootstrap", "Terraform plan", "AWS_REGION", "TF_STATE_BUCKET", "TF_STATE_LOCK_TABLE")
	assertSecretsOnlyReachSteps(t, "bootstrap-terraform-state.yml", "bootstrap", map[string][]string{
		"AWS_BOOTSTRAP_ROLE_ARN": {"Validate protected configuration", "Configure AWS credentials"},
		"AWS_REGION":             {"Validate protected configuration", "Configure AWS credentials", "Terraform plan"},
		"TF_STATE_BUCKET":        {"Validate protected configuration", "Terraform plan"},
		"TF_STATE_LOCK_TABLE":    {"Validate protected configuration", "Terraform plan"},
	})
	raw := readWorkflowSource(t, "bootstrap-terraform-state.yml")
	for _, exposed := range []string{
		"inputs.aws_region",
		"inputs.state_bucket_name",
		"inputs.lock_table_name",
		"backend.env",
		"terraform-backend-env",
	} {
		if strings.Contains(raw, exposed) {
			t.Errorf("bootstrap workflow retains exposed backend path %q", exposed)
		}
	}
	if !strings.Contains(raw, "mask-aws-account-id: true") {
		t.Error("bootstrap workflow does not mask the AWS account ID")
	}
	applyAt := workflowStepIndex(t, workflow, "bootstrap", "Terraform apply")
	clearAt := workflowStepIndex(t, workflow, "bootstrap", "Clear AWS credentials")
	if clearAt <= applyAt {
		t.Errorf("Clear AWS credentials step index = %d, must follow Terraform apply at %d", clearAt, applyAt)
	}
	clearScript := workflowStepScript(t, workflow, "bootstrap", "Clear AWS credentials")
	for _, name := range []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", "AWS_REGION"} {
		if !strings.Contains(clearScript, `echo "`+name+`="`) {
			t.Errorf("Clear AWS credentials does not blank %s", name)
		}
	}
	if !strings.Contains(clearScript, `>> "$GITHUB_ENV"`) {
		t.Error("Clear AWS credentials does not persist cleared values through GITHUB_ENV")
	}
	summary := workflowStepScript(t, workflow, "bootstrap", "Write step summary")
	for _, name := range []string{"TF_STATE_BUCKET", "TF_STATE_LOCK_TABLE", "AWS_REGION"} {
		if strings.Contains(summary, "$"+name) {
			t.Errorf("bootstrap summary prints %s", name)
		}
	}
}

func readWorkflowSource(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("..", "..", ".github", "workflows", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func readScopedActionsWorkflow(t *testing.T, name string) scopedActionsWorkflow {
	t.Helper()
	data := []byte(readWorkflowSource(t, name))
	var workflow scopedActionsWorkflow
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	return workflow
}

func assertProtectedValuesNotAtJobScope(t *testing.T, env map[string]string, names ...string) {
	t.Helper()
	for _, name := range names {
		if value, ok := env[name]; ok && strings.TrimSpace(value) != "" {
			t.Errorf("protected %s remains job-scoped as %q", name, value)
		}
	}
}

func assertStepEnvSecrets(t *testing.T, workflow actionsWorkflow, jobName, stepName string, names ...string) {
	t.Helper()
	job, ok := workflow.Jobs[jobName]
	if !ok {
		t.Fatalf("job %q not found", jobName)
	}
	for _, step := range job.Steps {
		if step.Name != stepName {
			continue
		}
		for _, name := range names {
			want := "${{ secrets." + name + " }}"
			if got := step.Env[name]; got != want {
				t.Errorf("%s env %s = %q, want %q", stepName, name, got, want)
			}
		}
		return
	}
	t.Fatalf("step %q not found in job %q", stepName, jobName)
}

func workflowStepIndex(t *testing.T, workflow actionsWorkflow, jobName, stepName string) int {
	t.Helper()
	job, ok := workflow.Jobs[jobName]
	if !ok {
		t.Fatalf("job %q not found", jobName)
	}
	for index, step := range job.Steps {
		if step.Name == stepName {
			return index
		}
	}
	t.Fatalf("step %q not found in job %q", stepName, jobName)
	return -1
}

func assertSecretsOnlyReachSteps(t *testing.T, workflowName, jobName string, allowed map[string][]string) {
	t.Helper()
	workflow := readScopedActionsWorkflow(t, workflowName)
	job, ok := workflow.Jobs[jobName]
	if !ok {
		t.Fatalf("job %q not found", jobName)
	}

	for secret, allowedSteps := range allowed {
		expression := "${{ secrets." + secret + " }}"
		if stringMapContains(job.Env, expression) {
			t.Errorf("%s is referenced from job-level env", secret)
		}
		seen := map[string]bool{}
		for _, step := range job.Steps {
			if !scopedStepContains(step, expression) {
				continue
			}
			if !slices.Contains(allowedSteps, step.Name) {
				t.Errorf("%s reaches unexpected step %q", secret, step.Name)
			}
			seen[step.Name] = true
		}
		for _, stepName := range allowedSteps {
			if !seen[stepName] {
				t.Errorf("%s is not referenced by required step %q", secret, stepName)
			}
		}
	}
}

func laneSecretStepAllowlist() map[string][]string {
	backendSteps := []string{
		"Validate protected configuration",
		"Run lane setup",
		"Wait for lane readiness",
		"Export local suite env",
		"Provision downstream Linode K3s",
		"Override local webhook image",
		"Override downstream webhook image",
		"Run Rancher upgrade",
		"Wait for webhook chart rollout",
		"Run lane cleanup",
	}
	configSteps := []string{"Validate protected configuration", "Render lane config"}
	return map[string][]string{
		"AWS_AUTOMATION_ROLE_ARN": {"Configure AWS credentials", "Refresh AWS credentials before cleanup"},
		"AWS_REGION":              {"Validate protected configuration", "Render lane config", "Configure AWS credentials", "Refresh AWS credentials before cleanup"},
		"AWS_PREFIX":              {"Generate sign-off plan", "Render lane config"},
		"AWS_VPC":                 configSteps,
		"AWS_SUBNET_A":            configSteps,
		"AWS_SUBNET_B":            configSteps,
		"AWS_SUBNET_C":            configSteps,
		"AWS_AMI":                 configSteps,
		"AWS_SUBNET_ID":           configSteps,
		"AWS_SECURITY_GROUP_ID":   configSteps,
		"AWS_PEM_KEY_NAME":        configSteps,
		"AWS_ROUTE53_FQDN":        configSteps,
		"OWNER_FIRST_NAME":        configSteps,
		"OWNER_LAST_NAME":         configSteps,
		"RANCHER_BOOTSTRAP_PASSWORD": {
			"Validate protected configuration",
			"Render lane config",
		},
		"TF_STATE_BUCKET":     backendSteps,
		"TF_STATE_LOCK_TABLE": backendSteps,
		"TF_STATE_REGION":     backendSteps,
		"DOCKERHUB_USERNAME":  {"Run lane setup", "Run Rancher upgrade"},
		"DOCKERHUB_PASSWORD":  {"Run lane setup", "Run Rancher upgrade"},
		"LINODE_TOKEN":        {"Validate Linode token", "Provision downstream Linode K3s"},
	}
}

func scopedStepByName(t *testing.T, job scopedActionsJob, stepName string) (int, scopedActionsStep) {
	t.Helper()
	for index, step := range job.Steps {
		if step.Name == stepName {
			return index, step
		}
	}
	t.Fatalf("step %q not found", stepName)
	return -1, scopedActionsStep{}
}

func scopedStepByID(t *testing.T, job scopedActionsJob, stepID string) (int, scopedActionsStep) {
	t.Helper()
	for index, step := range job.Steps {
		if step.ID == stepID {
			return index, step
		}
	}
	t.Fatalf("step id %q not found", stepID)
	return -1, scopedActionsStep{}
}

func scopedStepContains(step scopedActionsStep, target string) bool {
	if strings.Contains(step.Run, target) || stringMapContains(step.Env, target) {
		return true
	}
	for _, value := range step.With {
		if strings.Contains(stringValue(value), target) {
			return true
		}
	}
	return false
}

func stringMapContains(values map[string]string, target string) bool {
	for _, value := range values {
		if strings.Contains(value, target) {
			return true
		}
	}
	return false
}

func stringValue(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	default:
		data, _ := json.Marshal(typed)
		return string(data)
	}
}

func shellArrayBody(t *testing.T, script, name string) string {
	t.Helper()
	marker := name + "=("
	inside := false
	entries := []string{}
	for _, line := range strings.Split(script, "\n") {
		trimmed := strings.TrimSpace(line)
		if !inside {
			if trimmed == marker {
				inside = true
			}
			continue
		}
		if trimmed == ")" {
			return strings.Join(entries, "\n")
		}
		entries = append(entries, trimmed)
	}
	t.Fatalf("shell array %q not found or not terminated", name)
	return ""
}

func containsJSONKey(value any, target string) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == target || containsJSONKey(child, target) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if containsJSONKey(child, target) {
				return true
			}
		}
	}
	return false
}
