package test

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/brudnak/ha-rancher-rke2/terratest/hcl"
	"github.com/brudnak/ha-rancher-rke2/terratest/settings"
	"github.com/gruntwork-io/terratest/modules/logger"
	"github.com/gruntwork-io/terratest/modules/terraform"
	"github.com/spf13/viper"
)

func setupConfig(t *testing.T) {
	t.Helper()
	if err := setupConfigE(""); err != nil {
		t.Fatalf("Failed to read config: %v", err)
	}
}

func setupConfigE(repoRoot string) error {
	viperConfigMu.Lock()
	defer viperConfigMu.Unlock()

	viper.Reset()

	if strings.TrimSpace(repoRoot) == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to determine working directory: %w", err)
		}
		resolvedRoot, _, err := resolveControlPanelPaths(cwd)
		if err != nil {
			return err
		}
		repoRoot = resolvedRoot
	}

	configPath := filepath.Join(repoRoot, "tool-config.yml")
	viper.SetConfigFile(configPath)
	viper.SetConfigType("yml")

	if err := viper.ReadInConfig(); err != nil {
		return err
	}
	return nil
}

func getTerraformOptions(t *testing.T, totalHAs int) *terraform.Options {
	if err := settings.ValidateAWSPrefixConfig(); err != nil {
		t.Fatalf("AWS prefix preflight failed: %v", err)
	}
	if err := settings.ValidateAWSPemKeyNameConfig(); err != nil {
		t.Fatalf("AWS PEM key preflight failed: %v", err)
	}
	if err := settings.ValidateOwnerConfig(); err != nil {
		t.Fatalf("Owner preflight failed: %v", err)
	}
	generateAwsVars()
	customHostnamePrefix, err := settings.ConfiguredCustomHostnamePrefix()
	if err != nil {
		t.Fatalf("Invalid custom Rancher hostname: %v", err)
	}

	backendConfig, err := terraformBackendConfigFromEnv()
	if err != nil {
		t.Fatalf("Invalid Terraform backend environment: %v", err)
	}
	if err := syncTerraformBackendFile(backendConfig); err != nil {
		t.Fatalf("Failed to sync Terraform backend file: %v", err)
	}

	vars := filterTerraformVarsForModule(map[string]interface{}{
		"total_has":                totalHAs,
		"aws_prefix":               terraformAWSPrefix(viper.GetString("tf_vars.aws_prefix")),
		"aws_vpc":                  viper.GetString("tf_vars.aws_vpc"),
		"aws_subnet_a":             viper.GetString("tf_vars.aws_subnet_a"),
		"aws_subnet_b":             viper.GetString("tf_vars.aws_subnet_b"),
		"aws_subnet_c":             viper.GetString("tf_vars.aws_subnet_c"),
		"aws_ami":                  viper.GetString("tf_vars.aws_ami"),
		"aws_subnet_id":            viper.GetString("tf_vars.aws_subnet_id"),
		"aws_security_group_id":    viper.GetString("tf_vars.aws_security_group_id"),
		"aws_pem_key_name":         viper.GetString("tf_vars.aws_pem_key_name"),
		"aws_route53_fqdn":         viper.GetString("tf_vars.aws_route53_fqdn"),
		"custom_hostname_prefix":   customHostnamePrefix,
		"owner_first_name":         settings.OwnerFirstName(),
		"owner_last_name":          settings.OwnerLastName(),
		"run_id":                   currentTerraformRunID(),
		"gpu_worker_enabled":       settings.CurrentGPUWorkerConfig().Enabled,
		"gpu_worker_instance_type": settings.CurrentGPUWorkerConfig().InstanceType,
		"gpu_worker_ami":           settings.CurrentGPUWorkerConfig().AMI,
		"gpu_worker_subnet_id":     settings.CurrentGPUWorkerConfig().SubnetID,
	}, terraformModuleDir())

	options := terraform.WithDefaultRetryableErrors(t, &terraform.Options{
		TerraformDir:  terraformModuleDir(),
		NoColor:       true,
		Lock:          true,
		LockTimeout:   "5m",
		EnvVars:       terraformEnvVarsFromEnv(),
		BackendConfig: backendConfig,
		Vars:          vars,
	})

	if os.Getenv("GITHUB_ACTIONS") == "true" {
		log.Printf("Logging disabled. Terraform logs will be suppressed.")
		logger.Default = logger.Discard
		options.Logger = logger.Discard
	}

	return options
}

var terraformVariableBlockPattern = regexp.MustCompile(`(?m)^\s*variable\s+"([^"]+)"`)

func filterTerraformVarsForModule(vars map[string]interface{}, moduleDir string) map[string]interface{} {
	declared, err := declaredTerraformVariables(moduleDir)
	if err != nil {
		return vars
	}

	filtered := make(map[string]interface{}, len(vars))
	for key, value := range vars {
		if declared[key] {
			filtered[key] = value
		}
	}
	return filtered
}

func declaredTerraformVariables(moduleDir string) (map[string]bool, error) {
	entries, err := os.ReadDir(moduleDir)
	if err != nil {
		return nil, err
	}

	declared := map[string]bool{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".tf") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(moduleDir, entry.Name()))
		if err != nil {
			return nil, err
		}
		for _, match := range terraformVariableBlockPattern.FindAllStringSubmatch(string(data), -1) {
			if len(match) > 1 {
				declared[match[1]] = true
			}
		}
	}
	return declared, nil
}

func terraformBackendConfigFromEnv() (map[string]interface{}, error) {
	return terraformBackendConfigFromEnvForRun(
		strings.TrimSpace(os.Getenv(runIDEnv)),
		strings.TrimSpace(os.Getenv(terraformStatePathEnv)),
	)
}

func validateScopedCleanupTarget() error {
	runID := currentTerraformRunID()
	if runID == "" {
		return fmt.Errorf("%s must be set; cleanup is allowed only for a recorded isolated run", runIDEnv)
	}
	if strings.TrimSpace(os.Getenv(terraformModuleDirEnv)) == "" {
		return fmt.Errorf("%s must be set; cleanup must run from the recorded per-run Terraform module", terraformModuleDirEnv)
	}
	if strings.TrimSpace(os.Getenv(terraformStatePathEnv)) != "" {
		return nil
	}

	values := []string{
		"TF_STATE_BUCKET",
		"TF_STATE_LOCK_TABLE",
		"TF_STATE_REGION",
		"TF_STATE_KEY",
	}
	for _, key := range values {
		if strings.TrimSpace(os.Getenv(key)) == "" {
			return fmt.Errorf("%s must be set unless remote Terraform backend env vars are fully configured", terraformStatePathEnv)
		}
	}
	return nil
}

func terraformBackendConfigFromEnvForRun(runID string, localStatePath string) (map[string]interface{}, error) {
	values := map[string]string{
		"TF_STATE_BUCKET":     strings.TrimSpace(os.Getenv("TF_STATE_BUCKET")),
		"TF_STATE_LOCK_TABLE": strings.TrimSpace(os.Getenv("TF_STATE_LOCK_TABLE")),
		"TF_STATE_REGION":     strings.TrimSpace(os.Getenv("TF_STATE_REGION")),
		"TF_STATE_KEY":        strings.TrimSpace(os.Getenv("TF_STATE_KEY")),
	}

	anySet := false
	var missing []string
	for key, value := range values {
		if value != "" {
			anySet = true
			continue
		}
		missing = append(missing, key)
	}

	if !anySet {
		if localStatePath != "" {
			return map[string]interface{}{"path": localStatePath}, nil
		}
		return nil, nil
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("set all remote backend env vars or none; missing %s", strings.Join(missing, ", "))
	}

	return map[string]interface{}{
		"bucket":         values["TF_STATE_BUCKET"],
		"key":            terraformStateKeyForRun(values["TF_STATE_KEY"], runID),
		"region":         values["TF_STATE_REGION"],
		"dynamodb_table": values["TF_STATE_LOCK_TABLE"],
		"encrypt":        true,
	}, nil
}

func terraformStateKeyForRun(baseKey string, runID string) string {
	baseKey = strings.Trim(strings.TrimSpace(baseKey), "/")
	runID = safeRunPathSegment(runID)
	if baseKey == "" || runID == "" || runID == "unknown" {
		return baseKey
	}

	dir := strings.Trim(strings.TrimSuffix(filepath.Dir(baseKey), "."), "/")
	file := filepath.Base(baseKey)
	if file == "." || file == "/" || file == "" {
		file = "terraform.tfstate"
	}
	if dir == "" {
		return filepath.ToSlash(filepath.Join("runs", runID, file))
	}
	return filepath.ToSlash(filepath.Join(dir, "runs", runID, file))
}

func terraformEnvVarsFromEnv() map[string]string {
	envVars := map[string]string{}
	if dataDir := strings.TrimSpace(os.Getenv(terraformDataDirEnv)); dataDir != "" {
		envVars["TF_DATA_DIR"] = dataDir
	}
	return envVars
}

func terraformAWSPrefix(basePrefix string) string {
	return terraformAWSPrefixForRun(basePrefix, os.Getenv(runIDEnv))
}

func currentTerraformRunID() string {
	runID := safeRunPathSegment(os.Getenv(runIDEnv))
	if runID == "" || runID == "unknown" {
		return ""
	}
	return runID
}

func terraformAWSPrefixForRun(basePrefix string, runIDValue string) string {
	basePrefix = strings.ToLower(strings.TrimSpace(basePrefix))
	if settings.IsAutomationAWSPrefix(basePrefix) {
		return basePrefix
	}
	runID := safeRunPathSegment(runIDValue)
	if runID == "" || runID == "unknown" {
		return basePrefix
	}
	if len(runID) > 8 {
		runID = runID[:8]
	}
	return fmt.Sprintf("%s-r%s", basePrefix, runID)
}

func syncTerraformBackendFile(backendConfig map[string]interface{}) error {
	path := filepath.Join(terraformModuleDir(), "backend.tf")
	if backendConfig == nil {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}

	if _, ok := backendConfig["path"]; ok {
		return os.WriteFile(path, []byte(`terraform {
  backend "local" {}
}
`), 0644)
	}

	return os.WriteFile(path, []byte(`terraform {
  backend "s3" {}
}
`), 0644)
}

func generateAwsVars() {
	hcl.GenAwsVarFile(
		filepath.Join(terraformModuleDir(), "terraform.tfvars"),
		terraformAWSPrefix(viper.GetString("tf_vars.aws_prefix")),
		viper.GetString("tf_vars.aws_vpc"),
		viper.GetString("tf_vars.aws_subnet_a"),
		viper.GetString("tf_vars.aws_subnet_b"),
		viper.GetString("tf_vars.aws_subnet_c"),
		viper.GetString("tf_vars.aws_ami"),
		viper.GetString("tf_vars.aws_subnet_id"),
		viper.GetString("tf_vars.aws_security_group_id"),
		viper.GetString("tf_vars.aws_pem_key_name"),
		viper.GetString("tf_vars.aws_route53_fqdn"),
		settings.CurrentCustomHostnamePrefix(),
		settings.OwnerFirstName(),
		settings.OwnerLastName(),
		currentTerraformRunID(),
	)
}

func getTerraformOutputs(t *testing.T, terraformOptions *terraform.Options) map[string]string {
	outputs, err := getTerraformOutputsE(t, terraformOptions)
	if err != nil {
		t.Fatalf("Failed to get terraform outputs: %v", err)
	}
	return outputs
}

func getTerraformOutputsE(t *testing.T, terraformOptions *terraform.Options) (map[string]string, error) {
	output, err := terraform.OutputJsonE(t, terraformOptions, "flat_outputs")
	if err != nil {
		return nil, err
	}
	var outputs map[string]string
	if err := json.Unmarshal([]byte(output), &outputs); err != nil {
		log.Printf("Raw output: %s", output)
		return nil, fmt.Errorf("failed to parse terraform outputs: %w", err)
	}

	maskTerraformOutputs(outputs)
	return outputs, nil
}

func maskTerraformOutputs(outputs map[string]string) {
	for key, value := range outputs {
		if strings.HasSuffix(key, "_rancher_url") {
			maskGitHubActionsURL(value)
		}
	}
}

func getHAOutputs(instanceNum int, outputs map[string]string) TerraformOutputs {
	prefix := fmt.Sprintf("ha_%d", instanceNum)
	return TerraformOutputs{
		Server1IP:             outputs[fmt.Sprintf("%s_server1_ip", prefix)],
		Server2IP:             outputs[fmt.Sprintf("%s_server2_ip", prefix)],
		Server3IP:             outputs[fmt.Sprintf("%s_server3_ip", prefix)],
		Server1PrivateIP:      outputs[fmt.Sprintf("%s_server1_private_ip", prefix)],
		Server2PrivateIP:      outputs[fmt.Sprintf("%s_server2_private_ip", prefix)],
		Server3PrivateIP:      outputs[fmt.Sprintf("%s_server3_private_ip", prefix)],
		GPUWorkerIP:           outputs[fmt.Sprintf("%s_gpu_worker_ip", prefix)],
		GPUWorkerPrivateIP:    outputs[fmt.Sprintf("%s_gpu_worker_private_ip", prefix)],
		GPUWorkerInstanceType: outputs[fmt.Sprintf("%s_gpu_worker_instance_type", prefix)],
		GPUWorkerAMI:          outputs[fmt.Sprintf("%s_gpu_worker_ami", prefix)],
		GPUWorkerSubnetID:     outputs[fmt.Sprintf("%s_gpu_worker_subnet_id", prefix)],
		LoadBalancerDNS:       outputs[fmt.Sprintf("%s_aws_lb", prefix)],
		RancherURL:            outputs[fmt.Sprintf("%s_rancher_url", prefix)],
	}
}

func logHASummary(totalHAs int, outputs map[string]string, resolvedPlans []*RancherResolvedPlan) {
	log.Printf("HA setup complete. Rancher URLs:")
	for i := 1; i <= totalHAs; i++ {
		haOutputs := getHAOutputs(i, outputs)
		requestedVersion := ""
		if len(resolvedPlans) >= i && resolvedPlans[i-1] != nil {
			requestedVersion = resolvedPlans[i-1].RequestedVersion
		}
		if requestedVersion != "" {
			log.Printf("Rancher instance %d (%s) -> %s", i, requestedVersion, clickableURL(haOutputs.RancherURL))
			continue
		}
		log.Printf("Rancher instance %d -> %s", i, clickableURL(haOutputs.RancherURL))
	}
}
