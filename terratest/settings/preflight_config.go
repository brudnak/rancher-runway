package settings

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/spf13/viper"
)

var ownerNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z .'-]{0,63}$`)

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
	Distro                string            `json:"distro"`
	BootstrapPassword     string            `json:"bootstrapPassword"`
	PreloadImages         bool              `json:"preloadImages"`
	ServerCount           int               `json:"serverCount"`
	DeploymentType        string            `json:"deploymentType"`
	HostedRDSPassword     string            `json:"hostedRDSPassword"`
	HostedEC2InstanceType string            `json:"hostedEC2InstanceType"`
	LinodeDockerHub       string            `json:"linodeDockerHub"`
	LinodeCustomImage     string            `json:"linodeCustomImage"`
	LinodeSSHRootPassword string            `json:"linodeSSHRootPassword"`
	UserFirstName         string            `json:"userFirstName"`
	UserLastName          string            `json:"userLastName"`
	TFVars                map[string]string `json:"tfVars"`
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
		Distro:                distro,
		BootstrapPassword:     viper.GetString("rancher.bootstrap_password"),
		PreloadImages:         preloadImages,
		ServerCount:           CurrentRKE2ServerCount(),
		DeploymentType:        deploymentType,
		HostedRDSPassword:     viper.GetString("tf_vars.aws_rds_password"),
		HostedEC2InstanceType: strings.TrimSpace(viper.GetString("tf_vars.aws_ec2_instance_type")),
		LinodeDockerHub:       linodeDockerHub,
		LinodeCustomImage:     linodeCustomImage,
		LinodeSSHRootPassword: viper.GetString("linode.ssh_root_password"),
		UserFirstName:         OwnerFirstName(),
		UserLastName:          OwnerLastName(),
		TFVars:                tfVars,
	}
}

func NormalizePreflightConfigUpdate(update *PreflightConfigUpdate) error {
	if update.TFVars == nil && strings.TrimSpace(update.Distro) == "" && strings.TrimSpace(update.BootstrapPassword) == "" && strings.TrimSpace(update.UserFirstName) == "" && strings.TrimSpace(update.UserLastName) == "" {
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

	update.BootstrapPassword = strings.TrimSpace(update.BootstrapPassword)
	if update.BootstrapPassword == "" {
		return fmt.Errorf("rancher.bootstrap_password must be set")
	}
	update.ServerCount = NormalizeRKE2ServerCount(update.ServerCount)
	update.UserFirstName = normalizeOwnerNamePart(update.UserFirstName)
	update.UserLastName = normalizeOwnerNamePart(update.UserLastName)
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
		return fmt.Errorf("tf_vars.aws_pem_key_name must be set")
	}
	for _, key := range EditableTFVarKeys {
		update.TFVars[key] = strings.TrimSpace(update.TFVars[key])
	}
	return nil
}

func ValidateAWSPemKeyNameConfig() error {
	if strings.TrimSpace(viper.GetString("tf_vars.aws_pem_key_name")) == "" {
		return fmt.Errorf("tf_vars.aws_pem_key_name must be set")
	}
	return nil
}
