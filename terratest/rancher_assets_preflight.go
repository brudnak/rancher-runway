package test

import (
	"cmp"
	"context"
	"fmt"
	"log"
	"maps"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	"gopkg.in/yaml.v3"
)

// Inspect the images Helm will actually install. A head image can use a GA
// chart, and that chart's assets tag can differ from the server build's default.
func preflightRancherAssets(plans []*RancherResolvedPlan) ([]*RancherResolvedPlan, error) {
	service := newPreferredRancherImageLookupService()
	defer service.CloseIdleConnections()
	catalog := &helmLabCatalogService{}
	for i, plan := range plans {
		for _, command := range plan.HelmCommands {
			kubeVersion := helmKubeVersionFromRKE2Version(cmp.Or(plan.RecommendedK3SVersion, plan.RecommendedRKE2Version))
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			summary, mismatch := checkPlannedRancherAssets(ctx, command, kubeVersion, service, catalog)
			cancel()
			if mismatch != nil {
				return nil, fmt.Errorf("Rancher %d chart preflight: %w", i+1, mismatch)
			}
			log.Printf("[preflight] System charts for Rancher %d: %s", i+1, summary)
			plan.SystemChartPreflight = summary
		}
	}
	return plans, nil
}

type systemChartImageLookup interface {
	Inspect(context.Context, imagelookup.InspectRequest) (imagelookup.InspectResponse, error)
	FetchSourceBuildYAML(context.Context, imagelookup.SourceBuildYAMLRequest) (imagelookup.SourceBuildYAMLResponse, error)
}

func checkPlannedRancherAssets(ctx context.Context, command, kubeVersion string, images systemChartImageLookup, catalog *helmLabCatalogService) (string, error) {
	args, err := rancherHelmTemplateArgs(command, kubeVersion)
	if err != nil {
		return "Inconclusive: could not parse the Helm invocation", nil
	}
	args = append(args, "--show-only", "templates/deployment.yaml")
	raw, err := exec.CommandContext(ctx, "helm", args...).Output()
	if err != nil {
		// The Helm arguments and rendered manifests can contain the bootstrap password.
		return "Inconclusive: Helm could not render the Rancher deployment", nil
	}
	var deployment rancherDeployment
	if err := yaml.Unmarshal(raw, &deployment); err != nil || deployment.server().Image == "" {
		return "Inconclusive: could not identify the rendered Rancher server", nil
	}
	if deployment.assetsImage() == "" {
		return "Unavailable: the rendered deployment has no separate rancher-assets image", nil
	}
	return compareRancherAssets(ctx, deployment, images, catalog)
}

func imageEnvironment(entries []string) map[string]string {
	values := map[string]string{}
	for _, entry := range entries {
		if name, value, ok := strings.Cut(entry, "="); ok {
			values[name] = value
		}
	}
	return values
}

// A missing pin in a successfully read, revision-pinned index is a mismatch.
// Unavailable metadata is inconclusive and leaves verification to the cluster.
func compareRancherAssets(ctx context.Context, deployment rancherDeployment, images systemChartImageLookup, catalog *helmLabCatalogService) (string, error) {
	server := deployment.server()
	assets := deployment.assetsImage()
	inspect := func(ref string) (imagelookup.InspectResponse, error) {
		return images.Inspect(ctx, imagelookup.InspectRequest{Reference: ref, Platform: "linux/amd64", SkipTagMetadata: true})
	}
	image, err := inspect(server.Image)
	if imagelookup.RegistryNotFound(err) {
		return "", fmt.Errorf("registry could not find the Rancher server image %s", server.Image)
	}
	if err != nil {
		return "Inconclusive: could not inspect " + server.Image, nil
	}
	bundle, err := inspect(assets)
	if imagelookup.RegistryNotFound(err) {
		return "", fmt.Errorf("registry could not find the rancher-assets image %s selected for %s", assets, server.Image)
	}
	if err != nil {
		return "Inconclusive: could not inspect " + assets, nil
	}
	revision := rancherChartsRevision(bundle.Config)
	if !imagelookup.GitRevisionPattern.MatchString(revision) {
		return "Inconclusive: " + assets + " has no pinned rancher/charts revision", nil
	}

	env := imageEnvironment(image.Config.Env)
	if len(server.EnvFrom) > 0 {
		return "Inconclusive: server environment comes from cluster resources", nil
	}
	for _, e := range server.Env {
		if e.ValueFrom != nil && (strings.HasSuffix(e.Name, "_VERSION") || e.Name == "CATTLE_FEATURES" || e.Name == "CATTLE_IMPORTED_CLUSTER_VERSION_MANAGEMENT") {
			return "Inconclusive: server chart settings come from cluster resources", nil
		}
		if e.ValueFrom == nil {
			env[e.Name] = e.Value
		}
	}
	pins := rancherSystemChartPins(ctx, image, env, images, catalog)
	index, err := readSystemChartIndex(ctx, revision, catalog)
	if err != nil {
		return fmt.Sprintf("Inconclusive: %s for %s", err, assets), nil
	}
	var missing, unknown, verified []string
	for chart, version := range pins {
		if version == "" {
			unknown = append(unknown, chart)
			continue
		}
		pin := chart + "@" + version
		if index.contains(chart, version) {
			verified = append(verified, pin)
		} else {
			missing = append(missing, pin)
		}
	}
	slices.Sort(missing)
	slices.Sort(unknown)
	slices.Sort(verified)
	if len(missing) > 0 {
		return "", fmt.Errorf("%s requires %s, missing from %s (rancher/charts %s). Select a Rancher build or assets image containing these chart versions", server.Image, strings.Join(missing, ", "), assets, revision)
	}
	if len(unknown) > 0 {
		return fmt.Sprintf("Inconclusive for %s: version pins unavailable for %s", server.Image, strings.Join(unknown, ", ")), nil
	}
	return fmt.Sprintf("Verified %s in %s (rancher/charts %s)", strings.Join(verified, ", "), assets, revision), nil
}

func rancherSystemChartPins(ctx context.Context, image imagelookup.InspectResponse, env map[string]string, images systemChartImageLookup, catalog *helmLabCatalogService) map[string]string {
	pins := map[string]string{
		"rancher-webhook": env["CATTLE_RANCHER_WEBHOOK_VERSION"],
	}
	flags := env["CATTLE_FEATURES"]
	if !(featureDisabled(flags, "multi-cluster-management") && featureDisabled(flags, "fleet")) {
		pins["fleet"] = cmp.Or(env["CATTLE_FLEET_MIN_VERSION"], env["CATTLE_FLEET_VERSION"])
	}
	if !featureDisabled(flags, "turtles") {
		pins["rancher-turtles"] = env["CATTLE_RANCHER_TURTLES_VERSION"]
	}
	// Newer images compile these defaults into the server. Resolve build.yaml
	// through the source revision on the inspected image, never a moving branch.
	if slices.Contains(slices.Collect(maps.Values(pins)), "") {
		defaults := systemChartBuildDefaults(ctx, image, images, catalog)
		for chart, key := range map[string]string{"fleet": "fleetVersion", "rancher-webhook": "webhookVersion", "rancher-turtles": "turtlesVersion"} {
			if value, required := pins[chart]; required && value == "" {
				pins[chart], _ = defaults[key].(string)
			}
		}
	}
	if fleet, ok := pins["fleet"]; ok {
		pins["fleet-crd"] = fleet
	}
	if env["CATTLE_IMPORTED_CLUSTER_VERSION_MANAGEMENT"] != "false" {
		pins["system-upgrade-controller"] = env["CATTLE_SYSTEM_UPGRADE_CONTROLLER_CHART_VERSION"]
	}
	return pins
}

func rancherChartsRevision(config imagelookup.ImageConfig) string {
	return cmp.Or(config.Labels["io.rancher.charts.commit"], imageEnvironment(config.Env)["CHART_COMMIT"])
}

type systemChartIndex map[string][]struct{ Version string }

func (index systemChartIndex) contains(chart, version string) bool {
	return slices.ContainsFunc(index[chart], func(entry struct{ Version string }) bool { return entry.Version == version })
}

func readSystemChartIndex(ctx context.Context, revision string, catalog *helmLabCatalogService) (systemChartIndex, error) {
	data, err := catalog.fetch(ctx, "https://raw.githubusercontent.com/rancher/charts/"+revision+"/index.yaml", 32<<20, false)
	if err != nil {
		return nil, fmt.Errorf("could not read rancher/charts index at %s", revision)
	}
	var index struct {
		APIVersion string `yaml:"apiVersion"`
		Entries    systemChartIndex
	}
	if yaml.Unmarshal(data, &index) != nil || index.APIVersion != "v1" || len(index.Entries) == 0 {
		return nil, fmt.Errorf("invalid rancher/charts index at %s", revision)
	}
	return index.Entries, nil
}

// Public build metadata works on CI runners without a GitHub CLI login. For
// Prime, prefer the declared private revision, then its explicitly linked OSS commit.
func systemChartBuildDefaults(ctx context.Context, image imagelookup.InspectResponse, images systemChartImageLookup, catalog *helmLabCatalogService) map[string]any {
	labels := image.Config.Labels
	revision := labels[imagelookup.RevisionLabel]
	owner, repo, err := imagelookup.ParseGitHubSource(labels[imagelookup.SourceLabel], revision)
	if err != nil || owner != "rancher" || (repo != "rancher" && repo != "rancher-prime") {
		return nil
	}
	if repo == "rancher-prime" {
		source, err := images.FetchSourceBuildYAML(ctx, imagelookup.SourceBuildYAMLRequest{
			Reference: image.Reference, ExpectedDigest: image.Digest, Platform: image.Platform,
		})
		if err == nil {
			return source.Data
		}
		revision = labels[imagelookup.OSSRevisionLabel]
		if !imagelookup.GitRevisionPattern.MatchString(revision) {
			return nil
		}
	}
	raw, err := catalog.fetch(ctx, "https://raw.githubusercontent.com/rancher/rancher/"+revision+"/build.yaml", 1<<20, false)
	var defaults map[string]any
	if err != nil || yaml.Unmarshal(raw, &defaults) != nil {
		return nil
	}
	return defaults
}
