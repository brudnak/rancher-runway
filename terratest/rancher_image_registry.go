package test

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/brudnak/ha-rancher-rke2/terratest/settings"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

const (
	imageLookupVersionLabel            = "org.opencontainers.image.version"
	imageLookupCanonicalReferenceLabel = "org.opensuse.reference"
)

type rancherImageProvenance struct {
	Reference          string
	Digest             string
	CreatedAt          time.Time
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
type rancherImageTagListFunc func(context.Context, string, string) ([]string, error)

type preferredRancherImageLookupServiceContextKey struct{}

var inspectPreferredRancherImage rancherImageInspectFunc = inspectRancherImageReference
var listPatchHeadStagingImageTags rancherImageTagListFunc = listRancherImageTags

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
			if isPrimeCommitHeadRancherVersion(requestedVersion) {
				if err := validatePatchHeadServerProvenance(requestedVersion, serverProvenance); err != nil {
					return nil, err
				}
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
	service, _ := ctx.Value(preferredRancherImageLookupServiceContextKey{}).(*imageLookupService)
	if service == nil {
		service = newPreferredRancherImageLookupService()
		defer service.closeIdleConnections()
	}
	return inspectRancherImageReferenceWithService(ctx, service, reference)
}

func newPreferredRancherImageLookupService() *imageLookupService {
	service := newImageLookupService()
	service.keychain = preferredRancherImageKeychain{}
	return service
}

func inspectRancherImageReferenceWithService(ctx context.Context, service *imageLookupService, reference string) (rancherImageProvenance, bool, error) {
	platform := "linux/amd64"
	if parsed, parseErr := service.parseReference(reference, true); parseErr == nil && parsed.tag != "" {
		if architecture, _ := imageLookupTagArchitecture(parsed.tag); architecture != "" && architecture != "multi" && architecture != "unknown" {
			platform = "linux/" + architecture
		}
	}
	response, err := service.Inspect(ctx, imageLookupInspectRequest{
		Reference:        reference,
		Platform:         platform,
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
	createdAt, _ := time.Parse(time.RFC3339Nano, response.CreatedAt)
	return rancherImageProvenance{
		Reference:          response.Reference,
		Digest:             response.Digest,
		CreatedAt:          createdAt,
		BuildVersion:       safeOCIProvenanceLabel(labels[imageLookupVersionLabel]),
		SourceURL:          safeOCIProvenanceLabel(labels[imageLookupSourceLabel]),
		Revision:           safeOCIProvenanceLabel(labels[imageLookupRevisionLabel]),
		OSSRevision:        safeOCIProvenanceLabel(labels[imageLookupOSSRevisionLabel]),
		CanonicalReference: safeOCIProvenanceLabel(labels[imageLookupCanonicalReferenceLabel]),
	}, true, nil
}

type patchHeadStagingCandidate struct {
	version    string
	resolution *preferredRancherImageResolution
	completeAt time.Time
	err        error
	lookupErr  bool
}

type patchHeadStagingResolution struct {
	version string
	images  *preferredRancherImageResolution
}

type patchHeadStagingResolver func(string) (string, *preferredRancherImageResolution, error)

func resolveCachedPatchHeadStagingBundle(cache map[string]patchHeadStagingResolution, requestedVersion string, resolver patchHeadStagingResolver) (string, *preferredRancherImageResolution, error) {
	key := normalizeVersionInput(requestedVersion)
	if cached, ok := cache[key]; ok {
		return cached.version, cached.images, nil
	}
	version, images, err := resolver(key)
	if err != nil {
		return "", nil, err
	}
	cache[key] = patchHeadStagingResolution{version: version, images: images}
	return version, images, nil
}

// resolvePatchHeadStagingBundle treats X.Y.Z-head as a Runway selector, not a
// literal OCI tag. Rancher staging publishes immutable X.Y.Z-SHA-head tags, so
// the selector is resolved to the newest matching, provenance-validated server
// and agent image pair. Normal chart resolution then prefers an exact eligible
// chart and retains the existing compatible-chart fallback when chart
// publication lags the images.
func resolvePatchHeadStagingBundle(requestedVersion string) (string, *preferredRancherImageResolution, error) {
	startedAt := time.Now()
	requestedVersion = normalizeVersionInput(requestedVersion)
	if !isPatchHeadAliasRancherVersion(requestedVersion) {
		return "", nil, fmt.Errorf("%s is not a patch-qualified Rancher head selector", requestedVersion)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*rancherResolverHTTPTimeout)
	defer cancel()
	log.Printf("[resolver] Resolving Rancher %s from SUSE staging image tags", requestedVersion)
	tags, err := listPatchHeadStagingImageTags(ctx, "stgregistry.suse.com", "rancher/rancher")
	if err != nil {
		return "", nil, fmt.Errorf("list immutable Rancher head images in SUSE staging: %w", err)
	}

	patchVersion := strings.TrimSuffix(requestedVersion, "-head")
	seen := map[string]bool{}
	versions := make([]string, 0)
	for _, tag := range tags {
		version := normalizeVersionInput(tag)
		if !isPrimeCommitHeadRancherVersion(version) || !strings.HasPrefix(version, patchVersion+"-") || seen[version] {
			continue
		}
		seen[version] = true
		versions = append(versions, version)
	}
	if len(versions) == 0 {
		return "", nil, fmt.Errorf("no immutable %s-SHA-head server images were found in SUSE staging", patchVersion)
	}
	log.Printf("[resolver] SUSE staging tag scan matched %d immutable %s-SHA-head candidate(s) out of %d tags", len(versions), patchVersion, len(tags))

	imageLookup := newPreferredRancherImageLookupService()
	defer imageLookup.closeIdleConnections()
	ctx = context.WithValue(ctx, preferredRancherImageLookupServiceContextKey{}, imageLookup)

	jobs := make(chan string)
	results := make(chan patchHeadStagingCandidate, len(versions))
	workerCount := 4
	if len(versions) < workerCount {
		workerCount = len(versions)
	}
	log.Printf("[resolver] Inspecting %d SUSE staging Rancher server/agent image pair(s) with %d workers", len(versions), workerCount)

	var workers sync.WaitGroup
	for worker := 0; worker < workerCount; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for version := range jobs {
				results <- inspectPatchHeadStagingCandidate(ctx, version)
			}
		}()
	}
	go func() {
		defer close(results)
		for _, version := range versions {
			select {
			case jobs <- version:
			case <-ctx.Done():
				close(jobs)
				workers.Wait()
				return
			}
		}
		close(jobs)
		workers.Wait()
	}()

	complete := make([]patchHeadStagingCandidate, 0, len(versions))
	misses := make([]string, 0, len(versions))
	lookupErrors := make([]string, 0)
	for candidate := range results {
		if candidate.err != nil {
			detail := candidate.version + ": " + candidate.err.Error()
			if candidate.lookupErr {
				lookupErrors = append(lookupErrors, detail)
			} else {
				misses = append(misses, detail)
			}
			continue
		}
		complete = append(complete, candidate)
	}
	if err := ctx.Err(); err != nil {
		return "", nil, fmt.Errorf("resolve %s from SUSE staging: %w", requestedVersion, err)
	}
	if len(lookupErrors) > 0 {
		sort.Strings(lookupErrors)
		return "", nil, fmt.Errorf("could not safely resolve %s from SUSE staging because image inspection failed: %s", requestedVersion, strings.Join(lookupErrors, "; "))
	}
	if len(complete) == 0 {
		sort.Strings(misses)
		detail := strings.Join(misses, "; ")
		if len(misses) > 5 {
			detail = strings.Join(misses[:5], "; ") + fmt.Sprintf("; and %d more", len(misses)-5)
		}
		return "", nil, fmt.Errorf("no complete SUSE staging server/agent image pair was found for %s: %s", requestedVersion, detail)
	}

	sort.SliceStable(complete, func(i, j int) bool {
		if complete[i].completeAt.Equal(complete[j].completeAt) {
			return complete[i].version > complete[j].version
		}
		return complete[i].completeAt.After(complete[j].completeAt)
	})
	log.Printf("[resolver] Selected immutable Rancher head %s for %s after %s", complete[0].version, requestedVersion, time.Since(startedAt).Round(time.Millisecond))
	return complete[0].version, complete[0].resolution, nil
}

func listRancherImageTags(ctx context.Context, registry, repository string) ([]string, error) {
	parsed, err := name.NewRepository(registry+"/"+repository, name.WeakValidation)
	if err != nil {
		return nil, err
	}
	return remote.List(parsed, remote.WithContext(ctx), remote.WithAuth(authn.Anonymous))
}

func inspectPatchHeadStagingCandidate(ctx context.Context, version string) patchHeadStagingCandidate {
	tag := normalizeDockerRancherTag(version)
	serverRepository := "stgregistry.suse.com/rancher/rancher"
	agentRepository := "stgregistry.suse.com/rancher/rancher-agent"
	serverReference := serverRepository + ":" + tag
	agentReference := agentRepository + ":" + tag

	server, serverFound, err := inspectPreferredRancherImage(ctx, serverReference)
	if err != nil {
		return patchHeadStagingCandidate{version: version, err: fmt.Errorf("inspect server image: %w", err), lookupErr: true}
	}
	if !serverFound {
		return patchHeadStagingCandidate{version: version, err: fmt.Errorf("server image was not found")}
	}
	agent, agentFound, err := inspectPreferredRancherImage(ctx, agentReference)
	if err != nil {
		return patchHeadStagingCandidate{version: version, err: fmt.Errorf("inspect agent image: %w", err), lookupErr: true}
	}
	if !agentFound {
		return patchHeadStagingCandidate{version: version, err: fmt.Errorf("agent image was not found")}
	}
	if err := validateExactHeadImagePair(tag, server, agent); err != nil {
		return patchHeadStagingCandidate{version: version, err: err}
	}
	if err := validatePatchHeadServerProvenance(version, server); err != nil {
		return patchHeadStagingCandidate{version: version, err: err}
	}

	if server.CreatedAt.IsZero() {
		return patchHeadStagingCandidate{version: version, err: fmt.Errorf("server image did not declare a creation timestamp")}
	}
	if agent.CreatedAt.IsZero() {
		return patchHeadStagingCandidate{version: version, err: fmt.Errorf("agent image did not declare a creation timestamp")}
	}
	completeAt := server.CreatedAt
	if agent.CreatedAt.After(completeAt) {
		completeAt = agent.CreatedAt
	}
	return patchHeadStagingCandidate{
		version: version,
		resolution: &preferredRancherImageResolution{
			Registry:          "stgregistry.suse.com",
			RegistryLabel:     preferredRancherRegistryLabel("stgregistry.suse.com"),
			RancherImage:      serverRepository,
			RancherImageTag:   tag,
			AgentImage:        agentReference,
			RancherProvenance: server,
			AgentProvenance:   agent,
		},
		completeAt: completeAt,
	}
}

func validateExactHeadImagePair(tag string, server, agent rancherImageProvenance) error {
	normalizedTag := normalizeVersionInput(tag)
	if !isCommitHeadRancherVersion(normalizedTag) {
		return nil
	}
	expectedTag := normalizeDockerRancherTag(normalizedTag)
	_, serverCanonicalRepository, serverCanonicalTag, serverErr := parseRegistryImage(server.CanonicalReference)
	_, agentCanonicalRepository, agentCanonicalTag, agentErr := parseRegistryImage(agent.CanonicalReference)
	if serverErr != nil || agentErr != nil || serverCanonicalTag == "" || agentCanonicalTag == "" {
		return fmt.Errorf("exact Rancher head image pair %s did not declare canonical server and agent org.opensuse.reference labels", expectedTag)
	}
	if serverCanonicalRepository != "rancher/rancher" || agentCanonicalRepository != "rancher/rancher-agent" {
		return fmt.Errorf("exact Rancher head image pair %s has unexpected canonical repositories: server %s, agent %s", expectedTag, serverCanonicalRepository, agentCanonicalRepository)
	}
	if serverCanonicalTag != expectedTag || agentCanonicalTag != expectedTag {
		return fmt.Errorf("exact Rancher head image pair %s has mismatched canonical tags: server %s, agent %s", expectedTag, serverCanonicalTag, agentCanonicalTag)
	}
	return nil
}

func validatePatchHeadServerProvenance(version string, server rancherImageProvenance) error {
	normalizedVersion := normalizeVersionInput(version)
	if !isPrimeCommitHeadRancherVersion(normalizedVersion) {
		return fmt.Errorf("%s is not an immutable patch-qualified Rancher head", version)
	}
	components := strings.Split(normalizedVersion, "-")
	expectedRevision := strings.ToLower(components[len(components)-2])
	source := strings.TrimSpace(server.SourceURL)
	if source != "https://github.com/rancher/rancher-prime" && source != "https://github.com/rancher/rancher-prime.git" {
		return fmt.Errorf("Rancher head image %s did not declare the canonical Rancher Prime source", normalizeDockerRancherTag(normalizedVersion))
	}
	ossRevision := strings.ToLower(strings.TrimSpace(server.OSSRevision))
	if !imageLookupGitRevisionPattern.MatchString(ossRevision) || !strings.HasPrefix(ossRevision, expectedRevision) {
		return fmt.Errorf("Rancher head image %s identifies commit %s, but its public OSS revision is %s", normalizeDockerRancherTag(normalizedVersion), expectedRevision, ossRevision)
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
