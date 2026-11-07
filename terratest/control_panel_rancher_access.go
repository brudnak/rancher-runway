package test

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

func (p *localControlPanel) ensureDownstreamKubeconfig(runID, rancherURL, clusterKey, managementClusterID, existingPath string) (string, error) {
	if existingPath != "" {
		if _, err := os.Stat(existingPath); err == nil {
			return existingPath, nil
		}
	}

	p.mu.Lock()
	if path := p.downstreamKubeconfigCache[clusterKey]; path != "" {
		if _, err := os.Stat(path); err == nil {
			p.mu.Unlock()
			return path, nil
		}
	}
	p.mu.Unlock()

	token, err := p.rancherToken(runID, rancherURL)
	if err != nil {
		return "", err
	}
	kubeconfig, err := generateRancherKubeconfig(rancherURL, token, managementClusterID)
	if err != nil {
		return "", err
	}

	cacheDir := filepath.Join(automationOutputDir(), "control-panel")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(cacheDir, clusterKey+".yaml")
	if err := os.WriteFile(path, []byte(kubeconfig), 0o600); err != nil {
		return "", err
	}

	p.mu.Lock()
	p.downstreamKubeconfigCache[clusterKey] = path
	p.mu.Unlock()
	return path, nil
}

func (p *localControlPanel) rancherToken(runID, rancherURL string) (string, error) {
	base, err := clusterWorkspaceURL(rancherURL)
	if err != nil || base == "" {
		return "", fmt.Errorf("Rancher URL is invalid")
	}
	// Run identity also prevents a removed cluster's token from being reused
	// when a later run deliberately reuses the same custom hostname.
	key := runID + "\x00" + base
	p.mu.Lock()
	if token := p.rancherTokens[key]; token != "" {
		p.mu.Unlock()
		return token, nil
	}
	p.mu.Unlock()

	password, err := p.runBootstrapPassword(runID)
	if err != nil {
		return "", err
	}
	token, err := createRancherAdminToken(base, password)
	if err != nil {
		return "", err
	}

	p.mu.Lock()
	if p.rancherTokens == nil {
		p.rancherTokens = make(map[string]string)
	}
	p.rancherTokens[key] = token
	p.mu.Unlock()
	return token, nil
}

func (p *localControlPanel) runBootstrapPassword(runID string) (string, error) {
	if runID != "" {
		path := p.runConfigPath(runID)
		record, _ := p.readRunRecord(runID)
		if _, err := os.Stat(path); record.ConfigSnapshot || !os.IsNotExist(err) {
			config := viper.New()
			config.SetConfigFile(path)
			if err := config.ReadInConfig(); err != nil {
				return "", fmt.Errorf("could not read saved configuration for run %s", runID)
			}
			return config.GetString("rancher.bootstrap_password"), nil
		}
	}
	viperConfigMu.RLock()
	defer viperConfigMu.RUnlock()
	return viper.GetString("rancher.bootstrap_password"), nil
}
