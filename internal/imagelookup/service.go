package imagelookup

import (
	"context"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"net/http"
	"regexp"
	"time"
)

const (
	RequestLimit                  = 64 << 10
	imageLookupDefaultResultLimit = 50
	imageLookupMaxResultLimit     = 200
	imageLookupMaxRecentDays      = 3650
	MaxTagScan                    = 10000
	imageLookupTagPageSize        = 1000
	imageLookupMaxBuildYAML       = 1 << 20
	imageLookupMaxBuildYAMLLayer  = 16 << 20
	imageLookupMaxLayerScan       = 256 << 20
	imageLookupMaxHistoryEntries  = 256
	imageLookupMaxHistoryText     = 8192
	SearchTimeout                 = 30 * time.Second
	InspectTimeout                = 90 * time.Second
	SourceTimeout                 = 45 * time.Second
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

var GitRevisionPattern = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

var imageLookupDigestPattern = regexp.MustCompile(`^sha256:[0-9a-fA-F]{64}$`)

const (
	SourceLabel      = "org.opencontainers.image.source"
	RevisionLabel    = "org.opencontainers.image.revision"
	OSSRevisionLabel = "org.opencontainers.image.oss.revision"
)

type SearchRequest struct {
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

type SearchResponse struct {
	Query        string        `json:"query"`
	RecentDays   int           `json:"recentDays"`
	RecentCutoff string        `json:"recentCutoff,omitempty"`
	ScanMode     string        `json:"scanMode"`
	Channel      string        `json:"channel"`
	Architecture string        `json:"architecture"`
	PrimeHead    string        `json:"primeHead"`
	HeadKind     string        `json:"headKind"`
	VersionLine  string        `json:"versionLine"`
	Commit       string        `json:"commit"`
	PairStatus   string        `json:"pairStatus"`
	SortBy       string        `json:"sortBy"`
	SortOrder    string        `json:"sortOrder"`
	SearchedAt   time.Time     `json:"searchedAt"`
	Groups       []SearchGroup `json:"groups"`
}

type SearchGroup struct {
	Key                     string `json:"key"`
	Label                   string `json:"label"`
	Registry                string `json:"registry"`
	Repository              string `json:"repository"`
	Reference               string `json:"reference"`
	ImageRole               string `json:"imageRole"`
	CompanionRepository     string `json:"companionRepository,omitempty"`
	Tags                    []Tag  `json:"tags"`
	Matched                 int    `json:"matched"`
	Scanned                 int    `json:"scanned"`
	PrimeHeadCount          int    `json:"primeHeadCount"`
	MovingPrimeHeadCount    int    `json:"movingPrimeHeadCount"`
	ImmutablePrimeHeadCount int    `json:"immutablePrimeHeadCount"`
	VerifiedPrimeHeadCount  int    `json:"verifiedPrimeHeadCount"`
	InvalidPrimeHeadCount   int    `json:"invalidPrimeHeadCount"`
	MissingCompanionCount   int    `json:"missingCompanionCount"`
	RecentExcludedCount     int    `json:"recentExcludedCount"`
	UnknownTimestampCount   int    `json:"unknownTimestampCount"`
	Truncated               bool   `json:"truncated"`
	Error                   string `json:"error,omitempty"`
}

type Tag struct {
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

type InspectRequest struct {
	Reference        string `json:"reference"`
	Platform         string `json:"platform"`
	IncludeBuildYAML bool   `json:"includeBuildYaml"`
	SkipTagMetadata  bool   `json:"-"`
}

type SourceBuildYAMLRequest struct {
	Reference      string `json:"reference"`
	Platform       string `json:"platform"`
	ExpectedDigest string `json:"expectedDigest"`
}

type SourceBuildYAMLResponse struct {
	Found      bool                      `json:"found"`
	Path       string                    `json:"path"`
	Origin     string                    `json:"origin"`
	Provenance SourceBuildYAMLProvenance `json:"provenance"`
	Raw        string                    `json:"raw"`
	Data       map[string]any            `json:"data"`
}

type SourceBuildYAMLProvenance struct {
	RepositoryURL  string `json:"repositoryUrl"`
	Revision       string `json:"revision"`
	Path           string `json:"path"`
	ImageReference string `json:"imageReference"`
	ImageDigest    string `json:"imageDigest"`
	Platform       string `json:"platform"`
	SourceLabel    string `json:"sourceLabel"`
	RevisionLabel  string `json:"revisionLabel"`
}

type InspectResponse struct {
	Reference  string      `json:"reference"`
	Registry   string      `json:"registry"`
	Repository string      `json:"repository"`
	Tag        string      `json:"tag,omitempty"`
	Digest     string      `json:"digest"`
	MediaType  string      `json:"mediaType"`
	CreatedAt  string      `json:"createdAt,omitempty"`
	UploadedAt string      `json:"uploadedAt,omitempty"`
	Platform   string      `json:"platform,omitempty"`
	Platforms  []Platform  `json:"platforms"`
	Size       int64       `json:"size"`
	Config     ImageConfig `json:"config"`
	Layers     []Layer     `json:"layers"`
	BuildYAML  BuildYAML   `json:"buildYaml"`
	PrimeHead  PrimeHead   `json:"primeHead"`
	Warnings   []string    `json:"warnings"`
}

// imageLookupPrimeHead combines tag-derived classification with the OCI
// provenance labels available only after inspection. CompanionVerified is
// deliberately false: Image Lookup exposes the expected same-tag pair but
// does not claim that the companion manifest exists without probing it.
type PrimeHead struct {
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

type Platform struct {
	OS           string `json:"os,omitempty"`
	Architecture string `json:"architecture,omitempty"`
	Variant      string `json:"variant,omitempty"`
	Digest       string `json:"digest,omitempty"`
	MediaType    string `json:"mediaType,omitempty"`
	Size         int64  `json:"size,omitempty"`
}

type ImageConfig struct {
	Digest       string            `json:"digest"`
	Size         int64             `json:"size"`
	Architecture string            `json:"architecture,omitempty"`
	OS           string            `json:"os,omitempty"`
	Variant      string            `json:"variant,omitempty"`
	CreatedAt    string            `json:"createdAt,omitempty"`
	Labels       map[string]string `json:"labels"`
	Env          []string          `json:"env"`
	Entrypoint   []string          `json:"entrypoint"`
	Cmd          []string          `json:"cmd"`
	History      []HistoryEntry    `json:"history"`
}

type HistoryEntry struct {
	Created    string `json:"created,omitempty"`
	CreatedBy  string `json:"createdBy,omitempty"`
	Comment    string `json:"comment,omitempty"`
	EmptyLayer bool   `json:"emptyLayer"`
}

type Layer struct {
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
	MediaType string `json:"mediaType"`
}

type BuildYAML struct {
	Found   bool           `json:"found"`
	Path    string         `json:"path,omitempty"`
	Raw     string         `json:"raw,omitempty"`
	Data    map[string]any `json:"data,omitempty"`
	Error   string         `json:"error,omitempty"`
	Reason  string         `json:"reason,omitempty"`
	Skipped bool           `json:"skipped"`
}

type Service struct {
	transport     http.RoundTripper
	keychain      authn.Keychain
	allowHTTP     bool
	now           func() time.Time
	maxTagScan    int
	maxBuildYML   int64
	maxBuildLayer int64
	maxLayerScan  int64
	runCommand    CommandRunner
}

type CommandRunner func(context.Context, string, []string, []string, int64) ([]byte, error)

type searchTarget struct {
	registry   string
	repository string
}

type searchOptions struct {
	movingHeadsOnly  bool // Internal complete moving-head discovery across community and Prime tags.
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

type InputError struct {
	Message string
}

func (e *InputError) Error() string { return e.Message }

type imageLookupConflictError struct {
	message string
}

func (e *imageLookupConflictError) Error() string { return e.message }

type imageLookupSourceMetadataError struct {
	message string
}

func (e *imageLookupSourceMetadataError) Error() string { return e.message }

func New() *Service {
	return &Service{
		transport:     newImageLookupSafeTransport(),
		keychain:      imageLookupCredentialKeychain{},
		now:           time.Now,
		maxTagScan:    MaxTagScan,
		maxBuildYML:   imageLookupMaxBuildYAML,
		maxBuildLayer: imageLookupMaxBuildYAMLLayer,
		maxLayerScan:  imageLookupMaxLayerScan,
		runCommand:    ExecCommand,
	}
}

func (s *Service) defaults() {
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
		s.maxTagScan = MaxTagScan
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
		s.runCommand = ExecCommand
	}
}
