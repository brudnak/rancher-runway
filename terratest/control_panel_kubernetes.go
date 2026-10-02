package test

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

func fetchLocalRancherPods(kubeconfigPath string) ([]podView, error) {
	pods, err := fetchPods(kubeconfigPath, "cattle-system")
	if err != nil {
		return nil, err
	}

	filtered := make([]podView, 0, len(pods))
	for _, pod := range pods {
		nameLower := strings.ToLower(pod.Name)
		if !strings.Contains(nameLower, "rancher") && !strings.Contains(nameLower, "webhook") {
			continue
		}
		filtered = append(filtered, pod)
	}
	return filtered, nil
}

func fetchAllPods(kubeconfigPath string) ([]podView, error) {
	return fetchPods(kubeconfigPath, "")
}

func fetchRelevantPods(kubeconfigPath string) ([]podView, error) {
	return fetchLocalRancherPods(kubeconfigPath)
}

func fetchPods(kubeconfigPath, namespace string) ([]podView, error) {
	args := []string{"get", "pods"}
	if namespace == "" {
		args = append(args, "-A")
	} else {
		args = append(args, "-n", namespace)
	}
	args = append(args, "-o", "json")

	output, err := runKubectl(kubeconfigPath, args...)
	if err != nil {
		return nil, err
	}

	var list kubectlPodList
	if err := json.Unmarshal([]byte(output), &list); err != nil {
		return nil, fmt.Errorf("failed to parse pod list: %w", err)
	}

	leaderLabels := discoverLeaderLabels(kubeconfigPath)

	pods := make([]podView, 0)
	for _, item := range list.Items {
		pods = append(pods, podViewFromKubectlPod(item, leaderLabels[item.Metadata.Name]))
	}

	sort.Slice(pods, func(i, j int) bool {
		return pods[i].Name < pods[j].Name
	})

	return pods, nil
}

func podViewFromKubectlPod(item kubectlPod, leaderLabel string) podView {
	totalContainers := len(item.Spec.Containers)
	readyContainers := 0
	restarts := 0
	status := item.Status.Phase
	containerStatusByName := make(map[string]struct {
		Image   string
		ImageID string
		Ready   bool
	}, len(item.Status.ContainerStatuses))
	for _, containerStatus := range item.Status.ContainerStatuses {
		if containerStatus.Ready {
			readyContainers++
		}
		restarts += containerStatus.RestartCount
		if containerStatus.State.Waiting.Reason != "" {
			status = containerStatus.State.Waiting.Reason
		}
		if containerStatus.State.Terminated.Reason != "" {
			status = containerStatus.State.Terminated.Reason
		}
		containerStatusByName[containerStatus.Name] = struct {
			Image   string
			ImageID string
			Ready   bool
		}{
			Image:   strings.TrimSpace(containerStatus.Image),
			ImageID: strings.TrimSpace(containerStatus.ImageID),
			Ready:   containerStatus.Ready,
		}
	}
	if item.Status.Reason != "" {
		status = item.Status.Reason
	}

	containerNames := make([]string, 0, len(item.Spec.Containers))
	images := make([]containerImageView, 0, len(item.Spec.Containers))
	for _, container := range item.Spec.Containers {
		containerNames = append(containerNames, container.Name)
		containerStatus := containerStatusByName[container.Name]
		image := strings.TrimSpace(container.Image)
		if image == "" {
			image = containerStatus.Image
		}
		if !isRancherComponentImage(item.Metadata.Name, container.Name, image) {
			continue
		}
		images = append(images, containerImageView{
			Name:    container.Name,
			Image:   image,
			ImageID: containerStatus.ImageID,
			Ready:   containerStatus.Ready,
		})
	}

	return podView{
		Namespace:   item.Metadata.Namespace,
		Name:        item.Metadata.Name,
		Ready:       fmt.Sprintf("%d/%d", readyContainers, totalContainers),
		Status:      status,
		Restarts:    restarts,
		Age:         humanDurationSince(item.Metadata.CreationTimestamp),
		Node:        item.Spec.NodeName,
		Containers:  strings.Join(containerNames, ", "),
		Images:      images,
		Leader:      leaderLabel != "",
		LeaderLabel: leaderLabel,
	}
}

func isRancherComponentImage(_ string, containerName, image string) bool {
	containerName = strings.ToLower(strings.TrimSpace(containerName))
	if containerName == "rancher" || strings.Contains(containerName, "rancher-webhook") {
		return true
	}

	reference := strings.ToLower(strings.TrimSpace(image))
	if separator := strings.Index(reference, "@"); separator >= 0 {
		reference = reference[:separator]
	}
	lastSlash := strings.LastIndex(reference, "/")
	if separator := strings.LastIndex(reference, ":"); separator > lastSlash {
		reference = reference[:separator]
	}
	if lastSlash = strings.LastIndex(reference, "/"); lastSlash >= 0 {
		reference = reference[lastSlash+1:]
	}
	return reference == "rancher" || reference == "rancher-webhook" || reference == "rancher-agent"
}

func discoverLeaderLabels(kubeconfigPath string) map[string]string {
	leaders := map[string]string{}
	if holder, err := leaseHolderIdentity(kubeconfigPath, "kube-system", "cattle-controllers"); err == nil && holder != "" {
		leaders[holder] = "Leader"
	}
	if holder, err := leaseHolderIdentity(kubeconfigPath, "cattle-system", "rancher-webhook-leader"); err == nil && holder != "" {
		leaders[holder] = "Webhook Leader"
	}
	return leaders
}

func leaseHolderIdentity(kubeconfigPath, namespace, name string) (string, error) {
	output, err := runKubectl(kubeconfigPath, "get", "lease", name, "-n", namespace, "-o", "json")
	if err != nil {
		return "", err
	}

	var lease struct {
		Spec struct {
			HolderIdentity string `json:"holderIdentity"`
		} `json:"spec"`
	}
	if err := json.Unmarshal([]byte(output), &lease); err != nil {
		return "", fmt.Errorf("failed to parse %s/%s lease: %w", namespace, name, err)
	}

	return strings.TrimSpace(lease.Spec.HolderIdentity), nil
}

func humanDurationSince(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	d := time.Since(ts)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func runKubectl(kubeconfigPath string, args ...string) (string, error) {
	return runKubectlContext(context.Background(), kubeconfigPath, args...)
}

func runKubectlContext(ctx context.Context, kubeconfigPath string, args ...string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	// --request-timeout bounds each HTTP request, not discovery retries or
	// credential plugins. Bound the entire process as well.
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "kubectl", append([]string{"--kubeconfig", kubeconfigPath, "--request-timeout=5s"}, args...)...)
	cmd.WaitDelay = time.Second
	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", fmt.Errorf("kubectl %s stopped: %w", strings.Join(args, " "), ctxErr)
		}
		return "", fmt.Errorf("kubectl %s failed: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}
