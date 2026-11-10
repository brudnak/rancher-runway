package test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

const machineProvisionerWorkaroundEnv = "RANCHER_RUNWAY_MACHINE_PROVISIONER_WORKAROUND"

// Image substitution is an explicit escape hatch for testing other Rancher
// features; default runs must exercise the provisioner shipped with the build.
func prepareDownstreamProvisionerCompatibility(ctx context.Context, call downstreamRancherCall, inspect rancherImageInspectFunc) (string, error) {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv(machineProvisionerWorkaroundEnv)), "true") {
		return "", nil
	}
	return ensureDownstreamProvisionerCompatibility(ctx, call, inspect)
}

// These exact machine builds switched to bci-nano without the sed/grep used by
// download_driver.sh. Keep the workaround tied to known bad images, not Rancher
// version ranges, so fixed builds and custom provisioners retain their settings.
// The previous stable tags use bci-busybox and stay within the same release line:
// https://github.com/rancher/machine/blob/v0.15.0-rancher145.3/package/Dockerfile
// https://github.com/rancher/machine/blob/v0.15.0-rancher142.7/package/Dockerfile
func compatibleDownstreamProvisioner(image string) string {
	repository, tag, ok := strings.Cut(image, ":")
	if !ok {
		return image
	}
	switch repository {
	case "rancher/machine", "docker.io/rancher/machine", "registry.rancher.com/rancher/machine", "stgregistry.suse.com/rancher/machine":
	default:
		return image
	}
	switch tag {
	case "v0.15.0-rancher145.4-rc.1":
		return repository + ":v0.15.0-rancher145.3"
	case "v0.15.0-rancher142.8-rc.1":
		return repository + ":v0.15.0-rancher142.7"
	default:
		return image
	}
}

func readDownstreamRancherSetting(call downstreamRancherCall, name string) (string, error) {
	var setting struct {
		Value   string `json:"value"`
		Default string `json:"default"`
	}
	if err := call(http.MethodGet, "/v3/settings/"+name, nil, &setting); err != nil {
		return "", fmt.Errorf("cannot read Rancher's %s setting: %w", name, err)
	}
	if setting.Value != "" {
		return setting.Value, nil
	}
	return setting.Default, nil
}

// Called only for an authorized downstream creation, before provider resources
// are created. This changes Rancher's server-wide setting for future machine
// jobs; it must not run during read-only catalog or preflight requests.
func ensureDownstreamProvisionerCompatibility(ctx context.Context, call downstreamRancherCall, inspect rancherImageInspectFunc) (string, error) {
	image, err := readDownstreamRancherSetting(call, "machine-provision-image")
	if err != nil {
		return "", err
	}
	replacement := compatibleDownstreamProvisioner(image)
	if replacement == image {
		return "", nil
	}
	registry, err := readDownstreamRancherSetting(call, "system-default-registry")
	if err != nil {
		return "", err
	}
	// Match Rancher's PrefixPrivateRegistry, including for qualified image names.
	reference := replacement
	if registry != "" {
		reference = strings.TrimRight(registry, "/") + "/" + replacement
	}
	checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	provenance, found, err := inspect(checkCtx, reference)
	if err != nil || !found || provenance.Digest == "" {
		// Registry errors can contain credentials; retain only the image reference.
		return "", fmt.Errorf("machine provisioner %s lacks sed/grep; cannot verify compatibility image %s. The setting is unchanged and no downstream resources were created", image, reference)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := call(http.MethodPut, "/v3/settings/machine-provision-image", map[string]string{"value": replacement}, nil); err != nil {
		return "", fmt.Errorf("could not apply the machine provisioner compatibility setting; no downstream resources were created: %w", err)
	}
	// HA replicas may briefly serve an older settings cache after the update.
	waitCtx, stop := context.WithTimeout(ctx, 15*time.Second)
	defer stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		current, err := readDownstreamRancherSetting(call, "machine-provision-image")
		if err != nil {
			return "", fmt.Errorf("machine provisioner setting was submitted but could not be confirmed; no downstream resources were created: %w", err)
		}
		if current == replacement {
			return fmt.Sprintf("Runway compatibility workaround: machine-provision-image %s -> %s (verified %s @ %s) because the original image lacks sed/grep. This server-wide setting applies to future machine jobs; Rancher version and system-default-registry are unchanged.", image, replacement, reference, provenance.Digest), nil
		}
		select {
		case <-waitCtx.Done():
			return "", fmt.Errorf("machine provisioner setting was submitted but Rancher did not confirm %s; no downstream resources were created: %w", replacement, waitCtx.Err())
		case <-ticker.C:
		}
	}
}

func prepareLinodeDownstreamProvisioner(ctx context.Context, rancherURL, token string) (string, error) {
	rancherURL = strings.TrimRight(clickableURL(rancherURL), "/")
	client, err := deployedRancherClient(rancherOperationRequest{Token: token, Insecure: true})
	if err != nil {
		return "", err
	}
	defer client.CloseIdleConnections()
	call := func(method, path string, payload, out any) error {
		return deployedRancherJSON(ctx, client, rancherURL, token, method, path, payload, out)
	}
	return prepareDownstreamProvisionerCompatibility(ctx, call, inspectRancherImageReference)
}
