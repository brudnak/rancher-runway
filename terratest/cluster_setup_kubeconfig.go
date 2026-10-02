package test

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

func getAndSaveKubeconfig(serverIP string, haDir string) error {
	rawKubeconfig, err := RunCommand("sudo cat /etc/rancher/rke2/rke2.yaml", serverIP)
	if err != nil {
		return fmt.Errorf("failed to retrieve kubeconfig: %w", err)
	}

	configIP := fmt.Sprintf("https://%s:6443", serverIP)
	modifiedKubeconfig := strings.Replace(rawKubeconfig, "https://127.0.0.1:6443", configIP, -1)

	absHADir, err := absoluteFromWorkingDir(haDir)
	if err != nil {
		return err
	}

	if _, err := os.Stat(absHADir); os.IsNotExist(err) {
		if mkdirErr := os.MkdirAll(absHADir, 0o700); mkdirErr != nil {
			return fmt.Errorf("failed to create directory %s: %w", absHADir, mkdirErr)
		}
		log.Printf("Created directory %s", absHADir)
	}

	absKubeConfigPath := filepath.Join(absHADir, "kube_config.yaml")
	err = os.WriteFile(absKubeConfigPath, []byte(modifiedKubeconfig), 0o600)
	if err != nil {
		return fmt.Errorf("failed to write kubeconfig file: %w", err)
	}

	log.Printf("Kubeconfig saved to %s", absKubeConfigPath)
	return nil
}
