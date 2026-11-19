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

func featureEnabled(flags, feature string, fallback bool) bool {
	for _, flag := range strings.Split(flags, ",") {
		switch strings.TrimSpace(flag) {
		case feature + "=true":
			return true
		case feature + "=false":
			return false
		}
	}
	return fallback
}

func featureDisabled(flags, feature string) bool {
	return !featureEnabled(flags, feature, true)
}

const (
	packagedRancherChartsURL = "https://git.rancher.io/charts"
	serverDeployment         = "cattle-system/rancher"
	webhookDeployment        = "cattle-system/rancher-webhook"
	fleetDeployment          = "cattle-fleet-system/fleet-controller"
	turtlesDeployment        = "cattle-turtles-system/rancher-turtles-controller-manager"
	upgradeDeployment        = "cattle-system/system-upgrade-controller"
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

// Keep the last observed chart error for the timeout even if it ages out of
// the 90-second log window. Each management cluster has its own wait.
type rancherSystemReadiness struct {
	lastChartMiss, catalogAtMiss string
}

func (w *rancherSystemReadiness) timeoutSummary(kubeconfig, summary string) string {
	if w.lastChartMiss == "" {
		return summary
	}
	if !strings.Contains(summary, w.lastChartMiss) {
		summary += ". Last observed chart install error: " + w.lastChartMiss
	}
	catalog, _ := runtimeChartCatalog(kubeconfig)
	if catalog != w.catalogAtMiss {
		summary += ". At timeout: " + catalog
	}
	return summary
}

// Rancher initially builds the index from the local checkout even in external
// mode. The next reconcile refreshes from upstream and requeues chart installs.
func runtimeChartCatalog(kubeconfig string) (summary string, bundled bool) {
	mode := "unknown"
	if raw, err := runKubectl(kubeconfig, "get", "settings.management.cattle.io", "system-catalog", "-o", "json"); err == nil {
		if value, _ := parseRancherSettingValue([]byte(raw)); value == "bundled" || value == "external" {
			mode = value
		}
	}
	var repo struct {
		Metadata struct{ Generation int64 }
		Spec     struct{ GitRepo, GitBranch string }
		Status   struct {
			URL, Branch, Commit string
			ObservedGeneration  int64
			ShouldNotSkip       bool
			Conditions          []struct{ Type, Status, Reason string }
		}
	}
	summary = "catalog mode=" + mode
	raw, err := runKubectl(kubeconfig, "get", "clusterrepos.catalog.cattle.io", "rancher-charts", "-o", "json")
	if err != nil || json.Unmarshal([]byte(raw), &repo) != nil {
		return summary + ", rancher-charts status unavailable", false
	}
	summary += fmt.Sprintf(", rancher-charts branch=%q commit=%q", repo.Spec.GitBranch, cmp.Or(repo.Status.Commit, "unreported"))
	if repo.Status.Branch != repo.Spec.GitBranch {
		summary += fmt.Sprintf(" activeBranch=%q", repo.Status.Branch)
	}
	if repo.Status.ShouldNotSkip {
		summary += " refresh pending"
	}
	downloaded := false
	for _, condition := range repo.Status.Conditions {
		if condition.Type == "Downloaded" {
			summary += fmt.Sprintf(" Downloaded=%q", condition.Status)
			if condition.Reason != "" {
				summary += fmt.Sprintf(" reason=%q", condition.Reason)
			}
			downloaded = condition.Status == "True"
			break
		}
	}
	// A custom ClusterRepo can still fetch remotely in bundled mode. Only
	// fail early for Rancher's packaged repo after it has reported a revision.
	bundled = mode == "bundled" && repo.Spec.GitRepo == packagedRancherChartsURL && repo.Status.URL == repo.Spec.GitRepo &&
		repo.Status.Branch == repo.Spec.GitBranch && repo.Status.Commit != "" && downloaded && !repo.Status.ShouldNotSkip &&
		repo.Metadata.Generation > 0 && repo.Status.ObservedGeneration == repo.Metadata.Generation
	return summary, bundled
}

func (w *rancherSystemReadiness) deploymentsReady(kubeconfig string) (bool, string, error) {
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
	var upgradePolicy string
	if !versionKnown || compareVersionStrings(version, "2.10") >= 0 {
		requireUpgrade := !featureDisabled(flags, "managed-system-upgrade-controller")
		if server.Image != "" && versionKnown && compareVersionStrings(version, "2.11") >= 0 {
			// Always check an existing controller's rollout. For an absent
			// controller, use Rancher's current installation policy.
			_, requireUpgrade = deployments[upgradeDeployment]
			if !requireUpgrade {
				requireUpgrade, upgradePolicy = localUpgradeControllerPolicy(kubeconfig, server, version)
			}
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
		w.lastChartMiss = ""
		details = append(details, strings.Join(required, ", ")+" ready")
	} else if server.Image != "" {
		// The chart controller may be on any HA replica. Ignore historical failures on
		// healthy components, startup warnings, and transient errors fetching logs.
		logs, err := runKubectl(kubeconfig, "logs", "-n", "cattle-system", "-l", "app=rancher", "-c", "rancher", "--since=90s", "--tail=300")
		if err == nil {
			if missing := missingSystemChartVersions(logs, pending); len(missing) > 0 {
				catalog, bundled := runtimeChartCatalog(kubeconfig)
				w.catalogAtMiss = catalog
				w.lastChartMiss = fmt.Sprintf("Rancher %s reported %s missing from its index (%s)", server.Image, strings.Join(missing, ", "), catalog)
				if bundled && rancher.fullyReady() {
					return false, "", fmt.Errorf("%s. Check %s", w.lastChartMiss, cmp.Or(rancher.assetsImage(), "the rancher-assets image or bundled chart catalog"))
				}
				details = append(details, w.lastChartMiss)
			}
		}
	}
	if upgradePolicy != "" {
		details = append(details, "system-upgrade-controller policy: "+upgradePolicy)
	}
	return len(pending) == 0, strings.Join(details, "; "), nil
}

func localUpgradeControllerPolicy(kubeconfig string, server systemChartContainer, version string) (bool, string) {
	flags := server.envValue("CATTLE_FEATURES")
	mcm := !featureDisabled(flags, "multi-cluster-management")
	agent := featureEnabled(flags, "multi-cluster-management-agent", false)
	agentRequiresUpgrade := agent && !featureDisabled(flags, "managed-system-upgrade-controller")
	harvester := !mcm && !agent && !featureDisabled(flags, "harvester") && compareVersionStrings(version, "2.12") >= 0
	if !mcm && !harvester {
		return agentRequiresUpgrade, fmt.Sprintf("management disabled, agent requires controller=%t", agentRequiresUpgrade)
	}

	var cluster struct {
		Metadata struct{ Annotations map[string]string }
		Status   struct {
			Driver, Provider string
			Version          struct{ GitVersion string }
		}
	}
	raw, err := runKubectl(kubeconfig, "get", "clusters.management.cattle.io", "local", "-o", "json")
	if err != nil || json.Unmarshal([]byte(raw), &cluster) != nil {
		return true, "waiting for local cluster status"
	}
	driver, kubeVersion := cluster.Status.Driver, cluster.Status.Version.GitVersion
	detail := fmt.Sprintf("local driver=%q kubernetes=%q", driver, kubeVersion)
	if driver == "" {
		return true, detail + ", waiting for local cluster driver"
	}
	// Match Rancher's dashboard/systemcharts controller. The global setting
	// applies only after the driver is RKE2/K3s (or Harvester in its local mode).
	// An imported/local driver can persist with no status.version. Predicting
	// a future driver here makes readiness wait for a chart Rancher has not enabled.
	managedLocal := mcm && (driver == "rke2" || driver == "k3s")
	managedHarvester := harvester && driver == "imported" && cluster.Status.Provider == "harvester"
	if !managedLocal && !managedHarvester {
		return agentRequiresUpgrade, fmt.Sprintf("%s, controller enabled=%t for current driver", detail, agentRequiresUpgrade)
	}
	value := cluster.Metadata.Annotations["rancher.io/imported-cluster-version-management"]
	source := "annotation"
	if value != "true" && value != "false" {
		source, value = "server environment", server.envValue("CATTLE_IMPORTED_CLUSTER_VERSION_MANAGEMENT")
		if value == "" {
			source = "global setting"
			raw, err := runKubectl(kubeconfig, "get", "settings.management.cattle.io", "imported-cluster-version-management", "-o", "json")
			if err == nil {
				value, _ = parseRancherSettingValue([]byte(raw))
			}
		}
	}
	if value != "true" && value != "false" {
		return true, detail + ", waiting for version-management policy"
	}
	return value == "true", fmt.Sprintf("%s, version management=%s (%s)", detail, value, source)
}
