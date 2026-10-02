package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

type downstreamRancherCall func(method, path string, payload, out any) error

type downstreamProvisioningRuntime struct {
	inspect rancherImageInspectFunc
	command rancherUpgradeCommand
}

// Read the running server's settings rather than guessing system image versions
// from the chart or the Rancher tag. This check runs before any resource POST.
func checkDownstreamProvisioner(ctx context.Context, call downstreamRancherCall, inspect rancherImageInspectFunc) (string, error) {
	read := func(name string) (string, error) {
		var setting struct {
			Value   *string `json:"value"`
			Default string  `json:"default"`
		}
		if err := call(http.MethodGet, "/v3/settings/"+name, nil, &setting); err != nil {
			return "", fmt.Errorf("cannot read Rancher's %s setting; no resources were created: %w", name, err)
		}
		if setting.Value != nil {
			return *setting.Value, nil
		}
		return setting.Default, nil
	}
	image, err := read("machine-provision-image")
	if err != nil {
		return "", err
	}
	registry, err := read("system-default-registry")
	if err != nil {
		return "", err
	}
	if image == "" {
		return "", fmt.Errorf("Rancher did not declare a machine provisioner image; no resources were created")
	}
	// Rancher's PrefixPrivateRegistry prepends the setting even to qualified names.
	reference := image
	if registry != "" {
		reference = strings.TrimRight(registry, "/") + "/" + image
	}
	checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	provenance, found, err := inspect(checkCtx, reference)
	if err != nil {
		return "", fmt.Errorf("cannot verify Rancher's machine provisioner image %s; check registry access and credentials before retrying. No resources were created", reference)
	}
	if !found || provenance.Digest == "" {
		return "", fmt.Errorf("Rancher's machine provisioner image %s is unavailable. Check system-default-registry against this Rancher build's image source (especially after a stable/head upgrade). No resources were created", reference)
	}
	return "Machine provisioner image verified: " + reference + " @ " + provenance.Digest, nil
}

// Correlate via CAPI machine labels, never by a cluster-name prefix. Only known
// status codes and resource identifiers are retained; raw provider messages or
// container logs can contain credentials and are deliberately excluded.
func observeDownstreamProvisioning(ctx context.Context, kubeconfig, clusterName string, command rancherUpgradeCommand) (string, error) {
	if kubeconfig == "" {
		return "Provisioning pod diagnostics unavailable: management kubeconfig is not configured; cluster readiness is still monitored", nil
	}
	query := func(resource string, selector string, out any) error {
		raw, err := command(ctx, nil, "kubectl", "--kubeconfig", kubeconfig, "--request-timeout=10s", "-n", "fleet-default", "get", resource, "-l", selector, "-o", "json")
		if err != nil {
			return err
		}
		return json.Unmarshal(raw, out)
	}
	var machines struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
		} `json:"items"`
	}
	if err := query("machines.cluster.x-k8s.io", "cluster.x-k8s.io/cluster-name="+clusterName, &machines); err != nil {
		return "Provisioning machine diagnostics unavailable; cluster readiness is still monitored", nil
	}
	if len(machines.Items) == 0 {
		return "Waiting for Rancher to create provisioning machines", nil
	}
	names := map[string]bool{}
	for _, machine := range machines.Items {
		names[machine.Metadata.Name] = true
	}
	type containerStatus struct {
		Name  string `json:"name"`
		State struct {
			Waiting *struct {
				Reason string `json:"reason"`
			} `json:"waiting"`
			Terminated *struct {
				ExitCode int `json:"exitCode"`
			} `json:"terminated"`
		} `json:"state"`
	}
	var pods struct {
		Items []struct {
			Metadata struct {
				Name     string            `json:"name"`
				Labels   map[string]string `json:"labels"`
				Created  string            `json:"creationTimestamp"`
				Deleting *string           `json:"deletionTimestamp"`
			} `json:"metadata"`
			Status struct {
				Phase      string            `json:"phase"`
				Containers []containerStatus `json:"containerStatuses"`
				Init       []containerStatus `json:"initContainerStatuses"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := query("pods", "rke.cattle.io/capi-machine-name,rke.cattle.io/infra-remove=false", &pods); err != nil {
		return "Provisioning pod diagnostics unavailable; cluster readiness is still monitored", nil
	}
	// Job retries may leave failed pods behind. Diagnose only the newest live
	// attempt for each machine, so a successful retry is not marked failed.
	latest := map[string]int{}
	for i, pod := range pods.Items {
		machine := pod.Metadata.Labels["rke.cattle.io/capi-machine-name"]
		if !names[machine] || pod.Metadata.Deleting != nil {
			continue
		}
		previous, exists := latest[machine]
		if !exists || pod.Metadata.Created >= pods.Items[previous].Metadata.Created {
			latest[machine] = i
		}
	}
	statuses := []string{}
	for i, pod := range pods.Items {
		machine := pod.Metadata.Labels["rke.cattle.io/capi-machine-name"]
		selected, exists := latest[machine]
		if !exists || selected != i {
			continue
		}
		if !names[pod.Metadata.Labels["rke.cattle.io/capi-machine-name"]] {
			continue
		}
		for _, container := range append(pod.Status.Init, pod.Status.Containers...) {
			if wait := container.State.Waiting; wait != nil {
				switch wait.Reason {
				case "ImagePullBackOff", "ErrImagePull", "InvalidImageName":
					return "", fmt.Errorf("provisioning blocked: pod fleet-default/%s has %s. Rancher cannot start its machine provisioner; check the image and system-default-registry. Resources are retained and Rancher may continue retrying. Inspect this existing cluster before retrying creation", pod.Metadata.Name, wait.Reason)
				case "CrashLoopBackOff", "CreateContainerConfigError", "CreateContainerError":
					return "", fmt.Errorf("provisioning blocked: pod fleet-default/%s has %s. Inspect this pod in Rancher. Resources are retained; do not create a duplicate cluster", pod.Metadata.Name, wait.Reason)
				}
			}
			if ended := container.State.Terminated; ended != nil && ended.ExitCode != 0 {
				return "", fmt.Errorf("provisioning job failed: pod fleet-default/%s exited with code %d. Inspect its logs in Rancher for the provider error. Resources are retained; do not create a duplicate cluster", pod.Metadata.Name, ended.ExitCode)
			}
		}
		switch pod.Status.Phase {
		case "Pending", "Running", "Succeeded", "Failed", "Unknown":
			statuses = append(statuses, pod.Metadata.Name+"="+pod.Status.Phase)
		}
	}
	if len(statuses) == 0 {
		return fmt.Sprintf("%d provisioning machine(s); waiting for provisioner pods", len(names)), nil
	}
	sort.Strings(statuses)
	return "Provisioner pods: " + strings.Join(statuses, ", "), nil
}
