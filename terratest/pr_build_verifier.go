package test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/brudnak/ha-rancher-rke2/terratest/settings"
)

const (
	prBuildVerifyTimeout      = 2 * time.Minute
	prBuildImageLookupTimeout = 30 * time.Second
	prBuildGitHubTimeout      = 15 * time.Second
	prBuildGitHubOutputLimit  = 64 << 10
	prBuildWorkerLimit        = 4
	prBuildPlatform           = "linux/amd64"
)

var prBuildHeadTagPattern = regexp.MustCompile(`(?i)^(?:head|v?([0-9]+\.[0-9]+)-head)$`)

type prBuildVerifyRequest struct {
	PullRequest string `json:"pullRequest"`
	Tag         string `json:"tag"`
}

type prBuildVerifyResponse struct {
	CheckedAt   time.Time               `json:"checkedAt"`
	Tag         string                  `json:"tag"`
	Platform    string                  `json:"platform"`
	PullRequest prBuildPullRequest      `json:"pullRequest"`
	Summary     prBuildSummary          `json:"summary"`
	Registries  []prBuildRegistryResult `json:"registries"`
	Warnings    []string                `json:"warnings"`
}

type prBuildPullRequest struct {
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

type prBuildSummary struct {
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

type prBuildRegistryResult struct {
	Registry      string             `json:"registry"`
	Label         string             `json:"label"`
	Status        string             `json:"status"`
	PairAvailable bool               `json:"pairAvailable"`
	Server        prBuildImageResult `json:"server"`
	Agent         prBuildImageResult `json:"agent"`
}

type prBuildImageResult struct {
	Reference      string             `json:"reference"`
	Found          bool               `json:"found"`
	Digest         string             `json:"digest,omitempty"`
	PlatformDigest string             `json:"platformDigest,omitempty"`
	Platform       string             `json:"platform,omitempty"`
	BuildVersion   string             `json:"buildVersion,omitempty"`
	SourceURL      string             `json:"sourceUrl,omitempty"`
	Revision       string             `json:"revision,omitempty"`
	OSSRevision    string             `json:"ossRevision,omitempty"`
	Error          string             `json:"error,omitempty"`
	Match          prBuildCommitMatch `json:"match"`
}

type prBuildCommitMatch struct {
	Verdict           string `json:"verdict"`
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

type prBuildTarget struct {
	owner      string
	repository string
	number     int
	url        string
}

type prBuildGitHubPull struct {
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

type prBuildGitHubCompare struct {
	Status       string `json:"status"`
	AheadBy      int    `json:"ahead_by"`
	BehindBy     int    `json:"behind_by"`
	HTMLURL      string `json:"html_url"`
	MergeBaseSHA string `json:"merge_base_sha"`
}

type prBuildGitHubHTTPError struct {
	status    int
	operation string
}

func (e *prBuildGitHubHTTPError) Error() string {
	switch e.status {
	case http.StatusNotFound:
		return fmt.Sprintf("GitHub %s was not found or is not accessible with the configured GitHub CLI login", e.operation)
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Sprintf("GitHub denied %s access; confirm GitHub CLI authentication, repository access, and API rate limits", e.operation)
	case http.StatusTooManyRequests:
		return fmt.Sprintf("GitHub rate-limited the %s lookup; wait and try again", e.operation)
	default:
		return fmt.Sprintf("GitHub %s lookup returned %d %s", e.operation, e.status, http.StatusText(e.status))
	}
}

type prBuildImageInspector func(context.Context, imageLookupInspectRequest) (imageLookupInspectResponse, error)
type prBuildPullFetcher func(context.Context, prBuildTarget) (prBuildGitHubPull, error)
type prBuildCommitComparator func(context.Context, prBuildTarget, string, string) (prBuildGitHubCompare, error)

type prBuildVerifierService struct {
	defaultsOnce sync.Once
	imageLookup  *imageLookupService
	runCommand   imageLookupCommandRunner
	now          func() time.Time
	inspect      prBuildImageInspector
	fetchPull    prBuildPullFetcher
	compare      prBuildCommitComparator
}

func newPRBuildVerifierService(imageLookup *imageLookupService) *prBuildVerifierService {
	if imageLookup == nil {
		imageLookup = newImageLookupService()
	}
	return &prBuildVerifierService{imageLookup: imageLookup}
}

func (s *prBuildVerifierService) defaults() {
	s.defaultsOnce.Do(func() {
		if s.imageLookup == nil {
			s.imageLookup = newImageLookupService()
		}
		s.imageLookup.defaults()
		if s.runCommand == nil {
			s.runCommand = s.imageLookup.runCommand
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

func (s *prBuildVerifierService) Verify(ctx context.Context, request prBuildVerifyRequest) (prBuildVerifyResponse, error) {
	s.defaults()
	target, err := parsePRBuildPullRequestURL(request.PullRequest)
	if err != nil {
		return prBuildVerifyResponse{}, err
	}
	tag, minorLine, err := normalizePRBuildHeadTag(request.Tag)
	if err != nil {
		return prBuildVerifyResponse{}, err
	}

	rawPull, err := s.fetchPull(ctx, target)
	if err != nil {
		return prBuildVerifyResponse{}, err
	}
	pull, err := normalizePRBuildPullRequest(target, rawPull)
	if err != nil {
		return prBuildVerifyResponse{}, err
	}

	registries, err := s.inspectKnownRegistries(ctx, tag)
	if err != nil {
		return prBuildVerifyResponse{}, err
	}
	if err := s.compareImageRevisions(ctx, target, pull, registries); err != nil {
		return prBuildVerifyResponse{}, err
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

	return prBuildVerifyResponse{
		CheckedAt:   s.now().UTC(),
		Tag:         tag,
		Platform:    prBuildPlatform,
		PullRequest: pull,
		Summary:     summarizePRBuildResults(registries),
		Registries:  registries,
		Warnings:    warnings,
	}, nil
}

func parsePRBuildPullRequestURL(input string) (prBuildTarget, error) {
	hasControl := strings.IndexFunc(input, unicode.IsControl) >= 0
	input = strings.TrimSpace(input)
	invalid := func() (prBuildTarget, error) {
		return prBuildTarget{}, &imageLookupInputError{message: "pullRequest must be an exact https://github.com/{owner}/{repository}/pull/{number} URL"}
	}
	if input == "" || len(input) > 512 || hasControl || imageLookupHasUnsafeCharacters(input) {
		return invalid()
	}
	parsed, err := url.Parse(input)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" || parsed.Hostname() != "github.com" || parsed.Port() != "" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.RawPath != "" {
		return invalid()
	}
	pathValue := strings.TrimSuffix(parsed.Path, "/")
	parts := strings.Split(strings.TrimPrefix(pathValue, "/"), "/")
	if len(parts) != 4 || parts[2] != "pull" || !imageLookupGitHubPathComponent(parts[0]) || !imageLookupGitHubPathComponent(parts[1]) {
		return invalid()
	}
	number, numberErr := strconv.Atoi(parts[3])
	if numberErr != nil || number < 1 || number > 1_000_000_000 || strconv.Itoa(number) != parts[3] {
		return invalid()
	}
	canonical := fmt.Sprintf("https://github.com/%s/%s/pull/%d", parts[0], parts[1], number)
	if input != canonical && input != canonical+"/" {
		return invalid()
	}
	return prBuildTarget{owner: parts[0], repository: parts[1], number: number, url: canonical}, nil
}

func normalizePRBuildHeadTag(input string) (string, string, error) {
	input = strings.TrimSpace(input)
	if input == "" || len(input) > 128 || imageLookupHasUnsafeCharacters(input) {
		return "", "", &imageLookupInputError{message: "tag must be head or a minor-line head tag such as 2.14-head"}
	}
	matches := prBuildHeadTagPattern.FindStringSubmatch(input)
	if matches == nil {
		return "", "", &imageLookupInputError{message: "tag must be head or a minor-line head tag such as 2.14-head"}
	}
	normalized := strings.ToLower(input)
	if normalized != "head" && !strings.HasPrefix(normalized, "v") {
		normalized = "v" + normalized
	}
	minorLine := ""
	if len(matches) > 1 {
		minorLine = strings.ToLower(matches[1])
	}
	return normalized, minorLine, nil
}

func prBuildBaseRefMatchesMinorLine(baseRef, minorLine string) bool {
	baseRef = strings.ToLower(strings.TrimSpace(baseRef))
	minorLine = strings.ToLower(strings.TrimSpace(minorLine))
	if baseRef == "" || minorLine == "" {
		return false
	}
	patterns := []string{
		minorLine,
		"v" + minorLine,
		"release-" + minorLine,
		"release-v" + minorLine,
		"release/" + minorLine,
		"release/v" + minorLine,
	}
	for _, pattern := range patterns {
		if baseRef == pattern {
			return true
		}
	}
	return false
}

func normalizePRBuildPullRequest(target prBuildTarget, raw prBuildGitHubPull) (prBuildPullRequest, error) {
	githubError := func(message string) (prBuildPullRequest, error) {
		return prBuildPullRequest{}, errors.New(message)
	}
	wantRepository := target.owner + "/" + target.repository
	if raw.Number != target.number || !strings.EqualFold(raw.Base.Repo.FullName, wantRepository) {
		return githubError("GitHub returned PR metadata for a different repository or pull request")
	}
	headSHA := strings.ToLower(strings.TrimSpace(raw.Head.SHA))
	if !imageLookupGitRevisionPattern.MatchString(headSHA) {
		return githubError("GitHub PR metadata did not include a valid full head commit SHA")
	}
	mergeSHA := strings.ToLower(strings.TrimSpace(raw.MergeCommitSHA))
	inclusionSHA := headSHA
	inclusionBasis := "pr_head"
	if raw.Merged {
		if !imageLookupGitRevisionPattern.MatchString(mergeSHA) {
			return githubError("Merged GitHub PR metadata did not include a valid integration commit SHA")
		}
		inclusionSHA = mergeSHA
		inclusionBasis = "merged_commit"
	} else {
		mergeSHA = ""
	}
	state := strings.ToLower(safeOCIProvenanceLabel(raw.State))
	if raw.Merged {
		state = "merged"
	} else if state != "open" && state != "closed" {
		state = "unknown"
	}
	return prBuildPullRequest{
		URL:                target.url,
		Repository:         wantRepository,
		Number:             target.number,
		Title:              safeOCIProvenanceLabel(raw.Title),
		State:              state,
		Draft:              raw.Draft,
		Merged:             raw.Merged,
		MergedAt:           safeOCIProvenanceLabel(raw.MergedAt),
		BaseRef:            safeOCIProvenanceLabel(raw.Base.Ref),
		HeadRef:            safeOCIProvenanceLabel(raw.Head.Ref),
		HeadRepository:     safeOCIProvenanceLabel(raw.Head.Repo.FullName),
		HeadSHA:            headSHA,
		MergeCommitSHA:     mergeSHA,
		InclusionCommitSHA: inclusionSHA,
		InclusionCommitURL: fmt.Sprintf("https://github.com/%s/%s/commit/%s", target.owner, target.repository, inclusionSHA),
		InclusionBasis:     inclusionBasis,
	}, nil
}

func (s *prBuildVerifierService) fetchPullFromGitHub(ctx context.Context, target prBuildTarget) (prBuildGitHubPull, error) {
	endpoint := fmt.Sprintf("/repos/%s/%s/pulls/%d", target.owner, target.repository, target.number)
	jq := `{number:.number,html_url:.html_url,title:.title,state:.state,draft:.draft,merged:.merged,merged_at:.merged_at,merge_commit_sha:.merge_commit_sha,head:{sha:.head.sha,ref:.head.ref,repo:{full_name:.head.repo.full_name}},base:{sha:.base.sha,ref:.base.ref,repo:{full_name:.base.repo.full_name}}}`
	var result prBuildGitHubPull
	if err := s.runGitHubJSON(ctx, endpoint, jq, &result, "pull request"); err != nil {
		return prBuildGitHubPull{}, err
	}
	return result, nil
}

func (s *prBuildVerifierService) compareCommitsOnGitHub(ctx context.Context, target prBuildTarget, baseSHA, headSHA string) (prBuildGitHubCompare, error) {
	if !imageLookupGitRevisionPattern.MatchString(baseSHA) || !imageLookupGitRevisionPattern.MatchString(headSHA) {
		return prBuildGitHubCompare{}, errors.New("GitHub ancestry check requires full commit SHAs")
	}
	endpoint := fmt.Sprintf("/repos/%s/%s/compare/%s...%s", target.owner, target.repository, strings.ToLower(baseSHA), strings.ToLower(headSHA))
	jq := `{status:.status,ahead_by:.ahead_by,behind_by:.behind_by,html_url:.html_url,merge_base_sha:.merge_base_commit.sha}`
	var result prBuildGitHubCompare
	if err := s.runGitHubJSON(ctx, endpoint, jq, &result, "commit ancestry"); err != nil {
		return prBuildGitHubCompare{}, err
	}
	result.Status = strings.ToLower(strings.TrimSpace(result.Status))
	result.MergeBaseSHA = strings.ToLower(strings.TrimSpace(result.MergeBaseSHA))
	return result, nil
}

func (s *prBuildVerifierService) runGitHubJSON(ctx context.Context, endpoint, jq string, destination any, operation string) error {
	commandCtx, cancel := context.WithTimeout(ctx, prBuildGitHubTimeout)
	defer cancel()
	arguments := []string{
		"api",
		"--include",
		"--hostname", "github.com",
		"--method", http.MethodGet,
		"-H", "Accept:application/vnd.github+json",
		endpoint,
		"--jq", jq,
	}
	raw, err := s.runCommand(commandCtx, "gh", arguments, imageLookupSanitizedGHEnvironment(), prBuildGitHubOutputLimit)
	status, payload := parsePRBuildGitHubIncludedResponse(raw)
	if err != nil {
		switch {
		case commandCtx.Err() != nil:
			return fmt.Errorf("GitHub %s lookup failed: %w", operation, commandCtx.Err())
		case errors.Is(err, errImageLookupCommandOutputLimit):
			return fmt.Errorf("GitHub %s response exceeded the safe output limit", operation)
		case status >= 400:
			return &prBuildGitHubHTTPError{status: status, operation: operation}
		default:
			return fmt.Errorf("could not read GitHub %s; confirm GitHub CLI authentication and repository access", operation)
		}
	}
	if status != 0 && (status < 200 || status >= 300) {
		return &prBuildGitHubHTTPError{status: status, operation: operation}
	}
	if err := json.Unmarshal(payload, destination); err != nil {
		return fmt.Errorf("GitHub returned invalid %s metadata", operation)
	}
	return nil
}

func parsePRBuildGitHubIncludedResponse(raw []byte) (int, []byte) {
	remaining := string(raw)
	status := 0
	for strings.HasPrefix(remaining, "HTTP/") {
		headerEnd := strings.Index(remaining, "\r\n\r\n")
		separatorLength := len("\r\n\r\n")
		if headerEnd < 0 {
			headerEnd = strings.Index(remaining, "\n\n")
			separatorLength = len("\n\n")
		}
		if headerEnd < 0 {
			return 0, raw
		}
		header := remaining[:headerEnd]
		firstLine := header
		if lineEnd := strings.IndexByte(firstLine, '\n'); lineEnd >= 0 {
			firstLine = firstLine[:lineEnd]
		}
		fields := strings.Fields(strings.TrimSpace(firstLine))
		if len(fields) < 2 {
			return 0, raw
		}
		parsedStatus, err := strconv.Atoi(fields[1])
		if err != nil || parsedStatus < 100 || parsedStatus > 599 {
			return 0, raw
		}
		status = parsedStatus
		remaining = remaining[headerEnd+separatorLength:]
	}
	return status, []byte(strings.TrimSpace(remaining))
}

type prBuildInspectJob struct {
	registryIndex int
	kind          string
	reference     string
}

type prBuildInspectResult struct {
	registryIndex int
	kind          string
	image         prBuildImageResult
}

func (s *prBuildVerifierService) inspectKnownRegistries(ctx context.Context, tag string) ([]prBuildRegistryResult, error) {
	registries := make([]prBuildRegistryResult, len(settings.PreferredImageRegistryOptions))
	jobs := make(chan prBuildInspectJob)
	results := make(chan prBuildInspectResult)
	workerCount := prBuildWorkerLimit
	if workerCount > len(settings.PreferredImageRegistryOptions)*2 {
		workerCount = len(settings.PreferredImageRegistryOptions) * 2
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
		for index, registry := range settings.PreferredImageRegistryOptions {
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

	for index, registry := range settings.PreferredImageRegistryOptions {
		registries[index] = prBuildRegistryResult{Registry: registry, Label: preferredRancherRegistryLabel(registry)}
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

func (s *prBuildVerifierService) inspectImage(ctx context.Context, reference string) prBuildImageResult {
	result := prBuildImageResult{
		Reference: reference,
		Match: prBuildCommitMatch{
			Verdict: "not_applicable",
			Reason:  "Image provenance was not inspected.",
		},
	}
	response, err := s.inspect(ctx, imageLookupInspectRequest{
		Reference:        reference,
		Platform:         prBuildPlatform,
		IncludeBuildYAML: false,
		SkipTagMetadata:  true,
	})
	if err != nil {
		if imageLookupRegistryNotFound(err) {
			result.Match.Reason = "The exact image tag was not found in this registry."
			return result
		}
		result.Error = imageLookupSafeError(err)
		result.Match.Reason = "The registry lookup failed before provenance could be inspected."
		return result
	}
	labels := response.Config.Labels
	result.Found = true
	if response.Reference != "" {
		result.Reference = response.Reference
	}
	result.Digest = response.Digest
	result.PlatformDigest = prBuildSelectedPlatformDigest(response)
	result.Platform = response.Platform
	result.BuildVersion = safeOCIProvenanceLabel(labels[imageLookupVersionLabel])
	result.SourceURL = safeOCIProvenanceLabel(labels[imageLookupSourceLabel])
	result.Revision = safeOCIProvenanceLabel(labels[imageLookupRevisionLabel])
	result.OSSRevision = safeOCIProvenanceLabel(labels[imageLookupOSSRevisionLabel])
	result.Match = prBuildCommitMatch{Verdict: "unknown", Reason: "The image does not expose comparable GitHub provenance."}
	return result
}

func prBuildSelectedPlatformDigest(response imageLookupInspectResponse) string {
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

type prBuildComparisonResult struct {
	comparison prBuildGitHubCompare
	err        error
}

func (s *prBuildVerifierService) compareImageRevisions(ctx context.Context, target prBuildTarget, pull prBuildPullRequest, registries []prBuildRegistryResult) error {
	type imagePointer struct {
		image *prBuildImageResult
	}
	byRevision := map[string][]imagePointer{}
	for index := range registries {
		for _, image := range []*prBuildImageResult{&registries[index].Server, &registries[index].Agent} {
			if !image.Found || image.Error != "" {
				continue
			}
			revision, label, reason := prBuildComparableRevision(target, *image)
			if revision == "" {
				image.Match = prBuildCommitMatch{
					Verdict:          "unknown",
					Reason:           reason,
					RequiredRevision: pull.InclusionCommitSHA,
					Basis:            pull.InclusionBasis,
				}
				continue
			}
			image.Match = prBuildCommitMatch{
				Verdict:           "unknown",
				Reason:            "GitHub ancestry has not been checked yet.",
				CandidateRevision: revision,
				RequiredRevision:  pull.InclusionCommitSHA,
				RevisionLabel:     label,
				Basis:             pull.InclusionBasis,
				CommitURL:         fmt.Sprintf("https://github.com/%s/%s/commit/%s", target.owner, target.repository, revision),
			}
			if revision == pull.InclusionCommitSHA {
				image.Match.Verdict = "included"
				image.Match.Relation = "exact"
				image.Match.Reason = "The image declares the exact commit selected from the pull request."
				continue
			}
			byRevision[revision] = append(byRevision[revision], imagePointer{image: image})
		}
	}

	comparisonResults := make(map[string]prBuildComparisonResult, len(byRevision))
	if len(byRevision) > 0 {
		jobs := make(chan string)
		results := make(chan struct {
			revision string
			result   prBuildComparisonResult
		})
		workerCount := prBuildWorkerLimit
		if len(byRevision) < workerCount {
			workerCount = len(byRevision)
		}
		var workers sync.WaitGroup
		for worker := 0; worker < workerCount; worker++ {
			workers.Add(1)
			go func() {
				defer workers.Done()
				for revision := range jobs {
					comparison, err := s.compare(ctx, target, pull.InclusionCommitSHA, revision)
					select {
					case results <- struct {
						revision string
						result   prBuildComparisonResult
					}{revision: revision, result: prBuildComparisonResult{comparison: comparison, err: err}}:
					case <-ctx.Done():
						return
					}
				}
			}()
		}
		go func() {
			for revision := range byRevision {
				select {
				case jobs <- revision:
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
		for result := range results {
			comparisonResults[result.revision] = result.result
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}

	for revision, pointers := range byRevision {
		result := comparisonResults[revision]
		for _, pointer := range pointers {
			applyPRBuildComparison(pointer.image, target, pull, result)
		}
	}
	return nil
}

func prBuildComparableRevision(target prBuildTarget, image prBuildImageResult) (string, string, string) {
	requestedRepository := target.owner + "/" + target.repository
	standardOwner, standardRepository, sourceErr := imageLookupParseGitHubSource(strings.TrimSpace(image.SourceURL), strings.Repeat("0", 40))
	standardRevision := strings.ToLower(strings.TrimSpace(image.Revision))
	if sourceErr == nil && strings.EqualFold(standardOwner+"/"+standardRepository, requestedRepository) && imageLookupGitRevisionPattern.MatchString(standardRevision) {
		return standardRevision, imageLookupRevisionLabel, ""
	}
	if strings.EqualFold(requestedRepository, "rancher/rancher") && sourceErr == nil && strings.EqualFold(standardOwner+"/"+standardRepository, "rancher/rancher-prime") {
		ossRevision := strings.ToLower(strings.TrimSpace(image.OSSRevision))
		if imageLookupGitRevisionPattern.MatchString(ossRevision) {
			return ossRevision, imageLookupOSSRevisionLabel, ""
		}
		return "", "", "The Prime image does not declare a valid full Rancher OSS revision."
	}
	if sourceErr != nil {
		return "", "", sourceErr.Error()
	}
	if strings.EqualFold(standardOwner+"/"+standardRepository, requestedRepository) {
		return "", "", "The image does not declare a valid full 40-character Git revision."
	}
	return "", "", fmt.Sprintf("The image source %s does not match pull request repository %s.", standardOwner+"/"+standardRepository, requestedRepository)
}

func applyPRBuildComparison(image *prBuildImageResult, target prBuildTarget, pull prBuildPullRequest, result prBuildComparisonResult) {
	image.Match.CompareURL = fmt.Sprintf("https://github.com/%s/%s/compare/%s...%s", target.owner, target.repository, pull.InclusionCommitSHA, image.Match.CandidateRevision)
	if result.err != nil {
		image.Match.Verdict = "unknown"
		image.Match.Reason = safePRBuildGitHubError(result.err, "commit ancestry")
		image.Match.ComparisonError = true
		return
	}
	comparison := result.comparison
	image.Match.Relation = comparison.Status
	switch comparison.Status {
	case "identical":
		image.Match.Verdict = "included"
		image.Match.Reason = "GitHub reports that the image revision is identical to the selected PR commit."
	case "ahead":
		if comparison.MergeBaseSHA != pull.InclusionCommitSHA {
			image.Match.Verdict = "unknown"
			image.Match.Reason = "GitHub reported the image revision as ahead, but did not confirm the selected PR commit as the merge base."
			image.Match.ComparisonError = true
			return
		}
		image.Match.Verdict = "included"
		image.Match.Reason = "GitHub confirms that the selected PR commit is an ancestor of the image's declared revision."
	case "behind", "diverged":
		image.Match.Verdict = "not_included"
		image.Match.Reason = "GitHub did not find the selected PR commit in this image revision's ancestry. This does not detect an equivalent cherry-pick with a different SHA."
	default:
		image.Match.Verdict = "unknown"
		image.Match.Reason = "GitHub returned an unrecognized commit relationship."
		image.Match.ComparisonError = true
	}
}

func safePRBuildGitHubError(err error, operation string) string {
	var githubErr *prBuildGitHubHTTPError
	if errors.As(err, &githubErr) {
		return githubErr.Error()
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return fmt.Sprintf("GitHub %s lookup timed out or was cancelled", operation)
	}
	return fmt.Sprintf("GitHub could not check %s; confirm GitHub CLI authentication and repository access", operation)
}

func prBuildRegistryStatus(result prBuildRegistryResult) string {
	if result.Server.Error != "" || result.Agent.Error != "" {
		return "error"
	}
	if result.Server.Found && result.Agent.Found {
		return "complete"
	}
	if result.Server.Found || result.Agent.Found {
		return "partial"
	}
	return "missing"
}

func summarizePRBuildResults(registries []prBuildRegistryResult) prBuildSummary {
	summary := prBuildSummary{RegistryCount: len(registries), ScanComplete: true}
	for _, registry := range registries {
		if registry.PairAvailable {
			summary.CompletePairRegistries++
		}
		if registry.Server.Error != "" {
			summary.ServerErrorRegistries++
			summary.ScanComplete = false
		} else if !registry.Server.Found {
			summary.ServerMissingRegistries++
		} else {
			switch registry.Server.Match.Verdict {
			case "included":
				summary.ServerIncludedRegistries++
			case "not_included":
				summary.ServerNotIncludedRegistries++
			default:
				summary.ServerUnknownRegistries++
			}
		}
		for _, image := range []prBuildImageResult{registry.Server, registry.Agent} {
			if image.Error != "" || image.Match.ComparisonError {
				summary.ScanComplete = false
			}
		}
	}
	switch {
	case summary.ServerIncludedRegistries > 0:
		summary.Verdict = "included"
	case summary.ServerUnknownRegistries > 0 || summary.ServerErrorRegistries > 0:
		summary.Verdict = "unknown"
	case summary.ServerNotIncludedRegistries > 0:
		summary.Verdict = "not_included"
	default:
		summary.Verdict = "unknown"
	}
	return summary
}

func (p *localControlPanel) prBuildVerifierBackend() *prBuildVerifierService {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.prBuildVerifier == nil {
		if p.imageLookup == nil {
			p.imageLookup = newImageLookupService()
		}
		p.prBuildVerifier = newPRBuildVerifierService(p.imageLookup)
	}
	return p.prBuildVerifier
}

func (p *localControlPanel) handlePRBuildVerify(w http.ResponseWriter, request *http.Request) {
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
	var payload prBuildVerifyRequest
	if err := decodeImageLookupJSON(w, request, &payload); err != nil {
		http.Error(w, imageLookupSafeError(err), imageLookupHTTPStatus(err))
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), prBuildVerifyTimeout)
	defer cancel()
	response, err := p.prBuildVerifierBackend().Verify(ctx, payload)
	if err != nil {
		message := imageLookupSafeError(err)
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			message = "PR image verification timed out"
		}
		http.Error(w, message, prBuildHTTPStatus(err))
		return
	}
	writeJSON(w, response)
}

func prBuildHTTPStatus(err error) int {
	var githubErr *prBuildGitHubHTTPError
	if errors.As(err, &githubErr) {
		switch githubErr.status {
		case http.StatusNotFound:
			return http.StatusNotFound
		case http.StatusTooManyRequests:
			return http.StatusTooManyRequests
		default:
			return http.StatusBadGateway
		}
	}
	return imageLookupHTTPStatus(err)
}
