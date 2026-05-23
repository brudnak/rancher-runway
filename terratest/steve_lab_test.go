package test

import (
	"os"
	"path/filepath"
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
