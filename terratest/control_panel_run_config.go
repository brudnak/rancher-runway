package test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/brudnak/ha-rancher-rke2/internal/localtools"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

const runToolConfigPathEnv = "HA_RANCHER_TOOL_CONFIG_PATH"

func (p *localControlPanel) runConfigPath(runID string) string {
	return filepath.Join(filepath.Dir(p.haOutputRootForRun(runID)), "tool-config.yml")
}

// Capture the effective configuration, including values applied by the setup
// editor. Every later phase reads these same values even after another setup.
func (p *localControlPanel) freezeRunConfig(runID string) error {
	viperConfigMu.RLock()
	data, err := yaml.Marshal(viper.AllSettings())
	viperConfigMu.RUnlock()
	if err != nil {
		return fmt.Errorf("encode run configuration: %w", err)
	}
	path := p.runConfigPath(runID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create run configuration directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create run configuration: %w", err)
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil {
		return fmt.Errorf("write run configuration: %w", writeErr)
	}
	return closeErr
}

func (p *localControlPanel) runToolPaths(runID string) map[string]string {
	root := filepath.Join(filepath.Dir(p.runConfigPath(runID)), "tools")
	config := filepath.Join(root, "helm", "config")
	cache := filepath.Join(root, "helm", "cache")
	return map[string]string{
		"HELM_CONFIG_HOME":       config,
		"HELM_CACHE_HOME":        cache,
		"HELM_DATA_HOME":         filepath.Join(root, "helm", "data"),
		"HELM_REPOSITORY_CONFIG": filepath.Join(config, "repositories.yaml"),
		"HELM_REGISTRY_CONFIG":   filepath.Join(config, "registry", "config.json"),
		"HELM_REPOSITORY_CACHE":  filepath.Join(cache, "repository"),
		"HELM_CONTENT_CACHE":     filepath.Join(cache, "content"),
		"TF_PLUGIN_CACHE_DIR":    filepath.Join(root, "terraform", "providers"),
	}
}

// Helm updates repository configuration and indexes during preflight. Give each
// run its own files while retaining existing custom repositories and credentials.
func (p *localControlPanel) prepareRunWorkerTools(runID string) error {
	paths := p.runToolPaths(runID)
	for key, path := range paths {
		if key == "HELM_REPOSITORY_CONFIG" || key == "HELM_REGISTRY_CONFIG" {
			path = filepath.Dir(path)
		}
		if err := os.MkdirAll(path, 0o700); err != nil {
			return fmt.Errorf("prepare run tool directory: %w", err)
		}
	}
	// Reuse the private Helm configuration on subsequent phases.
	marker := filepath.Join(paths["HELM_CONFIG_HOME"], ".initialized")
	if _, err := os.Stat(marker); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	sources, err := currentHelmConfigPaths()
	if err != nil {
		return err
	}
	for _, key := range []string{"HELM_REPOSITORY_CONFIG", "HELM_REGISTRY_CONFIG"} {
		source := sources[key]
		if source == "" {
			continue
		}
		data, err := os.ReadFile(source)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read existing Helm configuration: %w", err)
		}
		if err := writePrivateConfigAtomically(paths[key], data); err != nil {
			return fmt.Errorf("copy run Helm configuration: %w", err)
		}
	}
	return os.WriteFile(marker, nil, 0o600)
}

func currentHelmConfigPaths() (map[string]string, error) {
	paths := map[string]string{
		"HELM_REPOSITORY_CONFIG": strings.TrimSpace(os.Getenv("HELM_REPOSITORY_CONFIG")),
		"HELM_REGISTRY_CONFIG":   strings.TrimSpace(os.Getenv("HELM_REGISTRY_CONFIG")),
	}
	if paths["HELM_REPOSITORY_CONFIG"] != "" && paths["HELM_REGISTRY_CONFIG"] != "" {
		return paths, nil
	}
	helm, err := localtools.Resolve("helm")
	if err != nil {
		// The provisioning preflight reports missing tools before cloud changes.
		return paths, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, helm, "env")
	cmd.Env = localToolEnv(nil)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("read Helm configuration paths: %w", err)
	}
	for _, line := range strings.Split(string(output), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok || (key != "HELM_REPOSITORY_CONFIG" && key != "HELM_REGISTRY_CONFIG") {
			continue
		}
		path, err := strconv.Unquote(value)
		if err != nil {
			return nil, fmt.Errorf("parse Helm configuration path %s: %w", key, err)
		}
		paths[key] = path
	}
	return paths, nil
}

func (p *localControlPanel) panelRunWorkerEnv(env []string, runID string) []string {
	path := p.runConfigPath(runID)
	// Runs created before configuration snapshots continue to use the saved
	// workspace configuration and the existing run-specific overrides.
	record, _ := p.readRunRecord(runID)
	if _, err := os.Stat(path); record.ConfigSnapshot || !os.IsNotExist(err) {
		env = panelEnvWithValue(env, runToolConfigPathEnv, path)
	} else {
		env = panelEnvWithValue(env, runToolConfigPathEnv, "")
	}
	for key, path := range p.runToolPaths(runID) {
		env = panelEnvWithValue(env, key, path)
	}
	return env
}
