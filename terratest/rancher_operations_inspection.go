package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	version "github.com/hashicorp/go-version"
	"os/exec"
	"time"
)

func operationCommand(ctx context.Context, input []byte, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = 2 * time.Second
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	// stderr can contain rendered secrets (Helm and admission webhooks). Never persist it.
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s command failed (%v); inspect the cluster and tool configuration", name, err)
	}
	return stdout.Bytes(), nil
}

func inspectInstalledRancher(ctx context.Context, kubeconfig string) (rancherInstalledState, error) {
	var result rancherInstalledState
	if kubeconfig == "" {
		return result, fmt.Errorf("a management kubeconfig is required for Helm upgrades; Docker installs are not supported by this workflow")
	}
	raw, err := operationCommand(ctx, nil, "helm", "--kubeconfig", kubeconfig, "list", "-n", "cattle-system", "--filter", "^rancher$", "-o", "json")
	if err != nil {
		return result, err
	}
	var releases []struct {
		Chart      string `json:"chart"`
		AppVersion string `json:"app_version"`
		Revision   string `json:"revision"`
		Status     string `json:"status"`
	}
	if json.Unmarshal(raw, &releases) != nil || len(releases) != 1 || releases[0].Status != "deployed" {
		return result, fmt.Errorf("Rancher must have one deployed Helm release named rancher in cattle-system")
	}
	result.Version = releases[0].AppVersion
	result.Chart = releases[0].Chart
	fmt.Sscanf(releases[0].Revision, "%d", &result.Revision)
	raw, err = operationCommand(ctx, nil, "kubectl", "--kubeconfig", kubeconfig, "get", "deployment", "rancher", "-n", "cattle-system", "-o", "json")
	if err != nil {
		return result, err
	}
	var dep struct {
		Metadata struct{ Generation int64 }
		Spec     struct {
			Replicas int
			Template struct {
				Spec struct {
					Containers []struct {
						Name  string
						Image string
					}
				}
			}
		}
		Status struct {
			ObservedGeneration int64
			ReadyReplicas      int
			UpdatedReplicas    int
			AvailableReplicas  int
		}
	}
	if err = json.Unmarshal(raw, &dep); err != nil {
		return result, err
	}
	if dep.Spec.Replicas < 1 || dep.Status.ObservedGeneration < dep.Metadata.Generation || dep.Status.ReadyReplicas != dep.Spec.Replicas || dep.Status.UpdatedReplicas != dep.Spec.Replicas || dep.Status.AvailableReplicas != dep.Spec.Replicas {
		return result, fmt.Errorf("Rancher deployment is not fully ready; finish or repair its current rollout first")
	}
	for _, container := range dep.Spec.Template.Spec.Containers {
		if container.Name == "rancher" {
			result.Image = container.Image
		}
	}
	if result.Image == "" {
		return result, fmt.Errorf("cannot identify the installed Rancher container")
	}
	// Chart appVersion can differ from an explicitly overridden server image.
	raw, err = operationCommand(ctx, nil, "kubectl", "--kubeconfig", kubeconfig, "get", "settings.management.cattle.io", "server-version", "-o", "json")
	if err != nil {
		return result, err
	}
	result.Version, err = parseRancherServerVersionSettingJSON(raw)
	if err != nil {
		return result, err
	}
	result.Pods, err = fetchLocalRancherPods(kubeconfig)
	return result, err
}

func validateUpgradeStep(from, to string, experimental bool) error {
	if minorHeadVersion.MatchString(from) || minorHeadVersion.MatchString(to) {
		return validateMinorHeadUpgradeStep(from, to, experimental)
	}
	a, err := version.NewVersion(from)
	if err != nil {
		return fmt.Errorf("cannot classify running Rancher version %q", from)
	}
	b, err := version.NewVersion(to)
	if err != nil {
		return fmt.Errorf("cannot classify target Rancher version %q", to)
	}
	as, bs := a.Segments(), b.Segments()
	if as[0] != bs[0] || bs[1] < as[1] || bs[1] > as[1]+1 {
		return fmt.Errorf("choose the current or next minor release; upgrade one minor at a time")
	}
	if b.Core().LessThan(a.Core()) || (!experimental && !b.GreaterThan(a)) {
		return fmt.Errorf("downgrades and unchanged stable versions are not upgrade targets")
	}
	if !experimental && (a.Prerelease() != "" || b.Prerelease() != "") {
		return fmt.Errorf("prerelease transitions require experimental mode")
	}
	return nil
}
