package test

import (
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/terratest/settings"
	"github.com/spf13/viper"
	"log"
	"strings"
	"time"
)

const defaultRunSlotID = "default"

const (
	panelDownstreamStatusPending = "downstream_pending"
	panelDownstreamStatusRunning = "downstream_running"
	panelDownstreamStatusReady   = "downstream_ready"
	panelDownstreamStatusFailed  = "downstream_failed"
)

type panelWorkspaceState struct {
	Mode                     string           `json:"mode"`
	SlotID                   string           `json:"slotId"`
	SlotName                 string           `json:"slotName"`
	CanStartFresh            bool             `json:"canStartFresh"`
	CanStartIsolatedRun      bool             `json:"canStartIsolatedRun"`
	IsolatedRunBlockedReason string           `json:"isolatedRunBlockedReason,omitempty"`
	Summary                  string           `json:"summary"`
	CurrentRun               *panelRunRecord  `json:"currentRun,omitempty"`
	Runs                     []panelRunRecord `json:"runs"`
	SharedPathLabels         []string         `json:"sharedPathLabels"`
}

type panelRunRecord struct {
	RunID                 string                          `json:"runId"`
	SlotID                string                          `json:"slotId"`
	SlotName              string                          `json:"slotName"`
	Status                string                          `json:"status"`
	DeploymentType        string                          `json:"deploymentType,omitempty"`
	CreatedAt             time.Time                       `json:"createdAt"`
	UpdatedAt             time.Time                       `json:"updatedAt"`
	TotalHAs              int                             `json:"totalHAs"`
	AWSPrefix             string                          `json:"awsPrefix,omitempty"`
	Route53FQDN           string                          `json:"route53Fqdn,omitempty"`
	Owner                 string                          `json:"owner,omitempty"`
	CustomHostnamePrefix  string                          `json:"customHostnamePrefix,omitempty"`
	RancherVersions       []string                        `json:"rancherVersions,omitempty"`
	DownstreamLinodePlans []settings.LinodeDownstreamPlan `json:"downstreamLinodePlans,omitempty"`
	DownstreamStatus      string                          `json:"downstreamStatus,omitempty"`
	DownstreamError       string                          `json:"downstreamError,omitempty"`
	DownstreamUpdatedAt   *time.Time                      `json:"downstreamUpdatedAt,omitempty"`
	GPUWorkerEnabled      bool                            `json:"gpuWorkerEnabled,omitempty"`
	GPUWorkerInstanceType string                          `json:"gpuWorkerInstanceType,omitempty"`
	TerraformBackend      string                          `json:"terraformBackend"`
	TerraformModuleDir    string                          `json:"terraformModuleDir,omitempty"`
	TerraformStatePath    string                          `json:"terraformStatePath,omitempty"`
	TerraformDataDir      string                          `json:"terraformDataDir,omitempty"`
	HAOutputRoot          string                          `json:"haOutputRoot"`
	RunFolderPath         string                          `json:"runFolderPath,omitempty"`
	RunFolderExists       bool                            `json:"runFolderExists"`
	SharedPaths           []string                        `json:"sharedPaths"`
}

type localArtifactCleanupResult struct {
	Removed []string `json:"removed"`
}

func (p *localControlPanel) workspaceState() panelWorkspaceState {
	records := p.listRunRecords()
	primaryRun, hasPrimaryRun := primaryPanelRunRecord(records)
	if hasPrimaryRun {
		primaryRun = p.ensureCurrentRunModuleIsolation(primaryRun)
		records = upsertPanelRunRecord(records, primaryRun)
	}
	canStartIsolated, isolatedBlockedReason := p.isolatedRunStartStatus()
	state := panelWorkspaceState{
		Mode:                     "run-slot workspace",
		SlotID:                   defaultRunSlotID,
		SlotName:                 "Run slots",
		CanStartFresh:            len(records) == 0 && len(p.sharedWorkspaceResidueBlockers()) == 0,
		CanStartIsolatedRun:      canStartIsolated,
		IsolatedRunBlockedReason: isolatedBlockedReason,
		Summary:                  "Each panel run gets isolated Terraform state, module files, HA output, kubeconfigs, logs, AWS names, and Route53 hostnames.",
		Runs:                     records,
		SharedPathLabels:         p.sharedWorkspacePathLabels(),
	}
	if hasPrimaryRun {
		state.CurrentRun = &primaryRun
		state.SlotID = primaryRun.SlotID
		state.SlotName = primaryRun.SlotName
	}
	return state
}

func (p *localControlPanel) ensureCurrentRunModuleIsolation(record panelRunRecord) panelRunRecord {
	if strings.TrimSpace(record.RunID) == "" || strings.TrimSpace(record.TerraformModuleDir) != "" || p.anyOperationRunning() {
		return record
	}

	moduleDir := p.terraformModuleDirForRun(record.RunID)
	if !pathExists(moduleDir) {
		if err := p.prepareTerraformModuleForRun(record.RunID); err != nil {
			log.Printf("[control-panel] Failed to prepare isolated Terraform module for current run %s: %v", record.RunID, err)
			return record
		}
	}
	record.TerraformModuleDir = moduleDir
	record.UpdatedAt = time.Now()
	p.writeCurrentRunRecord(record)
	return record
}

func (p *localControlPanel) startIsolatedRun() error {
	if ok, reason := p.isolatedRunStartStatus(); !ok {
		return fmt.Errorf("isolated run blocked: %s", reason)
	}
	afterSuccess := p.startReadinessAfterSetup
	operation := panelOperationSetup
	if isHostedTenantK3SDeployment() {
		afterSuccess = nil
	}
	if isLinodeDockerDeployment() {
		operation = panelOperationLinodeSetup
	}
	return p.startPanelCommand(panelCommandSpec{
		Operation:    operation,
		DisplayName:  "setup",
		TestName:     "TestHaSetup",
		Timeout:      "90m",
		StartLine:    "[control-panel] Starting canonical setup via go test -run ^TestHaSetup$",
		SuccessLine:  "[control-panel] Setup completed successfully",
		AfterSuccess: afterSuccess,
	})
}

func (p *localControlPanel) isolatedRunStartStatus() (bool, string) {
	if blockers := p.sharedWorkspaceResidueBlockers(); len(blockers) > 0 {
		return false, "cleanup shared workspace residue first: " + compactPathList(blockers, 3)
	}
	operation := panelOperationSetup
	if isLinodeDockerDeployment() {
		operation = panelOperationLinodeSetup
	}
	p.mu.Lock()
	conflicting := p.conflictingOperationRunningLocked(operation)
	runningName := p.runningConflictingOperationNameLocked(operation)
	p.mu.Unlock()
	if conflicting {
		return false, fmt.Sprintf("%s is already running", runningName)
	}
	preflight := p.collectPanelPreflightForRunSlotStart()
	if !preflight.Ready {
		return false, preflight.Summary
	}
	if ok, reason := p.customHostnameAvailableForIsolatedRun(); !ok {
		return false, reason
	}
	return true, ""
}

func (p *localControlPanel) customHostnameAvailableForIsolatedRun() (bool, string) {
	if isLinodeDockerDeployment() {
		return true, ""
	}
	prefix, err := settings.ConfiguredCustomHostnamePrefix()
	if err != nil {
		return false, err.Error()
	}
	if prefix == "" {
		return true, ""
	}

	route53FQDN := strings.TrimSpace(viper.GetString("tf_vars.aws_route53_fqdn"))
	requestedHostname := customHostnameFQDN(prefix, route53FQDN)
	for _, record := range p.listRunRecords() {
		recordHostname := customHostnameFQDN(record.CustomHostnamePrefix, record.Route53FQDN)
		if recordHostname == "" || recordHostname != requestedHostname {
			continue
		}
		return false, fmt.Sprintf("custom Rancher hostname %s is already used by run %s; choose a unique custom_hostname_prefix or destroy that slot first", requestedHostname, record.RunID)
	}
	return true, ""
}

func customHostnameFQDN(prefix, route53FQDN string) string {
	prefix = strings.Trim(strings.ToLower(strings.TrimSpace(prefix)), ".")
	route53FQDN = strings.Trim(strings.ToLower(strings.TrimSpace(route53FQDN)), ".")
	if prefix == "" {
		return ""
	}
	if route53FQDN == "" {
		return prefix
	}
	return prefix + "." + route53FQDN
}
