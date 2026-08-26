package test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const defaultLinodeNamespace = "fleet-default"

type downstreamOutputRecord struct {
	RunID               string `json:"run_id,omitempty"`
	HAIndex             int    `json:"ha_index"`
	RancherHost         string `json:"rancher_host"`
	ClusterName         string `json:"cluster_name"`
	ManagementClusterID string `json:"management_cluster_id"`
	KubeconfigPath      string `json:"kubeconfig_path"`
	Distribution        string `json:"distribution,omitempty"`
	KubernetesVersion   string `json:"kubernetes_version,omitempty"`
	K3SVersion          string `json:"k3s_version,omitempty"`
	Phase               string `json:"phase,omitempty"`
	Error               string `json:"error,omitempty"`
	LinodeRegion        string `json:"linode_region"`
	LinodeType          string `json:"linode_type"`
	LinodeImage         string `json:"linode_image"`
	MachineConfig       string `json:"machine_config"`
	SecretName          string `json:"secret_name"`
	Namespace           string `json:"namespace"`
}

func readDownstreamOutputRecords() ([]downstreamOutputRecord, error) {
	return readDownstreamOutputRecordsForRun(os.Getenv(runIDEnv))
}

func readDownstreamOutputRecordsForRun(runID string) ([]downstreamOutputRecord, error) {
	runID = downstreamOutputRunID(runID)
	paths, err := filepath.Glob(filepath.Join(downstreamOutputDirForRun(runID), "downstream-ha-*.json"))
	if err != nil {
		return nil, err
	}

	records := make([]downstreamOutputRecord, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var record downstreamOutputRecord
		if err := json.Unmarshal(data, &record); err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", path, err)
		}
		if record.ClusterName == "" || record.HAIndex < 1 {
			return nil, fmt.Errorf("invalid downstream output record %s", path)
		}
		if record.Namespace == "" {
			record.Namespace = defaultLinodeNamespace
		}
		if err := validateDownstreamRecordRunID(&record, runID, path); err != nil {
			return nil, err
		}
		normalizeDownstreamOutputRecord(&record)
		records = append(records, record)
	}
	sort.SliceStable(records, func(i, j int) bool { return records[i].HAIndex < records[j].HAIndex })
	return records, nil
}

func readDownstreamOutputRecord(haIndex int) (downstreamOutputRecord, bool, error) {
	runID := downstreamOutputRunID(os.Getenv(runIDEnv))
	path := downstreamOutputRecordPathForRun(runID, haIndex)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return downstreamOutputRecord{}, false, nil
	}
	if err != nil {
		return downstreamOutputRecord{}, false, err
	}
	var record downstreamOutputRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return downstreamOutputRecord{}, false, fmt.Errorf("failed to parse %s: %w", path, err)
	}
	if record.ClusterName == "" || record.HAIndex != haIndex {
		return downstreamOutputRecord{}, false, fmt.Errorf("invalid downstream output record %s", path)
	}
	if record.Namespace == "" {
		record.Namespace = defaultLinodeNamespace
	}
	if err := validateDownstreamRecordRunID(&record, runID, path); err != nil {
		return downstreamOutputRecord{}, false, err
	}
	normalizeDownstreamOutputRecord(&record)
	return record, true, nil
}

func writeDownstreamOutputRecord(record downstreamOutputRecord) error {
	if record.HAIndex < 1 || strings.TrimSpace(record.ClusterName) == "" {
		return fmt.Errorf("downstream output record requires a positive HA index and cluster name")
	}
	if strings.TrimSpace(record.Namespace) == "" {
		record.Namespace = defaultLinodeNamespace
	}
	runID := downstreamOutputRunID(record.RunID)
	if runID == "" {
		runID = downstreamOutputRunID(os.Getenv(runIDEnv))
	}
	record.RunID = runID
	normalizeDownstreamOutputRecord(&record)
	outputDir := downstreamOutputDirForRun(runID)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	tempFile, err := os.CreateTemp(outputDir, fmt.Sprintf(".downstream-ha-%d-*.json", record.HAIndex))
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if err := tempFile.Chmod(0o600); err != nil {
		_ = tempFile.Close()
		return err
	}
	if _, err := tempFile.Write(append(data, '\n')); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, downstreamOutputRecordPathForRun(runID, record.HAIndex)); err != nil {
		return err
	}
	removeTemp = false
	return nil
}

func removeDownstreamOutputArtifacts(haIndex int) error {
	return removeDownstreamOutputArtifactsForRun(os.Getenv(runIDEnv), haIndex)
}

func removeDownstreamOutputArtifactsForRun(runID string, haIndex int) error {
	runID = downstreamOutputRunID(runID)
	auxiliaryPaths := []string{
		downstreamOutputPathForRun(runID, fmt.Sprintf("downstream-ha-%d.env", haIndex)),
		downstreamOutputPathForRun(runID, fmt.Sprintf("downstream-ha-%d.kubeconfig", haIndex)),
	}
	var errs []error
	for _, path := range auxiliaryPaths {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, fmt.Errorf("remove %s: %w", path, err))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	recordPath := downstreamOutputRecordPathForRun(runID, haIndex)
	if err := os.Remove(recordPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", recordPath, err)
	}
	return nil
}

func downstreamOutputRecordPath(haIndex int) string {
	return downstreamOutputRecordPathForRun(os.Getenv(runIDEnv), haIndex)
}

func downstreamOutputRecordPathForRun(runID string, haIndex int) string {
	return downstreamOutputPathForRun(runID, fmt.Sprintf("downstream-ha-%d.json", haIndex))
}

func downstreamOutputPathForRun(runID, name string) string {
	return filepath.Join(downstreamOutputDirForRun(runID), name)
}

func downstreamOutputDirForRun(runID string) string {
	runID = downstreamOutputRunID(runID)
	if runID == "" {
		return automationOutputDir()
	}
	return filepath.Join(automationOutputDir(), "runs", runID, "downstream")
}

func downstreamOutputRunID(runID string) string {
	if strings.TrimSpace(runID) == "" {
		return ""
	}
	return safeRunPathSegment(runID)
}

func validateDownstreamRecordRunID(record *downstreamOutputRecord, runID, path string) error {
	recordedRunID := downstreamOutputRunID(record.RunID)
	if recordedRunID != "" && recordedRunID != runID {
		return fmt.Errorf("downstream output record %s belongs to run %s, not %s", path, recordedRunID, runID)
	}
	record.RunID = runID
	return nil
}

func normalizeDownstreamOutputRecord(record *downstreamOutputRecord) {
	record.Distribution = strings.ToLower(strings.TrimSpace(record.Distribution))
	record.KubernetesVersion = normalizeDownstreamKubernetesVersion(record.KubernetesVersion)
	record.K3SVersion = normalizeDownstreamKubernetesVersion(record.K3SVersion)
	if record.KubernetesVersion == "" {
		record.KubernetesVersion = record.K3SVersion
	}
	if record.Distribution == "" {
		if strings.Contains(strings.ToLower(record.KubernetesVersion), "+rke2") {
			record.Distribution = "rke2"
		} else {
			record.Distribution = "k3s"
		}
	}
	if record.Distribution == "k3s" && record.K3SVersion == "" {
		record.K3SVersion = record.KubernetesVersion
	}
	if strings.TrimSpace(record.Phase) == "" {
		if strings.TrimSpace(record.ManagementClusterID) != "" {
			record.Phase = "active"
		} else {
			record.Phase = "provisioning"
		}
	}
}
