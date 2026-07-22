package test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
)

func TestPanelStarterToolConfigCreatesAutoModeTemplate(t *testing.T) {
	repoRoot := t.TempDir()

	configPath, created, err := ensureStarterToolConfigForPanel(repoRoot)
	if err != nil {
		t.Fatalf("unexpected error creating starter config: %v", err)
	}
	if !created {
		t.Fatal("expected starter config to be created")
	}

	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("expected starter config to exist: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected starter config mode 0600, got %v", info.Mode().Perm())
	}

	if err := setupConfigE(repoRoot); err != nil {
		t.Fatalf("expected starter config to be readable: %v", err)
	}
	if got := viper.GetString("rancher.mode"); got != "auto" {
		t.Fatalf("expected rancher.mode auto, got %q", got)
	}
	if got := viper.GetString("rancher.version"); got != "" {
		t.Fatalf("expected blank rancher.version, got %q", got)
	}
	if got := viper.GetString("rancher.distro"); got != "auto" {
		t.Fatalf("expected rancher.distro auto, got %q", got)
	}
	if got := viper.GetInt("total_has"); got != 1 {
		t.Fatalf("expected total_has 1, got %d", got)
	}
	if got := viper.GetString("tf_vars.aws_prefix"); got != "" {
		t.Fatalf("expected blank aws_prefix, got %q", got)
	}

	panel := &localControlPanel{repoRoot: repoRoot, configPath: configPath}
	item := panel.checkSetupConfigState()
	if item.Status != "error" {
		t.Fatalf("expected blank starter config to block setup, got %#v", item)
	}
	if !strings.Contains(item.Detail, "rancher.version") {
		t.Fatalf("expected missing rancher.version in detail, got %q", item.Detail)
	}
	if !strings.Contains(item.Detail, "tf_vars.aws_prefix") {
		t.Fatalf("expected missing aws_prefix in detail, got %q", item.Detail)
	}
}

func TestPanelStarterToolConfigDoesNotOverwriteExistingConfig(t *testing.T) {
	repoRoot := t.TempDir()
	configPath := filepath.Join(repoRoot, "tool-config.yml")
	existing := "rancher:\n  mode: manual\ntotal_has: 7\n"
	if err := os.WriteFile(configPath, []byte(existing), 0o600); err != nil {
		t.Fatalf("failed to write existing config: %v", err)
	}

	gotPath, created, err := ensureStarterToolConfigForPanel(repoRoot)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created {
		t.Fatal("expected existing config to be preserved")
	}
	if gotPath != configPath {
		t.Fatalf("expected config path %q, got %q", configPath, gotPath)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}
	if string(data) != existing {
		t.Fatalf("expected existing config to remain unchanged, got %q", string(data))
	}
}

func TestSetupConfigDoesNotCreateMissingConfigOutsidePanel(t *testing.T) {
	repoRoot := t.TempDir()

	err := setupConfigE(repoRoot)
	if err == nil {
		t.Fatal("expected missing config to fail outside panel bootstrap")
	}
	if !strings.Contains(err.Error(), "tool-config.yml") {
		t.Fatalf("expected missing tool-config.yml error, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(repoRoot, "tool-config.yml")); !os.IsNotExist(statErr) {
		t.Fatalf("expected setupConfigE not to create config, stat err=%v", statErr)
	}
}

func TestPanelSetupConfigPreflightAcceptsFilledAutoConfig(t *testing.T) {
	repoRoot := t.TempDir()
	configPath := filepath.Join(repoRoot, "tool-config.yml")
	config := `rancher:
  mode: auto
  version: "2.13-head"
  distro: auto
  bootstrap_password: "change-me-now"
  auto_approve: false
rke2:
  preload_images: true
total_has: 1
user:
  first_name: "Ada"
  last_name: "Lovelace"
tf_vars:
  aws_region: ""
  aws_prefix: "ATB"
  aws_vpc: "vpc-123"
  aws_subnet_a: "subnet-a"
  aws_subnet_b: "subnet-b"
  aws_subnet_c: "subnet-c"
  aws_ami: "ami-123"
  aws_subnet_id: "subnet-a"
  aws_security_group_id: "sg-123"
  aws_pem_key_name: "qa-key"
  aws_route53_fqdn: "qa.example.com"
  custom_hostname_prefix: ""
`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
	if err := setupConfigE(repoRoot); err != nil {
		t.Fatalf("expected config to load: %v", err)
	}

	panel := &localControlPanel{repoRoot: repoRoot, configPath: configPath}
	item := panel.checkSetupConfigState()
	if item.Status != "ok" {
		t.Fatalf("expected filled config to pass, got %#v", item)
	}
}

func TestCurrentRunRecordCapturesSingleRunWorkspaceMetadata(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("GITHUB_WORKSPACE", workspace)
	t.Setenv("TF_STATE_BUCKET", "")
	t.Setenv("TF_STATE_LOCK_TABLE", "")
	t.Setenv("TF_STATE_REGION", "")
	t.Setenv("TF_STATE_KEY", "")

	repoRoot := filepath.Join(workspace, "repo")
	testDir := filepath.Join(repoRoot, "terratest")
	if err := os.MkdirAll(testDir, 0o755); err != nil {
		t.Fatalf("failed to create test dir: %v", err)
	}

	viper.Reset()
	viper.Set("total_has", 1)
	viper.Set("rancher.version", "2.13-head")
	viper.Set("tf_vars.aws_prefix", "atb")
	viper.Set("tf_vars.aws_route53_fqdn", "qa.example.com")
	viper.Set("tf_vars.custom_hostname_prefix", "fresh")
	viper.Set("user.first_name", "Ada")
	viper.Set("user.last_name", "Lovelace")

	panel := &localControlPanel{
		repoRoot: repoRoot,
		testDir:  testDir,
		totalHAs: 1,
	}
	now := time.Date(2026, 5, 6, 23, 0, 0, 0, time.UTC)
	panel.createCurrentRunRecord("abc12345", now)

	record, ok := panel.readCurrentRunRecord()
	if !ok {
		t.Fatal("expected current run record")
	}
	if record.RunID != "abc12345" {
		t.Fatalf("expected run id, got %q", record.RunID)
	}
	if record.Status != "setup_running" {
		t.Fatalf("expected setup_running status, got %q", record.Status)
	}
	if !strings.HasPrefix(record.TerraformBackend, "local (") {
		t.Fatalf("expected local backend, got %q", record.TerraformBackend)
	}
	wantHAOutputRoot := filepath.Join(testDir, "automation-output", "runs", "abc12345", "ha")
	if record.HAOutputRoot != wantHAOutputRoot {
		t.Fatalf("expected HA output root %q, got %q", wantHAOutputRoot, record.HAOutputRoot)
	}
	wantTerraformStatePath := filepath.Join(testDir, "automation-output", "runs", "abc12345", "terraform", "terraform.tfstate")
	if record.TerraformStatePath != wantTerraformStatePath {
		t.Fatalf("expected Terraform state path %q, got %q", wantTerraformStatePath, record.TerraformStatePath)
	}
	wantTerraformModuleDir := filepath.Join(testDir, "automation-output", "runs", "abc12345", "terraform", "module")
	if record.TerraformModuleDir != wantTerraformModuleDir {
		t.Fatalf("expected Terraform module dir %q, got %q", wantTerraformModuleDir, record.TerraformModuleDir)
	}
	wantTerraformDataDir := filepath.Join(testDir, "automation-output", "runs", "abc12345", "terraform", ".terraform")
	if record.TerraformDataDir != wantTerraformDataDir {
		t.Fatalf("expected Terraform data dir %q, got %q", wantTerraformDataDir, record.TerraformDataDir)
	}
	if got := strings.Join(record.RancherVersions, ","); got != "2.13-head" {
		t.Fatalf("expected rancher version, got %q", got)
	}

	state := panel.workspaceState()
	if state.CurrentRun == nil {
		t.Fatal("expected workspace state to include current run")
	}
	if state.CanStartFresh {
		t.Fatal("expected current run record to block fresh start in single-run workspace")
	}

	panel.updateCurrentRunStatus("ready")
	record, ok = panel.readCurrentRunRecord()
	if !ok || record.Status != "ready" {
		t.Fatalf("expected ready current run record, got %#v ok=%v", record, ok)
	}

	panel.removeCurrentRunRecord()
	if _, ok := panel.readCurrentRunRecord(); ok {
		t.Fatal("expected current run record to be removed")
	}
}

func TestIsolatedRunStartStatusAllowsUniqueCustomHostname(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("GITHUB_WORKSPACE", filepath.Join(workspace, "output"))
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("DOCKERHUB_USERNAME", "test")
	t.Setenv("DOCKERHUB_PASSWORD", "test")

	repoRoot := filepath.Join(workspace, "repo")
	testDir := filepath.Join(repoRoot, "terratest")
	if err := os.MkdirAll(filepath.Join(repoRoot, "modules", "aws"), 0o755); err != nil {
		t.Fatalf("failed to create terraform dir: %v", err)
	}
	if err := os.MkdirAll(testDir, 0o755); err != nil {
		t.Fatalf("failed to create terratest dir: %v", err)
	}
	configPath := filepath.Join(repoRoot, "tool-config.yml")
	if err := os.WriteFile(configPath, []byte("total_has: 1\n"), 0o600); err != nil {
		t.Fatalf("failed to write config marker: %v", err)
	}

	viper.Reset()
	viper.Set("total_has", 1)
	viper.Set("rancher.mode", "auto")
	viper.Set("rancher.version", "2.13-head")
	viper.Set("rancher.distro", "auto")
	viper.Set("rancher.bootstrap_password", "change-me")
	viper.Set("tf_vars.aws_prefix", "atb")
	viper.Set("tf_vars.aws_vpc", "vpc-123")
	viper.Set("tf_vars.aws_subnet_a", "subnet-a")
	viper.Set("tf_vars.aws_subnet_b", "subnet-b")
	viper.Set("tf_vars.aws_subnet_c", "subnet-c")
	viper.Set("tf_vars.aws_ami", "ami-123")
	viper.Set("tf_vars.aws_subnet_id", "subnet-a")
	viper.Set("tf_vars.aws_security_group_id", "sg-123")
	viper.Set("tf_vars.aws_pem_key_name", "qa-key")
	viper.Set("tf_vars.aws_route53_fqdn", "qa.example.com")
	viper.Set("tf_vars.custom_hostname_prefix", "fixed")
	viper.Set("user.first_name", "Ada")
	viper.Set("user.last_name", "Lovelace")

	panel := &localControlPanel{
		repoRoot:           repoRoot,
		testDir:            testDir,
		configPath:         configPath,
		totalHAs:           1,
		operations:         newPanelOperations(),
		readinessCollector: readySystemReadinessForTest,
	}

	ok, reason := panel.isolatedRunStartStatus()
	if !ok {
		t.Fatalf("expected unique custom hostname to allow isolated run, got %q", reason)
	}
}

func TestIsolatedRunStartStatusBlocksDuplicateCustomHostname(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("GITHUB_WORKSPACE", filepath.Join(workspace, "output"))
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("DOCKERHUB_USERNAME", "test")
	t.Setenv("DOCKERHUB_PASSWORD", "test")

	repoRoot := filepath.Join(workspace, "repo")
	testDir := filepath.Join(repoRoot, "terratest")
	if err := os.MkdirAll(filepath.Join(repoRoot, "modules", "aws"), 0o755); err != nil {
		t.Fatalf("failed to create terraform dir: %v", err)
	}
	if err := os.MkdirAll(testDir, 0o755); err != nil {
		t.Fatalf("failed to create terratest dir: %v", err)
	}
	configPath := filepath.Join(repoRoot, "tool-config.yml")
	if err := os.WriteFile(configPath, []byte("total_has: 1\n"), 0o600); err != nil {
		t.Fatalf("failed to write config marker: %v", err)
	}

	viper.Reset()
	viper.Set("total_has", 1)
	viper.Set("rancher.mode", "auto")
	viper.Set("rancher.version", "2.13-head")
	viper.Set("rancher.distro", "auto")
	viper.Set("rancher.bootstrap_password", "change-me")
	viper.Set("tf_vars.aws_prefix", "atb")
	viper.Set("tf_vars.aws_vpc", "vpc-123")
	viper.Set("tf_vars.aws_subnet_a", "subnet-a")
	viper.Set("tf_vars.aws_subnet_b", "subnet-b")
	viper.Set("tf_vars.aws_subnet_c", "subnet-c")
	viper.Set("tf_vars.aws_ami", "ami-123")
	viper.Set("tf_vars.aws_subnet_id", "subnet-a")
	viper.Set("tf_vars.aws_security_group_id", "sg-123")
	viper.Set("tf_vars.aws_pem_key_name", "qa-key")
	viper.Set("tf_vars.aws_route53_fqdn", "qa.example.com")
	viper.Set("tf_vars.custom_hostname_prefix", "fixed")
	viper.Set("user.first_name", "Ada")
	viper.Set("user.last_name", "Lovelace")

	panel := &localControlPanel{
		repoRoot:           repoRoot,
		testDir:            testDir,
		configPath:         configPath,
		totalHAs:           1,
		operations:         newPanelOperations(),
		readinessCollector: readySystemReadinessForTest,
	}
	panel.writeRunRecord(panelRunRecord{
		RunID:                "abc12345",
		SlotID:               "slot-abc12345",
		SlotName:             "Run abc12345",
		Status:               "ready",
		CreatedAt:            time.Now(),
		UpdatedAt:            time.Now(),
		TotalHAs:             1,
		Route53FQDN:          "qa.example.com",
		CustomHostnamePrefix: "fixed",
	})

	ok, reason := panel.isolatedRunStartStatus()
	if ok {
		t.Fatal("expected duplicate custom hostname to block isolated run")
	}
	if !strings.Contains(reason, "fixed.qa.example.com") || !strings.Contains(reason, "abc12345") {
		t.Fatalf("expected duplicate custom hostname reason, got %q", reason)
	}
}

func TestIsolatedRunStartStatusAllowsCleanGeneratedHostnameConfig(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("GITHUB_WORKSPACE", filepath.Join(workspace, "output"))
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("DOCKERHUB_USERNAME", "test")
	t.Setenv("DOCKERHUB_PASSWORD", "test")

	repoRoot := filepath.Join(workspace, "repo")
	testDir := filepath.Join(repoRoot, "terratest")
	if err := os.MkdirAll(filepath.Join(repoRoot, "modules", "aws"), 0o755); err != nil {
		t.Fatalf("failed to create terraform dir: %v", err)
	}
	if err := os.MkdirAll(testDir, 0o755); err != nil {
		t.Fatalf("failed to create terratest dir: %v", err)
	}
	configPath := filepath.Join(repoRoot, "tool-config.yml")
	if err := os.WriteFile(configPath, []byte("total_has: 1\n"), 0o600); err != nil {
		t.Fatalf("failed to write config marker: %v", err)
	}

	viper.Reset()
	viper.Set("total_has", 1)
	viper.Set("rancher.mode", "auto")
	viper.Set("rancher.version", "2.13-head")
	viper.Set("rancher.distro", "auto")
	viper.Set("rancher.bootstrap_password", "change-me")
	viper.Set("tf_vars.aws_prefix", "atb")
	viper.Set("tf_vars.aws_vpc", "vpc-123")
	viper.Set("tf_vars.aws_subnet_a", "subnet-a")
	viper.Set("tf_vars.aws_subnet_b", "subnet-b")
	viper.Set("tf_vars.aws_subnet_c", "subnet-c")
	viper.Set("tf_vars.aws_ami", "ami-123")
	viper.Set("tf_vars.aws_subnet_id", "subnet-a")
	viper.Set("tf_vars.aws_security_group_id", "sg-123")
	viper.Set("tf_vars.aws_pem_key_name", "qa-key")
	viper.Set("tf_vars.aws_route53_fqdn", "qa.example.com")
	viper.Set("tf_vars.custom_hostname_prefix", "")
	viper.Set("user.first_name", "Ada")
	viper.Set("user.last_name", "Lovelace")

	panel := &localControlPanel{
		repoRoot:           repoRoot,
		testDir:            testDir,
		configPath:         configPath,
		totalHAs:           1,
		operations:         newPanelOperations(),
		readinessCollector: readySystemReadinessForTest,
	}

	ok, reason := panel.isolatedRunStartStatus()
	if !ok {
		t.Fatalf("expected clean generated hostname config to allow isolated run, got %q", reason)
	}
}

func TestIsolatedRunStartStatusAllowsExistingIsolatedRunRecords(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("GITHUB_WORKSPACE", filepath.Join(workspace, "output"))
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("DOCKERHUB_USERNAME", "test")
	t.Setenv("DOCKERHUB_PASSWORD", "test")

	repoRoot := filepath.Join(workspace, "repo")
	testDir := filepath.Join(repoRoot, "terratest")
	if err := os.MkdirAll(filepath.Join(repoRoot, "modules", "aws"), 0o755); err != nil {
		t.Fatalf("failed to create terraform dir: %v", err)
	}
	if err := os.MkdirAll(testDir, 0o755); err != nil {
		t.Fatalf("failed to create terratest dir: %v", err)
	}
	configPath := filepath.Join(repoRoot, "tool-config.yml")
	if err := os.WriteFile(configPath, []byte("total_has: 1\n"), 0o600); err != nil {
		t.Fatalf("failed to write config marker: %v", err)
	}

	viper.Reset()
	viper.Set("total_has", 1)
	viper.Set("rancher.mode", "auto")
	viper.Set("rancher.version", "2.13-head")
	viper.Set("rancher.distro", "auto")
	viper.Set("rancher.bootstrap_password", "change-me")
	viper.Set("tf_vars.aws_prefix", "atb")
	viper.Set("tf_vars.aws_vpc", "vpc-123")
	viper.Set("tf_vars.aws_subnet_a", "subnet-a")
	viper.Set("tf_vars.aws_subnet_b", "subnet-b")
	viper.Set("tf_vars.aws_subnet_c", "subnet-c")
	viper.Set("tf_vars.aws_ami", "ami-123")
	viper.Set("tf_vars.aws_subnet_id", "subnet-a")
	viper.Set("tf_vars.aws_security_group_id", "sg-123")
	viper.Set("tf_vars.aws_pem_key_name", "qa-key")
	viper.Set("tf_vars.aws_route53_fqdn", "qa.example.com")
	viper.Set("tf_vars.custom_hostname_prefix", "")
	viper.Set("user.first_name", "Ada")
	viper.Set("user.last_name", "Lovelace")

	panel := &localControlPanel{
		repoRoot:           repoRoot,
		testDir:            testDir,
		configPath:         configPath,
		totalHAs:           1,
		operations:         newPanelOperations(),
		readinessCollector: readySystemReadinessForTest,
	}
	panel.createCurrentRunRecord("abc12345", time.Now())

	ok, reason := panel.isolatedRunStartStatus()
	if !ok {
		t.Fatalf("expected existing isolated run record to allow a new run slot, got %q", reason)
	}

	state := panel.workspaceState()
	if len(state.Runs) != 1 {
		t.Fatalf("expected workspace to expose one run slot, got %#v", state.Runs)
	}
	if state.Runs[0].SlotID != "slot-abc12345" {
		t.Fatalf("expected slot-scoped id, got %q", state.Runs[0].SlotID)
	}
	if !state.CanStartIsolatedRun {
		t.Fatalf("expected workspace to allow another isolated run slot, got %q", state.IsolatedRunBlockedReason)
	}
}

func readySystemReadinessForTest(string) systemReadinessState {
	return systemReadinessState{Ready: true, Summary: "Ready for test"}
}

func TestTerraformBackendConfigFromEnvEmptyUsesLocalState(t *testing.T) {
	t.Setenv("TF_STATE_BUCKET", "")
	t.Setenv("TF_STATE_LOCK_TABLE", "")
	t.Setenv("TF_STATE_REGION", "")
	t.Setenv("TF_STATE_KEY", "")

	backendConfig, err := terraformBackendConfigFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if backendConfig != nil {
		t.Fatalf("expected nil backend config, got %#v", backendConfig)
	}
}

func TestTerraformBackendConfigUsesPanelLocalStatePath(t *testing.T) {
	t.Setenv("TF_STATE_BUCKET", "")
	t.Setenv("TF_STATE_LOCK_TABLE", "")
	t.Setenv("TF_STATE_REGION", "")
	t.Setenv("TF_STATE_KEY", "")
	t.Setenv(terraformStatePathEnv, "/tmp/ha-rancher/terraform.tfstate")

	backendConfig, err := terraformBackendConfigFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := backendConfig["path"]; got != "/tmp/ha-rancher/terraform.tfstate" {
		t.Fatalf("expected local backend path, got %#v", backendConfig)
	}
}

func TestSyncTerraformBackendFileWritesLocalBackendForPanelState(t *testing.T) {
	tempDir := t.TempDir()
	terraformDir := filepath.Join(tempDir, "modules", "aws")
	if err := os.MkdirAll(terraformDir, 0o755); err != nil {
		t.Fatalf("failed to create terraform dir: %v", err)
	}

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get original dir: %v", err)
	}
	if err := os.Chdir(filepath.Join(tempDir, "terratest")); err != nil {
		if mkdirErr := os.MkdirAll(filepath.Join(tempDir, "terratest"), 0o755); mkdirErr != nil {
			t.Fatalf("failed to create terratest dir: %v", mkdirErr)
		}
		if chdirErr := os.Chdir(filepath.Join(tempDir, "terratest")); chdirErr != nil {
			t.Fatalf("failed to chdir: %v", chdirErr)
		}
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Fatalf("failed to restore original dir: %v", err)
		}
	})

	if err := syncTerraformBackendFile(map[string]interface{}{"path": "/tmp/run.tfstate"}); err != nil {
		t.Fatalf("unexpected sync error: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(terraformDir, "backend.tf"))
	if err != nil {
		t.Fatalf("failed to read backend.tf: %v", err)
	}
	if !strings.Contains(string(data), `backend "local"`) {
		t.Fatalf("expected local backend, got %s", data)
	}
}

func TestTerraformBackendConfigFromEnvRequiresAllValues(t *testing.T) {
	t.Setenv("TF_STATE_BUCKET", "bucket")
	t.Setenv("TF_STATE_LOCK_TABLE", "")
	t.Setenv("TF_STATE_REGION", "us-east-2")
	t.Setenv("TF_STATE_KEY", "state.tfstate")

	if _, err := terraformBackendConfigFromEnv(); err == nil {
		t.Fatal("expected error for incomplete backend config")
	}
}

func TestTerraformBackendConfigFromEnvBuildsS3Backend(t *testing.T) {
	t.Setenv("TF_STATE_BUCKET", "bucket")
	t.Setenv("TF_STATE_LOCK_TABLE", "locks")
	t.Setenv("TF_STATE_REGION", "us-east-2")
	t.Setenv("TF_STATE_KEY", "rancher-runway/signoff/v2.14/v2.14.1-alpha6/123/webhook-fresh-install/terraform.tfstate")

	backendConfig, err := terraformBackendConfigFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := map[string]interface{}{
		"bucket":         "bucket",
		"key":            "rancher-runway/signoff/v2.14/v2.14.1-alpha6/123/webhook-fresh-install/terraform.tfstate",
		"region":         "us-east-2",
		"dynamodb_table": "locks",
		"encrypt":        true,
	}

	for key, want := range expected {
		if got := backendConfig[key]; got != want {
			t.Fatalf("backendConfig[%s]: expected %#v, got %#v", key, want, got)
		}
	}
}

func TestSetupConfigAppliesRunScopedOverrides(t *testing.T) {
	t.Cleanup(viper.Reset)
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "tool-config.yml")
	if err := os.WriteFile(configPath, []byte(`deployment:
  type: ha-rke2
total_has: 1
rancher:
  version: 2.14-head
tf_vars:
  aws_prefix: atb
  aws_route53_fqdn: current.example.com
`), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	t.Setenv(runDeploymentTypeEnv, deploymentTypeLinodeDocker)
	t.Setenv(runTotalHAsEnv, "2")
	t.Setenv(runRancherVersionsEnv, "2.13-head,2.14.1-alpha3")
	t.Setenv(runAWSPrefixEnv, "atb-rabc12345")
	t.Setenv(runRoute53FQDNEnv, "recorded.example.com")

	if err := setupConfigE(tempDir); err != nil {
		t.Fatalf("setupConfigE failed: %v", err)
	}
	if got := deploymentType(); got != deploymentTypeLinodeDocker {
		t.Fatalf("expected deployment override %q, got %q", deploymentTypeLinodeDocker, got)
	}
	if got := configuredRancherInstanceCount(); got != 2 {
		t.Fatalf("expected total override 2, got %d", got)
	}
	if got := linodeRancherVersions(2); len(got) != 2 || got[0] != "2.13-head" || got[1] != "2.14.1-alpha3" {
		t.Fatalf("expected recorded versions, got %#v", got)
	}
	if got := viper.GetString("tf_vars.aws_prefix"); got != "atb-rabc12345" {
		t.Fatalf("expected recorded prefix, got %q", got)
	}
	if got := viper.GetString("tf_vars.aws_route53_fqdn"); got != "recorded.example.com" {
		t.Fatalf("expected recorded route53 domain, got %q", got)
	}
}

func TestTerraformAWSPrefixForRunPreservesRecordedRunPrefix(t *testing.T) {
	if got := terraformAWSPrefixForRun("atb-rabc12345", "abc12345"); got != "atb-rabc12345" {
		t.Fatalf("expected recorded run prefix to be preserved, got %q", got)
	}
}

func TestTerraformBackendConfigAddsRunSuffixToS3Key(t *testing.T) {
	t.Setenv("TF_STATE_BUCKET", "bucket")
	t.Setenv("TF_STATE_LOCK_TABLE", "locks")
	t.Setenv("TF_STATE_REGION", "us-east-2")
	t.Setenv("TF_STATE_KEY", "rancher-runway/local/terraform.tfstate")
	t.Setenv("HA_RANCHER_RUN_ID", "ABC_123")

	backendConfig, err := terraformBackendConfigFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "rancher-runway/local/runs/abc_123/terraform.tfstate"
	if got := backendConfig["key"]; got != want {
		t.Fatalf("expected key %q, got %#v", want, backendConfig["key"])
	}
}

func TestTerraformEnvVarsUsesPanelDataDir(t *testing.T) {
	t.Setenv(terraformDataDirEnv, "/tmp/ha-rancher/.terraform")
	envVars := terraformEnvVarsFromEnv()
	if got := envVars["TF_DATA_DIR"]; got != "/tmp/ha-rancher/.terraform" {
		t.Fatalf("expected TF_DATA_DIR, got %#v", envVars)
	}
}

func TestTerraformAWSPrefixAddsPanelRunID(t *testing.T) {
	t.Setenv(runIDEnv, "8B95AE13-extra")

	if got := terraformAWSPrefix("ATB"); got != "atb-r8b95ae13" {
		t.Fatalf("expected run scoped prefix, got %q", got)
	}
}

func TestTerraformAWSPrefixKeepsBasePrefixWithoutRun(t *testing.T) {
	t.Setenv(runIDEnv, "")

	if got := terraformAWSPrefix("ATB"); got != "atb" {
		t.Fatalf("expected base prefix, got %q", got)
	}
}

func TestPanelNonInteractiveModeReadsEnv(t *testing.T) {
	t.Setenv(panelNonInteractiveEnv, "true")

	if !panelNonInteractiveMode() {
		t.Fatal("expected panel non-interactive mode")
	}
}

func TestTerraformModuleDirUsesPanelRunModule(t *testing.T) {
	t.Setenv(terraformModuleDirEnv, "/tmp/ha-rancher/module")

	if got := terraformModuleDir(); got != "/tmp/ha-rancher/module" {
		t.Fatalf("expected Terraform module dir from env, got %q", got)
	}
}

func TestPrepareTerraformModuleForRunCopiesOnlySourceFiles(t *testing.T) {
	workspace := t.TempDir()
	repoRoot := filepath.Join(workspace, "repo")
	testDir := filepath.Join(repoRoot, "terratest")
	sourceDir := filepath.Join(repoRoot, "modules", "aws")
	if err := os.MkdirAll(filepath.Join(sourceDir, "modules", "child"), 0o755); err != nil {
		t.Fatalf("failed to create source module: %v", err)
	}
	for name, contents := range map[string]string{
		"main.tf":                  "resource test",
		"variables.tf":             "variable test",
		"modules/child/main.tf":    "child",
		"backend.tf":               "generated backend",
		"terraform.tfvars":         "generated vars",
		".terraform.lock.hcl":      "lock",
		"terraform.tfstate":        "state",
		"terraform.tfstate.backup": "backup",
	} {
		path := filepath.Join(sourceDir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("failed to create parent for %s: %v", name, err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatalf("failed to write %s: %v", name, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(sourceDir, ".terraform", "providers"), 0o755); err != nil {
		t.Fatalf("failed to create .terraform: %v", err)
	}

	panel := &localControlPanel{repoRoot: repoRoot, testDir: testDir}
	if err := panel.prepareTerraformModuleForRun("ABC123"); err != nil {
		t.Fatalf("unexpected prepare error: %v", err)
	}

	targetDir := panel.terraformModuleDirForRun("ABC123")
	for _, name := range []string{"main.tf", "variables.tf", "modules/child/main.tf"} {
		if _, err := os.Stat(filepath.Join(targetDir, name)); err != nil {
			t.Fatalf("expected %s to be copied: %v", name, err)
		}
	}
	for _, name := range []string{"backend.tf", "terraform.tfvars", ".terraform.lock.hcl", "terraform.tfstate", "terraform.tfstate.backup", ".terraform"} {
		if _, err := os.Stat(filepath.Join(targetDir, name)); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be skipped, stat err=%v", name, err)
		}
	}
}
