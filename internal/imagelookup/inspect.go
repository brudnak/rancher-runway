package imagelookup

import (
	"context"
	"errors"
	"fmt"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"gopkg.in/yaml.v3"
	"net/http"
	"strings"
)

func (s *Service) Inspect(ctx context.Context, request InspectRequest) (InspectResponse, error) {
	s.defaults()
	parsed, err := s.parseReference(request.Reference, true)
	if err != nil {
		return InspectResponse{}, err
	}
	platform, err := imageLookupParsePlatform(request.Platform)
	if err != nil {
		return InspectResponse{}, err
	}

	descriptor, err := remote.Get(parsed.parsed, s.remoteOptions(ctx, platform, 0)...)
	if err != nil {
		return InspectResponse{}, err
	}
	response := InspectResponse{
		Reference:  parsed.canonical,
		Registry:   parsed.registry,
		Repository: parsed.repository,
		Tag:        parsed.tag,
		Digest:     descriptor.Digest.String(),
		MediaType:  string(descriptor.MediaType),
		Platform:   platform.String(),
		Platforms:  []Platform{},
		Layers:     []Layer{},
		BuildYAML:  BuildYAML{},
		Warnings:   []string{},
	}

	if descriptor.MediaType == types.OCIImageIndex || descriptor.MediaType == types.DockerManifestList {
		index, indexErr := descriptor.ImageIndex()
		if indexErr != nil {
			return InspectResponse{}, fmt.Errorf("read image index: %w", indexErr)
		}
		manifest, manifestErr := index.IndexManifest()
		if manifestErr != nil {
			return InspectResponse{}, fmt.Errorf("read image index manifest: %w", manifestErr)
		}
		for _, item := range manifest.Manifests {
			entry := Platform{
				Digest:    item.Digest.String(),
				MediaType: string(item.MediaType),
				Size:      item.Size,
			}
			if item.Platform != nil {
				entry.OS = item.Platform.OS
				entry.Architecture = item.Platform.Architecture
				entry.Variant = item.Platform.Variant
			}
			response.Platforms = append(response.Platforms, entry)
		}
	}

	image, err := descriptor.Image()
	if err != nil {
		return InspectResponse{}, fmt.Errorf("select image platform %s: %w", platform.String(), err)
	}
	configFile, err := image.ConfigFile()
	if err != nil {
		return InspectResponse{}, fmt.Errorf("read image config: %w", err)
	}
	manifest, err := image.Manifest()
	if err != nil {
		return InspectResponse{}, fmt.Errorf("read image manifest: %w", err)
	}
	response.Config = ImageConfig{
		Digest:       manifest.Config.Digest.String(),
		Size:         manifest.Config.Size,
		Architecture: configFile.Architecture,
		OS:           configFile.OS,
		Variant:      configFile.Variant,
		CreatedAt:    imageLookupFormatTime(configFile.Created.Time),
		Labels:       imageLookupBoundedLabels(configFile.Config.Labels),
		Env:          imageLookupBoundedStrings(configFile.Config.Env, 512, 8192),
		Entrypoint:   imageLookupBoundedStrings(configFile.Config.Entrypoint, 128, 8192),
		Cmd:          imageLookupBoundedStrings(configFile.Config.Cmd, 128, 8192),
		History:      imageLookupBoundedHistory(configFile.History),
	}
	response.CreatedAt = response.Config.CreatedAt
	response.PrimeHead = imageLookupInspectPrimeHead(parsed, configFile.Config.Labels)
	if configFile.OS != "" && configFile.Architecture != "" {
		response.Platform = (&v1.Platform{OS: configFile.OS, Architecture: configFile.Architecture, Variant: configFile.Variant}).String()
	}
	response.Size = manifest.Config.Size
	for _, layer := range manifest.Layers {
		response.Layers = append(response.Layers, Layer{
			Digest:    layer.Digest.String(),
			Size:      layer.Size,
			MediaType: string(layer.MediaType),
		})
		response.Size += layer.Size
	}

	if !request.SkipTagMetadata && parsed.registry == "docker.io" && parsed.tag != "" {
		if metadata, metadataErr := s.dockerHubTag(ctx, parsed.repository, parsed.tag); metadataErr == nil {
			response.UploadedAt = imageLookupFormatTime(metadata.TagLastPushed)
		} else if ctx.Err() == nil {
			response.Warnings = append(response.Warnings, "Docker Hub did not expose tag upload metadata")
		}
	}

	if request.IncludeBuildYAML {
		response.BuildYAML, response.Warnings = s.findBuildYAML(ctx, image, response.Warnings)
	}
	return response, nil
}

func (s *Service) FetchSourceBuildYAML(ctx context.Context, request SourceBuildYAMLRequest) (SourceBuildYAMLResponse, error) {
	s.defaults()
	parsed, err := s.parseReference(request.Reference, true)
	if err != nil {
		return SourceBuildYAMLResponse{}, err
	}
	platform, err := imageLookupParsePlatform(request.Platform)
	if err != nil {
		return SourceBuildYAMLResponse{}, err
	}
	expectedDigest := strings.TrimSpace(request.ExpectedDigest)
	if !imageLookupDigestPattern.MatchString(expectedDigest) {
		return SourceBuildYAMLResponse{}, &InputError{Message: "expectedDigest must be a sha256 image digest"}
	}

	descriptor, err := remote.Get(parsed.parsed, s.remoteOptions(ctx, platform, 0)...)
	if err != nil {
		return SourceBuildYAMLResponse{}, err
	}
	resolvedDigest := descriptor.Digest.String()
	if !strings.EqualFold(resolvedDigest, expectedDigest) {
		return SourceBuildYAMLResponse{}, &imageLookupConflictError{message: "image reference moved since inspection; inspect it again before fetching build.yaml"}
	}

	image, err := descriptor.Image()
	if err != nil {
		return SourceBuildYAMLResponse{}, fmt.Errorf("select image platform %s: %w", platform.String(), err)
	}
	configFile, err := image.ConfigFile()
	if err != nil {
		return SourceBuildYAMLResponse{}, fmt.Errorf("read image config: %w", err)
	}
	source := configFile.Config.Labels[SourceLabel]
	revision := configFile.Config.Labels[RevisionLabel]
	owner, repository, err := ParseGitHubSource(source, revision)
	if err != nil {
		return SourceBuildYAMLResponse{}, err
	}
	revision = strings.ToLower(revision)

	fetchFromGitHub := func(fetchOwner, fetchRepository, fetchRevision string) ([]byte, error) {
		commandCtx, cancel := context.WithTimeout(ctx, imageLookupGHTimeout)
		defer cancel()
		endpoint := "/repos/" + fetchOwner + "/" + fetchRepository + "/contents/build.yaml?ref=" + fetchRevision
		arguments := []string{
			"api",
			"--hostname", "github.com",
			"--method", http.MethodGet,
			"-H", "Accept:application/vnd.github.raw+json",
			endpoint,
		}
		raw, commandErr := s.runCommand(commandCtx, "gh", arguments, SanitizedGHEnvironment(), imageLookupMaxSourceBuildYAML)
		if commandErr == nil {
			return raw, nil
		}
		switch {
		case errors.Is(commandCtx.Err(), context.DeadlineExceeded):
			return nil, errors.New("declared-source build.yaml request timed out")
		case errors.Is(commandErr, ErrImageLookupCommandOutputLimit):
			return nil, fmt.Errorf("declared-source build.yaml exceeds the %s response limit", imageLookupByteSize(imageLookupMaxSourceBuildYAML))
		default:
			return nil, errors.New("could not fetch build.yaml from the declared GitHub source; confirm GitHub CLI authentication and repository access")
		}
	}

	origin := "declared-source"
	repositoryURL := "https://github.com/" + owner + "/" + repository
	revisionLabel := RevisionLabel
	raw, fetchErr := fetchFromGitHub(owner, repository, revision)
	if fetchErr != nil && owner == "rancher" && repository == "rancher-prime" {
		// rancher-prime images can declare the matching public rancher/rancher
		// commit separately. Keep this fallback exact and revision-pinned so an
		// arbitrary source label cannot redirect the authenticated gh request.
		ossRevision := configFile.Config.Labels[OSSRevisionLabel]
		if GitRevisionPattern.MatchString(ossRevision) {
			ossRevision = strings.ToLower(ossRevision)
			fallbackRaw, fallbackErr := fetchFromGitHub("rancher", "rancher", ossRevision)
			if fallbackErr == nil {
				raw = fallbackRaw
				fetchErr = nil
				origin = "declared-oss-source"
				repositoryURL = "https://github.com/rancher/rancher"
				revision = ossRevision
				revisionLabel = OSSRevisionLabel
			} else {
				fetchErr = fallbackErr
			}
		}
	}
	if fetchErr != nil {
		return SourceBuildYAMLResponse{}, fetchErr
	}
	if len(raw) == 0 {
		return SourceBuildYAMLResponse{}, errors.New("declared GitHub source returned an empty build.yaml")
	}
	if int64(len(raw)) > imageLookupMaxSourceBuildYAML {
		return SourceBuildYAMLResponse{}, fmt.Errorf("declared-source build.yaml exceeds the %s response limit", imageLookupByteSize(imageLookupMaxSourceBuildYAML))
	}
	var data map[string]any
	if err := yaml.Unmarshal(raw, &data); err != nil {
		return SourceBuildYAMLResponse{}, errors.New("declared GitHub source returned invalid build.yaml content")
	}
	if data == nil {
		data = map[string]any{}
	}
	selectedPlatform := platform.String()
	if configFile.OS != "" && configFile.Architecture != "" {
		selectedPlatform = (&v1.Platform{OS: configFile.OS, Architecture: configFile.Architecture, Variant: configFile.Variant}).String()
	}
	return SourceBuildYAMLResponse{
		Found:  true,
		Path:   "build.yaml",
		Origin: origin,
		Provenance: SourceBuildYAMLProvenance{
			RepositoryURL:  repositoryURL,
			Revision:       revision,
			Path:           "build.yaml",
			ImageReference: parsed.canonical,
			ImageDigest:    resolvedDigest,
			Platform:       selectedPlatform,
			SourceLabel:    SourceLabel,
			RevisionLabel:  revisionLabel,
		},
		Raw:  string(raw),
		Data: data,
	}, nil
}
