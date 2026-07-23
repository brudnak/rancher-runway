package test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestConfigureDownstreamRancherWebhookConfigCreatesMissingConfigMap(t *testing.T) {
	valuesJSON := `{"global":{"cattle":{"systemDefaultRegistry":"stgregistry.suse.com"}},"image":{"repository":"rancher/rancher-webhook","tag":"v0.8.9-rc.1"}}`
	var directKubeconfig string
	var directArgs []string
	outputCalls := 0
	runner := downstreamWebhookKubectl{
		output: func(kubeconfigPath string, args ...string) (string, error) {
			if kubeconfigPath != "/tmp/downstream.yaml" {
				t.Fatalf("unexpected kubeconfig path %q", kubeconfigPath)
			}
			outputCalls++
			switch outputCalls {
			case 1:
				return "", nil
			case 2:
				return rancherWebhookConfigMapOutput(t, valuesJSON), nil
			default:
				t.Fatalf("unexpected ConfigMap inspection call %d", outputCalls)
				return "", nil
			}
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
	if outputCalls != 2 {
		t.Fatalf("expected an initial inspection and read-back, got %d calls", outputCalls)
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
	outputCalls := 0
	runner := downstreamWebhookKubectl{
		output: func(_ string, _ ...string) (string, error) {
			outputCalls++
			if outputCalls == 1 {
				return rancherWebhookConfigMapOutput(t, `{"existing":"value"}`), nil
			}
			return rancherWebhookConfigMapOutput(t, valuesJSON), nil
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
	if outputCalls != 2 {
		t.Fatalf("expected an initial inspection and read-back, got %d calls", outputCalls)
	}
}

func TestConfigureDownstreamRancherWebhookConfigRecoversWhenAbsentConfigMapIsCreatedConcurrently(t *testing.T) {
	valuesJSON := `{"image":{"repository":"rancher/rancher-webhook","tag":"v0.8.9-rc.1"}}`
	var directCalls [][]string
	outputCalls := 0
	sleepCalls := 0
	runner := downstreamWebhookKubectl{
		output: func(_ string, _ ...string) (string, error) {
			outputCalls++
			switch outputCalls {
			case 1:
				return "", nil
			case 2, 3:
				return rancherWebhookConfigMapOutput(t, `{"image":{"tag":"old"}}`), nil
			case 4:
				return rancherWebhookConfigMapOutput(t, valuesJSON), nil
			default:
				t.Fatalf("unexpected ConfigMap inspection call %d", outputCalls)
				return "", nil
			}
		},
		direct: func(_ string, args ...string) error {
			directCalls = append(directCalls, append([]string(nil), args...))
			if len(directCalls) == 1 {
				return fmt.Errorf("AlreadyExists")
			}
			return nil
		},
		sleep: func(delay time.Duration) {
			if delay != downstreamWebhookConfigRetryInterval {
				t.Fatalf("unexpected retry delay %s", delay)
			}
			sleepCalls++
		},
	}

	if err := configureDownstreamRancherWebhookConfig("/tmp/downstream.yaml", valuesJSON, runner); err != nil {
		t.Fatalf("expected absent-to-created race to recover through merge patch, got %v", err)
	}
	if len(directCalls) != 2 {
		t.Fatalf("expected create and patch calls, got %#v", directCalls)
	}
	if directCalls[0][0] != "create" || directCalls[1][0] != "patch" {
		t.Fatalf("expected create followed by patch, got %#v", directCalls)
	}
	if outputCalls != 4 || sleepCalls != 1 {
		t.Fatalf("expected two convergence attempts, got %d inspections and %d sleeps", outputCalls, sleepCalls)
	}
}

func TestConfigureDownstreamRancherWebhookConfigRecoversWhenPresentConfigMapIsDeletedConcurrently(t *testing.T) {
	valuesJSON := `{"image":{"repository":"rancher/rancher-webhook","tag":"v0.10.9-rc.2"}}`
	var directCalls [][]string
	outputCalls := 0
	sleepCalls := 0
	runner := downstreamWebhookKubectl{
		output: func(_ string, _ ...string) (string, error) {
			outputCalls++
			switch outputCalls {
			case 1:
				return rancherWebhookConfigMapOutput(t, `{"image":{"tag":"old"}}`), nil
			case 2, 3:
				return "", nil
			case 4:
				return rancherWebhookConfigMapOutput(t, valuesJSON), nil
			default:
				t.Fatalf("unexpected ConfigMap inspection call %d", outputCalls)
				return "", nil
			}
		},
		direct: func(_ string, args ...string) error {
			directCalls = append(directCalls, append([]string(nil), args...))
			if len(directCalls) == 1 {
				return fmt.Errorf("NotFound")
			}
			return nil
		},
		sleep: func(delay time.Duration) {
			if delay != downstreamWebhookConfigRetryInterval {
				t.Fatalf("unexpected retry delay %s", delay)
			}
			sleepCalls++
		},
	}

	if err := configureDownstreamRancherWebhookConfig("/tmp/downstream.yaml", valuesJSON, runner); err != nil {
		t.Fatalf("expected present-to-deleted race to recover through create, got %v", err)
	}
	if len(directCalls) != 2 {
		t.Fatalf("expected patch and create calls, got %#v", directCalls)
	}
	if directCalls[0][0] != "patch" || directCalls[1][0] != "create" {
		t.Fatalf("expected patch followed by create, got %#v", directCalls)
	}
	if outputCalls != 4 || sleepCalls != 1 {
		t.Fatalf("expected two convergence attempts, got %d inspections and %d sleeps", outputCalls, sleepCalls)
	}
}

func TestConfigureDownstreamRancherWebhookConfigReturnsWhenValueAlreadyConverged(t *testing.T) {
	valuesJSON := `{"image":{"repository":"rancher/rancher-webhook","tag":"v0.10.9-rc.2"}}`
	runner := downstreamWebhookKubectl{
		output: func(_ string, _ ...string) (string, error) {
			return rancherWebhookConfigMapOutput(t, valuesJSON), nil
		},
		direct: func(_ string, _ ...string) error {
			t.Fatal("kubectl mutation should not run when the ConfigMap already matches")
			return nil
		},
	}

	if err := configureDownstreamRancherWebhookConfig("/tmp/downstream.yaml", valuesJSON, runner); err != nil {
		t.Fatalf("unexpected converged ConfigMap error: %v", err)
	}
}

func TestConfigureDownstreamRancherWebhookConfigPropagatesInspectionFailure(t *testing.T) {
	outputCalls := 0
	sleepCalls := 0
	runner := downstreamWebhookKubectl{
		output: func(_ string, _ ...string) (string, error) {
			outputCalls++
			return "", fmt.Errorf("API unavailable")
		},
		direct: func(_ string, _ ...string) error {
			t.Fatal("direct kubectl should not run after inspection failure")
			return nil
		},
		sleep: func(delay time.Duration) {
			if delay != downstreamWebhookConfigRetryInterval {
				t.Fatalf("unexpected retry delay %s", delay)
			}
			sleepCalls++
		},
	}

	err := configureDownstreamRancherWebhookConfig("/tmp/downstream.yaml", `{}`, runner)
	if err == nil {
		t.Fatal("expected ConfigMap inspection error")
	}
	if outputCalls != downstreamWebhookConfigMaxAttempts {
		t.Fatalf("expected %d bounded inspection attempts, got %d", downstreamWebhookConfigMaxAttempts, outputCalls)
	}
	if sleepCalls != downstreamWebhookConfigMaxAttempts-1 {
		t.Fatalf("expected %d retry sleeps, got %d", downstreamWebhookConfigMaxAttempts-1, sleepCalls)
	}
	if !strings.Contains(err.Error(), "did not converge after") || !strings.Contains(err.Error(), "API unavailable") {
		t.Fatalf("unexpected convergence error: %v", err)
	}
}

func TestDownstreamWebhookKubectlCommandArgsBoundsAPIRequests(t *testing.T) {
	got := downstreamWebhookKubectlCommandArgs(
		"/tmp/downstream.yaml",
		"get", "configmap", rancherConfigName,
		"-n", rancherConfigNamespace,
	)
	want := []string{
		"--kubeconfig", "/tmp/downstream.yaml",
		"--request-timeout=15s",
		"get", "configmap", rancherConfigName,
		"-n", rancherConfigNamespace,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected bounded kubectl arguments:\n got: %#v\nwant: %#v", got, want)
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

func rancherWebhookConfigMapOutput(t *testing.T, valuesJSON string) string {
	t.Helper()
	output, err := json.Marshal(struct {
		Data map[string]string `json:"data"`
	}{
		Data: map[string]string{rancherWebhookConfigDataKey: valuesJSON},
	})
	if err != nil {
		t.Fatalf("encode ConfigMap test fixture: %v", err)
	}
	return string(output)
}
