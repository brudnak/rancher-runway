package test

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"gopkg.in/yaml.v3"
)

const (
	imageLookupRequestLimit       = 64 << 10
	imageLookupDefaultResultLimit = 50
	imageLookupMaxResultLimit     = 200
	imageLookupMaxRecentDays      = 3650
	imageLookupMaxTagScan         = 10000
	imageLookupTagPageSize        = 1000
	imageLookupMaxBuildYAML       = 1 << 20
	imageLookupMaxBuildYAMLLayer  = 16 << 20
	imageLookupMaxLayerScan       = 256 << 20
	imageLookupMaxHistoryEntries  = 256
	imageLookupMaxHistoryText     = 8192
	imageLookupSearchTimeout      = 30 * time.Second
	imageLookupInspectTimeout     = 90 * time.Second
	imageLookupSourceTimeout      = 45 * time.Second
	imageLookupGHTimeout          = 15 * time.Second
	imageLookupMaxSourceBuildYAML = 1 << 20
)

var imageLookupKnownRegistries = []string{
	"stgregistry.suse.com",
	"registry.rancher.com",
	"registry.suse.com",
	"docker.io",
}

var imageLookupKnownRepositories = []string{
	"rancher/rancher",
	"rancher/rancher-agent",
	"rancher/rancher-webhook",
}

var imageLookupFullVersionTagPattern = regexp.MustCompile(`(?i)^v?[0-9]+\.[0-9]+\.[0-9]+(?:[-._][a-z0-9][a-z0-9._-]*)?$`)

var imageLookupVersionLinePattern = regexp.MustCompile(`(?i)^v?([0-9]+)\.([0-9]+)(?:\.([0-9]+))?$`)

var imageLookupMinorHeadPattern = regexp.MustCompile(`(?i)^v?([0-9]+)\.([0-9]+)-head$`)

var imageLookupTagVersionPattern = regexp.MustCompile(`(?i)^v?([0-9]+\.[0-9]+\.[0-9]+)(?:$|[-._])`)

var imageLookupGitRevisionPattern = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

var imageLookupDigestPattern = regexp.MustCompile(`^sha256:[0-9a-fA-F]{64}$`)

const (
	imageLookupSourceLabel      = "org.opencontainers.image.source"
	imageLookupRevisionLabel    = "org.opencontainers.image.revision"
	imageLookupOSSRevisionLabel = "org.opencontainers.image.oss.revision"
)

type imageLookupSearchRequest struct {
	Registry         string `json:"registry"`
	Repository       string `json:"repository"`
	Query            string `json:"query"`
	Limit            int    `json:"limit"`
	RecentDays       int    `json:"recentDays"`
	ScanMode         string `json:"scanMode"`
	IncludeArtifacts bool   `json:"includeArtifacts"`
	Channel          string `json:"channel"`
	Architecture     string `json:"architecture"`
	PrimeHead        string `json:"primeHead"`
	HeadKind         string `json:"headKind"`
	VersionLine      string `json:"versionLine"`
	Commit           string `json:"commit"`
	PairStatus       string `json:"pairStatus"`
	SortBy           string `json:"sortBy"`
	SortOrder        string `json:"sortOrder"`
}

type imageLookupSearchResponse struct {
	Query        string                   `json:"query"`
	RecentDays   int                      `json:"recentDays"`
	RecentCutoff string                   `json:"recentCutoff,omitempty"`
	ScanMode     string                   `json:"scanMode"`
	Channel      string                   `json:"channel"`
	Architecture string                   `json:"architecture"`
	PrimeHead    string                   `json:"primeHead"`
	HeadKind     string                   `json:"headKind"`
	VersionLine  string                   `json:"versionLine"`
	Commit       string                   `json:"commit"`
	PairStatus   string                   `json:"pairStatus"`
	SortBy       string                   `json:"sortBy"`
	SortOrder    string                   `json:"sortOrder"`
	SearchedAt   time.Time                `json:"searchedAt"`
	Groups       []imageLookupSearchGroup `json:"groups"`
}

type imageLookupSearchGroup struct {
	Key                     string           `json:"key"`
	Label                   string           `json:"label"`
	Registry                string           `json:"registry"`
	Repository              string           `json:"repository"`
	Reference               string           `json:"reference"`
	ImageRole               string           `json:"imageRole"`
	CompanionRepository     string           `json:"companionRepository,omitempty"`
	Tags                    []imageLookupTag `json:"tags"`
	Matched                 int              `json:"matched"`
	Scanned                 int              `json:"scanned"`
	PrimeHeadCount          int              `json:"primeHeadCount"`
	MovingPrimeHeadCount    int              `json:"movingPrimeHeadCount"`
	ImmutablePrimeHeadCount int              `json:"immutablePrimeHeadCount"`
	VerifiedPrimeHeadCount  int              `json:"verifiedPrimeHeadCount"`
	InvalidPrimeHeadCount   int              `json:"invalidPrimeHeadCount"`
	MissingCompanionCount   int              `json:"missingCompanionCount"`
	RecentExcludedCount     int              `json:"recentExcludedCount"`
	UnknownTimestampCount   int              `json:"unknownTimestampCount"`
	Truncated               bool             `json:"truncated"`
	Error                   string           `json:"error,omitempty"`
}

type imageLookupTag struct {
	Name               string `json:"name"`
	Reference          string `json:"reference"`
	Channel            string `json:"channel"`
	Architecture       string `json:"architecture"`
	BaseTag            string `json:"baseTag"`
	IsPrimeHead        bool   `json:"isPrimeHead"`
	HeadKind           string `json:"headKind,omitempty"`
	Mutable            bool   `json:"mutable"`
	Version            string `json:"version,omitempty"`
	VersionLine        string `json:"versionLine,omitempty"`
	Commit             string `json:"commit,omitempty"`
	Selector           string `json:"selector,omitempty"`
	ImageRole          string `json:"imageRole"`
	CompanionReference string `json:"companionReference,omitempty"`
	CompanionVerified  bool   `json:"companionVerified"`
	PairStatus         string `json:"pairStatus,omitempty"`
	PairComplete       bool   `json:"pairComplete"`
	PairCompletedAt    string `json:"pairCompletedAt,omitempty"`
	PairError          string `json:"pairError,omitempty"`
	ProvenanceValid    bool   `json:"provenanceValid"`
	PrimeSource        bool   `json:"primeSource"`
	Source             string `json:"source,omitempty"`
	CanonicalReference string `json:"canonicalReference,omitempty"`
	OSSRevision        string `json:"ossRevision,omitempty"`
	ResolvedRank       int    `json:"resolvedRank,omitempty"`
	Artifact           bool   `json:"artifact"`
	CreatedAt          string `json:"createdAt,omitempty"`
	UploadedAt         string `json:"uploadedAt,omitempty"`
	Digest             string `json:"digest,omitempty"`
	Size               int64  `json:"size,omitempty"`
}

type imageLookupInspectRequest struct {
	Reference        string `json:"reference"`
	Platform         string `json:"platform"`
	IncludeBuildYAML bool   `json:"includeBuildYaml"`
	SkipTagMetadata  bool   `json:"-"`
}

type imageLookupSourceBuildYAMLRequest struct {
	Reference      string `json:"reference"`
	Platform       string `json:"platform"`
	ExpectedDigest string `json:"expectedDigest"`
}

type imageLookupSourceBuildYAMLResponse struct {
	Found      bool                                 `json:"found"`
	Path       string                               `json:"path"`
	Origin     string                               `json:"origin"`
	Provenance imageLookupSourceBuildYAMLProvenance `json:"provenance"`
	Raw        string                               `json:"raw"`
	Data       map[string]any                       `json:"data"`
}

type imageLookupSourceBuildYAMLProvenance struct {
	RepositoryURL  string `json:"repositoryUrl"`
	Revision       string `json:"revision"`
	Path           string `json:"path"`
	ImageReference string `json:"imageReference"`
	ImageDigest    string `json:"imageDigest"`
	Platform       string `json:"platform"`
	SourceLabel    string `json:"sourceLabel"`
	RevisionLabel  string `json:"revisionLabel"`
}

type imageLookupInspectResponse struct {
	Reference  string                 `json:"reference"`
	Registry   string                 `json:"registry"`
	Repository string                 `json:"repository"`
	Tag        string                 `json:"tag,omitempty"`
	Digest     string                 `json:"digest"`
	MediaType  string                 `json:"mediaType"`
	CreatedAt  string                 `json:"createdAt,omitempty"`
	UploadedAt string                 `json:"uploadedAt,omitempty"`
	Platform   string                 `json:"platform,omitempty"`
	Platforms  []imageLookupPlatform  `json:"platforms"`
	Size       int64                  `json:"size"`
	Config     imageLookupImageConfig `json:"config"`
	Layers     []imageLookupLayer     `json:"layers"`
	BuildYAML  imageLookupBuildYAML   `json:"buildYaml"`
	PrimeHead  imageLookupPrimeHead   `json:"primeHead"`
	Warnings   []string               `json:"warnings"`
}

// imageLookupPrimeHead combines tag-derived classification with the OCI
// provenance labels available only after inspection. CompanionVerified is
// deliberately false: Image Lookup exposes the expected same-tag pair but
// does not claim that the companion manifest exists without probing it.
type imageLookupPrimeHead struct {
	IsPrimeHead             bool     `json:"isPrimeHead"`
	HeadKind                string   `json:"headKind,omitempty"`
	Mutable                 bool     `json:"mutable"`
	Version                 string   `json:"version,omitempty"`
	VersionLine             string   `json:"versionLine,omitempty"`
	Commit                  string   `json:"commit,omitempty"`
	Selector                string   `json:"selector,omitempty"`
	ImageRole               string   `json:"imageRole"`
	CompanionReference      string   `json:"companionReference,omitempty"`
	CompanionVerified       bool     `json:"companionVerified"`
	Source                  string   `json:"source,omitempty"`
	Revision                string   `json:"revision,omitempty"`
	OSSRevision             string   `json:"ossRevision,omitempty"`
	CanonicalReference      string   `json:"canonicalReference,omitempty"`
	CanonicalRepository     string   `json:"canonicalRepository,omitempty"`
	CanonicalTag            string   `json:"canonicalTag,omitempty"`
	PrimeSource             bool     `json:"primeSource"`
	CanonicalMatchesRequest bool     `json:"canonicalMatchesRequest"`
	CommitMatchesOSS        bool     `json:"commitMatchesOss"`
	Consistent              bool     `json:"consistent"`
	Issues                  []string `json:"issues"`
}

type imageLookupPlatform struct {
	OS           string `json:"os,omitempty"`
	Architecture string `json:"architecture,omitempty"`
	Variant      string `json:"variant,omitempty"`
	Digest       string `json:"digest,omitempty"`
	MediaType    string `json:"mediaType,omitempty"`
	Size         int64  `json:"size,omitempty"`
}

type imageLookupImageConfig struct {
	Digest       string                    `json:"digest"`
	Size         int64                     `json:"size"`
	Architecture string                    `json:"architecture,omitempty"`
	OS           string                    `json:"os,omitempty"`
	Variant      string                    `json:"variant,omitempty"`
	CreatedAt    string                    `json:"createdAt,omitempty"`
	Labels       map[string]string         `json:"labels"`
	Env          []string                  `json:"env"`
	Entrypoint   []string                  `json:"entrypoint"`
	Cmd          []string                  `json:"cmd"`
	History      []imageLookupHistoryEntry `json:"history"`
}

type imageLookupHistoryEntry struct {
	Created    string `json:"created,omitempty"`
	CreatedBy  string `json:"createdBy,omitempty"`
	Comment    string `json:"comment,omitempty"`
	EmptyLayer bool   `json:"emptyLayer"`
}

type imageLookupLayer struct {
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
	MediaType string `json:"mediaType"`
}

type imageLookupBuildYAML struct {
	Found   bool           `json:"found"`
	Path    string         `json:"path,omitempty"`
	Raw     string         `json:"raw,omitempty"`
	Data    map[string]any `json:"data,omitempty"`
	Error   string         `json:"error,omitempty"`
	Reason  string         `json:"reason,omitempty"`
	Skipped bool           `json:"skipped"`
}

type imageLookupService struct {
	transport     http.RoundTripper
	keychain      authn.Keychain
	allowHTTP     bool
	now           func() time.Time
	maxTagScan    int
	maxBuildYML   int64
	maxBuildLayer int64
	maxLayerScan  int64
	runCommand    imageLookupCommandRunner
}

type imageLookupCommandRunner func(context.Context, string, []string, []string, int64) ([]byte, error)

type imageLookupTarget struct {
	registry   string
	repository string
}

type imageLookupSearchOptions struct {
	query            string
	limit            int
	includeArtifacts bool
	channel          string
	architecture     string
	primeHead        string
	headKind         string
	versionLine      string
	commit           string
	pairStatus       string
	sortBy           string
	sortOrder        string
	fullScan         bool
	exactLookup      bool
	verifyPrimePairs bool
	primeVersion     string
	recentDays       int
	recentCutoff     time.Time
	scanMode         string
}

type imageLookupReference struct {
	parsed     name.Reference
	registry   string
	repository string
	tag        string
	digest     string
	canonical  string
}

type imageLookupInputError struct {
	message string
}

func (e *imageLookupInputError) Error() string { return e.message }

type imageLookupConflictError struct {
	message string
}

func (e *imageLookupConflictError) Error() string { return e.message }

type imageLookupSourceMetadataError struct {
	message string
}

func (e *imageLookupSourceMetadataError) Error() string { return e.message }

func newImageLookupService() *imageLookupService {
	return &imageLookupService{
		transport:     newImageLookupSafeTransport(),
		keychain:      imageLookupCredentialKeychain{},
		now:           time.Now,
		maxTagScan:    imageLookupMaxTagScan,
		maxBuildYML:   imageLookupMaxBuildYAML,
		maxBuildLayer: imageLookupMaxBuildYAMLLayer,
		maxLayerScan:  imageLookupMaxLayerScan,
		runCommand:    imageLookupExecCommand,
	}
}

func (s *imageLookupService) defaults() {
	if s.transport == nil {
		s.transport = newImageLookupSafeTransport()
	}
	if s.keychain == nil {
		s.keychain = imageLookupCredentialKeychain{}
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.maxTagScan <= 0 {
		s.maxTagScan = imageLookupMaxTagScan
	}
	if s.maxBuildYML <= 0 {
		s.maxBuildYML = imageLookupMaxBuildYAML
	}
	if s.maxBuildLayer <= 0 {
		s.maxBuildLayer = imageLookupMaxBuildYAMLLayer
	}
	if s.maxLayerScan <= 0 {
		s.maxLayerScan = imageLookupMaxLayerScan
	}
	if s.runCommand == nil {
		s.runCommand = imageLookupExecCommand
	}
}

func (s *imageLookupService) Search(ctx context.Context, request imageLookupSearchRequest) (imageLookupSearchResponse, error) {
	s.defaults()
	targets, options, err := s.searchParameters(request)
	if err != nil {
		return imageLookupSearchResponse{}, err
	}
	searchedAt := s.now().UTC()
	if options.recentDays > 0 {
		options.recentCutoff = searchedAt.Add(-time.Duration(options.recentDays) * 24 * time.Hour)
	}

	response := imageLookupSearchResponse{
		Query:        options.query,
		RecentDays:   options.recentDays,
		ScanMode:     options.scanMode,
		Channel:      options.channel,
		Architecture: options.architecture,
		PrimeHead:    options.primeHead,
		HeadKind:     options.headKind,
		VersionLine:  options.versionLine,
		Commit:       options.commit,
		PairStatus:   options.pairStatus,
		SortBy:       options.sortBy,
		SortOrder:    options.sortOrder,
		SearchedAt:   searchedAt,
		Groups:       make([]imageLookupSearchGroup, len(targets)),
	}
	if !options.recentCutoff.IsZero() {
		response.RecentCutoff = imageLookupFormatTime(options.recentCutoff)
	}

	workerLimit := 4
	if len(targets) < workerLimit {
		workerLimit = len(targets)
	}
	jobs := make(chan int)
	var workers sync.WaitGroup
	for worker := 0; worker < workerLimit; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				response.Groups[index] = s.searchTargetWithOptions(ctx, targets[index], options)
			}
		}()
	}
	for index := range targets {
		select {
		case jobs <- index:
		case <-ctx.Done():
			close(jobs)
			workers.Wait()
			return imageLookupSearchResponse{}, ctx.Err()
		}
	}
	close(jobs)
	workers.Wait()

	succeeded := 0
	for _, group := range response.Groups {
		if group.Error == "" {
			succeeded++
		}
	}
	if succeeded == 0 {
		if err := ctx.Err(); err != nil {
			return imageLookupSearchResponse{}, err
		}
		return imageLookupSearchResponse{}, fmt.Errorf("all registry searches failed")
	}
	if options.verifyPrimePairs {
		if err := s.verifyPrimeHeadPairs(ctx, &response, options); err != nil {
			return imageLookupSearchResponse{}, err
		}
	}
	return response, nil
}

func (s *imageLookupService) searchTarget(ctx context.Context, target imageLookupTarget, query string, limit int, includeArtifacts bool) imageLookupSearchGroup {
	return s.searchTargetWithOptions(ctx, target, imageLookupSearchOptions{
		query:            query,
		limit:            limit,
		includeArtifacts: includeArtifacts,
		channel:          "all",
		architecture:     "all",
		primeHead:        "all",
		headKind:         "all",
		sortBy:           "natural",
		sortOrder:        "desc",
		exactLookup:      true,
	})
}

func (s *imageLookupService) searchTargetWithOptions(ctx context.Context, target imageLookupTarget, options imageLookupSearchOptions) imageLookupSearchGroup {
	canonical := target.registry + "/" + target.repository
	imageRole, companionRepository := imageLookupRepositoryRole(target.repository)
	group := imageLookupSearchGroup{
		Key:                 canonical,
		Label:               imageLookupRegistryLabel(target.registry) + " / " + target.repository,
		Registry:            target.registry,
		Repository:          target.repository,
		Reference:           canonical,
		ImageRole:           imageRole,
		CompanionRepository: companionRepository,
		Tags:                []imageLookupTag{},
	}

	repository, err := s.parseRepository(target.registry, target.repository)
	if err != nil {
		group.Error = imageLookupSafeError(err)
		return group
	}
	if exactDigest, ok := imageLookupExactDigestReference(repository, options.query, s.allowHTTP); ok {
		descriptor, getErr := remote.Get(exactDigest, s.remoteOptions(ctx, nil, 0)...)
		group.Scanned = 1
		if getErr == nil {
			digest := strings.ToLower(options.query)
			result := imageLookupClassifyDigest(target.registry, target.repository, digest)
			result.Digest = descriptor.Digest.String()
			if imageLookupTagMatchesOptions(result, options) {
				group.Matched = 1
				group.Tags = []imageLookupTag{result}
			}
			if !options.verifyPrimePairs {
				imageLookupApplyRecentFilter(&group, options)
			}
			return group
		}
		if !imageLookupRegistryNotFound(getErr) {
			group.Error = imageLookupSafeError(getErr)
		}
		return group
	}
	if exactTag, ok := imageLookupExactTagReference(repository, options.query, s.allowHTTP); ok &&
		!imageLookupPatchHeadSelector(options.query) &&
		(options.exactLookup || !imageLookupBarePatchVersion(options.query)) &&
		(options.includeArtifacts || !imageLookupArtifactTag(options.query)) {
		descriptor, getErr := remote.Get(exactTag, s.remoteOptions(ctx, nil, 0)...)
		if getErr == nil {
			group.Scanned = 1
			tag := imageLookupClassifyTag(target.registry, target.repository, options.query)
			tag.Digest = descriptor.Digest.String()
			if imageLookupTagMatchesOptions(tag, options) {
				group.Matched = 1
				group.Tags = []imageLookupTag{tag}
			}
			if target.registry == "docker.io" {
				requireComplete := options.sortBy == "uploaded" || options.recentDays > 0
				complete, metadataErr := s.enrichDockerHubTags(ctx, target.repository, options.query, group.Tags, requireComplete)
				if requireComplete {
					switch {
					case metadataErr != nil:
						group.Error = imageLookupSafeError(metadataErr)
					case !complete && options.sortBy == "uploaded":
						group.Error = "Docker Hub did not expose upload metadata for every matched tag"
					}
				}
			}
			if !options.verifyPrimePairs {
				imageLookupApplyRecentFilter(&group, options)
			}
			imageLookupCountPrimeHeads(&group, group.Tags)
			return group
		}
		if !imageLookupRegistryNotFound(getErr) {
			group.Error = imageLookupSafeError(getErr)
			return group
		}
		if imageLookupFullVersionTag(options.query) {
			group.Scanned = 1
			return group
		}
	}
	puller, err := remote.NewPuller(s.remoteOptions(ctx, nil, imageLookupTagPageSize)...)
	if err != nil {
		group.Error = imageLookupSafeError(err)
		return group
	}
	lister, err := puller.Lister(ctx, repository)
	if err != nil {
		group.Error = imageLookupSafeError(err)
		return group
	}

	matches := make([]imageLookupTag, 0, options.limit)
	limitReached := false
	for lister.HasNext() && group.Scanned < s.maxTagScan {
		page, pageErr := lister.Next(ctx)
		if pageErr != nil {
			group.Error = imageLookupSafeError(pageErr)
			break
		}
		for pageIndex, tagName := range page.Tags {
			if group.Scanned >= s.maxTagScan {
				group.Truncated = true
				break
			}
			group.Scanned++
			if !options.includeArtifacts && imageLookupArtifactTag(tagName) {
				continue
			}
			tag := imageLookupClassifyTag(target.registry, target.repository, tagName)
			if !imageLookupTagMatchesOptions(tag, options) {
				continue
			}
			matches = append(matches, tag)
			if !options.fullScan && len(matches) == options.limit {
				limitReached = true
				if pageIndex+1 < len(page.Tags) || lister.HasNext() {
					group.Truncated = true
				}
				break
			}
		}
		if limitReached {
			break
		}
	}
	if !limitReached && lister.HasNext() {
		group.Truncated = true
	}

	group.Matched = len(matches)
	if target.registry == "docker.io" && len(matches) > 0 {
		requireComplete := options.sortBy == "uploaded" || options.recentDays > 0
		complete, metadataErr := s.enrichDockerHubTags(ctx, target.repository, options.query, matches, requireComplete)
		if requireComplete {
			switch {
			case metadataErr != nil:
				group.Error = imageLookupSafeError(metadataErr)
			case !complete && options.sortBy == "uploaded":
				group.Error = "Docker Hub did not expose upload metadata for every matched tag"
			}
		}
	}
	if !options.verifyPrimePairs && !options.recentCutoff.IsZero() {
		matches, group.RecentExcludedCount, group.UnknownTimestampCount = imageLookupFilterRecentTags(matches, options.recentCutoff)
		group.Matched = len(matches)
	}
	imageLookupSortTags(matches, options.sortBy, options.sortOrder)
	imageLookupCountPrimeHeads(&group, matches)
	if options.fullScan && len(matches) > options.limit {
		group.Truncated = true
		matches = matches[:options.limit]
	}
	group.Tags = matches
	return group
}

type imageLookupPrimePairResult struct {
	tag         string
	status      string
	detail      string
	completedAt time.Time
	server      rancherImageProvenance
	agent       rancherImageProvenance
	lookupErr   error
}

func (s *imageLookupService) verifyPrimeHeadPairs(ctx context.Context, response *imageLookupSearchResponse, options imageLookupSearchOptions) error {
	authorityGroups := make([]int, 0, 2)
	truncatedReference := ""
	for groupIndex := range response.Groups {
		group := &response.Groups[groupIndex]
		if group.Registry == "stgregistry.suse.com" && group.Error == "" && group.Truncated &&
			(group.ImageRole == "server" || group.ImageRole == "agent") && truncatedReference == "" {
			truncatedReference = group.Reference
		}
		if group.Registry != "stgregistry.suse.com" || group.Error != "" || group.Truncated ||
			(group.ImageRole != "server" && group.ImageRole != "agent") {
			continue
		}
		authorityGroups = append(authorityGroups, groupIndex)
	}
	if len(authorityGroups) == 0 {
		if truncatedReference != "" {
			return fmt.Errorf("Prime head pair verification is incomplete: %s matched more candidates than the bounded search could verify; narrow the patch, commit, or result set", truncatedReference)
		}
		return errors.New("Prime head pair verification is incomplete: neither the SUSE staging server nor agent search produced a successful complete candidate list")
	}

	candidateNames := map[string]struct{}{}
	for _, groupIndex := range authorityGroups {
		authority := &response.Groups[groupIndex]
		for tagIndex := range authority.Tags {
			tag := &authority.Tags[tagIndex]
			if tag.IsPrimeHead && tag.HeadKind == "immutable" && (options.primeVersion == "" || tag.Version == options.primeVersion) {
				candidateNames[tag.Name] = struct{}{}
			}
		}
	}
	for groupIndex := range response.Groups {
		group := &response.Groups[groupIndex]
		if group.Registry != "stgregistry.suse.com" || (group.ImageRole != "server" && group.ImageRole != "agent") {
			continue
		}
		for tagIndex := range group.Tags {
			tag := &group.Tags[tagIndex]
			if tag.IsPrimeHead && tag.HeadKind == "moving" {
				tag.PairStatus = "unverified"
				tag.PairError = "mutable Prime head selector; pair verification ranks immutable patch-qualified commit tags"
			}
		}
	}

	names := make([]string, 0, len(candidateNames))
	for tag := range candidateNames {
		names = append(names, tag)
	}
	sort.Slice(names, func(i, j int) bool { return imageLookupNaturalCompare(names[i], names[j]) > 0 })

	results := make(chan imageLookupPrimePairResult, len(names))
	workerCount := 4
	if len(names) < workerCount {
		workerCount = len(names)
	}
	if workerCount > 0 {
		jobs := make(chan string)
		var workers sync.WaitGroup
		for worker := 0; worker < workerCount; worker++ {
			workers.Add(1)
			go func() {
				defer workers.Done()
				for tag := range jobs {
					results <- s.inspectPrimeHeadPair(ctx, tag)
				}
			}()
		}
		go func() {
			defer close(results)
			for _, tag := range names {
				select {
				case jobs <- tag:
				case <-ctx.Done():
					close(jobs)
					workers.Wait()
					return
				}
			}
			close(jobs)
			workers.Wait()
		}()
	} else {
		close(results)
	}

	byTag := make(map[string]imageLookupPrimePairResult, len(names))
	var lookupErrors []string
	for result := range results {
		byTag[result.tag] = result
		if result.lookupErr != nil {
			lookupErrors = append(lookupErrors, result.tag+": "+imageLookupSafeError(result.lookupErr))
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(lookupErrors) > 0 {
		sort.Strings(lookupErrors)
		return fmt.Errorf("could not safely verify Prime head image pairs: %s", strings.Join(lookupErrors, "; "))
	}

	verified := make([]imageLookupPrimePairResult, 0, len(byTag))
	for _, result := range byTag {
		if result.status == "verified" {
			verified = append(verified, result)
		}
	}
	sort.SliceStable(verified, func(i, j int) bool {
		if verified[i].completedAt.Equal(verified[j].completedAt) {
			return imageLookupNaturalCompare(verified[i].tag, verified[j].tag) > 0
		}
		return verified[i].completedAt.After(verified[j].completedAt)
	})
	ranks := make(map[string]int, len(verified))
	for index, result := range verified {
		ranks[result.tag] = index + 1
	}

	for groupIndex := range response.Groups {
		group := &response.Groups[groupIndex]
		for tagIndex := range group.Tags {
			tag := &group.Tags[tagIndex]
			result, ok := byTag[tag.Name]
			if !ok {
				continue
			}
			tag.PairStatus = result.status
			tag.PairComplete = result.status == "verified"
			tag.CompanionVerified = tag.PairComplete
			tag.ProvenanceValid = tag.PairComplete
			tag.PairError = result.detail
			tag.ResolvedRank = ranks[tag.Name]
			if !result.completedAt.IsZero() {
				tag.PairCompletedAt = imageLookupFormatTime(result.completedAt)
			}
			provenance := result.server
			if group.ImageRole == "agent" {
				provenance = result.agent
			}
			tag.PrimeSource = imageLookupRancherPrimeSource(result.server.SourceURL)
			tag.Source = result.server.SourceURL
			tag.CanonicalReference = provenance.CanonicalReference
			tag.OSSRevision = strings.ToLower(strings.TrimSpace(provenance.OSSRevision))
			if !provenance.CreatedAt.IsZero() {
				tag.CreatedAt = imageLookupFormatTime(provenance.CreatedAt)
			}
		}
		if options.pairStatus != "all" {
			filtered := group.Tags[:0]
			for _, tag := range group.Tags {
				if tag.PairStatus == options.pairStatus {
					filtered = append(filtered, tag)
				}
			}
			group.Tags = filtered
			group.Matched = len(filtered)
		}
		imageLookupApplyRecentFilter(group, options)
		if options.sortBy == "pair-completed" {
			imageLookupSortTags(group.Tags, options.sortBy, options.sortOrder)
		}
		imageLookupCountPrimeHeads(group, group.Tags)
		imageLookupCountPrimePairStatuses(group, group.Tags)
	}
	return nil
}

func (s *imageLookupService) inspectPrimeHeadPair(ctx context.Context, tag string) imageLookupPrimePairResult {
	result := imageLookupPrimePairResult{tag: tag}
	classification := imageLookupClassifyTag("stgregistry.suse.com", "rancher/rancher", tag)
	if !classification.IsPrimeHead || classification.HeadKind != "immutable" {
		result.status = "invalid"
		result.detail = "candidate is not an immutable patch-qualified Prime head tag"
		return result
	}
	serverReference := "stgregistry.suse.com/rancher/rancher:" + tag
	agentReference := "stgregistry.suse.com/rancher/rancher-agent:" + tag

	server, serverFound, err := inspectRancherImageReferenceWithService(ctx, s, serverReference)
	result.server = server
	if err != nil {
		result.lookupErr = fmt.Errorf("inspect server image: %w", err)
		return result
	}
	if !serverFound {
		result.status = "missing"
		result.detail = "server image was not found"
		return result
	}
	agent, agentFound, err := inspectRancherImageReferenceWithService(ctx, s, agentReference)
	result.agent = agent
	if err != nil {
		result.lookupErr = fmt.Errorf("inspect agent image: %w", err)
		return result
	}
	if !agentFound {
		result.status = "missing"
		result.detail = "matching rancher-agent image was not found"
		return result
	}
	if err := imageLookupValidateExactPrimeHeadPair(tag, server, agent); err != nil {
		result.status = "invalid"
		result.detail = err.Error()
		return result
	}
	if err := validatePatchHeadServerProvenance(classification.BaseTag, server); err != nil {
		result.status = "invalid"
		result.detail = err.Error()
		return result
	}
	if server.CreatedAt.IsZero() {
		result.status = "invalid"
		result.detail = "server image did not declare a creation timestamp"
		return result
	}
	if agent.CreatedAt.IsZero() {
		result.status = "invalid"
		result.detail = "agent image did not declare a creation timestamp"
		return result
	}
	result.status = "verified"
	result.completedAt = server.CreatedAt
	if agent.CreatedAt.After(result.completedAt) {
		result.completedAt = agent.CreatedAt
	}
	return result
}

func imageLookupValidateExactPrimeHeadPair(tag string, server, agent rancherImageProvenance) error {
	expectedTag := normalizeDockerRancherTag(normalizeVersionInput(tag))
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

func (s *imageLookupService) Inspect(ctx context.Context, request imageLookupInspectRequest) (imageLookupInspectResponse, error) {
	s.defaults()
	parsed, err := s.parseReference(request.Reference, true)
	if err != nil {
		return imageLookupInspectResponse{}, err
	}
	platform, err := imageLookupParsePlatform(request.Platform)
	if err != nil {
		return imageLookupInspectResponse{}, err
	}

	descriptor, err := remote.Get(parsed.parsed, s.remoteOptions(ctx, platform, 0)...)
	if err != nil {
		return imageLookupInspectResponse{}, err
	}
	response := imageLookupInspectResponse{
		Reference:  parsed.canonical,
		Registry:   parsed.registry,
		Repository: parsed.repository,
		Tag:        parsed.tag,
		Digest:     descriptor.Digest.String(),
		MediaType:  string(descriptor.MediaType),
		Platform:   platform.String(),
		Platforms:  []imageLookupPlatform{},
		Layers:     []imageLookupLayer{},
		BuildYAML:  imageLookupBuildYAML{},
		Warnings:   []string{},
	}

	if descriptor.MediaType == types.OCIImageIndex || descriptor.MediaType == types.DockerManifestList {
		index, indexErr := descriptor.ImageIndex()
		if indexErr != nil {
			return imageLookupInspectResponse{}, fmt.Errorf("read image index: %w", indexErr)
		}
		manifest, manifestErr := index.IndexManifest()
		if manifestErr != nil {
			return imageLookupInspectResponse{}, fmt.Errorf("read image index manifest: %w", manifestErr)
		}
		for _, item := range manifest.Manifests {
			entry := imageLookupPlatform{
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
		return imageLookupInspectResponse{}, fmt.Errorf("select image platform %s: %w", platform.String(), err)
	}
	configFile, err := image.ConfigFile()
	if err != nil {
		return imageLookupInspectResponse{}, fmt.Errorf("read image config: %w", err)
	}
	manifest, err := image.Manifest()
	if err != nil {
		return imageLookupInspectResponse{}, fmt.Errorf("read image manifest: %w", err)
	}
	response.Config = imageLookupImageConfig{
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
		response.Layers = append(response.Layers, imageLookupLayer{
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

func (s *imageLookupService) FetchSourceBuildYAML(ctx context.Context, request imageLookupSourceBuildYAMLRequest) (imageLookupSourceBuildYAMLResponse, error) {
	s.defaults()
	parsed, err := s.parseReference(request.Reference, true)
	if err != nil {
		return imageLookupSourceBuildYAMLResponse{}, err
	}
	platform, err := imageLookupParsePlatform(request.Platform)
	if err != nil {
		return imageLookupSourceBuildYAMLResponse{}, err
	}
	expectedDigest := strings.TrimSpace(request.ExpectedDigest)
	if !imageLookupDigestPattern.MatchString(expectedDigest) {
		return imageLookupSourceBuildYAMLResponse{}, &imageLookupInputError{message: "expectedDigest must be a sha256 image digest"}
	}

	descriptor, err := remote.Get(parsed.parsed, s.remoteOptions(ctx, platform, 0)...)
	if err != nil {
		return imageLookupSourceBuildYAMLResponse{}, err
	}
	resolvedDigest := descriptor.Digest.String()
	if !strings.EqualFold(resolvedDigest, expectedDigest) {
		return imageLookupSourceBuildYAMLResponse{}, &imageLookupConflictError{message: "image reference moved since inspection; inspect it again before fetching build.yaml"}
	}

	image, err := descriptor.Image()
	if err != nil {
		return imageLookupSourceBuildYAMLResponse{}, fmt.Errorf("select image platform %s: %w", platform.String(), err)
	}
	configFile, err := image.ConfigFile()
	if err != nil {
		return imageLookupSourceBuildYAMLResponse{}, fmt.Errorf("read image config: %w", err)
	}
	source := configFile.Config.Labels[imageLookupSourceLabel]
	revision := configFile.Config.Labels[imageLookupRevisionLabel]
	owner, repository, err := imageLookupParseGitHubSource(source, revision)
	if err != nil {
		return imageLookupSourceBuildYAMLResponse{}, err
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
		raw, commandErr := s.runCommand(commandCtx, "gh", arguments, imageLookupSanitizedGHEnvironment(), imageLookupMaxSourceBuildYAML)
		if commandErr == nil {
			return raw, nil
		}
		switch {
		case errors.Is(commandCtx.Err(), context.DeadlineExceeded):
			return nil, errors.New("declared-source build.yaml request timed out")
		case errors.Is(commandErr, errImageLookupCommandOutputLimit):
			return nil, fmt.Errorf("declared-source build.yaml exceeds the %s response limit", imageLookupByteSize(imageLookupMaxSourceBuildYAML))
		default:
			return nil, errors.New("could not fetch build.yaml from the declared GitHub source; confirm GitHub CLI authentication and repository access")
		}
	}

	origin := "declared-source"
	repositoryURL := "https://github.com/" + owner + "/" + repository
	revisionLabel := imageLookupRevisionLabel
	raw, fetchErr := fetchFromGitHub(owner, repository, revision)
	if fetchErr != nil && owner == "rancher" && repository == "rancher-prime" {
		// rancher-prime images can declare the matching public rancher/rancher
		// commit separately. Keep this fallback exact and revision-pinned so an
		// arbitrary source label cannot redirect the authenticated gh request.
		ossRevision := configFile.Config.Labels[imageLookupOSSRevisionLabel]
		if imageLookupGitRevisionPattern.MatchString(ossRevision) {
			ossRevision = strings.ToLower(ossRevision)
			fallbackRaw, fallbackErr := fetchFromGitHub("rancher", "rancher", ossRevision)
			if fallbackErr == nil {
				raw = fallbackRaw
				fetchErr = nil
				origin = "declared-oss-source"
				repositoryURL = "https://github.com/rancher/rancher"
				revision = ossRevision
				revisionLabel = imageLookupOSSRevisionLabel
			} else {
				fetchErr = fallbackErr
			}
		}
	}
	if fetchErr != nil {
		return imageLookupSourceBuildYAMLResponse{}, fetchErr
	}
	if len(raw) == 0 {
		return imageLookupSourceBuildYAMLResponse{}, errors.New("declared GitHub source returned an empty build.yaml")
	}
	if int64(len(raw)) > imageLookupMaxSourceBuildYAML {
		return imageLookupSourceBuildYAMLResponse{}, fmt.Errorf("declared-source build.yaml exceeds the %s response limit", imageLookupByteSize(imageLookupMaxSourceBuildYAML))
	}
	var data map[string]any
	if err := yaml.Unmarshal(raw, &data); err != nil {
		return imageLookupSourceBuildYAMLResponse{}, errors.New("declared GitHub source returned invalid build.yaml content")
	}
	if data == nil {
		data = map[string]any{}
	}
	selectedPlatform := platform.String()
	if configFile.OS != "" && configFile.Architecture != "" {
		selectedPlatform = (&v1.Platform{OS: configFile.OS, Architecture: configFile.Architecture, Variant: configFile.Variant}).String()
	}
	return imageLookupSourceBuildYAMLResponse{
		Found:  true,
		Path:   "build.yaml",
		Origin: origin,
		Provenance: imageLookupSourceBuildYAMLProvenance{
			RepositoryURL:  repositoryURL,
			Revision:       revision,
			Path:           "build.yaml",
			ImageReference: parsed.canonical,
			ImageDigest:    resolvedDigest,
			Platform:       selectedPlatform,
			SourceLabel:    imageLookupSourceLabel,
			RevisionLabel:  revisionLabel,
		},
		Raw:  string(raw),
		Data: data,
	}, nil
}

func (s *imageLookupService) findBuildYAML(ctx context.Context, image v1.Image, warnings []string) (imageLookupBuildYAML, []string) {
	layers, err := image.Layers()
	if err != nil {
		return imageLookupBuildYAML{Error: imageLookupSafeError(err), Skipped: true}, append(warnings, "build.yaml scan could not enumerate image layers")
	}

	hiddenPaths := map[string]struct{}{}
	hiddenDirectories := map[string]struct{}{}
	var scanned int64
	skippedLargeLayers := 0
	skippedUnknownSizeLayers := 0
	for index := len(layers) - 1; index >= 0; index-- {
		if err := ctx.Err(); err != nil {
			return imageLookupBuildYAML{Error: "build.yaml scan timed out", Skipped: true}, append(warnings, "build.yaml scan timed out")
		}
		compressedSize, sizeErr := layers[index].Size()
		if sizeErr != nil || compressedSize < 0 {
			skippedUnknownSizeLayers++
			continue
		}
		if compressedSize > s.maxBuildLayer {
			skippedLargeLayers++
			continue
		}
		mediaType, mediaErr := layers[index].MediaType()
		if mediaErr != nil {
			warnings = append(warnings, fmt.Sprintf("build.yaml scan skipped layer %d with unknown media type", index))
			continue
		}
		if mediaType == types.OCILayerZStd {
			warnings = append(warnings, fmt.Sprintf("build.yaml scan skipped zstd layer %d", index))
			continue
		}
		reader, openErr := layers[index].Uncompressed()
		if openErr != nil {
			warnings = append(warnings, fmt.Sprintf("build.yaml scan skipped unreadable layer %d", index))
			continue
		}
		candidate, layerErr := s.scanBuildYAMLLayer(ctx, reader, hiddenPaths, hiddenDirectories, &scanned)
		_ = reader.Close()
		if layerErr != nil {
			if errors.Is(layerErr, errImageLookupScanLimit) {
				reason := imageLookupBuildYAMLScanLimitReason(s.maxLayerScan)
				if skippedReason := imageLookupBuildYAMLSkipReason(skippedLargeLayers, skippedUnknownSizeLayers, s.maxBuildLayer); skippedReason != "" {
					reason = skippedReason + " " + reason
				}
				return imageLookupBuildYAML{Reason: reason, Skipped: true}, append(warnings, reason)
			}
			warnings = append(warnings, fmt.Sprintf("build.yaml scan skipped malformed layer %d", index))
			continue
		}
		if candidate != nil {
			warnings = imageLookupAppendBuildYAMLSkipWarning(warnings, skippedLargeLayers, skippedUnknownSizeLayers, s.maxBuildLayer)
			result := imageLookupBuildYAML{Found: true, Path: candidate.path, Raw: string(candidate.content)}
			var data map[string]any
			if yamlErr := yaml.Unmarshal(candidate.content, &data); yamlErr != nil {
				result.Error = "build.yaml was found but could not be parsed: " + imageLookupSafeError(yamlErr)
				warnings = append(warnings, "build.yaml was found but contains invalid YAML")
			} else {
				result.Data = data
			}
			return result, warnings
		}
	}
	if reason := imageLookupBuildYAMLSkipReason(skippedLargeLayers, skippedUnknownSizeLayers, s.maxBuildLayer); reason != "" {
		return imageLookupBuildYAML{Skipped: true, Reason: reason}, append(warnings, reason)
	}
	return imageLookupBuildYAML{}, warnings
}

func imageLookupAppendBuildYAMLSkipWarning(warnings []string, skippedLarge, skippedUnknown int, limit int64) []string {
	if reason := imageLookupBuildYAMLSkipReason(skippedLarge, skippedUnknown, limit); reason != "" {
		return append(warnings, reason)
	}
	return warnings
}

func imageLookupBuildYAMLSkipReason(skippedLarge, skippedUnknown int, limit int64) string {
	reasons := make([]string, 0, 2)
	if skippedLarge > 0 {
		reasons = append(reasons, fmt.Sprintf(
			"Skipped %d layer%s larger than the %s safe scan limit.",
			skippedLarge,
			imageLookupPlural(skippedLarge),
			imageLookupByteSize(limit),
		))
	}
	if skippedUnknown > 0 {
		reasons = append(reasons, fmt.Sprintf(
			"Skipped %d layer%s because the compressed size could not be verified.",
			skippedUnknown,
			imageLookupPlural(skippedUnknown),
		))
	}
	return strings.Join(reasons, " ")
}

func imageLookupBuildYAMLScanLimitReason(limit int64) string {
	return fmt.Sprintf(
		"Stopped after reaching the %s cumulative uncompressed safe scan limit; remaining image layer data was not scanned.",
		imageLookupByteSize(limit),
	)
}

func imageLookupPlural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func imageLookupByteSize(size int64) string {
	if size > 0 && size%(1<<20) == 0 {
		return fmt.Sprintf("%d MiB", size>>20)
	}
	if size > 0 && size%(1<<10) == 0 {
		return fmt.Sprintf("%d KiB", size>>10)
	}
	return fmt.Sprintf("%d bytes", size)
}

var errImageLookupScanLimit = errors.New("image layer scan limit exceeded")

type imageLookupBuildCandidate struct {
	path    string
	content []byte
}

type imageLookupCountingReader struct {
	reader io.Reader
	count  *int64
	limit  int64
}

func (r *imageLookupCountingReader) Read(buffer []byte) (int, error) {
	if *r.count >= r.limit {
		return 0, errImageLookupScanLimit
	}
	remaining := r.limit - *r.count
	if int64(len(buffer)) > remaining {
		buffer = buffer[:remaining]
	}
	n, err := r.reader.Read(buffer)
	*r.count += int64(n)
	if err == nil && *r.count >= r.limit {
		return n, errImageLookupScanLimit
	}
	return n, err
}

func (s *imageLookupService) scanBuildYAMLLayer(ctx context.Context, reader io.Reader, hiddenPaths, hiddenDirectories map[string]struct{}, scanned *int64) (*imageLookupBuildCandidate, error) {
	archive := tar.NewReader(&imageLookupCountingReader{reader: reader, count: scanned, limit: s.maxLayerScan})
	candidates := map[string][]byte{}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		cleaned, valid := imageLookupCleanArchivePath(header.Name)
		if !valid {
			continue
		}
		base := path.Base(cleaned)
		directory := path.Dir(cleaned)
		if base == ".wh..wh..opq" {
			hiddenDirectories[directory] = struct{}{}
			continue
		}
		if strings.HasPrefix(base, ".wh.") {
			hiddenPaths[path.Join(directory, strings.TrimPrefix(base, ".wh."))] = struct{}{}
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			continue
		}
		if base != "build.yaml" || imageLookupArchivePathHidden(cleaned, hiddenPaths, hiddenDirectories) {
			continue
		}
		if header.Size < 0 || header.Size > s.maxBuildYML {
			continue
		}
		content, readErr := io.ReadAll(io.LimitReader(archive, s.maxBuildYML+1))
		if readErr != nil {
			return nil, readErr
		}
		if int64(len(content)) > s.maxBuildYML {
			continue
		}
		candidates[cleaned] = content
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	paths := make([]string, 0, len(candidates))
	for candidatePath := range candidates {
		paths = append(paths, candidatePath)
	}
	sort.Slice(paths, func(i, j int) bool {
		if len(paths[i]) != len(paths[j]) {
			return len(paths[i]) < len(paths[j])
		}
		return paths[i] < paths[j]
	})
	return &imageLookupBuildCandidate{path: paths[0], content: candidates[paths[0]]}, nil
}

func (s *imageLookupService) searchTargets(request imageLookupSearchRequest) ([]imageLookupTarget, string, int, error) {
	targets, options, err := s.searchParameters(request)
	if err != nil {
		return nil, "", 0, err
	}
	return targets, options.query, options.limit, nil
}

func (s *imageLookupService) searchParameters(request imageLookupSearchRequest) ([]imageLookupTarget, imageLookupSearchOptions, error) {
	options := imageLookupSearchOptions{
		includeArtifacts: request.IncludeArtifacts,
		channel:          "all",
		architecture:     "all",
		primeHead:        "all",
		headKind:         "all",
		pairStatus:       "all",
		sortBy:           "natural",
		sortOrder:        "desc",
		scanMode:         "auto",
	}
	registryValue := strings.TrimSpace(request.Registry)
	if registryValue == "" {
		registryValue = "all"
	}
	query := strings.TrimSpace(request.Query)
	if len(query) > 256 {
		return nil, imageLookupSearchOptions{}, &imageLookupInputError{message: "query must be 256 characters or fewer"}
	}
	if imageLookupHasUnsafeCharacters(query) {
		return nil, imageLookupSearchOptions{}, &imageLookupInputError{message: "query contains whitespace or control characters"}
	}
	limit := request.Limit
	if limit == 0 {
		limit = imageLookupDefaultResultLimit
	}
	if limit < 1 || limit > imageLookupMaxResultLimit {
		return nil, imageLookupSearchOptions{}, &imageLookupInputError{message: fmt.Sprintf("limit must be between 1 and %d", imageLookupMaxResultLimit)}
	}
	options.limit = limit
	if request.RecentDays < 0 || request.RecentDays > imageLookupMaxRecentDays {
		return nil, imageLookupSearchOptions{}, &imageLookupInputError{message: fmt.Sprintf("recentDays must be 0 (all dates) or between 1 and %d", imageLookupMaxRecentDays)}
	}
	options.recentDays = request.RecentDays

	repositoryValue := strings.TrimSpace(request.Repository)
	var explicitRegistry string
	bareDigestQuery := imageLookupDigestPattern.MatchString(strings.ToLower(query))
	if !bareDigestQuery && imageLookupLooksLikeReference(query) {
		parsed, err := s.parseReference(query, true)
		if err != nil {
			return nil, imageLookupSearchOptions{}, err
		}
		options.exactLookup = true
		explicitRegistry = parsed.registry
		repositoryValue = parsed.repository
		if parsed.tag != "" {
			query = parsed.tag
		} else {
			query = parsed.digest
		}
	}

	if repositoryValue == "" || strings.EqualFold(repositoryValue, "all") {
		repositoryValue = "all"
	} else {
		parsedRegistry, repository, selector, err := s.parseSearchRepository(repositoryValue)
		if err != nil {
			return nil, imageLookupSearchOptions{}, err
		}
		if parsedRegistry != "" {
			explicitRegistry = parsedRegistry
		}
		repositoryValue = repository
		if selector != "" && query == "" {
			query = selector
			options.exactLookup = true
		}
	}

	var err error
	if options.channel, err = imageLookupChoice(request.Channel, "all", "all", "head", "devel", "alpha", "rcs", "rc", "stable"); err != nil {
		return nil, imageLookupSearchOptions{}, err
	}
	if options.architecture, err = imageLookupChoice(request.Architecture, "all", "all", "multi", "amd64", "arm64", "s390x", "ppc64le", "386", "arm"); err != nil {
		return nil, imageLookupSearchOptions{}, err
	}
	if options.primeHead, err = imageLookupChoice(request.PrimeHead, "all", "all", "only", "exclude"); err != nil {
		return nil, imageLookupSearchOptions{}, err
	}
	if options.headKind, err = imageLookupChoice(request.HeadKind, "all", "all", "moving", "immutable"); err != nil {
		return nil, imageLookupSearchOptions{}, err
	}
	if options.pairStatus, err = imageLookupChoice(request.PairStatus, "all", "all", "verified", "unverified", "missing", "invalid"); err != nil {
		return nil, imageLookupSearchOptions{}, err
	}
	if options.sortBy, err = imageLookupChoice(request.SortBy, "natural", "natural", "tag", "version", "uploaded", "pair-completed"); err != nil {
		return nil, imageLookupSearchOptions{}, err
	}
	if options.sortOrder, err = imageLookupChoice(request.SortOrder, "desc", "asc", "desc"); err != nil {
		return nil, imageLookupSearchOptions{}, err
	}
	if options.scanMode, err = imageLookupChoice(request.ScanMode, "auto", "auto", "bounded", "complete"); err != nil {
		return nil, imageLookupSearchOptions{}, err
	}
	if options.versionLine, err = imageLookupNormalizeVersionLine(request.VersionLine); err != nil {
		return nil, imageLookupSearchOptions{}, err
	}
	options.commit = strings.ToLower(strings.TrimSpace(request.Commit))
	if options.commit != "" && !imageLookupCommitPrefix(options.commit) {
		return nil, imageLookupSearchOptions{}, &imageLookupInputError{message: "commit must be a 7 to 40 character hexadecimal Git revision prefix"}
	}
	if options.primeHead == "exclude" && options.headKind != "all" {
		return nil, imageLookupSearchOptions{}, &imageLookupInputError{message: "headKind cannot be combined with primeHead=exclude"}
	}

	query = strings.TrimSpace(query)
	primeQueryVersion, primeQueryKind := imageLookupPrimeHeadQuery(query)
	primeAliasQuery := imageLookupPrimeHeadAliasQuery(query)
	if primeAliasQuery {
		options.primeHead = "only"
		query = "prime-head"
	}
	if primeQueryKind != "" {
		options.primeHead = "only"
		options.primeVersion = primeQueryVersion
		options.verifyPrimePairs = true
		if primeQueryKind == "moving" && strings.TrimSpace(request.SortBy) == "" {
			options.sortBy = "pair-completed"
		}
	}
	if options.headKind != "all" {
		options.primeHead = "only"
	}
	if options.primeHead == "only" && imageLookupPatchVersion(options.versionLine) {
		options.primeVersion = options.versionLine
		options.verifyPrimePairs = true
	}
	if options.primeHead == "only" && imageLookupBarePatchVersion(query) {
		options.primeVersion, err = imageLookupNormalizeVersionLine(query)
		if err != nil {
			return nil, imageLookupSearchOptions{}, err
		}
		options.verifyPrimePairs = true
		if strings.TrimSpace(request.SortBy) == "" {
			options.sortBy = "pair-completed"
		}
	}
	if options.pairStatus != "all" || options.sortBy == "pair-completed" {
		if !options.verifyPrimePairs {
			return nil, imageLookupSearchOptions{}, &imageLookupInputError{message: "pairStatus and pair-completed sorting require an exact Prime head tag or patch-qualified Prime selector"}
		}
	}
	options.query = query
	autoFullScan := imageLookupEnrichedSearchRequested(request) || primeAliasQuery || primeQueryKind == "moving" || imageLookupBarePatchVersion(query)
	switch options.scanMode {
	case "bounded":
		// Date filtering and pair verification require a complete candidate set.
		// Other filters and sorts may operate on the bounded sample; Truncated
		// tells clients that the result is not a global ordering.
		options.fullScan = options.recentDays > 0 || options.verifyPrimePairs
	case "complete":
		options.fullScan = true
	default:
		options.fullScan = autoFullScan
	}

	registries := []string{}
	if explicitRegistry != "" {
		registries = []string{explicitRegistry}
	} else if strings.EqualFold(registryValue, "all") {
		if options.primeHead == "only" || primeQueryKind != "" {
			registries = []string{"stgregistry.suse.com"}
		} else {
			registries = append(registries, imageLookupKnownRegistries...)
		}
	} else {
		registry, err := imageLookupNormalizeRegistry(registryValue)
		if err != nil {
			return nil, imageLookupSearchOptions{}, err
		}
		registries = []string{registry}
	}

	repositories := []string{repositoryValue}
	if repositoryValue == "all" {
		if options.primeHead == "only" || primeQueryKind != "" {
			repositories = []string{"rancher/rancher", "rancher/rancher-agent"}
		} else {
			repositories = append([]string(nil), imageLookupKnownRepositories...)
		}
	}
	targets := make([]imageLookupTarget, 0, len(registries)*len(repositories))
	for _, registry := range registries {
		for _, repository := range repositories {
			if _, err := s.parseRepository(registry, repository); err != nil {
				return nil, imageLookupSearchOptions{}, err
			}
			targets = append(targets, imageLookupTarget{registry: registry, repository: repository})
		}
	}
	if options.primeHead == "only" || primeQueryKind != "" {
		for _, target := range targets {
			if target.registry != "stgregistry.suse.com" {
				return nil, imageLookupSearchOptions{}, &imageLookupInputError{message: "Prime-head image search is supported only in stgregistry.suse.com"}
			}
			if target.repository != "rancher/rancher" && target.repository != "rancher/rancher-agent" {
				return nil, imageLookupSearchOptions{}, &imageLookupInputError{message: "Prime-head image search requires the canonical rancher/rancher or rancher/rancher-agent repository"}
			}
		}
	}
	if (options.pairStatus != "all" || options.sortBy == "pair-completed") && !options.verifyPrimePairs {
		return nil, imageLookupSearchOptions{}, &imageLookupInputError{message: "pair filtering and pair-completed sorting are supported only for SUSE staging Prime head lookups"}
	}
	return targets, options, nil
}

func (s *imageLookupService) parseSearchRepository(input string) (string, string, string, error) {
	if len(input) > 512 {
		return "", "", "", &imageLookupInputError{message: "repository must be 512 characters or fewer"}
	}
	cleaned, err := imageLookupStripScheme(input, s.allowHTTP)
	if err != nil {
		return "", "", "", err
	}
	if imageLookupHasUnsafeCharacters(cleaned) || strings.ContainsAny(cleaned, "?#\\") {
		return "", "", "", &imageLookupInputError{message: "repository contains invalid characters"}
	}

	selector := ""
	repositoryPart := cleaned
	if at := strings.LastIndex(repositoryPart, "@"); at >= 0 {
		selector = repositoryPart[at+1:]
		repositoryPart = repositoryPart[:at]
	} else if colon := strings.LastIndex(repositoryPart, ":"); colon > strings.LastIndex(repositoryPart, "/") {
		selector = repositoryPart[colon+1:]
		repositoryPart = repositoryPart[:colon]
	}
	parts := strings.Split(repositoryPart, "/")
	if len(parts) == 0 {
		return "", "", "", &imageLookupInputError{message: "repository is required"}
	}
	explicitRegistry := ""
	if imageLookupFirstComponentIsRegistry(parts[0]) {
		explicitRegistry, err = imageLookupNormalizeRegistry(parts[0])
		if err != nil {
			return "", "", "", err
		}
		parts = parts[1:]
	}
	repository := strings.Join(parts, "/")
	if repository == "" {
		return "", "", "", &imageLookupInputError{message: "repository path is required"}
	}
	if len(parts) == 1 && (explicitRegistry == "" || explicitRegistry == "docker.io") {
		repository = "library/" + repository
	}
	validationRegistry := explicitRegistry
	if validationRegistry == "" {
		validationRegistry = "docker.io"
	}
	if _, parseErr := s.parseRepository(validationRegistry, repository); parseErr != nil {
		return "", "", "", parseErr
	}
	if selector != "" {
		if _, parseErr := s.parseReference(validationRegistry+"/"+repository+":"+selector, true); parseErr != nil {
			if _, digestErr := s.parseReference(validationRegistry+"/"+repository+"@"+selector, true); digestErr != nil {
				return "", "", "", &imageLookupInputError{message: "repository tag or digest is invalid"}
			}
		}
	}
	return explicitRegistry, repository, selector, nil
}

func (s *imageLookupService) parseRepository(registry, repository string) (name.Repository, error) {
	registry, err := imageLookupNormalizeRegistry(registry)
	if err != nil {
		return name.Repository{}, err
	}
	repository = strings.Trim(strings.TrimSpace(repository), "/")
	if repository == "" || len(repository) > 255 || strings.Contains(repository, "..") || imageLookupHasUnsafeCharacters(repository) || strings.ContainsAny(repository, "@:#?\\") {
		return name.Repository{}, &imageLookupInputError{message: "repository path is invalid"}
	}
	options := []name.Option{name.StrictValidation}
	if s.allowHTTP {
		options = append(options, name.Insecure)
	}
	parsed, parseErr := name.NewRepository(imageLookupRegistryForLibrary(registry)+"/"+repository, options...)
	if parseErr != nil {
		return name.Repository{}, &imageLookupInputError{message: "repository path is invalid: " + parseErr.Error()}
	}
	return parsed, nil
}

func (s *imageLookupService) parseReference(input string, requireSelector bool) (imageLookupReference, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return imageLookupReference{}, &imageLookupInputError{message: "image reference is required"}
	}
	if len(input) > 1024 {
		return imageLookupReference{}, &imageLookupInputError{message: "image reference must be 1024 characters or fewer"}
	}
	cleaned, err := imageLookupStripScheme(input, s.allowHTTP)
	if err != nil {
		return imageLookupReference{}, err
	}
	if imageLookupHasUnsafeCharacters(cleaned) || strings.ContainsAny(cleaned, "?#\\") {
		return imageLookupReference{}, &imageLookupInputError{message: "image reference contains invalid characters"}
	}
	for _, component := range strings.Split(cleaned, "/") {
		if component == "." || component == ".." {
			return imageLookupReference{}, &imageLookupInputError{message: "image reference may not contain path traversal"}
		}
	}

	if !strings.Contains(cleaned, "/") {
		cleaned = "docker.io/library/" + cleaned
	} else {
		first := strings.SplitN(cleaned, "/", 2)[0]
		if !imageLookupFirstComponentIsRegistry(first) {
			cleaned = "docker.io/" + cleaned
		}
	}
	parts := strings.SplitN(cleaned, "/", 2)
	normalizedRegistry, registryErr := imageLookupNormalizeRegistry(parts[0])
	if registryErr != nil {
		return imageLookupReference{}, registryErr
	}
	cleaned = normalizedRegistry + "/" + parts[1]
	lastSlash := strings.LastIndex(cleaned, "/")
	hasDigest := strings.LastIndex(cleaned, "@") > lastSlash
	hasTag := strings.LastIndex(cleaned, ":") > lastSlash
	if requireSelector && !hasDigest && !hasTag {
		return imageLookupReference{}, &imageLookupInputError{message: "image reference must include a tag or digest"}
	}
	options := []name.Option{name.StrictValidation}
	if s.allowHTTP {
		options = append(options, name.Insecure)
	}
	parsed, parseErr := name.ParseReference(imageLookupRegistryForLibraryReference(cleaned), options...)
	if parseErr != nil {
		return imageLookupReference{}, &imageLookupInputError{message: "image reference is invalid: " + parseErr.Error()}
	}
	registry := imageLookupRegistryForDisplay(parsed.Context().RegistryStr())
	repository := parsed.Context().RepositoryStr()
	result := imageLookupReference{parsed: parsed, registry: registry, repository: repository}
	switch typed := parsed.(type) {
	case name.Tag:
		result.tag = typed.TagStr()
		result.canonical = registry + "/" + repository + ":" + typed.TagStr()
	case name.Digest:
		result.digest = typed.DigestStr()
		result.canonical = registry + "/" + repository + "@" + typed.DigestStr()
	default:
		return imageLookupReference{}, &imageLookupInputError{message: "image reference must include a tag or digest"}
	}
	return result, nil
}

func imageLookupParsePlatform(input string) (*v1.Platform, error) {
	platformText := strings.TrimSpace(input)
	if platformText == "" {
		platformText = "linux/amd64"
	}
	if len(platformText) > 64 {
		return nil, &imageLookupInputError{message: "platform is too long"}
	}
	platform, err := v1.ParsePlatform(platformText)
	if err != nil || platform.OS == "" || platform.Architecture == "" {
		return nil, &imageLookupInputError{message: "platform must use os/architecture or os/architecture/variant format"}
	}
	if !imageLookupSimpleToken(platform.OS) || !imageLookupSimpleToken(platform.Architecture) || (platform.Variant != "" && !imageLookupSimpleToken(platform.Variant)) {
		return nil, &imageLookupInputError{message: "platform contains invalid characters"}
	}
	return platform, nil
}

func imageLookupParseGitHubSource(source, revision string) (string, string, error) {
	if source == "" {
		return "", "", &imageLookupSourceMetadataError{message: "image does not declare org.opencontainers.image.source"}
	}
	if revision == "" {
		return "", "", &imageLookupSourceMetadataError{message: "image does not declare org.opencontainers.image.revision"}
	}
	if !imageLookupGitRevisionPattern.MatchString(revision) {
		return "", "", &imageLookupSourceMetadataError{message: "image revision label is not a full 40-character Git commit SHA"}
	}
	metadataError := func() error {
		return &imageLookupSourceMetadataError{message: "image source label must be an exact https://github.com/{owner}/{repository} URL, optionally ending in .git"}
	}
	parsed, err := url.Parse(source)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" {
		return "", "", metadataError()
	}
	components := strings.Split(strings.TrimPrefix(parsed.Path, "/"), "/")
	if len(components) != 2 || components[0] == "" || components[1] == "" {
		return "", "", metadataError()
	}

	// OCI source labels commonly use the clone URL form ending in lowercase
	// ".git". Accept exactly one such suffix, but keep the repository name
	// canonical for GitHub API requests and provenance.
	repository := components[1]
	if strings.HasSuffix(repository, ".git") {
		repository = strings.TrimSuffix(repository, ".git")
		if repository == "" || strings.HasSuffix(repository, ".git") {
			return "", "", metadataError()
		}
	} else if len(repository) >= len(".git") && strings.EqualFold(repository[len(repository)-len(".git"):], ".git") {
		// The optional clone suffix is intentionally case-sensitive.
		return "", "", metadataError()
	}
	if !imageLookupGitHubPathComponent(components[0]) || !imageLookupGitHubPathComponent(repository) {
		return "", "", metadataError()
	}
	canonical := "https://github.com/" + components[0] + "/" + repository
	if source != canonical && source != canonical+".git" {
		return "", "", metadataError()
	}
	return components[0], repository, nil
}

func imageLookupGitHubPathComponent(value string) bool {
	if len(value) == 0 || len(value) > 100 || value == "." || value == ".." {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '-' || character == '_' || character == '.' {
			continue
		}
		return false
	}
	return true
}

func (s *imageLookupService) remoteOptions(ctx context.Context, platform *v1.Platform, pageSize int) []remote.Option {
	options := []remote.Option{
		remote.WithContext(ctx),
		remote.WithTransport(s.transport),
		remote.WithAuthFromKeychain(s.keychain),
		remote.WithUserAgent("rancher-runway-image-lookup"),
	}
	if platform != nil {
		options = append(options, remote.WithPlatform(*platform))
	}
	if pageSize > 0 {
		options = append(options, remote.WithPageSize(pageSize))
	}
	return options
}

func imageLookupExactTagReference(repository name.Repository, query string, allowHTTP bool) (name.Tag, bool) {
	query = strings.TrimSpace(query)
	if query == "" || imageLookupQuickQuery(query) || strings.EqualFold(query, "all") {
		return name.Tag{}, false
	}
	options := []name.Option{name.StrictValidation}
	if allowHTTP {
		options = append(options, name.Insecure)
	}
	tag, err := name.NewTag(repository.Name()+":"+query, options...)
	return tag, err == nil
}

func imageLookupExactDigestReference(repository name.Repository, query string, allowHTTP bool) (name.Digest, bool) {
	query = strings.ToLower(strings.TrimSpace(query))
	if !imageLookupDigestPattern.MatchString(query) {
		return name.Digest{}, false
	}
	options := []name.Option{name.StrictValidation}
	if allowHTTP {
		options = append(options, name.Insecure)
	}
	digest, err := name.NewDigest(repository.Name()+"@"+query, options...)
	return digest, err == nil
}

func imageLookupFullVersionTag(query string) bool {
	return imageLookupFullVersionTagPattern.MatchString(strings.TrimSpace(query))
}

type imageLookupCredentialKeychain struct{}

func (imageLookupCredentialKeychain) Resolve(resource authn.Resource) (authn.Authenticator, error) {
	registry := imageLookupRegistryForDisplay(resource.RegistryStr())
	username := strings.TrimSpace(os.Getenv("DOCKERHUB_USERNAME"))
	password := os.Getenv("DOCKERHUB_PASSWORD")
	if registry == "docker.io" && username != "" && password != "" {
		return authn.FromConfig(authn.AuthConfig{Username: username, Password: password}), nil
	}
	return authn.DefaultKeychain.Resolve(resource)
}

type imageLookupSafeRoundTripper struct {
	inner http.RoundTripper
}

func (t *imageLookupSafeRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil || request.URL == nil {
		return nil, errors.New("registry request has no URL")
	}
	if request.URL.Scheme != "https" {
		return nil, fmt.Errorf("registry request scheme %q is not allowed", request.URL.Scheme)
	}
	if request.URL.User != nil || request.URL.Hostname() == "" {
		return nil, errors.New("registry request authority is invalid")
	}
	return t.inner.RoundTrip(request)
}

func (t *imageLookupSafeRoundTripper) CloseIdleConnections() {
	if closer, ok := t.inner.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

func (s *imageLookupService) closeIdleConnections() {
	if s == nil {
		return
	}
	if closer, ok := s.transport.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

func newImageLookupSafeTransport() http.RoundTripper {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	base := &http.Transport{
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	base.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("invalid registry address: %w", err)
		}
		addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("resolve registry host: %w", err)
		}
		if len(addresses) == 0 {
			return nil, errors.New("registry host did not resolve")
		}
		for _, candidate := range addresses {
			if !imageLookupPublicIP(candidate) {
				return nil, fmt.Errorf("registry host resolves to a private or reserved address")
			}
		}
		var lastErr error
		for _, candidate := range addresses {
			connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(candidate.String(), port))
			if dialErr == nil {
				return connection, nil
			}
			lastErr = dialErr
		}
		return nil, lastErr
	}
	return &imageLookupSafeRoundTripper{inner: base}
}

var imageLookupBlockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("ff00::/8"),
	netip.MustParsePrefix("2001:db8::/32"),
}

func imageLookupPublicIP(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsValid() || !address.IsGlobalUnicast() {
		return false
	}
	for _, prefix := range imageLookupBlockedPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

type imageLookupDockerHubTag struct {
	Name          string    `json:"name"`
	FullSize      int64     `json:"full_size"`
	TagLastPushed time.Time `json:"tag_last_pushed"`
	Images        []struct {
		Digest string `json:"digest"`
		Size   int64  `json:"size"`
	} `json:"images"`
}

func (s *imageLookupService) dockerHubTag(ctx context.Context, repository, tag string) (imageLookupDockerHubTag, error) {
	parts := strings.Split(repository, "/")
	if len(parts) != 2 {
		return imageLookupDockerHubTag{}, errors.New("Docker Hub metadata requires namespace/repository")
	}
	endpoint := "https://hub.docker.com/v2/namespaces/" + url.PathEscape(parts[0]) + "/repositories/" + url.PathEscape(parts[1]) + "/tags/" + url.PathEscape(tag)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return imageLookupDockerHubTag{}, err
	}
	response, err := (&http.Client{Transport: s.transport}).Do(request)
	if err != nil {
		return imageLookupDockerHubTag{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return imageLookupDockerHubTag{}, fmt.Errorf("Docker Hub metadata returned %s", response.Status)
	}
	var result imageLookupDockerHubTag
	decoder := json.NewDecoder(io.LimitReader(response.Body, 2<<20))
	if err := decoder.Decode(&result); err != nil {
		return imageLookupDockerHubTag{}, err
	}
	return result, nil
}

func (s *imageLookupService) enrichDockerHubTags(ctx context.Context, repository, query string, tags []imageLookupTag, requireComplete bool) (bool, error) {
	parts := strings.Split(repository, "/")
	if len(parts) != 2 {
		return false, errors.New("Docker Hub metadata requires namespace/repository")
	}
	if len(tags) == 0 {
		return true, nil
	}
	indexes := make(map[string][]int, len(tags))
	remaining := make(map[string]struct{}, len(tags))
	for index := range tags {
		indexes[tags[index].Name] = append(indexes[tags[index].Name], index)
		remaining[tags[index].Name] = struct{}{}
	}
	endpointURL, err := url.Parse("https://hub.docker.com/v2/namespaces/" + url.PathEscape(parts[0]) + "/repositories/" + url.PathEscape(parts[1]) + "/tags")
	if err != nil {
		return false, err
	}
	maxPages := 1
	if requireComplete {
		maxPages = (s.maxTagScan + 99) / 100
		if maxPages < 1 {
			maxPages = 1
		}
	}
	client := &http.Client{
		Transport: s.transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	for pageNumber := 1; pageNumber <= maxPages; pageNumber++ {
		pageURL := *endpointURL
		values := pageURL.Query()
		values.Set("page_size", "100")
		values.Set("page", strconv.Itoa(pageNumber))
		if query != "" && !imageLookupQuickQuery(query) {
			values.Set("name", query)
		}
		pageURL.RawQuery = values.Encode()
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, pageURL.String(), nil)
		if requestErr != nil {
			return false, requestErr
		}
		response, requestErr := client.Do(request)
		if requestErr != nil {
			return false, requestErr
		}
		var page struct {
			Next    string                    `json:"next"`
			Results []imageLookupDockerHubTag `json:"results"`
		}
		decodeErr := func() error {
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				return fmt.Errorf("Docker Hub metadata returned %s", response.Status)
			}
			return json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&page)
		}()
		if decodeErr != nil {
			return false, decodeErr
		}
		for _, metadata := range page.Results {
			matchedIndexes, ok := indexes[metadata.Name]
			if !ok {
				continue
			}
			for _, index := range matchedIndexes {
				tags[index].UploadedAt = imageLookupFormatTime(metadata.TagLastPushed)
				tags[index].Size = metadata.FullSize
				if len(metadata.Images) == 1 {
					tags[index].Digest = metadata.Images[0].Digest
				}
			}
			if !metadata.TagLastPushed.IsZero() {
				delete(remaining, metadata.Name)
			}
		}
		if len(remaining) == 0 {
			return true, nil
		}
		if page.Next == "" {
			return false, nil
		}
	}
	return false, errors.New("Docker Hub tag metadata exceeded the bounded pagination limit")
}

func imageLookupNormalizeRegistry(input string) (string, error) {
	input = strings.TrimSpace(strings.ToLower(input))
	if input == "" || len(input) > 253 || strings.ContainsAny(input, "/@?#\\") || imageLookupHasUnsafeCharacters(input) {
		return "", &imageLookupInputError{message: "registry host is invalid"}
	}
	if input == "index.docker.io" || input == "registry-1.docker.io" {
		input = "docker.io"
	}
	if strings.HasSuffix(input, ":") {
		return "", &imageLookupInputError{message: "registry host has an empty port"}
	}
	if _, err := name.NewRegistry(imageLookupRegistryForLibrary(input), name.StrictValidation); err != nil {
		return "", &imageLookupInputError{message: "registry host is invalid: " + err.Error()}
	}
	return input, nil
}

func imageLookupRegistryForLibrary(registry string) string {
	if registry == "docker.io" {
		return name.DefaultRegistry
	}
	return registry
}

func imageLookupRegistryForLibraryReference(reference string) string {
	if strings.HasPrefix(reference, "docker.io/") {
		return name.DefaultRegistry + strings.TrimPrefix(reference, "docker.io")
	}
	return reference
}

func imageLookupRegistryForDisplay(registry string) string {
	registry = strings.ToLower(registry)
	if registry == name.DefaultRegistry || registry == "registry-1.docker.io" {
		return "docker.io"
	}
	return registry
}

func imageLookupRegistryLabel(registry string) string {
	switch registry {
	case "docker.io":
		return "Docker Hub"
	case "stgregistry.suse.com":
		return "SUSE Staging"
	case "registry.rancher.com":
		return "Rancher Registry"
	case "registry.suse.com":
		return "SUSE Registry"
	default:
		return registry
	}
}

func imageLookupStripScheme(input string, allowHTTP bool) (string, error) {
	input = strings.TrimSpace(input)
	lower := strings.ToLower(input)
	for _, prefix := range []string{"docker://", "oci://", "https://"} {
		if strings.HasPrefix(lower, prefix) {
			return input[len(prefix):], nil
		}
	}
	if strings.HasPrefix(lower, "http://") {
		if !allowHTTP {
			return "", &imageLookupInputError{message: "plain HTTP registry references are not allowed"}
		}
		return input[len("http://"):], nil
	}
	if strings.Contains(lower, "://") {
		return "", &imageLookupInputError{message: "image reference scheme is not supported"}
	}
	return input, nil
}

func imageLookupFirstComponentIsRegistry(component string) bool {
	return strings.Contains(component, ".") || strings.Contains(component, ":") || strings.EqualFold(component, "localhost")
}

func imageLookupHasUnsafeCharacters(value string) bool {
	for _, character := range value {
		if unicode.IsSpace(character) || unicode.IsControl(character) {
			return true
		}
	}
	return false
}

func imageLookupLooksLikeReference(value string) bool {
	if !strings.Contains(value, "/") {
		return false
	}
	lastSlash := strings.LastIndex(value, "/")
	return strings.LastIndex(value, "@") > lastSlash || strings.LastIndex(value, ":") > lastSlash
}

func imageLookupSimpleToken(value string) bool {
	for _, character := range value {
		if !(character >= 'a' && character <= 'z') && !(character >= '0' && character <= '9') && character != '_' && character != '-' && character != '.' {
			return false
		}
	}
	return value != ""
}

func imageLookupChoice(input, fallback string, allowed ...string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(input))
	if value == "" {
		value = fallback
	}
	for _, candidate := range allowed {
		if value == candidate {
			return value, nil
		}
	}
	return "", &imageLookupInputError{message: fmt.Sprintf("unsupported filter value %q; expected one of %s", input, strings.Join(allowed, ", "))}
}

func imageLookupNormalizeVersionLine(input string) (string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return "", nil
	}
	match := imageLookupVersionLinePattern.FindStringSubmatch(value)
	if len(match) != 4 {
		return "", &imageLookupInputError{message: "versionLine must use X.Y or X.Y.Z format"}
	}
	result := match[1] + "." + match[2]
	if match[3] != "" {
		result += "." + match[3]
	}
	return result, nil
}

func imageLookupCommitPrefix(value string) bool {
	if len(value) < 7 || len(value) > 40 {
		return false
	}
	for _, character := range value {
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f') || (character >= 'A' && character <= 'F')) {
			return false
		}
	}
	return true
}

func imageLookupEnrichedSearchRequested(request imageLookupSearchRequest) bool {
	nonDefault := func(value, defaultValue string) bool {
		value = strings.ToLower(strings.TrimSpace(value))
		return value != "" && value != defaultValue
	}
	return nonDefault(request.Channel, "all") ||
		nonDefault(request.Architecture, "all") ||
		nonDefault(request.PrimeHead, "all") ||
		nonDefault(request.HeadKind, "all") ||
		strings.TrimSpace(request.VersionLine) != "" ||
		strings.TrimSpace(request.Commit) != "" ||
		nonDefault(request.PairStatus, "all") ||
		nonDefault(request.SortBy, "natural") ||
		nonDefault(request.SortOrder, "desc") ||
		request.RecentDays > 0
}

func imageLookupRepositoryRole(repository string) (string, string) {
	repository = strings.Trim(strings.ToLower(strings.TrimSpace(repository)), "/")
	switch {
	case strings.HasSuffix(repository, "/rancher-agent"):
		return "agent", strings.TrimSuffix(repository, "/rancher-agent") + "/rancher"
	case strings.HasSuffix(repository, "/rancher-webhook"):
		return "webhook", ""
	case strings.HasSuffix(repository, "/rancher"):
		return "server", strings.TrimSuffix(repository, "/rancher") + "/rancher-agent"
	default:
		return "other", ""
	}
}

func imageLookupClassifyDigest(registry, repository, digest string) imageLookupTag {
	imageRole, _ := imageLookupRepositoryRole(repository)
	digest = strings.ToLower(strings.TrimSpace(digest))
	return imageLookupTag{
		Name:         digest,
		Reference:    registry + "/" + repository + "@" + digest,
		Channel:      "digest",
		Architecture: "unknown",
		ImageRole:    imageRole,
		Digest:       digest,
	}
}

func imageLookupClassifyTag(registry, repository, tagName string) imageLookupTag {
	architecture, baseTag := imageLookupTagArchitecture(tagName)
	imageRole, companionRepository := imageLookupRepositoryRole(repository)
	result := imageLookupTag{
		Name:         tagName,
		Reference:    registry + "/" + repository + ":" + tagName,
		Channel:      imageLookupTagChannel(tagName),
		Architecture: architecture,
		BaseTag:      baseTag,
		ImageRole:    imageRole,
		Artifact:     imageLookupArtifactTag(tagName),
	}
	if companionRepository != "" {
		result.CompanionReference = registry + "/" + companionRepository + ":" + tagName
	}
	if imageRole != "server" && imageRole != "agent" {
		return result
	}

	normalized := strings.ToLower(normalizeVersionInput(baseTag))
	if match := imageLookupTagVersionPattern.FindStringSubmatch(normalized); len(match) == 2 {
		result.Version = match[1]
		parts := strings.Split(result.Version, ".")
		if len(parts) >= 2 {
			result.VersionLine = parts[0] + "." + parts[1]
		}
	}
	switch {
	case isPatchHeadAliasRancherVersion(normalized):
		result.IsPrimeHead = true
		result.HeadKind = "moving"
		result.Mutable = true
		result.Version = strings.TrimSuffix(normalized, "-head")
	case isPrimeCommitHeadRancherVersion(normalized):
		result.IsPrimeHead = true
		result.HeadKind = "immutable"
		withoutHead := strings.TrimSuffix(normalized, "-head")
		separator := strings.LastIndex(withoutHead, "-")
		if separator > 0 {
			result.Version = withoutHead[:separator]
			result.Commit = withoutHead[separator+1:]
		}
	}
	if !result.IsPrimeHead {
		return result
	}
	result.Selector = "v" + result.Version + "-head"
	result.PairStatus = "unverified"
	return result
}

func imageLookupPrimeHeadAliasQuery(query string) bool {
	switch strings.ToLower(strings.TrimSpace(query)) {
	case "prime-head", "primehead", "prime_head":
		return true
	default:
		return false
	}
}

func imageLookupPrimeHeadQuery(query string) (string, string) {
	_, baseTag := imageLookupTagArchitecture(strings.TrimSpace(query))
	normalized := strings.ToLower(normalizeVersionInput(baseTag))
	switch {
	case isPatchHeadAliasRancherVersion(normalized):
		return strings.TrimSuffix(normalized, "-head"), "moving"
	case isPrimeCommitHeadRancherVersion(normalized):
		withoutHead := strings.TrimSuffix(normalized, "-head")
		separator := strings.LastIndex(withoutHead, "-")
		if separator > 0 {
			return withoutHead[:separator], "immutable"
		}
	}
	return "", ""
}

func imageLookupPatchHeadSelector(query string) bool {
	_, kind := imageLookupPrimeHeadQuery(query)
	return kind == "moving"
}

func imageLookupBarePatchVersion(query string) bool {
	match := imageLookupVersionLinePattern.FindStringSubmatch(strings.TrimSpace(query))
	return len(match) == 4 && match[3] != ""
}

func imageLookupPatchVersion(version string) bool {
	match := imageLookupVersionLinePattern.FindStringSubmatch(strings.TrimSpace(version))
	return len(match) == 4 && match[3] != ""
}

func imageLookupTagMatchesOptions(tag imageLookupTag, options imageLookupSearchOptions) bool {
	query := strings.ToLower(strings.TrimSpace(options.query))
	if imageLookupPrimeHeadAliasQuery(query) {
		if !tag.IsPrimeHead {
			return false
		}
	} else if version, kind := imageLookupPrimeHeadQuery(query); kind == "moving" {
		if !tag.IsPrimeHead || tag.Version != version {
			return false
		}
	} else if !imageLookupTagMatches(tag.Name, tag.Channel, query) {
		return false
	}
	if options.channel != "" && options.channel != "all" && !imageLookupTagMatches(tag.Name, tag.Channel, options.channel) {
		return false
	}
	if options.architecture != "" && options.architecture != "all" && tag.Architecture != options.architecture {
		return false
	}
	switch options.primeHead {
	case "only":
		if !tag.IsPrimeHead {
			return false
		}
	case "exclude":
		if tag.IsPrimeHead {
			return false
		}
	}
	if options.headKind != "" && options.headKind != "all" && tag.HeadKind != options.headKind {
		return false
	}
	if options.versionLine != "" {
		if imageLookupPatchVersion(options.versionLine) {
			if tag.Version != options.versionLine {
				return false
			}
		} else if tag.VersionLine != options.versionLine {
			return false
		}
	}
	if options.commit != "" && !strings.HasPrefix(strings.ToLower(tag.Commit), options.commit) {
		return false
	}
	return true
}

// imageLookupFilterRecentTags applies an evidence-based cutoff. Unknown
// timestamps are excluded rather than guessed: OCI tag-list responses do not
// contain upload dates and do not guarantee chronological ordering.
func imageLookupFilterRecentTags(tags []imageLookupTag, cutoff time.Time) ([]imageLookupTag, int, int) {
	if cutoff.IsZero() {
		return tags, 0, 0
	}
	filtered := tags[:0]
	excluded, unknown := 0, 0
	for _, tag := range tags {
		observedAt, ok := imageLookupTagObservedAt(tag)
		switch {
		case !ok:
			unknown++
		case observedAt.Before(cutoff):
			excluded++
		default:
			filtered = append(filtered, tag)
		}
	}
	return filtered, excluded, unknown
}

func imageLookupTagObservedAt(tag imageLookupTag) (time.Time, bool) {
	for _, value := range []string{tag.PairCompletedAt, tag.UploadedAt, tag.CreatedAt} {
		if observedAt, err := time.Parse(time.RFC3339Nano, value); err == nil && !observedAt.IsZero() {
			return observedAt.UTC(), true
		}
	}
	return time.Time{}, false
}

func imageLookupApplyRecentFilter(group *imageLookupSearchGroup, options imageLookupSearchOptions) {
	if options.recentCutoff.IsZero() {
		return
	}
	group.Tags, group.RecentExcludedCount, group.UnknownTimestampCount = imageLookupFilterRecentTags(group.Tags, options.recentCutoff)
	group.Matched = len(group.Tags)
}

func imageLookupSortTags(tags []imageLookupTag, sortBy, sortOrder string) {
	descending := sortOrder != "asc"
	sort.SliceStable(tags, func(i, j int) bool {
		comparison := imageLookupCompareTags(tags[i], tags[j], sortBy, descending)
		return comparison < 0
	})
}

func imageLookupCompareTags(left, right imageLookupTag, sortBy string, descending bool) int {
	compareNatural := func(leftValue, rightValue string) int {
		comparison := imageLookupNaturalCompare(leftValue, rightValue)
		if descending {
			comparison = -comparison
		}
		return comparison
	}
	compareOptionalTime := func(leftValue, rightValue string) int {
		leftTime, leftErr := time.Parse(time.RFC3339Nano, leftValue)
		rightTime, rightErr := time.Parse(time.RFC3339Nano, rightValue)
		leftKnown, rightKnown := leftErr == nil, rightErr == nil
		if leftKnown != rightKnown {
			if leftKnown {
				return -1
			}
			return 1
		}
		if leftKnown && !leftTime.Equal(rightTime) {
			if leftTime.Before(rightTime) != descending {
				return -1
			}
			return 1
		}
		return 0
	}

	var comparison int
	switch sortBy {
	case "tag":
		leftName, rightName := strings.ToLower(left.Name), strings.ToLower(right.Name)
		if leftName < rightName {
			comparison = -1
		} else if leftName > rightName {
			comparison = 1
		}
		if descending {
			comparison = -comparison
		}
	case "version":
		if left.Version == "" || right.Version == "" {
			if left.Version != right.Version {
				if left.Version != "" {
					return -1
				}
				return 1
			}
		} else {
			comparison = compareNatural(left.Version, right.Version)
		}
	case "uploaded":
		comparison = compareOptionalTime(left.UploadedAt, right.UploadedAt)
	case "pair-completed":
		comparison = compareOptionalTime(left.PairCompletedAt, right.PairCompletedAt)
	default:
		comparison = compareNatural(left.Name, right.Name)
	}
	if comparison != 0 {
		return comparison
	}
	return compareNatural(left.Name, right.Name)
}

func imageLookupCountPrimeHeads(group *imageLookupSearchGroup, tags []imageLookupTag) {
	group.PrimeHeadCount = 0
	group.MovingPrimeHeadCount = 0
	group.ImmutablePrimeHeadCount = 0
	for _, tag := range tags {
		if !tag.IsPrimeHead {
			continue
		}
		group.PrimeHeadCount++
		switch tag.HeadKind {
		case "moving":
			group.MovingPrimeHeadCount++
		case "immutable":
			group.ImmutablePrimeHeadCount++
		}
	}
}

func imageLookupCountPrimePairStatuses(group *imageLookupSearchGroup, tags []imageLookupTag) {
	group.VerifiedPrimeHeadCount = 0
	group.InvalidPrimeHeadCount = 0
	group.MissingCompanionCount = 0
	for _, tag := range tags {
		switch tag.PairStatus {
		case "verified":
			group.VerifiedPrimeHeadCount++
		case "invalid":
			group.InvalidPrimeHeadCount++
		case "missing":
			group.MissingCompanionCount++
		}
	}
}

func imageLookupInspectPrimeHead(parsed imageLookupReference, labels map[string]string) imageLookupPrimeHead {
	requested := imageLookupClassifyTag(parsed.registry, parsed.repository, parsed.tag)
	result := imageLookupPrimeHead{
		IsPrimeHead:        requested.IsPrimeHead,
		HeadKind:           requested.HeadKind,
		Mutable:            requested.Mutable,
		Version:            requested.Version,
		VersionLine:        requested.VersionLine,
		Commit:             requested.Commit,
		Selector:           requested.Selector,
		ImageRole:          requested.ImageRole,
		CompanionReference: requested.CompanionReference,
		Source:             safeOCIProvenanceLabel(labels[imageLookupSourceLabel]),
		Revision:           strings.ToLower(safeOCIProvenanceLabel(labels[imageLookupRevisionLabel])),
		OSSRevision:        strings.ToLower(safeOCIProvenanceLabel(labels[imageLookupOSSRevisionLabel])),
		CanonicalReference: safeOCIProvenanceLabel(labels[imageLookupCanonicalReferenceLabel]),
		Issues:             []string{},
	}
	result.PrimeSource = imageLookupRancherPrimeSource(result.Source)
	canonicalRepository, canonicalTag, canonicalOK := imageLookupCanonicalTag(result.CanonicalReference)
	result.CanonicalRepository = canonicalRepository
	result.CanonicalTag = canonicalTag
	canonical := imageLookupClassifyTag(parsed.registry, canonicalRepository, canonicalTag)

	minorAlias := imageLookupMinorHeadPattern.MatchString(strings.TrimSpace(parsed.tag))
	if !result.IsPrimeHead && result.PrimeSource && canonicalOK && canonical.IsPrimeHead && canonical.HeadKind == "immutable" &&
		(minorAlias || parsed.tag == "") {
		if minorAlias {
			match := imageLookupMinorHeadPattern.FindStringSubmatch(strings.TrimSpace(parsed.tag))
			if len(match) == 3 && canonical.VersionLine == match[1]+"."+match[2] {
				result.IsPrimeHead = true
				result.HeadKind = "moving"
				result.Mutable = true
			}
		} else {
			result.IsPrimeHead = true
			result.HeadKind = "immutable"
		}
		if result.IsPrimeHead {
			result.Version = canonical.Version
			result.VersionLine = canonical.VersionLine
			result.Commit = canonical.Commit
			result.Selector = canonical.Selector
			companionRepository := ""
			_, companionRepository = imageLookupRepositoryRole(parsed.repository)
			if companionRepository != "" {
				result.CompanionReference = parsed.registry + "/" + companionRepository + ":" + canonicalTag
			}
		}
	}
	if !result.IsPrimeHead {
		return result
	}
	expectedRepository := ""
	switch result.ImageRole {
	case "server":
		expectedRepository = "rancher/rancher"
		if !result.PrimeSource {
			result.Issues = append(result.Issues, "org.opencontainers.image.source is not the canonical Rancher Prime repository")
		}
	case "agent":
		expectedRepository = "rancher/rancher-agent"
	default:
		result.Issues = append(result.Issues, "Prime head classification is supported only for Rancher server and agent images")
	}
	if !canonicalOK {
		result.Issues = append(result.Issues, "org.opensuse.reference does not contain a valid canonical tag")
	} else {
		if expectedRepository != "" && canonicalRepository != expectedRepository {
			result.Issues = append(result.Issues, "org.opensuse.reference names an unexpected canonical repository")
		}
		result.CanonicalMatchesRequest = canonical.IsPrimeHead && canonical.HeadKind == "immutable" && canonical.Version == result.Version
		if requested.HeadKind == "immutable" {
			result.CanonicalMatchesRequest = result.CanonicalMatchesRequest && strings.EqualFold(canonicalTag, requested.Name)
		}
		if !result.CanonicalMatchesRequest {
			result.Issues = append(result.Issues, "org.opensuse.reference does not identify the expected immutable Prime head tag")
		}
	}
	if result.ImageRole == "server" {
		if imageLookupGitRevisionPattern.MatchString(result.OSSRevision) && result.Commit != "" {
			result.CommitMatchesOSS = strings.HasPrefix(result.OSSRevision, strings.ToLower(result.Commit))
		}
		if !result.CommitMatchesOSS {
			result.Issues = append(result.Issues, "org.opencontainers.image.oss.revision does not match the tag commit")
		}
	}
	result.Consistent = len(result.Issues) == 0
	return result
}

func imageLookupCanonicalTag(reference string) (string, string, bool) {
	reference = strings.TrimSpace(reference)
	if reference == "" || imageLookupHasUnsafeCharacters(reference) {
		return "", "", false
	}
	parsed, err := name.ParseReference(imageLookupRegistryForLibraryReference(reference), name.WeakValidation)
	if err != nil {
		return "", "", false
	}
	tag, ok := parsed.(name.Tag)
	if !ok {
		return "", "", false
	}
	return tag.Context().RepositoryStr(), tag.TagStr(), true
}

func imageLookupRancherPrimeSource(source string) bool {
	source = strings.ToLower(strings.TrimSpace(source))
	source = strings.TrimSuffix(source, "/")
	source = strings.TrimSuffix(source, ".git")
	return source == "https://github.com/rancher/rancher-prime"
}

func imageLookupTagChannel(tag string) string {
	lower := strings.ToLower(tag)
	switch {
	case strings.Contains(lower, "head"):
		return "head"
	case strings.Contains(lower, "alpha"):
		return "alpha"
	case strings.Contains(lower, "devel") || strings.Contains(lower, "dev") || strings.Contains(lower, "beta") || strings.Contains(lower, "master") || strings.Contains(lower, "main"):
		return "devel"
	case strings.Contains(lower, "-rcs-") || strings.Contains(lower, ".rcs."):
		return "rcs"
	case strings.Contains(lower, "-rc") || strings.Contains(lower, ".rc"):
		return "rc"
	default:
		return "stable"
	}
}

func imageLookupTagMatches(tag, channel, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	if imageLookupQuickQuery(query) {
		if query == "devel" {
			return channel == "devel" || channel == "alpha"
		}
		return channel == query
	}
	return strings.Contains(strings.ToLower(tag), query)
}

func imageLookupQuickQuery(query string) bool {
	switch strings.ToLower(strings.TrimSpace(query)) {
	case "head", "devel", "alpha", "rcs", "rc", "stable", "prime-head", "primehead", "prime_head":
		return true
	default:
		return false
	}
}

func imageLookupTagArchitecture(tag string) (string, string) {
	lower := strings.ToLower(tag)
	for _, architecture := range []string{"amd64", "arm64", "s390x", "ppc64le", "386", "arm"} {
		for _, suffix := range []string{"-linux-" + architecture, "-" + architecture, "_" + architecture, "." + architecture} {
			if strings.HasSuffix(lower, suffix) {
				return architecture, tag[:len(tag)-len(suffix)]
			}
		}
	}
	return "multi", tag
}

func imageLookupArtifactTag(tag string) bool {
	lower := strings.ToLower(tag)
	if strings.HasSuffix(lower, ".sig") || strings.HasSuffix(lower, ".att") || strings.HasSuffix(lower, ".sbom") || strings.HasSuffix(lower, ".attestation") {
		return true
	}
	if strings.HasPrefix(lower, "sha256-") && (strings.Contains(lower, ".sig") || strings.Contains(lower, ".att") || strings.Contains(lower, ".sbom")) {
		return true
	}
	return strings.Contains(lower, "cosign") && (strings.Contains(lower, "signature") || strings.Contains(lower, "attestation"))
}

func imageLookupNaturalCompare(left, right string) int {
	left = strings.ToLower(left)
	right = strings.ToLower(right)
	for len(left) > 0 && len(right) > 0 {
		leftDigit := left[0] >= '0' && left[0] <= '9'
		rightDigit := right[0] >= '0' && right[0] <= '9'
		leftEnd := imageLookupTokenEnd(left, leftDigit)
		rightEnd := imageLookupTokenEnd(right, rightDigit)
		leftToken, rightToken := left[:leftEnd], right[:rightEnd]
		if leftDigit && rightDigit {
			leftNumber := strings.TrimLeft(leftToken, "0")
			rightNumber := strings.TrimLeft(rightToken, "0")
			if leftNumber == "" {
				leftNumber = "0"
			}
			if rightNumber == "" {
				rightNumber = "0"
			}
			if len(leftNumber) != len(rightNumber) {
				if len(leftNumber) > len(rightNumber) {
					return 1
				}
				return -1
			}
			if leftNumber != rightNumber {
				if leftNumber > rightNumber {
					return 1
				}
				return -1
			}
			if len(leftToken) != len(rightToken) {
				if len(leftToken) < len(rightToken) {
					return 1
				}
				return -1
			}
		} else if leftToken != rightToken {
			if leftToken > rightToken {
				return 1
			}
			return -1
		}
		left, right = left[leftEnd:], right[rightEnd:]
	}
	if len(left) == len(right) {
		return 0
	}
	if len(left) > 0 {
		return 1
	}
	return -1
}

func imageLookupTokenEnd(value string, digits bool) int {
	index := 0
	for index < len(value) {
		isDigit := value[index] >= '0' && value[index] <= '9'
		if isDigit != digits {
			break
		}
		index++
	}
	return index
}

func imageLookupCleanArchivePath(value string) (string, bool) {
	value = strings.TrimPrefix(strings.ReplaceAll(value, "\\", "/"), "./")
	if value == "" || strings.HasPrefix(value, "/") {
		return "", false
	}
	cleaned := path.Clean(value)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false
	}
	return cleaned, true
}

func imageLookupArchivePathHidden(value string, hiddenPaths, hiddenDirectories map[string]struct{}) bool {
	if _, hidden := hiddenPaths[value]; hidden {
		return true
	}
	for directory := path.Dir(value); directory != "." && directory != "/"; directory = path.Dir(directory) {
		if _, hidden := hiddenDirectories[directory]; hidden {
			return true
		}
	}
	return false
}

func imageLookupBoundedLabels(labels map[string]string) map[string]string {
	if len(labels) == 0 {
		return map[string]string{}
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) > 256 {
		keys = keys[:256]
	}
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		value := labels[key]
		if len(value) > 8192 {
			value = value[:8192]
		}
		result[key] = value
	}
	return result
}

func imageLookupBoundedStrings(values []string, maxItems, maxLength int) []string {
	if len(values) > maxItems {
		values = values[:maxItems]
	}
	result := make([]string, len(values))
	for index, value := range values {
		if len(value) > maxLength {
			value = value[:maxLength]
		}
		result[index] = value
	}
	return result
}

func imageLookupBoundedHistory(history []v1.History) []imageLookupHistoryEntry {
	if len(history) == 0 {
		return []imageLookupHistoryEntry{}
	}
	if len(history) > imageLookupMaxHistoryEntries {
		history = history[len(history)-imageLookupMaxHistoryEntries:]
	}
	result := make([]imageLookupHistoryEntry, 0, len(history))
	for _, entry := range history {
		createdBy := entry.CreatedBy
		if len(createdBy) > imageLookupMaxHistoryText {
			createdBy = createdBy[:imageLookupMaxHistoryText]
		}
		comment := entry.Comment
		if len(comment) > imageLookupMaxHistoryText {
			comment = comment[:imageLookupMaxHistoryText]
		}
		result = append(result, imageLookupHistoryEntry{
			Created:    imageLookupFormatTime(entry.Created.Time),
			CreatedBy:  createdBy,
			Comment:    comment,
			EmptyLayer: entry.EmptyLayer,
		})
	}
	return result
}

var errImageLookupCommandOutputLimit = errors.New("command output limit exceeded")

type imageLookupLimitedBuffer struct {
	buffer   bytes.Buffer
	limit    int64
	exceeded bool
}

func (b *imageLookupLimitedBuffer) Write(value []byte) (int, error) {
	remaining := b.limit - int64(b.buffer.Len())
	if remaining <= 0 {
		b.exceeded = true
		return 0, errImageLookupCommandOutputLimit
	}
	if int64(len(value)) > remaining {
		written, _ := b.buffer.Write(value[:remaining])
		b.exceeded = true
		return written, errImageLookupCommandOutputLimit
	}
	return b.buffer.Write(value)
}

func imageLookupExecCommand(ctx context.Context, executable string, arguments, environment []string, outputLimit int64) ([]byte, error) {
	command := exec.CommandContext(ctx, executable, arguments...)
	command.Env = environment
	stdout := &imageLookupLimitedBuffer{limit: outputLimit}
	command.Stdout = stdout
	command.Stderr = io.Discard
	err := command.Run()
	if stdout.exceeded {
		return nil, errImageLookupCommandOutputLimit
	}
	if err != nil {
		// Preserve bounded stdout for callers that deliberately request structured
		// status information (for example, `gh api --include`). Callers must still
		// treat the accompanying error as authoritative and must not expose the raw
		// command output without validating it first.
		return stdout.buffer.Bytes(), err
	}
	return stdout.buffer.Bytes(), nil
}

func imageLookupSanitizedGHEnvironment() []string {
	blocked := map[string]struct{}{
		"GH_PROMPT_DISABLED":  {},
		"GIT_TERMINAL_PROMPT": {},
		"GH_PAGER":            {},
		"GH_DEBUG":            {},
	}
	environment := make([]string, 0, len(os.Environ())+3)
	for _, entry := range os.Environ() {
		key := entry
		if separator := strings.IndexByte(entry, '='); separator >= 0 {
			key = entry[:separator]
		}
		if _, skip := blocked[strings.ToUpper(key)]; skip {
			continue
		}
		environment = append(environment, entry)
	}
	return append(environment,
		"GH_PROMPT_DISABLED=1",
		"GIT_TERMINAL_PROMPT=0",
		"GH_PAGER=cat",
	)
}

func imageLookupFormatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func imageLookupSafeError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "registry request timed out"
	}
	var registryErr *transport.Error
	if errors.As(err, &registryErr) {
		return fmt.Sprintf("registry returned %d %s", registryErr.StatusCode, http.StatusText(registryErr.StatusCode))
	}
	message := err.Error()
	if len(message) > 240 {
		message = message[:240]
	}
	return message
}

func imageLookupRegistryNotFound(err error) bool {
	var registryErr *transport.Error
	return errors.As(err, &registryErr) && registryErr.StatusCode == http.StatusNotFound
}

func imageLookupHTTPStatus(err error) int {
	var inputErr *imageLookupInputError
	if errors.As(err, &inputErr) {
		return http.StatusBadRequest
	}
	var conflictErr *imageLookupConflictError
	if errors.As(err, &conflictErr) {
		return http.StatusConflict
	}
	var metadataErr *imageLookupSourceMetadataError
	if errors.As(err, &metadataErr) {
		return http.StatusUnprocessableEntity
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return http.StatusGatewayTimeout
	}
	var registryErr *transport.Error
	if errors.As(err, &registryErr) {
		switch registryErr.StatusCode {
		case http.StatusNotFound:
			return http.StatusNotFound
		case http.StatusTooManyRequests:
			return http.StatusTooManyRequests
		case http.StatusUnauthorized, http.StatusForbidden:
			return http.StatusBadGateway
		}
	}
	return http.StatusBadGateway
}

func decodeImageLookupJSON(w http.ResponseWriter, request *http.Request, destination any) error {
	request.Body = http.MaxBytesReader(w, request.Body, imageLookupRequestLimit)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return &imageLookupInputError{message: "invalid JSON request: " + err.Error()}
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return &imageLookupInputError{message: "request body must contain exactly one JSON object"}
	}
	return nil
}

func (p *localControlPanel) imageLookupBackend() *imageLookupService {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.imageLookup == nil {
		p.imageLookup = newImageLookupService()
	}
	return p.imageLookup
}

func (p *localControlPanel) handleImageLookupSearch(w http.ResponseWriter, request *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !p.authorizedLocalAction(request) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if request.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload imageLookupSearchRequest
	if err := decodeImageLookupJSON(w, request, &payload); err != nil {
		http.Error(w, imageLookupSafeError(err), imageLookupHTTPStatus(err))
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), imageLookupSearchTimeout)
	defer cancel()
	response, err := p.imageLookupBackend().Search(ctx, payload)
	if err != nil {
		http.Error(w, imageLookupSafeError(err), imageLookupHTTPStatus(err))
		return
	}
	writeJSON(w, response)
}

func (p *localControlPanel) handleImageLookupInspect(w http.ResponseWriter, request *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !p.authorizedLocalAction(request) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if request.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload imageLookupInspectRequest
	if err := decodeImageLookupJSON(w, request, &payload); err != nil {
		http.Error(w, imageLookupSafeError(err), imageLookupHTTPStatus(err))
		return
	}
	timeout := imageLookupSearchTimeout
	if payload.IncludeBuildYAML {
		timeout = imageLookupInspectTimeout
	}
	ctx, cancel := context.WithTimeout(request.Context(), timeout)
	defer cancel()
	response, err := p.imageLookupBackend().Inspect(ctx, payload)
	if err != nil {
		http.Error(w, imageLookupSafeError(err), imageLookupHTTPStatus(err))
		return
	}
	writeJSON(w, response)
}

func (p *localControlPanel) handleImageLookupSourceBuildYAML(w http.ResponseWriter, request *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !p.authorizedLocalAction(request) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if request.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload imageLookupSourceBuildYAMLRequest
	if err := decodeImageLookupJSON(w, request, &payload); err != nil {
		http.Error(w, imageLookupSafeError(err), imageLookupHTTPStatus(err))
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), imageLookupSourceTimeout)
	defer cancel()
	response, err := p.imageLookupBackend().FetchSourceBuildYAML(ctx, payload)
	if err != nil {
		http.Error(w, imageLookupSafeError(err), imageLookupHTTPStatus(err))
		return
	}
	writeJSON(w, response)
}
