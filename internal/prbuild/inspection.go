package prbuild

import (
	"context"
	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	"github.com/brudnak/ha-rancher-rke2/internal/registrycatalog"
	"strings"
	"sync"
)

type prBuildInspectJob struct {
	registryIndex int
	kind          string
	reference     string
}

type prBuildInspectResult struct {
	registryIndex int
	kind          string
	image         ImageResult
}

func (s *Service) inspectKnownRegistries(ctx context.Context, tag string) ([]RegistryResult, error) {
	registries := make([]RegistryResult, len(registrycatalog.Preferred))
	jobs := make(chan prBuildInspectJob)
	results := make(chan prBuildInspectResult)
	workerCount := prBuildWorkerLimit
	if workerCount > len(registrycatalog.Preferred)*2 {
		workerCount = len(registrycatalog.Preferred) * 2
	}
	var workers sync.WaitGroup
	for worker := 0; worker < workerCount; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for job := range jobs {
				lookupCtx, cancel := context.WithTimeout(ctx, prBuildImageLookupTimeout)
				image := s.inspectImage(lookupCtx, job.reference)
				cancel()
				select {
				case results <- prBuildInspectResult{registryIndex: job.registryIndex, kind: job.kind, image: image}:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	go func() {
		for index, registry := range registrycatalog.Preferred {
			select {
			case jobs <- prBuildInspectJob{registryIndex: index, kind: "server", reference: registry + "/rancher/rancher:" + tag}:
			case <-ctx.Done():
				close(jobs)
				return
			}
			select {
			case jobs <- prBuildInspectJob{registryIndex: index, kind: "agent", reference: registry + "/rancher/rancher-agent:" + tag}:
			case <-ctx.Done():
				close(jobs)
				return
			}
		}
		close(jobs)
	}()
	go func() {
		workers.Wait()
		close(results)
	}()

	for index, registry := range registrycatalog.Preferred {
		registries[index] = RegistryResult{Registry: registry, Label: imagelookup.PreferredRegistryLabel(registry)}
	}
	for result := range results {
		if result.kind == "server" {
			registries[result.registryIndex].Server = result.image
		} else {
			registries[result.registryIndex].Agent = result.image
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return registries, nil
}

func (s *Service) inspectImage(ctx context.Context, reference string) ImageResult {
	result := ImageResult{
		Reference: reference,
		Match: CommitMatch{
			Verdict: "not_applicable",
			Reason:  "Image provenance was not inspected.",
		},
	}
	response, err := s.inspect(ctx, imagelookup.InspectRequest{
		Reference:        reference,
		Platform:         prBuildPlatform,
		IncludeBuildYAML: false,
		SkipTagMetadata:  true,
	})
	if err != nil {
		if imagelookup.RegistryNotFound(err) {
			result.Match.Reason = "The exact image tag was not found in this registry."
			return result
		}
		result.Error = imagelookup.SafeError(err)
		result.Match.Reason = "The registry lookup failed before provenance could be inspected."
		return result
	}
	for _, entry := range response.Config.Env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		switch key {
		case "CATTLE_RANCHER_WEBHOOK_VERSION":
			result.WebhookVersion = imagelookup.SafeOCIProvenanceLabel(value)
		case "CATTLE_CHART_DEFAULT_BRANCH":
			result.ChartBranch = imagelookup.SafeOCIProvenanceLabel(value)
		}
	}
	labels := response.Config.Labels
	result.Found = true
	if response.Reference != "" {
		result.Reference = response.Reference
	}
	result.Digest = response.Digest
	result.PlatformDigest = prBuildSelectedPlatformDigest(response)
	result.Platform = response.Platform
	result.BuildVersion = imagelookup.SafeOCIProvenanceLabel(labels[imagelookup.VersionLabel])
	result.SourceURL = imagelookup.SafeOCIProvenanceLabel(labels[imagelookup.SourceLabel])
	result.Revision = imagelookup.SafeOCIProvenanceLabel(labels[imagelookup.RevisionLabel])
	result.OSSRevision = imagelookup.SafeOCIProvenanceLabel(labels[imagelookup.OSSRevisionLabel])
	result.Match = CommitMatch{Verdict: "unknown", Reason: "The image does not expose comparable GitHub provenance."}
	return result
}

func prBuildSelectedPlatformDigest(response imagelookup.InspectResponse) string {
	if len(response.Platforms) == 0 {
		return response.Digest
	}
	wanted := strings.TrimSpace(response.Platform)
	for _, platform := range response.Platforms {
		candidate := platform.OS + "/" + platform.Architecture
		if platform.Variant != "" {
			candidate += "/" + platform.Variant
		}
		if candidate == wanted {
			return platform.Digest
		}
	}
	return ""
}
