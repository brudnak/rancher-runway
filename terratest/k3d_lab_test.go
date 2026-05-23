package test

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestK3DLabRecordsRoundTrip(t *testing.T) {
	withTempWorkingDir(t, func(tempDir string) {
		panel := &localControlPanel{}
		record := k3dLabRecord{
			RunID:       "k3d-abc123",
			Status:      "running",
			K3SVersion:  "v1.33.5-k3s1",
			ClusterName: "rancher-runway-k3d-abc123",
			APIPort:     16443,
			APIURL:      "https://127.0.0.1:16443",
			Kubeconfig:  filepath.Join(tempDir, "kubeconfig.yaml"),
			RunDir:      filepath.Join(tempDir, "run"),
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}
		if err := panel.writeK3DLabRecord(record); err != nil {
			t.Fatalf("failed to write record: %v", err)
		}
		got, ok := panel.readK3DLabRecord(record.RunID)
		if !ok {
			t.Fatal("expected K3D Lab record to be readable")
		}
		if got.APIURL != record.APIURL || got.K3SVersion != record.K3SVersion {
			t.Fatalf("unexpected record round trip: %#v", got)
		}
		if got := panel.listK3DLabRecords(); len(got) != 1 || got[0].RunID != record.RunID {
			t.Fatalf("expected one listed record, got %#v", got)
		}
	})
}

func TestLocalLabPortsIncludeSteveAndK3DRecords(t *testing.T) {
	withTempWorkingDir(t, func(tempDir string) {
		panel := &localControlPanel{}
		now := time.Now()
		if err := panel.writeSteveLabRunRecord(steveLabRunRecord{
			RunID:       "steve-abc123",
			Status:      "serving",
			HTTPPort:    18080,
			HTTPSPort:   18443,
			RunDir:      filepath.Join(tempDir, "steve"),
			CreatedAt:   now,
			UpdatedAt:   now,
			KeepCluster: true,
		}); err != nil {
			t.Fatalf("failed to write Steve Lab record: %v", err)
		}
		if err := panel.writeK3DLabRecord(k3dLabRecord{
			RunID:     "k3d-abc123",
			Status:    "running",
			APIPort:   16443,
			RunDir:    filepath.Join(tempDir, "k3d"),
			CreatedAt: now,
			UpdatedAt: now,
		}); err != nil {
			t.Fatalf("failed to write K3D Lab record: %v", err)
		}

		used := panel.localLabUsedPorts()
		for _, port := range []int{18080, 18443, 16443} {
			if !used[port] {
				t.Fatalf("expected port %d to be reserved, got %#v", port, used)
			}
			if err := panel.ensureLocalLabPortAvailable(port); err == nil {
				t.Fatalf("expected port %d to be rejected as reserved", port)
			}
		}
	})
}

func TestLocalLabKubeconfigDownloadName(t *testing.T) {
	steveName := steveLabKubeconfigDownloadName(steveLabRunRecord{
		RunID:    "steve-abc123",
		SteveRef: "feature/wrangler:test",
	})
	if steveName != "rancher-runway-steve-feature-wrangler-test-steve-abc123-kubeconfig.yaml" {
		t.Fatalf("unexpected Steve kubeconfig filename: %s", steveName)
	}

	k3dName := k3dLabKubeconfigDownloadName(k3dLabRecord{
		RunID:      "k3d-abc123",
		K3SVersion: "v1.33.5-k3s1",
	})
	if k3dName != "rancher-runway-k3d-v1.33.5-k3s1-k3d-abc123-kubeconfig.yaml" {
		t.Fatalf("unexpected K3D kubeconfig filename: %s", k3dName)
	}
}

func TestLocalLabKubeconfigContent(t *testing.T) {
	withTempWorkingDir(t, func(tempDir string) {
		path := filepath.Join(tempDir, "kubeconfig.yaml")
		if err := os.WriteFile(path, []byte("apiVersion: v1\n"), 0o600); err != nil {
			t.Fatalf("failed to write kubeconfig: %v", err)
		}
		content, filename, err := localLabKubeconfigContent(path, "rancher-runway-k3d-test.yaml")
		if err != nil {
			t.Fatalf("expected kubeconfig content: %v", err)
		}
		if string(content) != "apiVersion: v1\n" {
			t.Fatalf("unexpected kubeconfig content: %q", string(content))
		}
		if filename != "rancher-runway-k3d-test.yaml" {
			t.Fatalf("unexpected filename: %s", filename)
		}
	})
}

func withTempWorkingDir(t *testing.T, fn func(tempDir string)) {
	t.Helper()
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
	fn(tempDir)
}
