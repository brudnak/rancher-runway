package test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
)

const (
	prBuildTestHeadSHA  = "1111111111111111111111111111111111111111"
	prBuildTestMergeSHA = "2222222222222222222222222222222222222222"
	prBuildTestImageSHA = "3333333333333333333333333333333333333333"
)

func TestParsePRBuildPullRequestURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		input     string
		wantOwner string
		wantRepo  string
		wantNum   int
		wantError bool
	}{
		{name: "canonical", input: "https://github.com/rancher/rancher/pull/12345", wantOwner: "rancher", wantRepo: "rancher", wantNum: 12345},
		{name: "trailing slash", input: "https://github.com/rancher/rancher/pull/9/", wantOwner: "rancher", wantRepo: "rancher", wantNum: 9},
		{name: "trimmed", input: "  https://github.com/rancher/rancher/pull/7  ", wantOwner: "rancher", wantRepo: "rancher", wantNum: 7},
		{name: "http", input: "http://github.com/rancher/rancher/pull/1", wantError: true},
		{name: "lookalike host", input: "https://github.com.evil/rancher/rancher/pull/1", wantError: true},
		{name: "userinfo", input: "https://github.com@evil.example/rancher/rancher/pull/1", wantError: true},
		{name: "port", input: "https://github.com:443/rancher/rancher/pull/1", wantError: true},
		{name: "query", input: "https://github.com/rancher/rancher/pull/1?x=1", wantError: true},
		{name: "fragment", input: "https://github.com/rancher/rancher/pull/1#files", wantError: true},
		{name: "extra path", input: "https://github.com/rancher/rancher/pull/1/files", wantError: true},
		{name: "encoded path", input: "https://github.com/rancher/rancher/%70ull/1", wantError: true},
		{name: "leading zero", input: "https://github.com/rancher/rancher/pull/01", wantError: true},
		{name: "zero", input: "https://github.com/rancher/rancher/pull/0", wantError: true},
		{name: "negative", input: "https://github.com/rancher/rancher/pull/-1", wantError: true},
		{name: "control", input: "https://github.com/rancher/rancher/pull/1\n", wantError: true},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			target, err := parsePRBuildPullRequestURL(testCase.input)
			if testCase.wantError {
				if err == nil {
					t.Fatalf("parsePRBuildPullRequestURL(%q) unexpectedly succeeded: %+v", testCase.input, target)
				}
				var inputErr *imageLookupInputError
				if !errors.As(err, &inputErr) {
					t.Fatalf("error = %T, want imageLookupInputError", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parsePRBuildPullRequestURL(%q): %v", testCase.input, err)
			}
			if target.owner != testCase.wantOwner || target.repository != testCase.wantRepo || target.number != testCase.wantNum {
				t.Fatalf("target = %+v, want owner=%q repo=%q number=%d", target, testCase.wantOwner, testCase.wantRepo, testCase.wantNum)
			}
		})
	}
}

func TestNormalizePRBuildHeadTag(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input     string
		wantTag   string
		wantMinor string
		wantError bool
	}{
		{input: "head", wantTag: "head"},
		{input: "HEAD", wantTag: "head"},
		{input: "2.14-head", wantTag: "v2.14-head", wantMinor: "2.14"},
		{input: "v2.14-head", wantTag: "v2.14-head", wantMinor: "2.14"},
		{input: "V2.14-HEAD", wantTag: "v2.14-head", wantMinor: "2.14"},
		{input: "2.14.1", wantError: true},
		{input: "2.14-head-extra", wantError: true},
		{input: "docker.io/rancher/rancher:v2.14-head", wantError: true},
		{input: "2.14-head;whoami", wantError: true},
		{input: "", wantError: true},
	}
	for _, testCase := range tests {
		t.Run(testCase.input, func(t *testing.T) {
			tag, minor, err := normalizePRBuildHeadTag(testCase.input)
			if testCase.wantError {
				if err == nil {
					t.Fatalf("normalizePRBuildHeadTag(%q) unexpectedly returned %q", testCase.input, tag)
				}
				return
			}
			if err != nil || tag != testCase.wantTag || minor != testCase.wantMinor {
				t.Fatalf("normalizePRBuildHeadTag(%q) = %q, %q, %v; want %q, %q", testCase.input, tag, minor, err, testCase.wantTag, testCase.wantMinor)
			}
		})
	}
}

func TestPRBuildBaseRefMatchesMinorLineExactly(t *testing.T) {
	t.Parallel()
	tests := []struct {
		baseRef string
		minor   string
		want    bool
	}{
		{baseRef: "release-v2.14", minor: "2.14", want: true},
		{baseRef: "release-2.14", minor: "2.14", want: true},
		{baseRef: "release/v2.14", minor: "2.14", want: true},
		{baseRef: "release/2.14", minor: "2.14", want: true},
		{baseRef: "v2.14", minor: "2.14", want: true},
		{baseRef: "2.14", minor: "2.14", want: true},
		{baseRef: "release-v2.140", minor: "2.14", want: false},
		{baseRef: "feature/2.14-experiment", minor: "2.14", want: false},
		{baseRef: "main", minor: "2.14", want: false},
	}
	for _, testCase := range tests {
		if got := prBuildBaseRefMatchesMinorLine(testCase.baseRef, testCase.minor); got != testCase.want {
			t.Errorf("prBuildBaseRefMatchesMinorLine(%q, %q) = %v, want %v", testCase.baseRef, testCase.minor, got, testCase.want)
		}
	}
}

func TestNormalizePRBuildPullRequestUsesIntegrationCommitOnlyAfterMerge(t *testing.T) {
	t.Parallel()
	target := prBuildTarget{owner: "rancher", repository: "rancher", number: 42, url: "https://github.com/rancher/rancher/pull/42"}
	raw := testPRBuildPull(42)
	raw.MergeCommitSHA = prBuildTestMergeSHA

	openPull, err := normalizePRBuildPullRequest(target, raw)
	if err != nil {
		t.Fatal(err)
	}
	if openPull.InclusionCommitSHA != prBuildTestHeadSHA || openPull.InclusionBasis != "pr_head" || openPull.MergeCommitSHA != "" || openPull.State != "open" {
		t.Fatalf("open PR normalized incorrectly: %+v", openPull)
	}

	raw.Merged = true
	raw.State = "closed"
	mergedPull, err := normalizePRBuildPullRequest(target, raw)
	if err != nil {
		t.Fatal(err)
	}
	if mergedPull.InclusionCommitSHA != prBuildTestMergeSHA || mergedPull.InclusionBasis != "merged_commit" || mergedPull.State != "merged" {
		t.Fatalf("merged PR normalized incorrectly: %+v", mergedPull)
	}
	if mergedPull.InclusionCommitURL != "https://github.com/rancher/rancher/commit/"+prBuildTestMergeSHA {
		t.Fatalf("commit URL = %q", mergedPull.InclusionCommitURL)
	}

	raw.MergeCommitSHA = ""
	if _, err := normalizePRBuildPullRequest(target, raw); err == nil {
		t.Fatal("merged PR without a valid integration SHA unexpectedly succeeded")
	}
}

func TestPRBuildComparableRevisionUsesTrustedSourceAndPrimeOSSRevision(t *testing.T) {
	t.Parallel()
	target := prBuildTarget{owner: "rancher", repository: "rancher"}
	tests := []struct {
		name       string
		image      prBuildImageResult
		wantSHA    string
		wantLabel  string
		wantReason string
	}{
		{
			name:    "matching public source",
			image:   prBuildImageResult{SourceURL: "https://github.com/rancher/rancher", Revision: prBuildTestImageSHA},
			wantSHA: prBuildTestImageSHA, wantLabel: imageLookupRevisionLabel,
		},
		{
			name:    "prime OSS revision without private revision",
			image:   prBuildImageResult{SourceURL: "https://github.com/rancher/rancher-prime", OSSRevision: prBuildTestImageSHA},
			wantSHA: prBuildTestImageSHA, wantLabel: imageLookupOSSRevisionLabel,
		},
		{
			name:       "source mismatch",
			image:      prBuildImageResult{SourceURL: "https://github.com/example/rancher", Revision: prBuildTestImageSHA},
			wantReason: "does not match",
		},
		{
			name:       "short revision",
			image:      prBuildImageResult{SourceURL: "https://github.com/rancher/rancher", Revision: "abc123"},
			wantReason: "full 40-character",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			sha, label, reason := prBuildComparableRevision(target, testCase.image)
			if sha != testCase.wantSHA || label != testCase.wantLabel {
				t.Fatalf("revision = %q, label = %q; want %q, %q (reason %q)", sha, label, testCase.wantSHA, testCase.wantLabel, reason)
			}
			if testCase.wantReason != "" && !strings.Contains(reason, testCase.wantReason) {
				t.Fatalf("reason = %q, want containing %q", reason, testCase.wantReason)
			}
		})
	}
}

func TestApplyPRBuildComparisonUsesRequiredCommitAsAncestor(t *testing.T) {
	t.Parallel()
	target := prBuildTarget{owner: "rancher", repository: "rancher"}
	pull := prBuildPullRequest{InclusionCommitSHA: prBuildTestMergeSHA, InclusionBasis: "merged_commit"}
	tests := []struct {
		name        string
		comparison  prBuildGitHubCompare
		err         error
		wantVerdict string
		wantError   bool
	}{
		{name: "ahead includes", comparison: prBuildGitHubCompare{Status: "ahead", MergeBaseSHA: prBuildTestMergeSHA}, wantVerdict: "included"},
		{name: "ahead wrong merge base is unknown", comparison: prBuildGitHubCompare{Status: "ahead", MergeBaseSHA: prBuildTestHeadSHA}, wantVerdict: "unknown", wantError: true},
		{name: "behind excludes", comparison: prBuildGitHubCompare{Status: "behind"}, wantVerdict: "not_included"},
		{name: "diverged excludes", comparison: prBuildGitHubCompare{Status: "diverged"}, wantVerdict: "not_included"},
		{name: "identical includes", comparison: prBuildGitHubCompare{Status: "identical"}, wantVerdict: "included"},
		{name: "API failure is unknown", err: errors.New("unavailable"), wantVerdict: "unknown", wantError: true},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			image := &prBuildImageResult{Match: prBuildCommitMatch{CandidateRevision: prBuildTestImageSHA}}
			applyPRBuildComparison(image, target, pull, prBuildComparisonResult{comparison: testCase.comparison, err: testCase.err})
			if image.Match.Verdict != testCase.wantVerdict || image.Match.ComparisonError != testCase.wantError {
				t.Fatalf("match = %+v, want verdict=%q comparisonError=%v", image.Match, testCase.wantVerdict, testCase.wantError)
			}
			if !strings.Contains(image.Match.CompareURL, prBuildTestMergeSHA+"..."+prBuildTestImageSHA) {
				t.Fatalf("compare URL = %q; compare direction is reversed", image.Match.CompareURL)
			}
		})
	}
}

func TestPRBuildVerifierChecksEveryKnownRegistryAndDeduplicatesComparisons(t *testing.T) {
	request := prBuildVerifyRequest{PullRequest: "https://github.com/rancher/rancher/pull/42", Tag: "2.14-head"}
	rawPull := testPRBuildPull(42)
	rawPull.Merged = true
	rawPull.State = "closed"
	rawPull.MergeCommitSHA = prBuildTestMergeSHA

	var inspectMu sync.Mutex
	inspected := []string{}
	var compareMu sync.Mutex
	compareCalls := 0
	service := &prBuildVerifierService{
		now: func() time.Time { return time.Date(2026, time.August, 13, 20, 0, 0, 0, time.UTC) },
		fetchPull: func(context.Context, prBuildTarget) (prBuildGitHubPull, error) {
			return rawPull, nil
		},
		inspect: func(_ context.Context, request imageLookupInspectRequest) (imageLookupInspectResponse, error) {
			inspectMu.Lock()
			inspected = append(inspected, request.Reference)
			inspectMu.Unlock()
			if strings.HasPrefix(request.Reference, "registry.suse.com/") {
				return imageLookupInspectResponse{}, &transport.Error{StatusCode: http.StatusNotFound}
			}
			if strings.HasPrefix(request.Reference, "docker.io/") {
				return imageLookupInspectResponse{}, &transport.Error{StatusCode: http.StatusTooManyRequests}
			}
			labels := map[string]string{
				imageLookupSourceLabel:   "https://github.com/rancher/rancher",
				imageLookupRevisionLabel: prBuildTestImageSHA,
				imageLookupVersionLabel:  "v2.14-head-build-42",
			}
			if strings.HasPrefix(request.Reference, "registry.rancher.com/") {
				labels[imageLookupSourceLabel] = "https://github.com/rancher/rancher-prime"
				labels[imageLookupRevisionLabel] = ""
				labels[imageLookupOSSRevisionLabel] = prBuildTestImageSHA
			}
			return imageLookupInspectResponse{
				Reference: request.Reference,
				Digest:    "sha256:" + strings.Repeat("a", 64),
				Platform:  prBuildPlatform,
				Platforms: []imageLookupPlatform{{OS: "linux", Architecture: "amd64", Digest: "sha256:" + strings.Repeat("b", 64)}},
				Config:    imageLookupImageConfig{Labels: labels},
			}, nil
		},
		compare: func(_ context.Context, _ prBuildTarget, base, head string) (prBuildGitHubCompare, error) {
			compareMu.Lock()
			compareCalls++
			compareMu.Unlock()
			if base != prBuildTestMergeSHA || head != prBuildTestImageSHA {
				t.Errorf("compare(%q, %q), want required PR commit then image revision", base, head)
			}
			return prBuildGitHubCompare{Status: "ahead", MergeBaseSHA: prBuildTestMergeSHA}, nil
		},
	}

	response, err := service.Verify(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if response.Tag != "v2.14-head" || response.Platform != prBuildPlatform || !response.CheckedAt.Equal(service.now()) {
		t.Fatalf("response metadata = tag %q platform %q checked %s", response.Tag, response.Platform, response.CheckedAt)
	}
	if response.PullRequest.InclusionCommitSHA != prBuildTestMergeSHA || response.PullRequest.InclusionBasis != "merged_commit" {
		t.Fatalf("pull request = %+v", response.PullRequest)
	}
	if len(response.Registries) != 4 {
		t.Fatalf("registry count = %d, want 4", len(response.Registries))
	}
	wantOrder := []string{"stgregistry.suse.com", "registry.rancher.com", "registry.suse.com", "docker.io"}
	for index, registry := range response.Registries {
		if registry.Registry != wantOrder[index] {
			t.Fatalf("registry[%d] = %q, want %q", index, registry.Registry, wantOrder[index])
		}
	}
	sort.Strings(inspected)
	wantReferences := make([]string, 0, 8)
	for _, registry := range wantOrder {
		wantReferences = append(wantReferences,
			registry+"/rancher/rancher:v2.14-head",
			registry+"/rancher/rancher-agent:v2.14-head",
		)
	}
	sort.Strings(wantReferences)
	if strings.Join(inspected, "\n") != strings.Join(wantReferences, "\n") {
		t.Fatalf("inspected references =\n%s\nwant\n%s", strings.Join(inspected, "\n"), strings.Join(wantReferences, "\n"))
	}
	compareMu.Lock()
	gotCompareCalls := compareCalls
	compareMu.Unlock()
	if gotCompareCalls != 1 {
		t.Fatalf("compare calls = %d, want one deduplicated comparison", gotCompareCalls)
	}
	if response.Summary.Verdict != "included" || response.Summary.ServerIncludedRegistries != 2 || response.Summary.ServerMissingRegistries != 1 || response.Summary.ServerErrorRegistries != 1 || response.Summary.ScanComplete {
		t.Fatalf("summary = %+v", response.Summary)
	}
	if !response.Registries[0].PairAvailable || response.Registries[0].Server.PlatformDigest != "sha256:"+strings.Repeat("b", 64) {
		t.Fatalf("staging result = %+v", response.Registries[0])
	}
	if response.Registries[1].Server.Match.RevisionLabel != imageLookupOSSRevisionLabel || response.Registries[1].Server.Match.Verdict != "included" {
		t.Fatalf("Prime result = %+v", response.Registries[1].Server)
	}
	if response.Registries[2].Status != "missing" || response.Registries[3].Status != "error" {
		t.Fatalf("isolated miss/error results = %+v / %+v", response.Registries[2], response.Registries[3])
	}
}

func TestPRBuildVerifierOpenPRUsesHeadAndSkipsCompareForExactRevision(t *testing.T) {
	t.Parallel()
	rawPull := testPRBuildPull(42)
	rawPull.MergeCommitSHA = prBuildTestMergeSHA // Synthetic test merge must be ignored while unmerged.
	var compareMu sync.Mutex
	compareCalls := 0
	service := &prBuildVerifierService{
		fetchPull: func(context.Context, prBuildTarget) (prBuildGitHubPull, error) { return rawPull, nil },
		inspect: func(_ context.Context, request imageLookupInspectRequest) (imageLookupInspectResponse, error) {
			return imageLookupInspectResponse{
				Reference: request.Reference,
				Digest:    "sha256:" + strings.Repeat("a", 64),
				Platform:  prBuildPlatform,
				Config: imageLookupImageConfig{Labels: map[string]string{
					imageLookupSourceLabel:   "https://github.com/rancher/rancher",
					imageLookupRevisionLabel: prBuildTestHeadSHA,
				}},
			}, nil
		},
		compare: func(context.Context, prBuildTarget, string, string) (prBuildGitHubCompare, error) {
			compareMu.Lock()
			compareCalls++
			compareMu.Unlock()
			return prBuildGitHubCompare{}, errors.New("exact revisions must bypass compare")
		},
	}
	response, err := service.Verify(context.Background(), prBuildVerifyRequest{
		PullRequest: "https://github.com/rancher/rancher/pull/42",
		Tag:         "2.14-head",
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.PullRequest.InclusionCommitSHA != prBuildTestHeadSHA || response.PullRequest.InclusionBasis != "pr_head" || response.PullRequest.MergeCommitSHA != "" {
		t.Fatalf("open PR inclusion metadata = %+v", response.PullRequest)
	}
	compareMu.Lock()
	gotCompareCalls := compareCalls
	compareMu.Unlock()
	if gotCompareCalls != 0 || response.Registries[0].Server.Match.Relation != "exact" || response.Registries[0].Server.Match.Verdict != "included" {
		t.Fatalf("exact result = calls %d match %+v", gotCompareCalls, response.Registries[0].Server.Match)
	}
	if len(response.Warnings) == 0 || !strings.Contains(response.Warnings[0], "not merged") {
		t.Fatalf("warnings = %v, want unmerged PR warning", response.Warnings)
	}
}

func TestSummarizePRBuildResultsDoesNotCallAllMissingImagesNotIncluded(t *testing.T) {
	t.Parallel()
	missing := []prBuildRegistryResult{
		{Server: prBuildImageResult{Found: false}},
		{Server: prBuildImageResult{Found: false}},
	}
	if summary := summarizePRBuildResults(missing); summary.Verdict != "unknown" || !summary.ScanComplete {
		t.Fatalf("all-missing summary = %+v, want unknown complete scan", summary)
	}
	definitive := append(missing, prBuildRegistryResult{Server: prBuildImageResult{Found: true, Match: prBuildCommitMatch{Verdict: "not_included"}}})
	if summary := summarizePRBuildResults(definitive); summary.Verdict != "not_included" {
		t.Fatalf("definitive summary = %+v, want not_included", summary)
	}
}

func TestPRBuildVerifierGitHubCommandIsFixedAndBounded(t *testing.T) {
	t.Parallel()
	target := prBuildTarget{owner: "rancher", repository: "rancher", number: 42}
	var executable string
	var arguments []string
	var outputLimit int64
	service := &prBuildVerifierService{
		runCommand: func(_ context.Context, command string, args, _ []string, limit int64) ([]byte, error) {
			executable = command
			arguments = append([]string(nil), args...)
			outputLimit = limit
			return []byte("HTTP/2.0 200 OK\r\nContent-Type: application/json\r\n\r\n" + `{"number":42,"title":"Fix","state":"open","draft":false,"merged":false,"head":{"sha":"` + prBuildTestHeadSHA + `","ref":"fix","repo":{"full_name":"rancher/rancher"}},"base":{"sha":"` + prBuildTestMergeSHA + `","ref":"release-v2.14","repo":{"full_name":"rancher/rancher"}}}`), nil
		},
	}
	pull, err := service.fetchPullFromGitHub(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if pull.Number != 42 || executable != "gh" || outputLimit != prBuildGitHubOutputLimit {
		t.Fatalf("command = %q args=%v limit=%d pull=%+v", executable, arguments, outputLimit, pull)
	}
	joined := strings.Join(arguments, " ")
	for _, expected := range []string{"api --include", "--hostname github.com", "--method GET", "/repos/rancher/rancher/pulls/42", "--jq"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("arguments %q do not contain %q", joined, expected)
		}
	}
}

func TestPRBuildGitHubTimeoutPreservesContextIdentity(t *testing.T) {
	t.Parallel()
	service := &prBuildVerifierService{
		runCommand: func(ctx context.Context, _ string, _ []string, _ []string, _ int64) ([]byte, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var destination map[string]any
	err := service.runGitHubJSON(ctx, "/repos/rancher/rancher/pulls/1", ".", &destination, "pull request")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want wrapping context.Canceled", err)
	}
}

func TestPRBuildGitHubHTTPStatusIsParsedWithoutExposingResponseBody(t *testing.T) {
	t.Parallel()
	secretBody := `{"message":"not found","token":"must-not-leak"}`
	service := &prBuildVerifierService{
		runCommand: func(context.Context, string, []string, []string, int64) ([]byte, error) {
			return []byte("HTTP/2.0 404 Not Found\r\nContent-Type: application/json\r\n\r\n" + secretBody), errors.New("exit status 1")
		},
	}
	var destination map[string]any
	err := service.runGitHubJSON(context.Background(), "/repos/rancher/rancher/pulls/999", ".", &destination, "pull request")
	var githubErr *prBuildGitHubHTTPError
	if !errors.As(err, &githubErr) || githubErr.status != http.StatusNotFound {
		t.Fatalf("error = %T %v, want typed GitHub 404", err, err)
	}
	if strings.Contains(err.Error(), "must-not-leak") || prBuildHTTPStatus(err) != http.StatusNotFound {
		t.Fatalf("safe error/status = %q/%d", err, prBuildHTTPStatus(err))
	}
}

func TestParsePRBuildGitHubIncludedResponse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		raw        string
		wantStatus int
		wantBody   string
	}{
		{name: "included headers", raw: "HTTP/2.0 200 OK\r\nContent-Type: application/json\r\n\r\n{\"ok\":true}\n", wantStatus: 200, wantBody: `{"ok":true}`},
		{name: "plain fixture JSON", raw: `{"ok":true}`, wantStatus: 0, wantBody: `{"ok":true}`},
		{name: "invalid header falls back", raw: "HTTP/not-valid\n\nbody", wantStatus: 0, wantBody: "HTTP/not-valid\n\nbody"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			status, body := parsePRBuildGitHubIncludedResponse([]byte(testCase.raw))
			if status != testCase.wantStatus || string(body) != testCase.wantBody {
				t.Fatalf("parsed = %d %q, want %d %q", status, body, testCase.wantStatus, testCase.wantBody)
			}
		})
	}
}

func TestPRBuildVerifierConcurrentFirstUse(t *testing.T) {
	service := &prBuildVerifierService{
		fetchPull: func(_ context.Context, target prBuildTarget) (prBuildGitHubPull, error) {
			return testPRBuildPull(target.number), nil
		},
		inspect: func(context.Context, imageLookupInspectRequest) (imageLookupInspectResponse, error) {
			return imageLookupInspectResponse{}, &transport.Error{StatusCode: http.StatusNotFound}
		},
	}
	const requestCount = 8
	errorsCh := make(chan error, requestCount)
	var workers sync.WaitGroup
	for index := 0; index < requestCount; index++ {
		workers.Add(1)
		go func(number int) {
			defer workers.Done()
			_, err := service.Verify(context.Background(), prBuildVerifyRequest{
				PullRequest: fmt.Sprintf("https://github.com/rancher/rancher/pull/%d", number+1),
				Tag:         "2.14-head",
			})
			errorsCh <- err
		}(index)
	}
	workers.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestPRBuildVerifyHandlerAuthMethodStrictJSONAndSuccess(t *testing.T) {
	rawPull := testPRBuildPull(42)
	service := &prBuildVerifierService{
		fetchPull: func(context.Context, prBuildTarget) (prBuildGitHubPull, error) { return rawPull, nil },
		inspect: func(context.Context, imageLookupInspectRequest) (imageLookupInspectResponse, error) {
			return imageLookupInspectResponse{}, &transport.Error{StatusCode: http.StatusNotFound}
		},
		now: func() time.Time { return time.Date(2026, time.August, 13, 20, 0, 0, 0, time.UTC) },
	}
	panel := &localControlPanel{token: "test-token", prBuildVerifier: service}
	tests := []struct {
		name        string
		method      string
		body        string
		authorized  bool
		wantStatus  int
		wantBody    string
		wantAllowed string
	}{
		{name: "auth", method: http.MethodPost, body: `{}`, wantStatus: http.StatusForbidden, wantBody: "invalid control panel token"},
		{name: "method", method: http.MethodGet, authorized: true, wantStatus: http.StatusMethodNotAllowed, wantBody: "method not allowed", wantAllowed: http.MethodPost},
		{name: "unknown field", method: http.MethodPost, authorized: true, body: `{"pullRequest":"https://github.com/rancher/rancher/pull/42","tag":"2.14-head","unexpected":true}`, wantStatus: http.StatusBadRequest, wantBody: "unknown field"},
		{name: "invalid URL", method: http.MethodPost, authorized: true, body: `{"pullRequest":"https://example.com/rancher/rancher/pull/42","tag":"2.14-head"}`, wantStatus: http.StatusBadRequest, wantBody: "github.com"},
		{name: "success", method: http.MethodPost, authorized: true, body: `{"pullRequest":"https://github.com/rancher/rancher/pull/42","tag":"2.14-head"}`, wantStatus: http.StatusOK, wantBody: `"tag": "v2.14-head"`},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(testCase.method, "/api/pr-builds/verify", strings.NewReader(testCase.body))
			request.RemoteAddr = "198.51.100.5:12345"
			if testCase.authorized {
				request.Header.Set("X-Control-Panel-Token", "test-token")
			}
			recorder := httptest.NewRecorder()
			panel.handlePRBuildVerify(recorder, request)
			if recorder.Code != testCase.wantStatus || !strings.Contains(recorder.Body.String(), testCase.wantBody) {
				t.Fatalf("status=%d body=%q, want status=%d containing %q", recorder.Code, recorder.Body.String(), testCase.wantStatus, testCase.wantBody)
			}
			if recorder.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("Cache-Control = %q", recorder.Header().Get("Cache-Control"))
			}
			if testCase.wantAllowed != "" && recorder.Header().Get("Allow") != testCase.wantAllowed {
				t.Fatalf("Allow = %q, want %q", recorder.Header().Get("Allow"), testCase.wantAllowed)
			}
		})
	}
}

func TestPRBuildVerifierResponseJSONDoesNotExposeInternalComparisonError(t *testing.T) {
	t.Parallel()
	response := prBuildVerifyResponse{Registries: []prBuildRegistryResult{{Server: prBuildImageResult{Match: prBuildCommitMatch{ComparisonError: true}}}}}
	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "ComparisonError") || strings.Contains(string(raw), "comparisonError") {
		t.Fatalf("internal comparison error flag leaked in JSON: %s", raw)
	}
}

func testPRBuildPull(number int) prBuildGitHubPull {
	var pull prBuildGitHubPull
	pull.Number = number
	pull.HTMLURL = fmt.Sprintf("https://github.com/rancher/rancher/pull/%d", number)
	pull.Title = "Verify the fix"
	pull.State = "open"
	pull.Head.SHA = prBuildTestHeadSHA
	pull.Head.Ref = "fix-branch"
	pull.Head.Repo.FullName = "rancher/rancher"
	pull.Base.SHA = prBuildTestMergeSHA
	pull.Base.Ref = "release-v2.14"
	pull.Base.Repo.FullName = "rancher/rancher"
	return pull
}
