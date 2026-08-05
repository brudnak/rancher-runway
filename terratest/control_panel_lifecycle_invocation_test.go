package test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLifecycleInvocationUsesGoInCheckout(t *testing.T) {
	t.Setenv(packagedLifecycleBinaryEnv, "")
	panel := &localControlPanel{repoRoot: "/checkout", testDir: "/checkout/terratest"}
	spec := panelCommandSpec{TestName: "TestHaSetup", Timeout: "90m"}

	got, err := panel.lifecycleInvocation(spec)
	if err != nil {
		t.Fatalf("lifecycleInvocation returned error: %v", err)
	}
	wantArgs := []string{"test", "-v", "-run", "^TestHaSetup$", "-timeout", "90m", "-count=1", "./terratest"}
	if got.path != "go" || got.dir != panel.repoRoot || !reflect.DeepEqual(got.args, wantArgs) {
		t.Fatalf("checkout invocation = %#v, want path=go dir=%q args=%q", got, panel.repoRoot, wantArgs)
	}
	if want := "go test -v -run '^TestHaSetup$' -timeout 90m -count=1 ./terratest"; got.display != want {
		t.Fatalf("display = %q, want %q", got.display, want)
	}
}

func TestLifecycleInvocationUsesPackagedWorker(t *testing.T) {
	worker := filepath.Join(t.TempDir(), "rancher-runway-lifecycle")
	if err := os.WriteFile(worker, []byte("worker"), 0o755); err != nil {
		t.Fatalf("create lifecycle worker: %v", err)
	}
	t.Setenv(packagedLifecycleBinaryEnv, worker)
	panel := &localControlPanel{repoRoot: "/managed/runtime/1.0.0", testDir: "/managed/runtime/1.0.0/terratest"}
	spec := panelCommandSpec{TestName: "TestHAWaitReady", Timeout: "35m"}

	got, err := panel.lifecycleInvocation(spec)
	if err != nil {
		t.Fatalf("lifecycleInvocation returned error: %v", err)
	}
	wantArgs := []string{"-test.v", "-test.run=^TestHAWaitReady$", "-test.timeout=35m", "-test.count=1"}
	if got.path != worker || got.dir != panel.testDir || !reflect.DeepEqual(got.args, wantArgs) {
		t.Fatalf("packaged invocation = %#v, want path=%q dir=%q args=%q", got, worker, panel.testDir, wantArgs)
	}
	if want := "rancher-runway-lifecycle -test.run='^TestHAWaitReady$' -test.timeout=35m"; got.display != want {
		t.Fatalf("display = %q, want %q", got.display, want)
	}
}

func TestLifecycleInvocationRejectsMissingPackagedWorker(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-worker")
	t.Setenv(packagedLifecycleBinaryEnv, missing)
	panel := &localControlPanel{repoRoot: "/managed/runtime/1.0.0", testDir: "/managed/runtime/1.0.0/terratest"}

	_, err := panel.lifecycleInvocation(panelCommandSpec{TestName: "TestHaSetup", Timeout: "90m"})
	if err == nil || !strings.Contains(err.Error(), "packaged lifecycle worker is unavailable") {
		t.Fatalf("error = %v, want missing packaged worker error", err)
	}
}

func TestLifecycleInvocationRejectsNonExecutablePackagedWorker(t *testing.T) {
	worker := filepath.Join(t.TempDir(), "rancher-runway-lifecycle")
	if err := os.WriteFile(worker, []byte("worker"), 0o600); err != nil {
		t.Fatalf("create non-executable lifecycle worker: %v", err)
	}
	t.Setenv(packagedLifecycleBinaryEnv, worker)
	panel := &localControlPanel{repoRoot: "/managed/runtime/1.0.0", testDir: "/managed/runtime/1.0.0/terratest"}

	_, err := panel.lifecycleInvocation(panelCommandSpec{TestName: "TestHaSetup", Timeout: "90m"})
	if err == nil || !strings.Contains(err.Error(), "packaged lifecycle worker is not executable") {
		t.Fatalf("error = %v, want non-executable packaged worker error", err)
	}
}

func TestLifecycleInvocationRequiresWorkerMatchingPackagedWorkspace(t *testing.T) {
	managedRoot := t.TempDir()
	workspaceRoot := filepath.Join(managedRoot, "workspace")
	runtimeRoot := filepath.Join(managedRoot, "runtime", "1.0.0")
	worker := filepath.Join(runtimeRoot, "bin", "rancher-runway-lifecycle")
	for path, content := range map[string]string{
		filepath.Join(workspaceRoot, ".rancher-runway-runtime-version"): "1.0.0\n",
		filepath.Join(runtimeRoot, ".rancher-runway-runtime-version"):   "2.0.0\n",
		worker: "worker",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create runtime path: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			t.Fatalf("write runtime fixture: %v", err)
		}
	}
	t.Setenv(packagedLifecycleBinaryEnv, worker)
	panel := &localControlPanel{repoRoot: workspaceRoot, testDir: filepath.Join(workspaceRoot, "terratest")}

	_, err := panel.lifecycleInvocation(panelCommandSpec{TestName: "TestHaSetup", Timeout: "90m"})
	if err == nil || !strings.Contains(err.Error(), "does not match workspace runtime") {
		t.Fatalf("error = %v, want mismatched runtime worker error", err)
	}
}

func TestLifecycleInvocationFailsClosedWhenPackagedWorkerIsUnset(t *testing.T) {
	workspaceRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspaceRoot, ".rancher-runway-runtime-version"), []byte("1.0.0\n"), 0o600); err != nil {
		t.Fatalf("write workspace marker: %v", err)
	}
	t.Setenv(packagedLifecycleBinaryEnv, "")
	panel := &localControlPanel{repoRoot: workspaceRoot, testDir: filepath.Join(workspaceRoot, "terratest")}

	_, err := panel.lifecycleInvocation(panelCommandSpec{TestName: "TestHaSetup", Timeout: "90m"})
	if err == nil || !strings.Contains(err.Error(), "missing its lifecycle worker") {
		t.Fatalf("error = %v, want missing packaged worker error", err)
	}
}
