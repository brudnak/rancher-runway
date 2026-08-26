package test

import (
	"errors"
	"strings"
	"testing"

	"github.com/brudnak/ha-rancher-rke2/terratest/settings"
)

func TestRenderLinodeDownstreamResources(t *testing.T) {
	cfg := downstreamProvisioningConfig{
		ClusterName:       "test-cluster",
		MachineName:       "nc-test-cluster-pool1-abc12",
		SecretName:        "cc-test-cluster",
		Namespace:         "fleet-default",
		Distribution:      "k3s",
		KubernetesVersion: "v1.33.4+k3s1",
		Region:            "us-ord",
		InstanceType:      "g6-standard-2",
		Image:             "linode/ubuntu22.04",
		LinodeToken:       "secret-token",
	}

	secretManifest := renderLinodeCredentialSecretManifest(cfg)
	secretExpected := []string{
		"kind: Secret",
		"linodecredentialConfig-token: \"secret-token\"",
	}
	for _, snippet := range secretExpected {
		if !strings.Contains(secretManifest, snippet) {
			t.Fatalf("expected secret manifest to contain %q:\n%s", snippet, secretManifest)
		}
	}

	payload := linodeMachineConfigPayload(cfg)
	if payload["type"] != "rke-machine-config.cattle.io.linodeconfig" {
		t.Fatalf("unexpected machine config payload type: %#v", payload["type"])
	}
	if payload["image"] != "linode/ubuntu22.04" || payload["instanceType"] != "g6-standard-2" || payload["region"] != "us-ord" {
		t.Fatalf("unexpected machine config payload: %#v", payload)
	}
	metadata, ok := payload["metadata"].(map[string]interface{})
	if !ok {
		t.Fatalf("machine config payload metadata has unexpected shape: %#v", payload["metadata"])
	}
	if metadata["namespace"] != "fleet-default" || metadata["name"] != "nc-test-cluster-pool1-abc12" {
		t.Fatalf("unexpected machine config payload metadata: %#v", metadata)
	}
	if _, ok := payload["interfaces"].([]interface{}); !ok {
		t.Fatalf("machine config payload interfaces has unexpected shape: %#v", payload["interfaces"])
	}

	clusterManifest := renderLinodeDownstreamClusterManifest(cfg)
	expected := []string{
		"kind: Cluster",
		"cloudCredentialSecretName: \"cattle-global-data:cc-test-cluster\"",
		"kubernetesVersion: \"v1.33.4+k3s1\"",
		"defaultPodSecurityAdmissionConfigurationTemplateName: \"\"",
		"disable-cloud-controller: false",
		"machineSelectorConfig:",
		"protect-kernel-defaults: false",
		"registries:",
		"controlPlaneRole: true",
		"etcdRole: true",
		"workerRole: true",
		"quantity: 1",
		"machineConfigRef:",
		"kind: LinodeConfig",
		"name: \"nc-test-cluster-pool1-abc12\"",
		"controlPlaneConcurrency: \"1\"",
	}

	for _, snippet := range expected {
		if !strings.Contains(clusterManifest, snippet) {
			t.Fatalf("expected cluster manifest to contain %q:\n%s", snippet, clusterManifest)
		}
	}

	if strings.Contains(clusterManifest, "apiVersion: rke-machine-config.cattle.io/v1") {
		t.Fatalf("machineConfigRef contains API version that Rancher UI does not send:\n%s", clusterManifest)
	}

}

func TestRenderLinodeDownstreamRKE2ManifestOmitsK3sOnlyConfig(t *testing.T) {
	cfg := downstreamProvisioningConfig{
		ClusterName:       "rke2-cluster",
		MachineName:       "nc-rke2-cluster-pool1-abc12",
		SecretName:        "cc-rke2-cluster",
		Namespace:         defaultLinodeNamespace,
		Distribution:      "rke2",
		KubernetesVersion: "v1.36.3+rke2r1",
	}
	manifest := renderLinodeDownstreamClusterManifest(cfg)
	if !strings.Contains(manifest, `kubernetesVersion: "v1.36.3+rke2r1"`) {
		t.Fatalf("RKE2 manifest does not contain the requested version:\n%s", manifest)
	}
	if strings.Contains(manifest, "ingress-controller: traefik") {
		t.Fatalf("RKE2 manifest contains K3s-only ingress configuration:\n%s", manifest)
	}
	for _, role := range []string{"controlPlaneRole: true", "etcdRole: true", "workerRole: true", "quantity: 1"} {
		if !strings.Contains(manifest, role) {
			t.Fatalf("RKE2 manifest missing one-node all-role setting %q:\n%s", role, manifest)
		}
	}
}

func TestConfiguredLinodeDownstreamPlansPreferFrozenEnvironment(t *testing.T) {
	frozen := []settings.LinodeDownstreamPlan{
		{
			Enabled:      true,
			Distribution: "rke2",
			Region:       "us-sea",
			InstanceType: "g6-standard-4",
			Image:        "linode/ubuntu24.04",
		},
		settings.DefaultLinodeDownstreamPlan(),
	}
	data := `[{"enabled":true,"distribution":"rke2","region":"us-sea","instanceType":"g6-standard-4","image":"linode/ubuntu24.04"},{"enabled":false,"distribution":"k3s","region":"us-ord","instanceType":"g6-standard-2","image":"linode/ubuntu22.04"}]`
	t.Setenv(configuredDownstreamLinodePlansEnv, data)

	plans, err := configuredLinodeDownstreamPlans(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != len(frozen) || !plans[0].Enabled || plans[0].Distribution != "rke2" || plans[0].Region != "us-sea" {
		t.Fatalf("unexpected frozen plans: %#v", plans)
	}
}

func TestConfiguredLinodeDownstreamPlansRejectEmptyFrozenEnvironment(t *testing.T) {
	t.Setenv(configuredDownstreamLinodePlansEnv, " ")
	if _, err := configuredLinodeDownstreamPlans(1); err == nil {
		t.Fatal("expected an explicitly empty frozen plan environment to fail closed")
	}
}

func TestLegacyLinodeDownstreamPlansStillEnableEveryHAWithLegacyOverrides(t *testing.T) {
	t.Setenv("K3S_VERSION", "1.35.8+k3s1")
	t.Setenv("LINODE_REGION", "us-sea")
	t.Setenv("LINODE_INSTANCE_TYPE", "g6-standard-4")
	t.Setenv("LINODE_IMAGE", "linode/ubuntu24.04")

	plans, err := legacyLinodeDownstreamPlans(3)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 3 {
		t.Fatalf("legacy plan count = %d, want 3", len(plans))
	}
	for index, plan := range plans {
		if !plan.Enabled || plan.Distribution != "k3s" || plan.KubernetesVersion != "v1.35.8+k3s1" || plan.Region != "us-sea" || plan.InstanceType != "g6-standard-4" || plan.Image != "linode/ubuntu24.04" {
			t.Fatalf("legacy plan %d changed behavior: %#v", index+1, plan)
		}
	}
}

func TestValidateLinodeDownstreamPlansAgainstCatalog(t *testing.T) {
	catalog := linodeCatalogResponse{
		Regions: []linodeCatalogRegion{{ID: "us-ord"}},
		Types:   []linodeCatalogType{{ID: "g6-standard-2"}},
		Images:  []linodeCatalogImage{{ID: "linode/ubuntu22.04"}},
	}
	valid := settings.DefaultLinodeDownstreamPlan()
	valid.Enabled = true
	if err := validateLinodeDownstreamPlansAgainstCatalog([]settings.LinodeDownstreamPlan{valid}, catalog); err != nil {
		t.Fatal(err)
	}

	invalid := valid
	invalid.Region = "missing-region"
	if err := validateLinodeDownstreamPlansAgainstCatalog([]settings.LinodeDownstreamPlan{invalid}, catalog); err == nil {
		t.Fatal("expected unavailable enabled region to be rejected")
	}
	invalid.Enabled = false
	if err := validateLinodeDownstreamPlansAgainstCatalog([]settings.LinodeDownstreamPlan{invalid}, catalog); err != nil {
		t.Fatalf("disabled plan should not block provider preflight: %v", err)
	}
}

func TestDownstreamCatalogPreflightExcludesReusableActiveRows(t *testing.T) {
	activePlan := settings.DefaultLinodeDownstreamPlan()
	activePlan.Enabled = true
	activePlan.Region = "retired-region"
	activePlan.InstanceType = "retired-type"
	activePlan.Image = "retired/image"

	retryPlan := settings.DefaultLinodeDownstreamPlan()
	retryPlan.Enabled = true
	plans := []settings.LinodeDownstreamPlan{activePlan, retryPlan}
	records := map[int]downstreamOutputRecord{
		1: {
			HAIndex:             1,
			ClusterName:         "active-on-retired-provider-options",
			Namespace:           defaultLinodeNamespace,
			ManagementClusterID: "c-m-active",
			Phase:               "active",
		},
		2: {
			HAIndex:     2,
			ClusterName: "failed-on-current-provider-options",
			Namespace:   defaultLinodeNamespace,
			Phase:       "failed",
		},
	}
	statusCalls := 0
	work, err := determineDownstreamProvisioningWork(
		plans,
		func(haIndex int) (downstreamOutputRecord, bool, error) {
			record, found := records[haIndex]
			return record, found, nil
		},
		func(int) (string, error) {
			return "/management.kubeconfig", nil
		},
		func(_, _, clusterName string) (provisioningClusterStatus, error) {
			statusCalls++
			if clusterName != records[1].ClusterName {
				t.Fatalf("unexpected live verification for %s", clusterName)
			}
			status := provisioningClusterStatus{}
			status.Status.Phase = "Active"
			status.Status.Ready = true
			status.Status.ClusterName = records[1].ManagementClusterID
			return status, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if statusCalls != 1 {
		t.Fatalf("live status calls = %d, want 1 for only the recorded active row", statusCalls)
	}
	if len(work) != 1 || work[0].InstanceNum != 2 || work[0].Existing == nil || work[0].Existing.Phase != "failed" {
		t.Fatalf("provisioning work = %#v, want only failed HA 2", work)
	}

	plansNeedingMutation, err := downstreamPlansForProvisioningWork(len(plans), work)
	if err != nil {
		t.Fatal(err)
	}
	if plansNeedingMutation[0].Enabled || !plansNeedingMutation[1].Enabled {
		t.Fatalf("catalog preflight mask = %#v, want only HA 2 enabled", plansNeedingMutation)
	}
	catalog := linodeCatalogResponse{
		Regions: []linodeCatalogRegion{{ID: retryPlan.Region}},
		Types:   []linodeCatalogType{{ID: retryPlan.InstanceType}},
		Images:  []linodeCatalogImage{{ID: retryPlan.Image}},
	}
	if err := validateLinodeDownstreamPlansAgainstCatalog(plansNeedingMutation, catalog); err != nil {
		t.Fatalf("reusable active row with retired provider options blocked retry: %v", err)
	}
	if err := validateLinodeDownstreamPlansAgainstCatalog(plans, catalog); err == nil {
		t.Fatal("test setup error: full-plan validation unexpectedly accepted the retired active row")
	}
}

func TestDNSLabel(t *testing.T) {
	got := dnsLabel("Rancher_Runway/Some Lane!!")
	if got != "rancher-runway-some-lane" {
		t.Fatalf("dnsLabel() = %q", got)
	}
}

func TestShortRunID(t *testing.T) {
	if got := shortRunID("1234567890"); got != "34567890" {
		t.Fatalf("shortRunID() = %q", got)
	}
}

func TestDownstreamClusterNamePrefix(t *testing.T) {
	tests := []struct {
		name     string
		explicit string
		runID    string
		want     string
	}{
		{name: "explicit", explicit: "custom", runID: "1234567890", want: "custom"},
		{name: "github", runID: "1234567890", want: "gha"},
		{name: "local", want: "rancher-runway"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := downstreamClusterNamePrefix(tt.explicit, tt.runID); got != tt.want {
				t.Fatalf("downstreamClusterNamePrefix() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDownstreamClusterNamePreservesRunAndUniqueSuffix(t *testing.T) {
	got := downstreamClusterName(strings.Repeat("very-long-prefix-", 8), "1234567890", 2, "abc12345")
	if len(got) > 53 {
		t.Fatalf("cluster name length = %d: %s", len(got), got)
	}
	if !strings.HasSuffix(got, "34567890-ha2-abc12345") {
		t.Fatalf("cluster name lost collision-resistant suffix: %s", got)
	}
}

func TestDownstreamProvisioningRunIDFallsBackToPanelRunID(t *testing.T) {
	t.Setenv("GITHUB_RUN_ID", "")
	t.Setenv("SIGNOFF_RUN_ID", "")
	t.Setenv(runIDEnv, "panel-run-12345678")
	if got := downstreamProvisioningRunID(); got != "panel-run-12345678" {
		t.Fatalf("downstreamProvisioningRunID() = %q", got)
	}
	name := downstreamClusterName("runway", downstreamProvisioningRunID(), 1, "abc12345")
	if !strings.Contains(name, "12345678-ha1-abc12345") {
		t.Fatalf("panel run id was not included in downstream resource name: %s", name)
	}
}

func TestReusableActiveDownstreamRecordSkipsSuccessfulRowOnRetry(t *testing.T) {
	record := downstreamOutputRecord{
		HAIndex:             1,
		ClusterName:         "already-active",
		Namespace:           defaultLinodeNamespace,
		ManagementClusterID: "c-m-active",
		Phase:               "active",
	}
	called := 0
	reusable, err := reusableActiveDownstreamRecord(record, "/management.kubeconfig", func(kubeconfigPath, namespace, clusterName string) (provisioningClusterStatus, error) {
		called++
		if kubeconfigPath != "/management.kubeconfig" || namespace != defaultLinodeNamespace || clusterName != record.ClusterName {
			t.Fatalf("unexpected verification target: %s %s/%s", kubeconfigPath, namespace, clusterName)
		}
		status := provisioningClusterStatus{}
		status.Status.Phase = "Active"
		status.Status.Ready = true
		status.Status.ClusterName = record.ManagementClusterID
		return status, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reusable || called != 1 {
		t.Fatalf("active record reusable=%t status calls=%d", reusable, called)
	}
}

func TestReusableActiveDownstreamRecordRetriesOnlyIncompleteOrMissingRows(t *testing.T) {
	failedRecord := downstreamOutputRecord{ClusterName: "failed", Phase: "failed"}
	called := false
	reusable, err := reusableActiveDownstreamRecord(failedRecord, "unused", func(_, _, _ string) (provisioningClusterStatus, error) {
		called = true
		return provisioningClusterStatus{}, nil
	})
	if err != nil || reusable || called {
		t.Fatalf("failed row reusable=%t called=%t err=%v", reusable, called, err)
	}

	missingRecord := downstreamOutputRecord{
		ClusterName:         "missing",
		Namespace:           defaultLinodeNamespace,
		ManagementClusterID: "c-m-missing",
		Phase:               "active",
	}
	reusable, err = reusableActiveDownstreamRecord(missingRecord, "unused", func(_, _, _ string) (provisioningClusterStatus, error) {
		return provisioningClusterStatus{}, errors.New("Error from server (NotFound): clusters.provisioning.cattle.io missing not found")
	})
	if err != nil || reusable {
		t.Fatalf("missing active row reusable=%t err=%v", reusable, err)
	}
}

func TestReusableActiveDownstreamRecordDoesNotDeleteOnUncertainState(t *testing.T) {
	record := downstreamOutputRecord{
		ClusterName:         "active-but-unreachable",
		Namespace:           defaultLinodeNamespace,
		ManagementClusterID: "c-m-active",
		Phase:               "active",
	}
	if reusable, err := reusableActiveDownstreamRecord(record, "unused", func(_, _, _ string) (provisioningClusterStatus, error) {
		return provisioningClusterStatus{}, errors.New("temporary API timeout")
	}); err == nil || reusable {
		t.Fatalf("uncertain active row reusable=%t err=%v", reusable, err)
	}
}

func TestSummarizeProvisioningClusterStatus(t *testing.T) {
	status := provisioningClusterStatus{}
	status.Status.Phase = "Updating"
	status.Status.Ready = false
	status.Status.Conditions = append(status.Status.Conditions, struct {
		Type    string `json:"type"`
		Status  string `json:"status"`
		Reason  string `json:"reason"`
		Message string `json:"message"`
	}{Type: "Ready", Status: "False", Reason: "Waiting", Message: "node pending"})

	summary := summarizeProvisioningClusterStatus(status)
	if !strings.Contains(summary, "phase=Updating ready=false") || !strings.Contains(summary, "Ready=False/Waiting node pending") {
		t.Fatalf("unexpected summary: %s", summary)
	}
}
