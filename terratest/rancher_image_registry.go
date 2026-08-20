package test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"unicode"

	"github.com/brudnak/ha-rancher-rke2/terratest/settings"
	"github.com/google/go-containerregistry/pkg/authn"
)

const (
	imageLookupVersionLabel            = "org.opencontainers.image.version"
	imageLookupCanonicalReferenceLabel = "org.opensuse.reference"
)

type rancherImageProvenance struct {
	Reference          string
	Digest             string
	BuildVersion       string
	SourceURL          string
	Revision           string
	OSSRevision        string
	CanonicalReference string
}

type preferredRancherImageResolution struct {
	Registry          string
	RegistryLabel     string
	RancherImage      string
	RancherImageTag   string
	AgentImage        string
	RancherProvenance rancherImageProvenance
	AgentProvenance   rancherImageProvenance
}

type rancherImageInspectFunc func(context.Context, string) (rancherImageProvenance, bool, error)

var inspectPreferredRancherImage rancherImageInspectFunc = inspectRancherImageReference

func resolvePreferredRancherImageSettings(requestedVersion string, registries []string) (*preferredRancherImageResolution, error) {
	registries, err := settings.NormalizePreferredImageRegistries(registries)
	if err != nil {
		return nil, err
	}
	if len(registries) == 0 {
		return nil, nil
	}

	tag := normalizeDockerRancherTag(normalizeVersionInput(requestedVersion))
	if tag == "" {
		return nil, fmt.Errorf("cannot verify preferred image registries without a Rancher image tag")
	}

	misses := make([]string, 0, len(registries))
	for _, registry := range registries {
		serverRepository := registry + "/rancher/rancher"
		agentRepository := registry + "/rancher/rancher-agent"
		serverReference := serverRepository + ":" + tag
		agentReference := agentRepository + ":" + tag

		serverCtx, serverCancel := context.WithTimeout(context.Background(), rancherResolverHTTPTimeout)
		serverProvenance, serverFound, serverErr := inspectPreferredRancherImage(serverCtx, serverReference)
		serverCancel()
		if serverErr != nil {
			return nil, fmt.Errorf("could not verify %s in %s: %w", serverReference, preferredRancherRegistryLabel(registry), serverErr)
		}
		if !serverFound {
			misses = append(misses, fmt.Sprintf("%s missing %s", preferredRancherRegistryLabel(registry), serverReference))
			continue
		}

		agentCtx, agentCancel := context.WithTimeout(context.Background(), rancherResolverHTTPTimeout)
		agentProvenance, agentFound, agentErr := inspectPreferredRancherImage(agentCtx, agentReference)
		agentCancel()
		if agentErr != nil {
			return nil, fmt.Errorf("could not verify %s in %s: %w", agentReference, preferredRancherRegistryLabel(registry), agentErr)
		}

		if serverFound && agentFound {
			if err := validateExactHeadImagePair(tag, serverProvenance, agentProvenance); err != nil {
				return nil, err
			}
			return &preferredRancherImageResolution{
				Registry:          registry,
				RegistryLabel:     preferredRancherRegistryLabel(registry),
				RancherImage:      serverRepository,
				RancherImageTag:   tag,
				AgentImage:        agentReference,
				RancherProvenance: serverProvenance,
				AgentProvenance:   agentProvenance,
			}, nil
		}

		missing := make([]string, 0, 2)
		if !agentFound {
			missing = append(missing, agentReference)
		}
		misses = append(misses, fmt.Sprintf("%s missing %s", preferredRancherRegistryLabel(registry), strings.Join(missing, " and ")))
	}

	return nil, fmt.Errorf("preferred registries do not contain a complete Rancher server/agent image pair for %s: %s. No unselected registry fallback was attempted", tag, strings.Join(misses, "; "))
}

func inspectExplicitRancherImagePair(serverRepository, tag, agentReference string) (*preferredRancherImageResolution, error) {
	serverReference := strings.TrimSpace(serverRepository) + ":" + strings.TrimSpace(tag)
	registry, _, _, err := parseRegistryImage(serverReference)
	if err != nil {
		return nil, err
	}

	serverCtx, serverCancel := context.WithTimeout(context.Background(), rancherResolverHTTPTimeout)
	serverProvenance, serverFound, serverErr := inspectPreferredRancherImage(serverCtx, serverReference)
	serverCancel()
	if serverErr != nil {
		return nil, fmt.Errorf("could not inspect exact Rancher server image %s: %w", serverReference, serverErr)
	}
	if !serverFound {
		return nil, fmt.Errorf("exact Rancher server image %s was not found", serverReference)
	}

	agentCtx, agentCancel := context.WithTimeout(context.Background(), rancherResolverHTTPTimeout)
	agentProvenance, agentFound, agentErr := inspectPreferredRancherImage(agentCtx, agentReference)
	agentCancel()
	if agentErr != nil {
		return nil, fmt.Errorf("could not inspect exact Rancher agent image %s: %w", agentReference, agentErr)
	}
	if !agentFound {
		return nil, fmt.Errorf("exact Rancher agent image %s was not found", agentReference)
	}
	if err := validateExactHeadImagePair(tag, serverProvenance, agentProvenance); err != nil {
		return nil, err
	}

	return &preferredRancherImageResolution{
		Registry:          registry,
		RegistryLabel:     preferredRancherRegistryLabel(registry),
		RancherImage:      strings.TrimSpace(serverRepository),
		RancherImageTag:   strings.TrimSpace(tag),
		AgentImage:        strings.TrimSpace(agentReference),
		RancherProvenance: serverProvenance,
		AgentProvenance:   agentProvenance,
	}, nil
}

func inspectRancherImageReference(ctx context.Context, reference string) (rancherImageProvenance, bool, error) {
	service := newImageLookupService()
	service.keychain = preferredRancherImageKeychain{}
	return inspectRancherImageReferenceWithService(ctx, service, reference)
}

func inspectRancherImageReferenceWithService(ctx context.Context, service *imageLookupService, reference string) (rancherImageProvenance, bool, error) {
	response, err := service.Inspect(ctx, imageLookupInspectRequest{
		Reference:        reference,
		Platform:         "linux/amd64",
		IncludeBuildYAML: false,
		SkipTagMetadata:  true,
	})
	if err != nil {
		if imageLookupRegistryNotFound(err) {
			return rancherImageProvenance{Reference: reference}, false, nil
		}
		return rancherImageProvenance{Reference: reference}, false, fmt.Errorf("%s", imageLookupSafeError(err))
	}

	labels := response.Config.Labels
	return rancherImageProvenance{
		Reference:          response.Reference,
		Digest:             response.Digest,
		BuildVersion:       safeOCIProvenanceLabel(labels[imageLookupVersionLabel]),
		SourceURL:          safeOCIProvenanceLabel(labels[imageLookupSourceLabel]),
		Revision:           safeOCIProvenanceLabel(labels[imageLookupRevisionLabel]),
		OSSRevision:        safeOCIProvenanceLabel(labels[imageLookupOSSRevisionLabel]),
		CanonicalReference: safeOCIProvenanceLabel(labels[imageLookupCanonicalReferenceLabel]),
	}, true, nil
}

func validateExactHeadImagePair(tag string, server, agent rancherImageProvenance) error {
	normalizedTag := normalizeVersionInput(tag)
	if !isCommitHeadRancherVersion(normalizedTag) {
		return nil
	}
	expectedTag := normalizeDockerRancherTag(normalizedTag)
	_, _, serverCanonicalTag, serverErr := parseRegistryImage(server.CanonicalReference)
	_, _, agentCanonicalTag, agentErr := parseRegistryImage(agent.CanonicalReference)
	if serverErr != nil || agentErr != nil || serverCanonicalTag == "" || agentCanonicalTag == "" {
		return fmt.Errorf("exact Rancher head image pair %s did not declare canonical server and agent org.opensuse.reference labels", expectedTag)
	}
	if serverCanonicalTag != expectedTag || agentCanonicalTag != expectedTag {
		return fmt.Errorf("exact Rancher head image pair %s has mismatched canonical tags: server %s, agent %s", expectedTag, serverCanonicalTag, agentCanonicalTag)
	}
	return nil
}

// Preferred-image verification must match what provisioned nodes can pull.
// Docker Hub credentials are propagated to RKE2/K3s; credentials from the
// operator's local Docker keychain for other registries are not.
type preferredRancherImageKeychain struct{}

func (preferredRancherImageKeychain) Resolve(resource authn.Resource) (authn.Authenticator, error) {
	registry := imageLookupRegistryForDisplay(resource.RegistryStr())
	username := strings.TrimSpace(os.Getenv("DOCKERHUB_USERNAME"))
	password := os.Getenv("DOCKERHUB_PASSWORD")
	if registry == "docker.io" && username != "" && password != "" {
		return authn.FromConfig(authn.AuthConfig{Username: username, Password: password}), nil
	}
	return authn.Anonymous, nil
}

func preferredRancherRegistryLabel(registry string) string {
	if registry == "registry.rancher.com" {
		return "Rancher Prime"
	}
	return imageLookupRegistryLabel(registry)
}

func safeOCIProvenanceLabel(value string) string {
	value = strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return ' '
		}
		return character
	}, value)
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	runes := []rune(value)
	if len(runes) > 512 {
		value = string(runes[:512])
	}
	return value
}

func rancherImageSourceCommitURL(source, revision string) string {
	owner, repository, err := imageLookupParseGitHubSource(strings.TrimSpace(source), strings.TrimSpace(revision))
	if err != nil {
		return ""
	}
	return fmt.Sprintf("https://github.com/%s/%s/commit/%s", owner, repository, revision)
}

func preferredImageResolutionExplanation(resolution *preferredRancherImageResolution, registries []string) []string {
	if resolution == nil {
		return nil
	}
	return []string{
		fmt.Sprintf("Verified the requested Rancher server and agent image pair in %s before provisioning", resolution.RegistryLabel),
		fmt.Sprintf("Preferred registry order was %s; selected the first complete pair", strings.Join(registries, ", ")),
		"Recorded resolution-time OCI digests; mutable image tags are not digest-pinned by Helm",
	}
}
