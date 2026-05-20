package test

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/brudnak/ha-rancher-rke2/terratest/settings"
	"github.com/spf13/viper"
)

const defaultRunSlotID = "default"

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
	RunID                string    `json:"runId"`
	SlotID               string    `json:"slotId"`
	SlotName             string    `json:"slotName"`
	Status               string    `json:"status"`
	DeploymentType       string    `json:"deploymentType,omitempty"`
	CreatedAt            time.Time `json:"createdAt"`
	UpdatedAt            time.Time `json:"updatedAt"`
	TotalHAs             int       `json:"totalHAs"`
	AWSPrefix            string    `json:"awsPrefix,omitempty"`
	Route53FQDN          string    `json:"route53Fqdn,omitempty"`
	Owner                string    `json:"owner,omitempty"`
	CustomHostnamePrefix string    `json:"customHostnamePrefix,omitempty"`
	RancherVersions      []string  `json:"rancherVersions,omitempty"`
	TerraformBackend     string    `json:"terraformBackend"`
	TerraformModuleDir   string    `json:"terraformModuleDir,omitempty"`
	TerraformStatePath   string    `json:"terraformStatePath,omitempty"`
	TerraformDataDir     string    `json:"terraformDataDir,omitempty"`
	HAOutputRoot         string    `json:"haOutputRoot"`
	RunFolderPath        string    `json:"runFolderPath,omitempty"`
	RunFolderExists      bool      `json:"runFolderExists"`
	SharedPaths          []string  `json:"sharedPaths"`
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
		afterSuccess = nil
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

func (p *localControlPanel) createCurrentRunRecord(runID string, now time.Time) {
	statePath := p.terraformStatePathForRun(runID)
	dataDir := p.terraformDataDirForRun(runID)
	moduleDir := p.terraformModuleDirForRun(runID)
	slotID := panelRunSlotID(runID)
	customHostnamePrefix, _ := settings.ConfiguredCustomHostnamePrefix()
	if isLinodeDockerDeployment() {
		customHostnamePrefix = ""
	}
	record := panelRunRecord{
		RunID:                runID,
		SlotID:               slotID,
		SlotName:             "Run " + safeRunPathSegment(runID),
		Status:               "setup_running",
		DeploymentType:       deploymentType(),
		CreatedAt:            now,
		UpdatedAt:            now,
		TotalHAs:             p.totalHAs,
		AWSPrefix:            terraformAWSPrefixForRun(viper.GetString("tf_vars.aws_prefix"), runID),
		Route53FQDN:          strings.TrimSpace(viper.GetString("tf_vars.aws_route53_fqdn")),
		Owner:                settings.OwnerLabel(),
		CustomHostnamePrefix: customHostnamePrefix,
		RancherVersions:      requestedRancherVersionsForRunRecord(p.totalHAs),
		TerraformBackend:     terraformBackendLabelForRun(runID, statePath),
		TerraformModuleDir:   moduleDir,
		TerraformStatePath:   statePath,
		TerraformDataDir:     dataDir,
		HAOutputRoot:         p.haOutputRootForRun(runID),
		SharedPaths:          p.sharedWorkspacePathLabels(),
	}
	p.writeCurrentRunRecord(record)
}

func (p *localControlPanel) updateCurrentRunStatus(status string) {
	record, ok := p.readCurrentRunRecord()
	if !ok {
		return
	}
	p.updateRunRecordStatus(record.RunID, status)
}

func (p *localControlPanel) updateRunRecordStatus(runID string, status string) {
	record, ok := p.readRunRecord(runID)
	if !ok {
		return
	}
	record.Status = status
	record.UpdatedAt = time.Now()
	p.writeCurrentRunRecord(record)
}

func (p *localControlPanel) removeCurrentRunRecord() {
	if err := os.Remove(p.currentRunRecordPath()); err != nil && !os.IsNotExist(err) {
		log.Printf("[control-panel] Failed to remove current run record: %v", err)
	}
}

func (p *localControlPanel) removeRunRecord(runID string) {
	safeRunID := safeRunPathSegment(runID)
	if err := os.Remove(p.runRecordPath(safeRunID)); err != nil && !os.IsNotExist(err) {
		log.Printf("[control-panel] Failed to remove run record %s: %v", safeRunID, err)
	}
	if current, ok := p.readCurrentRunRecord(); ok && sameRunID(current.RunID, safeRunID) {
		p.removeCurrentRunRecord()
	}
}

func (p *localControlPanel) readCurrentRunRecord() (panelRunRecord, bool) {
	data, err := os.ReadFile(p.currentRunRecordPath())
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[control-panel] Failed to read current run record: %v", err)
		}
		return panelRunRecord{}, false
	}

	var record panelRunRecord
	if err := json.Unmarshal(data, &record); err != nil {
		log.Printf("[control-panel] Failed to parse current run record: %v", err)
		return panelRunRecord{}, false
	}
	return record, true
}

func (p *localControlPanel) readRunRecord(runID string) (panelRunRecord, bool) {
	safeRunID := safeRunPathSegment(runID)
	data, err := os.ReadFile(p.runRecordPath(safeRunID))
	if err == nil {
		var record panelRunRecord
		if parseErr := json.Unmarshal(data, &record); parseErr != nil {
			log.Printf("[control-panel] Failed to parse run record %s: %v", safeRunID, parseErr)
			return panelRunRecord{}, false
		}
		return p.enrichRunRecord(record), true
	}
	if err != nil && !os.IsNotExist(err) {
		log.Printf("[control-panel] Failed to read run record %s: %v", safeRunID, err)
	}

	if current, ok := p.readCurrentRunRecord(); ok && sameRunID(current.RunID, safeRunID) {
		return p.enrichRunRecord(current), true
	}
	return panelRunRecord{}, false
}

func (p *localControlPanel) writeCurrentRunRecord(record panelRunRecord) {
	record.RunID = safeRunPathSegment(record.RunID)
	if strings.TrimSpace(record.SlotID) == "" || record.SlotID == defaultRunSlotID {
		record.SlotID = panelRunSlotID(record.RunID)
	}
	if strings.TrimSpace(record.SlotName) == "" || record.SlotName == "Default local run" {
		record.SlotName = "Run " + record.RunID
	}
	p.writeRunRecord(record)

	path := p.currentRunRecordPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Printf("[control-panel] Failed to create current run record directory: %v", err)
		return
	}

	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		log.Printf("[control-panel] Failed to encode current run record: %v", err)
		return
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		log.Printf("[control-panel] Failed to write current run record %s: %v", path, err)
	}
}

func (p *localControlPanel) writeRunRecord(record panelRunRecord) {
	record.RunID = safeRunPathSegment(record.RunID)
	path := p.runRecordPath(record.RunID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Printf("[control-panel] Failed to create run record directory: %v", err)
		return
	}

	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		log.Printf("[control-panel] Failed to encode run record: %v", err)
		return
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		log.Printf("[control-panel] Failed to write run record %s: %v", path, err)
	}
}

func (p *localControlPanel) currentRunRecordPath() string {
	return filepath.Join(automationOutputDir(), "control-panel", "current-run.json")
}

func (p *localControlPanel) runRecordsDir() string {
	return filepath.Join(automationOutputDir(), "control-panel", "runs")
}

func (p *localControlPanel) runRecordPath(runID string) string {
	return filepath.Join(p.runRecordsDir(), safeRunPathSegment(runID)+".json")
}

func (p *localControlPanel) listRunRecords() []panelRunRecord {
	recordsByID := map[string]panelRunRecord{}
	entries, err := os.ReadDir(p.runRecordsDir())
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
				continue
			}
			data, readErr := os.ReadFile(filepath.Join(p.runRecordsDir(), entry.Name()))
			if readErr != nil {
				log.Printf("[control-panel] Failed to read run record %s: %v", entry.Name(), readErr)
				continue
			}
			var record panelRunRecord
			if parseErr := json.Unmarshal(data, &record); parseErr != nil {
				log.Printf("[control-panel] Failed to parse run record %s: %v", entry.Name(), parseErr)
				continue
			}
			if strings.TrimSpace(record.RunID) == "" {
				continue
			}
			record.RunID = safeRunPathSegment(record.RunID)
			record = p.enrichRunRecord(record)
			recordsByID[record.RunID] = record
		}
	} else if !os.IsNotExist(err) {
		log.Printf("[control-panel] Failed to list run records: %v", err)
	}

	if current, ok := p.readCurrentRunRecord(); ok && strings.TrimSpace(current.RunID) != "" {
		current.RunID = safeRunPathSegment(current.RunID)
		current = p.enrichRunRecord(current)
		recordsByID[current.RunID] = current
		p.writeRunRecord(current)
	}

	records := make([]panelRunRecord, 0, len(recordsByID))
	for _, record := range recordsByID {
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].CreatedAt.After(records[j].CreatedAt)
	})
	return records
}

func (p *localControlPanel) enrichRunRecord(record panelRunRecord) panelRunRecord {
	record = p.normalizeRunRecordArtifactPaths(record)
	record.RunFolderPath = runFolderPathForRecord(record)
	record.RunFolderExists = record.RunFolderPath != "" && pathExists(record.RunFolderPath)
	return record
}

func (p *localControlPanel) normalizeRunRecordArtifactPaths(record panelRunRecord) panelRunRecord {
	runID := safeRunPathSegment(record.RunID)
	record.TerraformModuleDir = p.remapRunArtifactPath(record.TerraformModuleDir, runID)
	record.TerraformStatePath = p.remapRunArtifactPath(record.TerraformStatePath, runID)
	record.TerraformDataDir = p.remapRunArtifactPath(record.TerraformDataDir, runID)
	record.HAOutputRoot = p.remapRunArtifactPath(record.HAOutputRoot, runID)
	record.RunFolderPath = p.remapRunArtifactPath(record.RunFolderPath, runID)
	if strings.HasPrefix(record.TerraformBackend, "local (") && strings.HasSuffix(record.TerraformBackend, ")") {
		backendPath := strings.TrimSuffix(strings.TrimPrefix(record.TerraformBackend, "local ("), ")")
		record.TerraformBackend = "local (" + p.remapRunArtifactPath(backendPath, runID) + ")"
	}
	return record
}

func (p *localControlPanel) remapRunArtifactPath(rawPath string, runID string) string {
	path := strings.TrimSpace(rawPath)
	if path == "" || runID == "" {
		return path
	}

	cleanPath := filepath.Clean(path)
	if !filepath.IsAbs(cleanPath) || pathExists(cleanPath) {
		return cleanPath
	}

	parts := strings.Split(filepath.ToSlash(cleanPath), "/")
	for i := 0; i+2 < len(parts); i++ {
		if parts[i] != "automation-output" || parts[i+1] != "runs" || parts[i+2] != runID {
			continue
		}
		suffix := filepath.FromSlash(strings.Join(parts[i+3:], "/"))
		return filepath.Join(p.testDir, "automation-output", "runs", runID, suffix)
	}
	return cleanPath
}

func runFolderPathForRecord(record panelRunRecord) string {
	if path := strings.TrimSpace(record.TerraformModuleDir); path != "" {
		return strings.TrimSuffix(filepath.Clean(path), string(filepath.Separator)+filepath.Join("terraform", "module"))
	}
	if path := strings.TrimSpace(record.TerraformStatePath); path != "" {
		return strings.TrimSuffix(filepath.Clean(path), string(filepath.Separator)+filepath.Join("terraform", "terraform.tfstate"))
	}
	if path := strings.TrimSpace(record.HAOutputRoot); path != "" {
		return strings.TrimSuffix(filepath.Clean(path), string(filepath.Separator)+"ha")
	}
	return ""
}

func (p *localControlPanel) currentHAOutputRoot() string {
	record, ok := p.readCurrentRunRecord()
	if !ok {
		return ""
	}
	return strings.TrimSpace(record.HAOutputRoot)
}

func (p *localControlPanel) haOutputRootForRun(runID string) string {
	return filepath.Join(p.testDir, "automation-output", "runs", safeRunPathSegment(runID), "ha")
}

func (p *localControlPanel) terraformStatePathForRun(runID string) string {
	return filepath.Join(p.testDir, "automation-output", "runs", safeRunPathSegment(runID), "terraform", "terraform.tfstate")
}

func (p *localControlPanel) terraformDataDirForRun(runID string) string {
	return filepath.Join(p.testDir, "automation-output", "runs", safeRunPathSegment(runID), "terraform", ".terraform")
}

func (p *localControlPanel) terraformModuleDirForRun(runID string) string {
	return filepath.Join(p.testDir, "automation-output", "runs", safeRunPathSegment(runID), "terraform", "module")
}

func (p *localControlPanel) prepareTerraformModuleForRun(runID string) error {
	sourceDir := p.terraformSourceModuleDir()
	targetDir := p.terraformModuleDirForRun(runID)
	if err := os.RemoveAll(targetDir); err != nil {
		return fmt.Errorf("failed to clear run Terraform module %s: %w", targetDir, err)
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("failed to create run Terraform module %s: %w", targetDir, err)
	}

	return filepath.WalkDir(sourceDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if shouldSkipTerraformModuleCopy(rel, entry) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		target := filepath.Join(targetDir, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to copy symlink from Terraform module: %s", path)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
}

func (p *localControlPanel) terraformSourceModuleDir() string {
	if isLinodeDockerDeployment() {
		return filepath.Join(p.repoRoot, "modules", "linode-docker-cattle")
	}
	return filepath.Join(p.repoRoot, "modules", "aws")
}

func shouldSkipTerraformModuleCopy(rel string, entry os.DirEntry) bool {
	name := entry.Name()
	if entry.IsDir() && name == ".terraform" {
		return true
	}
	switch name {
	case ".terraform.lock.hcl", "backend.tf", "terraform.tfvars", "terraform.tfstate", "terraform.tfstate.backup":
		return true
	}
	return strings.HasSuffix(name, ".tfstate") || strings.HasPrefix(name, ".terraform.")
}

func (p *localControlPanel) haInstanceDir(instanceNum int) string {
	if root := p.currentHAOutputRoot(); root != "" {
		return filepath.Join(root, fmt.Sprintf("high-availability-%d", instanceNum))
	}
	return filepath.Join(p.testDir, fmt.Sprintf("high-availability-%d", instanceNum))
}

func (p *localControlPanel) haInstanceDirForRun(record panelRunRecord, instanceNum int) string {
	if root := strings.TrimSpace(record.HAOutputRoot); root != "" {
		return filepath.Join(root, fmt.Sprintf("high-availability-%d", instanceNum))
	}
	return p.haInstanceDir(instanceNum)
}

func (p *localControlPanel) hostedTenantInstanceDirForRun(record panelRunRecord, instanceNum int) string {
	root := strings.TrimSpace(record.HAOutputRoot)
	if root == "" {
		root = p.currentHAOutputRoot()
	}
	name := "host-rancher"
	if instanceNum > 1 {
		name = fmt.Sprintf("tenant-%d-rancher", instanceNum-1)
	}
	if root == "" {
		return name
	}
	return filepath.Join(root, name)
}

func runScopedClusterName(runID string, name string) string {
	runID = safeRunPathSegment(runID)
	if runID == "" || runID == "unknown" {
		return name
	}
	return fmt.Sprintf("%s (%s)", name, runID)
}

func runScopedDownloadName(runID string, name string) string {
	runID = safeRunPathSegment(runID)
	if runID == "" || runID == "unknown" {
		return name
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	return fmt.Sprintf("%s-%s%s", base, runID, ext)
}

func safeRunPathSegment(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	cleaned := strings.Trim(b.String(), "-_")
	if cleaned == "" {
		return "unknown"
	}
	return cleaned
}

func (p *localControlPanel) hasWorkspaceRunResidue() bool {
	if _, ok := p.readCurrentRunRecord(); ok {
		return true
	}
	return len(p.workspaceBlockers()) > 0
}

func (p *localControlPanel) workspaceBlockers() []string {
	var blockers []string
	if _, ok := p.readCurrentRunRecord(); ok {
		blockers = append(blockers, p.currentRunRecordPath())
	}
	entries, err := os.ReadDir(p.runRecordsDir())
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
				continue
			}
			blockers = append(blockers, filepath.Join(p.runRecordsDir(), entry.Name()))
		}
	}
	blockers = append(blockers, p.sharedWorkspaceResidueBlockers()...)
	return blockers
}

func (p *localControlPanel) sharedWorkspaceResidueBlockers() []string {
	var blockers []string
	blockers = append(blockers, existingGlobPaths(filepath.Join(p.testDir, "high-availability-*"))...)
	for _, name := range []string{
		".terraform.lock.hcl",
		"terraform.tfstate",
		"terraform.tfstate.backup",
		".terraform",
		"backend.tf",
		"terraform.tfvars",
	} {
		path := filepath.Join(p.repoRoot, "modules", "aws", name)
		if _, err := os.Stat(path); err == nil {
			blockers = append(blockers, path)
		}
	}
	return blockers
}

func (p *localControlPanel) cleanLocalArtifacts() (localArtifactCleanupResult, error) {
	if records := p.listRunRecords(); len(records) > 0 {
		return localArtifactCleanupResult{}, fmt.Errorf("destroy recorded run slots before cleaning local artifacts; this keeps Terraform destroy targets available")
	}

	var result localArtifactCleanupResult
	for _, path := range p.localArtifactCleanupTargets() {
		if removeLocalArtifactPath(path) {
			result.Removed = append(result.Removed, path)
		}
	}

	for _, dir := range []string{
		p.runRecordsDir(),
		filepath.Join(p.testDir, "automation-output", "control-panel"),
		filepath.Join(p.testDir, "automation-output"),
	} {
		removeEmptyDir(dir)
	}

	sort.Strings(result.Removed)
	return result, nil
}

func (p *localControlPanel) localArtifactCleanupTargets() []string {
	targets := p.sharedWorkspaceResidueBlockers()
	targets = append(targets,
		filepath.Join(p.testDir, "automation-output", "runs"),
		p.currentRunRecordPath(),
		filepath.Join(p.testDir, "automation-output", "control-panel", "lifecycle-state.json"),
	)
	targets = append(targets, existingGlobPaths(filepath.Join(p.testDir, "automation-output", "rancher-resolution-*.json"))...)
	targets = append(targets, existingGlobPaths(filepath.Join(p.testDir, "automation-output", "control-panel", "run-*.yaml"))...)
	return uniqueExistingPaths(targets)
}

func removeLocalArtifactPath(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	if _, err := os.Stat(path); err != nil {
		return false
	}
	if err := os.RemoveAll(path); err != nil {
		log.Printf("[control-panel] Failed to remove local artifact %s: %v", path, err)
		return false
	}
	return true
}

func uniqueExistingPaths(paths []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, path := range paths {
		path = filepath.Clean(strings.TrimSpace(path))
		if path == "." || seen[path] {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			continue
		}
		seen[path] = true
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}

func removeEmptyDir(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) > 0 {
		return
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		log.Printf("[control-panel] Failed to remove empty directory %s: %v", path, err)
	}
}

func (p *localControlPanel) sharedWorkspacePathLabels() []string {
	paths := []string{
		filepath.Join(p.repoRoot, "modules", "aws"),
		filepath.Join(p.testDir, "high-availability-*"),
		filepath.Join(p.testDir, "automation-output", "runs"),
		automationOutputDir(),
	}
	return paths
}

func requestedRancherVersionsForRunRecord(totalHAs int) []string {
	versions := nonEmptyStringSlice(viper.GetStringSlice("rancher.versions"))
	if len(versions) > 0 {
		return versions
	}

	version := strings.TrimSpace(viper.GetString("rancher.version"))
	if version == "" {
		return nil
	}
	if totalHAs <= 1 {
		return []string{version}
	}

	out := make([]string, totalHAs)
	for i := range out {
		out[i] = version
	}
	return out
}

func terraformBackendLabelForRun(runID string, localStatePath string) string {
	backendConfig, err := terraformBackendConfigFromEnvForRun(runID, localStatePath)
	if err != nil {
		return "invalid: " + err.Error()
	}
	if backendConfig == nil {
		return "local"
	}
	if path := strings.TrimSpace(fmt.Sprint(backendConfig["path"])); path != "" {
		return "local (" + path + ")"
	}

	bucket := fmt.Sprint(backendConfig["bucket"])
	key := fmt.Sprint(backendConfig["key"])
	region := fmt.Sprint(backendConfig["region"])
	return fmt.Sprintf("s3://%s/%s (%s)", bucket, key, region)
}

func panelRunSlotID(runID string) string {
	return "slot-" + safeRunPathSegment(runID)
}

func sameRunID(left string, right string) bool {
	return safeRunPathSegment(left) == safeRunPathSegment(right)
}

func primaryPanelRunRecord(records []panelRunRecord) (panelRunRecord, bool) {
	if len(records) == 0 {
		return panelRunRecord{}, false
	}
	primary := records[0]
	for _, record := range records[1:] {
		if record.UpdatedAt.After(primary.UpdatedAt) {
			primary = record
		}
	}
	return primary, true
}

func upsertPanelRunRecord(records []panelRunRecord, updated panelRunRecord) []panelRunRecord {
	out := append([]panelRunRecord(nil), records...)
	for i := range out {
		if sameRunID(out[i].RunID, updated.RunID) {
			out[i] = updated
			return out
		}
	}
	return append(out, updated)
}
