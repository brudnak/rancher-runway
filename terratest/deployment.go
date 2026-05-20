package test

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/spf13/viper"
)

const (
	deploymentTypeHARKE2          = "ha-rke2"
	deploymentTypeHostedTenantK3S = "hosted-tenant-k3s"
	deploymentTypeLinodeDocker    = "linode-docker-cattle"
	hostedTenantMinInstances      = 2
	hostedTenantMaxInstances      = 4
)

func deploymentType() string {
	value := strings.ToLower(strings.TrimSpace(viper.GetString("deployment.type")))
	if value == "" {
		value = strings.ToLower(strings.TrimSpace(viper.GetString("environment.type")))
	}
	if value == "" {
		return deploymentTypeHARKE2
	}
	return value
}

func validateDeploymentType() error {
	switch deploymentType() {
	case deploymentTypeHARKE2, deploymentTypeHostedTenantK3S, deploymentTypeLinodeDocker:
		return nil
	default:
		return fmt.Errorf("deployment.type must be %s, %s, or %s", deploymentTypeHARKE2, deploymentTypeHostedTenantK3S, deploymentTypeLinodeDocker)
	}
}

func isHostedTenantK3SDeployment() bool {
	return deploymentType() == deploymentTypeHostedTenantK3S
}

func isLinodeDockerDeployment() bool {
	return deploymentType() == deploymentTypeLinodeDocker
}

func isLinodeDockerRecord(record panelRunRecord) bool {
	return strings.TrimSpace(record.DeploymentType) == deploymentTypeLinodeDocker
}

func configuredRancherInstanceCount() int {
	if isHostedTenantK3SDeployment() {
		if total := viper.GetInt("hosted_tenant.total_rancher_instances"); total > 0 {
			return total
		}
		if total := viper.GetInt("total_rancher_instances"); total > 0 {
			return total
		}
	}
	return viper.GetInt("total_has")
}

func hostedTenantRancherInstanceCount() int {
	if !isHostedTenantK3SDeployment() {
		return 0
	}
	if total := viper.GetInt("hosted_tenant.total_rancher_instances"); total > 0 {
		return total
	}
	if total := viper.GetInt("total_rancher_instances"); total > 0 {
		return total
	}
	return viper.GetInt("total_has")
}

func hostedTenantRDSPassword() string {
	if value := strings.TrimSpace(os.Getenv("AWS_RDS_PASSWORD")); value != "" {
		return value
	}
	return strings.TrimSpace(viper.GetString("tf_vars.aws_rds_password"))
}

func hostedTenantEC2InstanceType() string {
	if value := strings.TrimSpace(viper.GetString("tf_vars.aws_ec2_instance_type")); value != "" {
		return value
	}
	if value := strings.TrimSpace(viper.GetString("hosted_tenant.aws_ec2_instance_type")); value != "" {
		return value
	}
	return "m5.large"
}

func validateHostedTenantRDSPassword(password string) error {
	if len(password) < 8 {
		return fmt.Errorf("hosted tenant RDS password must be at least 8 characters")
	}
	if len(password) > 41 {
		return fmt.Errorf("hosted tenant RDS password must be 41 characters or fewer for RDS MySQL/Aurora")
	}
	if regexp.MustCompile(`[/'"@ ]`).MatchString(password) {
		return fmt.Errorf("hosted tenant RDS password cannot contain /, ', \", @, or spaces")
	}
	for _, r := range password {
		if r < 32 || r > 126 {
			return fmt.Errorf("hosted tenant RDS password must contain printable ASCII characters only")
		}
	}
	return nil
}
