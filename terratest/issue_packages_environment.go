package test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Each field keeps its provenance. A configured head tag is never represented
// as the exact server version, and refreshing a draft never rewrites a session.
func packageEnvironmentDetail(e *issuePackageEnvironment, label, value, source string, at *time.Time) {
	value = strings.TrimSpace(value)
	if value == "" || !packageText(label, 500, true) || !packageText(value, 8192, true) {
		return
	}
	detail := issuePackageEnvironmentDetail{Label: label, Value: value, Source: source, ObservedAt: at}
	for i := range e.Details {
		if e.Details[i].Label == label {
			e.Details[i] = detail
			return
		}
	}
	if len(e.Details) < 500 {
		e.Details = append(e.Details, detail)
	}
}

func packageEnvironmentImages(e *issuePackageEnvironment, pods []podView, source string, at *time.Time) {
	images := clusterDeploymentImagesFromPods(pods)
	seen := map[string]bool{}
	values := []string{}
	for _, img := range images {
		for _, value := range []string{img.DeclaredImage, img.ImageID} {
			if value != "" && !seen[value] && len(values) < 500 {
				values = append(values, value)
				seen[value] = true
			}
		}
		label := strings.ToUpper(img.Role[:1]) + img.Role[1:] + " image · " + img.Namespace + "/" + img.Pod + "/" + img.Container
		packageEnvironmentDetail(e, label, img.DeclaredImage, source, at)
		if img.Digest != "" {
			packageEnvironmentDetail(e, label+" · digest", img.Digest, source, at)
		}
	}
	if len(values) > 0 {
		sort.Strings(values)
		e.Images = values
	}
}

func (p *localControlPanel) enrichIssuePackageEnvironment(e *issuePackageEnvironment, cluster clusterView, known bool) {
	if known {
		packageEnvironmentDetail(e, "Deployment", cluster.DeploymentType, "recorded", nil)
		packageEnvironmentDetail(e, "Cluster type", cluster.Type, "recorded", nil)
		packageEnvironmentDetail(e, "Cluster role", cluster.Role, "recorded", nil)
		packageEnvironmentDetail(e, "Run", cluster.RunID, "recorded", nil)
		packageEnvironmentDetail(e, "Namespace", cluster.Namespace, "recorded", nil)
		packageEnvironmentDetail(e, "Management cluster", cluster.ManagementClusterID, "recorded", nil)
		if cluster.HAIndex > 0 {
			packageEnvironmentDetail(e, "Run instance", fmt.Sprint(cluster.HAIndex), "recorded", nil)
		}
		packageEnvironmentDetail(e, "GPU worker instance type", cluster.GPUWorkerInstanceType, "recorded", nil)
		packageEnvironmentDetail(e, "GPU worker image", cluster.GPUWorkerAMI, "recorded", nil)
		if cluster.Version != "" {
			packageEnvironmentDetail(e, "Configured version", cluster.Version, "recorded", nil)
		}
		packageEnvironmentImages(e, cluster.Pods, "recorded", nil)
		if cluster.Type == "local" && cluster.KubeconfigPath != "" {
			if raw, err := issuePackageReadBounded(filepath.Join(filepath.Dir(cluster.KubeconfigPath), "install.sh"), 1<<20); err == nil {
				if command, err := extractHelmCommandFromInstallScript(string(raw)); err == nil {
					e.HelmCommand = sanitizeIssuePackageHelmCommand(command)
					if e.HelmCommand != "" {
						packageEnvironmentDetail(e, "Helm command source", "Saved Rancher install script; credentials, hostnames, and local file paths redacted", "recorded", nil)
						if fields, err := parseHelmCommandFields(e.HelmCommand); err == nil {
							for i, field := range fields {
								if field == "--version" && i+1 < len(fields) {
									packageEnvironmentDetail(e, "Rancher chart version (install)", fields[i+1], "recorded", nil)
								}
							}
						}
					}
				}
			}
		}
	}
	p.mu.Lock()
	cached, exists := p.issuePackageEnvironments[e.ClusterID]
	p.mu.Unlock()
	if exists && cached.URL == e.URL {
		if packageEnvironmentHasObservedImages(cached) {
			kept := e.Details[:0]
			for _, detail := range e.Details {
				if !strings.Contains(detail.Label, " image · ") {
					kept = append(kept, detail)
				}
			}
			e.Details = kept
		}
		for _, detail := range cached.Details {
			if detail.Source == "observed" {
				packageEnvironmentDetail(e, detail.Label, detail.Value, detail.Source, detail.ObservedAt)
			}
		}
		for _, detail := range cached.Details {
			switch detail.Label {
			case "Rancher server version":
				e.RancherVersion = detail.Value
			case "Kubernetes server version":
				e.KubernetesVersion = detail.Value
			}
		}
		e.ObservedAt = cached.ObservedAt
		e.Warnings = append([]string(nil), cached.Warnings...)
		// Runtime images belong to the same observation as their digest metadata.
		if cached.ObservedAt != nil && packageEnvironmentHasObservedImages(cached) {
			e.Images = append([]string(nil), cached.Images...)
		}
	} else {
		e.Warnings = []string{"Showing saved Runway details. Refresh from cluster to record exact server, Kubernetes, and webhook versions."}
	}
}

func packageEnvironmentHasObservedImages(e issuePackageEnvironment) bool {
	for _, d := range e.Details {
		if d.Source == "observed" && (strings.Contains(d.Label, " image · ") || d.Label == "Runtime image inventory") {
			return true
		}
	}
	return false
}

// Cluster cards already fetch exact versions on demand. Reuse those server-side
// observations in future session drafts; the card's pod images are only cached
// discovery data, so their freshness is deliberately not upgraded here.
func (p *localControlPanel) rememberIssuePackageDeploymentDetails(details clusterDeploymentDetailsResponse) {
	if details.ClusterID == "" || details.CollectedAt.IsZero() {
		return
	}
	e, err := p.issuePackageEnvironment(issuePackageEnvironment{ClusterID: details.ClusterID})
	if err != nil {
		return
	}
	observed := false
	for _, field := range []struct{ label, value string }{
		{"Rancher server version", details.RancherVersion},
		{"Kubernetes server version", details.KubernetesVersion},
		{"Webhook chart version", details.WebhookChartVersion},
	} {
		if packageText(field.value, 240, true) {
			packageEnvironmentDetail(&e, field.label, field.value, "observed", &details.CollectedAt)
			observed = true
		}
	}
	if !observed {
		return
	}
	e.ObservedAt = &details.CollectedAt
	e.Warnings = nil
	if len(details.Warnings) > 0 {
		e.Warnings = []string{"Some deployment details could not be refreshed. Available observations are preserved with their collection timestamps."}
	}
	p.mu.Lock()
	if p.issuePackageEnvironments == nil {
		p.issuePackageEnvironments = map[string]issuePackageEnvironment{}
	}
	p.issuePackageEnvironments[e.ClusterID] = e
	p.mu.Unlock()
}

func (p *localControlPanel) refreshIssuePackageEnvironment(ctx context.Context, input issuePackageEnvironment) (issuePackageEnvironment, error) {
	e, err := p.issuePackageEnvironment(input)
	if err != nil {
		return e, err
	}
	if e.ClusterID == "" {
		return e, fmt.Errorf("choose a known cluster to refresh its environment")
	}
	records, err := p.labClusterCandidates()
	if err != nil {
		return e, err
	}
	var record clusterWorkspaceRecord
	for _, r := range records {
		if r.ID == e.ClusterID {
			record = r
			break
		}
	}
	if record.ID == "" || record.Archived {
		return e, fmt.Errorf("this cluster is archived or no longer available; its saved environment remains intact")
	}
	if record.Kubeconfig == "" {
		return e, fmt.Errorf("this cluster has no registered kubeconfig; saved details remain available")
	}
	contextName := clusterWorkspaceKubeconfigContext(record.Kubeconfig, record.Context)
	if contextName == "" {
		return e, fmt.Errorf("the registered kubeconfig has no selected context")
	}
	record.Context = contextName
	connection, err := issuePackageReadLogConnection(record)
	if err != nil {
		return e, err
	}
	cluster, _ := p.clusterFromSnapshot(e.ClusterID)
	cluster.ID, cluster.Role, cluster.KubeconfigPath = record.ID, record.Role, record.Kubeconfig
	ctx, cancel := context.WithTimeout(ctx, clusterDeploymentProbeTimeout)
	defer cancel()
	e = probeIssuePackageEnvironment(ctx, e, cluster, contextName, runIssuePackageEnvironmentCommand)
	after, err := issuePackageReadLogConnection(record)
	if err != nil || connection.fingerprint != after.fingerprint {
		return e, fmt.Errorf("the registered kubeconfig changed while collecting metadata; refresh again before preserving it")
	}
	if err := validateIssuePackageEnvironment(e); err != nil {
		return e, err
	}
	p.mu.Lock()
	if p.issuePackageEnvironments == nil {
		p.issuePackageEnvironments = map[string]issuePackageEnvironment{}
	}
	p.issuePackageEnvironments[e.ClusterID] = e
	p.mu.Unlock()
	return e, nil
}

type issuePackageEnvironmentRunner func(context.Context, string, ...string) (string, error)

func runIssuePackageEnvironmentCommand(ctx context.Context, path string, args ...string) (string, error) {
	output := &issuePackageLogBuffer{max: 16 << 20}
	err := runIssuePackageLogCommand(ctx, append([]string{"--kubeconfig", path, "--request-timeout=10s"}, args...), nil, output)
	if err != nil {
		return "", err
	}
	if output.truncated {
		return "", fmt.Errorf("environment metadata exceeds 16 MiB")
	}
	return string(output.Bytes()), nil
}

// Probes are explicit and bounded, use the selected registered context, and
// return only allowlisted metadata. Raw command errors can contain credentials
// or local paths and therefore never become portable warnings.
func probeIssuePackageEnvironment(ctx context.Context, e issuePackageEnvironment, cluster clusterView, contextName string, run issuePackageEnvironmentRunner) issuePackageEnvironment {
	type probe struct {
		label string
		args  []string
		parse func([]byte) (string, error)
	}
	probes := []probe{{"Kubernetes server version", []string{"version", "-o", "json"}, parseKubernetesServerVersionJSON}}
	management := cluster.Type != "downstream" && !packageEnum(cluster.Role, "downstream", "k3d", "steve", "docker") && !isLinodeDockerCluster(cluster)
	if management {
		probes = append(probes,
			probe{"Rancher server version", []string{"get", "settings.management.cattle.io", "server-version", "-o", "json"}, parseRancherServerVersionSettingJSON},
			probe{"Webhook chart version", []string{"get", "apps.catalog.cattle.io", "rancher-webhook", "-n", "cattle-system", "-o", "json"}, parseRancherWebhookChartVersionJSON},
			probe{"Runtime images", []string{"get", "pods", "-n", "cattle-system", "-o", "json"}, nil})
	}
	type result struct {
		label, value string
		pods         []podView
		err          error
	}
	results := make(chan result, len(probes))
	for _, item := range probes {
		go func(item probe) {
			output, err := run(ctx, cluster.KubeconfigPath, append([]string{"--context", contextName}, item.args...)...)
			r := result{label: item.label, err: err}
			if err == nil {
				if len(output) > 16<<20 {
					r.err = fmt.Errorf("metadata response is too large")
				} else if item.parse != nil {
					r.value, r.err = item.parse([]byte(output))
					if !packageText(r.value, 240, true) {
						r.err = fmt.Errorf("invalid version")
					}
				} else {
					var list kubectlPodList
					r.err = json.Unmarshal([]byte(output), &list)
					if len(list.Items) > 500 {
						r.err = fmt.Errorf("too many pods")
					}
					if r.err == nil {
						for _, pod := range list.Items {
							r.pods = append(r.pods, podViewFromKubectlPod(pod, ""))
						}
					}
				}
			}
			results <- r
		}(item)
	}
	e.Warnings = nil
	for range probes {
		r := <-results
		if r.err != nil {
			e.Warnings = append(e.Warnings, r.label+" could not be refreshed. Any earlier observation is retained with its original timestamp; check cluster access and try again.")
			continue
		}
		now := time.Now().UTC()
		e.ObservedAt = &now
		if r.label == "Runtime images" {
			filtered := e.Details[:0]
			for _, d := range e.Details {
				if !strings.Contains(d.Label, " image · ") {
					filtered = append(filtered, d)
				}
			}
			e.Details = filtered
			e.Images = []string{}
			packageEnvironmentImages(&e, r.pods, "observed", &now)
			packageEnvironmentDetail(&e, "Runtime image inventory", fmt.Sprintf("%d image references in cattle-system", len(e.Images)), "observed", &now)
			if len(e.Images) == 0 {
				e.Warnings = append(e.Warnings, "No container images were reported in cattle-system at refresh time.")
			}
			continue
		}
		packageEnvironmentDetail(&e, r.label, r.value, "observed", &now)
		switch r.label {
		case "Rancher server version":
			e.RancherVersion = r.value
		case "Kubernetes server version":
			e.KubernetesVersion = r.value
		}
	}
	sort.Slice(e.Details, func(i, j int) bool { return e.Details[i].Label < e.Details[j].Label })
	sort.Strings(e.Warnings)
	return e
}

var issuePackageSafeHelmValue = regexp.MustCompile(`^[A-Za-z0-9_./:@+%,-]+$`)

// Preserve the stored invocation's flags and safe chart settings, but never
// guess that arbitrary custom values are safe. Unknown values, file references,
// credentials, URLs and target hostnames become explicit placeholders.
func sanitizeIssuePackageHelmCommand(command string) string {
	if len(command) > 64000 {
		return ""
	}
	fields, err := parseHelmCommandFields(command)
	if err != nil || len(fields) < 4 || fields[0] != "helm" || !packageEnum(fields[1], "install", "upgrade") {
		return ""
	}
	for _, field := range fields {
		if isShellControlField(field) {
			return ""
		}
	}
	out := []string{"helm " + fields[1]}
	positionals := 0
	for i := 2; i < len(fields); i++ {
		field := fields[i]
		if !strings.HasPrefix(field, "-") {
			positionals++
			if positionals <= 2 && issuePackageSafeHelmValue.MatchString(field) && !strings.Contains(field, "://") {
				out[0] += " " + field
			} else {
				out[0] += " " + shellQuote("<redacted>")
			}
			continue
		}
		flag, value, equals := strings.Cut(field, "=")
		if !issuePackageSafeHelmValue.MatchString(strings.TrimLeft(flag, "-")) {
			return ""
		}
		if !equals && packageEnum(flag, "--install", "--wait", "--wait-for-jobs", "--atomic", "--create-namespace", "--debug", "--devel", "--dependency-update", "--disable-openapi-validation", "--dry-run", "--insecure-skip-tls-verify", "--pass-credentials", "--reset-values", "--reuse-values", "--skip-crds", "--verify", "--force", "--no-hooks", "--plain-http", "--render-subchart-notes") {
			out = append(out, flag)
			continue
		}
		if !equals && i+1 < len(fields) && !strings.HasPrefix(fields[i+1], "-") {
			i++
			value = fields[i]
			equals = true
		}
		if !equals {
			out = append(out, flag)
			continue
		}
		safe := "<redacted>"
		switch flag {
		case "--set", "--set-string", "--set-literal":
			if strings.Contains(value, "={") {
				safe = sanitizeIssuePackageHelmJSON(value)
			} else {
				safe = sanitizeIssuePackageHelmAssignments(value)
			}
		case "--set-json":
			safe = sanitizeIssuePackageHelmJSON(value)
		case "--version", "--namespace", "-n", "--timeout", "--kube-version":
			if issuePackageSafeHelmValue.MatchString(value) {
				safe = value
			}
		case "-f", "--values", "--kubeconfig", "--set-file", "--registry-config", "--repository-config", "--repository-cache", "--ca-file", "--cert-file", "--key-file":
			safe = "<local-file>"
		}
		out = append(out, flag+" "+shellQuote(safe))
	}
	return strings.Join(out, " \\\n  ")
}

func safeIssuePackageHelmKey(key string) bool {
	lower := strings.ToLower(key)
	for _, sensitive := range []string{"password", "token", "secret", "credential", "private", "auth", "key", "hostname", "proxy", "url"} {
		if strings.Contains(lower, sensitive) {
			return false
		}
	}
	return packageEnum(lower, "replicas", "tls", "ingress.tls.source", "rancherimage", "rancherimagetag", "systemdefaultregistry", "usedynamiclistener", "debug", "privileged", "agenttlsmode", "antiaffinity") || strings.HasSuffix(lower, ".repository") || strings.HasSuffix(lower, ".tag") || strings.HasSuffix(lower, ".version") || strings.HasSuffix(lower, ".enabled") || strings.HasPrefix(lower, "features.")
}

func sanitizeIssuePackageHelmAssignments(value string) string {
	// Helm's escaped commas and brace lists can cross argument boundaries; keep
	// them private unless the whole assignment has a simple, recognized value.
	if strings.ContainsAny(value, "\\{}\n\r") {
		return "<redacted>"
	}
	parts := strings.Split(value, ",")
	for i, part := range parts {
		key, v, ok := strings.Cut(part, "=")
		if !ok || !issuePackageSafeHelmValue.MatchString(key) {
			parts[i] = "<redacted>"
			continue
		}
		if !safeIssuePackageHelmKey(key) || !issuePackageSafeHelmValue.MatchString(v) || strings.Contains(v, "://") {
			v = "<redacted>"
		}
		parts[i] = key + "=" + v
	}
	return strings.Join(parts, ",")
}

func sanitizeIssuePackageHelmJSON(value string) string {
	key, raw, ok := strings.Cut(value, "=")
	if !ok || !issuePackageSafeHelmValue.MatchString(key) {
		return "<redacted>"
	}
	var data any
	if json.Unmarshal([]byte(raw), &data) != nil {
		return key + "=<redacted>"
	}
	var clean func(string, any) any
	clean = func(path string, v any) any {
		switch obj := v.(type) {
		case map[string]any:
			out := map[string]any{}
			for k, child := range obj {
				out[k] = clean(path+"."+k, child)
			}
			return out
		case string:
			if safeIssuePackageHelmKey(path) && issuePackageSafeHelmValue.MatchString(obj) && !strings.Contains(obj, "://") {
				return obj
			}
		case bool, float64:
			if safeIssuePackageHelmKey(path) {
				return obj
			}
		}
		return "<redacted>"
	}
	encoded, _ := json.Marshal(clean(key, data))
	return key + "=" + string(encoded)
}
