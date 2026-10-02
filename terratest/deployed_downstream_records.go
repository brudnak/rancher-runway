package test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Separate from the original one-downstream-per-HA output format: a deployed
// Rancher can have any number of later-created clusters, across providers.
type deployedDownstreamRecord struct {
	OperationID   string     `json:"operationId"`
	RunID         string     `json:"runId"`
	ParentID      string     `json:"parentId"`
	Kubeconfig    string     `json:"kubeconfig"`
	Namespace     string     `json:"namespace"`
	Name          string     `json:"name"`
	Provider      string     `json:"provider"`
	MachineConfig string     `json:"machineConfig"`
	UID           string     `json:"uid,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	DeletedAt     *time.Time `json:"deletedAt,omitempty"`
}

const deployedOwnerAnnotation = "runway.rancher.io/operation-id"

func saveDeployedDownstream(record deployedDownstreamRecord) error {
	if !validDeployedRecord(record) {
		return fmt.Errorf("invalid downstream ownership record")
	}
	dir := durableDataPath("deployed-downstreams")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".downstream-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(dir, record.OperationID+".json"))
}
func validDeployedRecord(r deployedDownstreamRecord) bool {
	return len(r.OperationID) == 32 && safeRunPathSegment(r.OperationID) == r.OperationID && deployedResourceName.MatchString(r.Name) && r.Namespace == "fleet-default" && (r.Provider == "linode" || r.Provider == "amazonec2") && r.ParentID != ""
}
func readDeployedDownstreams(runID string) ([]deployedDownstreamRecord, error) {
	records := []deployedDownstreamRecord{}
	entries, err := os.ReadDir(durableDataPath("deployed-downstreams"))
	if os.IsNotExist(err) {
		return records, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(durableDataPath("deployed-downstreams"), entry.Name()))
		if err != nil {
			return nil, err
		}
		var record deployedDownstreamRecord
		if json.Unmarshal(raw, &record) != nil || !validDeployedRecord(record) {
			return nil, fmt.Errorf("cannot verify downstream ownership record %s; management destroy stopped", entry.Name())
		}
		if record.RunID == runID && record.DeletedAt == nil {
			records = append(records, record)
		}
	}
	return records, nil
}
