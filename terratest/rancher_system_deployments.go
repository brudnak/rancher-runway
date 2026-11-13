package test

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
)

// Rancher's management chart and the cluster API expose the same pod template.
type systemChartContainer struct {
	Name    string
	Image   string
	Env     []systemChartEnvVar
	EnvFrom []any `yaml:"envFrom"`
}

type systemChartEnvVar struct {
	Name, Value string
	ValueFrom   any `yaml:"valueFrom"`
}

func (c systemChartContainer) envValue(name string) string {
	for _, e := range c.Env {
		if e.Name == name {
			return e.Value
		}
	}
	return ""
}

type rancherDeployment struct {
	Metadata struct {
		Name, Namespace   string
		Generation        int64
		DeletionTimestamp *string
	}
	Spec struct {
		Replicas int
		Template struct {
			Spec struct {
				Containers     []systemChartContainer
				InitContainers []systemChartContainer `yaml:"initContainers"`
			}
		}
	}
	Status struct {
		ObservedGeneration                                          int64
		Replicas, ReadyReplicas, UpdatedReplicas, AvailableReplicas int
	}
}

func (d rancherDeployment) fullyReady() bool {
	n := d.Spec.Replicas
	return n > 0 && d.Metadata.DeletionTimestamp == nil && d.Status.ObservedGeneration >= d.Metadata.Generation &&
		d.Status.Replicas == n && d.Status.ReadyReplicas == n && d.Status.UpdatedReplicas == n && d.Status.AvailableReplicas == n
}

func (d rancherDeployment) server() systemChartContainer {
	for _, c := range d.Spec.Template.Spec.Containers {
		if c.Name == "rancher" {
			return c
		}
	}
	return systemChartContainer{}
}

func (d rancherDeployment) assetsImage() string {
	for _, c := range d.Spec.Template.Spec.InitContainers {
		if c.Name == "rancher-charts-copy" {
			return c.Image
		}
	}
	return ""
}

func featureDisabled(flags, feature string) bool {
	for _, flag := range strings.Split(flags, ",") {
		if strings.TrimSpace(flag) == feature+"=false" {
			return true
		}
	}
	return false
}

const (
	serverDeployment  = "cattle-system/rancher"
	webhookDeployment = "cattle-system/rancher-webhook"
	fleetDeployment   = "cattle-fleet-system/fleet-controller"
	turtlesDeployment = "cattle-turtles-system/rancher-turtles-controller-manager"
	upgradeDeployment = "cattle-system/system-upgrade-controller"
)

var systemChartMissingVersion = regexp.MustCompile(`Failed to install system chart ([a-z0-9-]+) \(release name: [a-z0-9-]+\)(?:, will retry)?: specified version ([0-9][0-9A-Za-z.+-]*) doesn't exist in the index`)

var deploymentForChart = map[string]string{
	"rancher-webhook": webhookDeployment, "fleet": fleetDeployment, "fleet-crd": fleetDeployment,
	"rancher-turtles": turtlesDeployment, "system-upgrade-controller": upgradeDeployment,
}

func missingSystemChartVersions(logs string, pending map[string]bool) []string {
	var missing []string
	for _, m := range systemChartMissingVersion.FindAllStringSubmatch(logs, -1) {
		deployment := deploymentForChart[m[1]]
		if pending[deployment] {
			missing = append(missing, m[1]+"@"+m[2])
		}
	}
	slices.Sort(missing)
	return slices.Compact(missing)
}

// Observation failures are retryable. Only an explicit chart installation failure is terminal.
func rancherSystemDeploymentsReady(kubeconfig string) (bool, string, error) {
	if _, err := os.Stat(kubeconfig); err != nil {
		return false, fmt.Sprintf("kubeconfig unavailable: %v", err), nil
	}
	raw, err := runKubectl(kubeconfig, "get", "deployments.apps", "-A", "-o", "json")
	if err != nil {
		return false, sanitizeDiagnosticOutput(err.Error()), nil
	}
	var list struct{ Items []rancherDeployment }
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return false, "could not decode Kubernetes deployments", nil
	}
	deployments := make(map[string]rancherDeployment, len(list.Items))
	for _, d := range list.Items {
		deployments[d.Metadata.Namespace+"/"+d.Metadata.Name] = d
	}
	rancher := deployments[serverDeployment]
	server := rancher.server()
	flags := server.envValue("CATTLE_FEATURES")

	// Use the running image's minor line; a fallback Helm chart can be older.
	version := clusterDeploymentImageVersion(server.Image)
	if server.Image != "" && !customImageMinorLinePattern.MatchString(version) {
		setting, err := runKubectl(kubeconfig, "get", "settings.management.cattle.io", "server-version", "-o", "json")
		if err == nil {
			version, _ = parseRancherSettingValue([]byte(setting))
		}
	}
	required := []string{serverDeployment, webhookDeployment}
	if !(featureDisabled(flags, "multi-cluster-management") && featureDisabled(flags, "fleet")) {
		required = append(required, fleetDeployment)
	}
	versionKnown := customImageMinorLinePattern.MatchString(version)
	if !featureDisabled(flags, "turtles") && (!versionKnown || compareVersionStrings(version, "2.13") >= 0) {
		required = append(required, turtlesDeployment)
	}
	if !versionKnown || compareVersionStrings(version, "2.10") >= 0 {
		requireUpgrade := !featureDisabled(flags, "managed-system-upgrade-controller")
		if server.Image != "" && versionKnown && compareVersionStrings(version, "2.11") >= 0 {
			// Only query policy when it would hold up readiness. A local cluster
			// override takes precedence over the global Rancher setting.
			requireUpgrade = deployments[upgradeDeployment].fullyReady() || localUpgradeControllerEnabled(kubeconfig, server)
		}
		if requireUpgrade {
			required = append(required, upgradeDeployment)
		}
	}

	var details []string
	pending := map[string]bool{}
	for _, name := range required {
		d, found := deployments[name]
		if found && d.fullyReady() {
			continue
		}
		pending[name] = true
		if !found {
			details = append(details, name+" missing")
		} else {
			details = append(details, fmt.Sprintf("%s ready=%d/%d updated=%d available=%d generation=%d/%d", name,
				d.Status.ReadyReplicas, d.Spec.Replicas, d.Status.UpdatedReplicas, d.Status.AvailableReplicas, d.Status.ObservedGeneration, d.Metadata.Generation))
		}
	}
	if len(pending) == 0 {
		return true, strings.Join(required, ", ") + " ready", nil
	}
	if server.Image != "" {
		// The chart controller may be on any HA replica. Ignore historical failures on
		// healthy components, startup warnings, and transient errors fetching logs.
		logs, err := runKubectl(kubeconfig, "logs", "-n", "cattle-system", "-l", "app=rancher", "-c", "rancher", "--since=90s", "--tail=300")
		if err == nil {
			if missing := missingSystemChartVersions(logs, pending); len(missing) > 0 {
				return false, "", fmt.Errorf("Rancher %s cannot install system charts missing from its index: %s. Check %s and retry with matching charts", server.Image, strings.Join(missing, ", "), cmp.Or(rancher.assetsImage(), "the rancher-assets image or bundled chart catalog"))
			}
		}
	}
	return false, strings.Join(details, "; "), nil
}

func localUpgradeControllerEnabled(kubeconfig string, server systemChartContainer) bool {
	override, err := runKubectl(kubeconfig, "get", "clusters.management.cattle.io", "local", "-o", `jsonpath={.metadata.annotations.rancher\.io/imported-cluster-version-management}`)
	if err != nil {
		return true
	}
	if override == "true" || override == "false" {
		return override == "true"
	}
	value := server.envValue("CATTLE_IMPORTED_CLUSTER_VERSION_MANAGEMENT")
	if value == "" {
		raw, err := runKubectl(kubeconfig, "get", "settings.management.cattle.io", "imported-cluster-version-management", "-o", "json")
		if err == nil {
			value, _ = parseRancherSettingValue([]byte(raw))
		}
	}
	return value != "false"
}
