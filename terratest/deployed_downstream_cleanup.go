package test

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"time"
)

func cleanupDeployedDownstreamsForRun(ctx context.Context, runID string, command rancherUpgradeCommand) error {
	records, err := readDeployedDownstreams(runID)
	if err != nil {
		return err
	}
	if err = checkUntrackedDeployedDownstreams(runID); err != nil {
		return err
	}
	for _, record := range records {
		log.Printf("[cleanup] Deleting Rancher-created downstream %s/%s before management destroy", record.Namespace, record.Name)
		if err = deleteDeployedDownstream(ctx, record, command); err != nil {
			return fmt.Errorf("downstream %s: %w; management infrastructure retained", record.Name, err)
		}
		now := time.Now().UTC()
		record.DeletedAt = &now
		if err = saveDeployedDownstream(record); err != nil {
			return fmt.Errorf("save downstream cleanup result: %w", err)
		}
	}
	return nil
}

// Earlier versions retained operation logs but not deletion ownership. Do not
// silently destroy their parent or adopt another cluster with a reused name.
func checkUntrackedDeployedDownstreams(runID string) error {
	all, err := os.ReadDir(durableDataPath("rancher-operations"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	tracked := map[string]bool{}
	// Include completed cleanup records as well.
	entries, _ := os.ReadDir(durableDataPath("deployed-downstreams"))
	for _, entry := range entries {
		raw, e := os.ReadFile(durableDataPath("deployed-downstreams") + "/" + entry.Name())
		var r deployedDownstreamRecord
		if e == nil && json.Unmarshal(raw, &r) == nil && r.RunID == runID {
			tracked[r.OperationID] = true
		}
	}
	for _, entry := range all {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(durableDataPath("rancher-operations") + "/" + entry.Name())
		if err != nil {
			return err
		}
		var record rancherOperationRecord
		if json.Unmarshal(raw, &record) != nil {
			return fmt.Errorf("cannot verify prior downstream operations; management destroy stopped")
		}
		if record.Kind != "downstream" || !strings.HasPrefix(record.ClusterID, "run-"+runID+"-") || tracked[record.ID] {
			continue
		}
		for _, resource := range record.Resources {
			if strings.HasPrefix(resource, "provisioning cluster ") {
				return fmt.Errorf("%s was created before automatic Destroy tracking; verify and enroll its exact cluster UID before destroying management", resource)
			}
		}
	}
	return nil
}

func deleteDeployedDownstream(ctx context.Context, record deployedDownstreamRecord, command rancherUpgradeCommand) error {
	if record.Kubeconfig == "" {
		return fmt.Errorf("management kubeconfig unavailable")
	}
	call := func(input []byte, args ...string) ([]byte, error) {
		return command(ctx, input, "kubectl", append([]string{"--kubeconfig", record.Kubeconfig, "--request-timeout=20s"}, args...)...)
	}
	read := func() (string, error) {
		raw, err := call(nil, "get", "clusters.provisioning.cattle.io", record.Name, "-n", record.Namespace, "--ignore-not-found", "-o", "json")
		if err != nil {
			return "", fmt.Errorf("cannot verify downstream through management API")
		}
		if len(strings.TrimSpace(string(raw))) == 0 {
			return "", nil
		}
		var obj struct {
			Metadata struct {
				UID         string            `json:"uid"`
				Annotations map[string]string `json:"annotations"`
			} `json:"metadata"`
		}
		if json.Unmarshal(raw, &obj) != nil || obj.Metadata.UID == "" {
			return "", fmt.Errorf("downstream identity unavailable")
		}
		if record.UID != "" {
			if record.UID != obj.Metadata.UID {
				return "", fmt.Errorf("cluster UID changed; refusing to delete a replacement cluster")
			}
		} else if obj.Metadata.Annotations[deployedOwnerAnnotation] != record.OperationID {
			return "", fmt.Errorf("cluster ownership does not match this creation operation")
		}
		return obj.Metadata.UID, nil
	}
	uid, err := read()
	if err != nil {
		return err
	}
	if uid != "" {
		options, _ := json.Marshal(map[string]any{"apiVersion": "v1", "kind": "DeleteOptions", "propagationPolicy": "Foreground", "preconditions": map[string]string{"uid": uid}})
		path := "/apis/provisioning.cattle.io/v1/namespaces/" + url.PathEscape(record.Namespace) + "/clusters/" + url.PathEscape(record.Name)
		if _, err = call(options, "delete", "--raw", path, "-f", "-"); err != nil {
			return fmt.Errorf("Rancher rejected downstream deletion; retry after checking its status")
		}
	}
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		remaining, err := read()
		if err != nil {
			return err
		}
		raw, err := call(nil, "get", "machines.cluster.x-k8s.io", "-n", record.Namespace, "-l", "cluster.x-k8s.io/cluster-name="+record.Name, "-o", "json")
		if err != nil {
			return fmt.Errorf("cannot verify downstream machine cleanup")
		}
		var machines struct {
			Items []json.RawMessage `json:"items"`
		}
		if json.Unmarshal(raw, &machines) != nil || machines.Items == nil {
			return fmt.Errorf("invalid downstream machine cleanup response")
		}
		if remaining == "" && len(machines.Items) == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("downstream deletion did not complete; inspect Rancher finalizers and remaining machines before retrying")
		case <-ticker.C:
		}
	}
}
