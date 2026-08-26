package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
)

var clusterDeploymentImageDigestPattern = regexp.MustCompile(`(?i)sha256:[a-f0-9]{64}`)

const clusterDeploymentProbeTimeout = 12 * time.Second

// clusterDeploymentDetailsResponse is assembled only when the user opens a
// cluster's deployment details. Keeping these probes out of the regular state
// refresh avoids adding registry or Kubernetes work to its polling path.
type clusterDeploymentDetailsResponse struct {
	ClusterID           string                   `json:"clusterId"`
	CollectedAt         time.Time                `json:"collectedAt"`
	ConfiguredVersion   string                   `json:"configuredVersion"`
	RancherVersion      string                   `json:"rancherVersion,omitempty"`
	KubernetesVersion   string                   `json:"kubernetesVersion,omitempty"`
	WebhookChartVersion string                   `json:"webhookChartVersion,omitempty"`
	Images              []clusterDeploymentImage `json:"images"`
	Warnings            []string                 `json:"warnings"`
}

type clusterDeploymentImage struct {
	Role             string `json:"role"`
	Namespace        string `json:"namespace,omitempty"`
	Pod              string `json:"pod,omitempty"`
	Container        string `json:"container,omitempty"`
	DeclaredImage    string `json:"declaredImage,omitempty"`
	ImageID          string `json:"imageId,omitempty"`
	Digest           string `json:"digest,omitempty"`
	InspectReference string `json:"inspectReference,omitempty"`
	Version          string `json:"version,omitempty"`
	Ready            bool   `json:"ready"`
}

func (p *localControlPanel) handleClusterDeploymentDetails(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !p.authorizedReadOnly(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clusterID := strings.TrimSpace(r.URL.Query().Get("cluster"))
	if clusterID == "" {
		http.Error(w, "cluster is required", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), clusterDeploymentProbeTimeout)
	defer cancel()

	cluster, found := p.clusterFromSnapshot(clusterID)
	if !found {
		var err error
		cluster, err = p.clusterByID(clusterID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
	}

	writeJSON(w, p.collectClusterDeploymentDetails(ctx, cluster))
}

// rememberClusterSnapshot keeps the already-discovered state response available
// to detail handlers. The UI requests deployment details from cluster cards that
// were built from this exact snapshot, so resolving them here avoids repeating
// Terraform, Kubernetes, and downstream discovery once per visible card.
func (p *localControlPanel) rememberClusterSnapshot(clusters []clusterView) {
	snapshot := make(map[string]clusterView, len(clusters))
	for _, cluster := range clusters {
		clusterID := strings.TrimSpace(cluster.ID)
		if clusterID == "" {
			continue
		}
		snapshot[clusterID] = cloneClusterDeploymentView(cluster)
	}

	p.mu.Lock()
	p.clusterSnapshot = snapshot
	p.mu.Unlock()
}

func (p *localControlPanel) clusterFromSnapshot(clusterID string) (clusterView, bool) {
	clusterID = strings.TrimSpace(clusterID)
	if clusterID == "" {
		return clusterView{}, false
	}
	p.mu.Lock()
	cluster, found := p.clusterSnapshot[clusterID]
	p.mu.Unlock()
	if !found {
		return clusterView{}, false
	}
	return cloneClusterDeploymentView(cluster), true
}

func cloneClusterDeploymentView(cluster clusterView) clusterView {
	if cluster.Pods == nil {
		return cluster
	}
	cluster.Pods = append([]podView(nil), cluster.Pods...)
	for index := range cluster.Pods {
		cluster.Pods[index].Images = append([]containerImageView(nil), cluster.Pods[index].Images...)
	}
	return cluster
}

func (p *localControlPanel) collectClusterDeploymentDetails(ctx context.Context, cluster clusterView) clusterDeploymentDetailsResponse {
	details := clusterDeploymentDetailsResponse{
		ClusterID:         cluster.ID,
		CollectedAt:       time.Now().UTC(),
		ConfiguredVersion: strings.TrimSpace(cluster.Version),
		Images:            clusterDeploymentImagesFromPods(cluster.Pods),
		Warnings:          []string{},
	}
	if clusterErr := strings.TrimSpace(cluster.Error); clusterErr != "" {
		details.Warnings = append(details.Warnings, clusterDeploymentProbeWarning("Cluster state", fmt.Errorf("%s", clusterErr)))
	}

	if isLinodeDockerCluster(cluster) {
		details.Warnings = append(details.Warnings, "Live Linode Docker image metadata was not collected because the control panel cannot verify the SSH host identity; the configured Rancher version is shown without runtime verification.")
		details.Warnings = normalizedClusterDeploymentWarnings(details.Warnings)
		return details
	}

	kubeconfigPath := strings.TrimSpace(cluster.KubeconfigPath)
	if kubeconfigPath == "" {
		details.Warnings = append(details.Warnings, "Live deployment metadata is unavailable because this cluster has no kubeconfig.")
		details.Warnings = normalizedClusterDeploymentWarnings(details.Warnings)
		return details
	}

	type probeResult struct {
		name  string
		value string
		err   error
	}
	managementCluster := isKubeconfigBackedManagementCluster(cluster)
	probeCount := 1
	if managementCluster {
		probeCount += 2
	}
	results := make(chan probeResult, probeCount)

	go func() {
		output, err := runKubectlContext(ctx, kubeconfigPath, "version", "-o", "json")
		if err == nil {
			var parseErr error
			output, parseErr = parseKubernetesServerVersionJSON([]byte(output))
			err = parseErr
		}
		results <- probeResult{name: "Kubernetes version", value: output, err: err}
	}()
	if managementCluster {
		go func() {
			output, err := runKubectlContext(ctx, kubeconfigPath, "get", "apps.catalog.cattle.io", "rancher-webhook", "-n", "cattle-system", "-o", "json")
			if err == nil {
				var parseErr error
				output, parseErr = parseRancherWebhookChartVersionJSON([]byte(output))
				err = parseErr
			}
			results <- probeResult{name: "Rancher webhook chart version", value: output, err: err}
		}()
		go func() {
			output, err := runKubectlContext(ctx, kubeconfigPath, "get", "settings.management.cattle.io", "server-version", "-o", "json")
			if err == nil {
				var parseErr error
				output, parseErr = parseRancherServerVersionSettingJSON([]byte(output))
				err = parseErr
			}
			results <- probeResult{name: "Rancher server version", value: output, err: err}
		}()
	}

	for i := 0; i < probeCount; i++ {
		result := <-results
		if result.err != nil {
			details.Warnings = append(details.Warnings, clusterDeploymentProbeWarning(result.name, result.err))
			continue
		}
		switch result.name {
		case "Kubernetes version":
			details.KubernetesVersion = result.value
		case "Rancher webhook chart version":
			details.WebhookChartVersion = result.value
		case "Rancher server version":
			details.RancherVersion = result.value
		}
	}

	details.Warnings = normalizedClusterDeploymentWarnings(details.Warnings)
	return details
}

func isLinodeDockerCluster(cluster clusterView) bool {
	return cluster.DeploymentType == deploymentTypeLinodeDocker || cluster.Type == "linode"
}

func isKubeconfigBackedManagementCluster(cluster clusterView) bool {
	return strings.TrimSpace(cluster.KubeconfigPath) != "" && cluster.Type != "downstream" && !isLinodeDockerCluster(cluster)
}

func parseRancherServerVersionSettingJSON(data []byte) (string, error) {
	var setting struct {
		Value   string `json:"value"`
		Default string `json:"default"`
		Spec    struct {
			Value   string `json:"value"`
			Default string `json:"default"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(data, &setting); err != nil {
		return "", fmt.Errorf("failed to parse server-version Setting: %w", err)
	}
	for _, value := range []string{setting.Value, setting.Default, setting.Spec.Value, setting.Spec.Default} {
		if value = strings.TrimSpace(value); value != "" {
			return value, nil
		}
	}
	return "", fmt.Errorf("server-version Setting value is empty")
}

func parseKubernetesServerVersionJSON(data []byte) (string, error) {
	var version struct {
		ServerVersion struct {
			GitVersion string `json:"gitVersion"`
			Major      string `json:"major"`
			Minor      string `json:"minor"`
		} `json:"serverVersion"`
	}
	if err := json.Unmarshal(data, &version); err != nil {
		return "", fmt.Errorf("failed to parse kubectl version output: %w", err)
	}
	if value := strings.TrimSpace(version.ServerVersion.GitVersion); value != "" {
		return value, nil
	}
	major := strings.TrimSpace(version.ServerVersion.Major)
	minor := strings.TrimSpace(version.ServerVersion.Minor)
	if major != "" && minor != "" {
		return "v" + major + "." + minor, nil
	}
	return "", fmt.Errorf("kubectl version output has no server version")
}

func parseRancherWebhookChartVersionJSON(data []byte) (string, error) {
	var app struct {
		Spec struct {
			Chart struct {
				Metadata struct {
					Version string `json:"version"`
				} `json:"metadata"`
			} `json:"chart"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(data, &app); err != nil {
		return "", fmt.Errorf("failed to parse rancher-webhook App: %w", err)
	}
	if version := strings.TrimSpace(app.Spec.Chart.Metadata.Version); version != "" {
		return version, nil
	}
	return "", fmt.Errorf("rancher-webhook App spec.chart.metadata.version is empty")
}

func clusterDeploymentImagesFromPods(pods []podView) []clusterDeploymentImage {
	imagesByKey := make(map[string]clusterDeploymentImage)
	for _, pod := range pods {
		for _, runtimeImage := range pod.Images {
			declaredImage := strings.TrimSpace(runtimeImage.Image)
			imageID := strings.TrimSpace(runtimeImage.ImageID)
			inspectReference, digest := clusterDeploymentInspectReference(declaredImage, imageID)
			image := clusterDeploymentImage{
				Role:             clusterDeploymentImageRole(pod.Name, runtimeImage.Name, declaredImage),
				Namespace:        strings.TrimSpace(pod.Namespace),
				Pod:              strings.TrimSpace(pod.Name),
				Container:        strings.TrimSpace(runtimeImage.Name),
				DeclaredImage:    declaredImage,
				ImageID:          imageID,
				Digest:           digest,
				InspectReference: inspectReference,
				Version:          clusterDeploymentImageVersion(declaredImage),
				Ready:            runtimeImage.Ready,
			}

			// Pod names are intentionally omitted so replicas collapse. The exact
			// inspect reference remains in the key, preserving distinct digests
			// while an old and new replica coexist during a rollout.
			identity := image.InspectReference
			if identity == "" {
				identity = image.ImageID
			}
			key := strings.Join([]string{
				image.Role,
				image.Namespace,
				image.Container,
				image.DeclaredImage,
				identity,
			}, "\x00")
			existing, exists := imagesByKey[key]
			if !exists || (!existing.Ready && image.Ready) || (existing.Ready == image.Ready && image.Pod < existing.Pod) {
				imagesByKey[key] = image
			}
		}
	}

	images := make([]clusterDeploymentImage, 0, len(imagesByKey))
	for _, image := range imagesByKey {
		images = append(images, image)
	}
	sortClusterDeploymentImages(images)
	return images
}

func sortClusterDeploymentImages(images []clusterDeploymentImage) {
	sort.Slice(images, func(i, j int) bool {
		left := []string{images[i].Role, images[i].Namespace, images[i].Container, images[i].DeclaredImage, images[i].InspectReference, images[i].Pod}
		right := []string{images[j].Role, images[j].Namespace, images[j].Container, images[j].DeclaredImage, images[j].InspectReference, images[j].Pod}
		return strings.Join(left, "\x00") < strings.Join(right, "\x00")
	})
}

func clusterDeploymentImageRole(podName, containerName, image string) string {
	value := strings.ToLower(strings.Join([]string{podName, containerName, image}, " "))
	switch {
	case strings.Contains(value, "webhook"):
		return "webhook"
	case strings.Contains(value, "rancher-agent"),
		strings.Contains(value, "system-agent"),
		strings.Contains(value, "cattle-cluster-agent"),
		strings.Contains(value, "cattle-node-agent"):
		return "agent"
	case strings.EqualFold(strings.TrimSpace(containerName), "rancher"),
		strings.Contains(value, "/rancher/rancher"):
		return "rancher"
	default:
		return "component"
	}
}

func clusterDeploymentInspectReference(declaredImage, imageID string) (string, string) {
	runtimeDigest := clusterDeploymentImageDigest(imageID)
	if runtimeDigest != "" {
		if repository := clusterDeploymentRepositoryFromRuntimeImageID(imageID); repository != "" {
			return repository + "@" + runtimeDigest, runtimeDigest
		}
		if repository := clusterDeploymentImageRepository(declaredImage); repository != "" {
			return repository + "@" + runtimeDigest, runtimeDigest
		}
		return "", runtimeDigest
	}

	declaredDigest := clusterDeploymentImageDigest(declaredImage)
	if declaredDigest == "" {
		return "", ""
	}
	if repository := clusterDeploymentImageRepository(declaredImage); repository != "" {
		return repository + "@" + declaredDigest, declaredDigest
	}
	return "", declaredDigest
}

func clusterDeploymentImageDigest(reference string) string {
	digest := clusterDeploymentImageDigestPattern.FindString(strings.TrimSpace(reference))
	return strings.ToLower(digest)
}

func clusterDeploymentRepositoryFromRuntimeImageID(imageID string) string {
	cleaned := stripClusterRuntimeImageScheme(imageID)
	if cleaned == "" || strings.EqualFold(cleaned, clusterDeploymentImageDigest(cleaned)) || !strings.Contains(cleaned, "@") {
		return ""
	}
	return clusterDeploymentImageRepository(cleaned)
}

func clusterDeploymentImageRepository(reference string) string {
	cleaned := stripClusterRuntimeImageScheme(reference)
	if cleaned == "" || strings.EqualFold(cleaned, clusterDeploymentImageDigest(cleaned)) {
		return ""
	}
	parsed, err := newImageLookupService().parseReference(cleaned, false)
	if err != nil {
		return ""
	}
	return parsed.registry + "/" + parsed.repository
}

func stripClusterRuntimeImageScheme(reference string) string {
	reference = strings.TrimSpace(reference)
	lower := strings.ToLower(reference)
	for _, prefix := range []string{"docker-pullable://", "containerd://", "docker://", "oci://", "https://", "http://"} {
		if strings.HasPrefix(lower, prefix) {
			return strings.TrimSpace(reference[len(prefix):])
		}
	}
	return reference
}

func clusterDeploymentImageVersion(reference string) string {
	cleaned := stripClusterRuntimeImageScheme(reference)
	if at := strings.LastIndex(cleaned, "@"); at >= 0 {
		cleaned = cleaned[:at]
	}
	lastSlash := strings.LastIndex(cleaned, "/")
	if colon := strings.LastIndex(cleaned, ":"); colon > lastSlash {
		return strings.TrimSpace(cleaned[colon+1:])
	}
	return ""
}

func clusterDeploymentProbeWarning(name string, err error) string {
	message := strings.TrimSpace(err.Error())
	if len(message) > 360 {
		message = message[:360] + "..."
	}
	return strings.TrimSpace(name) + " unavailable: " + message
}

func normalizedClusterDeploymentWarnings(warnings []string) []string {
	seen := make(map[string]struct{}, len(warnings))
	result := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		warning = strings.TrimSpace(warning)
		if warning == "" {
			continue
		}
		if _, exists := seen[warning]; exists {
			continue
		}
		seen[warning] = struct{}{}
		result = append(result, warning)
	}
	sort.Strings(result)
	return result
}
