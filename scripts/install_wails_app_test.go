package scripts

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInstallWailsAppReplacesAndRemovesReadOnlyBundles(t *testing.T) {
	requireBash(t)
	repoRoot := newFakeInstallerRepo(t)
	installDir := filepath.Join(t.TempDir(), "Applications with spaces")
	targetApp := filepath.Join(installDir, "Rancher Runway.app")
	t.Cleanup(func() { makeDirectoriesOwnerWritable(targetApp) })

	for attempt := 1; attempt <= 2; attempt++ {
		runFakeInstaller(t, repoRoot, installDir, "0")
		if _, err := os.Stat(filepath.Join(targetApp, "Contents", "Resources", "runtime", "nested", "runtime.txt")); err != nil {
			t.Fatalf("attempt %d did not install runtime: %v", attempt, err)
		}
		if _, err := os.Stat(filepath.Join(repoRoot, "desktop", "wails", "build", "bin", "Rancher Runway.app")); !os.IsNotExist(err) {
			t.Fatalf("attempt %d left source app behind: %v", attempt, err)
		}
		matches, err := filepath.Glob(filepath.Join(installDir, ".Rancher Runway.app.tmp.*"))
		if err != nil {
			t.Fatalf("glob temporary apps: %v", err)
		}
		if len(matches) != 0 {
			t.Fatalf("attempt %d left temporary apps: %v", attempt, matches)
		}
	}
}

func TestInstallWailsAppCanKeepReadOnlyBuildBundle(t *testing.T) {
	requireBash(t)
	repoRoot := newFakeInstallerRepo(t)
	installDir := filepath.Join(t.TempDir(), "Applications")
	sourceApp := filepath.Join(repoRoot, "desktop", "wails", "build", "bin", "Rancher Runway.app")
	targetApp := filepath.Join(installDir, "Rancher Runway.app")
	t.Cleanup(func() {
		makeDirectoriesOwnerWritable(sourceApp)
		makeDirectoriesOwnerWritable(targetApp)
	})

	runFakeInstaller(t, repoRoot, installDir, "1")
	if _, err := os.Stat(sourceApp); err != nil {
		t.Fatalf("keep-build option did not preserve source app: %v", err)
	}
	if _, err := os.Stat(targetApp); err != nil {
		t.Fatalf("keep-build option did not install target app: %v", err)
	}
}

func TestRemoveAppTreeDoesNotFollowSymlinks(t *testing.T) {
	requireBash(t)
	root := t.TempDir()
	appPath := filepath.Join(root, "Bundle With Spaces.app")
	t.Cleanup(func() { makeDirectoriesOwnerWritable(appPath) })
	runtimeDir := filepath.Join(appPath, "Contents", "Resources", "runtime", "nested")
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatalf("create app runtime: %v", err)
	}
	if err := os.WriteFile(filepath.Join(runtimeDir, "runtime.txt"), []byte("runtime\n"), 0o444); err != nil {
		t.Fatalf("write runtime file: %v", err)
	}

	externalDir := filepath.Join(root, "external")
	if err := os.MkdirAll(externalDir, 0o700); err != nil {
		t.Fatalf("create external directory: %v", err)
	}
	externalFile := filepath.Join(externalDir, "keep.txt")
	if err := os.WriteFile(externalFile, []byte("keep\n"), 0o600); err != nil {
		t.Fatalf("write external file: %v", err)
	}
	if err := os.Symlink(externalDir, filepath.Join(runtimeDir, "external-link")); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlinks unavailable: %v", err)
		}
		t.Fatalf("create internal symlink: %v", err)
	}
	if err := os.Chmod(runtimeDir, 0o555); err != nil {
		t.Fatalf("make nested runtime read-only: %v", err)
	}
	if err := os.Chmod(filepath.Dir(runtimeDir), 0o555); err != nil {
		t.Fatalf("make runtime read-only: %v", err)
	}

	helperPath := filepath.Join("app-bundle-utils.sh")
	if output, err := runRemoveAppHelper(helperPath, appPath); err != nil {
		t.Fatalf("remove read-only app: %v\n%s", err, output)
	}
	if _, err := os.Stat(appPath); !os.IsNotExist(err) {
		t.Fatalf("app bundle still exists: %v", err)
	}
	if data, err := os.ReadFile(externalFile); err != nil || string(data) != "keep\n" {
		t.Fatalf("external symlink target was changed: data=%q err=%v", data, err)
	}
}

func TestRemoveAppTreeRejectsUnsafeTargets(t *testing.T) {
	requireBash(t)
	helperPath := filepath.Join("app-bundle-utils.sh")
	for _, target := range []string{"", "/", filepath.Join(t.TempDir(), "not-an-app")} {
		t.Run(strings.ReplaceAll(target, string(filepath.Separator), "_"), func(t *testing.T) {
			if output, err := runRemoveAppHelper(helperPath, target); err == nil {
				t.Fatalf("unsafe target %q was accepted: %s", target, output)
			}
		})
	}

	external := t.TempDir()
	linkedApp := filepath.Join(t.TempDir(), "Linked.app")
	if err := os.Symlink(external, linkedApp); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlinks unavailable: %v", err)
		}
		t.Fatalf("create app symlink: %v", err)
	}
	if output, err := runRemoveAppHelper(helperPath, linkedApp); err == nil {
		t.Fatalf("app symlink was accepted: %s", output)
	}
	if _, err := os.Stat(external); err != nil {
		t.Fatalf("app symlink target was changed: %v", err)
	}
}

func TestReleasePackagingUsesTemporaryAppBundle(t *testing.T) {
	data, err := os.ReadFile("package-macos-release.sh")
	if err != nil {
		t.Fatalf("read release packaging script: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, `app_path="${temporary_root}/${app_name}.app"`) {
		t.Fatal("release app is not assembled under temporary_root")
	}
	if strings.Contains(text, `app_path="${repo_root}/desktop/wails/build/bin/${app_name}.app"`) {
		t.Fatal("release app still contaminates the shared Wails build directory")
	}
}

func newFakeInstallerRepo(t *testing.T) string {
	t.Helper()
	requireBash(t)
	repoRoot := t.TempDir()
	scriptsDir := filepath.Join(repoRoot, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("create fake scripts directory: %v", err)
	}
	for _, name := range []string{"install-wails-app.sh", "app-bundle-utils.sh"} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(scriptsDir, name), data, 0o755); err != nil {
			t.Fatalf("copy %s: %v", name, err)
		}
	}

	fakeBuild := `#!/usr/bin/env bash
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
app_name="${RANCHER_RUNWAY_APP_NAME:-Rancher Runway}"
runtime_dir="${repo_root}/desktop/wails/build/bin/${app_name}.app/Contents/Resources/runtime"
mkdir -p "${runtime_dir}/nested"
printf 'runtime\n' > "${runtime_dir}/nested/runtime.txt"
chmod 0444 "${runtime_dir}/nested/runtime.txt"
chmod 0555 "${runtime_dir}/nested" "${runtime_dir}"
`
	buildPath := filepath.Join(scriptsDir, "build-wails-app.sh")
	if err := os.WriteFile(buildPath, []byte(fakeBuild), 0o755); err != nil {
		t.Fatalf("write fake build script: %v", err)
	}
	return repoRoot
}

func runFakeInstaller(t *testing.T, repoRoot, installDir, keepBuild string) {
	t.Helper()
	cmd := exec.Command("bash", filepath.Join(repoRoot, "scripts", "install-wails-app.sh"))
	cmd.Env = append(os.Environ(),
		"RANCHER_RUNWAY_APP_NAME=Rancher Runway",
		"RANCHER_RUNWAY_INSTALL_DIR="+installDir,
		"RANCHER_RUNWAY_KEEP_WAILS_BUILD_APP="+keepBuild,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("installer failed: %v\n%s", err, output)
	}
}

func runRemoveAppHelper(helperPath, target string) ([]byte, error) {
	command := `source "$1"; rancher_runway_remove_app_tree "$2"`
	cmd := exec.Command("bash", "-c", command, "remove-app-test", helperPath, target)
	return cmd.CombinedOutput()
}

func requireBash(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is required for installer script tests")
	}
}

func makeDirectoriesOwnerWritable(root string) {
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if !entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		_ = os.Chmod(path, info.Mode().Perm()|0o700)
		return nil
	})
}
