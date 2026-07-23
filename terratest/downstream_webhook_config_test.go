package test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func TestConfigureDownstreamRancherWebhookConfigCreatesMissingConfigMap(t *testing.T) {
	valuesJSON := `{"global":{"cattle":{"systemDefaultRegistry":"stgregistry.suse.com"}},"image":{"repository":"rancher/rancher-webhook","tag":"v0.8.9-rc.1"}}`
	var directKubeconfig string
	var directArgs []string
	runner := downstreamWebhookKubectl{
		output: func(kubeconfigPath string, args ...string) (string, error) {
			if kubeconfigPath != "/tmp/downstream.yaml" {
				t.Fatalf("unexpected kubeconfig path %q", kubeconfigPath)
			}
			return "", nil
		},
		direct: func(kubeconfigPath string, args ...string) error {
			directKubeconfig = kubeconfigPath
			directArgs = append([]string(nil), args...)
			return nil
		},
	}

	if err := configureDownstreamRancherWebhookConfig("/tmp/downstream.yaml", valuesJSON, runner); err != nil {
		t.Fatalf("unexpected ConfigMap create error: %v", err)
	}
	if directKubeconfig != "/tmp/downstream.yaml" {
		t.Fatalf("unexpected direct kubeconfig path %q", directKubeconfig)
	}
	want := []string{
		"create", "configmap", rancherConfigName,
		"-n", rancherConfigNamespace,
		"--from-literal=" + rancherWebhookConfigDataKey + "=" + valuesJSON,
	}
	if !reflect.DeepEqual(directArgs, want) {
		t.Fatalf("unexpected ConfigMap create args:\n got: %#v\nwant: %#v", directArgs, want)
	}
}

func TestConfigureDownstreamRancherWebhookConfigPatchesOnlyWebhookData(t *testing.T) {
	valuesJSON := `{"global":{"cattle":{"systemDefaultRegistry":"stgregistry.suse.com"}},"image":{"repository":"rancher/rancher-webhook","tag":"v0.8.9-rc.1"}}`
	var directArgs []string
	runner := downstreamWebhookKubectl{
		output: func(_ string, _ ...string) (string, error) {
			return "configmap/rancher-config\n", nil
		},
		direct: func(_ string, args ...string) error {
			directArgs = append([]string(nil), args...)
			return nil
		},
	}

	if err := configureDownstreamRancherWebhookConfig("/tmp/downstream.yaml", valuesJSON, runner); err != nil {
		t.Fatalf("unexpected ConfigMap patch error: %v", err)
	}
	if len(directArgs) != 8 {
		t.Fatalf("unexpected ConfigMap patch args: %#v", directArgs)
	}
	wantPrefix := []string{"patch", "configmap", rancherConfigName, "-n", rancherConfigNamespace, "--type=merge", "--patch"}
	if !reflect.DeepEqual(directArgs[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("unexpected ConfigMap patch args: %#v", directArgs)
	}

	var patch struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal([]byte(directArgs[len(directArgs)-1]), &patch); err != nil {
		t.Fatalf("invalid ConfigMap patch JSON: %v", err)
	}
	if len(patch.Data) != 1 || patch.Data[rancherWebhookConfigDataKey] != valuesJSON {
		t.Fatalf("expected patch to contain only %s, got %#v", rancherWebhookConfigDataKey, patch.Data)
	}
}

func TestConfigureDownstreamRancherWebhookConfigPatchesAfterCreateRace(t *testing.T) {
	valuesJSON := `{"image":{"repository":"rancher/rancher-webhook","tag":"v0.8.9-rc.1"}}`
	var directCalls [][]string
	runner := downstreamWebhookKubectl{
		output: func(_ string, _ ...string) (string, error) {
			return "", nil
		},
		direct: func(_ string, args ...string) error {
			directCalls = append(directCalls, append([]string(nil), args...))
			if len(directCalls) == 1 {
				return fmt.Errorf("AlreadyExists")
			}
			return nil
		},
	}

	if err := configureDownstreamRancherWebhookConfig("/tmp/downstream.yaml", valuesJSON, runner); err != nil {
		t.Fatalf("expected create race to recover through merge patch, got %v", err)
	}
	if len(directCalls) != 2 {
		t.Fatalf("expected create and patch calls, got %#v", directCalls)
	}
	if directCalls[0][0] != "create" || directCalls[1][0] != "patch" {
		t.Fatalf("expected create followed by patch, got %#v", directCalls)
	}
}

func TestConfigureDownstreamRancherWebhookConfigPropagatesInspectionFailure(t *testing.T) {
	runner := downstreamWebhookKubectl{
		output: func(_ string, _ ...string) (string, error) {
			return "", fmt.Errorf("API unavailable")
		},
		direct: func(_ string, _ ...string) error {
			t.Fatal("direct kubectl should not run after inspection failure")
			return nil
		},
	}

	err := configureDownstreamRancherWebhookConfig("/tmp/downstream.yaml", `{}`, runner)
	if err == nil {
		t.Fatal("expected ConfigMap inspection error")
	}
}

func TestRancherWebhookValuesJSONTrimsAndParsesImage(t *testing.T) {
	payload, err := rancherWebhookValuesJSON("  stgregistry.suse.com/rancher/rancher-webhook:v0.8.9-rc.1  ")
	if err != nil {
		t.Fatalf("unexpected Rancher webhook values error: %v", err)
	}

	var values rancherWebhookOverrideValues
	if err := json.Unmarshal([]byte(payload), &values); err != nil {
		t.Fatalf("invalid Rancher webhook values JSON: %v", err)
	}
	if values.Global.Cattle.SystemDefaultRegistry != "stgregistry.suse.com" {
		t.Fatalf("unexpected webhook registry %q", values.Global.Cattle.SystemDefaultRegistry)
	}
	if values.Image.Repository != "rancher/rancher-webhook" || values.Image.Tag != "v0.8.9-rc.1" {
		t.Fatalf("unexpected webhook image values: %#v", values.Image)
	}
}
