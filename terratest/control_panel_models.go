package test

import (
	"github.com/brudnak/ha-rancher-rke2/internal/buildinfo"
	"time"
)

type GPUInfrastructureSummary struct {
	Active  bool                      `json:"active"`
	Count   int                       `json:"count"`
	Details []GPUInfrastructureDetail `json:"details,omitempty"`
}

type GPUInfrastructureDetail struct {
	RunID        string `json:"runId,omitempty"`
	HAIndex      int    `json:"haIndex,omitempty"`
	IP           string `json:"ip,omitempty"`
	PrivateIP    string `json:"privateIp,omitempty"`
	InstanceType string `json:"instanceType,omitempty"`
}

type panelState struct {
	TestLab       testLabActivityState      `json:"testLab"`
	Panel         panelSessionState         `json:"panel"`
	Workspace     panelWorkspaceState       `json:"workspace"`
	Setup         panelOperationSnapshot    `json:"setup"`
	Readiness     panelOperationSnapshot    `json:"readiness"`
	Downstream    panelOperationSnapshot    `json:"downstream"`
	LinodeSetup   panelOperationSnapshot    `json:"linodeSetup"`
	LinodeCleanup panelOperationSnapshot    `json:"linodeCleanup"`
	Steve         steveLabPanelState        `json:"steve"`
	K3D           k3dLabPanelState          `json:"k3d"`
	Clusters      panelClusterState         `json:"clusters"`
	AWS           panelAWSInventoryState    `json:"aws"`
	AWSCleanup    awsCleanupSnapshot        `json:"awsCleanup"`
	Cleanup       panelOperationSnapshot    `json:"cleanup"`
	CleanupBatch  panelCleanupBatchSnapshot `json:"cleanupBatch"`
	Costs         panelCostHistoryState     `json:"costs"`
}

type panelSessionState struct {
	SessionID            string         `json:"sessionId"`
	StartedAt            time.Time      `json:"startedAt"`
	RepoRoot             string         `json:"repoRoot"`
	ConfigPath           string         `json:"configPath"`
	StarterConfigCreated bool           `json:"starterConfigCreated"`
	Build                buildinfo.Info `json:"build"`
}

type panelClusterState struct {
	Items      []clusterView `json:"items"`
	Refreshing bool          `json:"refreshing"`
	UpdatedAt  *time.Time    `json:"updatedAt,omitempty"`
}

type panelAWSInventoryState struct {
	Refreshing bool              `json:"refreshing"`
	UpdatedAt  time.Time         `json:"updatedAt,omitzero"`
	Region     string            `json:"region"`
	Owner      string            `json:"owner,omitempty"`
	Queries    []string          `json:"queries"`
	Items      []awsResourceView `json:"items"`
	Error      string            `json:"error,omitempty"`
}

type awsResourceView struct {
	Type            string            `json:"type"`
	ID              string            `json:"id"`
	Name            string            `json:"name,omitempty"`
	Region          string            `json:"region,omitempty"`
	Status          string            `json:"status,omitempty"`
	RunID           string            `json:"runId,omitempty"`
	Owner           string            `json:"owner,omitempty"`
	Source          string            `json:"source"`
	Details         string            `json:"details,omitempty"`
	Tags            map[string]string `json:"tags,omitempty"`
	CleanupEligible bool              `json:"cleanupEligible"`
	CleanupReason   string            `json:"cleanupReason,omitempty"`
}

type clusterView struct {
	ID                    string    `json:"id"`
	RunID                 string    `json:"runId,omitempty"`
	Type                  string    `json:"type"`
	DeploymentType        string    `json:"deploymentType,omitempty"`
	Role                  string    `json:"role,omitempty"`
	HAIndex               int       `json:"haIndex"`
	Name                  string    `json:"name"`
	Version               string    `json:"version,omitempty"`
	RancherURL            string    `json:"rancherUrl,omitempty"`
	LoadBalancer          string    `json:"loadBalancer,omitempty"`
	GPUWorkerIP           string    `json:"gpuWorkerIp,omitempty"`
	GPUWorkerPrivateIP    string    `json:"gpuWorkerPrivateIp,omitempty"`
	GPUWorkerInstanceType string    `json:"gpuWorkerInstanceType,omitempty"`
	GPUWorkerAMI          string    `json:"gpuWorkerAmi,omitempty"`
	GPUWorkerSubnetID     string    `json:"gpuWorkerSubnetId,omitempty"`
	Namespace             string    `json:"namespace,omitempty"`
	ManagementClusterID   string    `json:"managementClusterId,omitempty"`
	KubeconfigPath        string    `json:"kubeconfigPath,omitempty"`
	DownloadName          string    `json:"downloadName,omitempty"`
	Provisioning          bool      `json:"provisioning,omitempty"`
	ProvisioningMessage   string    `json:"provisioningMessage,omitempty"`
	Available             bool      `json:"available"`
	Reachable             bool      `json:"reachable"`
	Error                 string    `json:"error,omitempty"`
	Pods                  []podView `json:"pods"`
}

type podView struct {
	Namespace   string               `json:"namespace,omitempty"`
	Name        string               `json:"name"`
	Ready       string               `json:"ready"`
	Status      string               `json:"status"`
	Restarts    int                  `json:"restarts"`
	Age         string               `json:"age"`
	Node        string               `json:"node,omitempty"`
	Containers  string               `json:"containers"`
	Images      []containerImageView `json:"images,omitempty"`
	Leader      bool                 `json:"leader"`
	LeaderLabel string               `json:"leaderLabel,omitempty"`
}

// containerImageView keeps both the image requested by the workload and the
// immutable runtime image ID reported by Kubernetes. The control panel uses
// the latter to inspect the exact artifact that is running even when the
// declared tag is mutable.
type containerImageView struct {
	Name    string `json:"name"`
	Image   string `json:"image"`
	ImageID string `json:"imageId,omitempty"`
	Ready   bool   `json:"ready"`
}

type panelOperationSnapshot struct {
	Running    bool       `json:"running"`
	PID        int        `json:"pid,omitempty"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	Error      string     `json:"error,omitempty"`
	Warning    string     `json:"warning,omitempty"`
	Output     []string   `json:"output"`
	RunID      string     `json:"runId,omitempty"`
	Command    string     `json:"command,omitempty"`
	UpdatedAt  *time.Time `json:"updatedAt,omitempty"`
}

type panelOperationName string

const (
	panelOperationSetup         panelOperationName = "setup"
	panelOperationReadiness     panelOperationName = "readiness"
	panelOperationDownstream    panelOperationName = "downstream"
	panelOperationCleanup       panelOperationName = "cleanup"
	panelOperationLinodeSetup   panelOperationName = "linodeSetup"
	panelOperationLinodeCleanup panelOperationName = "linodeCleanup"
	panelOperationCleanupBatch  panelOperationName = "cleanupBatch"
	panelOperationSteveLab      panelOperationName = "steveLab"
	panelOperationK3DLab        panelOperationName = "k3dLab"
	panelOperationAWSCleanup    panelOperationName = "awsCleanup"
)

type panelOperationState struct {
	// InProcess marks an orchestration goroutine owned by this panel instance.
	// Its child PID can exit between steps without ending the operation.
	InProcess bool `json:"-"`

	Running    bool
	PID        int
	StartedAt  *time.Time
	FinishedAt *time.Time
	Error      string
	Warning    string
	Output     []string
	RunID      string
	Command    string
	UpdatedAt  *time.Time

	// Batch-only fields live on the dedicated cleanupBatch operation so they
	// are persisted alongside the other lifecycle state.
	RunIDs          []string
	CompletedRunIDs []string
	Failures        []panelCleanupBatchFailure
	CancelRequested bool
}

type panelCommandSpec struct {
	Operation      panelOperationName
	DisplayName    string
	TestName       string
	Timeout        string
	RunID          string
	StartLine      string
	SuccessLine    string
	AfterSuccess   func()
	AllowWhileDone bool
	BatchChild     bool
	Completion     chan<- error
	LabCleanup     panelLabCleanupOptions
}

type kubectlPodList struct {
	Items []kubectlPod `json:"items"`
}

type kubectlPod struct {
	Metadata struct {
		Namespace         string    `json:"namespace"`
		Name              string    `json:"name"`
		CreationTimestamp time.Time `json:"creationTimestamp"`
	} `json:"metadata"`
	Spec struct {
		NodeName   string `json:"nodeName"`
		Containers []struct {
			Name  string `json:"name"`
			Image string `json:"image"`
		} `json:"containers"`
		InitContainers []struct {
			Name string `json:"name"`
		} `json:"initContainers"`
	} `json:"spec"`
	Status struct {
		Phase             string `json:"phase"`
		Reason            string `json:"reason"`
		ContainerStatuses []struct {
			Name         string `json:"name"`
			Image        string `json:"image"`
			ImageID      string `json:"imageID"`
			Ready        bool   `json:"ready"`
			RestartCount int    `json:"restartCount"`
			State        struct {
				Waiting struct {
					Reason string `json:"reason"`
				} `json:"waiting"`
				Terminated struct {
					Reason string `json:"reason"`
				} `json:"terminated"`
			} `json:"state"`
		} `json:"containerStatuses"`
		InitContainerStatuses []struct {
			Name         string `json:"name"`
			Ready        bool   `json:"ready"`
			RestartCount int    `json:"restartCount"`
			State        struct {
				Waiting struct {
					Reason string `json:"reason"`
				} `json:"waiting"`
				Terminated struct {
					Reason string `json:"reason"`
				} `json:"terminated"`
			} `json:"state"`
		} `json:"initContainerStatuses"`
	} `json:"status"`
}

type provisioningClusterList struct {
	Items []provisioningClusterItem `json:"items"`
}

type provisioningClusterItem struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Status struct {
		ClusterName string `json:"clusterName"`
	} `json:"status"`
}

type managementClusterList struct {
	Items []managementClusterItem `json:"items"`
}

type managementClusterItem struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		DisplayName string `json:"displayName"`
	} `json:"spec"`
}

type discoveredDownstreamCluster struct {
	Name                string
	Namespace           string
	ManagementClusterID string
}
