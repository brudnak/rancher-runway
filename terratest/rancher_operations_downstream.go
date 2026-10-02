package test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (p *localControlPanel) startDeployedDownstream(ctx context.Context, cluster clusterView, in rancherOperationRequest) (any, error) {
	if !in.Confirmed {
		return nil, fmt.Errorf("review and confirm creation of billable cloud resources")
	}
	if !deployedResourceName.MatchString(in.Name) {
		return nil, fmt.Errorf("use a 2–40 character lowercase cluster name starting with a letter and ending in a letter or digit")
	}
	if in.Quantity != 1 && in.Quantity != 3 && in.Quantity != 5 {
		return nil, fmt.Errorf("choose one, three, or five all-role nodes")
	}
	catalog, err := deployedDownstreamOptions(ctx, cluster, in)
	if err != nil {
		return nil, err
	}
	found := false
	for _, v := range catalog.Versions {
		if v == in.KubernetesVersion {
			found = true
		}
	}
	if !found {
		return nil, fmt.Errorf("select a Kubernetes version from this Rancher's current release list")
	}
	if err = validateDeployedMachine(in.Machine, catalog.Fields); err != nil {
		return nil, err
	}
	if in.UseEnvironment {
		if in.CredentialID != "" {
			return nil, fmt.Errorf("choose either an existing Rancher credential or Runway environment credentials")
		}
		in.Credentials, err = deployedEnvironmentCredentials(in.Provider)
		if err != nil {
			return nil, err
		}
	}
	if in.CredentialID != "" {
		found = false
		for _, cred := range catalog.Credentials {
			if cred["id"] == in.CredentialID {
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("select a cloud credential belonging to this provider on this Rancher")
		}
	} else {
		required := []string{"token"}
		if in.Provider == "amazonec2" {
			required = []string{"accessKey", "secretKey"}
		}
		for _, key := range required {
			if strings.TrimSpace(in.Credentials[key]) == "" {
				return nil, fmt.Errorf("enter the provider's %s credential", key)
			}
		}
		for key := range in.Credentials {
			valid := false
			for _, allowed := range required {
				if key == allowed {
					valid = true
				}
			}
			if !valid {
				return nil, fmt.Errorf("unknown credential field")
			}
		}
	}
	id := operationID()
	if err = p.reserveRancherOperation(cluster.ID, id); err != nil {
		return nil, err
	}
	record := &rancherOperationRecord{ID: id, ClusterID: cluster.ID, Kind: "downstream", Status: "running", Started: time.Now().UTC(), To: in.Name + " / " + in.Provider + " / " + in.KubernetesVersion, Events: []rancherOperationEvent{}}
	if err = p.operationEvent(record, fmt.Sprintf("Creation approved: %d all-role nodes, live Rancher schema and Kubernetes release validated. Namespace: fleet-default.", in.Quantity)); err != nil {
		p.releaseRancherOperation(cluster.ID)
		return nil, err
	}
	if !p.workers.Start(func(context.Context) {
		p.finishRancherOperation(record, p.executeDeployedDownstream(cluster, in, record))
	}) {
		p.finishRancherOperation(record, fmt.Errorf("Runway shut down before the operation started"))
		return nil, fmt.Errorf("Runway is shutting down")
	}
	return map[string]string{"id": record.ID, "status": "running"}, nil
}

func (p *localControlPanel) executeDeployedDownstream(cluster clusterView, in rancherOperationRequest, record *rancherOperationRecord) error {
	return p.runDeployedDownstream(cluster, in, record, downstreamProvisioningRuntime{inspectRancherImageReference, operationCommand})
}

func (p *localControlPanel) runDeployedDownstream(cluster clusterView, in rancherOperationRequest, record *rancherOperationRecord, runtime downstreamProvisioningRuntime) error {
	ctx, cancel := context.WithTimeout(p.workers.Context(), 45*time.Minute)
	defer cancel()
	client, err := deployedRancherClient(in)
	if err != nil {
		return err
	}
	defer client.CloseIdleConnections()
	call := func(method, path string, payload, out any) error {
		return deployedRancherJSON(ctx, client, cluster.RancherURL, in.Token, method, path, payload, out)
	}
	// Fail before creating provider credentials or machine configs if the requested cluster exists.
	existingErr := call(http.MethodGet, "/v1/provisioning.cattle.io.cluster/fleet-default/"+url.PathEscape(in.Name), nil, nil)
	if existingErr == nil {
		return fmt.Errorf("cluster fleet-default/%s already exists; inspect that cluster instead of retrying creation", in.Name)
	}
	statusErr, ok := existingErr.(*deployedRancherHTTPError)
	if !ok || statusErr.status != http.StatusNotFound {
		return fmt.Errorf("cannot verify whether cluster already exists: %w", existingErr)
	}
	if err = p.operationEvent(record, "Checking Rancher's machine provisioner image before creating resources."); err != nil {
		return err
	}
	verified, err := checkDownstreamProvisioner(ctx, call, runtime.inspect)
	if err != nil {
		return err
	}
	if err = p.operationEvent(record, verified); err != nil {
		return err
	}
	// Unique names and POST (never PUT/apply) prevent overwriting existing resources.
	machineName := in.Name + "-" + record.ID[:8]
	checkpoint := func(resource string) error {
		record.Resources = append(record.Resources, resource)
		return p.operationEvent(record, "Created "+resource)
	}
	if in.CredentialID == "" {
		if err = p.operationEvent(record, "Creating Rancher cloud credential runway-"+machineName); err != nil {
			return err
		}
		var created struct {
			ID string `json:"id"`
		}
		if err = call(http.MethodPost, "/v3/cloudcredentials", map[string]any{"type": "cloudCredential", "name": "runway-" + machineName, in.Provider + "credentialConfig": in.Credentials}, &created); err != nil {
			return err
		}
		if created.ID == "" {
			return fmt.Errorf("Rancher did not return the created credential ID; inspect cloud credentials before retrying")
		}
		in.CredentialID = created.ID
		if err = checkpoint("cloud credential " + created.ID); err != nil {
			return err
		}
	}
	var created struct {
		ID string `json:"id"`
	}
	if err = p.operationEvent(record, "Creating machine configuration fleet-default/"+machineName); err != nil {
		return err
	}
	if err = call(http.MethodPost, "/v1/rke-machine-config.cattle.io."+in.Provider+"configs", downstreamMachinePayload(in, machineName), &created); err != nil {
		return fmt.Errorf("machine configuration creation failed; retained resources are listed in history: %w", err)
	}
	if err = checkpoint("machine configuration fleet-default/" + machineName); err != nil {
		return err
	}
	if err = p.operationEvent(record, "Creating provisioning cluster fleet-default/"+in.Name); err != nil {
		return err
	}
	ownership := deployedDownstreamRecord{OperationID: record.ID, RunID: cluster.RunID, ParentID: cluster.ID, Kubeconfig: cluster.KubeconfigPath, Namespace: "fleet-default", Name: in.Name, Provider: in.Provider, MachineConfig: machineName, CreatedAt: time.Now().UTC()}
	if err = saveDeployedDownstream(ownership); err != nil {
		return fmt.Errorf("cannot preserve downstream cleanup ownership; cluster was not created: %w", err)
	}
	payload := downstreamClusterPayload(in, in.CredentialID, machineName)
	payload["metadata"].(map[string]any)["annotations"] = map[string]string{deployedOwnerAnnotation: record.ID}
	if err = call(http.MethodPost, "/v1/provisioning.cattle.io.cluster", payload, &created); err != nil {
		return fmt.Errorf("cluster creation failed; retained resources are listed in history: %w", err)
	}
	if err = checkpoint("provisioning cluster fleet-default/" + in.Name); err != nil {
		return err
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	last := ""
	lastPods := ""
	lastBlocker := ""
	blockerCount := 0
	for {
		var state struct {
			Status struct {
				ClusterName string `json:"clusterName"`
				Conditions  []struct {
					Type   string `json:"type"`
					Status string `json:"status"`
					Reason string `json:"reason"`
				} `json:"conditions"`
			} `json:"status"`
		}
		err = call(http.MethodGet, "/v1/provisioning.cattle.io.cluster/fleet-default/"+url.PathEscape(in.Name), nil, &state)
		if err != nil {
			if e := p.operationEvent(record, "Waiting for Rancher status: "+err.Error()); e != nil {
				return e
			}
		} else {
			// Log condition types/status only: provider error messages may echo secrets.
			statuses := []string{}
			ready := false
			for _, condition := range state.Status.Conditions {
				statuses = append(statuses, condition.Type+"="+condition.Status)
				if condition.Type == "Ready" && condition.Status == "True" {
					ready = true
				}
			}
			message := strings.Join(statuses, ", ")
			if message != last {
				if e := p.operationEvent(record, "Cluster conditions: "+message); e != nil {
					return e
				}
				last = message
			}
			if ready && state.Status.ClusterName != "" {
				return p.operationEvent(record, "Rancher reports Ready=True; management cluster ID: "+state.Status.ClusterName)
			}
		}
		diagnostic, diagnosticErr := observeDownstreamProvisioning(ctx, cluster.KubeconfigPath, in.Name, runtime.command)
		if diagnosticErr != nil {
			if diagnosticErr.Error() == lastBlocker {
				blockerCount++
			} else {
				lastBlocker = diagnosticErr.Error()
				blockerCount = 1
			}
			if blockerCount >= 3 {
				return diagnosticErr
			}
			diagnostic = "Checking persistent provisioning problem: " + diagnosticErr.Error()
		} else {
			lastBlocker = ""
			blockerCount = 0
		}
		if diagnostic != lastPods {
			if err = p.operationEvent(record, diagnostic); err != nil {
				return err
			}
			lastPods = diagnostic
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("provisioning readiness timed out; cloud resources are retained. Inspect the recorded cluster in Rancher before retrying")
		case <-ticker.C:
		}
	}
}

// Reuse the exact credential sources already used by Runway's provider clients.
// Never return these maps to the browser or attach them to an operation record.
func deployedEnvironmentCredentials(provider string) (map[string]string, error) {
	switch provider {
	case "linode":
		token := linodeAccessToken()
		if token == "" {
			return nil, fmt.Errorf("No Linode token found in LINODE_TOKEN, LINODE_ACCESS_TOKEN, or Runway's linode.access_token setting")
		}
		return map[string]string{"token": token}, nil
	case "amazonec2":
		if getenvFallback("AWS_SESSION_TOKEN") != "" {
			return nil, fmt.Errorf("Runway has temporary AWS session credentials; this Rancher cloud-credential workflow requires an access key and secret without a session token")
		}
		key := configuredAWSValue("aws.access_key_id", "AWS_ACCESS_KEY_ID")
		secret := getenvFallback("AWS_SECRET_ACCESS_KEY")
		if key == "" || secret == "" {
			return nil, fmt.Errorf("No complete AWS credential pair found in Runway's AWS access key setting / AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY")
		}
		return map[string]string{"accessKey": key, "secretKey": secret}, nil
	default:
		return nil, fmt.Errorf("choose Linode or AWS")
	}
}
