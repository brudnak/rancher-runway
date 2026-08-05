package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestInstallManagedRuntimeSwitchesVersionsAndPreservesMutableState(t *testing.T) {
	workspace := runtimeTestWorkspace(t)
	managedRoot := filepath.Join(workspace, "managed")
	packagedV1 := createPackagedRuntimeForTest(t, filepath.Join(workspace, "package-v1"), "1.0.0", "version one\n")

	installationV1, err := installManagedRuntime(packagedV1, managedRoot)
	if err != nil {
		t.Fatalf("install v1 runtime: %v", err)
	}
	if want := filepath.Join(managedRoot, "runtime", "1.0.0"); installationV1.RuntimeRoot != want {
		t.Fatalf("v1 immutable runtime = %q, want %q", installationV1.RuntimeRoot, want)
	}
	if want := filepath.Join(managedRoot, workspaceDirectoryName); installationV1.WorkspaceRoot != want {
		t.Fatalf("v1 workspace = %q, want %q", installationV1.WorkspaceRoot, want)
	}

	workspaceRoot := installationV1.WorkspaceRoot
	writeRuntimeTestFile(t, filepath.Join(workspaceRoot, "tool-config.yml"), "saved config\n")
	writeRuntimeTestFile(t, filepath.Join(workspaceRoot, "terratest", "automation-output", "control-panel", "run.json"), "saved run\n")
	writeRuntimeTestFile(t, filepath.Join(workspaceRoot, "modules", "aws", "terraform.tfstate"), "saved state\n")
	writeRuntimeTestFile(t, filepath.Join(workspaceRoot, "local-only.txt"), "do not migrate\n")

	packagedV2 := createPackagedRuntimeForTest(t, filepath.Join(workspace, "package-v2"), "2.0.0", "version two\n")
	installationV2, err := installManagedRuntime(packagedV2, managedRoot)
	if err != nil {
		t.Fatalf("install v2 runtime: %v", err)
	}
	if want := filepath.Join(managedRoot, "runtime", "2.0.0"); installationV2.RuntimeRoot != want {
		t.Fatalf("v2 immutable runtime = %q, want %q", installationV2.RuntimeRoot, want)
	}
	if installationV2.WorkspaceRoot != workspaceRoot {
		t.Fatalf("workspace moved during upgrade: got %q, want %q", installationV2.WorkspaceRoot, workspaceRoot)
	}

	assertRuntimeTestFile(t, filepath.Join(workspaceRoot, "terratest", "runtime-source.txt"), "version two\n")
	assertRuntimeTestFile(t, filepath.Join(workspaceRoot, "tool-config.yml"), "saved config\n")
	assertRuntimeTestFile(t, filepath.Join(workspaceRoot, "terratest", "automation-output", "control-panel", "run.json"), "saved run\n")
	assertRuntimeTestFile(t, filepath.Join(workspaceRoot, "modules", "aws", "terraform.tfstate"), "saved state\n")
	if _, err := os.Stat(filepath.Join(workspaceRoot, "local-only.txt")); !os.IsNotExist(err) {
		t.Fatalf("non-mutable runtime file was migrated; stat error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(installationV1.RuntimeRoot, "tool-config.yml")); !os.IsNotExist(err) {
		t.Fatalf("mutable state leaked into immutable v1 runtime; stat error = %v", err)
	}
	assertRuntimeTestFile(t, filepath.Join(managedRoot, currentRuntimeFilename), "2.0.0\n")
}

func TestInstallManagedRuntimeDefersVersionSwitchWhileLifecycleIsActive(t *testing.T) {
	workspace := runtimeTestWorkspace(t)
	managedRoot := filepath.Join(workspace, "managed")
	packagedV1 := createPackagedRuntimeForTest(t, filepath.Join(workspace, "package-v1"), "1.0.0", "version one\n")
	installationV1, err := installManagedRuntime(packagedV1, managedRoot)
	if err != nil {
		t.Fatalf("install v1 runtime: %v", err)
	}

	state := `{"setup":{"running":true,"pid":` + strconv.Itoa(os.Getpid()) + `}}`
	writeRuntimeTestFile(t, filepath.Join(installationV1.WorkspaceRoot, "terratest", "automation-output", "control-panel", "lifecycle-state.json"), state)

	packagedV2 := createPackagedRuntimeForTest(t, filepath.Join(workspace, "package-v2"), "2.0.0", "version two\n")
	got, err := installManagedRuntime(packagedV2, managedRoot)
	if err != nil {
		t.Fatalf("install v2 runtime while lifecycle active: %v", err)
	}
	if got.Version != "1.0.0" || got.RuntimeRoot != installationV1.RuntimeRoot || got.WorkspaceRoot != installationV1.WorkspaceRoot {
		t.Fatalf("runtime switched while lifecycle was active: got %#v, want %#v", got, installationV1)
	}
	assertRuntimeTestFile(t, filepath.Join(got.WorkspaceRoot, "terratest", "runtime-source.txt"), "version one\n")
	assertRuntimeTestFile(t, filepath.Join(managedRoot, currentRuntimeFilename), "1.0.0\n")
}

func TestInstallManagedRuntimeRejectsPackagedSymlinks(t *testing.T) {
	workspace := runtimeTestWorkspace(t)
	packaged := createPackagedRuntimeForTest(t, filepath.Join(workspace, "package"), "1.0.0", "version one\n")
	link := filepath.Join(packaged, "linked-runtime-file")
	if err := os.Symlink(filepath.Join(packaged, "terratest", "runtime-source.txt"), link); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}

	_, err := installManagedRuntime(packaged, filepath.Join(workspace, "managed"))
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("install error = %v, want packaged symlink rejection", err)
	}
}

func TestInstallManagedRuntimeRejectsCorruptManifestFile(t *testing.T) {
	workspace := runtimeTestWorkspace(t)
	packaged := createPackagedRuntimeForTest(t, filepath.Join(workspace, "package"), "1.0.0", "version one\n")
	writeRuntimeTestFile(t, filepath.Join(packaged, "modules", "aws", "main.tf"), "tampered after manifest\n")

	_, err := installManagedRuntime(packaged, filepath.Join(workspace, "managed"))
	if err == nil || !strings.Contains(err.Error(), "failed SHA-256 verification") {
		t.Fatalf("install error = %v, want runtime SHA-256 rejection", err)
	}
}

func TestInstallManagedRuntimeCopiesReadOnlyBundleIntoWritableWorkspace(t *testing.T) {
	workspace := runtimeTestWorkspace(t)
	packaged := createPackagedRuntimeForTest(t, filepath.Join(workspace, "package"), "1.0.0", "version one\n")
	if err := makeRuntimeTreeReadOnly(packaged); err != nil {
		t.Fatalf("make packaged runtime read-only: %v", err)
	}
	t.Cleanup(func() { _ = makeTreeOwnerWritable(packaged) })

	installation, err := installManagedRuntime(packaged, filepath.Join(workspace, "managed"))
	if err != nil {
		t.Fatalf("install read-only packaged runtime: %v", err)
	}

	managedFile := filepath.Join(installation.RuntimeRoot, "terratest", "runtime-source.txt")
	managedInfo, err := os.Stat(managedFile)
	if err != nil {
		t.Fatalf("stat managed runtime file: %v", err)
	}
	if managedInfo.Mode().Perm()&0o222 != 0 {
		t.Fatalf("managed runtime file mode = %o, want no write bits", managedInfo.Mode().Perm())
	}
	managedDirectoryInfo, err := os.Stat(filepath.Join(installation.RuntimeRoot, "terratest"))
	if err != nil {
		t.Fatalf("stat managed runtime directory: %v", err)
	}
	if managedDirectoryInfo.Mode().Perm()&0o222 != 0 {
		t.Fatalf("managed runtime directory mode = %o, want no write bits", managedDirectoryInfo.Mode().Perm())
	}

	workspaceFile := filepath.Join(installation.WorkspaceRoot, "terratest", "runtime-source.txt")
	workspaceInfo, err := os.Stat(workspaceFile)
	if err != nil {
		t.Fatalf("stat managed workspace file: %v", err)
	}
	if workspaceInfo.Mode().Perm()&0o200 == 0 {
		t.Fatalf("managed workspace file mode = %o, want owner write bit", workspaceInfo.Mode().Perm())
	}
	writeRuntimeTestFile(t, filepath.Join(installation.WorkspaceRoot, "terratest", "automation-output", "writable.txt"), "ok\n")
	assertRuntimeTestFile(t, filepath.Join(installation.WorkspaceRoot, "terratest", "automation-output", "writable.txt"), "ok\n")
}

func TestMarkerBasedRuntimeIsRecognizedAsRepoRoot(t *testing.T) {
	root := t.TempDir()
	writeRuntimeTestFile(t, filepath.Join(root, "go.mod"), "module github.com/brudnak/ha-rancher-rke2\n")
	writeRuntimeTestFile(t, filepath.Join(root, runtimeVersionFilename), "1.0.0\n")
	if err := os.MkdirAll(filepath.Join(root, "terratest", "nested"), 0o755); err != nil {
		t.Fatalf("create runtime terratest directory: %v", err)
	}

	if !isRepoRoot(root) {
		t.Fatal("marker-based managed runtime was not recognized as a repo root")
	}
	got, err := walkToRepoRoot(filepath.Join(root, "terratest", "nested"))
	if err != nil {
		t.Fatalf("walk to marker-based runtime root: %v", err)
	}
	if got != root {
		t.Fatalf("walkToRepoRoot = %q, want %q", got, root)
	}
}

func createPackagedRuntimeForTest(t *testing.T, root, version, source string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "terratest"), 0o755); err != nil {
		t.Fatalf("create packaged runtime: %v", err)
	}
	writeRuntimeTestFile(t, filepath.Join(root, runtimeVersionFilename), version+"\n")
	writeRuntimeTestFile(t, filepath.Join(root, "go.mod"), "module github.com/brudnak/ha-rancher-rke2\n")
	writeRuntimeTestFile(t, filepath.Join(root, "terratest", "runtime-source.txt"), source)
	writeRuntimeTestFile(t, filepath.Join(root, "modules", "aws", "main.tf"), "# packaged terraform\n")
	writeRuntimeTestFile(t, filepath.Join(root, "modules", "linode-docker-cattle", "main.tf"), "# packaged terraform\n")
	writeRuntimeTestFile(t, filepath.Join(root, "bin", "rancher-runway-lifecycle"), "worker\n")
	if err := os.Chmod(filepath.Join(root, "bin", "rancher-runway-lifecycle"), 0o700); err != nil {
		t.Fatalf("make packaged lifecycle worker executable: %v", err)
	}
	writeRuntimeManifestForTest(t, root)
	return root
}

func writeRuntimeManifestForTest(t *testing.T, root string) {
	t.Helper()
	var paths []string
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() && entry.Name() != runtimeManifestFilename {
			paths = append(paths, path)
		}
		return nil
	}); err != nil {
		t.Fatalf("list packaged runtime files: %v", err)
	}
	sort.Strings(paths)
	var manifest strings.Builder
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read runtime manifest input %s: %v", path, err)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatalf("make runtime path relative: %v", err)
		}
		hash := sha256.Sum256(data)
		fmt.Fprintf(&manifest, "%x  %s\n", hash, filepath.ToSlash(rel))
	}
	writeRuntimeTestFile(t, filepath.Join(root, runtimeManifestFilename), manifest.String())
}

func writeRuntimeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func assertRuntimeTestFile(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if got := string(data); got != want {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}

func runtimeTestWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Cleanup(func() { _ = makeTreeOwnerWritable(root) })
	return root
}
