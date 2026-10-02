package test

import (
	"encoding/json"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/buildinfo"
	"github.com/brudnak/ha-rancher-rke2/terratest/settings"
	"github.com/spf13/viper"
	"html/template"
	"net/http"
	"strconv"
	"strings"
)

func interactiveSetupTemplateDataFor(token string, configPath string, initialVersions []string, basePath string, embedded bool) interactiveSetupTemplateData {
	viperConfigMu.RLock()
	defer viperConfigMu.RUnlock()

	initialCustomHostname := settings.CurrentCustomHostnamePrefix()
	initialStateJSON, _ := json.Marshal(interactiveSetupState{
		Token:                 token,
		BasePath:              normalizeInteractiveBasePath(basePath),
		ConfigPath:            configPath,
		DeploymentType:        deploymentType(),
		Mode:                  rancherMode(),
		Versions:              initialVersions,
		AgentImages:           currentAgentImageOverrides(initialVersions),
		HelmCommands:          currentManualHelmCommands(),
		K8SVersions:           currentManualK8SVersions(),
		InstallerSHA256s:      currentManualInstallerSHA256s(),
		ResolveInstallerSHA:   currentManualResolveInstallerSHA(),
		Config:                settings.CurrentEditablePreflightConfig(),
		CustomHostnameEnabled: initialCustomHostname != "",
		CustomHostname:        initialCustomHostname,
		Embedded:              embedded,
	})

	return interactiveSetupTemplateData{
		Token:            token,
		BuildLabel:       buildinfo.Current().AppLabel(),
		BasePath:         normalizeInteractiveBasePath(basePath),
		ConfigPath:       configPath,
		Embedded:         embedded,
		InitialStateJSON: template.JS(string(initialStateJSON)),
	}
}

func currentAgentImageOverrides(versions []string) []string {
	overrides := viper.GetStringSlice("rancher.agent_images")
	if len(overrides) == 0 {
		if single := strings.TrimSpace(viper.GetString("rancher.agent_image")); single != "" {
			overrides = []string{single}
		}
	}
	for len(overrides) < len(versions) {
		overrides = append(overrides, "")
	}
	if len(overrides) > len(versions) {
		overrides = overrides[:len(versions)]
	}
	return overrides
}

func currentManualHelmCommands() []string {
	commands := viper.GetStringSlice("rancher.helm_commands")
	if len(commands) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(commands))
	for _, command := range commands {
		out = append(out, strings.TrimSpace(command))
	}
	return out
}

func currentManualK8SVersions() []string {
	versions := viper.GetStringSlice("k8s.versions")
	if len(versions) > 0 {
		out := make([]string, 0, len(versions))
		for _, version := range versions {
			out = append(out, strings.TrimSpace(version))
		}
		return out
	}
	if version := strings.TrimSpace(viper.GetString("k8s.version")); version != "" {
		return []string{version}
	}
	return []string{}
}

func currentManualInstallerSHA256s() []string {
	versions := currentManualK8SVersions()
	checksums := viper.GetStringMapString("rke2.install_script_sha256s")
	out := make([]string, 0, len(versions))
	for _, version := range versions {
		checksum := strings.TrimSpace(checksums[version])
		if checksum == "" && strings.TrimSpace(viper.GetString("k8s.version")) == version {
			checksum = strings.TrimSpace(viper.GetString("rke2.install_script_sha256"))
		}
		out = append(out, checksum)
	}
	return out
}

func currentManualResolveInstallerSHA() bool {
	if rancherMode() != "manual" {
		return true
	}
	for _, checksum := range currentManualInstallerSHA256s() {
		if strings.TrimSpace(checksum) == "" {
			return true
		}
	}
	return false
}

func normalizeInteractiveBasePath(basePath string) string {
	basePath = "/" + strings.Trim(strings.TrimSpace(basePath), "/")
	if basePath == "/" {
		return ""
	}
	return basePath
}

func interactiveSetupPath(basePath string, path string) string {
	if basePath == "" {
		return path
	}
	if path == "/" {
		return basePath + "/"
	}
	return basePath + path
}

func (s *interactiveServer) authorized(r *http.Request) bool {
	if strings.TrimSpace(r.URL.Query().Get("token")) == s.token {
		return true
	}
	if r.FormValue("token") == s.token {
		return true
	}
	return requestFromLoopback(r) && sameOriginBrowserRequest(r)
}

func decodePreflightConfigUpdateRequest(r *http.Request) (settings.PreflightConfigUpdate, error) {
	contentType := strings.ToLower(r.Header.Get("Content-Type"))
	if strings.Contains(contentType, "application/json") {
		var req settings.PreflightConfigUpdate
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return req, err
		}
		return req, nil
	}

	if err := r.ParseForm(); err != nil {
		return settings.PreflightConfigUpdate{}, err
	}
	tfVars := make(map[string]string, len(settings.EditableTFVarKeys))
	for _, key := range settings.EditableTFVarKeys {
		tfVars[key] = r.FormValue("tfVars." + key)
	}
	var downstreamLinodePlans []settings.LinodeDownstreamPlan
	if rawPlans := strings.TrimSpace(r.FormValue("downstreamLinodePlans")); rawPlans != "" {
		if err := json.Unmarshal([]byte(rawPlans), &downstreamLinodePlans); err != nil {
			return settings.PreflightConfigUpdate{}, fmt.Errorf("invalid downstream Linode plans: %w", err)
		}
	}

	return settings.PreflightConfigUpdate{
		DeploymentType:           r.FormValue("deploymentType"),
		Mode:                     r.FormValue("mode"),
		Versions:                 r.Form["versions"],
		AgentImages:              r.Form["agentImages"],
		DownstreamLinodePlans:    downstreamLinodePlans,
		HelmCommands:             r.Form["helmCommands"],
		K8SVersions:              r.Form["k8sVersions"],
		InstallerSHA256s:         r.Form["installerSHA256s"],
		ResolveInstallerSHA:      parseHTMLBool(r.FormValue("resolveInstallerSHA")),
		Distro:                   r.FormValue("distro"),
		PreferredImageRegistries: r.Form["preferredImageRegistries"],
		BootstrapPassword:        r.FormValue("bootstrapPassword"),
		WebhookImage:             r.FormValue("webhookImage"),
		PreloadImages:            parseHTMLBool(r.FormValue("preloadImages")),
		ServerCount:              parseHTMLInt(r.FormValue("serverCount")),
		GPUWorkerEnabled:         parseHTMLBool(r.FormValue("gpuWorkerEnabled")),
		GPUWorkerProfile:         r.FormValue("gpuWorkerProfile"),
		GPUWorkerAMI:             r.FormValue("gpuWorkerAmi"),
		GPUWorkerSubnetID:        r.FormValue("gpuWorkerSubnetId"),
		HostedRDSPassword:        r.FormValue("hostedRDSPassword"),
		HostedEC2InstanceType:    r.FormValue("hostedEC2InstanceType"),
		LinodeDockerHub:          r.FormValue("linodeDockerHub"),
		LinodeCustomImage:        r.FormValue("linodeCustomImage"),
		LinodeSSHRootPassword:    r.FormValue("linodeSSHRootPassword"),
		UserFirstName:            r.FormValue("userFirstName"),
		UserLastName:             r.FormValue("userLastName"),
		TFVars:                   tfVars,
		CustomHostnameEnabled:    parseHTMLBool(r.FormValue("customHostnameEnabled")),
		CustomHostnameInput:      r.FormValue("customHostname"),
	}, nil
}

func parseHTMLBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "on", "1", "yes":
		return true
	default:
		return false
	}
}

func parseHTMLInt(value string) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0
	}
	return parsed
}
