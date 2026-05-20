package test

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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
	applyRunScopedConfigOverridesFromEnv()
	return nil
}

func applyRunScopedConfigOverridesFromEnv() {
	if value := strings.ToLower(strings.TrimSpace(os.Getenv(runDeploymentTypeEnv))); value != "" {
		viper.Set("deployment.type", value)
	}
	if value := strings.TrimSpace(os.Getenv(runTotalHAsEnv)); value != "" {
		if total, err := strconv.Atoi(value); err == nil && total > 0 {
			viper.Set("total_has", total)
			if deploymentType() == deploymentTypeHostedTenantK3S {
				viper.Set("total_rancher_instances", total)
				viper.Set("hosted_tenant.total_rancher_instances", total)
			}
		}
	}
	if value := strings.TrimSpace(os.Getenv(runRancherVersionsEnv)); value != "" {
		versions := nonEmptyStringSlice(strings.Split(value, ","))
		if len(versions) > 0 {
			viper.Set("rancher.version", "")
			viper.Set("rancher.versions", versions)
		}
	}
	if value := strings.TrimSpace(os.Getenv(runAWSPrefixEnv)); value != "" {
		viper.Set("tf_vars.aws_prefix", value)
	}
	if value := strings.TrimSpace(os.Getenv(runRoute53FQDNEnv)); value != "" {
		viper.Set("tf_vars.aws_route53_fqdn", value)
	}
}

func getTerraformOptions(t *testing.T, totalHAs int) *terraform.Options {
	if isLinodeDockerDeployment() {
		if err := validateLinodeDockerConfig(totalHAs); err != nil {
			t.Fatalf("Linode Docker preflight failed: %v", err)
		}
	} else {
		if err := settings.ValidateAWSPrefixConfig(); err != nil {
			t.Fatalf("AWS prefix preflight failed: %v", err)
		}
		if err := settings.ValidateAWSPemKeyNameConfig(); err != nil {
			t.Fatalf("AWS PEM key preflight failed: %v", err)
		}
		if err := settings.ValidateOwnerConfig(); err != nil {
			t.Fatalf("Owner preflight failed: %v", err)
		}
		if !isHostedTenantK3SDeployment() {
			if err := settings.ValidateRKE2ServerCountConfig(); err != nil {
				t.Fatalf("RKE2 server layout preflight failed: %v", err)
			}
		}
	}
	if isLinodeDockerDeployment() {
		cleanupTerraformVarFile()
	} else {
		generateAwsVars()
	}
	customHostnamePrefix := ""
	if !isLinodeDockerDeployment() {
		var err error
		customHostnamePrefix, err = settings.ConfiguredCustomHostnamePrefix()
		if err != nil {
			t.Fatalf("Invalid custom Rancher hostname: %v", err)
		}
	}

	backendConfig, err := terraformBackendConfigFromEnv()
	if err != nil {
		t.Fatalf("Invalid Terraform backend environment: %v", err)
	}
	if err := syncTerraformBackendFile(backendConfig); err != nil {
		t.Fatalf("Failed to sync Terraform backend file: %v", err)
	}

	rawVars := terraformVars(totalHAs, customHostnamePrefix)
	if isHostedTenantK3SDeployment() {
		delete(rawVars, "server_count")
	} else {
		delete(rawVars, "aws_rds_password")
		delete(rawVars, "aws_ec2_instance_type")
	}
	vars := filterTerraformVarsForModule(rawVars, terraformModuleDir())

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

func terraformVars(totalHAs int, customHostnamePrefix string) map[string]interface{} {
	if isLinodeDockerDeployment() {
		return map[string]interface{}{
			"aws_region":                 linodeDockerAWSRegion(),
			"aws_route53_fqdn":           viper.GetString("tf_vars.aws_route53_fqdn"),
			"label_prefix":               terraformAWSPrefix(viper.GetString("tf_vars.aws_prefix")),
			"linode_access_token":        linodeAccessToken(),
			"linode_ssh_root_password":   linodeRootPassword(),
			"linode_region":              linodeRegion(),
			"linode_type":                linodeInstanceType(),
			"linode_image":               linodeImage(),
			"linode_tags":                linodeTags(),
			"rancher_bootstrap_password": viper.GetString("rancher.bootstrap_password"),
			"dockerhub":                  linodeDockerHub(),
			"docker_install_version":     linodeDockerInstallVersion(),
			"rancher_instances":          linodeRancherInstances(totalHAs),
		}
	}
	return map[string]interface{}{
		"total_has":               totalHAs,
		"deployment_type":         deploymentType(),
		"total_rancher_instances": hostedTenantRancherInstanceCount(),
		"aws_prefix":              terraformAWSPrefix(viper.GetString("tf_vars.aws_prefix")),
		"aws_vpc":                 viper.GetString("tf_vars.aws_vpc"),
		"aws_subnet_a":            viper.GetString("tf_vars.aws_subnet_a"),
		"aws_subnet_b":            viper.GetString("tf_vars.aws_subnet_b"),
		"aws_subnet_c":            viper.GetString("tf_vars.aws_subnet_c"),
		"aws_ami":                 viper.GetString("tf_vars.aws_ami"),
		"aws_subnet_id":           viper.GetString("tf_vars.aws_subnet_id"),
		"aws_security_group_id":   viper.GetString("tf_vars.aws_security_group_id"),
		"aws_pem_key_name":        viper.GetString("tf_vars.aws_pem_key_name"),
		"aws_rds_password":        hostedTenantRDSPassword(),
		"aws_ec2_instance_type":   hostedTenantEC2InstanceType(),
		"server_count":            settings.CurrentRKE2ServerCount(),
		"aws_route53_fqdn":        viper.GetString("tf_vars.aws_route53_fqdn"),
		"custom_hostname_prefix":  customHostnamePrefix,
		"owner_first_name":        settings.OwnerFirstName(),
		"owner_last_name":         settings.OwnerLastName(),
		"run_id":                  currentTerraformRunID(),
	}
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
	if settings.IsAutomationAWSPrefix(basePrefix) || settings.IsRunScopedAWSPrefix(basePrefix) {
		return basePrefix
	}
	runID := safeRunPathSegment(runIDValue)
	if runID == "" || runID == "unknown" {
		return basePrefix
	}
	if len(runID) > 8 {
		runID = runID[:8]
	}
	if strings.HasSuffix(basePrefix, "-r"+runID) {
		return basePrefix
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

func cleanupTerraformVarFile() {
	path := filepath.Join(terraformModuleDir(), "terraform.tfvars")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		log.Printf("failed to remove stale Terraform var file %s: %v", path, err)
	}
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
		deploymentType(),
		hostedTenantRancherInstanceCount(),
		hostedTenantRDSPassword(),
		hostedTenantEC2InstanceType(),
		settings.CurrentRKE2ServerCount(),
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
			continue
		}
		if strings.HasSuffix(key, "_mysql_password") {
			maskGitHubActionsValue(value)
			continue
		}
		if strings.HasSuffix(key, "_ip") ||
			strings.HasSuffix(key, "_ips") ||
			strings.HasSuffix(key, "_private_ip") ||
			strings.HasSuffix(key, "_private_ips") ||
			strings.HasSuffix(key, "_aws_lb") {
			maskGitHubActionsValue(value)
		}
	}
}

func getHAOutputs(instanceNum int, outputs map[string]string) TerraformOutputs {
	prefix := fmt.Sprintf("ha_%d", instanceNum)
	haOutputs := TerraformOutputs{
		ServerCount:      parseTerraformIntOutput(outputs[fmt.Sprintf("%s_server_count", prefix)]),
		ServerIPs:        parseTerraformCSVOutput(outputs[fmt.Sprintf("%s_server_ips", prefix)]),
		ServerPrivateIPs: parseTerraformCSVOutput(outputs[fmt.Sprintf("%s_server_private_ips", prefix)]),
		Server1IP:        outputs[fmt.Sprintf("%s_server1_ip", prefix)],
		Server2IP:        outputs[fmt.Sprintf("%s_server2_ip", prefix)],
		Server3IP:        outputs[fmt.Sprintf("%s_server3_ip", prefix)],
		Server4IP:        outputs[fmt.Sprintf("%s_server4_ip", prefix)],
		Server5IP:        outputs[fmt.Sprintf("%s_server5_ip", prefix)],
		Server1PrivateIP: outputs[fmt.Sprintf("%s_server1_private_ip", prefix)],
		Server2PrivateIP: outputs[fmt.Sprintf("%s_server2_private_ip", prefix)],
		Server3PrivateIP: outputs[fmt.Sprintf("%s_server3_private_ip", prefix)],
		Server4PrivateIP: outputs[fmt.Sprintf("%s_server4_private_ip", prefix)],
		Server5PrivateIP: outputs[fmt.Sprintf("%s_server5_private_ip", prefix)],
		LoadBalancerDNS:  outputs[fmt.Sprintf("%s_aws_lb", prefix)],
		RancherURL:       outputs[fmt.Sprintf("%s_rancher_url", prefix)],
	}
	if len(haOutputs.ServerIPs) == 0 {
		haOutputs.ServerIPs = nonEmptyStrings(haOutputs.Server1IP, haOutputs.Server2IP, haOutputs.Server3IP, haOutputs.Server4IP, haOutputs.Server5IP)
	}
	if len(haOutputs.ServerPrivateIPs) == 0 {
		haOutputs.ServerPrivateIPs = nonEmptyStrings(haOutputs.Server1PrivateIP, haOutputs.Server2PrivateIP, haOutputs.Server3PrivateIP, haOutputs.Server4PrivateIP, haOutputs.Server5PrivateIP)
	}
	if haOutputs.ServerCount == 0 {
		haOutputs.ServerCount = len(haOutputs.ServerIPs)
	}
	maskHAOutputs(haOutputs)
	return haOutputs
}

func maskHAOutputs(haOutputs TerraformOutputs) {
	for _, value := range haOutputs.ServerIPs {
		maskGitHubActionsValue(value)
	}
	for _, value := range haOutputs.ServerPrivateIPs {
		maskGitHubActionsValue(value)
	}
	maskGitHubActionsValue(haOutputs.Server1IP)
	maskGitHubActionsValue(haOutputs.Server2IP)
	maskGitHubActionsValue(haOutputs.Server3IP)
	maskGitHubActionsValue(haOutputs.Server4IP)
	maskGitHubActionsValue(haOutputs.Server5IP)
	maskGitHubActionsValue(haOutputs.Server1PrivateIP)
	maskGitHubActionsValue(haOutputs.Server2PrivateIP)
	maskGitHubActionsValue(haOutputs.Server3PrivateIP)
	maskGitHubActionsValue(haOutputs.Server4PrivateIP)
	maskGitHubActionsValue(haOutputs.Server5PrivateIP)
	maskGitHubActionsValue(haOutputs.LoadBalancerDNS)
	maskGitHubActionsURL(haOutputs.RancherURL)
}

func parseTerraformCSVOutput(value string) []string {
	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			values = append(values, trimmed)
		}
	}
	return values
}

func parseTerraformIntOutput(value string) int {
	var parsed int
	if _, err := fmt.Sscanf(strings.TrimSpace(value), "%d", &parsed); err != nil {
		return 0
	}
	return parsed
}

func nonEmptyStrings(values ...string) []string {
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			filtered = append(filtered, trimmed)
		}
	}
	return filtered
}

func logHASummary(totalHAs int, outputs map[string]string, resolvedPlans []*RancherResolvedPlan) {
	if githubActions() {
		log.Printf("HA setup complete. Rancher endpoints configured:")
		for i := 1; i <= totalHAs; i++ {
			requestedVersion := ""
			if len(resolvedPlans) >= i && resolvedPlans[i-1] != nil {
				requestedVersion = resolvedPlans[i-1].RequestedVersion
			}
			if requestedVersion != "" {
				log.Printf("Rancher instance %d (%s) -> configured", i, requestedVersion)
				continue
			}
			log.Printf("Rancher instance %d -> configured", i)
		}
		return
	}

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
