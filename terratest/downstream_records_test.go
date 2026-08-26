package test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDownstreamOutputRecordRoundTripIncludesLifecycleAndDistribution(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("RANCHER_RUNWAY_WORKSPACE", workspace)
	t.Setenv(runIDEnv, "")
	record := downstreamOutputRecord{
		HAIndex:           2,
		RancherHost:       "https://rancher.example.test",
		ClusterName:       "downstream-rke2",
		Distribution:      "rke2",
		KubernetesVersion: "1.36.3+rke2r1",
		Phase:             "failed",
		Error:             "node did not become ready",
		Namespace:         defaultLinodeNamespace,
	}
	if err := writeDownstreamOutputRecord(record); err != nil {
		t.Fatal(err)
	}

	got, found, err := readDownstreamOutputRecord(2)
	if err != nil {
		t.Fatal(err)
	}
	if !found || got.Distribution != "rke2" || got.KubernetesVersion != "v1.36.3+rke2r1" || got.Phase != "failed" || got.Error == "" {
		t.Fatalf("unexpected record round trip: %#v", got)
	}
	data, err := os.ReadFile(downstreamOutputRecordPath(2))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "linodecredentialConfig-token") {
		t.Fatalf("output record persisted credential material: %s", data)
	}
}

func TestDownstreamOutputRecordFromConfigNeverPersistsLinodeToken(t *testing.T) {
	record := downstreamOutputRecordFromConfig(1, downstreamProvisioningConfig{
		ClusterName:       "token-test",
		SecretName:        "cc-token-test",
		Distribution:      "k3s",
		KubernetesVersion: "v1.35.8+k3s1",
		LinodeToken:       "linode-super-secret",
	}, TerraformOutputs{RancherURL: "https://rancher.example.test"})
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "linode-super-secret") {
		t.Fatalf("Linode token was serialized into downstream state: %s", data)
	}
}

func TestReadDownstreamOutputRecordNormalizesLegacyK3sVersion(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("RANCHER_RUNWAY_WORKSPACE", workspace)
	t.Setenv(runIDEnv, "")
	if err := os.MkdirAll(automationOutputDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := map[string]any{
		"ha_index":       1,
		"cluster_name":   "legacy-k3s",
		"k3s_version":    "1.35.8+k3s1",
		"namespace":      defaultLinodeNamespace,
		"secret_name":    "cc-legacy-k3s",
		"machine_config": "nc-legacy-k3s",
	}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(downstreamOutputRecordPath(1), data, 0o600); err != nil {
		t.Fatal(err)
	}

	got, found, err := readDownstreamOutputRecord(1)
	if err != nil {
		t.Fatal(err)
	}
	if !found || got.Distribution != "k3s" || got.KubernetesVersion != "v1.35.8+k3s1" || got.K3SVersion != got.KubernetesVersion {
		t.Fatalf("legacy record was not normalized: %#v", got)
	}
}

func TestRemoveDownstreamOutputArtifactsIsIdempotent(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("RANCHER_RUNWAY_WORKSPACE", workspace)
	t.Setenv(runIDEnv, "")
	if err := os.MkdirAll(automationOutputDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		downstreamOutputRecordPath(1),
		automationOutputPath("downstream-ha-1.env"),
		automationOutputPath("downstream-ha-1.kubeconfig"),
	} {
		if err := os.WriteFile(path, []byte("test"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := removeDownstreamOutputArtifacts(1); err != nil {
		t.Fatal(err)
	}
	if err := removeDownstreamOutputArtifacts(1); err != nil {
		t.Fatalf("second artifact removal should be idempotent: %v", err)
	}
	if matches, _ := filepath.Glob(filepath.Join(automationOutputDir(), "downstream-ha-1.*")); len(matches) != 0 {
		t.Fatalf("downstream artifacts remain: %v", matches)
	}
}

func TestRemoveDownstreamOutputArtifactsPreservesRecordWhenAuxiliaryRemovalFails(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("RANCHER_RUNWAY_WORKSPACE", workspace)
	t.Setenv(runIDEnv, "")
	if err := writeDownstreamOutputRecord(downstreamOutputRecord{HAIndex: 1, ClusterName: "preserve-me"}); err != nil {
		t.Fatal(err)
	}
	envPath := downstreamOutputPathForRun("", "downstream-ha-1.env")
	if err := os.MkdirAll(envPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(envPath, "child"), []byte("blocks directory removal"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := removeDownstreamOutputArtifacts(1); err == nil {
		t.Fatal("expected auxiliary artifact removal failure")
	}
	if _, err := os.Stat(downstreamOutputRecordPath(1)); err != nil {
		t.Fatalf("durable record was removed after partial artifact cleanup failure: %v", err)
	}
}

func TestDownstreamOutputRecordsAreIsolatedByRunID(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("RANCHER_RUNWAY_WORKSPACE", workspace)

	t.Setenv(runIDEnv, "run-a")
	if err := writeDownstreamOutputRecord(downstreamOutputRecord{HAIndex: 1, ClusterName: "cluster-a"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv(runIDEnv, "run-b")
	if err := writeDownstreamOutputRecord(downstreamOutputRecord{HAIndex: 1, ClusterName: "cluster-b"}); err != nil {
		t.Fatal(err)
	}

	recordsA, err := readDownstreamOutputRecordsForRun("run-a")
	if err != nil {
		t.Fatal(err)
	}
	recordsB, err := readDownstreamOutputRecordsForRun("run-b")
	if err != nil {
		t.Fatal(err)
	}
	if len(recordsA) != 1 || recordsA[0].ClusterName != "cluster-a" || recordsA[0].RunID != "run-a" {
		t.Fatalf("unexpected run-a records: %#v", recordsA)
	}
	if len(recordsB) != 1 || recordsB[0].ClusterName != "cluster-b" || recordsB[0].RunID != "run-b" {
		t.Fatalf("unexpected run-b records: %#v", recordsB)
	}
	if downstreamOutputRecordPathForRun("run-a", 1) == downstreamOutputRecordPathForRun("run-b", 1) {
		t.Fatal("same HA index in different runs resolved to the same record path")
	}
	if err := removeDownstreamOutputArtifactsForRun("run-a", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(downstreamOutputRecordPathForRun("run-b", 1)); err != nil {
		t.Fatalf("cleaning run-a affected run-b: %v", err)
	}
}

func TestCleanupDownstreamOutputRecordsAggregatesFailures(t *testing.T) {
	records := []downstreamOutputRecord{
		{HAIndex: 1, ClusterName: "one"},
		{HAIndex: 2, ClusterName: "two"},
	}
	var mu sync.Mutex
	called := map[int]bool{}
	err := cleanupDownstreamOutputRecords(records, time.Second, func(record downstreamOutputRecord, _ time.Duration) error {
		mu.Lock()
		called[record.HAIndex] = true
		mu.Unlock()
		if record.HAIndex == 2 {
			return errors.New("provider deletion failed")
		}
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "HA 2 cluster two") {
		t.Fatalf("expected contextual cleanup failure, got %v", err)
	}
	if !called[1] || !called[2] {
		t.Fatalf("cleanup did not attempt every record: %#v", called)
	}
}

func TestRedactDownstreamProvisioningErrorRemovesTokens(t *testing.T) {
	got := redactDownstreamProvisioningError(errors.New("request token=linode-secret bearer=admin-secret failed"), "linode-secret", "admin-secret")
	if strings.Contains(got, "linode-secret") || strings.Contains(got, "admin-secret") {
		t.Fatalf("secret remained in persisted error: %q", got)
	}
}
