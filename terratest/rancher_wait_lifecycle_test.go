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
	"strconv"
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

	timeout := durationFromEnv("RANCHER_READY_TIMEOUT", 25*time.Minute)
	initialDelay := durationFromEnv("RANCHER_READY_INITIAL_DELAY", 30*time.Second)
	settleDelay := durationFromEnv("RANCHER_READY_SETTLE_DELAY", 30*time.Second)

	var wg sync.WaitGroup
	errCh := make(chan error, totalHAs)
	for i := 1; i <= totalHAs; i++ {
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

	log.Printf("[ready][linode-docker-%d] Waiting for Rancher to become ready at %s", instanceNum, rancherURL)
	if linodeIP != "" {
		log.Printf("[ready][linode-docker-%d] Linode IP: %s", instanceNum, linodeIP)
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
		if httpReady {
			consecutiveReady++
			log.Printf("[ready][linode-docker-%d] Attempt %d ready check passed (%d/2): http=%s",
				instanceNum, attempt, consecutiveReady, httpSummary)
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
			log.Printf("[ready][linode-docker-%d] Attempt %d not ready yet: http=%s",
				instanceNum, attempt, httpSummary)
		}

		time.Sleep(20 * time.Second)
	}

	return fmt.Errorf("[linode-docker-%d] timed out after %s waiting for Rancher HTTP readiness", instanceNum, timeout)
}

func waitForHAReady(instanceNum int, outputs map[string]string, timeout, initialDelay, settleDelay time.Duration) error {
	haOutputs := getHAOutputs(instanceNum, outputs)
	rancherURL := clickableURL(haOutputs.RancherURL)
	kubeconfigPath := filepath.Join(haInstanceDir(instanceNum), "kube_config.yaml")

	log.Printf("[ready][ha-%d] Waiting for Rancher to become ready at %s", instanceNum, rancherURL)
	log.Printf("[ready][ha-%d] Kubeconfig: %s", instanceNum, kubeconfigPath)
	log.Printf("[ready][ha-%d] Initial delay: %s, timeout: %s", instanceNum, initialDelay, timeout)

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
		podsReady, podsSummary, podsErr := rancherPodsReady(kubeconfigPath)
		if podsErr != nil {
			podsSummary = podsErr.Error()
		}

		if httpReady && podsReady {
			consecutiveReady++
			log.Printf("[ready][ha-%d] Attempt %d ready check passed (%d/2): http=%s pods=%s",
				instanceNum, attempt, consecutiveReady, httpSummary, podsSummary)
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
			log.Printf("[ready][ha-%d] Attempt %d not ready yet: http=%s pods=%s",
				instanceNum, attempt, httpSummary, podsSummary)
		}

		time.Sleep(20 * time.Second)
	}

	logRancherReadinessDiagnostics(kubeconfigPath, instanceNum)
	return fmt.Errorf("[ha-%d] timed out after %s waiting for Rancher readiness", instanceNum, timeout)
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

func rancherPodsReady(kubeconfigPath string) (bool, string, error) {
	if _, err := os.Stat(kubeconfigPath); err != nil {
		return false, "", fmt.Errorf("kubeconfig not available at %s: %w", kubeconfigPath, err)
	}

	pods, err := fetchRelevantPods(kubeconfigPath)
	if err != nil {
		return false, "", err
	}

	ready, summary := summarizeRancherPods(pods)
	return ready, summary, nil
}

func summarizeRancherPods(pods []podView) (bool, string) {
	seenRancher := false
	seenWebhook := false
	notReady := make([]string, 0)
	interesting := 0

	for _, pod := range pods {
		name := strings.ToLower(pod.Name)
		isWebhook := strings.Contains(name, "rancher-webhook")
		isRancher := strings.HasPrefix(name, "rancher-") && !isWebhook
		if !isRancher && !isWebhook {
			continue
		}

		interesting++
		if isWebhook {
			seenWebhook = true
		}
		if isRancher {
			seenRancher = true
		}
		if !podReadyForSignoff(pod) {
			notReady = append(notReady, fmt.Sprintf("%s ready=%s status=%s restarts=%d", pod.Name, pod.Ready, pod.Status, pod.Restarts))
		}
	}

	missing := make([]string, 0, 2)
	if !seenRancher {
		missing = append(missing, "rancher")
	}
	if !seenWebhook {
		missing = append(missing, "rancher-webhook")
	}

	switch {
	case len(missing) > 0:
		summary := fmt.Sprintf("waiting for pod groups: %s (found %d relevant pods)", strings.Join(missing, ", "), interesting)
		if len(notReady) > 0 {
			summary += "; not ready: " + strings.Join(notReady, "; ")
		}
		return false, summary
	case len(notReady) > 0:
		return false, strings.Join(notReady, "; ")
	default:
		return true, fmt.Sprintf("rancher and rancher-webhook pods ready (%d relevant pods)", interesting)
	}
}

func podReadyForSignoff(pod podView) bool {
	parts := strings.Split(pod.Ready, "/")
	if len(parts) != 2 {
		return false
	}
	ready, readyErr := strconv.Atoi(parts[0])
	total, totalErr := strconv.Atoi(parts[1])
	if readyErr != nil || totalErr != nil || total == 0 || ready != total {
		return false
	}
	return strings.EqualFold(pod.Status, "Running")
}

func logRancherReadinessDiagnostics(kubeconfigPath string, instanceNum int) {
	if _, err := os.Stat(kubeconfigPath); err != nil {
		log.Printf("[ready][ha-%d][diagnostics] kubeconfig not available at %s: %v", instanceNum, kubeconfigPath, err)
		return
	}

	log.Printf("[ready][ha-%d][diagnostics] collecting bounded cattle-system diagnostics", instanceNum)
	logKubectlDiagnostic(kubeconfigPath, instanceNum, "cattle-system pods", 120, "get", "pods", "-n", "cattle-system", "-o", "wide")
	logKubectlDiagnostic(kubeconfigPath, instanceNum, "cattle-system deployments", 120, "get", "deployments", "-n", "cattle-system", "-o", "wide")
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
	replacements := []string{
		viper.GetString("rancher.bootstrap_password"),
		os.Getenv("RANCHER_BOOTSTRAP_PASSWORD"),
		os.Getenv("LINODE_TOKEN"),
		os.Getenv("DOCKERHUB_PASSWORD"),
	}
	for _, value := range replacements {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		output = strings.ReplaceAll(output, value, "***")
	}
	return output
}

func lastNonEmptyLines(output string, maxLines int) string {
	output = strings.TrimSpace(output)
	if output == "" || maxLines <= 0 {
		return output
	}
	lines := strings.Split(output, "\n")
	if len(lines) <= maxLines {
		return output
	}
	return strings.Join(lines[len(lines)-maxLines:], "\n")
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
