package imagelookup

import (
	"context"
	"github.com/google/go-containerregistry/pkg/registry"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestImageLookupPrimePatchSelectorVerifiesAndRanksCompletePairs(t *testing.T) {
	var tagListRequests atomic.Int32
	registryHandler := registry.New(registry.Logger(log.New(io.Discard, "", 0)))
	registryServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "/tags/list") {
			tagListRequests.Add(1)
		}
		registryHandler.ServeHTTP(response, request)
	}))
	defer registryServer.Close()

	shaNewest := strings.Repeat("a", 40)
	shaOlder := strings.Repeat("b", 40)
	shaMissing := strings.Repeat("c", 40)
	shaInvalid := strings.Repeat("d", 40)
	newestTag := "v2.15.1-" + shaNewest + "-head"
	olderTag := "v2.15.1-" + shaOlder + "-head"
	missingTag := "v2.15.1-" + shaMissing + "-head"
	invalidTag := "v2.15.1-" + shaInvalid + "-head"
	newestServerTime := time.Date(2026, time.August, 20, 10, 0, 0, 0, time.UTC)
	newestAgentTime := newestServerTime.Add(2 * time.Hour)
	olderServerTime := time.Date(2026, time.August, 10, 10, 0, 0, 0, time.UTC)
	olderAgentTime := olderServerTime.Add(time.Hour)

	pushImageLookupPrimeFixture(t, registryServer, "rancher/rancher", newestTag, shaNewest, newestServerTime)
	pushImageLookupPrimeFixture(t, registryServer, "rancher/rancher-agent", newestTag, shaNewest, newestAgentTime)
	pushImageLookupPrimeFixture(t, registryServer, "rancher/rancher", olderTag, shaOlder, olderServerTime)
	pushImageLookupPrimeFixture(t, registryServer, "rancher/rancher-agent", olderTag, shaOlder, olderAgentTime)
	pushImageLookupPrimeFixture(t, registryServer, "rancher/rancher", missingTag, shaMissing, newestAgentTime.Add(24*time.Hour))
	pushImageLookupPrimeFixture(t, registryServer, "rancher/rancher", invalidTag, strings.Repeat("e", 40), newestAgentTime.Add(48*time.Hour))
	pushImageLookupPrimeFixture(t, registryServer, "rancher/rancher-agent", invalidTag, shaInvalid, newestAgentTime.Add(49*time.Hour))

	service := newImageLookupRewritingTestService(t, registryServer, "")
	result, err := service.Search(context.Background(), SearchRequest{
		Registry:   "all",
		Repository: "rancher/rancher",
		Query:      "v2.15.1-head",
		Limit:      200,
	})
	if err != nil {
		t.Fatalf("verify Prime patch selector: %v", err)
	}
	if len(result.Groups) != 1 {
		t.Fatalf("Prime selector groups = %d, want 1", len(result.Groups))
	}
	group := result.Groups[0]
	if group.Registry != "stgregistry.suse.com" || group.Matched != 4 || group.PrimeHeadCount != 4 || group.ImmutablePrimeHeadCount != 4 || group.VerifiedPrimeHeadCount != 2 || group.InvalidPrimeHeadCount != 1 || group.MissingCompanionCount != 1 {
		t.Fatalf("Prime pair accounting = %#v", group)
	}
	if len(group.Tags) != 4 || group.Tags[0].Name != newestTag || group.Tags[1].Name != olderTag {
		t.Fatalf("Prime pair completion order = %#v", group.Tags)
	}
	newest := group.Tags[0]
	if newest.PairStatus != "verified" || !newest.PairComplete || !newest.CompanionVerified || !newest.ProvenanceValid || !newest.PrimeSource || newest.Source != "https://github.com/rancher/rancher-prime" || newest.ResolvedRank != 1 || newest.PairCompletedAt != imageLookupFormatTime(newestAgentTime) {
		t.Fatalf("newest verified Prime candidate = %#v", newest)
	}
	if newest.CompanionReference != "stgregistry.suse.com/rancher/rancher-agent:"+newestTag || newest.CanonicalReference != "stgregistry.suse.com/rancher/rancher:"+newestTag || newest.OSSRevision != shaNewest {
		t.Fatalf("newest Prime provenance hints = %#v", newest)
	}
	statuses := map[string]string{}
	for _, tag := range group.Tags {
		statuses[tag.Name] = tag.PairStatus
	}
	if statuses[missingTag] != "missing" || statuses[invalidTag] != "invalid" {
		t.Fatalf("Prime diagnostics by tag = %#v", statuses)
	}

	tagListRequests.Store(0)
	exact, err := service.Search(context.Background(), SearchRequest{
		Registry:   "all",
		Repository: "rancher/rancher",
		Query:      olderTag,
		Limit:      200,
	})
	if err != nil {
		t.Fatalf("verify exact immutable Prime tag: %v", err)
	}
	if tagListRequests.Load() != 0 {
		t.Fatalf("exact immutable Prime lookup made %d tag-list requests", tagListRequests.Load())
	}
	if len(exact.Groups) != 1 || len(exact.Groups[0].Tags) != 1 || exact.Groups[0].Tags[0].PairStatus != "verified" || !exact.Groups[0].Tags[0].PairComplete {
		t.Fatalf("exact immutable Prime verification = %#v", exact.Groups)
	}

	verifiedOnly, err := service.Search(context.Background(), SearchRequest{
		Registry:   "all",
		Repository: "rancher/rancher",
		Query:      "v2.15.1-head",
		PairStatus: "verified",
		Limit:      200,
	})
	if err != nil {
		t.Fatalf("filter verified Prime pairs: %v", err)
	}
	if len(verifiedOnly.Groups) != 1 || verifiedOnly.Groups[0].Matched != 2 || len(verifiedOnly.Groups[0].Tags) != 2 {
		t.Fatalf("verified Prime pair filter = %#v", verifiedOnly.Groups)
	}
	for _, tag := range verifiedOnly.Groups[0].Tags {
		if tag.PairStatus != "verified" || !tag.PairComplete {
			t.Fatalf("non-verified tag leaked through pairStatus filter: %#v", tag)
		}
	}

	failingService := newImageLookupRewritingTestService(t, registryServer, "/v2/rancher/rancher-agent/manifests/"+olderTag)
	if _, err := failingService.Search(context.Background(), SearchRequest{
		Registry:   "all",
		Repository: "rancher/rancher",
		Query:      olderTag,
		Limit:      200,
	}); err == nil || !strings.Contains(err.Error(), "could not safely verify Prime head image pairs") {
		t.Fatalf("pair lookup error did not fail closed: %v", err)
	}
}

func TestImageLookupRecentDaysFiltersVerifiedPrimePairsByCompletionEvidence(t *testing.T) {
	registryServer := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	defer registryServer.Close()

	searchedAt := time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC)
	recentSHA := strings.Repeat("1", 40)
	oldSHA := strings.Repeat("2", 40)
	recentTag := "v2.15.1-" + recentSHA + "-head"
	oldTag := "v2.15.1-" + oldSHA + "-head"
	recentServerTime := searchedAt.Add(-5 * 24 * time.Hour)
	recentAgentTime := recentServerTime.Add(time.Hour)
	oldServerTime := searchedAt.Add(-20 * 24 * time.Hour)
	oldAgentTime := oldServerTime.Add(time.Hour)
	pushImageLookupPrimeFixture(t, registryServer, "rancher/rancher", recentTag, recentSHA, recentServerTime)
	pushImageLookupPrimeFixture(t, registryServer, "rancher/rancher-agent", recentTag, recentSHA, recentAgentTime)
	pushImageLookupPrimeFixture(t, registryServer, "rancher/rancher", oldTag, oldSHA, oldServerTime)
	pushImageLookupPrimeFixture(t, registryServer, "rancher/rancher-agent", oldTag, oldSHA, oldAgentTime)

	service := newImageLookupRewritingTestService(t, registryServer, "")
	service.now = func() time.Time { return searchedAt }
	result, err := service.Search(context.Background(), SearchRequest{
		Registry:   "all",
		Repository: "rancher/rancher",
		Query:      "v2.15.1-head",
		PairStatus: "verified",
		RecentDays: 10,
		Limit:      200,
	})
	if err != nil {
		t.Fatalf("recent Prime pair search: %v", err)
	}
	wantCutoff := searchedAt.Add(-10 * 24 * time.Hour)
	if result.RecentDays != 10 || result.RecentCutoff != imageLookupFormatTime(wantCutoff) || !result.SearchedAt.Equal(searchedAt) {
		t.Fatalf("recent response window = %#v", result)
	}
	if len(result.Groups) != 1 {
		t.Fatalf("recent Prime groups = %d, want 1", len(result.Groups))
	}
	group := result.Groups[0]
	if group.Matched != 1 || len(group.Tags) != 1 || group.RecentExcludedCount != 1 || group.UnknownTimestampCount != 0 || group.VerifiedPrimeHeadCount != 1 {
		t.Fatalf("recent Prime accounting = %#v", group)
	}
	if got := group.Tags[0]; got.Name != recentTag || got.PairCompletedAt != imageLookupFormatTime(recentAgentTime) || got.CreatedAt != imageLookupFormatTime(recentServerTime) {
		t.Fatalf("recent Prime result = %#v", got)
	}
}

func TestImageLookupExactArchitecturePrimeHeadVerifiesFullTagPair(t *testing.T) {
	var tagListRequests atomic.Int32
	registryHandler := registry.New(registry.Logger(log.New(io.Discard, "", 0)))
	registryServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "/tags/list") {
			tagListRequests.Add(1)
		}
		registryHandler.ServeHTTP(response, request)
	}))
	defer registryServer.Close()

	sha := strings.Repeat("9", 40)
	tag := "v2.15.1-" + sha + "-head-arm64"
	serverTime := time.Date(2026, time.August, 24, 9, 0, 0, 0, time.UTC)
	agentTime := serverTime.Add(30 * time.Minute)
	pushImageLookupPrimeIndexFixture(t, registryServer, "rancher/rancher", tag, sha, serverTime)
	pushImageLookupPrimeIndexFixture(t, registryServer, "rancher/rancher-agent", tag, sha, agentTime)

	service := newImageLookupRewritingTestService(t, registryServer, "")
	result, err := service.Search(context.Background(), SearchRequest{
		Registry:   "all",
		Repository: "rancher/rancher",
		Query:      tag,
		Limit:      200,
	})
	if err != nil {
		t.Fatalf("verify exact architecture-suffixed Prime pair: %v", err)
	}
	if tagListRequests.Load() != 0 {
		t.Fatalf("exact architecture Prime lookup made %d tag-list requests", tagListRequests.Load())
	}
	if len(result.Groups) != 1 || len(result.Groups[0].Tags) != 1 {
		t.Fatalf("exact architecture Prime result = %#v", result.Groups)
	}
	got := result.Groups[0].Tags[0]
	wantServerCanonical := "stgregistry.suse.com/rancher/rancher:" + tag
	if !got.IsPrimeHead || got.HeadKind != "immutable" || got.Architecture != "arm64" || got.BaseTag != "v2.15.1-"+sha+"-head" || got.Commit != sha || got.PairStatus != "verified" || !got.PairComplete || !got.ProvenanceValid || got.PairCompletedAt != imageLookupFormatTime(agentTime) || got.CanonicalReference != wantServerCanonical || got.OSSRevision != sha {
		t.Fatalf("verified architecture-suffixed Prime metadata = %#v", got)
	}
	if got.CompanionReference != "stgregistry.suse.com/rancher/rancher-agent:"+tag {
		t.Fatalf("architecture-suffixed companion reference = %q", got.CompanionReference)
	}
	inspection, err := service.Inspect(context.Background(), InspectRequest{
		Reference:       "stgregistry.suse.com/rancher/rancher:" + tag,
		Platform:        "linux/arm64",
		SkipTagMetadata: true,
	})
	if err != nil {
		t.Fatalf("inspect architecture-suffixed Prime fixture: %v", err)
	}
	if inspection.Config.Architecture != "arm64" {
		t.Fatalf("architecture-suffixed fixture config architecture = %q, want arm64", inspection.Config.Architecture)
	}

	if err := imageLookupValidateExactPrimeHeadPair(tag,
		Provenance{CanonicalReference: "stgregistry.suse.com/rancher/rancher:" + got.BaseTag},
		Provenance{CanonicalReference: "stgregistry.suse.com/rancher/rancher-agent:" + tag},
	); err == nil || !strings.Contains(err.Error(), "mismatched canonical tags") {
		t.Fatalf("architecture-suffixed pair accepted an unsuffixed canonical server label: %v", err)
	}

	agentOnlySHA := strings.Repeat("7", 40)
	agentOnlyTag := "v2.15.1-" + agentOnlySHA + "-head-arm64"
	pushImageLookupPrimeFixture(t, registryServer, "rancher/rancher-agent", agentOnlyTag, agentOnlySHA, agentTime.Add(time.Hour))
	tagListRequests.Store(0)
	agentOnly, err := service.Search(context.Background(), SearchRequest{
		Registry:   "all",
		Repository: "all",
		Query:      agentOnlyTag,
		Limit:      200,
	})
	if err != nil {
		t.Fatalf("diagnose agent-only exact Prime tag: %v", err)
	}
	if tagListRequests.Load() != 0 {
		t.Fatalf("agent-only exact Prime lookup made %d tag-list requests", tagListRequests.Load())
	}
	foundMissingAgent := false
	for _, group := range agentOnly.Groups {
		if group.ImageRole == "agent" && len(group.Tags) == 1 {
			foundMissingAgent = group.Tags[0].Name == agentOnlyTag && group.Tags[0].PairStatus == "missing" && strings.Contains(group.Tags[0].PairError, "server image was not found")
		}
	}
	if !foundMissingAgent {
		t.Fatalf("agent-only exact Prime candidate was not diagnosed missing: %#v", agentOnly.Groups)
	}
}

func TestImageLookupPrimePairVerificationRejectsTruncatedCandidateSet(t *testing.T) {
	service := &Service{}
	response := SearchResponse{Groups: []SearchGroup{{
		Registry:   "stgregistry.suse.com",
		Repository: "rancher/rancher",
		Reference:  "stgregistry.suse.com/rancher/rancher",
		ImageRole:  "server",
		Matched:    imageLookupMaxResultLimit + 1,
		Truncated:  true,
	}}}
	err := service.verifyPrimeHeadPairs(context.Background(), &response, searchOptions{
		verifyPrimePairs: true,
		primeVersion:     "2.15.1",
	})
	if err == nil || !strings.Contains(err.Error(), "verification is incomplete") || !strings.Contains(err.Error(), "narrow") {
		t.Fatalf("truncated Prime candidates did not fail closed: %v", err)
	}
}

func TestImageLookupPrimePairVerificationRequiresCompleteAuthority(t *testing.T) {
	sha := strings.Repeat("8", 40)
	tagName := "v2.15.1-" + sha + "-head"
	partialTag := imageLookupClassifyTag("stgregistry.suse.com", "rancher/rancher", tagName)
	service := &Service{}

	response := SearchResponse{Groups: []SearchGroup{
		{
			Registry:   "stgregistry.suse.com",
			Repository: "rancher/rancher",
			Reference:  "stgregistry.suse.com/rancher/rancher",
			ImageRole:  "server",
			Error:      "tag listing failed after a partial page",
			Tags:       []Tag{partialTag},
		},
		{
			Registry:   "stgregistry.suse.com",
			Repository: "rancher/rancher-agent",
			Reference:  "stgregistry.suse.com/rancher/rancher-agent",
			ImageRole:  "agent",
			Tags:       []Tag{},
		},
	}}
	if err := service.verifyPrimeHeadPairs(context.Background(), &response, searchOptions{primeVersion: "2.15.1", pairStatus: "all"}); err != nil {
		t.Fatalf("complete agent authority should permit an empty verified candidate set: %v", err)
	}
	if response.Groups[0].Tags[0].PairStatus != "unverified" {
		t.Fatalf("candidate from partial-error server group was inspected: %#v", response.Groups[0].Tags[0])
	}

	response.Groups[1].Truncated = true
	err := service.verifyPrimeHeadPairs(context.Background(), &response, searchOptions{primeVersion: "2.15.1", pairStatus: "all"})
	if err == nil || !strings.Contains(err.Error(), "verification is incomplete") {
		t.Fatalf("partial-error groups did not fail closed without a complete authority: %v", err)
	}
}
