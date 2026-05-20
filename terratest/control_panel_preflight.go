package test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/brudnak/ha-rancher-rke2/terratest/settings"
	"github.com/spf13/viper"
)

func (p *localControlPanel) collectPanelPreflight() systemReadinessState {
	return p.collectPanelPreflightWithWorkspace(true)
}

func (p *localControlPanel) collectPanelPreflightForRunSlotStart() systemReadinessState {
	return p.collectPanelPreflightWithWorkspace(false)
}

func (p *localControlPanel) collectPanelPreflightWithWorkspace(blockOnRunRecords bool) systemReadinessState {
	readiness := collectSystemReadiness(p.configPath)
	readiness.Items = append(readiness.Items, p.checkSetupConfigState())
	readiness.Items = append(readiness.Items, p.checkSetupWorkspaceStateForMode(blockOnRunRecords))

	ready := true
	warnings := 0
	for _, item := range readiness.Items {
		switch item.Status {
		case "error", "blocked":
			ready = false
		case "warning":
			warnings++
		}
	}

	readiness.Ready = ready
	switch {
	case !ready:
		readiness.Summary = "Setup is blocked until required checks are resolved"
	case warnings > 0:
		readiness.Summary = fmt.Sprintf("Ready with %d warning(s)", warnings)
	default:
		readiness.Summary = "Ready to start setup"
	}
	return readiness
}

func (p *localControlPanel) checkSetupConfigState() systemReadinessItem {
	viperConfigMu.RLock()
	defer viperConfigMu.RUnlock()

	item := systemReadinessItem{Name: "Setup config"}

	var blockers []string
	if err := validateDeploymentType(); err != nil {
		blockers = append(blockers, err.Error())
	}
	totalInstances := configuredRancherInstanceCount()
	if isHostedTenantK3SDeployment() {
		if totalInstances < hostedTenantMinInstances {
			blockers = append(blockers, "total_rancher_instances must be at least 2")
		} else if totalInstances > hostedTenantMaxInstances {
			blockers = append(blockers, "total_rancher_instances cannot exceed 4")
		}
	} else if totalInstances < 1 {
		blockers = append(blockers, "total_has must be at least 1")
	}

	mode := rancherMode()
	switch mode {
	case "auto":
		blockers = append(blockers, panelAutoModeConfigBlockers(totalInstances)...)
	case "manual":
		blockers = append(blockers, panelManualModeConfigBlockers(totalInstances)...)
	default:
		blockers = append(blockers, fmt.Sprintf("rancher.mode must be auto or manual, got %q", mode))
	}

	if prefix := strings.TrimSpace(viper.GetString("tf_vars.aws_prefix")); prefix == "" {
		blockers = append(blockers, "tf_vars.aws_prefix")
	} else if _, err := settings.NormalizeAWSPrefix(prefix); err != nil {
		blockers = append(blockers, err.Error())
	}
	if err := settings.ValidateOwnerConfig(); err != nil {
		blockers = append(blockers, err.Error())
	}
	if !isHostedTenantK3SDeployment() {
		if err := settings.ValidateRKE2ServerCountConfig(); err != nil {
			blockers = append(blockers, err.Error())
		}
	}
	if isHostedTenantK3SDeployment() {
		if password := hostedTenantRDSPassword(); password == "" {
			blockers = append(blockers, "tf_vars.aws_rds_password or AWS_RDS_PASSWORD")
		} else if err := validateHostedTenantRDSPassword(password); err != nil {
			blockers = append(blockers, err.Error())
		}
	}

	for _, key := range []string{
		"tf_vars.aws_vpc",
		"tf_vars.aws_subnet_a",
		"tf_vars.aws_subnet_b",
		"tf_vars.aws_subnet_c",
		"tf_vars.aws_ami",
		"tf_vars.aws_subnet_id",
		"tf_vars.aws_security_group_id",
		"tf_vars.aws_pem_key_name",
		"tf_vars.aws_route53_fqdn",
	} {
		if strings.TrimSpace(viper.GetString(key)) == "" {
			blockers = append(blockers, key)
		}
	}

	if strings.TrimSpace(viper.GetString(settings.CustomHostnameConfigKey)) != "" {
		if _, err := settings.ConfiguredCustomHostnamePrefix(); err != nil {
			blockers = append(blockers, err.Error())
		}
	}

	if len(blockers) > 0 {
		item.Status = "error"
		item.Detail = fmt.Sprintf("Config file exists, but required setup values are missing or invalid in %s: %s.", p.configPath, compactPathList(blockers, 12))
		return item
	}

	item.Status = "ok"
	item.Detail = "Required local setup values are present."
	return item
}

func panelAutoModeConfigBlockers(totalHAs int) []string {
	var blockers []string
	distro := strings.ToLower(strings.TrimSpace(viper.GetString("rancher.distro")))
	switch distro {
	case "", "auto", "community", "prime":
	default:
		blockers = append(blockers, "rancher.distro must be auto, community, or prime")
	}

	if strings.TrimSpace(viper.GetString("rancher.bootstrap_password")) == "" {
		blockers = append(blockers, "rancher.bootstrap_password")
	}

	requestedVersion := normalizeVersionInput(viper.GetString("rancher.version"))
	requestedVersions := nonEmptyStringSlice(viper.GetStringSlice("rancher.versions"))
	if totalHAs <= 1 {
		if requestedVersion == "" && len(requestedVersions) == 0 {
			blockers = append(blockers, "rancher.version")
		}
		if len(requestedVersions) > 1 {
			blockers = append(blockers, "rancher.versions must contain one version when only one Rancher instance is configured")
		}
		return blockers
	}

	if len(requestedVersions) != totalHAs {
		blockers = append(blockers, fmt.Sprintf("rancher.versions must contain %d versions", totalHAs))
	}
	return blockers
}

func panelManualModeConfigBlockers(totalHAs int) []string {
	var blockers []string
	helmCommands := nonEmptyStringSlice(viper.GetStringSlice("rancher.helm_commands"))
	if len(helmCommands) != totalHAs {
		blockers = append(blockers, fmt.Sprintf("rancher.helm_commands must contain %d command(s)", totalHAs))
	}

	if isHostedTenantK3SDeployment() {
		k3sVersions := nonEmptyStringSlice(viper.GetStringSlice("k3s.versions"))
		if len(k3sVersions) != totalHAs {
			blockers = append(blockers, fmt.Sprintf("k3s.versions must contain %d version(s)", totalHAs))
		}
		installChecksums := viper.GetStringMapString("k3s.install_script_sha256s")
		airgapChecksums := viper.GetStringMapString("k3s.airgap_image_sha256s")
		for _, version := range k3sVersions {
			if strings.TrimSpace(installChecksums[version]) == "" {
				blockers = append(blockers, fmt.Sprintf("k3s.install_script_sha256s.%s", version))
			}
			if viper.GetBool("k3s.preload_images") && strings.TrimSpace(airgapChecksums[version]) == "" {
				blockers = append(blockers, fmt.Sprintf("k3s.airgap_image_sha256s.%s", version))
			}
		}
	} else {
		k8sVersions := nonEmptyStringSlice(viper.GetStringSlice("k8s.versions"))
		if len(k8sVersions) != totalHAs {
			blockers = append(blockers, fmt.Sprintf("k8s.versions must contain %d version(s)", totalHAs))
		}
		checksums := viper.GetStringMapString("rke2.install_script_sha256s")
		for _, version := range k8sVersions {
			if strings.TrimSpace(checksums[version]) == "" {
				blockers = append(blockers, fmt.Sprintf("rke2.install_script_sha256s.%s", version))
			}
		}
	}
	return blockers
}

func nonEmptyStringSlice(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func (p *localControlPanel) checkSetupWorkspaceState() systemReadinessItem {
	return p.checkSetupWorkspaceStateForMode(true)
}

func (p *localControlPanel) checkSetupWorkspaceStateForMode(blockOnRunRecords bool) systemReadinessItem {
	item := systemReadinessItem{Name: "Workspace state"}

	blockers := p.workspaceBlockers()
	if len(blockers) == 0 {
		item.Status = "ok"
		item.Detail = "No generated HA directories or Terraform working files were found."
		return item
	}
	if len(p.sharedWorkspaceResidueBlockers()) == 0 {
		if !blockOnRunRecords {
			item.Status = "ok"
			item.Detail = "Existing run records are isolated. A new run slot can start without reusing Terraform state or HA output paths."
			return item
		}
		item.Status = "blocked"
		item.Detail = "One or more live HA run records are active in this checkout. Use Start isolated run for another run slot, or cleanup a recorded run from this panel."
		return item
	}

	sort.Strings(blockers)
	item.Status = "error"
	item.Detail = fmt.Sprintf(
		"Found generated state or cleanup residue from a prior or partial run: %s. This does not prove infrastructure is still live, but this checkout is a single-run workspace today. Run cleanup before starting another setup, or use a separate checkout/state key if you need to keep the current Rancher live while starting a fresh one.",
		compactPathList(blockers, 5),
	)
	return item
}

func existingGlobPaths(pattern string) []string {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}

	out := make([]string, 0, len(matches))
	for _, match := range matches {
		if _, err := os.Stat(match); err == nil {
			out = append(out, match)
		}
	}
	return out
}

func compactPathList(paths []string, limit int) string {
	if len(paths) <= limit {
		return strings.Join(paths, ", ")
	}
	return fmt.Sprintf("%s, and %d more", strings.Join(paths[:limit], ", "), len(paths)-limit)
}
