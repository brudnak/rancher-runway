package test

import (
	"encoding/json"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	"io"
	"net/http"
	"strings"
)

func resolveImageSettings(requestedVersion, buildType, resolvedDistro string) (string, string, string, []string) {
	switch resolvedDistro {
	case "prime":
		if buildType == "release" {
			return "registry.rancher.com/rancher/rancher", "", "", []string{"Using Rancher Prime registry because distro=prime was requested explicitly"}
		}
		return "registry.rancher.com/rancher/rancher", "v" + requestedVersion, "", []string{"Using Rancher Prime registry because distro=prime was requested explicitly"}
	case "community-staging":
		imageTag := "v" + requestedVersion
		agentImage := fmt.Sprintf("stgregistry.suse.com/rancher/rancher-agent:%s", imageTag)
		return "stgregistry.suse.com/rancher/rancher", imageTag, agentImage, []string{"Using staging Rancher images because the requested version is not a standard released community build"}
	default:
		if buildType == "release" {
			return "", "", "", []string{"Using released community Rancher chart/image defaults"}
		}
		if requestedVersion == "head" {
			return "", "head", "", []string{"Using released community Rancher chart with the Docker Hub rancher/rancher:head image tag"}
		}
		return "", "v" + requestedVersion, "", []string{"Using released community Rancher chart/image settings"}
	}
}

type commitHeadImageCandidate struct {
	label        string
	registry     string
	rancherImage string
	agentImage   string
}

func resolveCommitHeadImageSettings(requestedVersion string) (string, string, string, []string, error) {
	imageTag := "v" + requestedVersion
	candidates := []commitHeadImageCandidate{
		{
			label:        "staging registry",
			registry:     "stgregistry.suse.com",
			rancherImage: "stgregistry.suse.com/rancher/rancher",
			agentImage:   "stgregistry.suse.com/rancher/rancher-agent:" + imageTag,
		},
	}
	if !imagelookup.IsPrimeCommitHeadRancherVersion(requestedVersion) {
		candidates = append(candidates, commitHeadImageCandidate{
			label:        "Docker Hub",
			registry:     "docker.io",
			rancherImage: "docker.io/rancher/rancher",
			agentImage:   "docker.io/rancher/rancher-agent:" + imageTag,
		}, commitHeadImageCandidate{
			label:        "Rancher registry",
			registry:     "registry.rancher.com",
			rancherImage: "registry.rancher.com/rancher/rancher",
			agentImage:   "registry.rancher.com/rancher/rancher-agent:" + imageTag,
		})
	}

	var misses []string
	for _, candidate := range candidates {
		serverFound, serverErr := registryImageTagExists(candidate.registry, "rancher/rancher", imageTag)
		agentFound, agentErr := registryImageTagExists(candidate.registry, "rancher/rancher-agent", imageTag)
		if serverErr == nil && agentErr == nil && serverFound && agentFound {
			return candidate.rancherImage, imageTag, candidate.agentImage, []string{
				fmt.Sprintf("Found commit-specific head images in %s; using %s:%s and %s", candidate.label, candidate.rancherImage, imageTag, candidate.agentImage),
			}, nil
		}

		var detail []string
		if serverErr != nil {
			detail = append(detail, "rancher/rancher lookup error: "+serverErr.Error())
		} else if !serverFound {
			detail = append(detail, "rancher/rancher missing")
		}
		if agentErr != nil {
			detail = append(detail, "rancher/rancher-agent lookup error: "+agentErr.Error())
		} else if !agentFound {
			detail = append(detail, "rancher/rancher-agent missing")
		}
		misses = append(misses, fmt.Sprintf("%s (%s)", candidate.label, strings.Join(detail, "; ")))
	}

	return "", "", "", nil, fmt.Errorf("commit-specific head tag %s was not found as a matching Rancher server and agent image pair in checked registries: %s", imageTag, strings.Join(misses, "; "))
}

func isExactCommunityPrereleaseChart(chartRepoAlias string) bool {
	return chartRepoAlias == "rancher-alpha" || chartRepoAlias == "rancher-latest"
}

func isExactStagingPrereleaseChart(chartRepoAlias string) bool {
	return strings.HasPrefix(chartRepoAlias, "optimus-") || chartRepoAlias == "rancher-optimus-alpha" || chartRepoAlias == "optimus-s3"
}

func validateResolvedRancherImages(rancherImage, rancherImageTag, agentImage string) error {
	var images []string
	if rancherImage != "" && rancherImageTag != "" {
		images = append(images, rancherImage+":"+rancherImageTag)
	}
	if rancherImage == "" && rancherImageTag != "" {
		images = append(images, "docker.io/rancher/rancher:"+rancherImageTag)
	}
	if agentImage != "" {
		images = append(images, agentImage)
	}

	for _, image := range images {
		registry, repository, tag, err := imagelookup.ParseRegistryImage(image)
		if err != nil {
			return err
		}
		found, err := registryImageTagExists(registry, repository, tag)
		if err != nil {
			return fmt.Errorf("%s: %w", image, err)
		}
		if !found {
			return fmt.Errorf("%s was not found in registry", image)
		}
	}
	return nil
}

func registryImageTagExists(registry, repository, tag string) (bool, error) {
	manifestURL := fmt.Sprintf("%s/v2/%s/manifests/%s", registryBaseURL(registry), repository, tag)
	req, err := http.NewRequest(http.MethodHead, manifestURL, nil)
	if err != nil {
		return false, err
	}
	setRegistryManifestAcceptHeader(req)

	resp, err := rancherRegistryHTTPClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	case http.StatusUnauthorized:
		token, err := registryBearerToken(resp.Header.Get("WWW-Authenticate"))
		if err != nil {
			return false, err
		}
		return registryImageTagExistsWithToken(manifestURL, token)
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return false, fmt.Errorf("registry manifest lookup failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
}

func registryImageTagExistsWithToken(manifestURL, token string) (bool, error) {
	req, err := http.NewRequest(http.MethodHead, manifestURL, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	setRegistryManifestAcceptHeader(req)

	resp, err := rancherRegistryHTTPClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return false, fmt.Errorf("registry authenticated manifest lookup failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
}

func registryBearerToken(authenticate string) (string, error) {
	params, err := parseRegistryBearerChallenge(authenticate)
	if err != nil {
		return "", err
	}
	realm := params["realm"]
	if realm == "" {
		return "", fmt.Errorf("registry Bearer challenge missing realm")
	}

	req, err := http.NewRequest(http.MethodGet, realm, nil)
	if err != nil {
		return "", err
	}
	query := req.URL.Query()
	if service := params["service"]; service != "" {
		query.Set("service", service)
	}
	if scope := params["scope"]; scope != "" {
		query.Set("scope", scope)
	}
	req.URL.RawQuery = query.Encode()

	resp, err := rancherRegistryHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("registry token request failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var tokenResponse struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResponse); err != nil {
		return "", err
	}
	token := tokenResponse.Token
	if token == "" {
		token = tokenResponse.AccessToken
	}
	if token == "" {
		return "", fmt.Errorf("registry token response did not include a token")
	}
	return token, nil
}

func parseRegistryBearerChallenge(value string) (map[string]string, error) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(strings.ToLower(value), "bearer ") {
		return nil, fmt.Errorf("unsupported registry auth challenge %q", value)
	}
	value = strings.TrimSpace(value[len("Bearer "):])
	params := map[string]string{}
	for _, part := range strings.Split(value, ",") {
		key, rawValue, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		params[strings.ToLower(strings.TrimSpace(key))] = strings.Trim(strings.TrimSpace(rawValue), `"`)
	}
	return params, nil
}

func setRegistryManifestAcceptHeader(req *http.Request) {
	req.Header.Set("Accept", strings.Join([]string{
		"application/vnd.oci.image.index.v1+json",
		"application/vnd.oci.image.manifest.v1+json",
		"application/vnd.docker.distribution.manifest.list.v2+json",
		"application/vnd.docker.distribution.manifest.v2+json",
	}, ", "))
}

func registryBaseURL(registry string) string {
	if base := rancherRegistryBaseURLs[registry]; base != "" {
		return strings.TrimRight(base, "/")
	}
	if registry == "docker.io" {
		return "https://registry-1.docker.io"
	}
	return "https://" + registry
}
