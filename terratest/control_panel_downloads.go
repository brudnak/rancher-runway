package test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (p *localControlPanel) kubeconfigContentForDownload(cluster clusterView) ([]byte, string, error) {
	filename := strings.TrimSpace(cluster.DownloadName)
	if filename == "" {
		filename = "kubeconfig.yaml"
	}

	switch cluster.Type {
	case "local":
		if cluster.KubeconfigPath == "" {
			return nil, "", fmt.Errorf("local kubeconfig path is unavailable")
		}
		data, err := os.ReadFile(cluster.KubeconfigPath)
		if err != nil {
			return nil, "", fmt.Errorf("failed to read local kubeconfig: %w", err)
		}
		return data, filename, nil
	case "downstream":
		if cluster.ManagementClusterID == "" {
			return nil, "", fmt.Errorf("downstream cluster has no management cluster id yet")
		}
		token, err := p.rancherToken(cluster.HAIndex, cluster.RancherURL)
		if err != nil {
			return nil, "", err
		}
		kubeconfig, err := generateRancherKubeconfig(cluster.RancherURL, token, cluster.ManagementClusterID)
		if err != nil {
			return nil, "", err
		}
		return []byte(kubeconfig), filename, nil
	default:
		return nil, "", fmt.Errorf("unsupported cluster type %q", cluster.Type)
	}
}

func localLabKubeconfigContent(path string, filename string) ([]byte, string, error) {
	if strings.TrimSpace(path) == "" {
		return nil, "", fmt.Errorf("local lab kubeconfig path is unavailable")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read local lab kubeconfig: %w", err)
	}
	return data, safeDownloadFilename(filename), nil
}

func steveLabKubeconfigDownloadName(record steveLabRunRecord) string {
	ref := strings.TrimSpace(record.SteveRef)
	if ref == "" {
		ref = strings.TrimSpace(record.K3SVersion)
	}
	return labKubeconfigDownloadName("steve", ref, record.RunID)
}

func k3dLabKubeconfigDownloadName(record k3dLabRecord) string {
	return labKubeconfigDownloadName("k3d", record.K3SVersion, record.RunID)
}

func labKubeconfigDownloadName(kind string, label string, runID string) string {
	parts := []string{"rancher-runway", safeLabFilenamePart(kind)}
	if label = strings.TrimSpace(label); label != "" {
		parts = append(parts, safeLabFilenamePart(label))
	}
	if runID = strings.TrimSpace(runID); runID != "" {
		parts = append(parts, safeLabFilenamePart(runID))
	}
	parts = append(parts, "kubeconfig")
	return safeDownloadFilename(strings.Join(parts, "-") + ".yaml")
}

func safeLabFilenamePart(value string) string {
	value = strings.TrimSpace(value)
	value = strings.NewReplacer("/", "-", "\\", "-", ":", "-", "@", "-", " ", "-").Replace(value)
	value = strings.Trim(value, ".-")
	if value == "" {
		return "local"
	}
	return value
}

func (p *localControlPanel) helmCommandForCluster(cluster clusterView) (string, error) {
	if cluster.Type != "local" {
		return "", fmt.Errorf("Helm install command is only available for local HA clusters")
	}
	if cluster.KubeconfigPath == "" {
		return "", fmt.Errorf("local kubeconfig path is unavailable")
	}

	installScriptPath := filepath.Join(filepath.Dir(cluster.KubeconfigPath), "install.sh")
	data, err := os.ReadFile(installScriptPath)
	if err != nil {
		return "", fmt.Errorf("failed to read install script: %w", err)
	}

	command, err := extractHelmCommandFromInstallScript(string(data))
	if err != nil {
		return "", fmt.Errorf("failed to extract Helm command from %s: %w", installScriptPath, err)
	}
	return command, nil
}

func extractHelmCommandFromInstallScript(script string) (string, error) {
	lines := strings.Split(script, "\n")
	for start := 0; start < len(lines); start++ {
		if !strings.HasPrefix(strings.TrimSpace(lines[start]), "helm ") {
			continue
		}

		end := start
		for shellLineContinues(lines[end]) && end+1 < len(lines) {
			end++
		}
		candidate := strings.TrimSpace(strings.Join(lines[start:end+1], "\n"))
		fields, err := parseHelmCommandFields(candidate)
		if err != nil {
			start = end
			continue
		}
		invocation, err := manualHelmInvocationFromFields(fields)
		if err == nil && helmInvocationInstalls(invocation) && strings.HasSuffix(invocation.chartRef, "/rancher") {
			return candidate, nil
		}
		start = end
	}

	return "", fmt.Errorf("no Rancher Helm install command found")
}

func helmInvocationInstalls(invocation manualHelmInvocation) bool {
	if invocation.operation == "install" {
		return true
	}
	if invocation.operation != "upgrade" {
		return false
	}
	for _, arg := range invocation.trailingArgs {
		if arg == "--install" {
			return true
		}
	}
	return false
}

func shellLineContinues(line string) bool {
	trimmed := strings.TrimRight(line, " \t\r")
	backslashes := 0
	for i := len(trimmed) - 1; i >= 0 && trimmed[i] == '\\'; i-- {
		backslashes++
	}
	return backslashes%2 == 1
}

func prepareHelmUpgradeCommand(command string) (string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", fmt.Errorf("Helm command is empty")
	}

	fields, err := parseHelmCommandFields(command)
	if err != nil {
		return "", err
	}
	if len(fields) < 2 || fields[0] != "helm" {
		return "", fmt.Errorf("command does not start with helm")
	}
	switch fields[1] {
	case "install":
		return rewriteFirstHelmOperation(command, "helm install ", "helm upgrade --install "), nil
	case "upgrade":
		for _, field := range fields[2:] {
			if field == "--install" {
				return command, nil
			}
		}
		return rewriteFirstHelmOperation(command, "helm upgrade ", "helm upgrade --install "), nil
	default:
		return "", fmt.Errorf("command must use helm install or helm upgrade")
	}
}

func rewriteFirstHelmOperation(command, from, to string) string {
	lines := strings.Split(command, "\n")
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, from) {
			prefix := line[:len(line)-len(trimmed)]
			lines[i] = prefix + strings.Replace(trimmed, from, to, 1)
			return strings.TrimSpace(strings.Join(lines, "\n"))
		}
	}
	return command
}

func saveDownloadFile(filename string, content []byte, perm os.FileMode) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to find home directory: %w", err)
	}

	downloadsDir := filepath.Join(home, "Downloads")
	if err := os.MkdirAll(downloadsDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create Downloads directory: %w", err)
	}

	path := uniqueDownloadPath(downloadsDir, filename)
	if err := os.WriteFile(path, content, perm); err != nil {
		return "", fmt.Errorf("failed to save file to Downloads: %w", err)
	}
	return path, nil
}

func uniqueDownloadPath(dir, filename string) string {
	filename = safeDownloadFilename(filename)
	candidate := filepath.Join(dir, filename)
	if _, err := os.Stat(candidate); os.IsNotExist(err) {
		return candidate
	}

	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)
	for i := 1; ; i++ {
		candidate = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

func safeDownloadFilename(filename string) string {
	filename = filepath.Base(strings.TrimSpace(filename))
	filename = strings.NewReplacer("/", "-", "\\", "-", ":", "-").Replace(filename)
	if filename == "." || filename == string(filepath.Separator) || filename == "" {
		return "kubeconfig.yaml"
	}
	return filename
}
