package prbuild

import (
	"context"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	"net/http"
	"regexp"
	"sync"
	"time"
)

const (
	VerifyTimeout             = 2 * time.Minute
	prBuildImageLookupTimeout = 30 * time.Second
	prBuildGitHubTimeout      = 15 * time.Second
	prBuildGitHubOutputLimit  = 64 << 10
	prBuildWorkerLimit        = 4
	prBuildPlatform           = "linux/amd64"
)

var prBuildHeadTagPattern = regexp.MustCompile(`(?i)^(?:head|v?([0-9]+\.[0-9]+)-head)$`)

type VerifyRequest struct {
	PullRequest string `json:"pullRequest"`
	Tag         string `json:"tag"`
}

type VerifyResponse struct {
	CheckedAt   time.Time        `json:"checkedAt"`
	Tag         string           `json:"tag"`
	Platform    string           `json:"platform"`
	PullRequest PullRequest      `json:"pullRequest"`
	Summary     Summary          `json:"summary"`
	Registries  []RegistryResult `json:"registries"`
	Warnings    []string         `json:"warnings"`
}

type PullRequest struct {
	URL                string `json:"url"`
	Repository         string `json:"repository"`
	Number             int    `json:"number"`
	Title              string `json:"title"`
	State              string `json:"state"`
	Draft              bool   `json:"draft"`
	Merged             bool   `json:"merged"`
	MergedAt           string `json:"mergedAt,omitempty"`
	BaseRef            string `json:"baseRef"`
	HeadRef            string `json:"headRef"`
	HeadRepository     string `json:"headRepository,omitempty"`
	HeadSHA            string `json:"headSha"`
	MergeCommitSHA     string `json:"mergeCommitSha,omitempty"`
	InclusionCommitSHA string `json:"inclusionCommitSha"`
	InclusionCommitURL string `json:"inclusionCommitUrl"`
	InclusionBasis     string `json:"inclusionBasis"`
}

type Summary struct {
	Verdict                     string `json:"verdict"`
	ScanComplete                bool   `json:"scanComplete"`
	RegistryCount               int    `json:"registryCount"`
	CompletePairRegistries      int    `json:"completePairRegistries"`
	ServerIncludedRegistries    int    `json:"serverIncludedRegistries"`
	ServerNotIncludedRegistries int    `json:"serverNotIncludedRegistries"`
	ServerUnknownRegistries     int    `json:"serverUnknownRegistries"`
	ServerMissingRegistries     int    `json:"serverMissingRegistries"`
	ServerErrorRegistries       int    `json:"serverErrorRegistries"`
}

type RegistryResult struct {
	Registry      string      `json:"registry"`
	Label         string      `json:"label"`
	Status        string      `json:"status"`
	PairAvailable bool        `json:"pairAvailable"`
	Server        ImageResult `json:"server"`
	Agent         ImageResult `json:"agent"`
}

type ImageResult struct {
	Reference      string      `json:"reference"`
	Found          bool        `json:"found"`
	Digest         string      `json:"digest,omitempty"`
	PlatformDigest string      `json:"platformDigest,omitempty"`
	Platform       string      `json:"platform,omitempty"`
	WebhookVersion string      `json:"webhookVersion,omitempty"`
	ChartBranch    string      `json:"chartBranch,omitempty"`
	BuildVersion   string      `json:"buildVersion,omitempty"`
	SourceURL      string      `json:"sourceUrl,omitempty"`
	Revision       string      `json:"revision,omitempty"`
	OSSRevision    string      `json:"ossRevision,omitempty"`
	Error          string      `json:"error,omitempty"`
	Match          CommitMatch `json:"match"`
}

type CommitMatch struct {
	Verdict           string `json:"verdict"`
	Pin               string `json:"pin,omitempty"`
	EvidenceURL       string `json:"evidenceUrl,omitempty"`
	ChartURL          string `json:"chartUrl,omitempty"`
	ComponentImage    string `json:"componentImage,omitempty"`
	ComponentDigest   string `json:"componentDigest,omitempty"`
	Relation          string `json:"relation,omitempty"`
	Reason            string `json:"reason"`
	CandidateRevision string `json:"candidateRevision,omitempty"`
	RequiredRevision  string `json:"requiredRevision,omitempty"`
	RevisionLabel     string `json:"revisionLabel,omitempty"`
	Basis             string `json:"basis,omitempty"`
	CompareURL        string `json:"compareUrl,omitempty"`
	CommitURL         string `json:"commitUrl,omitempty"`
	ComparisonError   bool   `json:"-"`
}

type Target struct {
	owner      string
	repository string
	number     int
	url        string
}

type GitHubPull struct {
	Body           string `json:"body"`
	Number         int    `json:"number"`
	HTMLURL        string `json:"html_url"`
	Title          string `json:"title"`
	State          string `json:"state"`
	Draft          bool   `json:"draft"`
	Merged         bool   `json:"merged"`
	MergedAt       string `json:"merged_at"`
	MergeCommitSHA string `json:"merge_commit_sha"`
	Head           struct {
		SHA  string `json:"sha"`
		Ref  string `json:"ref"`
		Repo struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"head"`
	Base struct {
		SHA  string `json:"sha"`
		Ref  string `json:"ref"`
		Repo struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"base"`
}

type GitHubCompare struct {
	Status       string `json:"status"`
	AheadBy      int    `json:"ahead_by"`
	BehindBy     int    `json:"behind_by"`
	HTMLURL      string `json:"html_url"`
	MergeBaseSHA string `json:"merge_base_sha"`
}

type GitHubHTTPError struct {
	Status    int
	Operation string
}

func (e *GitHubHTTPError) Error() string {
	switch e.Status {
	case http.StatusNotFound:
		return fmt.Sprintf("GitHub %s was not found or is not accessible with the configured GitHub CLI login", e.Operation)
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Sprintf("GitHub denied %s access; confirm GitHub CLI authentication, repository access, and API rate limits", e.Operation)
	case http.StatusTooManyRequests:
		return fmt.Sprintf("GitHub rate-limited the %s lookup; wait and try again", e.Operation)
	default:
		return fmt.Sprintf("GitHub %s lookup returned %d %s", e.Operation, e.Status, http.StatusText(e.Status))
	}
}

type ImageInspector func(context.Context, imagelookup.InspectRequest) (imagelookup.InspectResponse, error)

type PullFetcher func(context.Context, Target) (GitHubPull, error)

type CommitComparator func(context.Context, Target, string, string) (GitHubCompare, error)

type Service struct {
	defaultsOnce sync.Once
	imageLookup  *imagelookup.Service
	runCommand   imagelookup.CommandRunner
	now          func() time.Time
	inspect      ImageInspector
	fetchPull    PullFetcher
	compare      CommitComparator
}

func New(imageLookup *imagelookup.Service) *Service {
	if imageLookup == nil {
		imageLookup = imagelookup.New()
	}
	return &Service{imageLookup: imageLookup}
}

func (s *Service) defaults() {
	s.defaultsOnce.Do(func() {
		if s.imageLookup == nil {
			s.imageLookup = imagelookup.New()
		}
		runner := s.imageLookup.CommandRunner()
		if s.runCommand == nil {
			s.runCommand = runner
		}
		if s.now == nil {
			s.now = time.Now
		}
		if s.inspect == nil {
			s.inspect = s.imageLookup.Inspect
		}
		if s.fetchPull == nil {
			s.fetchPull = s.fetchPullFromGitHub
		}
		if s.compare == nil {
			s.compare = s.compareCommitsOnGitHub
		}
	})
}

func (s *Service) Verify(ctx context.Context, request VerifyRequest) (VerifyResponse, error) {
	s.defaults()
	target, err := parsePRBuildPullRequestURL(request.PullRequest)
	if err != nil {
		return VerifyResponse{}, err
	}
	tag, minorLine, err := normalizePRBuildHeadTag(request.Tag)
	if err != nil {
		return VerifyResponse{}, err
	}

	rawPull, err := s.fetchPull(ctx, target)
	if err != nil {
		return VerifyResponse{}, err
	}
	pull, err := normalizePRBuildPullRequest(target, rawPull)
	if err != nil {
		return VerifyResponse{}, err
	}

	registries, err := s.inspectKnownRegistries(ctx, tag)
	if err != nil {
		return VerifyResponse{}, err
	}
	if err := s.compareImageRevisions(ctx, target, pull, registries); err != nil {
		return VerifyResponse{}, err
	}
	for index := range registries {
		registries[index].PairAvailable = registries[index].Server.Found && registries[index].Agent.Found
		registries[index].Status = prBuildRegistryStatus(registries[index])
	}

	warnings := make([]string, 0, 3)
	if !pull.Merged {
		warnings = append(warnings, "This PR is not merged, so the check uses its current head commit rather than an integration commit on the base branch.")
	}
	if minorLine != "" && !prBuildBaseRefMatchesMinorLine(pull.BaseRef, minorLine) {
		warnings = append(warnings, fmt.Sprintf("The PR base branch %q does not contain release line %s from tag %s. A cherry-pick or backport with a different SHA cannot be proven by commit ancestry.", pull.BaseRef, minorLine, tag))
	}

	return VerifyResponse{
		CheckedAt:   s.now().UTC(),
		Tag:         tag,
		Platform:    prBuildPlatform,
		PullRequest: pull,
		Summary:     summarizePRBuildResults(registries),
		Registries:  registries,
		Warnings:    warnings,
	}, nil
}
