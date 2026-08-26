package settings

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/spf13/viper"
)

var ownerNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z .'-]{0,63}$`)

var PreferredImageRegistryOptions = []string{
	"stgregistry.suse.com",
	"registry.rancher.com",
	"registry.suse.com",
	"docker.io",
}

var EditableTFVarKeys = []string{
	"aws_region",
	"aws_prefix",
	"aws_vpc",
	"aws_subnet_a",
	"aws_subnet_b",
	"aws_subnet_c",
	"aws_ami",
	"aws_subnet_id",
	"aws_security_group_id",
	"aws_pem_key_name",
	"aws_route53_fqdn",
}

func OwnerFirstName() string {
	return normalizeOwnerNamePart(viper.GetString("user.first_name"))
}

func OwnerLastName() string {
	return normalizeOwnerNamePart(viper.GetString("user.last_name"))
}

func OwnerLabel() string {
	return strings.TrimSpace(OwnerFirstName() + " " + OwnerLastName())
}

func ValidateOwnerConfig() error {
	first := OwnerFirstName()
	last := OwnerLastName()
	if first == "" {
		return fmt.Errorf("user.first_name must be set")
	}
	if last == "" {
		return fmt.Errorf("user.last_name must be set")
	}
	if !ownerNamePattern.MatchString(first) {
		return fmt.Errorf("user.first_name must contain only letters, spaces, apostrophes, periods, or hyphens")
	}
	if !ownerNamePattern.MatchString(last) {
		return fmt.Errorf("user.last_name must contain only letters, spaces, apostrophes, periods, or hyphens")
	}
	viper.Set("user.first_name", first)
	viper.Set("user.last_name", last)
	return nil
}

func normalizeOwnerNamePart(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

type EditablePreflightConfig struct {
	Distro                   string                 `json:"distro"`
	PreferredImageRegistries []string               `json:"preferredImageRegistries"`
	BootstrapPassword        string                 `json:"bootstrapPassword"`
	WebhookImage             string                 `json:"webhookImage"`
	PreloadImages            bool                   `json:"preloadImages"`
	ServerCount              int                    `json:"serverCount"`
	GPUWorker                GPUWorkerConfig        `json:"gpuWorker"`
	DeploymentType           string                 `json:"deploymentType"`
	HostedRDSPassword        string                 `json:"hostedRDSPassword"`
	HostedEC2InstanceType    string                 `json:"hostedEC2InstanceType"`
	LinodeDockerHub          string                 `json:"linodeDockerHub"`
	LinodeCustomImage        string                 `json:"linodeCustomImage"`
	LinodeSSHRootPassword    string                 `json:"linodeSSHRootPassword"`
	UserFirstName            string                 `json:"userFirstName"`
	UserLastName             string                 `json:"userLastName"`
	TFVars                   map[string]string      `json:"tfVars"`
	DownstreamLinodePlans    []LinodeDownstreamPlan `json:"downstreamLinodePlans"`
}

type GPUWorkerConfig struct {
	Enabled      bool   `json:"enabled"`
	Profile      string `json:"profile"`
	InstanceType string `json:"instanceType"`
	AMI          string `json:"ami"`
	SubnetID     string `json:"subnetId"`
}

func CurrentRKE2ServerCount() int {
	return NormalizeRKE2ServerCount(viper.GetInt("rke2.server_count"))
}

func NormalizeRKE2ServerCount(count int) int {
	switch count {
	case 1, 3, 5:
		return count
	default:
		return 3
	}
}

func ValidateRKE2ServerCountConfig() error {
	count := viper.GetInt("rke2.server_count")
	if count == 0 {
		return nil
	}
	switch count {
	case 1, 3, 5:
		return nil
	default:
		return fmt.Errorf("rke2.server_count must be 1, 3, or 5")
	}
}

func CurrentGPUWorkerConfig() GPUWorkerConfig {
	profile := NormalizeGPUWorkerProfile(viper.GetString("gpu_worker.profile"))
	instanceType := strings.TrimSpace(viper.GetString("gpu_worker.instance_type"))
	if instanceType == "" {
		instanceType = GPUWorkerInstanceType(profile)
	}
	return GPUWorkerConfig{
		Enabled:      viper.GetBool("gpu_worker.enabled"),
		Profile:      profile,
		InstanceType: instanceType,
		AMI:          strings.TrimSpace(viper.GetString("gpu_worker.ami")),
		SubnetID:     strings.TrimSpace(viper.GetString("gpu_worker.subnet_id")),
	}
}

func NormalizeGPUWorkerProfile(profile string) string {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "", "standard", "small":
		return "standard"
	case "large":
		return "large"
	default:
		return "standard"
	}
}

func GPUWorkerInstanceType(profile string) string {
	switch NormalizeGPUWorkerProfile(profile) {
	case "large":
		return "p5.4xlarge"
	default:
		return "g5.xlarge"
	}
}

func CurrentEditablePreflightConfig() EditablePreflightConfig {
	tfVars := make(map[string]string, len(EditableTFVarKeys))
	for _, key := range EditableTFVarKeys {
		tfVars[key] = strings.TrimSpace(viper.GetString("tf_vars." + key))
	}
	if prefix, err := NormalizeAWSPrefix(tfVars["aws_prefix"]); err == nil {
		tfVars["aws_prefix"] = prefix
	}

	distro := strings.ToLower(strings.TrimSpace(viper.GetString("rancher.distro")))
	if distro == "" {
		distro = "auto"
	}
	deploymentType := strings.ToLower(strings.TrimSpace(viper.GetString("deployment.type")))
	if deploymentType == "" {
		deploymentType = strings.ToLower(strings.TrimSpace(viper.GetString("environment.type")))
	}
	preloadImages := viper.GetBool("rke2.preload_images")
	if deploymentType == "hosted-tenant-k3s" {
		preloadImages = viper.GetBool("k3s.preload_images")
	}
	linodeDockerHub := strings.TrimSpace(viper.GetString("linode.dockerhub"))
	linodeCustomImage := ""
	switch strings.ToLower(linodeDockerHub) {
	case "", "auto", "dockerhub", "docker.io/rancher/rancher", "rancher/rancher", "staging", "stg", "stgregistry.suse.com/rancher/rancher", "prime", "registry.rancher.com/rancher/rancher", "suse", "registry.suse.com/rancher/rancher":
	default:
		linodeCustomImage = linodeDockerHub
	}

	return EditablePreflightConfig{
		Distro:                   distro,
		PreferredImageRegistries: CurrentPreferredImageRegistries(),
		BootstrapPassword:        viper.GetString("rancher.bootstrap_password"),
		WebhookImage:             strings.TrimSpace(viper.GetString("rancher.webhook_image")),
		PreloadImages:            preloadImages,
		ServerCount:              CurrentRKE2ServerCount(),
		GPUWorker:                CurrentGPUWorkerConfig(),
		DeploymentType:           deploymentType,
		HostedRDSPassword:        viper.GetString("tf_vars.aws_rds_password"),
		HostedEC2InstanceType:    strings.TrimSpace(viper.GetString("tf_vars.aws_ec2_instance_type")),
		LinodeDockerHub:          linodeDockerHub,
		LinodeCustomImage:        linodeCustomImage,
		LinodeSSHRootPassword:    viper.GetString("linode.ssh_root_password"),
		UserFirstName:            OwnerFirstName(),
		UserLastName:             OwnerLastName(),
		TFVars:                   tfVars,
		DownstreamLinodePlans:    CurrentLinodeDownstreamPlans(len(currentConfiguredRancherVersions())),
	}
}

func currentConfiguredRancherVersions() []string {
	versions := viper.GetStringSlice("rancher.versions")
	if len(versions) > 0 {
		return versions
	}
	if strings.TrimSpace(viper.GetString("rancher.version")) != "" {
		return []string{viper.GetString("rancher.version")}
	}
	total := viper.GetInt("total_has")
	if total < 1 {
		total = 1
	}
	return make([]string, total)
}

func NormalizePreflightConfigUpdate(update *PreflightConfigUpdate) error {
	if update.TFVars == nil && strings.TrimSpace(update.Distro) == "" && len(update.PreferredImageRegistries) == 0 && strings.TrimSpace(update.BootstrapPassword) == "" && strings.TrimSpace(update.WebhookImage) == "" && strings.TrimSpace(update.UserFirstName) == "" && strings.TrimSpace(update.UserLastName) == "" {
		return nil
	}

	update.Distro = strings.ToLower(strings.TrimSpace(update.Distro))
	if update.Distro == "" {
		update.Distro = "auto"
	}
	switch update.Distro {
	case "auto", "community", "prime":
	default:
		return fmt.Errorf("rancher.distro must be auto, community, or prime")
	}
	preferredRegistries, err := NormalizePreferredImageRegistries(update.PreferredImageRegistries)
	if err != nil {
		return err
	}
	update.PreferredImageRegistries = preferredRegistries

	update.BootstrapPassword = strings.TrimSpace(update.BootstrapPassword)
	if update.BootstrapPassword == "" {
		return fmt.Errorf("rancher.bootstrap_password must be set")
	}
	update.WebhookImage = strings.TrimSpace(update.WebhookImage)
	update.ServerCount = NormalizeRKE2ServerCount(update.ServerCount)
	update.GPUWorkerProfile = NormalizeGPUWorkerProfile(update.GPUWorkerProfile)
	update.GPUWorkerAMI = strings.TrimSpace(update.GPUWorkerAMI)
	update.GPUWorkerSubnetID = strings.TrimSpace(update.GPUWorkerSubnetID)
	update.UserFirstName = normalizeOwnerNamePart(update.UserFirstName)
	update.UserLastName = normalizeOwnerNamePart(update.UserLastName)
	linodeDocker := strings.EqualFold(strings.TrimSpace(update.DeploymentType), "linode-docker-cattle")
	if linodeDocker {
		if update.UserFirstName == "" {
			update.UserFirstName = OwnerFirstName()
		}
		if update.UserFirstName == "" {
			update.UserFirstName = "Linode"
		}
		if update.UserLastName == "" {
			update.UserLastName = OwnerLastName()
		}
		if update.UserLastName == "" {
			update.UserLastName = "Docker"
		}
	}
	if update.UserFirstName == "" {
		return fmt.Errorf("user.first_name must be set")
	}
	if update.UserLastName == "" {
		return fmt.Errorf("user.last_name must be set")
	}
	if !ownerNamePattern.MatchString(update.UserFirstName) || !ownerNamePattern.MatchString(update.UserLastName) {
		return fmt.Errorf("user first and last name must contain only letters, spaces, apostrophes, periods, or hyphens")
	}

	if update.TFVars == nil {
		return nil
	}

	normalizedPrefix, err := NormalizeAWSPrefix(update.TFVars["aws_prefix"])
	if err != nil {
		return err
	}
	update.TFVars["aws_prefix"] = normalizedPrefix
	if strings.ToLower(strings.TrimSpace(update.DeploymentType)) != "linode-docker-cattle" && strings.TrimSpace(update.TFVars["aws_pem_key_name"]) == "" {
		return fmt.Errorf("tf_vars.aws_pem_key_name (AWS EC2 key pair name) must be set")
	}
	for _, key := range EditableTFVarKeys {
		update.TFVars[key] = strings.TrimSpace(update.TFVars[key])
	}
	return nil
}

func CurrentPreferredImageRegistries() []string {
	registries, err := NormalizePreferredImageRegistries(viper.GetStringSlice("rancher.preferred_image_registries"))
	if err != nil {
		return nil
	}
	return registries
}

func NormalizePreferredImageRegistries(values []string) ([]string, error) {
	allowed := make(map[string]struct{}, len(PreferredImageRegistryOptions))
	for _, registry := range PreferredImageRegistryOptions {
		allowed[registry] = struct{}{}
	}

	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		registry := strings.ToLower(strings.TrimSpace(value))
		if registry == "" || registry == "auto" {
			continue
		}
		if _, ok := allowed[registry]; !ok {
			return nil, fmt.Errorf("rancher.preferred_image_registries contains unsupported registry %q; choose from %s", value, strings.Join(PreferredImageRegistryOptions, ", "))
		}
		if _, duplicate := seen[registry]; duplicate {
			continue
		}
		seen[registry] = struct{}{}
	}

	// Checkboxes have a fixed visible priority. Canonicalize YAML and API input
	// to that same order so a saved selection round-trips without changing which
	// registry wins.
	normalized := make([]string, 0, len(seen))
	for _, registry := range PreferredImageRegistryOptions {
		if _, selected := seen[registry]; selected {
			normalized = append(normalized, registry)
		}
	}
	return normalized, nil
}

func ValidateAWSPemKeyNameConfig() error {
	if strings.TrimSpace(viper.GetString("tf_vars.aws_pem_key_name")) == "" {
		return fmt.Errorf("tf_vars.aws_pem_key_name (AWS EC2 key pair name) must be set")
	}
	return nil
}
