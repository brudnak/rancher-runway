package test

import (
	"context"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/brudnak/ha-rancher-rke2/terratest/settings"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

type preferredRancherImageResolution struct {
	Registry          string
	RegistryLabel     string
	RancherImage      string
	RancherImageTag   string
	AgentImage        string
	RancherProvenance imagelookup.Provenance
	AgentProvenance   imagelookup.Provenance
	HeadAlias         string
}

type rancherImageInspectFunc func(context.Context, string) (imagelookup.Provenance, bool, error)
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

	tag := imagelookup.NormalizeDockerRancherTag(imagelookup.NormalizeVersionInput(requestedVersion))
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
			return nil, fmt.Errorf("could not verify %s in %s: %w", serverReference, imagelookup.PreferredRegistryLabel(registry), serverErr)
		}
		if !serverFound {
			misses = append(misses, fmt.Sprintf("%s missing %s", imagelookup.PreferredRegistryLabel(registry), serverReference))
			continue
		}

		agentCtx, agentCancel := context.WithTimeout(context.Background(), rancherResolverHTTPTimeout)
		agentProvenance, agentFound, agentErr := inspectPreferredRancherImage(agentCtx, agentReference)
		agentCancel()
		if agentErr != nil {
			return nil, fmt.Errorf("could not verify %s in %s: %w", agentReference, imagelookup.PreferredRegistryLabel(registry), agentErr)
		}

		if serverFound && agentFound {
			if err := validateExactHeadImagePair(tag, serverProvenance, agentProvenance); err != nil {
				return nil, err
			}
			if imagelookup.IsPrimeCommitHeadRancherVersion(requestedVersion) {
				if err := imagelookup.ValidatePatchHeadServerProvenance(requestedVersion, serverProvenance); err != nil {
					return nil, err
				}
			}
			return &preferredRancherImageResolution{
				Registry:          registry,
				RegistryLabel:     imagelookup.PreferredRegistryLabel(registry),
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
		misses = append(misses, fmt.Sprintf("%s missing %s", imagelookup.PreferredRegistryLabel(registry), strings.Join(missing, " and ")))
	}

	for _, registry := range registries {
		if registry == "docker.io" && isCommunityMinorHeadSelector(requestedVersion) {
			resolution, err := resolveCommunityMainHeadAlias(requestedVersion)
			if err == nil {
				return resolution, nil
			}
			misses = append(misses, err.Error())
		}
	}
	return nil, fmt.Errorf("preferred registries do not contain a complete Rancher server/agent image pair for %s: %s. No unselected registry fallback was attempted", tag, strings.Join(misses, "; "))
}

func inspectExplicitRancherImagePair(serverRepository, tag, agentReference string) (*preferredRancherImageResolution, error) {
	serverReference := strings.TrimSpace(serverRepository) + ":" + strings.TrimSpace(tag)
	registry, _, _, err := imagelookup.ParseRegistryImage(serverReference)
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
		RegistryLabel:     imagelookup.PreferredRegistryLabel(registry),
		RancherImage:      strings.TrimSpace(serverRepository),
		RancherImageTag:   strings.TrimSpace(tag),
		AgentImage:        strings.TrimSpace(agentReference),
		RancherProvenance: serverProvenance,
		AgentProvenance:   agentProvenance,
	}, nil
}

func inspectRancherImageReference(ctx context.Context, reference string) (imagelookup.Provenance, bool, error) {
	service, _ := ctx.Value(preferredRancherImageLookupServiceContextKey{}).(*imagelookup.Service)
	if service == nil {
		service = newPreferredRancherImageLookupService()
		defer service.CloseIdleConnections()
	}
	return imagelookup.InspectProvenance(ctx, service, reference)
}

func newPreferredRancherImageLookupService() *imagelookup.Service {
	return imagelookup.NewWithOptions(imagelookup.Options{Keychain: preferredRancherImageKeychain{}})
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

type patchHeadStagingTimeouts struct {
	tagList time.Duration
	scan    time.Duration
	image   time.Duration
}

func resolveCachedPatchHeadStagingBundle(cache map[string]patchHeadStagingResolution, requestedVersion string, resolver patchHeadStagingResolver) (string, *preferredRancherImageResolution, error) {
	key := imagelookup.NormalizeVersionInput(requestedVersion)
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
	return resolvePatchHeadStagingBundleWithTimeouts(context.Background(), requestedVersion, patchHeadStagingTimeouts{
		tagList: 2 * rancherResolverHTTPTimeout,
		scan:    5 * time.Minute,
		image:   rancherResolverHTTPTimeout,
	})
}

func resolvePatchHeadStagingBundleWithTimeouts(parent context.Context, requestedVersion string, timeouts patchHeadStagingTimeouts) (string, *preferredRancherImageResolution, error) {
	startedAt := time.Now()
	requestedVersion = imagelookup.NormalizeVersionInput(requestedVersion)
	if !imagelookup.IsPatchHeadAliasRancherVersion(requestedVersion) {
		return "", nil, fmt.Errorf("%s is not a patch-qualified Rancher head selector", requestedVersion)
	}

	log.Printf("[resolver] Resolving Rancher %s from SUSE staging image tags", requestedVersion)
	listCtx, listCancel := context.WithTimeout(parent, timeouts.tagList)
	tags, err := listPatchHeadStagingImageTags(listCtx, "stgregistry.suse.com", "rancher/rancher")
	if err == nil {
		err = listCtx.Err()
	}
	listCancel()
	if err != nil {
		return "", nil, fmt.Errorf("list immutable Rancher head images in SUSE staging: %w", err)
	}

	patchVersion := strings.TrimSuffix(requestedVersion, "-head")
	seen := map[string]bool{}
	versions := make([]string, 0)
	for _, tag := range tags {
		version := imagelookup.NormalizeVersionInput(tag)
		if !imagelookup.IsPrimeCommitHeadRancherVersion(version) || !strings.HasPrefix(version, patchVersion+"-") || seen[version] {
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
	defer imageLookup.CloseIdleConnections()
	// A release line can contain hundreds of immutable builds. Give the full
	// scan its own budget; listing tags must not consume the inspection time.
	// Each individual image lookup still has a short deadline below.
	ctx, cancel := context.WithTimeout(parent, timeouts.scan)
	defer cancel()
	ctx = context.WithValue(ctx, preferredRancherImageLookupServiceContextKey{}, imageLookup)

	jobs := make(chan string)
	results := make(chan patchHeadStagingCandidate, len(versions))
	workerCount := 4
	if len(versions) < workerCount {
		workerCount = len(versions)
	}
	log.Printf("[resolver] Inspecting %d SUSE staging Rancher server/agent image pair(s) with %d workers (scan limit %s, image limit %s)", len(versions), workerCount, timeouts.scan, timeouts.image)

	var workers sync.WaitGroup
	for worker := 0; worker < workerCount; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for version := range jobs {
				if ctx.Err() != nil {
					return
				}
				results <- inspectPatchHeadStagingCandidate(ctx, version, timeouts.image)
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
	inspected := 0
	for candidate := range results {
		// Lookup failures leave a candidate unverified; do not count them as
		// inspected or use a partial scan to claim that a pair is newest.
		if !candidate.lookupErr {
			inspected++
		}
		finished := len(complete) + len(misses) + len(lookupErrors) + 1
		if finished%25 == 0 || finished == len(versions) {
			log.Printf("[resolver] SUSE staging scan processed %d/%d candidate pairs (%d inspected) after %s", finished, len(versions), inspected, time.Since(startedAt).Round(time.Second))
		}
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
		return "", nil, fmt.Errorf("resolve %s from SUSE staging: inspected %d of %d candidate pairs after %s (scan limit %s): %w; retry or select an explicit immutable %s-<SHA>-head version", requestedVersion, inspected, len(versions), time.Since(startedAt).Round(time.Second), timeouts.scan, err, patchVersion)
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

func inspectPatchHeadStagingCandidate(ctx context.Context, version string, imageTimeout time.Duration) patchHeadStagingCandidate {
	tag := imagelookup.NormalizeDockerRancherTag(version)
	serverRepository := "stgregistry.suse.com/rancher/rancher"
	agentRepository := "stgregistry.suse.com/rancher/rancher-agent"
	serverReference := serverRepository + ":" + tag
	agentReference := agentRepository + ":" + tag

	serverCtx, serverCancel := context.WithTimeout(ctx, imageTimeout)
	server, serverFound, err := inspectPreferredRancherImage(serverCtx, serverReference)
	if err == nil {
		err = serverCtx.Err()
	}
	serverCancel()
	if err != nil {
		return patchHeadStagingCandidate{version: version, err: fmt.Errorf("inspect server image: %w", err), lookupErr: true}
	}
	if !serverFound {
		return patchHeadStagingCandidate{version: version, err: fmt.Errorf("server image was not found")}
	}
	agentCtx, agentCancel := context.WithTimeout(ctx, imageTimeout)
	agent, agentFound, err := inspectPreferredRancherImage(agentCtx, agentReference)
	if err == nil {
		err = agentCtx.Err()
	}
	agentCancel()
	if err != nil {
		return patchHeadStagingCandidate{version: version, err: fmt.Errorf("inspect agent image: %w", err), lookupErr: true}
	}
	if !agentFound {
		return patchHeadStagingCandidate{version: version, err: fmt.Errorf("agent image was not found")}
	}
	if err := validateExactHeadImagePair(tag, server, agent); err != nil {
		return patchHeadStagingCandidate{version: version, err: err}
	}
	if err := imagelookup.ValidatePatchHeadServerProvenance(version, server); err != nil {
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
			RegistryLabel:     imagelookup.PreferredRegistryLabel("stgregistry.suse.com"),
			RancherImage:      serverRepository,
			RancherImageTag:   tag,
			AgentImage:        agentReference,
			RancherProvenance: server,
			AgentProvenance:   agent,
		},
		completeAt: completeAt,
	}
}

func validateExactHeadImagePair(tag string, server, agent imagelookup.Provenance) error {
	normalizedTag := imagelookup.NormalizeVersionInput(tag)
	if !isCommitHeadRancherVersion(normalizedTag) {
		return nil
	}
	expectedTag := imagelookup.NormalizeDockerRancherTag(normalizedTag)
	_, serverCanonicalRepository, serverCanonicalTag, serverErr := imagelookup.ParseRegistryImage(server.CanonicalReference)
	_, agentCanonicalRepository, agentCanonicalTag, agentErr := imagelookup.ParseRegistryImage(agent.CanonicalReference)
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

// Preferred-image verification must match what provisioned nodes can pull.
// Docker Hub credentials are propagated to RKE2/K3s; credentials from the
// operator's local Docker keychain for other registries are not.
type preferredRancherImageKeychain struct{}

func (preferredRancherImageKeychain) Resolve(resource authn.Resource) (authn.Authenticator, error) {
	registry := imagelookup.RegistryForDisplay(resource.RegistryStr())
	username := strings.TrimSpace(os.Getenv("DOCKERHUB_USERNAME"))
	password := os.Getenv("DOCKERHUB_PASSWORD")
	if registry == "docker.io" && username != "" && password != "" {
		return authn.FromConfig(authn.AuthConfig{Username: username, Password: password}), nil
	}
	return authn.Anonymous, nil
}

func rancherImageSourceCommitURL(source, revision string) string {
	owner, repository, err := imagelookup.ParseGitHubSource(strings.TrimSpace(source), strings.TrimSpace(revision))
	if err != nil {
		return ""
	}
	return fmt.Sprintf("https://github.com/%s/%s/commit/%s", owner, repository, revision)
}

func preferredImageResolutionExplanation(resolution *preferredRancherImageResolution, registries []string) []string {
	if resolution == nil {
		return nil
	}
	if len(registries) == 0 {
		registries = []string{resolution.Registry}
	}
	if resolution.HeadAlias != "" {
		return []string{
			fmt.Sprintf("Docker Hub did not publish a complete %s pair; verified its Community head belongs to the requested release line", resolution.HeadAlias),
			fmt.Sprintf("Pinned server and agent images to the matching immutable Docker Hub tag %s", resolution.RancherImageTag),
			"Recorded resolution-time OCI digests and provenance for both images",
		}
	}
	return []string{
		fmt.Sprintf("Verified the requested Rancher server and agent image pair in %s before provisioning", resolution.RegistryLabel),
		fmt.Sprintf("Preferred registry order was %s; selected the first complete pair", strings.Join(registries, ", ")),
		"Recorded resolution-time OCI digests; mutable image tags are not digest-pinned by Helm",
	}
}
