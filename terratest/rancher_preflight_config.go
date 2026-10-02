package test

import (
	"bytes"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	"github.com/brudnak/ha-rancher-rke2/terratest/settings"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
	"os"
	"strings"
)

func updateAutoModeConfigFile(configPath string, update settings.PreflightConfigUpdate) error {
	requestedDeploymentType := strings.ToLower(strings.TrimSpace(update.DeploymentType))
	if requestedDeploymentType == "" {
		requestedDeploymentType = deploymentType()
	}
	switch requestedDeploymentType {
	case deploymentTypeHARKE2, deploymentTypeHostedTenantK3S, deploymentTypeLinodeDocker:
	default:
		return fmt.Errorf("deployment.type must be %s, %s, or %s", deploymentTypeHARKE2, deploymentTypeHostedTenantK3S, deploymentTypeLinodeDocker)
	}
	hostedTenant := requestedDeploymentType == deploymentTypeHostedTenantK3S
	linodeDocker := requestedDeploymentType == deploymentTypeLinodeDocker
	mode := strings.ToLower(strings.TrimSpace(update.Mode))
	if mode == "" {
		mode = "auto"
	}
	if hostedTenant && mode != "auto" {
		return fmt.Errorf("hosted-tenant-k3s setup editor currently supports auto mode only")
	}
	if linodeDocker && mode != "auto" {
		return fmt.Errorf("linode-docker-cattle setup editor currently supports auto mode only")
	}
	var normalizedVersions []string
	var normalizedHelmCommands []string
	var normalizedK8SVersions []string
	var installerSHA256s []string
	var err error
	switch mode {
	case "auto":
		normalizedVersions, err = normalizePreflightVersions(update.Versions)
		if err == nil {
			if linodeDocker {
				err = validateLinodeDockerVersionInputs(normalizedVersions)
				update.AgentImages = make([]string, len(normalizedVersions))
			} else if update.AgentImages == nil {
				update.AgentImages = currentAgentImageOverrides(normalizedVersions)
			}
			if err == nil {
				update.AgentImages, err = normalizePreflightAgentImages(update.AgentImages, len(normalizedVersions))
			}
		}
	case "manual":
		normalizedHelmCommands, normalizedK8SVersions, installerSHA256s, err = normalizeManualPreflight(update)
	default:
		err = fmt.Errorf("rancher.mode must be auto or manual")
	}
	if err != nil {
		return err
	}
	if mode == "auto" && !hostedTenant && !linodeDocker {
		update.DownstreamLinodePlans, err = settings.NormalizeLinodeDownstreamPlans(update.DownstreamLinodePlans, len(normalizedVersions))
		if err != nil {
			return err
		}
		if settings.AnyLinodeDownstreamPlanEnabled(update.DownstreamLinodePlans) && strings.TrimSpace(linodeAccessToken()) == "" {
			return fmt.Errorf("automatic Linode downstream provisioning requires LINODE_TOKEN or LINODE_ACCESS_TOKEN")
		}
	} else {
		update.DownstreamLinodePlans = nil
	}
	if update.TFVars != nil && (mode != "auto" || linodeDocker) {
		// Registry controls are hidden outside HA/hosted auto mode. Preserve the
		// saved selection instead of treating an omitted form field as a clear.
		viperConfigMu.RLock()
		update.PreferredImageRegistries = viper.GetStringSlice("rancher.preferred_image_registries")
		viperConfigMu.RUnlock()
	}
	if err := settings.NormalizePreflightConfigUpdate(&update); err != nil {
		return err
	}
	if update.TFVars != nil && update.WebhookImage != "" {
		if _, _, _, err := imagelookup.ParseRegistryImage(update.WebhookImage); err != nil {
			return fmt.Errorf("rancher.webhook_image: %w", err)
		}
	}
	if hostedTenant {
		if len(normalizedVersions) < hostedTenantMinInstances {
			return fmt.Errorf("hosted-tenant-k3s requires one host and at least one tenant Rancher version")
		}
		if len(normalizedVersions) > hostedTenantMaxInstances {
			return fmt.Errorf("hosted-tenant-k3s supports at most 4 total Rancher instances")
		}
		update.CustomHostnameEnabled = false
		update.CustomHostnameInput = ""
		password := strings.TrimSpace(update.HostedRDSPassword)
		if password == "" {
			password = hostedTenantRDSPassword()
		}
		if err := validateHostedTenantRDSPassword(password); err != nil {
			return err
		}
		update.HostedRDSPassword = password
		update.HostedEC2InstanceType = strings.TrimSpace(update.HostedEC2InstanceType)
		if update.HostedEC2InstanceType == "" {
			update.HostedEC2InstanceType = hostedTenantEC2InstanceType()
		}
	}
	if linodeDocker {
		update.CustomHostnameEnabled = false
		update.CustomHostnameInput = ""
		update.LinodeCustomImage = strings.TrimSpace(update.LinodeCustomImage)
		if update.LinodeCustomImage != "" {
			customRepository, _, err := normalizeCustomLinodeDockerImage(update.LinodeCustomImage)
			if err != nil {
				return err
			}
			update.LinodeDockerHub = customRepository
		} else {
			update.LinodeDockerHub = normalizeLinodeDockerHubSelection(update.LinodeDockerHub)
			if update.LinodeDockerHub == "custom" {
				return fmt.Errorf("custom Linode Docker image source requires an image path")
			}
		}
		password := strings.TrimSpace(update.LinodeSSHRootPassword)
		if password == "" {
			password = linodeRootPassword()
		}
		if err := validateLinodeRootPassword(password); err != nil {
			return err
		}
		update.LinodeSSHRootPassword = password
	}
	viperConfigMu.RLock()
	route53FQDN := viper.GetString("tf_vars.aws_route53_fqdn")
	viperConfigMu.RUnlock()
	if update.TFVars != nil {
		route53FQDN = update.TFVars["aws_route53_fqdn"]
	}
	customHostnamePrefix, err := settings.NormalizeCustomHostnameSelectionForDomain(update.CustomHostnameEnabled, update.CustomHostnameInput, route53FQDN)
	if err != nil {
		return err
	}
	if customHostnamePrefix != "" && ((mode == "auto" && len(normalizedVersions) != 1) || (mode == "manual" && len(normalizedHelmCommands) != 1)) {
		return fmt.Errorf("custom Rancher URL can only be used with one HA")
	}
	if !hostedTenant && !linodeDocker && update.GPUWorkerEnabled && ((mode == "auto" && len(normalizedVersions) != 1) || (mode == "manual" && len(normalizedHelmCommands) != 1)) {
		return fmt.Errorf("GPU worker runs are limited to one Rancher HA per run slot")
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	var document yaml.Node
	if err := yaml.Unmarshal(content, &document); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}
	if len(document.Content) == 0 {
		return fmt.Errorf("config file is empty")
	}

	root := document.Content[0]
	if root.Kind != yaml.MappingNode {
		return fmt.Errorf("config root must be a YAML mapping")
	}

	deploymentNode := ensureMappingValue(root, "deployment")
	setStringValue(deploymentNode, "type", requestedDeploymentType)
	rancherNode := ensureMappingValue(root, "rancher")
	if update.TFVars != nil {
		setStringValue(rancherNode, "distro", update.Distro)
		if len(update.PreferredImageRegistries) == 0 {
			deleteMappingKey(rancherNode, "preferred_image_registries")
		} else {
			setStringSequenceValue(rancherNode, "preferred_image_registries", update.PreferredImageRegistries)
		}
		setStringValue(rancherNode, "bootstrap_password", update.BootstrapPassword)
		if update.WebhookImage == "" {
			deleteMappingKey(rancherNode, "webhook_image")
		} else {
			setStringValue(rancherNode, "webhook_image", update.WebhookImage)
		}
		userNode := ensureMappingValue(root, "user")
		setStringValue(userNode, "first_name", update.UserFirstName)
		setStringValue(userNode, "last_name", update.UserLastName)
		if hostedTenant {
			k3sNode := ensureMappingValue(root, "k3s")
			setBoolValue(k3sNode, "preload_images", update.PreloadImages)
		} else if !linodeDocker {
			rke2Node := ensureMappingValue(root, "rke2")
			setBoolValue(rke2Node, "preload_images", update.PreloadImages)
			setIntValue(rke2Node, "server_count", update.ServerCount)
			gpuWorkerNode := ensureMappingValue(root, "gpu_worker")
			gpuWorkerProfile := settings.NormalizeGPUWorkerProfile(update.GPUWorkerProfile)
			setBoolValue(gpuWorkerNode, "enabled", update.GPUWorkerEnabled)
			setStringValue(gpuWorkerNode, "profile", gpuWorkerProfile)
			setStringValue(gpuWorkerNode, "instance_type", settings.GPUWorkerInstanceType(gpuWorkerProfile))
			setStringValue(gpuWorkerNode, "ami", strings.TrimSpace(update.GPUWorkerAMI))
			setStringValue(gpuWorkerNode, "subnet_id", strings.TrimSpace(update.GPUWorkerSubnetID))
		}
	}
	setStringValue(rancherNode, "mode", mode)
	switch mode {
	case "auto":
		setStringSequenceValue(rancherNode, "versions", normalizedVersions)
		if hasNonEmptyString(update.AgentImages) {
			setStringSequenceValue(rancherNode, "agent_images", update.AgentImages)
		} else {
			deleteMappingKey(rancherNode, "agent_images")
		}
		deleteMappingKey(rancherNode, "version")
		deleteMappingKey(rancherNode, "agent_image")
		deleteMappingKey(rancherNode, "helm_commands")
		setIntValue(root, "total_has", len(normalizedVersions))
		if hostedTenant {
			setIntValue(root, "total_rancher_instances", len(normalizedVersions))
		} else {
			deleteMappingKey(root, "total_rancher_instances")
		}
	case "manual":
		setStringLiteralSequenceValue(rancherNode, "helm_commands", normalizedHelmCommands)
		deleteMappingKey(rancherNode, "version")
		deleteMappingKey(rancherNode, "versions")
		deleteMappingKey(rancherNode, "agent_image")
		deleteMappingKey(rancherNode, "agent_images")
		setIntValue(root, "total_has", len(normalizedHelmCommands))
		deleteMappingKey(root, "total_rancher_instances")
		k8sNode := ensureMappingValue(root, "k8s")
		setStringSequenceValue(k8sNode, "versions", normalizedK8SVersions)
		deleteMappingKey(k8sNode, "version")
		rke2Node := ensureMappingValue(root, "rke2")
		setStringMapValue(rke2Node, "install_script_sha256s", normalizedK8SVersions, installerSHA256s)
		deleteMappingKey(rke2Node, "install_script_sha256")
	}
	if settings.AnyLinodeDownstreamPlanEnabled(update.DownstreamLinodePlans) {
		downstreamNode := ensureMappingValue(root, "downstream")
		linodeDownstreamNode := ensureMappingValue(downstreamNode, "linode")
		setLinodeDownstreamPlanSequenceValue(linodeDownstreamNode, "plans", update.DownstreamLinodePlans)
	} else if downstreamNode := mappingValue(root, "downstream"); downstreamNode != nil {
		if linodeDownstreamNode := mappingValue(downstreamNode, "linode"); linodeDownstreamNode != nil {
			deleteMappingKey(linodeDownstreamNode, "plans")
			if len(linodeDownstreamNode.Content) == 0 {
				deleteMappingKey(downstreamNode, "linode")
			}
		}
		if len(downstreamNode.Content) == 0 {
			deleteMappingKey(root, "downstream")
		}
	}
	if update.TFVars != nil {
		tfVarsNode := ensureMappingValue(root, "tf_vars")
		for _, key := range settings.EditableTFVarKeys {
			setStringValue(tfVarsNode, key, update.TFVars[key])
		}
		if hostedTenant {
			setStringValue(tfVarsNode, "aws_rds_password", update.HostedRDSPassword)
			setStringValue(tfVarsNode, "aws_ec2_instance_type", update.HostedEC2InstanceType)
		}
		if linodeDocker {
			linodeNode := ensureMappingValue(root, "linode")
			setStringValue(linodeNode, "dockerhub", update.LinodeDockerHub)
			setStringValue(linodeNode, "ssh_root_password", update.LinodeSSHRootPassword)
		}
	} else if hostedTenant {
		tfVarsNode := ensureMappingValue(root, "tf_vars")
		setStringValue(tfVarsNode, "aws_rds_password", update.HostedRDSPassword)
		setStringValue(tfVarsNode, "aws_ec2_instance_type", update.HostedEC2InstanceType)
	} else if linodeDocker {
		linodeNode := ensureMappingValue(root, "linode")
		setStringValue(linodeNode, "dockerhub", update.LinodeDockerHub)
		setStringValue(linodeNode, "ssh_root_password", update.LinodeSSHRootPassword)
	}
	if customHostnamePrefix == "" {
		if tfVarsNode := mappingValue(root, "tf_vars"); tfVarsNode != nil {
			deleteMappingKey(tfVarsNode, "custom_hostname_prefix")
		}
	} else {
		tfVarsNode := ensureMappingValue(root, "tf_vars")
		setStringValue(tfVarsNode, "custom_hostname_prefix", customHostnamePrefix)
	}
	if hostedTenant || linodeDocker {
		if rke2Node := mappingValue(root, "rke2"); rke2Node != nil {
			deleteMappingKey(rke2Node, "ingress_controller")
			deleteMappingKey(rke2Node, "preload_images")
			deleteMappingKey(rke2Node, "server_count")
			if len(rke2Node.Content) == 0 {
				deleteMappingKey(root, "rke2")
			}
		}
		deleteMappingKey(root, "gpu_worker")
	}

	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	if err := encoder.Encode(&document); err != nil {
		return fmt.Errorf("failed to serialize config file: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return fmt.Errorf("failed to finalize config file: %w", err)
	}

	if err := writePrivateConfigAtomically(configPath, output.Bytes()); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	viperConfigMu.Lock()
	defer viperConfigMu.Unlock()
	viper.Set("deployment.type", requestedDeploymentType)
	viper.Set("rancher.mode", mode)
	viper.Set("rancher.version", "")
	switch mode {
	case "auto":
		viper.Set("rancher.versions", normalizedVersions)
		viper.Set("rancher.agent_image", "")
		if hasNonEmptyString(update.AgentImages) {
			viper.Set("rancher.agent_images", update.AgentImages)
		} else {
			viper.Set("rancher.agent_images", []string{})
		}
		viper.Set("rancher.helm_commands", []string{})
		viper.Set(settings.DownstreamLinodeConfigKey, update.DownstreamLinodePlans)
		viper.Set("total_has", len(normalizedVersions))
		if hostedTenant {
			viper.Set("total_rancher_instances", len(normalizedVersions))
		} else {
			viper.Set("total_rancher_instances", 0)
		}
	case "manual":
		viper.Set("rancher.versions", []string{})
		viper.Set("rancher.agent_image", "")
		viper.Set("rancher.agent_images", []string{})
		viper.Set("rancher.helm_commands", normalizedHelmCommands)
		viper.Set(settings.DownstreamLinodeConfigKey, []settings.LinodeDownstreamPlan{})
		viper.Set("k8s.versions", normalizedK8SVersions)
		viper.Set("k8s.version", "")
		checksumMap := make(map[string]string, len(normalizedK8SVersions))
		for i, version := range normalizedK8SVersions {
			checksumMap[version] = installerSHA256s[i]
		}
		viper.Set("rke2.install_script_sha256s", checksumMap)
		viper.Set("rke2.install_script_sha256", "")
		viper.Set("total_has", len(normalizedHelmCommands))
		viper.Set("total_rancher_instances", 0)
	}
	if update.TFVars != nil {
		viper.Set("rancher.distro", update.Distro)
		viper.Set("rancher.preferred_image_registries", update.PreferredImageRegistries)
		viper.Set("rancher.bootstrap_password", update.BootstrapPassword)
		viper.Set("rancher.webhook_image", update.WebhookImage)
		viper.Set("user.first_name", update.UserFirstName)
		viper.Set("user.last_name", update.UserLastName)
		if hostedTenant {
			viper.Set("k3s.preload_images", update.PreloadImages)
		} else if !linodeDocker {
			viper.Set("rke2.preload_images", update.PreloadImages)
			viper.Set("rke2.server_count", update.ServerCount)
			viper.Set("gpu_worker.enabled", update.GPUWorkerEnabled)
			viper.Set("gpu_worker.profile", settings.NormalizeGPUWorkerProfile(update.GPUWorkerProfile))
			viper.Set("gpu_worker.instance_type", settings.GPUWorkerInstanceType(update.GPUWorkerProfile))
			viper.Set("gpu_worker.ami", strings.TrimSpace(update.GPUWorkerAMI))
			viper.Set("gpu_worker.subnet_id", strings.TrimSpace(update.GPUWorkerSubnetID))
		}
		for _, key := range settings.EditableTFVarKeys {
			viper.Set("tf_vars."+key, update.TFVars[key])
		}
	}
	if hostedTenant {
		viper.Set("tf_vars.aws_rds_password", update.HostedRDSPassword)
		viper.Set("tf_vars.aws_ec2_instance_type", update.HostedEC2InstanceType)
		viper.Set("rke2.preload_images", false)
		viper.Set("rke2.server_count", 0)
		viper.Set("rke2.ingress_controller", "")
		viper.Set("gpu_worker.enabled", false)
	}
	if linodeDocker {
		viper.Set("linode.dockerhub", update.LinodeDockerHub)
		viper.Set("linode.ssh_root_password", update.LinodeSSHRootPassword)
		viper.Set("rke2.preload_images", false)
		viper.Set("rke2.server_count", 0)
		viper.Set("rke2.ingress_controller", "")
		viper.Set("gpu_worker.enabled", false)
	}
	viper.Set(settings.CustomHostnameConfigKey, customHostnamePrefix)

	return nil
}
