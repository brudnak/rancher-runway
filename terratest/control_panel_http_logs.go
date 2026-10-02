package test

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
)

func (p *localControlPanel) handleLogs(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedReadOnly(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cluster, pod, namespace, container, err := p.logRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	args := []string{"logs", pod, "-n", namespace, "--tail=200"}
	if container != "" {
		args = append(args, "-c", container)
	} else {
		args = append(args, "--all-containers=true")
	}

	output, err := runKubectl(cluster.KubeconfigPath, args...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	writeJSON(w, map[string]string{"text": output})
}

func (p *localControlPanel) handleLogStream(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedReadOnly(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	cluster, pod, namespace, container, err := p.logRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	args := []string{"logs", "-f", pod, "-n", namespace, "--tail=20"}
	if container != "" {
		args = append(args, "-c", container)
	} else {
		args = append(args, "--all-containers=true")
	}

	cmd := exec.CommandContext(r.Context(), "kubectl", append([]string{"--kubeconfig", cluster.KubeconfigPath}, args...)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to open log stream: %v", err), http.StatusInternalServerError)
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to open log stream: %v", err), http.StatusInternalServerError)
		return
	}
	if err := cmd.Start(); err != nil {
		http.Error(w, fmt.Sprintf("failed to start log stream: %v", err), http.StatusBadGateway)
		return
	}
	defer cmd.Wait()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	sendLine := func(eventName, line string) {
		fmt.Fprintf(w, "event: %s\n", eventName)
		fmt.Fprintf(w, "data: %s\n\n", strings.ReplaceAll(line, "\n", "\\n"))
		flusher.Flush()
	}

	stdoutDone := make(chan struct{})
	go func() {
		defer close(stdoutDone)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			sendLine("line", scanner.Text())
		}
	}()

	stderrBytes, _ := io.ReadAll(stderr)
	<-stdoutDone
	if len(stderrBytes) > 0 {
		sendLine("error", string(stderrBytes))
	}
	sendLine("end", "stream closed")
}

func (p *localControlPanel) handleDockerLogs(w http.ResponseWriter, r *http.Request) {
	if !p.authorizedReadOnly(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clusterID := strings.TrimSpace(r.URL.Query().Get("cluster"))
	if clusterID == "" {
		http.Error(w, "cluster is required", http.StatusBadRequest)
		return
	}

	cluster, err := p.clusterByID(clusterID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if cluster.DeploymentType != deploymentTypeLinodeDocker && cluster.Type != "linode" {
		http.Error(w, "Docker logs are only available for Linode Docker installs", http.StatusBadRequest)
		return
	}
	if !cluster.Available {
		http.Error(w, "Linode Docker install is not available yet", http.StatusConflict)
		return
	}

	output, err := runLinodeDockerSSHCommand(cluster.LoadBalancer, linodeRootPassword(), linodeDockerLogSnapshotCommand(220))
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to collect Docker logs over SSH: %v", err), http.StatusBadGateway)
		return
	}
	output = sanitizeDiagnosticOutput(output)
	output = lastNonEmptyLines(output, 420)
	if output == "" {
		output = "(no Docker output)"
	}
	writeJSON(w, map[string]string{"text": output})
}
