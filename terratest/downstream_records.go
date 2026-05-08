package test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const defaultLinodeNamespace = "fleet-default"

type downstreamOutputRecord struct {
	HAIndex             int    `json:"ha_index"`
	RancherHost         string `json:"rancher_host"`
	ClusterName         string `json:"cluster_name"`
	ManagementClusterID string `json:"management_cluster_id"`
	KubeconfigPath      string `json:"kubeconfig_path"`
	K3SVersion          string `json:"k3s_version"`
	LinodeRegion        string `json:"linode_region"`
	LinodeType          string `json:"linode_type"`
	LinodeImage         string `json:"linode_image"`
	MachineConfig       string `json:"machine_config"`
	SecretName          string `json:"secret_name"`
	Namespace           string `json:"namespace"`
}

func readDownstreamOutputRecords() ([]downstreamOutputRecord, error) {
	paths, err := filepath.Glob(automationOutputPath("downstream-ha-*.json"))
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
		records = append(records, record)
	}
	return records, nil
}
