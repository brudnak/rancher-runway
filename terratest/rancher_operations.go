package test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type rancherOperations struct {
	mu     sync.Mutex
	plans  map[string]*rancherUpgradePlan
	active map[string]string
}

type rancherOperationEvent struct {
	At      time.Time `json:"at"`
	Message string    `json:"message"`
}

type rancherOperationRecord struct {
	ID        string                  `json:"id"`
	ClusterID string                  `json:"clusterId"`
	Kind      string                  `json:"kind"`
	Status    string                  `json:"status"`
	Started   time.Time               `json:"started"`
	Finished  *time.Time              `json:"finished,omitempty"`
	From      string                  `json:"from,omitempty"`
	To        string                  `json:"to,omitempty"`
	Plan      *rancherUpgradePlan     `json:"plan,omitempty"`
	Resources []string                `json:"resources,omitempty"`
	Events    []rancherOperationEvent `json:"events"`
}

type rancherUpgradePlan struct {
	SystemDefaultRegistry   *string               `json:"systemDefaultRegistry,omitempty"`
	ImageRevision           string                `json:"imageRevision,omitempty"`
	ImageOSSRevision        string                `json:"imageOSSRevision,omitempty"`
	ImageCanonicalReference string                `json:"imageCanonicalReference,omitempty"`
	ID                      string                `json:"id"`
	ClusterID               string                `json:"clusterId"`
	Created                 time.Time             `json:"created"`
	Before                  rancherInstalledState `json:"before"`
	Repository              string                `json:"repository"`
	Chart                   helmLabChartVersion   `json:"chart"`
	Image                   string                `json:"image"`
	ImageTag                string                `json:"imageTag"`
	Digest                  string                `json:"digest"`
	TargetVersionSource     string                `json:"targetVersionSource,omitempty"`
	TargetVersionLabel      string                `json:"targetVersionLabel,omitempty"`
	TargetVersion           string                `json:"targetVersion"`
	AgentImage              string                `json:"agentImage"`
	AgentDigest             string                `json:"agentDigest"`
	Experimental            bool                  `json:"experimental"`
	Checks                  []string              `json:"checks"`
	Warnings                []string              `json:"warnings"`
	imageFields             bool                  // Selected chart uses image.registry/repository/tag.
	kubeconfig              string
	archive                 []byte
}

type rancherInstalledState struct {
	Version  string    `json:"version"`
	Image    string    `json:"image"`
	Revision int       `json:"revision"`
	Chart    string    `json:"chart"`
	Pods     []podView `json:"pods"`
}

type rancherOperationRequest struct {
	Action                string `json:"action"`
	ClusterID             string `json:"clusterId"`
	Distribution          string `json:"distribution"`
	Channel               string `json:"channel"`
	Registry              string `json:"registry"`
	Version               string `json:"version"`
	Image                 string `json:"image"`
	PlanID                string `json:"planId"`
	BackupConfirmed       bool   `json:"backupConfirmed"`
	ExperimentalConfirmed bool   `json:"experimentalConfirmed"`
	// These fields exist only for the lifetime of a downstream request/worker.
	Token             string            `json:"token"`
	Insecure          bool              `json:"insecure"`
	Provider          string            `json:"provider"`
	KubernetesVersion string            `json:"kubernetesVersion"`
	Name              string            `json:"name"`
	Quantity          int               `json:"quantity"`
	CredentialID      string            `json:"credentialId"`
	Credentials       map[string]string `json:"credentials"`
	UseEnvironment    bool              `json:"useEnvironment"`
	Machine           map[string]any    `json:"machine"`
	Confirmed         bool              `json:"confirmed"`
}

func operationID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

func (p *localControlPanel) rancherOperationsDir() string {
	return durableDataPath("rancher-operations")
}

func (p *localControlPanel) saveRancherOperation(record *rancherOperationRecord) error {
	dir := p.rancherOperationsDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".operation-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(dir, record.ID+".json"))
}

func (p *localControlPanel) operationEvent(record *rancherOperationRecord, message string) error {
	record.Events = append(record.Events, rancherOperationEvent{time.Now().UTC(), message})
	return p.saveRancherOperation(record)
}

func (p *localControlPanel) reserveRancherOperation(clusterID, id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rancherOps.mu.Lock()
	defer p.rancherOps.mu.Unlock()
	if p.rancherOps.active == nil {
		p.rancherOps.active = map[string]string{}
	}
	if p.rancherOps.active["linode-inventory"] != "" {
		return fmt.Errorf("wait for Linode resource cleanup to finish")
	}
	if p.rancherOps.active[clusterID] != "" {
		return fmt.Errorf("another Rancher operation is already running for this server")
	}
	// Also reject overlapping Runway provisioning and destructive lifecycle work.
	for _, op := range p.operations {
		if op != nil && op.Running {
			return fmt.Errorf("wait for the current Runway lifecycle operation to finish")
		}
	}
	p.rancherOps.active[clusterID] = id
	return nil
}

func (p *localControlPanel) releaseRancherOperation(clusterID string) {
	p.rancherOps.mu.Lock()
	delete(p.rancherOps.active, clusterID)
	p.rancherOps.mu.Unlock()
}

func (p *localControlPanel) finishRancherOperation(record *rancherOperationRecord, err error) {
	record.Status = "succeeded"
	message := "Readiness checks passed. Operation complete."
	if err != nil {
		record.Status = "failed"
		message = err.Error()
	}
	now := time.Now().UTC()
	record.Finished = &now
	if saveErr := p.operationEvent(record, message); saveErr != nil {
		fmt.Fprintf(os.Stderr, "Rancher operation %s history write failed: %v\n", record.ID, saveErr)
	}
	if cluster, found := p.clusterFromSnapshot(record.ClusterID); found {
		ctx, cancel := context.WithTimeout(p.workers.Context(), clusterDeploymentProbeTimeout)
		details := p.collectClusterDeploymentDetails(ctx, cluster)
		cancel()
		p.retainDeploymentDetails(details)
	}
	p.historyMu.Lock()
	delete(p.historyProbes, record.ClusterID)
	p.historyMu.Unlock()
	p.releaseRancherOperation(record.ClusterID)
}
