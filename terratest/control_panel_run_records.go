package test

import (
	"encoding/json"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/terratest/settings"
	"github.com/spf13/viper"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (p *localControlPanel) createCurrentRunRecord(runID string, now time.Time) {
	totalHAs := configuredRancherInstanceCount()
	if totalHAs < 1 {
		totalHAs = p.totalHAs
	}
	statePath := p.terraformStatePathForRun(runID)
	dataDir := p.terraformDataDirForRun(runID)
	moduleDir := p.terraformModuleDirForRun(runID)
	slotID := panelRunSlotID(runID)
	customHostnamePrefix, _ := settings.ConfiguredCustomHostnamePrefix()
	if isLinodeDockerDeployment() {
		customHostnamePrefix = ""
	}
	gpuWorker := settings.CurrentGPUWorkerConfig()
	gpuWorkerEnabled := deploymentType() == deploymentTypeHARKE2 && gpuWorker.Enabled
	downstreamPlans := settings.CurrentLinodeDownstreamPlans(totalHAs)
	downstreamStatus := ""
	if deploymentType() == deploymentTypeHARKE2 && settings.AnyLinodeDownstreamPlanEnabled(downstreamPlans) {
		downstreamStatus = panelDownstreamStatusPending
	}
	record := panelRunRecord{
		RunID:                 runID,
		SlotID:                slotID,
		SlotName:              "Run " + safeRunPathSegment(runID),
		Status:                "setup_running",
		DeploymentType:        deploymentType(),
		ConfigSnapshot:        pathExists(p.runConfigPath(runID)),
		CreatedAt:             now,
		UpdatedAt:             now,
		TotalHAs:              totalHAs,
		AWSPrefix:             terraformAWSPrefixForRun(viper.GetString("tf_vars.aws_prefix"), runID),
		Route53FQDN:           strings.TrimSpace(viper.GetString("tf_vars.aws_route53_fqdn")),
		Owner:                 settings.OwnerLabel(),
		CustomHostnamePrefix:  customHostnamePrefix,
		RancherVersions:       requestedRancherVersionsForRunRecord(totalHAs),
		DownstreamLinodePlans: append([]settings.LinodeDownstreamPlan(nil), downstreamPlans...),
		DownstreamStatus:      downstreamStatus,
		GPUWorkerEnabled:      gpuWorkerEnabled,
		GPUWorkerInstanceType: gpuWorker.InstanceType,
		TerraformBackend:      terraformBackendLabelForRun(runID, statePath),
		TerraformModuleDir:    moduleDir,
		TerraformStatePath:    statePath,
		TerraformDataDir:      dataDir,
		HAOutputRoot:          p.haOutputRootForRun(runID),
		SharedPaths:           p.sharedWorkspacePathLabels(),
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
	p.runRecordsMu.Lock()
	defer p.runRecordsMu.Unlock()
	record, ok := p.readRunRecord(runID)
	if !ok {
		return
	}
	record.Status = status
	record.UpdatedAt = time.Now()
	p.writeRunRecord(record)
	if current, ok := p.readCurrentRunRecord(); ok && sameRunID(current.RunID, runID) {
		p.writeCurrentRunRecordLocked(record)
	}
}

func (p *localControlPanel) updateRunDownstreamStatus(runID, status, errorMessage string) {
	p.runRecordsMu.Lock()
	defer p.runRecordsMu.Unlock()
	record, ok := p.readRunRecord(runID)
	if !ok {
		return
	}
	now := time.Now()
	record.DownstreamStatus = status
	record.DownstreamError = strings.TrimSpace(errorMessage)
	record.DownstreamUpdatedAt = &now
	record.UpdatedAt = now
	p.writeRunRecord(record)
	if current, currentOK := p.readCurrentRunRecord(); currentOK && sameRunID(current.RunID, record.RunID) {
		p.writeCurrentRunRecordLocked(record)
	}
}

func (p *localControlPanel) removeCurrentRunRecord() {
	if err := os.Remove(p.currentRunRecordPath()); err != nil && !os.IsNotExist(err) {
		log.Printf("[control-panel] Failed to remove current run record: %v", err)
	}
}

func (p *localControlPanel) removeRunRecord(runID string) {
	p.runRecordsMu.Lock()
	safeRunID := safeRunPathSegment(runID)
	if err := os.Remove(p.runRecordPath(safeRunID)); err != nil && !os.IsNotExist(err) {
		log.Printf("[control-panel] Failed to remove run record %s: %v", safeRunID, err)
		p.runRecordsMu.Unlock()
		return
	}
	if current, ok := p.readCurrentRunRecord(); ok && sameRunID(current.RunID, safeRunID) {
		p.removeCurrentRunRecord()
	}
	p.runRecordsMu.Unlock()

	// Retain logs for recorded runs, without growing lifecycle state forever
	// after their resources and records have been removed.
	p.mu.Lock()
	defer p.mu.Unlock()
	changed := false
	for name, op := range p.operations {
		if name != panelOperationBase(name) && isProvisioningPanelOperation(name) &&
			op != nil && sameRunID(op.RunID, safeRunID) && !panelOperationActive(op) {
			delete(p.operations, name)
			changed = true
		}
	}
	if changed {
		p.persistOperationsLocked()
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
	p.runRecordsMu.Lock()
	defer p.runRecordsMu.Unlock()
	p.writeCurrentRunRecordLocked(record)
}

func (p *localControlPanel) writeCurrentRunRecordLocked(record panelRunRecord) {
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
	if err := writePrivateConfigAtomically(path, data); err != nil {
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
	if err := writePrivateConfigAtomically(path, data); err != nil {
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
		if _, exists := recordsByID[current.RunID]; !exists {
			recordsByID[current.RunID] = current
			// Preserve legacy current-only records before a new setup selects a
			// different current run. Never overwrite a concurrent status update.
			p.runRecordsMu.Lock()
			if _, err := os.Stat(p.runRecordPath(current.RunID)); os.IsNotExist(err) {
				p.writeRunRecord(current)
			}
			p.runRecordsMu.Unlock()
		}
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
