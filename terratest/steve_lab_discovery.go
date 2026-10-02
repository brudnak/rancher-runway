package test

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"
)

type steveLabPanelState struct {
	Preflight   systemReadinessState   `json:"preflight"`
	Operation   panelOperationSnapshot `json:"operation"`
	Runs        []steveLabRunRecord    `json:"runs"`
	K3SVersions []string               `json:"k3sVersions"`
}

type steveLabRunRecord struct {
	RunID                        string    `json:"runId"`
	Status                       string    `json:"status"`
	SteveRef                     string    `json:"steveRef"`
	SteveCommit                  string    `json:"steveCommit,omitempty"`
	K3SVersion                   string    `json:"k3sVersion"`
	ClusterName                  string    `json:"clusterName"`
	Kubeconfig                   string    `json:"kubeconfig"`
	RunDir                       string    `json:"runDir"`
	SourceDir                    string    `json:"sourceDir"`
	HTTPPort                     int       `json:"httpPort,omitempty"`
	HTTPSPort                    int       `json:"httpsPort,omitempty"`
	HTTPURL                      string    `json:"httpUrl,omitempty"`
	HTTPSURL                     string    `json:"httpsUrl,omitempty"`
	StevePID                     int       `json:"stevePid,omitempty"`
	LogPath                      string    `json:"logPath,omitempty"`
	KeepCluster                  bool      `json:"keepCluster"`
	SQLCache                     bool      `json:"sqlCache"`
	SQLCacheFlag                 bool      `json:"sqlCacheFlag,omitempty"`
	EnableMetrics                bool      `json:"enableMetrics,omitempty"`
	MetricsUpdateIntervalSeconds int       `json:"metricsUpdateIntervalSeconds,omitempty"`
	ExtraEnv                     []string  `json:"extraEnv,omitempty"`
	ExtraArgs                    []string  `json:"extraArgs,omitempty"`
	CreatedAt                    time.Time `json:"createdAt"`
	UpdatedAt                    time.Time `json:"updatedAt"`
	Error                        string    `json:"error,omitempty"`
}

type steveVersionRef struct {
	Name   string `json:"name"`
	Commit string `json:"commit"`
	Kind   string `json:"kind"`
}

type steveVersionDiscovery struct {
	RepoURL string            `json:"repoUrl"`
	Tags    []steveVersionRef `json:"tags"`
	Error   string            `json:"error,omitempty"`
}

type steveRefDetails struct {
	Ref                     string   `json:"ref"`
	KubernetesModule        string   `json:"kubernetesModule,omitempty"`
	KubernetesModuleVersion string   `json:"kubernetesModuleVersion,omitempty"`
	RecommendedMinor        string   `json:"recommendedMinor,omitempty"`
	RecommendedK3SVersions  []string `json:"recommendedK3sVersions"`
	Error                   string   `json:"error,omitempty"`
}

type steveLabStartRequest struct {
	SteveRef                     string   `json:"steveRef"`
	K3SVersion                   string   `json:"k3sVersion"`
	KeepCluster                  bool     `json:"keepCluster"`
	HTTPPort                     int      `json:"httpPort"`
	HTTPSPort                    int      `json:"httpsPort"`
	HeaderAuth                   bool     `json:"headerAuth"`
	EnableSQLCache               bool     `json:"enableSqlCache"`
	EnableMetrics                bool     `json:"enableMetrics"`
	MetricsUpdateIntervalSeconds int      `json:"metricsUpdateIntervalSeconds"`
	ExtraEnv                     []string `json:"extraEnv"`
	ExtraArgs                    []string `json:"extraArgs"`
	Replace                      bool     `json:"replace"`
}

func collectSteveLabPreflight() systemReadinessState {
	tools := []systemReadinessToolConfig{
		{Name: "Go", Command: "go", Args: []string{"version"}, VersionPattern: `go([0-9]+\.[0-9]+(?:\.[0-9]+)?)`, MinimumVersion: "1.26.1", RecommendedVersion: "1.26.1"},
		{Name: "git", Command: "git", Args: []string{"--version"}, VersionPattern: `git version ([0-9]+\.[0-9]+(?:\.[0-9]+)?)`, MinimumVersion: "2.39.0"},
		{Name: "Docker", Command: "docker", Args: []string{"version", "--format", "{{.Server.Version}}"}, VersionPattern: `([0-9]+\.[0-9]+(?:\.[0-9]+)?)`, MinimumVersion: "24.0.0"},
		{Name: "k3d", Command: "k3d", Args: []string{"version"}, VersionPattern: `k3d version v?([0-9]+\.[0-9]+(?:\.[0-9]+)?)`, MinimumVersion: "5.6.0", RecommendedVersion: "5.8.3"},
		{Name: "kubectl", Command: "kubectl", Args: []string{"version", "--client=true"}, VersionPattern: `Client Version: v?([0-9]+\.[0-9]+(?:\.[0-9]+)?)`, MinimumVersion: "1.30.0"},
	}
	items := make([]systemReadinessItem, 0, len(tools))
	for _, tool := range tools {
		items = append(items, checkSystemReadinessTool(tool))
	}
	ready := true
	warnings := 0
	for _, item := range items {
		if item.Status == "error" {
			ready = false
		}
		if item.Status == "warning" {
			warnings++
		}
	}
	summary := "Ready to run Steve locally"
	if !ready {
		summary = "Steve Lab needs local tools before it can run"
	} else if warnings > 0 {
		summary = fmt.Sprintf("Ready with %d warning(s)", warnings)
	}
	return systemReadinessState{Ready: ready, Summary: summary, Items: items}
}

func discoverSteveVersions() steveVersionDiscovery {
	gitPath, err := resolveLocalToolPath("git")
	if err != nil {
		return steveVersionDiscovery{RepoURL: steveRepoURL, Error: err.Error()}
	}
	cmd := exec.Command(gitPath, "ls-remote", "--tags", "--refs", steveRepoURL)
	cmd.Env = localToolEnv(nil)
	output, err := cmd.Output()
	if err != nil {
		return steveVersionDiscovery{RepoURL: steveRepoURL, Error: err.Error()}
	}
	seen := map[string]bool{}
	var refs []steveVersionRef
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "refs/tags/")
		if !regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+`).MatchString(name) || seen[name] {
			continue
		}
		seen[name] = true
		refs = append(refs, steveVersionRef{Name: name, Commit: fields[0], Kind: "tag"})
	}
	sort.Slice(refs, func(i, j int) bool {
		return compareVersionStrings(strings.TrimPrefix(refs[i].Name, "v"), strings.TrimPrefix(refs[j].Name, "v")) > 0
	})
	if len(refs) > 80 {
		refs = refs[:80]
	}
	return steveVersionDiscovery{RepoURL: steveRepoURL, Tags: refs}
}

func inspectSteveRefDetails(ref string) steveRefDetails {
	ref = strings.TrimSpace(ref)
	details := steveRefDetails{Ref: ref, RecommendedK3SVersions: defaultK3SVersions()}
	if !validSteveRef(ref) {
		details.Error = "Ref can contain letters, numbers, dots, dashes, underscores, slashes, plus signs, and @ only."
		return details
	}
	client := http.Client{Timeout: 8 * time.Second}
	url := fmt.Sprintf("https://raw.githubusercontent.com/rancher/steve/%s/go.mod", ref)
	resp, err := client.Get(url)
	if err != nil {
		details.Error = err.Error()
		return details
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		details.Error = fmt.Sprintf("go.mod lookup returned %s", resp.Status)
		return details
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		details.Error = err.Error()
		return details
	}
	module, version := findSteveKubernetesModule(string(data))
	details.KubernetesModule = module
	details.KubernetesModuleVersion = version
	if minor := kubernetesMinorFromModule(version); minor != "" {
		details.RecommendedMinor = "1." + minor
		details.RecommendedK3SVersions = k3sVersionsForModuleVersion(version)
	}
	return details
}

func findSteveKubernetesModule(goMod string) (string, string) {
	for _, line := range strings.Split(goMod, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) >= 2 && (fields[0] == "k8s.io/client-go" || fields[0] == "k8s.io/apimachinery") {
			return fields[0], fields[1]
		}
	}
	return "", ""
}

func kubernetesMinorFromModule(version string) string {
	matches := regexp.MustCompile(`v0\.([0-9]+)\.`).FindStringSubmatch(version)
	if len(matches) != 2 {
		return ""
	}
	return matches[1]
}

func kubernetesPatchFromModule(version string) string {
	matches := regexp.MustCompile(`v0\.[0-9]+\.([0-9]+)`).FindStringSubmatch(version)
	if len(matches) != 2 {
		return "0"
	}
	return matches[1]
}

func defaultK3SVersions() []string {
	return []string{
		"v1.33.5-k3s1",
		"v1.32.9-k3s1",
		"v1.31.13-k3s1",
		"v1.30.14-k3s1",
		"v1.29.15-k3s1",
	}
}

func k3sVersionsForModuleVersion(moduleVersion string) []string {
	minor := kubernetesMinorFromModule(moduleVersion)
	if minor == "" {
		return defaultK3SVersions()
	}
	versions := k3sVersionsForMinor(minor)
	if len(versions) > 0 && strings.HasPrefix(versions[0], "v1."+minor+".") {
		return versions
	}
	patch := kubernetesPatchFromModule(moduleVersion)
	candidate := fmt.Sprintf("v1.%s.%s-k3s1", minor, patch)
	return append([]string{candidate}, removeString(defaultK3SVersions(), candidate)...)
}

func k3sVersionsForMinor(minor string) []string {
	for _, version := range defaultK3SVersions() {
		if strings.HasPrefix(version, "v1."+minor+".") {
			return append([]string{version}, removeString(defaultK3SVersions(), version)...)
		}
	}
	return defaultK3SVersions()
}

func removeString(values []string, remove string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != remove {
			out = append(out, value)
		}
	}
	return out
}

func validSteveRef(ref string) bool {
	return regexp.MustCompile(`^[A-Za-z0-9._/@+-]+$`).MatchString(strings.TrimSpace(ref))
}

func normalizeSteveLabK3SVersion(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "rancher/k3s:")
	return value
}

func k3sImage(value string) string {
	value = normalizeSteveLabK3SVersion(value)
	if value == "" {
		value = defaultK3SVersions()[0]
	}
	return "rancher/k3s:" + value
}

func normalizeSteveLabMetricsInterval(value int) int {
	if value <= 0 {
		return 15
	}
	return value
}

func normalizeSteveLabEnv(values []string) ([]string, error) {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if strings.ContainsRune(value, '\x00') {
			return nil, fmt.Errorf("Steve env var contains a null byte")
		}
		key, _, ok := strings.Cut(value, "=")
		if !ok {
			return nil, fmt.Errorf("Steve env var %q must use KEY=value format", value)
		}
		if !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(key) {
			return nil, fmt.Errorf("Steve env var %q has an invalid name", key)
		}
		out = append(out, value)
	}
	return out, nil
}

func normalizeSteveLabArgs(values []string) ([]string, error) {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if strings.ContainsRune(value, '\x00') || strings.ContainsAny(value, "\r\n") {
			return nil, fmt.Errorf("Steve argument %q contains unsupported control characters", value)
		}
		out = append(out, value)
	}
	return out, nil
}
