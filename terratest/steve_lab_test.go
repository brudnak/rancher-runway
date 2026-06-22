package test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestSteveLabRefDetailsParsesKubernetesModule(t *testing.T) {
	module, version := findSteveKubernetesModule(`module github.com/rancher/steve

require (
	k8s.io/apimachinery v0.33.4
	k8s.io/client-go v0.33.4
)
`)
	if module != "k8s.io/apimachinery" {
		t.Fatalf("expected first Kubernetes module, got %q", module)
	}
	if version != "v0.33.4" {
		t.Fatalf("expected module version v0.33.4, got %q", version)
	}
	if got := kubernetesMinorFromModule(version); got != "33" {
		t.Fatalf("expected Kubernetes minor 33, got %q", got)
	}
	if got := k3sVersionsForMinor("33"); len(got) == 0 || got[0] != "v1.33.5-k3s1" {
		t.Fatalf("expected 1.33 k3s recommendation first, got %#v", got)
	}
}

func TestSteveLabK3SRecommendationSynthesizesNewerMinor(t *testing.T) {
	if got := k3sVersionsForModuleVersion("v0.36.0"); len(got) == 0 || got[0] != "v1.36.0-k3s1" {
		t.Fatalf("expected synthesized 1.36 k3s recommendation first, got %#v", got)
	}
	if got := k3sVersionsForModuleVersion("v0.33.4"); len(got) == 0 || got[0] != "v1.33.5-k3s1" {
		t.Fatalf("expected known 1.33 k3s recommendation first, got %#v", got)
	}
}

func TestSteveLabRunRecordsRoundTrip(t *testing.T) {
	tempDir := t.TempDir()
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to read cwd: %v", err)
	}
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("failed to enter temp dir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Fatalf("failed to restore cwd: %v", err)
		}
	})

	panel := &localControlPanel{}
	record := steveLabRunRecord{
		RunID:       "steve-abc123",
		Status:      "ready",
		SteveRef:    "v0.9.10",
		K3SVersion:  "v1.33.5-k3s1",
		ClusterName: "rancher-runway-steve-abc123",
		Kubeconfig:  filepath.Join(tempDir, "kubeconfig.yaml"),
		RunDir:      filepath.Join(tempDir, "run"),
		SourceDir:   filepath.Join(tempDir, "run", "steve"),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := panel.writeSteveLabRunRecord(record); err != nil {
		t.Fatalf("failed to write record: %v", err)
	}
	got, ok := panel.readSteveLabRunRecord(record.RunID)
	if !ok {
		t.Fatal("expected record to be readable")
	}
	if got.SteveRef != record.SteveRef || got.K3SVersion != record.K3SVersion {
		t.Fatalf("unexpected record round trip: %#v", got)
	}
	if got := panel.listSteveLabRunRecords(); len(got) != 1 || got[0].RunID != record.RunID {
		t.Fatalf("expected one listed record, got %#v", got)
	}
}

func TestSteveLabCleanedRecordsAreHiddenAndDeletable(t *testing.T) {
	tempDir := t.TempDir()
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to read cwd: %v", err)
	}
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("failed to enter temp dir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Fatalf("failed to restore cwd: %v", err)
		}
	})

	panel := &localControlPanel{}
	record := steveLabRunRecord{
		RunID:     "steve-cleaned",
		Status:    "cleaned",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := panel.writeSteveLabRunRecord(record); err != nil {
		t.Fatalf("failed to write cleaned record: %v", err)
	}
	if got := panel.listSteveLabRunRecords(); len(got) != 0 {
		t.Fatalf("expected cleaned records to be hidden, got %#v", got)
	}
	if err := panel.deleteSteveLabRunRecord(record.RunID); err != nil {
		t.Fatalf("failed to delete record: %v", err)
	}
	if _, ok := panel.readSteveLabRunRecord(record.RunID); ok {
		t.Fatal("expected deleted record to be unreadable")
	}
}

func TestSteveLabActiveRunRecordsOnlyIncludeLiveEndpoints(t *testing.T) {
	withTempWorkingDir(t, func(_ string) {
		panel := &localControlPanel{}
		now := time.Now()
		for _, record := range []steveLabRunRecord{
			{RunID: "steve-serving", Status: "serving", CreatedAt: now, UpdatedAt: now},
			{RunID: "steve-stopped", Status: "stopped", CreatedAt: now.Add(-time.Minute), UpdatedAt: now.Add(-time.Minute)},
		} {
			if err := panel.writeSteveLabRunRecord(record); err != nil {
				t.Fatalf("failed to write record %s: %v", record.RunID, err)
			}
		}
		active := panel.activeSteveLabRunRecords()
		if len(active) != 1 || active[0].RunID != "steve-serving" {
			t.Fatalf("expected only serving record to be active, got %#v", active)
		}
	})
}

func TestSteveEndpointArgsAndEnvIncludeMetricsOverrides(t *testing.T) {
	record := &steveLabRunRecord{
		Kubeconfig:                   "/tmp/steve-kubeconfig.yaml",
		HTTPSPort:                    6080,
		SQLCache:                     true,
		EnableMetrics:                true,
		MetricsUpdateIntervalSeconds: 7,
		ExtraEnv:                     []string{"STEVE_EXPERIMENT=enabled"},
		ExtraArgs:                    []string{"--custom-flag", "--custom-value=on"},
	}

	wantArgs := []string{
		"run", "main.go",
		"--kubeconfig", "/tmp/steve-kubeconfig.yaml",
		"--http-listen-port", "0",
		"--https-listen-port", "6080",
		"--sql-cache",
		"--enable-metrics",
		"--metrics-update-interval-seconds=7",
		"--custom-flag",
		"--custom-value=on",
	}
	if got := steveEndpointArgs(record); !reflect.DeepEqual(got, wantArgs) {
		t.Fatalf("unexpected Steve args:\nwant %#v\n got %#v", wantArgs, got)
	}

	wantEnv := []string{
		"CGO_ENABLED=0",
		"KUBECONFIG=/tmp/steve-kubeconfig.yaml",
		"CATTLE_PROMETHEUS_METRICS=true",
		"STEVE_EXPERIMENT=enabled",
	}
	if got := steveEndpointEnv(record); !reflect.DeepEqual(got, wantEnv) {
		t.Fatalf("unexpected Steve env:\nwant %#v\n got %#v", wantEnv, got)
	}
}

func TestSteveEndpointMetricsIntervalDefaultsToFifteenSeconds(t *testing.T) {
	record := &steveLabRunRecord{EnableMetrics: true}
	got := steveEndpointArgs(record)
	if got[len(got)-1] != "--enable-metrics" {
		t.Fatalf("expected default metrics interval to omit interval flag, got %#v", got)
	}

	record.MetricsUpdateIntervalSeconds = 10
	got = steveEndpointArgs(record)
	if !reflect.DeepEqual(got[len(got)-2:], []string{"--enable-metrics", "--metrics-update-interval-seconds=10"}) {
		t.Fatalf("expected custom metrics interval, got %#v", got)
	}
}

func TestSteveLabRuntimeOverridesValidation(t *testing.T) {
	env, err := normalizeSteveLabEnv([]string{"", " CATTLE_PROMETHEUS_METRICS=true ", "INVALID-NAME=true"})
	if err == nil {
		t.Fatalf("expected invalid env var name error, got env %#v", env)
	}
	env, err = normalizeSteveLabEnv([]string{" CATTLE_PROMETHEUS_METRICS=true "})
	if err != nil {
		t.Fatalf("expected env var to validate: %v", err)
	}
	if !reflect.DeepEqual(env, []string{"CATTLE_PROMETHEUS_METRICS=true"}) {
		t.Fatalf("unexpected env normalization: %#v", env)
	}

	args, err := normalizeSteveLabArgs([]string{" --enable-metrics ", "--bad\narg"})
	if err == nil {
		t.Fatalf("expected invalid arg error, got args %#v", args)
	}
	args, err = normalizeSteveLabArgs([]string{" --enable-metrics ", "", "--metrics-update-interval-seconds=5"})
	if err != nil {
		t.Fatalf("expected args to validate: %v", err)
	}
	if !reflect.DeepEqual(args, []string{"--enable-metrics", "--metrics-update-interval-seconds=5"}) {
		t.Fatalf("unexpected args normalization: %#v", args)
	}
}

func TestGitHubTarballTargetStripsTopDirectory(t *testing.T) {
	dest := filepath.Join("tmp", "steve")
	target, ok := githubTarballTarget(dest, "rancher-steve-abc123/pkg/foo.go")
	if !ok {
		t.Fatal("expected archive path to be accepted")
	}
	want := filepath.Join(dest, "pkg", "foo.go")
	if target != want {
		t.Fatalf("expected %s, got %s", want, target)
	}

	if _, ok := githubTarballTarget(dest, "rancher-steve-abc123/../escape"); ok {
		t.Fatal("expected traversal path to be rejected")
	}
}
