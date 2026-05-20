package test

import (
	"fmt"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/gruntwork-io/terratest/modules/terraform"
	"github.com/spf13/viper"
)

func runLinodeDockerSetup(t *testing.T) {
	totalInstances := configuredRancherInstanceCount()
	if totalInstances < 1 {
		t.Fatal("linode-docker-cattle requires at least one Rancher instance")
	}
	if err := validateLinodeDockerConfig(totalInstances); err != nil {
		t.Fatalf("Linode Docker preflight failed: %v", err)
	}
	versions := linodeRancherVersions(totalInstances)
	imageRepo, _, _, err := resolveLinodeDockerImageSource(versions)
	if err != nil {
		t.Fatalf("Linode Docker image preflight failed: %v", err)
	}
	viper.Set("linode.dockerhub", imageRepo)

	terraformOptions := getTerraformOptions(t, totalInstances)
	terraform.InitAndApply(t, terraformOptions)

	outputs := getTerraformOutputs(t, terraformOptions)
	for i := 1; i <= totalInstances; i++ {
		log.Printf("[linode-docker] Rancher %d: %s", i, outputs[fmt.Sprintf("linode_%d_rancher_url", i)])
		log.Printf("[linode-docker] Linode %d IP: %s", i, outputs[fmt.Sprintf("linode_%d_ip", i)])
	}
}

func validateLinodeDockerConfig(totalInstances int) error {
	loadSecretEnvironmentFromZProfile()
	if totalInstances < 1 {
		return fmt.Errorf("total_has must be at least 1")
	}
	if strings.TrimSpace(viper.GetString("tf_vars.aws_prefix")) == "" {
		return fmt.Errorf("tf_vars.aws_prefix is required")
	}
	if strings.TrimSpace(viper.GetString("tf_vars.aws_route53_fqdn")) == "" {
		return fmt.Errorf("tf_vars.aws_route53_fqdn is required")
	}
	if strings.TrimSpace(viper.GetString("rancher.bootstrap_password")) == "" {
		return fmt.Errorf("rancher.bootstrap_password is required")
	}
	if strings.TrimSpace(linodeAccessToken()) == "" {
		return fmt.Errorf("LINODE_TOKEN or linode.access_token is required")
	}
	if strings.TrimSpace(linodeRootPassword()) == "" {
		return fmt.Errorf("linode.ssh_root_password is required")
	}
	if err := validateLinodeRootPassword(linodeRootPassword()); err != nil {
		return err
	}
	if len(linodeRancherVersions(totalInstances)) != totalInstances {
		return fmt.Errorf("rancher.versions must contain %d version(s)", totalInstances)
	}
	return nil
}

func prepareLinodeDockerPlans(totalInstances int) ([]*RancherResolvedPlan, error) {
	if err := validateLinodeDockerConfig(totalInstances); err != nil {
		return nil, err
	}
	versions := linodeRancherVersions(totalInstances)
	imageRepo, imageLabel, imageFindings, err := resolveLinodeDockerImageSource(versions)
	if err != nil {
		return nil, err
	}
	viper.Set("linode.dockerhub", imageRepo)
	plans := make([]*RancherResolvedPlan, 0, len(versions))
	for _, version := range versions {
		tag := normalizeDockerRancherTag(version)
		plans = append(plans, &RancherResolvedPlan{
			Mode:                  "linode-docker-cattle",
			RequestedVersion:      version,
			RequestedDistro:       strings.TrimSpace(viper.GetString("rancher.distro")),
			ResolvedDistro:        "docker",
			RancherImage:          imageRepo,
			RancherImageTag:       tag,
			ChartVersion:          tag,
			CompatibilityBaseline: "Docker image manifest",
			Explanation: append([]string{
				fmt.Sprintf("Selected %s because %s:%s was found.", imageLabel, imageRepo, tag),
				"Creates one Linode instance and one Route53 DNS A record for this Rancher version.",
				"Runs Rancher as a privileged Docker container with ACME DNS using the generated Route53 name.",
			}, imageFindings...),
		})
	}
	return plans, nil
}

func linodeAccessToken() string {
	for _, key := range []string{"LINODE_TOKEN", "LINODE_ACCESS_TOKEN"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return strings.TrimSpace(viper.GetString("linode.access_token"))
}

func linodeRootPassword() string {
	return strings.TrimSpace(viper.GetString("linode.ssh_root_password"))
}

func validateLinodeRootPassword(password string) error {
	password = strings.TrimSpace(password)
	if len(password) < 7 {
		return fmt.Errorf("linode.ssh_root_password must be at least 7 characters")
	}
	if len(password) > 128 {
		return fmt.Errorf("linode.ssh_root_password must be 128 characters or fewer")
	}

	classCount := 0
	hasUpper := false
	hasLower := false
	hasDigit := false
	hasPunct := false
	for _, char := range password {
		switch {
		case char >= 'A' && char <= 'Z':
			hasUpper = true
		case char >= 'a' && char <= 'z':
			hasLower = true
		case char >= '0' && char <= '9':
			hasDigit = true
		case (char >= 32 && char <= 47) || (char >= 58 && char <= 64) || (char >= 91 && char <= 96) || (char >= 123 && char <= 126):
			hasPunct = true
		case char == '\t':
			continue
		default:
			return fmt.Errorf("linode.ssh_root_password must use alphanumeric, punctuation, space, or tab characters only")
		}
	}
	for _, present := range []bool{hasUpper, hasLower, hasDigit, hasPunct} {
		if present {
			classCount++
		}
	}
	if classCount < 2 {
		return fmt.Errorf("linode.ssh_root_password must contain at least two of uppercase letters, lowercase letters, digits, and punctuation")
	}
	return nil
}

func linodeRegion() string {
	if value := strings.TrimSpace(viper.GetString("linode.region")); value != "" {
		return value
	}
	return "us-west"
}

func linodeInstanceType() string {
	if value := strings.TrimSpace(viper.GetString("linode.type")); value != "" {
		return value
	}
	return "g6-standard-6"
}

func linodeImage() string {
	if value := strings.TrimSpace(viper.GetString("linode.image")); value != "" {
		return value
	}
	return "linode/ubuntu22.04"
}

func linodeDockerHub() string {
	if value := strings.TrimSpace(os.Getenv("RANCHER_DOCKERHUB")); value != "" {
		return value
	}
	if value := strings.TrimSpace(viper.GetString("linode.dockerhub")); value != "" {
		value = normalizeLinodeDockerHubSelection(value)
		if value != "auto" {
			return value
		}
	}
	return "rancher/rancher"
}

type linodeDockerImageSource struct {
	Key        string
	Label      string
	Repository string
}

type linodeDockerImageSearchResult struct {
	Key        string `json:"key"`
	Label      string `json:"label"`
	Repository string `json:"repository"`
	Image      string `json:"image"`
	Tag        string `json:"tag"`
	Found      bool   `json:"found"`
	Error      string `json:"error,omitempty"`
}

var linodeDockerImageSources = []linodeDockerImageSource{
	{Key: "dockerhub", Label: "Docker Hub rancher/rancher", Repository: "docker.io/rancher/rancher"},
	{Key: "staging", Label: "SUSE staging registry", Repository: "stgregistry.suse.com/rancher/rancher"},
	{Key: "prime", Label: "Rancher Prime registry", Repository: "registry.rancher.com/rancher/rancher"},
	{Key: "suse", Label: "SUSE registry", Repository: "registry.suse.com/rancher/rancher"},
}

func normalizeLinodeDockerHubSelection(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "auto"
	}
	lower := strings.ToLower(value)
	switch lower {
	case "auto":
		return "auto"
	case "dockerhub", "docker.io/rancher/rancher", "rancher/rancher":
		return "docker.io/rancher/rancher"
	case "staging", "stg", "stgregistry.suse.com/rancher/rancher":
		return "stgregistry.suse.com/rancher/rancher"
	case "prime", "registry.rancher.com/rancher/rancher":
		return "registry.rancher.com/rancher/rancher"
	case "suse", "registry.suse.com/rancher/rancher":
		return "registry.suse.com/rancher/rancher"
	default:
		return value
	}
}

func resolveLinodeDockerImageSource(versions []string) (string, string, []string, error) {
	requested := normalizeLinodeDockerHubSelection(viper.GetString("linode.dockerhub"))
	if envValue := strings.TrimSpace(os.Getenv("RANCHER_DOCKERHUB")); envValue != "" {
		requested = normalizeLinodeDockerHubSelection(envValue)
	}
	tags := make([]string, 0, len(versions))
	for _, version := range versions {
		tags = append(tags, normalizeDockerRancherTag(version))
	}

	candidates := linodeDockerImageSources
	if requested != "" && requested != "auto" {
		candidates = []linodeDockerImageSource{linodeDockerImageSourceForRepository(requested)}
	}

	var found []linodeDockerImageSource
	var misses []string
	for _, candidate := range candidates {
		ok, details := linodeDockerImageSourceHasTags(candidate.Repository, tags)
		if ok {
			found = append(found, candidate)
			continue
		}
		misses = append(misses, fmt.Sprintf("%s (%s)", candidate.Label, strings.Join(details, "; ")))
	}
	if requested != "" && requested != "auto" {
		if len(found) > 0 {
			return found[0].Repository, found[0].Label, []string{fmt.Sprintf("Explicit Linode Docker image source selected: %s.", found[0].Label)}, nil
		}
		return "", "", nil, fmt.Errorf("selected Linode Docker image source %s did not contain all requested tags %s: %s", requested, strings.Join(tags, ", "), strings.Join(misses, "; "))
	}
	if len(found) == 0 {
		return "", "", nil, fmt.Errorf("could not find requested Linode Docker image tag(s) %s in checked sources: %s", strings.Join(tags, ", "), strings.Join(misses, "; "))
	}

	findings := []string{}
	if len(found) > 1 {
		options := make([]string, 0, len(found))
		for _, source := range found {
			options = append(options, source.Label+" ("+source.Repository+")")
		}
		findings = append(findings, "Also found matching tags in: "+strings.Join(options[1:], ", "))
	}
	return found[0].Repository, found[0].Label, findings, nil
}

func searchLinodeDockerImageSources(version, customImage string) (string, []linodeDockerImageSearchResult, error) {
	customRepository, customTag, err := normalizeCustomLinodeDockerImage(customImage)
	if err != nil {
		return "", nil, err
	}
	tag := normalizeDockerRancherTag(normalizeVersionInput(version))
	if tag == "" && customTag != "" {
		tag = customTag
	}
	if strings.TrimSpace(tag) == "" {
		return "", nil, fmt.Errorf("Rancher image tag is required")
	}

	sources := append([]linodeDockerImageSource(nil), linodeDockerImageSources...)
	if customRepository != "" {
		sources = append(sources, linodeDockerImageSource{Key: "custom", Label: "Custom image source", Repository: customRepository})
	}

	results := make([]linodeDockerImageSearchResult, 0, len(sources))
	for _, source := range sources {
		resultTag := tag
		if source.Key == "custom" && customTag != "" {
			resultTag = customTag
		}
		result := linodeDockerImageSearchResult{
			Key:        source.Key,
			Label:      source.Label,
			Repository: source.Repository,
			Image:      source.Repository + ":" + resultTag,
			Tag:        resultTag,
		}
		registry, imageRepository, err := parseRegistryRepository(source.Repository)
		if err != nil {
			result.Error = err.Error()
			results = append(results, result)
			continue
		}
		found, err := registryImageTagExists(registry, imageRepository, resultTag)
		if err != nil {
			result.Error = err.Error()
			results = append(results, result)
			continue
		}
		result.Found = found
		results = append(results, result)
	}
	return tag, results, nil
}

func normalizeCustomLinodeDockerImage(value string) (repository string, tag string, err error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", nil
	}
	value = strings.TrimPrefix(value, "docker://")
	value = strings.TrimPrefix(value, "https://")
	value = strings.TrimPrefix(value, "http://")
	value = strings.TrimSuffix(value, "/")
	if value == "" {
		return "", "", fmt.Errorf("custom image source is empty")
	}

	lastSlash := strings.LastIndex(value, "/")
	lastColon := strings.LastIndex(value, ":")
	if lastColon > lastSlash {
		tag = strings.TrimSpace(value[lastColon+1:])
		value = strings.TrimSpace(value[:lastColon])
		if tag == "" {
			return "", "", fmt.Errorf("custom image tag is empty")
		}
	}
	if strings.Count(value, "/") == 0 {
		return "", "", fmt.Errorf("custom image source must look like docker.io/user/image or registry.example.com/path/image")
	}
	return normalizeLinodeDockerHubSelection(value), tag, nil
}

func linodeDockerImageSourceForRepository(repository string) linodeDockerImageSource {
	repository = normalizeLinodeDockerHubSelection(repository)
	for _, candidate := range linodeDockerImageSources {
		if candidate.Repository == repository {
			return candidate
		}
	}
	return linodeDockerImageSource{Key: "custom", Label: "Custom image source", Repository: repository}
}

func linodeDockerImageSourceHasTags(repository string, tags []string) (bool, []string) {
	registry, imageRepository, err := parseRegistryRepository(repository)
	if err != nil {
		return false, []string{err.Error()}
	}
	var details []string
	for _, tag := range tags {
		found, lookupErr := registryImageTagExists(registry, imageRepository, tag)
		if lookupErr != nil {
			details = append(details, fmt.Sprintf("%s lookup error: %v", tag, lookupErr))
			continue
		}
		if !found {
			details = append(details, tag+" missing")
			continue
		}
		details = append(details, tag+" found")
	}
	for _, detail := range details {
		if strings.Contains(detail, "missing") || strings.Contains(detail, "error") {
			return false, details
		}
	}
	return true, details
}

func parseRegistryRepository(repository string) (registry string, imageRepository string, err error) {
	repository = strings.TrimSpace(repository)
	if repository == "" {
		return "", "", fmt.Errorf("image repository is required")
	}
	registry, imageRepository, ok := strings.Cut(repository, "/")
	if !ok || imageRepository == "" {
		return "docker.io", repository, nil
	}
	if !strings.Contains(registry, ".") && !strings.Contains(registry, ":") && registry != "localhost" {
		return "docker.io", repository, nil
	}
	return registry, imageRepository, nil
}

func linodeDockerInstallVersion() string {
	if value := strings.TrimSpace(viper.GetString("linode.docker_install_version")); value != "" {
		return value
	}
	return "27.1"
}

func linodeDockerAWSRegion() string {
	if value := strings.TrimSpace(viper.GetString("linode.aws_region")); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("AWS_REGION")); value != "" {
		return value
	}
	return "us-east-2"
}

func linodeTags() []string {
	tags := nonEmptyStringSlice(viper.GetStringSlice("linode.tags"))
	if len(tags) == 0 {
		tags = append(tags, "rancher-runway")
	}
	if owner := strings.TrimSpace(settingsOwnerTag()); owner != "" {
		tags = append(tags, owner)
	}
	if runID := currentTerraformRunID(); runID != "" {
		tags = append(tags, "run-"+runID)
	}
	return tags
}

func settingsOwnerTag() string {
	first := strings.ToLower(strings.TrimSpace(viper.GetString("user.first_name")))
	last := strings.ToLower(strings.TrimSpace(viper.GetString("user.last_name")))
	return strings.Trim(strings.Join([]string{first, last}, "-"), "-")
}

func linodeRancherVersions(totalInstances int) []string {
	versions := nonEmptyStringSlice(viper.GetStringSlice("rancher.versions"))
	if len(versions) == 0 {
		if version := normalizeVersionInput(viper.GetString("rancher.version")); version != "" {
			versions = []string{version}
		}
	}
	if totalInstances > 0 && len(versions) > totalInstances {
		return versions[:totalInstances]
	}
	return versions
}

func linodeRancherInstances(totalInstances int) []map[string]string {
	versions := linodeRancherVersions(totalInstances)
	instances := make([]map[string]string, 0, len(versions))
	for _, version := range versions {
		instances = append(instances, map[string]string{"rancher_version": normalizeDockerRancherTag(version)})
	}
	return instances
}

func normalizeDockerRancherTag(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return version
	}
	if version == "head" || strings.HasPrefix(version, "v") {
		return version
	}
	return "v" + version
}
