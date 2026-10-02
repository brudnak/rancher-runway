package test

import (
	"github.com/spf13/viper"
	"os"
	"path/filepath"
)

func (p *localControlPanel) ensureDownstreamKubeconfig(haIndex int, rancherURL, clusterKey, managementClusterID, existingPath string) (string, error) {
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

	token, err := p.rancherToken(haIndex, rancherURL)
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

func (p *localControlPanel) rancherToken(haIndex int, rancherURL string) (string, error) {
	p.mu.Lock()
	if token := p.rancherTokens[haIndex]; token != "" {
		p.mu.Unlock()
		return token, nil
	}
	p.mu.Unlock()

	token, err := createRancherAdminToken(rancherURL, viper.GetString("rancher.bootstrap_password"))
	if err != nil {
		return "", err
	}

	p.mu.Lock()
	p.rancherTokens[haIndex] = token
	p.mu.Unlock()
	return token, nil
}
