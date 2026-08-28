package test

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brudnak/ha-rancher-rke2/terratest/settings"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

func TestWritePrivateConfigAtomicallyTightensExistingPermissions(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "tool-config.yml")
	if err := os.WriteFile(configPath, []byte("old\n"), 0o644); err != nil {
		t.Fatalf("write existing config: %v", err)
	}
	if err := writePrivateConfigAtomically(configPath, []byte("new\n")); err != nil {
		t.Fatalf("write private config: %v", err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read private config: %v", err)
	}
	if string(data) != "new\n" {
		t.Fatalf("config = %q, want %q", data, "new\\n")
	}
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat private config: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("config mode = %o, want 600", got)
	}
}

func TestUpdateAutoModeConfigFileRewritesVersionsAndTotalHAs(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "tool-config.yml")

	initialConfig := `rancher:
  mode: auto
  version: "2.14.1-alpha3"
  bootstrap_password: "admin"
total_has: 1
rke2:
  ingress_controller: traefik
  preload_images: false
  server_count: 5
tf_vars:
  aws_region: "us-east-2"
`
	if err := os.WriteFile(configPath, []byte(initialConfig), 0o644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	if err := updateAutoModeConfigFile(configPath, settings.PreflightConfigUpdate{
		Versions: []string{"2.14.1-alpha3", "2.13.5-alpha3", "2.12.9-alpha3"},
	}); err != nil {
		t.Fatalf("updateAutoModeConfigFile returned error: %v", err)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read updated config: %v", err)
	}

	var parsed struct {
		Rancher  map[string]interface{} `yaml:"rancher"`
		TotalHAs int                    `yaml:"total_has"`
		TFVars   map[string]interface{} `yaml:"tf_vars"`
	}
	if err := yaml.Unmarshal(content, &parsed); err != nil {
		t.Fatalf("failed to parse updated config: %v", err)
	}

	if parsed.TotalHAs != 3 {
		t.Fatalf("expected total_has 3, got %d", parsed.TotalHAs)
	}

	rawVersions, ok := parsed.Rancher["versions"].([]interface{})
	if !ok {
		t.Fatalf("expected rancher.versions sequence, got %#v", parsed.Rancher["versions"])
	}
	if len(rawVersions) != 3 {
		t.Fatalf("expected 3 versions, got %d", len(rawVersions))
	}
	if rawVersions[0] != "2.14.1-alpha3" || rawVersions[1] != "2.13.5-alpha3" || rawVersions[2] != "2.12.9-alpha3" {
		t.Fatalf("unexpected version list: %#v", rawVersions)
	}
	if _, exists := parsed.Rancher["version"]; exists {
		t.Fatalf("expected rancher.version to be removed, but it is still present")
	}
	if parsed.TFVars["aws_region"] != "us-east-2" {
		t.Fatalf("expected unrelated tf_vars to be preserved, got %#v", parsed.TFVars)
	}
}

func TestUpdateAutoModeConfigFilePersistsAutomaticLinodeDownstreamPlans(t *testing.T) {
	t.Cleanup(viper.Reset)
	viper.Reset()
	t.Setenv("LINODE_TOKEN", "test-token")
	configPath := filepath.Join(t.TempDir(), "tool-config.yml")
	initialConfig := `deployment:
  type: ha-rke2
rancher:
  mode: auto
  versions:
    - "2.15-head"
    - "2.14-head"
  bootstrap_password: "admin"
total_has: 2
`
	if err := os.WriteFile(configPath, []byte(initialConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	first := settings.DefaultLinodeDownstreamPlan()
	first.Enabled = true
	first.Distribution = "rke2"
	second := settings.DefaultLinodeDownstreamPlan()
	if err := updateAutoModeConfigFile(configPath, settings.PreflightConfigUpdate{
		DeploymentType:        deploymentTypeHARKE2,
		Mode:                  "auto",
		Versions:              []string{"2.15-head", "2.14-head"},
		DownstreamLinodePlans: []settings.LinodeDownstreamPlan{first, second},
	}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Downstream struct {
			Linode struct {
				Plans []struct {
					Enabled      bool   `yaml:"enabled"`
					Distribution string `yaml:"distribution"`
					Region       string `yaml:"region"`
					InstanceType string `yaml:"instance_type"`
					Image        string `yaml:"image"`
				} `yaml:"plans"`
			} `yaml:"linode"`
		} `yaml:"downstream"`
	}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	plans := parsed.Downstream.Linode.Plans
	if len(plans) != 2 || !plans[0].Enabled || plans[0].Distribution != "rke2" || plans[0].Region != "us-ord" || plans[0].InstanceType != "g6-standard-2" || plans[0].Image != "linode/ubuntu22.04" || plans[1].Enabled {
		t.Fatalf("unexpected downstream config: %#v", plans)
	}
	if got := settings.CurrentLinodeDownstreamPlans(2); len(got) != 2 || !got[0].Enabled || got[0].Distribution != "rke2" {
		t.Fatalf("unexpected in-memory downstream plans: %#v", got)
	}
}

func TestUpdateAutoModeConfigFilePreservesExactDockerHubImage(t *testing.T) {
	t.Cleanup(viper.Reset)
	viper.Reset()
	const image = "bigkevmcd/rancher:v2.16-da0ab2f1dc-head"
	configPath := filepath.Join(t.TempDir(), "tool-config.yml")
	initialConfig := `rancher:
  mode: auto
  version: "2.15-head"
  bootstrap_password: "admin"
total_has: 1
`
	if err := os.WriteFile(configPath, []byte(initialConfig), 0o600); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	if err := updateAutoModeConfigFile(configPath, settings.PreflightConfigUpdate{
		Versions: []string{image},
	}); err != nil {
		t.Fatalf("updateAutoModeConfigFile returned error: %v", err)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read updated config: %v", err)
	}
	var parsed struct {
		Rancher struct {
			Versions []string `yaml:"versions"`
		} `yaml:"rancher"`
	}
	if err := yaml.Unmarshal(content, &parsed); err != nil {
		t.Fatalf("failed to parse updated config: %v", err)
	}
	if len(parsed.Rancher.Versions) != 1 || parsed.Rancher.Versions[0] != image {
		t.Fatalf("expected exact image in saved rancher.versions, got %#v", parsed.Rancher.Versions)
	}
	if got := viper.GetStringSlice("rancher.versions"); len(got) != 1 || got[0] != image {
		t.Fatalf("expected exact image in runtime config, got %#v", got)
	}
}

func TestUpdateAutoModeConfigFileSynchronizesAgentImageOverrides(t *testing.T) {
	t.Cleanup(viper.Reset)
	viper.Reset()
	viper.Set("rancher.agent_image", "docker.io/old/rancher-agent:old")
	viper.Set("rancher.agent_images", []string{"docker.io/old/rancher-agent:old"})
	configPath := filepath.Join(t.TempDir(), "tool-config.yml")
	initialConfig := `rancher:
  mode: auto
  versions:
    - "docker.io/old/rancher:old"
  agent_image: "docker.io/old/rancher-agent:old"
  agent_images:
    - "docker.io/old/rancher-agent:old"
total_has: 1
`
	if err := os.WriteFile(configPath, []byte(initialConfig), 0o600); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	const serverImage = "bigkevmcd/rancher:v2.16-da0ab2f1dc-head"
	const agentImage = "bigkevmcd/rancher-agent:v2.16-da0ab2f1dc-head"
	if err := updateAutoModeConfigFile(configPath, settings.PreflightConfigUpdate{
		Versions:    []string{serverImage},
		AgentImages: []string{agentImage},
	}); err != nil {
		t.Fatalf("updateAutoModeConfigFile returned error: %v", err)
	}
	if got := viper.GetStringSlice("rancher.agent_images"); len(got) != 1 || got[0] != agentImage {
		t.Fatalf("expected live agent override %q, got %#v", agentImage, got)
	}
	if got := viper.GetString("rancher.agent_image"); got != "" {
		t.Fatalf("expected legacy single agent override to be cleared, got %q", got)
	}

	if err := updateAutoModeConfigFile(configPath, settings.PreflightConfigUpdate{
		Versions:    []string{serverImage},
		AgentImages: []string{""},
	}); err != nil {
		t.Fatalf("clear derived-agent override: %v", err)
	}
	if got := viper.GetStringSlice("rancher.agent_images"); len(got) != 0 {
		t.Fatalf("expected live agent overrides to be cleared, got %#v", got)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read updated config: %v", err)
	}
	var parsed struct {
		Rancher map[string]interface{} `yaml:"rancher"`
	}
	if err := yaml.Unmarshal(content, &parsed); err != nil {
		t.Fatalf("failed to parse updated config: %v", err)
	}
	if _, exists := parsed.Rancher["agent_images"]; exists {
		t.Fatalf("expected saved agent_images override to be removed, got %#v", parsed.Rancher)
	}
	if _, exists := parsed.Rancher["agent_image"]; exists {
		t.Fatalf("expected saved legacy agent_image override to be removed, got %#v", parsed.Rancher)
	}
}

func TestUpdateAutoModeConfigFileRejectsExactImageInLinodeVersionRows(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	configPath := filepath.Join(t.TempDir(), "tool-config.yml")
	initialConfig := `deployment:
  type: linode-docker-cattle
rancher:
  mode: auto
  versions:
    - "2.16-head"
total_has: 1
`
	if err := os.WriteFile(configPath, []byte(initialConfig), 0o600); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	err := updateAutoModeConfigFile(configPath, settings.PreflightConfigUpdate{
		DeploymentType: deploymentTypeLinodeDocker,
		Versions:       []string{"bigkevmcd/rancher:v2.16-da0ab2f1dc-head"},
	})
	if err == nil || !strings.Contains(err.Error(), "Linode custom image source") {
		t.Fatalf("expected Linode exact-image guidance, got %v", err)
	}
}

func TestUpdateAutoModeConfigFileWritesCustomHostnameAndNormalizesVersion(t *testing.T) {
	t.Cleanup(viper.Reset)
	viper.Reset()
	viper.Set("tf_vars.aws_route53_fqdn", "qa.rancher.space")

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "tool-config.yml")

	initialConfig := `rancher:
  mode: auto
  versions:
    - "2.14.1"
    - "2.13.5"
total_has: 2
tf_vars:
  aws_region: "us-east-2"
  aws_route53_fqdn: "qa.rancher.space"
`
	if err := os.WriteFile(configPath, []byte(initialConfig), 0o644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	if err := updateAutoModeConfigFile(configPath, settings.PreflightConfigUpdate{
		Versions:              []string{"v2.14-head"},
		CustomHostnameEnabled: true,
		CustomHostnameInput:   "https://Brudnak.qa.rancher.space/saml",
	}); err != nil {
		t.Fatalf("updateAutoModeConfigFile returned error: %v", err)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read updated config: %v", err)
	}

	var parsed struct {
		Rancher  map[string]interface{} `yaml:"rancher"`
		TotalHAs int                    `yaml:"total_has"`
		TFVars   map[string]interface{} `yaml:"tf_vars"`
	}
	if err := yaml.Unmarshal(content, &parsed); err != nil {
		t.Fatalf("failed to parse updated config: %v", err)
	}

	if parsed.TotalHAs != 1 {
		t.Fatalf("expected total_has 1, got %d", parsed.TotalHAs)
	}
	rawVersions, ok := parsed.Rancher["versions"].([]interface{})
	if !ok || len(rawVersions) != 1 || rawVersions[0] != "2.14-head" {
		t.Fatalf("unexpected version list: %#v", parsed.Rancher["versions"])
	}
	if parsed.TFVars["custom_hostname_prefix"] != "brudnak" {
		t.Fatalf("expected custom_hostname_prefix brudnak, got %#v", parsed.TFVars["custom_hostname_prefix"])
	}
	if got := viper.GetString(settings.CustomHostnameConfigKey); got != "brudnak" {
		t.Fatalf("expected viper custom hostname brudnak, got %q", got)
	}
}

func TestUpdateAutoModeConfigFileWritesEditableConfig(t *testing.T) {
	t.Cleanup(viper.Reset)
	viper.Reset()
	viper.Set("tf_vars.aws_route53_fqdn", "qa.rancher.space")

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "tool-config.yml")

	initialConfig := `rancher:
  mode: auto
  versions:
    - "2.14-head"
  distro: auto
  bootstrap_password: "old"
  webhook_image: "registry.example.com/rancher/rancher-webhook:old"
rke2:
  ingress_controller: traefik
  preload_images: false
total_has: 1
tf_vars:
  aws_region: "us-east-2"
  aws_prefix: "old"
  aws_vpc: ""
  aws_subnet_a: ""
  aws_subnet_b: ""
  aws_subnet_c: ""
  aws_ami: ""
  aws_subnet_id: ""
  aws_security_group_id: ""
  aws_pem_key_name: ""
  aws_route53_fqdn: ""
`
	if err := os.WriteFile(configPath, []byte(initialConfig), 0o644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	tfVars := map[string]string{
		"aws_region":            "us-west-2",
		"aws_prefix":            "ATB",
		"aws_vpc":               "vpc-123",
		"aws_subnet_a":          "subnet-a",
		"aws_subnet_b":          "subnet-b",
		"aws_subnet_c":          "subnet-c",
		"aws_ami":               "ami-123",
		"aws_subnet_id":         "subnet-node",
		"aws_security_group_id": "sg-123",
		"aws_pem_key_name":      "qa-key",
		"aws_route53_fqdn":      "qa.rancher.space",
	}
	if err := updateAutoModeConfigFile(configPath, settings.PreflightConfigUpdate{
		Versions:          []string{"head"},
		Distro:            "community",
		BootstrapPassword: "new-password",
		WebhookImage:      "  stgregistry.suse.com/rancher/rancher-webhook:v0.12.1-rcs-0844.1  ",
		PreloadImages:     true,
		UserFirstName:     "Ada",
		UserLastName:      "Lovelace",
		TFVars:            tfVars,
	}); err != nil {
		t.Fatalf("updateAutoModeConfigFile returned error: %v", err)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read updated config: %v", err)
	}

	var parsed struct {
		Rancher map[string]interface{} `yaml:"rancher"`
		RKE2    map[string]interface{} `yaml:"rke2"`
		User    map[string]interface{} `yaml:"user"`
		TFVars  map[string]interface{} `yaml:"tf_vars"`
	}
	if err := yaml.Unmarshal(content, &parsed); err != nil {
		t.Fatalf("failed to parse updated config: %v", err)
	}

	if parsed.Rancher["distro"] != "community" || parsed.Rancher["bootstrap_password"] != "new-password" {
		t.Fatalf("expected Rancher settings to be updated, got %#v", parsed.Rancher)
	}
	const webhookImage = "stgregistry.suse.com/rancher/rancher-webhook:v0.12.1-rcs-0844.1"
	if parsed.Rancher["webhook_image"] != webhookImage {
		t.Fatalf("expected normalized rancher.webhook_image %q, got %#v", webhookImage, parsed.Rancher["webhook_image"])
	}
	if got := viper.GetString("rancher.webhook_image"); got != webhookImage {
		t.Fatalf("expected in-memory rancher.webhook_image %q, got %q", webhookImage, got)
	}
	if parsed.RKE2["preload_images"] != true {
		t.Fatalf("expected rke2.preload_images=true, got %#v", parsed.RKE2["preload_images"])
	}
	if parsed.RKE2["ingress_controller"] != "traefik" {
		t.Fatalf("expected advanced RKE2 ingress setting to be preserved, got %#v", parsed.RKE2)
	}
	if parsed.User["first_name"] != "Ada" || parsed.User["last_name"] != "Lovelace" {
		t.Fatalf("expected user owner fields to be updated, got %#v", parsed.User)
	}
	if parsed.TFVars["aws_prefix"] != "atb" || parsed.TFVars["aws_region"] != "us-west-2" {
		t.Fatalf("expected tf_vars to be updated and prefix lowercased, got %#v", parsed.TFVars)
	}
}

func TestUpdateAutoModeConfigFileClearsWebhookImageOverride(t *testing.T) {
	t.Cleanup(viper.Reset)
	viper.Reset()

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "tool-config.yml")
	initialConfig := `rancher:
  mode: auto
  versions:
    - "2.14-head"
  distro: auto
  bootstrap_password: "old"
  webhook_image: "registry.example.com/rancher/rancher-webhook:old"
total_has: 1
user:
  first_name: "Ada"
  last_name: "Lovelace"
tf_vars:
  aws_prefix: "atb"
  aws_pem_key_name: "qa-key"
`
	if err := os.WriteFile(configPath, []byte(initialConfig), 0o644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	if err := updateAutoModeConfigFile(configPath, settings.PreflightConfigUpdate{
		Versions:          []string{"2.14-head"},
		Distro:            "auto",
		BootstrapPassword: "new-password",
		WebhookImage:      "  ",
		UserFirstName:     "Ada",
		UserLastName:      "Lovelace",
		TFVars: map[string]string{
			"aws_prefix":       "atb",
			"aws_pem_key_name": "qa-key",
		},
	}); err != nil {
		t.Fatalf("updateAutoModeConfigFile returned error: %v", err)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read updated config: %v", err)
	}
	var parsed struct {
		Rancher map[string]interface{} `yaml:"rancher"`
	}
	if err := yaml.Unmarshal(content, &parsed); err != nil {
		t.Fatalf("failed to parse updated config: %v", err)
	}
	if _, exists := parsed.Rancher["webhook_image"]; exists {
		t.Fatalf("expected blank webhook override to remove rancher.webhook_image, got %#v", parsed.Rancher["webhook_image"])
	}
	if got := viper.GetString("rancher.webhook_image"); got != "" {
		t.Fatalf("expected blank in-memory rancher.webhook_image, got %q", got)
	}
}

func TestCurrentEditablePreflightConfigIncludesImageSettings(t *testing.T) {
	t.Cleanup(viper.Reset)
	viper.Reset()
	viper.Set("rancher.webhook_image", "  stgregistry.suse.com/rancher/rancher-webhook:v0.12.1-rcs-0844.1  ")
	viper.Set("rancher.preferred_image_registries", []string{"stgregistry.suse.com", "docker.io"})

	config := settings.CurrentEditablePreflightConfig()
	if config.WebhookImage != "stgregistry.suse.com/rancher/rancher-webhook:v0.12.1-rcs-0844.1" {
		t.Fatalf("unexpected editable webhook image: %q", config.WebhookImage)
	}
	if strings.Join(config.PreferredImageRegistries, ",") != "stgregistry.suse.com,docker.io" {
		t.Fatalf("unexpected editable preferred registries: %#v", config.PreferredImageRegistries)
	}
}

func TestUpdateAutoModeConfigFilePersistsPreferredImageRegistries(t *testing.T) {
	t.Cleanup(viper.Reset)
	viper.Reset()

	configPath := filepath.Join(t.TempDir(), "tool-config.yml")
	initialConfig := `rancher:
  mode: auto
  versions:
    - "2.14-head"
  bootstrap_password: "old-password"
total_has: 1
user:
  first_name: "Old"
  last_name: "Owner"
tf_vars:
  aws_prefix: "old"
  aws_pem_key_name: "old-key"
`
	if err := os.WriteFile(configPath, []byte(initialConfig), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	update := settings.PreflightConfigUpdate{
		Mode:                     "auto",
		Versions:                 []string{"2.14-head"},
		Distro:                   "auto",
		PreferredImageRegistries: []string{"stgregistry.suse.com", "docker.io", "stgregistry.suse.com"},
		BootstrapPassword:        "new-password",
		UserFirstName:            "Ada",
		UserLastName:             "Lovelace",
		TFVars: map[string]string{
			"aws_prefix":       "atb",
			"aws_pem_key_name": "qa-key",
		},
	}
	if err := updateAutoModeConfigFile(configPath, update); err != nil {
		t.Fatalf("updateAutoModeConfigFile returned error: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var parsed struct {
		Rancher struct {
			PreferredImageRegistries []string `yaml:"preferred_image_registries"`
		} `yaml:"rancher"`
	}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	want := "stgregistry.suse.com,docker.io"
	if got := strings.Join(parsed.Rancher.PreferredImageRegistries, ","); got != want {
		t.Fatalf("persisted registries = %q, want %q", got, want)
	}
	if got := strings.Join(viper.GetStringSlice("rancher.preferred_image_registries"), ","); got != want {
		t.Fatalf("in-memory registries = %q, want %q", got, want)
	}
}

func TestUpdateAutoModeConfigFileWritesHostedTenantConfig(t *testing.T) {
	t.Cleanup(viper.Reset)
	viper.Reset()

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "tool-config.yml")

	initialConfig := `deployment:
  type: ha-rke2
rancher:
  mode: auto
  versions:
    - "2.14-head"
rke2:
  ingress_controller: traefik
  preload_images: true
  server_count: 3
total_has: 1
tf_vars:
  aws_region: "us-east-2"
  aws_prefix: "atb"
  aws_pem_key_name: "qa-key"
`
	if err := os.WriteFile(configPath, []byte(initialConfig), 0o644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	if err := updateAutoModeConfigFile(configPath, settings.PreflightConfigUpdate{
		DeploymentType:        deploymentTypeHostedTenantK3S,
		Versions:              []string{"2.14-head", "v2.13-head", "2.12.9-alpha3"},
		Distro:                "auto",
		BootstrapPassword:     "change-me",
		UserFirstName:         "Ada",
		UserLastName:          "Lovelace",
		TFVars:                map[string]string{"aws_prefix": "atb", "aws_pem_key_name": "qa-key"},
		HostedRDSPassword:     "S3curePass1",
		HostedEC2InstanceType: "m5.xlarge",
		PreloadImages:         true,
	}); err != nil {
		t.Fatalf("updateAutoModeConfigFile returned error: %v", err)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read updated config: %v", err)
	}

	var parsed struct {
		Deployment            map[string]interface{} `yaml:"deployment"`
		Rancher               map[string]interface{} `yaml:"rancher"`
		K3S                   map[string]interface{} `yaml:"k3s"`
		RKE2                  map[string]interface{} `yaml:"rke2"`
		TotalHAs              int                    `yaml:"total_has"`
		TotalRancherInstances int                    `yaml:"total_rancher_instances"`
		TFVars                map[string]interface{} `yaml:"tf_vars"`
	}
	if err := yaml.Unmarshal(content, &parsed); err != nil {
		t.Fatalf("failed to parse updated config: %v", err)
	}

	if parsed.Deployment["type"] != deploymentTypeHostedTenantK3S {
		t.Fatalf("expected hosted tenant deployment type, got %#v", parsed.Deployment)
	}
	if parsed.TotalHAs != 3 || parsed.TotalRancherInstances != 3 {
		t.Fatalf("expected both counts to be 3, got total_has=%d total_rancher_instances=%d", parsed.TotalHAs, parsed.TotalRancherInstances)
	}
	rawVersions, ok := parsed.Rancher["versions"].([]interface{})
	if !ok || len(rawVersions) != 3 || rawVersions[1] != "2.13-head" {
		t.Fatalf("unexpected hosted tenant versions: %#v", parsed.Rancher["versions"])
	}
	if parsed.TFVars["aws_rds_password"] != "S3curePass1" || parsed.TFVars["aws_ec2_instance_type"] != "m5.xlarge" {
		t.Fatalf("expected hosted tenant tf vars, got %#v", parsed.TFVars)
	}
	if parsed.K3S["preload_images"] != true {
		t.Fatalf("expected hosted tenant k3s.preload_images=true, got %#v", parsed.K3S)
	}
	if parsed.RKE2 != nil {
		t.Fatalf("expected hosted tenant setup to remove rke2 node layout settings, got %#v", parsed.RKE2)
	}
	if got := viper.GetString("deployment.type"); got != deploymentTypeHostedTenantK3S {
		t.Fatalf("expected viper deployment.type hosted tenant, got %q", got)
	}
	if got := viper.GetInt("total_rancher_instances"); got != 3 {
		t.Fatalf("expected viper total_rancher_instances 3, got %d", got)
	}
	if !viper.GetBool("k3s.preload_images") {
		t.Fatalf("expected viper k3s.preload_images true")
	}
}

func TestUpdateAutoModeConfigFileRejectsTooManyHostedTenants(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "tool-config.yml")

	initialConfig := `deployment:
  type: hosted-tenant-k3s
rancher:
  mode: auto
total_rancher_instances: 2
`
	if err := os.WriteFile(configPath, []byte(initialConfig), 0o644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	err := updateAutoModeConfigFile(configPath, settings.PreflightConfigUpdate{
		DeploymentType:    deploymentTypeHostedTenantK3S,
		Versions:          []string{"2.14-head", "2.14-head", "2.14-head", "2.14-head", "2.14-head"},
		HostedRDSPassword: "S3curePass1",
	})
	if err == nil {
		t.Fatal("expected too many hosted tenants to fail validation")
	}
	if !strings.Contains(err.Error(), "at most 4") {
		t.Fatalf("expected max count guidance, got %v", err)
	}
}

func TestUpdateAutoModeConfigFileWritesManualModeCommandsAndChecksums(t *testing.T) {
	t.Cleanup(viper.Reset)
	viper.Reset()

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "tool-config.yml")

	initialConfig := `rancher:
  mode: auto
  versions:
    - "2.14.1"
  preferred_image_registries:
    - "stgregistry.suse.com"
    - "docker.io"
rke2:
  preload_images: true
  server_count: 1
total_has: 1
`
	if err := os.WriteFile(configPath, []byte(initialConfig), 0o644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}
	viper.Set("rancher.preferred_image_registries", []string{"stgregistry.suse.com", "docker.io"})

	checksum := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	helmCommand := `helm install rancher rancher-latest/rancher \
  --namespace cattle-system \
  --version 2.14.1 \
  --set hostname=placeholder \
  --set tls=external \
  --set agentTLSMode=system-store`
	if err := updateAutoModeConfigFile(configPath, settings.PreflightConfigUpdate{
		Mode:              "manual",
		HelmCommands:      []string{helmCommand},
		K8SVersions:       []string{"v1.34.6+rke2r1"},
		InstallerSHA256s:  []string{checksum},
		Distro:            "auto",
		BootstrapPassword: "admin",
		UserFirstName:     "Ada",
		UserLastName:      "Lovelace",
		TFVars: map[string]string{
			"aws_prefix":       "atb",
			"aws_pem_key_name": "qa-key",
		},
		ServerCount: 1,
	}); err != nil {
		t.Fatalf("updateAutoModeConfigFile returned error: %v", err)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read updated config: %v", err)
	}

	var parsed struct {
		Rancher  map[string]interface{} `yaml:"rancher"`
		K8S      map[string]interface{} `yaml:"k8s"`
		RKE2     map[string]interface{} `yaml:"rke2"`
		TotalHAs int                    `yaml:"total_has"`
	}
	if err := yaml.Unmarshal(content, &parsed); err != nil {
		t.Fatalf("failed to parse updated config: %v", err)
	}

	if parsed.TotalHAs != 1 {
		t.Fatalf("expected total_has 1, got %d", parsed.TotalHAs)
	}
	if parsed.Rancher["mode"] != "manual" {
		t.Fatalf("expected rancher.mode manual, got %#v", parsed.Rancher["mode"])
	}
	rawRegistries, ok := parsed.Rancher["preferred_image_registries"].([]interface{})
	if !ok || len(rawRegistries) != 2 || rawRegistries[0] != "stgregistry.suse.com" || rawRegistries[1] != "docker.io" {
		t.Fatalf("manual mode did not preserve preferred registries: %#v", parsed.Rancher["preferred_image_registries"])
	}
	if _, exists := parsed.Rancher["versions"]; exists {
		t.Fatalf("expected rancher.versions to be removed in manual mode")
	}
	rawCommands, ok := parsed.Rancher["helm_commands"].([]interface{})
	if !ok || len(rawCommands) != 1 || !strings.Contains(rawCommands[0].(string), "helm install rancher") {
		t.Fatalf("unexpected helm_commands: %#v", parsed.Rancher["helm_commands"])
	}
	if !strings.Contains(rawCommands[0].(string), "--set replicas=1") {
		t.Fatalf("expected single-server manual command to include replicas=1, got %q", rawCommands[0].(string))
	}
	rawVersions, ok := parsed.K8S["versions"].([]interface{})
	if !ok || len(rawVersions) != 1 || rawVersions[0] != "v1.34.6+rke2r1" {
		t.Fatalf("unexpected k8s versions: %#v", parsed.K8S["versions"])
	}
	rawChecksums, ok := parsed.RKE2["install_script_sha256s"].(map[string]interface{})
	if !ok || rawChecksums["v1.34.6+rke2r1"] != checksum {
		t.Fatalf("unexpected installer checksums: %#v", parsed.RKE2["install_script_sha256s"])
	}
	if got := viper.GetStringSlice("rancher.helm_commands"); len(got) != 1 || !strings.Contains(got[0], "helm install rancher") {
		t.Fatalf("expected viper manual helm command, got %#v", got)
	}
}

func TestNormalizeManualPreflightRejectsSingleServerReplicasAboveOne(t *testing.T) {
	_, _, _, err := normalizeManualPreflight(settings.PreflightConfigUpdate{
		ServerCount: 1,
		HelmCommands: []string{`helm install rancher rancher-latest/rancher \
  --namespace cattle-system \
  --version 2.14.1 \
  --set hostname=placeholder \
  --set tls=external \
  --set replicas=3 \
  --set agentTLSMode=system-store`},
		K8SVersions:      []string{"v1.34.6+rke2r1"},
		InstallerSHA256s: []string{"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
	})
	if err == nil {
		t.Fatal("expected single-server manual replicas override to be rejected")
	}
	if !strings.Contains(err.Error(), "replicas=1") {
		t.Fatalf("expected replicas guidance, got %v", err)
	}
}

func TestNormalizePreflightVersionsRejectsBlankValues(t *testing.T) {
	_, err := normalizePreflightVersions([]string{"2.14.1-alpha3", "  "})
	if err == nil {
		t.Fatal("expected blank preflight version to fail validation")
	}
}

func TestNormalizePreflightVersionsAcceptsLeadingV(t *testing.T) {
	versions, err := normalizePreflightVersions([]string{"v2.14-head", "V2.13.5"})
	if err != nil {
		t.Fatalf("normalizePreflightVersions returned error: %v", err)
	}
	if versions[0] != "2.14-head" || versions[1] != "2.13.5" {
		t.Fatalf("expected leading v to be stripped, got %#v", versions)
	}
}

func TestNormalizePreflightVersionsPreservesExactImage(t *testing.T) {
	const image = "bigkevmcd/rancher:v2.16-da0ab2f1dc-head"
	versions, err := normalizePreflightVersions([]string{image})
	if err != nil {
		t.Fatalf("normalizePreflightVersions returned error: %v", err)
	}
	if len(versions) != 1 || versions[0] != image {
		t.Fatalf("expected exact image to be preserved, got %#v", versions)
	}
}

func TestNormalizeVersionInputDoesNotStripDockerNamespace(t *testing.T) {
	const image = "vteam/rancher:v2.16-head"
	if got := normalizeVersionInput(image); got != image {
		t.Fatalf("normalizeVersionInput(%q) = %q", image, got)
	}
}

func TestValidateCustomHostnameConfigRequiresSingleHA(t *testing.T) {
	t.Cleanup(viper.Reset)
	viper.Reset()
	viper.Set("tf_vars.aws_route53_fqdn", "qa.rancher.space")
	viper.Set(settings.CustomHostnameConfigKey, "brudnak")

	err := settings.ValidateCustomHostnameConfig(2)
	if err == nil {
		t.Fatal("expected custom hostname with multiple HAs to fail validation")
	}
}

func TestConfiguredCustomHostnameTreatsQuotedEmptyAsUnset(t *testing.T) {
	t.Cleanup(viper.Reset)
	viper.Reset()
	viper.Set(settings.CustomHostnameConfigKey, ` '""' `)

	prefix, err := settings.ConfiguredCustomHostnamePrefix()
	if err != nil {
		t.Fatalf("configuredCustomHostnamePrefix returned error: %v", err)
	}
	if prefix != "" {
		t.Fatalf("expected quoted empty custom hostname to be unset, got %q", prefix)
	}
}

func TestNormalizeAWSPrefixRequiresTwoOrThreeLetters(t *testing.T) {
	prefix, err := settings.NormalizeAWSPrefix("ATB")
	if err != nil {
		t.Fatalf("expected ATB to be valid, got %v", err)
	}
	if prefix != "atb" {
		t.Fatalf("expected prefix to be lowercased, got %q", prefix)
	}

	for _, value := range []string{"a", "abcd", "a1", ""} {
		if _, err := settings.NormalizeAWSPrefix(value); err == nil {
			t.Fatalf("expected %q to be invalid", value)
		}
	}
}

func TestValidateHostedTenantRDSPasswordMatchesRDSMySQLRules(t *testing.T) {
	if err := validateHostedTenantRDSPassword("S3curePass1!"); err != nil {
		t.Fatalf("expected generated-safe password to be valid, got %v", err)
	}

	for _, value := range []string{
		"short7",
		"has space 1",
		"has/slash1",
		"has'quote1",
		`has"quote1`,
		"has@sign1",
		strings.Repeat("a", 42),
	} {
		if err := validateHostedTenantRDSPassword(value); err == nil {
			t.Fatalf("expected %q to be invalid", value)
		}
	}
}

func TestBuildResolvedPlansDialogMessageLabelsHostedTenantK3SPlans(t *testing.T) {
	message := buildResolvedPlansDialogMessage([]*RancherResolvedPlan{
		{
			RequestedVersion:      "2.14.2-alpha3",
			ChartRepoAlias:        "rancher-alpha",
			ChartVersion:          "2.14.2-alpha3",
			RecommendedK3SVersion: "v1.35.4+k3s1",
			HelmCommands:          []string{"helm install rancher rancher-alpha/rancher"},
		},
		{
			RequestedVersion:      "2.14.2-alpha3",
			ChartRepoAlias:        "rancher-alpha",
			ChartVersion:          "2.14.2-alpha3",
			RecommendedK3SVersion: "v1.35.4+k3s1",
			HelmCommands:          []string{"helm install rancher rancher-alpha/rancher"},
		},
	})

	if !strings.Contains(message, "Host\n") || !strings.Contains(message, "Tenant 1\n") {
		t.Fatalf("expected hosted tenant section labels, got:\n%s", message)
	}
	if !strings.Contains(message, "Resolved K3s/K8s: v1.35.4+k3s1") {
		t.Fatalf("expected K3s resolved label, got:\n%s", message)
	}
	if strings.Contains(message, "Resolved RKE2/K8s") {
		t.Fatalf("did not expect RKE2 label for hosted tenant plan, got:\n%s", message)
	}
}

func TestBuildResolvedPlansDialogMessageIncludesRegistryProvenance(t *testing.T) {
	revision := strings.Repeat("c", 40)
	digest := "sha256:" + strings.Repeat("a", 64)
	message := buildResolvedPlansDialogMessage([]*RancherResolvedPlan{{
		RequestedVersion:         "2.14-head",
		ResolvedDistro:           "community",
		PreferredImageRegistries: []string{"stgregistry.suse.com", "docker.io"},
		ResolvedImageRegistry:    "stgregistry.suse.com",
		RancherImage:             "stgregistry.suse.com/rancher/rancher",
		RancherImageTag:          "v2.14-head",
		AgentImage:               "stgregistry.suse.com/rancher/rancher-agent:v2.14-head",
		RancherImageDigest:       digest,
		AgentImageDigest:         "sha256:" + strings.Repeat("b", 64),
		ImageBuildVersion:        "v2.14-head-build42",
		ImageSourceRevision:      revision,
		ImageSourceCommitURL:     "https://github.com/rancher/rancher/commit/" + revision,
	}})

	for _, want := range []string{
		"Registry preference: stgregistry.suse.com → docker.io",
		"Resolved registry: stgregistry.suse.com",
		"Selected image: stgregistry.suse.com/rancher/rancher:v2.14-head",
		"Observed image OCI digest (resolution time): " + digest,
		"Selected agent image: stgregistry.suse.com/rancher/rancher-agent:v2.14-head",
		"Image build tag: v2.14-head-build42",
		"Image source commit: https://github.com/rancher/rancher/commit/" + revision,
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("expected plan message to contain %q:\n%s", want, message)
		}
	}
}

func TestDecodePreflightConfigUpdateRequestFromHTMXForm(t *testing.T) {
	body := strings.NewReader("deploymentType=hosted-tenant-k3s&versions=head&versions=v2.14-head&distro=community&preferredImageRegistries=stgregistry.suse.com&preferredImageRegistries=docker.io&bootstrapPassword=secret&webhookImage=stgregistry.suse.com%2Francher%2Francher-webhook%3Av0.12.1-rcs-0844.1&preloadImages=true&serverCount=5&hostedRDSPassword=S3curePass1&hostedEC2InstanceType=m5.xlarge&userFirstName=Ada&userLastName=Lovelace&customHostnameEnabled=true&customHostname=demo&tfVars.aws_prefix=ATB&tfVars.aws_pem_key_name=qa-key&tfVars.aws_route53_fqdn=qa.rancher.space")
	req := httptest.NewRequest("POST", "/submit", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	update, err := decodePreflightConfigUpdateRequest(req)
	if err != nil {
		t.Fatalf("decodePreflightConfigUpdateRequest returned error: %v", err)
	}
	if len(update.Versions) != 2 || update.Versions[0] != "head" || update.Versions[1] != "v2.14-head" {
		t.Fatalf("unexpected versions: %#v", update.Versions)
	}
	if update.DeploymentType != deploymentTypeHostedTenantK3S || update.Distro != "community" || update.BootstrapPassword != "secret" || !update.PreloadImages || !update.CustomHostnameEnabled {
		t.Fatalf("unexpected decoded update: %#v", update)
	}
	if update.WebhookImage != "stgregistry.suse.com/rancher/rancher-webhook:v0.12.1-rcs-0844.1" {
		t.Fatalf("unexpected decoded webhook image: %q", update.WebhookImage)
	}
	if strings.Join(update.PreferredImageRegistries, ",") != "stgregistry.suse.com,docker.io" {
		t.Fatalf("unexpected decoded preferred registries: %#v", update.PreferredImageRegistries)
	}
	if update.ServerCount != 5 || update.HostedRDSPassword != "S3curePass1" || update.HostedEC2InstanceType != "m5.xlarge" {
		t.Fatalf("unexpected hosted tenant settings: %#v", update)
	}
	if update.UserFirstName != "Ada" || update.UserLastName != "Lovelace" {
		t.Fatalf("unexpected decoded owner: %#v", update)
	}
	if update.TFVars["aws_prefix"] != "ATB" || update.TFVars["aws_pem_key_name"] != "qa-key" || update.TFVars["aws_route53_fqdn"] != "qa.rancher.space" {
		t.Fatalf("unexpected tf vars: %#v", update.TFVars)
	}
}
