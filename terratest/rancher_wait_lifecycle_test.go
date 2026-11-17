package test

import (
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/viper"
)

func TestHAWaitReady(t *testing.T) {
	requireExplicitLifecycleTest(t, "TestHAWaitReady")
	setupConfig(t)

	totalHAs := viper.GetInt("total_has")
	if totalHAs < 1 {
		t.Fatal("total_has must be at least 1")
	}

	terraformOptions := getTerraformOptions(t, totalHAs)
	outputs := getTerraformOutputs(t, terraformOptions)
	if len(outputs) == 0 {
		t.Fatal("No outputs received from terraform")
	}
	waitForConfiguredHAsReady(t, totalHAs, outputs)
}

func waitForConfiguredHAsReady(t *testing.T, totalHAs int, outputs map[string]string) {
	t.Helper()
	timeout := durationFromEnv("RANCHER_READY_TIMEOUT", 25*time.Minute)
	initialDelay := durationFromEnv("RANCHER_READY_INITIAL_DELAY", 30*time.Second)
	settleDelay := durationFromEnv("RANCHER_READY_SETTLE_DELAY", 30*time.Second)

	var wg sync.WaitGroup
	errCh := make(chan error, totalHAs)
	for i := 1; i <= totalHAs; i++ {
		if destroyedHA(terraformModuleDir(), i) {
			continue
		}
		instanceNum := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := waitForHAReady(instanceNum, outputs, timeout, initialDelay, settleDelay); err != nil {
				errCh <- err
			}
		}()
	}

	wg.Wait()
	close(errCh)

	var failures []string
	for err := range errCh {
		failures = append(failures, err.Error())
	}
	if len(failures) > 0 {
		t.Fatalf("Rancher readiness failed:\n%s", strings.Join(failures, "\n"))
	}
}

func TestLinodeDockerWaitReady(t *testing.T) {
	requireExplicitLifecycleTest(t, "TestLinodeDockerWaitReady")
	setupConfig(t)
	if !isLinodeDockerDeployment() {
		t.Skip("deployment.type is not linode-docker-cattle")
	}

	totalInstances := configuredRancherInstanceCount()
	if totalInstances < 1 {
		t.Fatal("linode-docker-cattle requires at least one Rancher instance")
	}

	terraformOptions := getTerraformOptions(t, totalInstances)
	outputs := getTerraformOutputs(t, terraformOptions)
	if len(outputs) == 0 {
		t.Fatal("No outputs received from terraform")
	}

	timeout := durationFromEnv("RANCHER_READY_TIMEOUT", 25*time.Minute)
	initialDelay := durationFromEnv("RANCHER_READY_INITIAL_DELAY", 30*time.Second)
	settleDelay := durationFromEnv("RANCHER_READY_SETTLE_DELAY", 30*time.Second)

	var wg sync.WaitGroup
	errCh := make(chan error, totalInstances)
	for i := 1; i <= totalInstances; i++ {
		instanceNum := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := waitForLinodeDockerReady(instanceNum, outputs, timeout, initialDelay, settleDelay); err != nil {
				errCh <- err
			}
		}()
	}

	wg.Wait()
	close(errCh)

	var failures []string
	for err := range errCh {
		failures = append(failures, err.Error())
	}
	if len(failures) > 0 {
		t.Fatalf("Linode Docker Rancher readiness failed:\n%s", strings.Join(failures, "\n"))
	}
}

func waitForLinodeDockerReady(instanceNum int, outputs map[string]string, timeout, initialDelay, settleDelay time.Duration) error {
	rancherURL := clickableURL(outputs[fmt.Sprintf("linode_%d_rancher_url", instanceNum)])
	linodeIP := strings.TrimSpace(outputs[fmt.Sprintf("linode_%d_ip", instanceNum)])
	rootPassword := linodeRootPassword()

	log.Printf("[ready][linode-docker-%d] Waiting for Rancher to become ready at %s", instanceNum, rancherURL)
	if linodeIP != "" {
		log.Printf("[ready][linode-docker-%d] Linode SSH target: root@%s", instanceNum, linodeIP)
	}
	if rootPassword == "" {
		log.Printf("[ready][linode-docker-%d] Linode SSH diagnostics disabled: linode.ssh_root_password is empty", instanceNum)
	}
	log.Printf("[ready][linode-docker-%d] Initial delay: %s, timeout: %s", instanceNum, initialDelay, timeout)

	if initialDelay > 0 {
		time.Sleep(initialDelay)
	}

	client := rancherReadyHTTPClient()
	deadline := time.Now().Add(timeout)
	attempt := 0
	consecutiveReady := 0

	for time.Now().Before(deadline) {
		attempt++
		httpReady, httpSummary := rancherHTTPReady(client, rancherURL)
		dockerSummary := linodeDockerReadinessSummary(instanceNum, linodeIP, rootPassword)
		if httpReady {
			consecutiveReady++
			log.Printf("[ready][linode-docker-%d] Attempt %d ready check passed (%d/2): http=%s docker=%s",
				instanceNum, attempt, consecutiveReady, httpSummary, dockerSummary)
			if consecutiveReady >= 2 {
				if settleDelay > 0 {
					log.Printf("[ready][linode-docker-%d] Rancher is ready; settling for %s before continuing", instanceNum, settleDelay)
					time.Sleep(settleDelay)
				}
				log.Printf("[ready][linode-docker-%d] Rancher readiness confirmed", instanceNum)
				return nil
			}
		} else {
			consecutiveReady = 0
			log.Printf("[ready][linode-docker-%d] Attempt %d not ready yet: http=%s docker=%s",
				instanceNum, attempt, httpSummary, dockerSummary)
			if attempt == 1 || attempt%3 == 0 {
				logLinodeDockerDiagnostic(instanceNum, linodeIP, rootPassword, "recent Rancher Docker logs", 100, linodeDockerLogsCommand(120))
			}
		}

		time.Sleep(20 * time.Second)
	}

	logLinodeDockerReadinessDiagnostics(instanceNum, linodeIP, rootPassword)
	return fmt.Errorf("[linode-docker-%d] timed out after %s waiting for Rancher HTTP readiness", instanceNum, timeout)
}

func linodeDockerReadinessSummary(instanceNum int, linodeIP string, rootPassword string) string {
	output, err := runLinodeDockerSSHCommand(linodeIP, rootPassword, linodeDockerStatusCommand())
	if err != nil {
		return fmt.Sprintf("ssh error: %v", err)
	}
	output = sanitizeKubeDiagnosticOutput(output)
	output = strings.TrimSpace(output)
	if output == "" {
		return "no docker status output"
	}
	log.Printf("[ready][linode-docker-%d][docker] status:\n%s", instanceNum, lastNonEmptyLines(output, 20))
	return dockerStatusSummary(output)
}

func logLinodeDockerReadinessDiagnostics(instanceNum int, linodeIP string, rootPassword string) {
	log.Printf("[ready][linode-docker-%d][diagnostics] collecting Docker diagnostics over SSH", instanceNum)
	logLinodeDockerDiagnostic(instanceNum, linodeIP, rootPassword, "docker ps -a", 120, "docker ps -a --no-trunc || true")
	logLinodeDockerDiagnostic(instanceNum, linodeIP, rootPassword, "docker inspect rancher", 160, linodeDockerInspectCommand())
	logLinodeDockerDiagnostic(instanceNum, linodeIP, rootPassword, "recent Rancher Docker logs", 240, linodeDockerLogsCommand(300))
	logLinodeDockerDiagnostic(instanceNum, linodeIP, rootPassword, "docker service logs", 120, "journalctl -u docker --no-pager -n 120 || true")
}

func logLinodeDockerDiagnostic(instanceNum int, linodeIP string, rootPassword string, title string, maxLines int, command string) {
	output, err := runLinodeDockerSSHCommand(linodeIP, rootPassword, command)
	if err != nil {
		log.Printf("[ready][linode-docker-%d][diagnostics] %s failed: %v", instanceNum, title, err)
		return
	}
	output = sanitizeKubeDiagnosticOutput(output)
	output = lastNonEmptyLines(output, maxLines)
	if output == "" {
		output = "(no output)"
	}
	log.Printf("[ready][linode-docker-%d][diagnostics] %s:\n%s", instanceNum, title, output)
}

func waitForHAReady(instanceNum int, outputs map[string]string, timeout, initialDelay, settleDelay time.Duration) error {
	haOutputs := getHAOutputs(instanceNum, outputs)
	rancherURL := clickableURL(haOutputs.RancherURL)
	kubeconfigPath := filepath.Join(haInstanceDir(instanceNum), "kube_config.yaml")

	log.Printf("[ready][ha-%d] Waiting for Rancher to become ready at %s", instanceNum, rancherURL)
	log.Printf("[ready][ha-%d] Kubeconfig: %s", instanceNum, kubeconfigPath)
	log.Printf("[ready][ha-%d] Initial delay: %s, timeout: %s", instanceNum, initialDelay, timeout)

	if initialDelay > 0 {
		// Rechecking a known broken install should fail before the initial delay.
		if _, _, err := rancherSystemDeploymentsReady(kubeconfigPath); err != nil {
			return fmt.Errorf("[ha-%d] %w", instanceNum, err)
		}
		time.Sleep(initialDelay)
	}

	client := rancherReadyHTTPClient()
	deadline := time.Now().Add(timeout)
	lastSummary := "no readiness observations"
	attempt := 0
	consecutiveReady := 0

	for time.Now().Before(deadline) {
		attempt++

		deploymentsReady, deploymentsSummary, err := rancherSystemDeploymentsReady(kubeconfigPath)
		if err != nil {
			return fmt.Errorf("[ha-%d] %w", instanceNum, err)
		}
		httpReady, httpSummary := rancherHTTPReady(client, rancherURL)
		lastSummary = fmt.Sprintf("http=%s deployments=%s", httpSummary, deploymentsSummary)
		if httpReady && deploymentsReady {
			consecutiveReady++
			log.Printf("[ready][ha-%d] Attempt %d ready check passed (%d/2): %s",
				instanceNum, attempt, consecutiveReady, lastSummary)
			if consecutiveReady >= 2 {
				if settleDelay > 0 {
					log.Printf("[ready][ha-%d] Rancher is ready; settling for %s before continuing", instanceNum, settleDelay)
					time.Sleep(settleDelay)
				}
				log.Printf("[ready][ha-%d] Rancher readiness confirmed", instanceNum)
				return nil
			}
		} else {
			consecutiveReady = 0
			log.Printf("[ready][ha-%d] Attempt %d not ready yet: %s",
				instanceNum, attempt, lastSummary)
		}

		time.Sleep(20 * time.Second)
	}

	logRancherReadinessDiagnostics(kubeconfigPath, instanceNum)
	return fmt.Errorf("[ha-%d] timed out after %s waiting for Rancher readiness: %s", instanceNum, timeout, lastSummary)
}

func TestRancherHTTPReadyRejectsAPIAggregationPlaceholder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("API Aggregation not ready"))
	}))
	defer server.Close()

	ready, summary := rancherHTTPReady(server.Client(), server.URL)
	if ready {
		t.Fatalf("expected API Aggregation placeholder to be not ready, summary=%s", summary)
	}
	if !strings.Contains(summary, "API Aggregation not ready") {
		t.Fatalf("expected summary to mention API Aggregation placeholder, got %s", summary)
	}
}

func TestLinodeDockerDiagnosticCommandsTargetNamedContainer(t *testing.T) {
	if got := linodeDockerStatusCommand(); !strings.Contains(got, "--filter name=^/rancher$") {
		t.Fatalf("expected status command to target named rancher container, got %q", got)
	}
	if got := linodeDockerInspectCommand(); !strings.Contains(got, "docker inspect") || !strings.Contains(got, "rancher") {
		t.Fatalf("expected inspect command to target rancher container, got %q", got)
	}
	if got := linodeDockerLogsCommand(42); !strings.Contains(got, "docker logs --tail=42 rancher") {
		t.Fatalf("expected logs command to tail named rancher container, got %q", got)
	}
	if got := linodeDockerLogSnapshotCommand(42); !strings.Contains(got, "### docker ps -a") || !strings.Contains(got, "docker logs --tail=42 rancher") {
		t.Fatalf("expected snapshot command to include Docker status and logs, got %q", got)
	}
}

func TestDockerStatusSummary(t *testing.T) {
	got := dockerStatusSummary("rancher docker.io/rancher/rancher:v2.14.0 Up 2 minutes\nother line")
	if got != "rancher docker.io/rancher/rancher:v2.14.0 Up 2 minutes" {
		t.Fatalf("unexpected summary %q", got)
	}
	if got := dockerStatusSummary(" \n "); got != "container not listed" {
		t.Fatalf("expected empty output summary, got %q", got)
	}
}

func rancherReadyHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
}

func rancherHTTPReady(client *http.Client, rancherURL string) (bool, string) {
	if rancherURL == "" {
		return false, "missing Rancher URL"
	}

	rootProbe, rootErr := rancherHTTPProbe(client, rancherURL)
	if rootErr != nil {
		return false, fmt.Sprintf("root error: %v", rootErr)
	}
	apiProbe, apiErr := rancherHTTPProbe(client, strings.TrimRight(rancherURL, "/")+"/v3")
	if apiErr != nil {
		return false, fmt.Sprintf("root=%d api error: %v", rootProbe.Status, apiErr)
	}

	if !rancherReadyProbe(rootProbe) {
		return false, fmt.Sprintf("root=%d %s api=%d", rootProbe.Status, rancherProbeNotReadyReason(rootProbe), apiProbe.Status)
	}
	if !rancherReadyProbe(apiProbe) {
		return false, fmt.Sprintf("root=%d api=%d %s", rootProbe.Status, apiProbe.Status, rancherProbeNotReadyReason(apiProbe))
	}
	return true, fmt.Sprintf("root=%d api=%d", rootProbe.Status, apiProbe.Status)
}

type rancherHTTPProbeResult struct {
	Status int
	Body   string
}

func rancherHTTPProbe(client *http.Client, target string) (rancherHTTPProbeResult, error) {
	resp, err := client.Get(target)
	if err != nil {
		return rancherHTTPProbeResult{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return rancherHTTPProbeResult{Status: resp.StatusCode, Body: string(body)}, nil
}

func rancherReadyProbe(probe rancherHTTPProbeResult) bool {
	if rancherProbeAPIAggregationNotReady(probe) {
		return false
	}
	switch probe.Status {
	case http.StatusOK,
		http.StatusMovedPermanently,
		http.StatusFound,
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusNotFound:
		return true
	default:
		return false
	}
}

func rancherProbeAPIAggregationNotReady(probe rancherHTTPProbeResult) bool {
	return strings.Contains(strings.ToLower(probe.Body), "api aggregation not ready")
}

func rancherProbeNotReadyReason(probe rancherHTTPProbeResult) string {
	if rancherProbeAPIAggregationNotReady(probe) {
		return "body=API Aggregation not ready"
	}
	return "not ready"
}

func logRancherReadinessDiagnostics(kubeconfigPath string, instanceNum int) {
	if _, err := os.Stat(kubeconfigPath); err != nil {
		log.Printf("[ready][ha-%d][diagnostics] kubeconfig not available at %s: %v", instanceNum, kubeconfigPath, err)
		return
	}

	log.Printf("[ready][ha-%d][diagnostics] collecting bounded cattle-system diagnostics", instanceNum)
	logKubectlDiagnostic(kubeconfigPath, instanceNum, "cattle-system pods", 120, "get", "pods", "-n", "cattle-system", "-o", "wide")
	logKubectlDiagnostic(kubeconfigPath, instanceNum, "cattle-system deployments", 120, "get", "deployments", "-n", "cattle-system", "-o", "wide")
	logKubectlDiagnostic(kubeconfigPath, instanceNum, "cattle-system chart releases", 40, "get", "apps.catalog.cattle.io", "-n", "cattle-system", "-o",
		"custom-columns=RELEASE:.metadata.name,CHART:.spec.chart.metadata.name,VERSION:.spec.chart.metadata.version,STATUS:.spec.info.status")
	logKubectlDiagnostic(kubeconfigPath, instanceNum, "cattle-system services", 120, "get", "svc,endpoints", "-n", "cattle-system", "-o", "wide")
	logKubectlDiagnostic(kubeconfigPath, instanceNum, "recent cattle-system events", 120, "get", "events", "-n", "cattle-system", "--sort-by=.lastTimestamp")

	pods, err := fetchRelevantPods(kubeconfigPath)
	if err != nil {
		log.Printf("[ready][ha-%d][diagnostics] failed to list Rancher pods: %v", instanceNum, err)
		return
	}
	for _, pod := range pods {
		name := strings.ToLower(pod.Name)
		if !strings.HasPrefix(name, "rancher-") {
			continue
		}
		namespace := strings.TrimSpace(pod.Namespace)
		if namespace == "" {
			namespace = "cattle-system"
		}
		logKubectlDiagnostic(kubeconfigPath, instanceNum, fmt.Sprintf("describe pod %s/%s", namespace, pod.Name), 180, "describe", "pod", pod.Name, "-n", namespace)
		logKubectlDiagnostic(kubeconfigPath, instanceNum, fmt.Sprintf("logs pod %s/%s", namespace, pod.Name), 160, "logs", "pod/"+pod.Name, "-n", namespace, "--all-containers", "--tail=120")
		if pod.Restarts > 0 {
			logKubectlDiagnostic(kubeconfigPath, instanceNum, fmt.Sprintf("previous logs pod %s/%s", namespace, pod.Name), 160, "logs", "pod/"+pod.Name, "-n", namespace, "--all-containers", "--previous", "--tail=120")
		}
	}
}

func logKubectlDiagnostic(kubeconfigPath string, instanceNum int, title string, maxLines int, args ...string) {
	output, err := runKubectlOutput(kubeconfigPath, args...)
	if err != nil {
		log.Printf("[ready][ha-%d][diagnostics] %s failed: %v", instanceNum, title, err)
		return
	}
	output = sanitizeKubeDiagnosticOutput(output)
	output = lastNonEmptyLines(output, maxLines)
	if output == "" {
		output = "(no output)"
	}
	log.Printf("[ready][ha-%d][diagnostics] %s:\n%s", instanceNum, title, output)
}

func sanitizeKubeDiagnosticOutput(output string) string {
	return sanitizeDiagnosticOutput(output)
}

func durationFromEnv(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		log.Printf("Invalid %s=%q, using default %s", name, value, fallback)
		return fallback
	}
	return duration
}
