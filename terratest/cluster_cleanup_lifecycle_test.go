package test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/logger"
	"github.com/gruntwork-io/terratest/modules/terraform"
)

// Install in the run's own module copy. Older slots have already frozen their
// main.tf. Keep the original for_each keys so survivors retain their addresses.
const clusterCleanupVariable = `variable "runway_retiring_ha" {
  type = string
  default = ""
}
`
const clusterCleanupOverride = `module "ha" {
  for_each = {
    for key, instance in local.ha_instances : key => instance
    if (key != var.runway_retiring_ha &&
      !fileexists("${path.module}/.runway-destroyed-ha-${key}") &&
      !fileexists("${path.module}/.runway-destroyed-ha-${key}.pending"))
  }
}
`

func TestDestroyRancherCluster(t *testing.T) {
	requireExplicitLifecycleTest(t, "TestDestroyRancherCluster")
	clusterID := os.Getenv(cleanupClusterIDEnv)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	panel := &localControlPanel{testDir: cwd}
	record, index, err := panel.clusterCleanupRecord(clusterID)
	if err != nil {
		t.Fatal(err)
	}
	if record.RunID != os.Getenv(runIDEnv) || record.TerraformModuleDir != os.Getenv(terraformModuleDirEnv) ||
		record.TerraformStatePath != os.Getenv(terraformStatePathEnv) || record.TerraformDataDir != os.Getenv(terraformDataDirEnv) ||
		record.HAOutputRoot != os.Getenv(haOutputRootEnv) {
		t.Fatal("worker environment does not match the selected cluster's recorded workspace")
	}
	if err := validateScopedCleanupTarget(); err != nil {
		t.Fatal(err)
	}
	// A slot recorded before backend labels were fixed can identify its backend
	// through Terraform's initialized metadata. Init refuses backend migration.
	switch record.TerraformBackend {
	case "", "local (<nil>)":
		if !fileExists(filepath.Join(record.TerraformDataDir, "terraform.tfstate")) {
			t.Fatal("the original Terraform backend cannot be verified for this slot")
		}
	case terraformBackendLabelForRun(record.RunID, record.TerraformStatePath):
	default:
		t.Fatal("configured Terraform backend differs from the recorded run backend")
	}
	setupConfig(t)
	if deploymentType() != deploymentTypeHARKE2 || configuredRancherInstanceCount() != record.TotalHAs {
		t.Fatal("worker configuration does not match the selected RKE2 run")
	}
	if err := validateSecretEnvironment(); err != nil {
		t.Fatal(err)
	}
	options := getTerraformOptions(t, record.TotalHAs)
	// Terratest's retry logger also prints command arguments independently of the options logger.
	logger.Default = logger.Discard
	options.Logger = logger.Discard
	// Init must refuse a changed backend. Never migrate or replace this slot's state.
	if _, err := terraform.InitE(t, options); err != nil {
		t.Fatalf("initialize recorded Terraform backend: %v", err)
	}

	haDir := panel.haInstanceDirForRun(record, index)
	marker := destroyedHAPath(options.TerraformDir, index)
	beforeApply := func(partiallyApplied bool) error {
		return checkClusterDependents(record.RunID, clusterID, index, filepath.Join(haDir, "kube_config.yaml"), partiallyApplied)
	}
	if err := applyClusterRemoval(t, options, index, beforeApply); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(haDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(marker+".pending", marker); err != nil {
		t.Fatal(err)
	}
	t.Logf("Destroyed HA %d Local (%s)", index, record.RunID)
}

var errDownstreamAPIUnavailable = errors.New("cannot contact the selected Rancher's Kubernetes API")

func checkClusterDependents(runID, clusterID string, index int, kubeconfig string, partiallyApplied bool) error {
	records, err := readDeployedDownstreams(runID)
	if err != nil {
		return err
	}
	children := slices.DeleteFunc(records, func(child deployedDownstreamRecord) bool { return child.ParentID != clusterID })
	_, configuredChild, err := readDownstreamOutputRecord(index)
	if err != nil {
		return err
	}
	tracked := configuredChild || len(children) > 0
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := verifyNoAttachedClusters(ctx, kubeconfig, tracked, operationCommand); err != nil {
		// A partial apply can remove the management API before its other AWS
		// resources. Only finish a previously verified, shrinking set of deletes.
		if partiallyApplied && !tracked && errors.Is(err, errDownstreamAPIUnavailable) {
			return nil
		}
		return err
	}
	// Rancher confirms these previously tracked downstreams and machines are
	// already gone. Keep sibling records and all lab history.
	for _, child := range children {
		now := time.Now().UTC()
		child.DeletedAt = &now
		if err := saveDeployedDownstream(child); err != nil {
			return err
		}
	}
	return removeDownstreamOutputArtifactsForRun(runID, index)
}

// Plan once, check every managed change, then apply those exact bytes.
// A receipt lets a retry finish after Terraform or local cleanup was interrupted.
func applyClusterRemoval(t *testing.T, options *terraform.Options, index int, beforeApply func(bool) error) error {
	dir := options.TerraformDir
	if err := writePrivateConfigAtomically(filepath.Join(dir, "runway_cluster_cleanup.tf"), []byte(clusterCleanupVariable)); err != nil {
		return err
	}
	if err := writePrivateConfigAtomically(filepath.Join(dir, "runway_cluster_cleanup_override.tf"), []byte(clusterCleanupOverride)); err != nil {
		return err
	}
	privateDir, err := os.MkdirTemp(dir, ".cluster-plan-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(privateDir)

	target := fmt.Sprintf(`module.ha["%d"]`, index)
	options.PlanFilePath = filepath.Join(privateDir, "plan")
	options.Vars["runway_retiring_ha"] = strconv.Itoa(index)
	args := terraform.FormatArgs(options, "plan", "-input=false", "-target="+target)
	if _, err := terraform.RunTerraformCommandE(t, options, args...); err != nil {
		return fmt.Errorf("plan individual cluster removal: %w", err)
	}
	raw, err := terraform.ShowE(t, options)
	if err != nil {
		return fmt.Errorf("inspect saved cluster destroy plan: %w", err)
	}
	pending := destroyedHAPath(dir, index) + ".pending"
	deletes, err := clusterRemovalPlanResources(raw, target)
	if err != nil {
		return err
	}
	prior, err := os.ReadFile(pending)
	partial := false
	switch {
	case err == nil:
		var reviewed map[string]string
		if err := json.Unmarshal(prior, &reviewed); err != nil || len(reviewed) == 0 {
			return fmt.Errorf("invalid pending cluster destroy record")
		}
		for address, id := range deletes {
			if reviewed[address] != id {
				return fmt.Errorf("Terraform identity changed for %s during cluster destroy", address)
			}
		}
		partial = len(deletes) < len(reviewed)
	case !os.IsNotExist(err):
		return err
	case len(deletes) == 0:
		return fmt.Errorf("Terraform has no resources to destroy for %s", target)
	}
	if err := beforeApply(partial); err != nil {
		return err
	}
	if prior == nil {
		receipt, err := json.Marshal(deletes)
		if err != nil {
			return err
		}
		if err := writePrivateConfigAtomically(pending, receipt); err != nil {
			return err
		}
	}
	t.Logf("Applying verified destroy plan for %s", target)
	_, err = terraform.ApplyE(t, options)
	return err
}

func clusterRemovalPlanResources(raw, target string) (map[string]string, error) {
	var plan struct {
		FormatVersion   string `json:"format_version"`
		ResourceChanges []struct {
			Address         string
			Mode            string
			Deposed         string
			PreviousAddress string `json:"previous_address"`
			Change          struct {
				Actions   []string
				Before    struct{ ID string }
				Importing json.RawMessage
			}
		} `json:"resource_changes"`
		DeferredChanges []json.RawMessage `json:"deferred_changes"`
	}
	if err := json.Unmarshal([]byte(raw), &plan); err != nil {
		return nil, err
	}
	if !strings.HasPrefix(plan.FormatVersion, "1.") || len(plan.DeferredChanges) != 0 {
		return nil, fmt.Errorf("unsupported or deferred Terraform plan")
	}
	deletes := make(map[string]string)
	for _, change := range plan.ResourceChanges {
		actions := change.Change.Actions
		if len(actions) != 1 || change.PreviousAddress != "" || len(change.Change.Importing) != 0 {
			return nil, fmt.Errorf("unexpected Terraform change to %s", change.Address)
		}
		switch {
		case actions[0] == "no-op", change.Mode == "data" && actions[0] == "read":
		case actions[0] == "delete" && strings.HasPrefix(change.Address, target+"."):
			if change.Mode == "managed" {
				if change.Change.Before.ID == "" {
					return nil, fmt.Errorf("missing resource identity for %s", change.Address)
				}
				deletes[change.Address+"#"+change.Deposed] = change.Change.Before.ID
			}
		default:
			return nil, fmt.Errorf("refusing plan with unexpected change %v to %s", actions, change.Address)
		}
	}
	return deletes, nil
}

// Query the CRDs first. A failed Rancher install can have a healthy RKE2 API
// with none of the Rancher types installed. API failures must not mean empty.
func verifyNoAttachedClusters(ctx context.Context, kubeconfig string, tracked bool, command rancherUpgradeCommand) error {
	get := func(args ...string) ([]byte, error) {
		return command(ctx, nil, "kubectl", append([]string{"--kubeconfig", kubeconfig, "--request-timeout=10s", "get"}, args...)...)
	}
	for _, resource := range []string{"clusters.provisioning.cattle.io", "clusters.management.cattle.io", "machines.cluster.x-k8s.io"} {
		crd, err := get("crd", resource, "--ignore-not-found", "-o", "name")
		if err != nil {
			return fmt.Errorf("%w: %v", errDownstreamAPIUnavailable, err)
		}
		if len(strings.TrimSpace(string(crd))) == 0 {
			if tracked {
				return fmt.Errorf("cannot verify recorded downstream cleanup because %s is missing from this Rancher", resource)
			}
			continue
		}
		data, err := get(resource, "-A", "-o", "json")
		if err != nil {
			return fmt.Errorf("cannot list %s on the selected Rancher: %w", resource, err)
		}
		var list struct {
			Items []struct {
				Metadata struct{ Name, Namespace string }
			}
		}
		if err := json.Unmarshal(data, &list); err != nil || list.Items == nil {
			return fmt.Errorf("invalid %s inventory on the selected Rancher", resource)
		}
		for _, item := range list.Items {
			if resource != "machines.cluster.x-k8s.io" && item.Metadata.Name == "local" && (item.Metadata.Namespace == "" || item.Metadata.Namespace == "fleet-local" || item.Metadata.Namespace == "local") {
				continue
			}
			return fmt.Errorf("remove or detach %s %s from the selected Rancher before destroying it", resource, item.Metadata.Name)
		}
	}
	return nil
}
