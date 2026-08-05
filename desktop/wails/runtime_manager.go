package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

const (
	packagedRuntimeOverrideEnv = "RANCHER_RUNWAY_PACKAGED_RUNTIME"
	lifecycleBinaryEnv         = "RANCHER_RUNWAY_LIFECYCLE_BIN"
	managedWorkspaceEnv        = "RANCHER_RUNWAY_WORKSPACE"
	runtimeVersionFilename     = ".rancher-runway-runtime-version"
	currentRuntimeFilename     = "current-runtime"
	runtimeInstallLockFilename = ".runtime-install.lock"
	runtimeManifestFilename    = "runtime.sha256"
	workspaceDirectoryName     = "workspace"
	previousWorkspaceName      = "workspace-previous"
)

var (
	safeRuntimeVersionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	safeRuntimeHashPattern    = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type persistedLifecycleOperation struct {
	Running bool `json:"running"`
	PID     int  `json:"pid"`
}

type managedDesktopRuntime struct {
	WorkspaceRoot string
	RuntimeRoot   string
	Version       string
}

func resolvePackagedDesktopRuntime() (string, bool, error) {
	packagedRoot, ok, err := discoverPackagedRuntimeRoot()
	if err != nil || !ok {
		return "", ok, err
	}

	configRoot, err := os.UserConfigDir()
	if err != nil {
		return "", true, fmt.Errorf("determine user application support directory: %w", err)
	}
	managedRoot := filepath.Join(configRoot, "Rancher Runway")
	installation, err := installManagedRuntime(packagedRoot, managedRoot)
	if err != nil {
		return "", true, err
	}

	helper := packagedLifecycleBinaryPath(installation.RuntimeRoot)
	if helper == "" {
		return "", true, fmt.Errorf("managed runtime %s is missing its lifecycle worker", installation.Version)
	}
	if err := os.Setenv(lifecycleBinaryEnv, helper); err != nil {
		return "", true, fmt.Errorf("configure packaged lifecycle worker: %w", err)
	}
	if err := os.Setenv(managedWorkspaceEnv, filepath.Join(installation.WorkspaceRoot, "terratest")); err != nil {
		return "", true, fmt.Errorf("configure packaged workspace: %w", err)
	}
	return installation.WorkspaceRoot, true, nil
}

func discoverPackagedRuntimeRoot() (string, bool, error) {
	executable, err := os.Executable()
	if err == nil {
		root := filepath.Clean(filepath.Join(filepath.Dir(executable), "..", "Resources", "runtime"))
		if _, statErr := os.Stat(root); statErr == nil {
			if err := validatePackagedRuntime(root); err != nil {
				return "", true, err
			}
			return root, true, nil
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return "", true, fmt.Errorf("inspect packaged runtime %s: %w", root, statErr)
		}
	}

	if override := strings.TrimSpace(os.Getenv(packagedRuntimeOverrideEnv)); override != "" {
		if err := validatePackagedRuntime(override); err != nil {
			return "", true, err
		}
		return filepath.Clean(override), true, nil
	}
	return "", false, nil
}

func packagedLifecycleBinaryPath(packagedRoot string) string {
	helper := filepath.Join(packagedRoot, "bin", "rancher-runway-lifecycle")
	info, err := os.Stat(helper)
	if err != nil || info.IsDir() || info.Mode().Perm()&0o111 == 0 {
		return ""
	}
	abs, err := filepath.Abs(helper)
	if err != nil {
		return ""
	}
	return abs
}

func validatePackagedRuntime(root string) error {
	info, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("inspect packaged runtime %s: %w", root, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("packaged runtime is not a directory: %s", root)
	}
	version, err := packagedRuntimeVersion(root)
	if err != nil {
		return err
	}
	if !safeRuntimeVersionPattern.MatchString(version) {
		return fmt.Errorf("packaged runtime version %q is not safe", version)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return fmt.Errorf("packaged runtime is missing go.mod: %w", err)
	}
	if info, err := os.Stat(filepath.Join(root, "terratest")); err != nil || !info.IsDir() {
		return fmt.Errorf("packaged runtime is missing terratest directory")
	}
	for _, directory := range []string{
		filepath.Join("modules", "aws"),
		filepath.Join("modules", "linode-docker-cattle"),
	} {
		if info, err := os.Stat(filepath.Join(root, directory)); err != nil || !info.IsDir() {
			return fmt.Errorf("packaged runtime is missing %s directory", directory)
		}
	}
	if packagedLifecycleBinaryPath(root) == "" {
		return fmt.Errorf("packaged runtime is missing executable bin/rancher-runway-lifecycle")
	}
	if err := verifyRuntimeManifest(root); err != nil {
		return err
	}
	return nil
}

func verifyRuntimeManifest(root string) error {
	manifestPath := filepath.Join(root, runtimeManifestFilename)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read runtime SHA-256 manifest: %w", err)
	}
	expectedFiles := map[string]string{}
	for lineNumber, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "  ", 2)
		if len(parts) != 2 || !safeRuntimeHashPattern.MatchString(parts[0]) {
			return fmt.Errorf("runtime SHA-256 manifest line %d is invalid", lineNumber+1)
		}
		rel := filepath.Clean(filepath.FromSlash(strings.TrimSpace(parts[1])))
		if rel == "." || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == runtimeManifestFilename {
			return fmt.Errorf("runtime SHA-256 manifest line %d has unsafe path %q", lineNumber+1, parts[1])
		}
		if _, duplicate := expectedFiles[rel]; duplicate {
			return fmt.Errorf("runtime SHA-256 manifest contains duplicate path %s", rel)
		}
		expectedFiles[rel] = parts[0]
	}

	for _, required := range []string{
		runtimeVersionFilename,
		"go.mod",
		filepath.Join("bin", "rancher-runway-lifecycle"),
	} {
		if _, ok := expectedFiles[required]; !ok {
			return fmt.Errorf("runtime SHA-256 manifest is missing %s", required)
		}
	}

	for rel, expected := range expectedFiles {
		path := filepath.Join(root, rel)
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("verify runtime file %s: %w", rel, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("runtime manifest entry is not a regular file: %s", rel)
		}
		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open runtime file %s: %w", rel, err)
		}
		hash := sha256.New()
		_, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if copyErr != nil {
			return fmt.Errorf("hash runtime file %s: %w", rel, copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close runtime file %s: %w", rel, closeErr)
		}
		if actual := fmt.Sprintf("%x", hash.Sum(nil)); actual != expected {
			return fmt.Errorf("runtime file %s failed SHA-256 verification", rel)
		}
	}

	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("runtime contains untrusted symlink %s", rel)
		}
		if entry.IsDir() || rel == runtimeManifestFilename {
			return nil
		}
		if _, ok := expectedFiles[rel]; !ok {
			return fmt.Errorf("runtime contains file absent from SHA-256 manifest: %s", rel)
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

func packagedRuntimeVersion(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, runtimeVersionFilename))
	if err != nil {
		return "", fmt.Errorf("read packaged runtime version: %w", err)
	}
	version := strings.TrimSpace(string(data))
	if version == "" {
		return "", fmt.Errorf("packaged runtime version is empty")
	}
	return version, nil
}

func installManagedRuntime(packagedRoot, managedRoot string) (managedDesktopRuntime, error) {
	if err := validatePackagedRuntime(packagedRoot); err != nil {
		return managedDesktopRuntime{}, err
	}
	version, _ := packagedRuntimeVersion(packagedRoot)
	if err := os.MkdirAll(managedRoot, 0o700); err != nil {
		return managedDesktopRuntime{}, fmt.Errorf("create managed application directory: %w", err)
	}

	lock, err := acquireRuntimeInstallLock(managedRoot)
	if err != nil {
		return managedDesktopRuntime{}, err
	}
	defer releaseRuntimeInstallLock(lock)

	return installManagedRuntimeLocked(packagedRoot, managedRoot, version)
}

func installManagedRuntimeLocked(packagedRoot, managedRoot, version string) (managedDesktopRuntime, error) {
	runtimesRoot := filepath.Join(managedRoot, "runtime")
	if err := os.MkdirAll(runtimesRoot, 0o700); err != nil {
		return managedDesktopRuntime{}, fmt.Errorf("create managed runtime directory: %w", err)
	}
	targetRuntime := filepath.Join(runtimesRoot, version)
	if !managedRuntimeValid(targetRuntime, version) {
		if err := stageManagedRuntime(packagedRoot, targetRuntime, runtimesRoot); err != nil {
			return managedDesktopRuntime{}, err
		}
	}

	workspaceRoot := filepath.Join(managedRoot, workspaceDirectoryName)
	workspaceVersion := workspaceRuntimeVersion(workspaceRoot)
	workspaceActive := runtimeLifecycleActive(workspaceRoot)
	if workspaceActive && workspaceVersion == "" {
		return managedDesktopRuntime{}, fmt.Errorf("the existing workspace has an active process but no valid runtime version marker; close it before upgrading")
	}
	if workspaceVersion != "" && workspaceVersion != version && workspaceActive {
		selectedRuntime := filepath.Join(runtimesRoot, workspaceVersion)
		if !managedRuntimeValid(selectedRuntime, workspaceVersion) {
			return managedDesktopRuntime{}, fmt.Errorf("workspace %s has an active lifecycle for runtime %s, but its matching worker is unavailable", workspaceRoot, workspaceVersion)
		}
		return managedDesktopRuntime{WorkspaceRoot: workspaceRoot, RuntimeRoot: selectedRuntime, Version: workspaceVersion}, nil
	}
	if workspaceActive && !managedWorkspaceValid(workspaceRoot, workspaceVersion) {
		return managedDesktopRuntime{}, fmt.Errorf("the active Rancher Runway workspace for runtime %s is incomplete", workspaceVersion)
	}

	if workspaceVersion == version && managedWorkspaceValid(workspaceRoot, version) {
		if err := writeFileAtomically(filepath.Join(managedRoot, currentRuntimeFilename), []byte(version+"\n"), 0o600); err != nil {
			return managedDesktopRuntime{}, fmt.Errorf("record current managed runtime: %w", err)
		}
		return managedDesktopRuntime{WorkspaceRoot: workspaceRoot, RuntimeRoot: targetRuntime, Version: version}, nil
	}

	if err := replaceManagedWorkspace(targetRuntime, workspaceRoot, managedRoot); err != nil {
		return managedDesktopRuntime{}, err
	}

	if err := writeFileAtomically(filepath.Join(managedRoot, currentRuntimeFilename), []byte(version+"\n"), 0o600); err != nil {
		return managedDesktopRuntime{}, fmt.Errorf("record current managed runtime: %w", err)
	}
	return managedDesktopRuntime{WorkspaceRoot: workspaceRoot, RuntimeRoot: targetRuntime, Version: version}, nil
}

func managedRuntimeValid(root, version string) bool {
	data, err := os.ReadFile(filepath.Join(root, runtimeVersionFilename))
	if err != nil || strings.TrimSpace(string(data)) != version {
		return false
	}
	info, err := os.Stat(filepath.Join(root, "terratest"))
	return err == nil && info.IsDir() && packagedLifecycleBinaryPath(root) != "" && verifyRuntimeManifest(root) == nil
}

func managedWorkspaceValid(root, version string) bool {
	if workspaceRuntimeVersion(root) != version {
		return false
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return false
	}
	info, err := os.Stat(filepath.Join(root, "terratest"))
	return err == nil && info.IsDir()
}

func workspaceRuntimeVersion(root string) string {
	data, err := os.ReadFile(filepath.Join(root, runtimeVersionFilename))
	if err != nil {
		return ""
	}
	version := strings.TrimSpace(string(data))
	if !safeRuntimeVersionPattern.MatchString(version) {
		return ""
	}
	return version
}

func acquireRuntimeInstallLock(managedRoot string) (*os.File, error) {
	path := filepath.Join(managedRoot, runtimeInstallLockFilename)
	lock, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open runtime installation lock: %w", err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("lock runtime installation: %w", err)
	}
	return lock, nil
}

func releaseRuntimeInstallLock(lock *os.File) {
	if lock == nil {
		return
	}
	_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	_ = lock.Close()
}

func stageManagedRuntime(packagedRoot, targetRoot, runtimesRoot string) error {
	stagingRoot, err := os.MkdirTemp(runtimesRoot, ".runtime-staging-")
	if err != nil {
		return fmt.Errorf("create runtime staging directory: %w", err)
	}
	stagingActive := true
	defer func() {
		if stagingActive {
			_ = makeTreeOwnerWritable(stagingRoot)
			_ = os.RemoveAll(stagingRoot)
		}
	}()

	if err := copyDirectory(packagedRoot, stagingRoot); err != nil {
		return fmt.Errorf("stage packaged runtime: %w", err)
	}
	version, err := packagedRuntimeVersion(packagedRoot)
	if err != nil || !managedRuntimeValid(stagingRoot, version) {
		return fmt.Errorf("staged runtime %s failed validation", version)
	}
	if err := makeRuntimeTreeReadOnly(stagingRoot); err != nil {
		return fmt.Errorf("make managed runtime immutable: %w", err)
	}

	if _, err := os.Lstat(targetRoot); err == nil {
		quarantineRoot, createErr := os.MkdirTemp(runtimesRoot, ".runtime-invalid-")
		if createErr != nil {
			return fmt.Errorf("prepare invalid runtime quarantine: %w", createErr)
		}
		if removeErr := os.Remove(quarantineRoot); removeErr != nil {
			return fmt.Errorf("prepare invalid runtime quarantine: %w", removeErr)
		}
		if renameErr := os.Rename(targetRoot, quarantineRoot); renameErr != nil {
			return fmt.Errorf("quarantine invalid managed runtime: %w", renameErr)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect managed runtime target: %w", err)
	}

	if err := os.Rename(stagingRoot, targetRoot); err != nil {
		return fmt.Errorf("activate managed runtime %s: %w", targetRoot, err)
	}
	stagingActive = false
	return nil
}

func replaceManagedWorkspace(runtimeRoot, workspaceRoot, managedRoot string) error {
	stagingRoot, err := os.MkdirTemp(managedRoot, ".workspace-staging-")
	if err != nil {
		return fmt.Errorf("create workspace staging directory: %w", err)
	}
	stagingActive := true
	defer func() {
		if stagingActive {
			_ = os.RemoveAll(stagingRoot)
		}
	}()

	if err := seedWorkspaceFromRuntime(runtimeRoot, stagingRoot); err != nil {
		return fmt.Errorf("stage runtime workspace assets: %w", err)
	}
	if info, err := os.Stat(workspaceRoot); err == nil && info.IsDir() {
		if err := migrateRuntimeState(workspaceRoot, stagingRoot); err != nil {
			return fmt.Errorf("migrate Rancher Runway workspace state: %w", err)
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect existing Rancher Runway workspace: %w", err)
	}

	version := workspaceRuntimeVersion(stagingRoot)
	if !managedWorkspaceValid(stagingRoot, version) {
		return fmt.Errorf("staged Rancher Runway workspace failed validation")
	}

	previousRoot := filepath.Join(managedRoot, previousWorkspaceName)
	workspaceMoved := false
	if _, err := os.Lstat(workspaceRoot); err == nil {
		if err := os.RemoveAll(previousRoot); err != nil {
			return fmt.Errorf("remove previous workspace backup: %w", err)
		}
		if err := os.Rename(workspaceRoot, previousRoot); err != nil {
			return fmt.Errorf("back up current workspace: %w", err)
		}
		workspaceMoved = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect current workspace: %w", err)
	}

	if err := os.Rename(stagingRoot, workspaceRoot); err != nil {
		if workspaceMoved {
			_ = os.Rename(previousRoot, workspaceRoot)
		}
		return fmt.Errorf("activate Rancher Runway workspace: %w", err)
	}
	stagingActive = false
	return nil
}

func seedWorkspaceFromRuntime(runtimeRoot, workspaceRoot string) error {
	for _, rel := range []string{
		runtimeVersionFilename,
		"go.mod",
		"terratest",
		"modules",
		"bootstrap",
	} {
		source := filepath.Join(runtimeRoot, rel)
		if _, err := os.Lstat(source); errors.Is(err, os.ErrNotExist) {
			if rel == "bootstrap" {
				continue
			}
			return fmt.Errorf("runtime asset %s is missing", rel)
		} else if err != nil {
			return err
		}
		if err := copyPath(source, filepath.Join(workspaceRoot, rel)); err != nil {
			return err
		}
	}
	if err := makeTreeOwnerWritable(workspaceRoot); err != nil {
		return fmt.Errorf("make managed workspace writable: %w", err)
	}
	return nil
}

func runtimeLifecycleActive(runtimeRoot string) bool {
	path := filepath.Join(runtimeRoot, "terratest", "automation-output", "control-panel", "lifecycle-state.json")
	data, err := os.ReadFile(path)
	if err == nil {
		operations := map[string]*persistedLifecycleOperation{}
		if json.Unmarshal(data, &operations) == nil {
			for _, operation := range operations {
				if operation == nil || !operation.Running {
					continue
				}
				if operation.PID > 0 && processIsAlive(operation.PID) {
					return true
				}
			}
		}
	}

	steveRecords := filepath.Join(runtimeRoot, "terratest", "automation-output", "control-panel", "steve-lab", "runs", "*.json")
	paths, _ := filepath.Glob(steveRecords)
	for _, recordPath := range paths {
		var record struct {
			StevePID int `json:"stevePid"`
		}
		data, err := os.ReadFile(recordPath)
		if err == nil && json.Unmarshal(data, &record) == nil && record.StevePID > 0 && processIsAlive(record.StevePID) {
			return true
		}
	}
	return false
}

func processIsAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

func migrateRuntimeState(sourceRoot, targetRoot string) error {
	paths := []string{
		"tool-config.yml",
		filepath.Join("terratest", "automation-output"),
	}
	for _, moduleRoot := range []string{
		filepath.Join("modules", "aws"),
		filepath.Join("modules", "linode-docker-cattle"),
		filepath.Join("bootstrap", "terraform-state"),
	} {
		for _, name := range []string{
			".terraform.lock.hcl", "backend.tf", "backend.env", "terraform.tfvars",
			"terraform.tfstate", "terraform.tfstate.backup", "tfplan",
		} {
			paths = append(paths, filepath.Join(moduleRoot, name))
		}
	}

	legacyOutputs, err := filepath.Glob(filepath.Join(sourceRoot, "terratest", "high-availability-*"))
	if err != nil {
		return err
	}
	for _, path := range legacyOutputs {
		rel, err := filepath.Rel(sourceRoot, path)
		if err == nil {
			paths = append(paths, rel)
		}
	}

	for _, rel := range paths {
		source := filepath.Join(sourceRoot, rel)
		if _, err := os.Lstat(source); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		target := filepath.Join(targetRoot, rel)
		if err := os.RemoveAll(target); err != nil {
			return err
		}
		if err := copyMutablePath(source, target); err != nil {
			return err
		}
		if rel == "tool-config.yml" {
			if err := os.Chmod(target, 0o600); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyMutablePath(source, target string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to migrate symlink %s", source)
	}
	if !info.IsDir() {
		return copyFile(source, target, info.Mode().Perm())
	}
	if err := os.MkdirAll(target, info.Mode().Perm()); err != nil {
		return err
	}
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil || rel == "." {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if entry.IsDir() && entry.Name() == ".terraform" {
			return filepath.SkipDir
		}
		return copyPathEntry(path, filepath.Join(target, rel), entry)
	})
}

func copyDirectory(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		return copyPathEntry(path, filepath.Join(target, rel), entry)
	})
}

func copyPath(source, target string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to copy symlink %s", source)
	}
	if info.IsDir() {
		if err := os.MkdirAll(target, info.Mode().Perm()|0o700); err != nil {
			return err
		}
		return copyDirectory(source, target)
	}
	return copyFile(source, target, info.Mode().Perm())
}

func copyPathEntry(source, target string, entry os.DirEntry) error {
	info, err := entry.Info()
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to copy symlink %s", source)
	}
	if info.IsDir() {
		return os.MkdirAll(target, info.Mode().Perm()|0o700)
	}
	return copyFile(source, target, info.Mode().Perm())
}

func makeRuntimeTreeReadOnly(root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to harden symlink %s", path)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		mode := info.Mode().Perm() &^ 0o222
		if entry.IsDir() {
			mode |= 0o555
		}
		return os.Chmod(path, mode)
	})
}

func makeTreeOwnerWritable(root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to make symlink writable %s", path)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		mode := info.Mode().Perm() | 0o600
		if entry.IsDir() {
			mode |= 0o100
		}
		return os.Chmod(path, mode)
	})
}

func copyFile(source, target string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func writeFileAtomically(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".runtime-pointer-")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}
