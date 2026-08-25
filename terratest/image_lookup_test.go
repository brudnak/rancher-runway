package test

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

func TestImageLookupParseReferenceNormalizesAndRejectsUnsafeInput(t *testing.T) {
	service := &imageLookupService{}
	digest := "sha256:" + strings.Repeat("a", 64)
	tests := []struct {
		name           string
		input          string
		wantCanonical  string
		wantRegistry   string
		wantRepository string
		wantTag        string
		wantDigest     string
	}{
		{
			name:           "unqualified Docker Hub repository",
			input:          "rancher/rancher:v2.16.0",
			wantCanonical:  "docker.io/rancher/rancher:v2.16.0",
			wantRegistry:   "docker.io",
			wantRepository: "rancher/rancher",
			wantTag:        "v2.16.0",
		},
		{
			name:           "single component Docker Hub image",
			input:          "ubuntu:24.04",
			wantCanonical:  "docker.io/library/ubuntu:24.04",
			wantRegistry:   "docker.io",
			wantRepository: "library/ubuntu",
			wantTag:        "24.04",
		},
		{
			name:           "staging reference with transport scheme",
			input:          "docker://stgregistry.suse.com/rancher/rancher:v2.16.0-rcs-0844.1",
			wantCanonical:  "stgregistry.suse.com/rancher/rancher:v2.16.0-rcs-0844.1",
			wantRegistry:   "stgregistry.suse.com",
			wantRepository: "rancher/rancher",
			wantTag:        "v2.16.0-rcs-0844.1",
		},
		{
			name:           "digest reference",
			input:          "registry.suse.com/rancher/rancher@" + digest,
			wantCanonical:  "registry.suse.com/rancher/rancher@" + digest,
			wantRegistry:   "registry.suse.com",
			wantRepository: "rancher/rancher",
			wantDigest:     digest,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := service.parseReference(testCase.input, true)
			if err != nil {
				t.Fatalf("parseReference(%q): %v", testCase.input, err)
			}
			if got.canonical != testCase.wantCanonical || got.registry != testCase.wantRegistry || got.repository != testCase.wantRepository || got.tag != testCase.wantTag || got.digest != testCase.wantDigest {
				t.Fatalf("parseReference(%q) = %#v, want canonical=%q registry=%q repository=%q tag=%q digest=%q", testCase.input, got, testCase.wantCanonical, testCase.wantRegistry, testCase.wantRepository, testCase.wantTag, testCase.wantDigest)
			}
		})
	}

	invalid := []string{
		"",
		"rancher/rancher",
		"http://registry.example.com/rancher/rancher:v2.16.0",
		"registry.example.com:/rancher/rancher:v2.16.0",
		"registry.example.com/rancher/../admin:v2.16.0",
		"registry.example.com/rancher/rancher:v2.16.0?debug=true",
		"registry.example.com/rancher/rancher:v2.16.0 other",
		"user:password@registry.example.com/rancher/rancher:v2.16.0",
		strings.Repeat("a", 1025),
	}
	for _, input := range invalid {
		t.Run("reject_"+strings.ReplaceAll(input, "/", "_"), func(t *testing.T) {
			if _, err := service.parseReference(input, true); err == nil {
				t.Fatalf("parseReference(%q) unexpectedly succeeded", input)
			}
		})
	}
}

func TestImageLookupSearchTargetsAndLimitValidation(t *testing.T) {
	service := &imageLookupService{}

	targets, query, limit, err := service.searchTargets(imageLookupSearchRequest{
		Registry:   "stgregistry.suse.com",
		Repository: "rancher/rancher",
		Query:      "head",
		Limit:      200,
	})
	if err != nil {
		t.Fatalf("searchTargets accepted UI limit: %v", err)
	}
	if limit != 200 || query != "head" || len(targets) != 1 || targets[0] != (imageLookupTarget{registry: "stgregistry.suse.com", repository: "rancher/rancher"}) {
		t.Fatalf("unexpected explicit search target: targets=%#v query=%q limit=%d", targets, query, limit)
	}

	targets, query, _, err = service.searchTargets(imageLookupSearchRequest{
		Registry:   "all",
		Repository: "stgregistry.suse.com/rancher/rancher:v2.16.0-rcs-0844.1",
	})
	if err != nil {
		t.Fatalf("searchTargets full reference: %v", err)
	}
	if len(targets) != 1 || targets[0].registry != "stgregistry.suse.com" || targets[0].repository != "rancher/rancher" || query != "v2.16.0-rcs-0844.1" {
		t.Fatalf("full reference was not narrowed correctly: targets=%#v query=%q", targets, query)
	}

	targets, _, limit, err = service.searchTargets(imageLookupSearchRequest{Registry: "all", Repository: "all"})
	if err != nil {
		t.Fatalf("searchTargets all known sources: %v", err)
	}
	if want := len(imageLookupKnownRegistries) * len(imageLookupKnownRepositories); len(targets) != want {
		t.Fatalf("all-source target count = %d, want %d", len(targets), want)
	}
	if limit != imageLookupDefaultResultLimit {
		t.Fatalf("default limit = %d, want %d", limit, imageLookupDefaultResultLimit)
	}

	for _, request := range []imageLookupSearchRequest{
		{Registry: "docker.io", Repository: "rancher/rancher", Limit: 201},
		{Registry: "docker.io", Repository: "rancher/rancher", Limit: -1},
		{Registry: "docker.io", Repository: "rancher/rancher", Query: "two words"},
	} {
		if _, _, _, err := service.searchTargets(request); err == nil {
			t.Fatalf("searchTargets(%#v) unexpectedly succeeded", request)
		}
	}
}

func TestImageLookupTagClassificationFilteringAndNaturalOrder(t *testing.T) {
	channelTests := map[string]string{
		"head":                         "head",
		"v2.17-head-amd64":             "head",
		"v2.17.0-devel-123":            "devel",
		"v2.17.0-alpha1":               "alpha",
		"v2.17.0-beta2":                "devel",
		"v2.16.0-rcs-0844.1":           "rcs",
		"v2.16.0-rc1":                  "rc",
		"v2.16.0":                      "stable",
		"v2.16.0-rcs-0844.1-arm64":     "rcs",
		"v2.16.0-rcs-0844.1-linux-386": "rcs",
	}
	for tag, want := range channelTests {
		if got := imageLookupTagChannel(tag); got != want {
			t.Errorf("imageLookupTagChannel(%q) = %q, want %q", tag, got, want)
		}
	}

	if !imageLookupTagMatches("v2.16.0-RCS-0844.1", "rcs", "rcs") {
		t.Fatal("quick rcs filter did not match the rcs channel")
	}
	if imageLookupTagMatches("v2.16.0-rcs-0844.1", "rcs", "rc") {
		t.Fatal("quick rc filter must not include security rcs tags")
	}
	if !imageLookupTagMatches("v2.16.0-rcs-0844.1", "rcs", "0844") {
		t.Fatal("substring filter did not match an exact build fragment")
	}
	if !imageLookupTagMatches("v2.17.0-alpha10", "alpha", "alpha") {
		t.Fatal("quick alpha filter did not match the alpha channel")
	}
	if !imageLookupTagMatches("v2.17.0-alpha10", "alpha", "devel") {
		t.Fatal("broad devel filter did not include the alpha channel")
	}
	if imageLookupTagMatches("v2.17.0-devel-10", "devel", "alpha") {
		t.Fatal("quick alpha filter must not include other devel builds")
	}

	architectureTests := []struct {
		tag              string
		wantArchitecture string
		wantBase         string
	}{
		{tag: "v2.16.0-rcs-0844.1-amd64", wantArchitecture: "amd64", wantBase: "v2.16.0-rcs-0844.1"},
		{tag: "v2.16.0-rcs-0844.1_arm64", wantArchitecture: "arm64", wantBase: "v2.16.0-rcs-0844.1"},
		{tag: "v2.16.0-linux-amd64", wantArchitecture: "amd64", wantBase: "v2.16.0"},
		{tag: "v2.16.0", wantArchitecture: "multi", wantBase: "v2.16.0"},
	}
	for _, testCase := range architectureTests {
		architecture, base := imageLookupTagArchitecture(testCase.tag)
		if architecture != testCase.wantArchitecture || base != testCase.wantBase {
			t.Errorf("imageLookupTagArchitecture(%q) = (%q, %q), want (%q, %q)", testCase.tag, architecture, base, testCase.wantArchitecture, testCase.wantBase)
		}
	}

	for tag, want := range map[string]bool{
		"sha256-deadbeef.sig":       true,
		"v2.16.0.sbom":              true,
		"v2.16.0.attestation":       true,
		"cosign-signature-deadbeef": true,
		"v2.16.0-rcs-0844.1":        false,
	} {
		if got := imageLookupArtifactTag(tag); got != want {
			t.Errorf("imageLookupArtifactTag(%q) = %t, want %t", tag, got, want)
		}
	}

	tags := []string{"v2.9.0", "v2.10.0", "v2.10.0-rc2", "v2.10.0-rc10"}
	sort.Slice(tags, func(i, j int) bool {
		return imageLookupNaturalCompare(tags[i], tags[j]) > 0
	})
	wantOrder := []string{"v2.10.0-rc10", "v2.10.0-rc2", "v2.10.0", "v2.9.0"}
	if !slices.Equal(tags, wantOrder) {
		t.Fatalf("natural descending order = %v, want %v", tags, wantOrder)
	}
	alphaTags := []string{"v2.17.0-alpha2", "v2.17.0-alpha10", "v2.17.0-alpha1"}
	sort.Slice(alphaTags, func(i, j int) bool {
		return imageLookupNaturalCompare(alphaTags[i], alphaTags[j]) > 0
	})
	wantAlphaOrder := []string{"v2.17.0-alpha10", "v2.17.0-alpha2", "v2.17.0-alpha1"}
	if !slices.Equal(alphaTags, wantAlphaOrder) {
		t.Fatalf("natural alpha order = %v, want %v", alphaTags, wantAlphaOrder)
	}

	for _, query := range []string{"v2.16.0", "2.16.0", "v2.16.0-rc1", "v2.16.0-rcs-0844.1", "v2.16.0-amd64"} {
		if !imageLookupFullVersionTag(query) {
			t.Errorf("full Rancher version tag %q was not recognized", query)
		}
	}
	for _, query := range []string{"", "0844", "deadbeef", "rcs", "v2.16", "release candidate"} {
		if imageLookupFullVersionTag(query) {
			t.Errorf("search fragment %q was misclassified as a full version tag", query)
		}
	}
}

func TestImageLookupPrimeHeadClassificationAndFiltering(t *testing.T) {
	sha := strings.Repeat("a", 40)
	tests := []struct {
		name              string
		repository        string
		tag               string
		wantRole          string
		wantPrime         bool
		wantKind          string
		wantMutable       bool
		wantVersion       string
		wantLine          string
		wantCommit        string
		wantCompanionRepo string
	}{
		{
			name:              "moving patch-qualified server selector",
			repository:        "rancher/rancher",
			tag:               "v2.15.1-head",
			wantRole:          "server",
			wantPrime:         true,
			wantKind:          "moving",
			wantMutable:       true,
			wantVersion:       "2.15.1",
			wantLine:          "2.15",
			wantCompanionRepo: "stgregistry.suse.com/rancher/rancher-agent:v2.15.1-head",
		},
		{
			name:              "immutable patch-qualified agent",
			repository:        "rancher/rancher-agent",
			tag:               "v2.15.1-" + sha + "-head-arm64",
			wantRole:          "agent",
			wantPrime:         true,
			wantKind:          "immutable",
			wantVersion:       "2.15.1",
			wantLine:          "2.15",
			wantCommit:        sha,
			wantCompanionRepo: "stgregistry.suse.com/rancher/rancher:v2.15.1-" + sha + "-head-arm64",
		},
		{
			name:              "minor head is not syntactically Prime",
			repository:        "rancher/rancher",
			tag:               "v2.15-head",
			wantRole:          "server",
			wantPrime:         false,
			wantVersion:       "",
			wantCompanionRepo: "stgregistry.suse.com/rancher/rancher-agent:v2.15-head",
		},
		{
			name:        "webhook is an independent build",
			repository:  "rancher/rancher-webhook",
			tag:         "v2.15.1-" + sha + "-head",
			wantRole:    "webhook",
			wantPrime:   false,
			wantVersion: "",
		},
		{
			name:              "stable tag gets normalized version metadata",
			repository:        "rancher/rancher",
			tag:               "v2.15.1-amd64",
			wantRole:          "server",
			wantPrime:         false,
			wantVersion:       "2.15.1",
			wantLine:          "2.15",
			wantCompanionRepo: "stgregistry.suse.com/rancher/rancher-agent:v2.15.1-amd64",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			tag := imageLookupClassifyTag("stgregistry.suse.com", testCase.repository, testCase.tag)
			if tag.ImageRole != testCase.wantRole || tag.IsPrimeHead != testCase.wantPrime || tag.HeadKind != testCase.wantKind || tag.Mutable != testCase.wantMutable || tag.Version != testCase.wantVersion || tag.VersionLine != testCase.wantLine || tag.Commit != testCase.wantCommit || tag.CompanionReference != testCase.wantCompanionRepo {
				t.Fatalf("classification = %#v", tag)
			}
			if tag.IsPrimeHead && tag.PairStatus != "unverified" {
				t.Fatalf("uninspected Prime tag pair status = %q, want unverified", tag.PairStatus)
			}
		})
	}

	immutable := imageLookupClassifyTag("stgregistry.suse.com", "rancher/rancher", "v2.15.1-"+sha+"-head")
	if !imageLookupTagMatchesOptions(immutable, imageLookupSearchOptions{query: "v2.15.1-head", primeHead: "only", headKind: "all", channel: "all", architecture: "all"}) {
		t.Fatal("moving patch selector did not match its immutable candidate")
	}
	if imageLookupTagMatchesOptions(immutable, imageLookupSearchOptions{query: "v2.15.2-head", primeHead: "only", headKind: "all", channel: "all", architecture: "all"}) {
		t.Fatal("moving patch selector crossed patch versions")
	}
	if !imageLookupTagMatchesOptions(immutable, imageLookupSearchOptions{primeHead: "only", headKind: "immutable", versionLine: "2.15.1", commit: sha[:12], channel: "all", architecture: "all"}) {
		t.Fatal("normalized Prime metadata filters rejected an exact candidate")
	}
}

func TestImageLookupPrimeTargetNarrowingAndValidation(t *testing.T) {
	service := &imageLookupService{}
	targets, options, err := service.searchParameters(imageLookupSearchRequest{
		Registry:   "all",
		Repository: "all",
		Query:      "primehead",
	})
	if err != nil {
		t.Fatalf("primehead parameters: %v", err)
	}
	wantTargets := []imageLookupTarget{
		{registry: "stgregistry.suse.com", repository: "rancher/rancher"},
		{registry: "stgregistry.suse.com", repository: "rancher/rancher-agent"},
	}
	if !slices.Equal(targets, wantTargets) || options.query != "prime-head" || options.primeHead != "only" || options.verifyPrimePairs || !options.fullScan {
		t.Fatalf("primehead parameters = targets %#v options %#v", targets, options)
	}

	sha := strings.Repeat("b", 40)
	targets, options, err = service.searchParameters(imageLookupSearchRequest{
		Registry:   "all",
		Repository: "all",
		Query:      "v2.15.1-head",
	})
	if err != nil {
		t.Fatalf("patch selector parameters: %v", err)
	}
	if !slices.Equal(targets, wantTargets) || !options.verifyPrimePairs || options.primeVersion != "2.15.1" || options.sortBy != "pair-completed" || !options.fullScan {
		t.Fatalf("patch selector parameters = targets %#v options %#v", targets, options)
	}

	_, options, err = service.searchParameters(imageLookupSearchRequest{
		Registry:   "all",
		Repository: "rancher/rancher",
		Query:      "v2.15.1",
		PrimeHead:  "only",
	})
	if err != nil {
		t.Fatalf("Prime-only bare patch parameters: %v", err)
	}
	if !options.verifyPrimePairs || options.primeVersion != "2.15.1" || options.sortBy != "pair-completed" {
		t.Fatalf("Prime-only bare patch did not enable verified resolution: %#v", options)
	}

	_, _, err = service.searchParameters(imageLookupSearchRequest{
		Registry:   "all",
		Repository: "all",
		Query:      "docker.io/rancher/rancher:v2.15.1-" + sha + "-head",
	})
	if err == nil || !strings.Contains(err.Error(), "only in stgregistry.suse.com") {
		t.Fatalf("explicit non-staging Prime reference was not rejected: %v", err)
	}

	architectureTag := "v2.15.1-" + sha + "-head-arm64"
	targets, options, err = service.searchParameters(imageLookupSearchRequest{
		Registry:   "all",
		Repository: "all",
		Query:      "stgregistry.suse.com/rancher/rancher:" + architectureTag,
	})
	if err != nil {
		t.Fatalf("architecture-suffixed Prime parameters: %v", err)
	}
	if len(targets) != 1 || targets[0] != (imageLookupTarget{registry: "stgregistry.suse.com", repository: "rancher/rancher"}) || !options.exactLookup || !options.verifyPrimePairs || options.primeVersion != "2.15.1" {
		t.Fatalf("architecture-suffixed Prime parameters = targets %#v options %#v", targets, options)
	}

	defaultTargets, _, err := service.searchParameters(imageLookupSearchRequest{Registry: "all", Repository: "rancher/rancher"})
	if err != nil {
		t.Fatalf("default target order: %v", err)
	}
	wantRegistryOrder := []string{"stgregistry.suse.com", "registry.rancher.com", "registry.suse.com", "docker.io"}
	gotRegistryOrder := make([]string, len(defaultTargets))
	for index, target := range defaultTargets {
		gotRegistryOrder[index] = target.registry
	}
	if !slices.Equal(gotRegistryOrder, wantRegistryOrder) {
		t.Fatalf("known registry order = %v, want %v", gotRegistryOrder, wantRegistryOrder)
	}

	invalid := []imageLookupSearchRequest{
		{Registry: "all", Repository: "all", PrimeHead: "sometimes"},
		{Registry: "all", Repository: "all", PrimeHead: "exclude", HeadKind: "immutable"},
		{Registry: "all", Repository: "all", VersionLine: "2.15.x"},
		{Registry: "all", Repository: "all", Commit: "abc123"},
		{Registry: "all", Repository: "all", SortBy: "pair-completed"},
		{Registry: "docker.io", Repository: "rancher/rancher", Query: "v2.15.1-head"},
		{Registry: "stgregistry.suse.com", Repository: "example/rancher", Query: "v2.15.1-" + sha + "-head"},
		{Registry: "stgregistry.suse.com", Repository: "rancher/rancher-webhook", PrimeHead: "only"},
	}
	for _, request := range invalid {
		if _, _, err := service.searchParameters(request); err == nil {
			t.Fatalf("invalid enriched request %#v unexpectedly succeeded", request)
		}
	}
}

func TestImageLookupInspectPrimeHeadProvenanceClassification(t *testing.T) {
	service := &imageLookupService{}
	sha := strings.Repeat("c", 40)
	canonicalTag := "v2.15.1-" + sha + "-head"
	labels := map[string]string{
		imageLookupSourceLabel:             "https://github.com/rancher/rancher-prime.git",
		imageLookupRevisionLabel:           strings.Repeat("d", 40),
		imageLookupOSSRevisionLabel:        sha,
		imageLookupCanonicalReferenceLabel: "stgregistry.suse.com/rancher/rancher:" + canonicalTag,
	}

	parsed, err := service.parseReference("stgregistry.suse.com/rancher/rancher:"+canonicalTag, true)
	if err != nil {
		t.Fatalf("parse immutable Prime reference: %v", err)
	}
	details := imageLookupInspectPrimeHead(parsed, labels)
	if !details.IsPrimeHead || details.HeadKind != "immutable" || details.Mutable || !details.PrimeSource || !details.CanonicalMatchesRequest || !details.CommitMatchesOSS || !details.Consistent || len(details.Issues) != 0 {
		t.Fatalf("consistent immutable Prime inspection = %#v", details)
	}

	agentLabels := map[string]string{
		imageLookupCanonicalReferenceLabel: "stgregistry.suse.com/rancher/rancher-agent:" + canonicalTag,
	}
	parsed, err = service.parseReference("stgregistry.suse.com/rancher/rancher-agent:"+canonicalTag, true)
	if err != nil {
		t.Fatalf("parse immutable Prime agent reference: %v", err)
	}
	details = imageLookupInspectPrimeHead(parsed, agentLabels)
	if !details.IsPrimeHead || details.ImageRole != "agent" || !details.CanonicalMatchesRequest || !details.Consistent || len(details.Issues) != 0 {
		t.Fatalf("consistent immutable Prime agent inspection = %#v", details)
	}

	parsed, err = service.parseReference("stgregistry.suse.com/rancher/rancher:v2.15-head", true)
	if err != nil {
		t.Fatalf("parse moving minor reference: %v", err)
	}
	details = imageLookupInspectPrimeHead(parsed, labels)
	if !details.IsPrimeHead || details.HeadKind != "moving" || !details.Mutable || details.Version != "2.15.1" || details.Commit != sha || !details.Consistent {
		t.Fatalf("provenance-proven minor Prime alias = %#v", details)
	}

	badLabels := map[string]string{}
	for key, value := range labels {
		badLabels[key] = value
	}
	badLabels[imageLookupOSSRevisionLabel] = strings.Repeat("e", 40)
	details = imageLookupInspectPrimeHead(parsed, badLabels)
	if details.Consistent || details.CommitMatchesOSS || len(details.Issues) == 0 {
		t.Fatalf("mismatched OSS revision was accepted: %#v", details)
	}

	parsed, err = service.parseReference("docker.io/rancher/rancher:head", true)
	if err != nil {
		t.Fatalf("parse bare head: %v", err)
	}
	details = imageLookupInspectPrimeHead(parsed, labels)
	if details.IsPrimeHead {
		t.Fatalf("bare head was inferred as Prime: %#v", details)
	}
}

func TestImageLookupExactTagReferenceEligibility(t *testing.T) {
	repository, err := name.NewRepository("registry.example.com/rancher/rancher", name.StrictValidation)
	if err != nil {
		t.Fatalf("parse exact-tag test repository: %v", err)
	}
	for _, query := range []string{"v2.16.0-rcs-0844.1", "0844", "deadbeef"} {
		if tag, ok := imageLookupExactTagReference(repository, query, false); !ok || tag.TagStr() != query {
			t.Errorf("valid exact-tag candidate %q produced tag %#v, eligible=%t", query, tag, ok)
		}
	}
	for _, query := range []string{"", "head", "devel", "alpha", "rcs", "rc", "stable", "all", "bad/tag"} {
		if tag, ok := imageLookupExactTagReference(repository, query, false); ok {
			t.Errorf("non-exact query %q unexpectedly produced tag %#v", query, tag)
		}
	}
}

func TestImageLookupSearchDigestUsesManifestFastPath(t *testing.T) {
	var manifestRequests atomic.Int32
	var tagListRequests atomic.Int32
	registryHandler := registry.New(registry.Logger(log.New(io.Discard, "", 0)))
	registryServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if strings.Contains(request.URL.Path, "/manifests/") {
			manifestRequests.Add(1)
		}
		if strings.HasSuffix(request.URL.Path, "/tags/list") {
			tagListRequests.Add(1)
		}
		registryHandler.ServeHTTP(response, request)
	}))
	defer registryServer.Close()

	service := newImageLookupTestService(t, registryServer)
	image := newImageLookupFixtureImage(t, "amd64", "", time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC), map[string]string{
		"README.txt": "digest fixture",
	})
	host := imageLookupTestServerHost(t, registryServer)
	reference, err := name.NewTag(host+"/rancher/rancher:digest-fixture", name.StrictValidation, name.Insecure)
	if err != nil {
		t.Fatalf("parse digest fixture tag: %v", err)
	}
	if err := remote.Write(reference, image, remote.WithTransport(registryServer.Client().Transport), remote.WithAuth(authn.Anonymous)); err != nil {
		t.Fatalf("push digest fixture: %v", err)
	}
	digest, err := image.Digest()
	if err != nil {
		t.Fatalf("digest fixture digest: %v", err)
	}
	digestText := digest.String()

	assertDigestResult := func(t *testing.T, request imageLookupSearchRequest) {
		t.Helper()
		manifestRequests.Store(0)
		tagListRequests.Store(0)
		result, searchErr := service.Search(context.Background(), request)
		if searchErr != nil {
			t.Fatalf("digest search: %v", searchErr)
		}
		if manifestRequests.Load() == 0 || tagListRequests.Load() != 0 {
			t.Fatalf("digest search requests: manifests=%d tag-lists=%d", manifestRequests.Load(), tagListRequests.Load())
		}
		if len(result.Groups) != 1 || result.Groups[0].Scanned != 1 || result.Groups[0].Matched != 1 || len(result.Groups[0].Tags) != 1 {
			t.Fatalf("digest search result = %#v", result.Groups)
		}
		got := result.Groups[0].Tags[0]
		wantReference := host + "/rancher/rancher@" + digestText
		if got.Name != digestText || got.Reference != wantReference || got.Digest != digestText || got.Channel != "digest" || got.Architecture != "unknown" || got.ImageRole != "server" || got.CompanionReference != "" {
			t.Fatalf("digest metadata = %#v, want reference %q", got, wantReference)
		}
	}

	assertDigestResult(t, imageLookupSearchRequest{
		Registry:   host,
		Repository: "rancher/rancher",
		Query:      digestText,
	})
	assertDigestResult(t, imageLookupSearchRequest{
		Registry:   "all",
		Repository: "all",
		Query:      host + "/rancher/rancher@" + digestText,
	})

	manifestRequests.Store(0)
	tagListRequests.Store(0)
	missingDigest := "sha256:" + strings.Repeat("0", 64)
	missing, err := service.Search(context.Background(), imageLookupSearchRequest{
		Registry:   host,
		Repository: "rancher/rancher",
		Query:      missingDigest,
	})
	if err != nil {
		t.Fatalf("missing digest search: %v", err)
	}
	if manifestRequests.Load() == 0 || tagListRequests.Load() != 0 || len(missing.Groups) != 1 || missing.Groups[0].Scanned != 1 || missing.Groups[0].Matched != 0 || len(missing.Groups[0].Tags) != 0 {
		t.Fatalf("missing digest fast path = %#v, manifests=%d tag-lists=%d", missing.Groups, manifestRequests.Load(), tagListRequests.Load())
	}
}

func TestImageLookupSearchPaginatesRegistryAndFiltersArtifacts(t *testing.T) {
	var tagRequests atomic.Int32
	registryServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v2/":
			response.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
			response.WriteHeader(http.StatusOK)
		case "/v2/rancher/rancher/tags/list":
			tagRequests.Add(1)
			response.Header().Set("Content-Type", "application/json")
			if request.URL.Query().Get("n") != "1000" {
				t.Errorf("registry page size = %q, want 1000", request.URL.Query().Get("n"))
			}
			if request.URL.Query().Get("last") == "" {
				response.Header().Set("Link", `</v2/rancher/rancher/tags/list?n=1000&last=v2.16.0-rcs-0844.1-amd64>; rel="next"`)
				_ = json.NewEncoder(response).Encode(map[string]any{
					"name": "rancher/rancher",
					"tags": []string{"v2.9.0", "v2.16.0-rcs-0844.1-amd64", "sha256-deadbeef.sig"},
				})
				return
			}
			_ = json.NewEncoder(response).Encode(map[string]any{
				"name": "rancher/rancher",
				"tags": []string{"v2.10.0", "v2.16.0-rcs-0844.1-arm64", "head"},
			})
		default:
			http.NotFound(response, request)
		}
	}))
	defer registryServer.Close()

	service := newImageLookupTestService(t, registryServer)
	result, err := service.Search(context.Background(), imageLookupSearchRequest{
		Registry:   imageLookupTestServerHost(t, registryServer),
		Repository: "rancher/rancher",
		Limit:      200,
	})
	if err != nil {
		t.Fatalf("Search local paginated registry: %v", err)
	}
	if tagRequests.Load() != 2 {
		t.Fatalf("tag page requests = %d, want 2", tagRequests.Load())
	}
	if len(result.Groups) != 1 {
		t.Fatalf("search groups = %d, want 1", len(result.Groups))
	}
	group := result.Groups[0]
	if group.Scanned != 6 || group.Matched != 5 || group.Truncated {
		t.Fatalf("unexpected page accounting: scanned=%d matched=%d truncated=%t error=%q", group.Scanned, group.Matched, group.Truncated, group.Error)
	}
	gotTags := make([]string, len(group.Tags))
	for index, tag := range group.Tags {
		gotTags[index] = tag.Name
		if imageLookupArtifactTag(tag.Name) {
			t.Fatalf("artifact tag %q leaked into default search", tag.Name)
		}
	}
	wantTags := []string{"v2.16.0-rcs-0844.1-arm64", "v2.16.0-rcs-0844.1-amd64", "v2.10.0", "v2.9.0", "head"}
	if !slices.Equal(gotTags, wantTags) {
		t.Fatalf("searched tags = %v, want %v", gotTags, wantTags)
	}
	if group.Tags[0].Architecture != "arm64" || group.Tags[0].BaseTag != "v2.16.0-rcs-0844.1" || group.Tags[0].Channel != "rcs" {
		t.Fatalf("unexpected tag metadata: %#v", group.Tags[0])
	}
}

func TestImageLookupBarePatchFilterScansThenSortsBeforeLimit(t *testing.T) {
	var manifestRequests atomic.Int32
	var tagRequests atomic.Int32
	registryServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if strings.Contains(request.URL.Path, "/manifests/") {
			manifestRequests.Add(1)
		}
		switch request.URL.Path {
		case "/v2/":
			response.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
			response.WriteHeader(http.StatusOK)
		case "/v2/rancher/rancher/tags/list":
			tagRequests.Add(1)
			response.Header().Set("Content-Type", "application/json")
			if request.URL.Query().Get("last") == "" {
				response.Header().Set("Link", `</v2/rancher/rancher/tags/list?n=1000&last=v2.15.1-beta1>; rel="next"`)
				_ = json.NewEncoder(response).Encode(map[string]any{
					"name": "rancher/rancher",
					"tags": []string{"v2.15.1-rc1", "v2.15.1-beta1"},
				})
				return
			}
			_ = json.NewEncoder(response).Encode(map[string]any{
				"name": "rancher/rancher",
				"tags": []string{"v2.15.1", "v2.15.1-alpha2"},
			})
		default:
			http.NotFound(response, request)
		}
	}))
	defer registryServer.Close()

	service := newImageLookupTestService(t, registryServer)
	result, err := service.Search(context.Background(), imageLookupSearchRequest{
		Registry:   imageLookupTestServerHost(t, registryServer),
		Repository: "rancher/rancher",
		Query:      "v2.15.1",
		Limit:      2,
		SortBy:     "tag",
		SortOrder:  "asc",
	})
	if err != nil {
		t.Fatalf("bare patch search: %v", err)
	}
	if manifestRequests.Load() != 0 {
		t.Fatalf("bare patch filter made %d exact manifest requests", manifestRequests.Load())
	}
	if tagRequests.Load() != 2 {
		t.Fatalf("bare patch filter tag pages = %d, want 2", tagRequests.Load())
	}
	group := result.Groups[0]
	if group.Scanned != 4 || group.Matched != 4 || !group.Truncated || len(group.Tags) != 2 {
		t.Fatalf("bare patch accounting = %#v", group)
	}
	got := []string{group.Tags[0].Name, group.Tags[1].Name}
	want := []string{"v2.15.1", "v2.15.1-alpha2"}
	if !slices.Equal(got, want) {
		t.Fatalf("globally sorted limited tags = %v, want %v", got, want)
	}
	if group.Tags[0].Version != "2.15.1" || group.Tags[0].VersionLine != "2.15" {
		t.Fatalf("normalized stable metadata = %#v", group.Tags[0])
	}
}

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
	result, err := service.Search(context.Background(), imageLookupSearchRequest{
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
	exact, err := service.Search(context.Background(), imageLookupSearchRequest{
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

	verifiedOnly, err := service.Search(context.Background(), imageLookupSearchRequest{
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
	if _, err := failingService.Search(context.Background(), imageLookupSearchRequest{
		Registry:   "all",
		Repository: "rancher/rancher",
		Query:      olderTag,
		Limit:      200,
	}); err == nil || !strings.Contains(err.Error(), "could not safely verify Prime head image pairs") {
		t.Fatalf("pair lookup error did not fail closed: %v", err)
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
	pushImageLookupPrimeFixture(t, registryServer, "rancher/rancher", tag, sha, serverTime)
	pushImageLookupPrimeFixture(t, registryServer, "rancher/rancher-agent", tag, sha, agentTime)

	service := newImageLookupRewritingTestService(t, registryServer, "")
	result, err := service.Search(context.Background(), imageLookupSearchRequest{
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
	inspection, err := service.Inspect(context.Background(), imageLookupInspectRequest{
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
		rancherImageProvenance{CanonicalReference: "stgregistry.suse.com/rancher/rancher:" + got.BaseTag},
		rancherImageProvenance{CanonicalReference: "stgregistry.suse.com/rancher/rancher-agent:" + tag},
	); err == nil || !strings.Contains(err.Error(), "mismatched canonical tags") {
		t.Fatalf("architecture-suffixed pair accepted an unsuffixed canonical server label: %v", err)
	}

	agentOnlySHA := strings.Repeat("7", 40)
	agentOnlyTag := "v2.15.1-" + agentOnlySHA + "-head-arm64"
	pushImageLookupPrimeFixture(t, registryServer, "rancher/rancher-agent", agentOnlyTag, agentOnlySHA, agentTime.Add(time.Hour))
	tagListRequests.Store(0)
	agentOnly, err := service.Search(context.Background(), imageLookupSearchRequest{
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
	service := &imageLookupService{}
	response := imageLookupSearchResponse{Groups: []imageLookupSearchGroup{{
		Registry:   "stgregistry.suse.com",
		Repository: "rancher/rancher",
		Reference:  "stgregistry.suse.com/rancher/rancher",
		ImageRole:  "server",
		Matched:    imageLookupMaxResultLimit + 1,
		Truncated:  true,
	}}}
	err := service.verifyPrimeHeadPairs(context.Background(), &response, imageLookupSearchOptions{
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
	service := &imageLookupService{}

	response := imageLookupSearchResponse{Groups: []imageLookupSearchGroup{
		{
			Registry:   "stgregistry.suse.com",
			Repository: "rancher/rancher",
			Reference:  "stgregistry.suse.com/rancher/rancher",
			ImageRole:  "server",
			Error:      "tag listing failed after a partial page",
			Tags:       []imageLookupTag{partialTag},
		},
		{
			Registry:   "stgregistry.suse.com",
			Repository: "rancher/rancher-agent",
			Reference:  "stgregistry.suse.com/rancher/rancher-agent",
			ImageRole:  "agent",
			Tags:       []imageLookupTag{},
		},
	}}
	if err := service.verifyPrimeHeadPairs(context.Background(), &response, imageLookupSearchOptions{primeVersion: "2.15.1", pairStatus: "all"}); err != nil {
		t.Fatalf("complete agent authority should permit an empty verified candidate set: %v", err)
	}
	if response.Groups[0].Tags[0].PairStatus != "unverified" {
		t.Fatalf("candidate from partial-error server group was inspected: %#v", response.Groups[0].Tags[0])
	}

	response.Groups[1].Truncated = true
	err := service.verifyPrimeHeadPairs(context.Background(), &response, imageLookupSearchOptions{primeVersion: "2.15.1", pairStatus: "all"})
	if err == nil || !strings.Contains(err.Error(), "verification is incomplete") {
		t.Fatalf("partial-error groups did not fail closed without a complete authority: %v", err)
	}
}

func TestImageLookupSearchExactTagUsesManifestFastPath(t *testing.T) {
	var manifestRequests atomic.Int32
	var tagListRequests atomic.Int32
	registryHandler := registry.New(registry.Logger(log.New(io.Discard, "", 0)))
	registryServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if strings.Contains(request.URL.Path, "/manifests/") {
			manifestRequests.Add(1)
		}
		if strings.HasSuffix(request.URL.Path, "/tags/list") {
			tagListRequests.Add(1)
		}
		registryHandler.ServeHTTP(response, request)
	}))
	defer registryServer.Close()

	service := newImageLookupTestService(t, registryServer)
	created := time.Date(2026, time.August, 5, 15, 0, 0, 0, time.UTC)
	fixtureImage := newImageLookupFixtureImage(t, "amd64", "", created, map[string]string{
		"build.yaml": "release: exact-fast-path\n",
	})
	referenceText := imageLookupTestServerHost(t, registryServer) + "/rancher/rancher:v2.16.0-rcs-0844.1"
	reference, err := name.NewTag(referenceText, name.StrictValidation, name.Insecure)
	if err != nil {
		t.Fatalf("parse exact-tag fixture reference: %v", err)
	}
	if err := remote.Write(reference, fixtureImage,
		remote.WithTransport(registryServer.Client().Transport),
		remote.WithAuth(authn.Anonymous),
	); err != nil {
		t.Fatalf("push exact-tag fixture image: %v", err)
	}
	expectedDigest, err := fixtureImage.Digest()
	if err != nil {
		t.Fatalf("fixture image digest: %v", err)
	}
	manifestRequests.Store(0)
	tagListRequests.Store(0)

	result, err := service.Search(context.Background(), imageLookupSearchRequest{
		Registry:   imageLookupTestServerHost(t, registryServer),
		Repository: "rancher/rancher",
		Query:      "v2.16.0-rcs-0844.1",
		Limit:      200,
	})
	if err != nil {
		t.Fatalf("Search exact local tag: %v", err)
	}
	if tagListRequests.Load() != 0 {
		t.Fatalf("exact-tag search made %d tag-list requests, want 0", tagListRequests.Load())
	}
	if manifestRequests.Load() == 0 {
		t.Fatal("exact-tag search did not request the manifest")
	}
	if len(result.Groups) != 1 {
		t.Fatalf("search groups = %d, want 1", len(result.Groups))
	}
	group := result.Groups[0]
	if group.Error != "" || group.Scanned != 1 || group.Matched != 1 || group.Truncated || len(group.Tags) != 1 {
		t.Fatalf("unexpected exact-tag group: %#v", group)
	}
	tag := group.Tags[0]
	if tag.Name != "v2.16.0-rcs-0844.1" || tag.Channel != "rcs" || tag.Architecture != "multi" || tag.BaseTag != tag.Name {
		t.Fatalf("unexpected exact-tag classification: %#v", tag)
	}
	if tag.Digest != expectedDigest.String() || tag.Size != 0 {
		t.Fatalf("exact-tag descriptor = digest %q size %d, want digest %q and unknown image size", tag.Digest, tag.Size, expectedDigest.String())
	}
}

func TestImageLookupSearchExactTagNotFoundFallsBackToListing(t *testing.T) {
	var missingManifestRequests atomic.Int32
	var tagListRequests atomic.Int32
	registryHandler := registry.New(registry.Logger(log.New(io.Discard, "", 0)))
	registryServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "/manifests/0844") {
			missingManifestRequests.Add(1)
		}
		if strings.HasSuffix(request.URL.Path, "/tags/list") {
			tagListRequests.Add(1)
		}
		registryHandler.ServeHTTP(response, request)
	}))
	defer registryServer.Close()

	service := newImageLookupTestService(t, registryServer)
	fixtureImage := newImageLookupFixtureImage(t, "amd64", "", time.Now().UTC(), map[string]string{
		"README.txt": "fallback fixture",
	})
	referenceText := imageLookupTestServerHost(t, registryServer) + "/rancher/rancher:v2.16.0-rcs-0844.1"
	reference, err := name.NewTag(referenceText, name.StrictValidation, name.Insecure)
	if err != nil {
		t.Fatalf("parse fallback fixture reference: %v", err)
	}
	if err := remote.Write(reference, fixtureImage,
		remote.WithTransport(registryServer.Client().Transport),
		remote.WithAuth(authn.Anonymous),
	); err != nil {
		t.Fatalf("push fallback fixture image: %v", err)
	}
	missingManifestRequests.Store(0)
	tagListRequests.Store(0)

	result, err := service.Search(context.Background(), imageLookupSearchRequest{
		Registry:   imageLookupTestServerHost(t, registryServer),
		Repository: "rancher/rancher",
		Query:      "0844",
		Limit:      200,
	})
	if err != nil {
		t.Fatalf("Search partial tag after exact miss: %v", err)
	}
	if missingManifestRequests.Load() == 0 {
		t.Fatal("partial query did not attempt the valid exact tag first")
	}
	if tagListRequests.Load() == 0 {
		t.Fatal("404 exact-tag response did not fall back to tag listing")
	}
	if len(result.Groups) != 1 || len(result.Groups[0].Tags) != 1 || result.Groups[0].Tags[0].Name != "v2.16.0-rcs-0844.1" {
		t.Fatalf("unexpected fallback result: %#v", result.Groups)
	}
}

func TestImageLookupSearchMissingFullVersionDoesNotList(t *testing.T) {
	var manifestRequests atomic.Int32
	var tagListRequests atomic.Int32
	registryHandler := registry.New(registry.Logger(log.New(io.Discard, "", 0)))
	registryServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if strings.Contains(request.URL.Path, "/manifests/") {
			manifestRequests.Add(1)
		}
		if strings.HasSuffix(request.URL.Path, "/tags/list") {
			tagListRequests.Add(1)
		}
		registryHandler.ServeHTTP(response, request)
	}))
	defer registryServer.Close()

	service := newImageLookupTestService(t, registryServer)
	result, err := service.Search(context.Background(), imageLookupSearchRequest{
		Registry:   imageLookupTestServerHost(t, registryServer),
		Repository: "rancher/rancher",
		Query:      "v2.16.0-rcs-0844.1",
		Limit:      200,
	})
	if err != nil {
		t.Fatalf("Search missing full version tag: %v", err)
	}
	if manifestRequests.Load() == 0 {
		t.Fatal("missing full version did not attempt the exact manifest")
	}
	if tagListRequests.Load() != 0 {
		t.Fatalf("missing full version made %d tag-list requests, want 0", tagListRequests.Load())
	}
	if len(result.Groups) != 1 {
		t.Fatalf("search groups = %d, want 1", len(result.Groups))
	}
	group := result.Groups[0]
	if group.Error != "" || group.Scanned != 1 || group.Matched != 0 || group.Truncated || len(group.Tags) != 0 {
		t.Fatalf("unexpected missing full-version group: %#v", group)
	}
}

func TestImageLookupSearchExactTagNonNotFoundDoesNotList(t *testing.T) {
	var tagListRequests atomic.Int32
	registryServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch {
		case request.URL.Path == "/v2/":
			response.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
			response.WriteHeader(http.StatusOK)
		case strings.Contains(request.URL.Path, "/manifests/"):
			response.Header().Set("Content-Type", "application/json")
			response.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(response).Encode(map[string]any{
				"errors": []map[string]string{{"code": "UNAVAILABLE", "message": "try later"}},
			})
		case strings.HasSuffix(request.URL.Path, "/tags/list"):
			tagListRequests.Add(1)
			_ = json.NewEncoder(response).Encode(map[string]any{
				"name": "rancher/rancher",
				"tags": []string{"v2.16.0"},
			})
		default:
			http.NotFound(response, request)
		}
	}))
	defer registryServer.Close()

	service := newImageLookupTestService(t, registryServer)
	group := service.searchTarget(context.Background(), imageLookupTarget{
		registry:   imageLookupTestServerHost(t, registryServer),
		repository: "rancher/rancher",
	}, "v2.16.0", 200, false)
	if group.Error != "registry returned 503 Service Unavailable" {
		t.Fatalf("exact-tag non-404 error = %q, want registry 503", group.Error)
	}
	if tagListRequests.Load() != 0 {
		t.Fatalf("non-404 exact-tag failure made %d tag-list requests, want 0", tagListRequests.Load())
	}
}

func TestImageLookupSearchStopsWhenResultLimitIsCollected(t *testing.T) {
	var tagListRequests atomic.Int32
	registryServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v2/":
			response.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
			response.WriteHeader(http.StatusOK)
		case "/v2/rancher/rancher/tags/list":
			tagListRequests.Add(1)
			response.Header().Set("Content-Type", "application/json")
			response.Header().Set("Link", `</v2/rancher/rancher/tags/list?n=1000&last=v3>; rel="next"`)
			_ = json.NewEncoder(response).Encode(map[string]any{
				"name": "rancher/rancher",
				"tags": []string{"v1", "v2", "v3"},
			})
		default:
			http.NotFound(response, request)
		}
	}))
	defer registryServer.Close()

	service := newImageLookupTestService(t, registryServer)
	result, err := service.Search(context.Background(), imageLookupSearchRequest{
		Registry:   imageLookupTestServerHost(t, registryServer),
		Repository: "rancher/rancher",
		Limit:      2,
	})
	if err != nil {
		t.Fatalf("Search with result limit: %v", err)
	}
	if tagListRequests.Load() != 1 {
		t.Fatalf("tag-list requests = %d, want 1", tagListRequests.Load())
	}
	group := result.Groups[0]
	if group.Scanned != 2 || group.Matched != 2 || !group.Truncated || len(group.Tags) != 2 {
		t.Fatalf("unexpected limited search group: %#v", group)
	}
	if group.Tags[0].Name != "v2" || group.Tags[1].Name != "v1" {
		t.Fatalf("limited search order = %#v, want v2 then v1", group.Tags)
	}
}

func TestImageLookupDockerHubUploadedSortPaginatesBeyondOneHundred(t *testing.T) {
	const tagCount = 150
	baseTime := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	tags := make([]imageLookupTag, tagCount)
	metadata := make([]imageLookupDockerHubTag, tagCount)
	for index := 0; index < tagCount; index++ {
		name := fmt.Sprintf("build-%03d", index)
		tags[index] = imageLookupTag{Name: name}
		metadata[index] = imageLookupDockerHubTag{
			Name:          name,
			FullSize:      int64(1000 + index),
			TagLastPushed: baseTime.Add(time.Duration(index) * time.Hour),
		}
	}

	requestedPages := []string{}
	service := &imageLookupService{
		maxTagScan: imageLookupMaxTagScan,
		transport: imageLookupTestRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.Scheme != "https" || request.URL.Host != "hub.docker.com" || request.URL.Path != "/v2/namespaces/rancher/repositories/rancher/tags" {
				return nil, fmt.Errorf("unexpected Docker Hub metadata URL %s", request.URL.String())
			}
			page := request.URL.Query().Get("page")
			requestedPages = append(requestedPages, page)
			var results []imageLookupDockerHubTag
			next := ""
			switch page {
			case "1":
				results = metadata[:100]
				// The implementation must treat Next only as a continuation signal,
				// never as a URL to request.
				next = "https://metadata.invalid/untrusted-next-page"
			case "2":
				results = metadata[100:]
			default:
				return nil, fmt.Errorf("unexpected Docker Hub page %q", page)
			}
			payload, err := json.Marshal(map[string]any{"next": next, "results": results})
			if err != nil {
				return nil, err
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(bytes.NewReader(payload)),
				Request:    request,
			}, nil
		}),
	}
	complete, err := service.enrichDockerHubTags(context.Background(), "rancher/rancher", "", tags, true)
	if err != nil || !complete {
		t.Fatalf("paginate complete Docker Hub metadata: complete=%t err=%v", complete, err)
	}
	if !slices.Equal(requestedPages, []string{"1", "2"}) {
		t.Fatalf("Docker Hub metadata pages = %v, want [1 2]", requestedPages)
	}
	for index := range tags {
		if tags[index].UploadedAt != imageLookupFormatTime(metadata[index].TagLastPushed) || tags[index].Size != metadata[index].FullSize {
			t.Fatalf("metadata for candidate %d = %#v", index, tags[index])
		}
	}

	ascending := append([]imageLookupTag(nil), tags...)
	imageLookupSortTags(ascending, "uploaded", "asc")
	if ascending[0].Name != "build-000" || ascending[len(ascending)-1].Name != "build-149" {
		t.Fatalf("ascending upload order endpoints = %q ... %q", ascending[0].Name, ascending[len(ascending)-1].Name)
	}
	descending := append([]imageLookupTag(nil), tags...)
	imageLookupSortTags(descending, "uploaded", "desc")
	if descending[0].Name != "build-149" || descending[len(descending)-1].Name != "build-000" {
		t.Fatalf("descending upload order endpoints = %q ... %q", descending[0].Name, descending[len(descending)-1].Name)
	}
}

func TestImageLookupInspectOCIIndexConfigLayersAndBuildYAML(t *testing.T) {
	registryServer := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	defer registryServer.Close()
	service := newImageLookupTestService(t, registryServer)

	created := time.Date(2026, time.August, 5, 14, 30, 0, 0, time.UTC)
	amd64Image := newImageLookupFixtureImage(t, "amd64", "", created, map[string]string{
		"usr/src/rancher/build.yaml": "webhookVersion: v0.12.1-rcs-0844.1\nrelease: security\n",
	})
	arm64Image := newImageLookupFixtureImage(t, "arm64", "v8", created.Add(time.Minute), map[string]string{
		"README.txt": "arm64 fixture",
	})
	index := mutate.AppendManifests(empty.Index,
		mutate.IndexAddendum{
			Add: amd64Image,
			Descriptor: v1.Descriptor{Platform: &v1.Platform{
				OS:           "linux",
				Architecture: "amd64",
			}},
		},
		mutate.IndexAddendum{
			Add: arm64Image,
			Descriptor: v1.Descriptor{Platform: &v1.Platform{
				OS:           "linux",
				Architecture: "arm64",
				Variant:      "v8",
			}},
		},
	)
	index = mutate.IndexMediaType(index, types.OCIImageIndex)

	referenceText := imageLookupTestServerHost(t, registryServer) + "/rancher/rancher:v2.16.0-rcs-0844.1"
	reference, err := name.NewTag(referenceText, name.StrictValidation, name.Insecure)
	if err != nil {
		t.Fatalf("parse fixture reference: %v", err)
	}
	if err := remote.WriteIndex(reference, index,
		remote.WithTransport(registryServer.Client().Transport),
		remote.WithAuth(authn.Anonymous),
	); err != nil {
		t.Fatalf("push fixture image index: %v", err)
	}

	result, err := service.Inspect(context.Background(), imageLookupInspectRequest{
		Reference:        referenceText,
		Platform:         "linux/amd64",
		IncludeBuildYAML: true,
	})
	if err != nil {
		t.Fatalf("Inspect local OCI index: %v", err)
	}
	if result.Reference != referenceText || result.Registry != imageLookupTestServerHost(t, registryServer) || result.Repository != "rancher/rancher" || result.Tag != "v2.16.0-rcs-0844.1" {
		t.Fatalf("unexpected normalized inspection identity: %#v", result)
	}
	if result.MediaType != string(types.OCIImageIndex) || len(result.Platforms) != 2 {
		t.Fatalf("unexpected index metadata: mediaType=%q platforms=%#v", result.MediaType, result.Platforms)
	}
	if result.Platform != "linux/amd64" || result.Config.OS != "linux" || result.Config.Architecture != "amd64" {
		t.Fatalf("unexpected selected configuration: platform=%q config=%#v", result.Platform, result.Config)
	}
	if result.CreatedAt != created.Format(time.RFC3339Nano) || result.Config.CreatedAt != created.Format(time.RFC3339Nano) {
		t.Fatalf("created timestamp = response %q config %q, want %q", result.CreatedAt, result.Config.CreatedAt, created.Format(time.RFC3339Nano))
	}
	if result.Config.Labels["org.opencontainers.image.version"] != "fixture-amd64" || !slices.Equal(result.Config.Env, []string{"FIXTURE=yes"}) || !slices.Equal(result.Config.Entrypoint, []string{"/usr/bin/rancher"}) || !slices.Equal(result.Config.Cmd, []string{"server"}) {
		t.Fatalf("unexpected image config details: %#v", result.Config)
	}
	if len(result.Config.History) != 2 || result.Config.History[0].Created != created.Format(time.RFC3339Nano) || result.Config.History[0].CreatedBy != "RUN fixture-build --arch=amd64" || result.Config.History[0].Comment != "fixture layer" || result.Config.History[0].EmptyLayer {
		t.Fatalf("unexpected image config history: %#v", result.Config.History)
	}
	if !result.Config.History[1].EmptyLayer || result.Config.History[1].CreatedBy != "LABEL org.opencontainers.image.version=fixture-amd64" {
		t.Fatalf("unexpected metadata history entry: %#v", result.Config.History[1])
	}
	if len(result.Layers) != 1 || result.Layers[0].MediaType != string(types.OCIUncompressedLayer) || result.Size <= result.Config.Size {
		t.Fatalf("unexpected layer/size metadata: layers=%#v size=%d configSize=%d", result.Layers, result.Size, result.Config.Size)
	}
	if !result.BuildYAML.Found || result.BuildYAML.Skipped || result.BuildYAML.Path != "usr/src/rancher/build.yaml" {
		t.Fatalf("unexpected build.yaml result: %#v warnings=%v", result.BuildYAML, result.Warnings)
	}
	if result.BuildYAML.Data["webhookVersion"] != "v0.12.1-rcs-0844.1" || !strings.Contains(result.BuildYAML.Raw, "release: security") {
		t.Fatalf("unexpected build.yaml content: %#v", result.BuildYAML)
	}
}

func TestImageLookupInspectCanSkipOptionalDockerHubTagMetadata(t *testing.T) {
	registryServer := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	defer registryServer.Close()
	pushImageLookupSourceFixture(t, registryServer, "v2.14-head", nil)

	target, err := url.Parse(registryServer.URL)
	if err != nil {
		t.Fatalf("parse registry fixture URL: %v", err)
	}
	var hubRequests atomic.Int32
	service := newImageLookupTestService(t, registryServer)
	service.allowHTTP = false
	service.transport = imageLookupTestRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Hostname() == "hub.docker.com" {
			hubRequests.Add(1)
			return nil, errors.New("optional Docker Hub metadata is unavailable")
		}
		cloned := request.Clone(request.Context())
		cloned.URL.Scheme = target.Scheme
		cloned.URL.Host = target.Host
		cloned.Host = target.Host
		return registryServer.Client().Transport.RoundTrip(cloned)
	})

	result, err := service.Inspect(context.Background(), imageLookupInspectRequest{
		Reference:       "docker.io/rancher/rancher:v2.14-head",
		Platform:        "linux/amd64",
		SkipTagMetadata: true,
	})
	if err != nil {
		t.Fatalf("Inspect with tag metadata disabled: %v", err)
	}
	if result.Digest == "" || hubRequests.Load() != 0 {
		t.Fatalf("manifest inspection result=%#v optional metadata requests=%d", result, hubRequests.Load())
	}
}

func TestImageLookupBoundedHistoryKeepsLatestEntriesAndBoundsText(t *testing.T) {
	created := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	history := make([]v1.History, imageLookupMaxHistoryEntries+2)
	for index := range history {
		history[index] = v1.History{
			Created:    v1.Time{Time: created.Add(time.Duration(index) * time.Minute)},
			CreatedBy:  "RUN ordinary-build-step",
			Comment:    "ordinary comment",
			EmptyLayer: index == len(history)-1,
		}
	}
	history[2].CreatedBy = strings.Repeat("c", imageLookupMaxHistoryText+50)
	history[2].Comment = strings.Repeat("m", imageLookupMaxHistoryText+75)

	bounded := imageLookupBoundedHistory(history)
	if len(bounded) != imageLookupMaxHistoryEntries {
		t.Fatalf("bounded history length = %d, want %d", len(bounded), imageLookupMaxHistoryEntries)
	}
	if bounded[0].Created != imageLookupFormatTime(history[2].Created.Time) {
		t.Fatalf("first retained history timestamp = %q, want latest bounded window beginning at %q", bounded[0].Created, imageLookupFormatTime(history[2].Created.Time))
	}
	if len(bounded[0].CreatedBy) != imageLookupMaxHistoryText || len(bounded[0].Comment) != imageLookupMaxHistoryText {
		t.Fatalf("history text bounds = createdBy %d comment %d, want %d each", len(bounded[0].CreatedBy), len(bounded[0].Comment), imageLookupMaxHistoryText)
	}
	if !bounded[len(bounded)-1].EmptyLayer {
		t.Fatal("bounded history did not preserve empty-layer metadata")
	}
	if empty := imageLookupBoundedHistory(nil); empty == nil || len(empty) != 0 {
		t.Fatalf("empty history = %#v, want non-nil empty slice", empty)
	}
}

func TestImageLookupBuildYAMLScanSkipsLargeLayersAndContinues(t *testing.T) {
	buildLayer := newImageLookupFixtureLayer(t, map[string]string{
		"usr/src/rancher/build.yaml": "release: bounded-layer-scan\n",
	})
	largeLayer := newImageLookupFixtureLayer(t, map[string]string{
		"large.bin": strings.Repeat("x", 32<<10),
	})
	buildSize, err := buildLayer.Size()
	if err != nil {
		t.Fatalf("build fixture layer size: %v", err)
	}
	largeSize, err := largeLayer.Size()
	if err != nil {
		t.Fatalf("large fixture layer size: %v", err)
	}
	if largeSize <= buildSize {
		t.Fatalf("large fixture layer size %d must exceed build layer size %d", largeSize, buildSize)
	}
	image, err := mutate.AppendLayers(empty.Image, buildLayer, largeLayer)
	if err != nil {
		t.Fatalf("append build.yaml fixture layers: %v", err)
	}
	service := &imageLookupService{
		maxBuildYML:   imageLookupMaxBuildYAML,
		maxBuildLayer: buildSize,
		maxLayerScan:  8 << 20,
	}

	result, warnings := service.findBuildYAML(context.Background(), image, nil)
	if !result.Found || result.Skipped || result.Error != "" || result.Path != "usr/src/rancher/build.yaml" {
		t.Fatalf("build.yaml scan result = %#v, warnings=%v", result, warnings)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "Skipped 1 layer larger than") {
		t.Fatalf("large-layer warning = %v, want one aggregate skip warning", warnings)
	}
}

func TestImageLookupBuildYAMLScanReturnsNonErrorReasonWhenLargeLayersWereSkipped(t *testing.T) {
	smallLayer := newImageLookupFixtureLayer(t, map[string]string{
		"README.txt": "eligible smaller layer without build metadata",
	})
	largeBuildLayer := newImageLookupFixtureLayer(t, map[string]string{
		"usr/src/rancher/build.yaml": "release: " + strings.Repeat("x", 32<<10) + "\n",
	})
	smallSize, err := smallLayer.Size()
	if err != nil {
		t.Fatalf("small fixture layer size: %v", err)
	}
	largeSize, err := largeBuildLayer.Size()
	if err != nil {
		t.Fatalf("large build fixture layer size: %v", err)
	}
	if largeSize <= smallSize {
		t.Fatalf("large build fixture layer size %d must exceed small layer size %d", largeSize, smallSize)
	}
	image, err := mutate.AppendLayers(empty.Image, smallLayer, largeBuildLayer, largeBuildLayer)
	if err != nil {
		t.Fatalf("append skipped build.yaml fixture layers: %v", err)
	}
	service := &imageLookupService{
		maxBuildYML:   imageLookupMaxBuildYAML,
		maxBuildLayer: smallSize,
		maxLayerScan:  8 << 20,
	}

	result, warnings := service.findBuildYAML(context.Background(), image, nil)
	if result.Found || !result.Skipped || result.Error != "" {
		t.Fatalf("large-layer-only build.yaml result = %#v, warnings=%v", result, warnings)
	}
	if !strings.Contains(result.Reason, "Skipped 2 layers larger than") || !strings.Contains(result.Reason, "safe scan limit") {
		t.Fatalf("non-error skip reason = %q", result.Reason)
	}
	if len(warnings) != 1 || warnings[0] != result.Reason {
		t.Fatalf("skip warnings = %v, want reason %q", warnings, result.Reason)
	}
	if got, want := imageLookupBuildYAMLSkipReason(6, 0, imageLookupMaxBuildYAMLLayer), "Skipped 6 layers larger than the 16 MiB safe scan limit."; got != want {
		t.Fatalf("default large-layer reason = %q, want %q", got, want)
	}
}

func TestImageLookupBuildYAMLScanLimitReturnsNonErrorReason(t *testing.T) {
	layer := newImageLookupFixtureLayer(t, map[string]string{
		"large-readme.txt": strings.Repeat("x", 8<<10),
	})
	compressedSize, err := layer.Size()
	if err != nil {
		t.Fatalf("scan-limit fixture layer size: %v", err)
	}
	image, err := mutate.AppendLayers(empty.Image, layer)
	if err != nil {
		t.Fatalf("append scan-limit fixture layer: %v", err)
	}
	service := &imageLookupService{
		maxBuildYML:   imageLookupMaxBuildYAML,
		maxBuildLayer: compressedSize,
		maxLayerScan:  512,
	}

	result, warnings := service.findBuildYAML(context.Background(), image, nil)
	if result.Found || !result.Skipped || result.Error != "" {
		t.Fatalf("cumulative scan-limit result = %#v, warnings=%v", result, warnings)
	}
	if !strings.Contains(result.Reason, "512 bytes cumulative uncompressed safe scan limit") || !strings.Contains(result.Reason, "remaining image layer data was not scanned") {
		t.Fatalf("cumulative scan-limit reason = %q", result.Reason)
	}
	if len(warnings) != 1 || warnings[0] != result.Reason {
		t.Fatalf("scan-limit warnings = %v, want reason %q", warnings, result.Reason)
	}
	if got, want := imageLookupBuildYAMLScanLimitReason(imageLookupMaxLayerScan), "Stopped after reaching the 256 MiB cumulative uncompressed safe scan limit; remaining image layer data was not scanned."; got != want {
		t.Fatalf("default cumulative scan-limit reason = %q, want %q", got, want)
	}
}

func TestImageLookupFetchSourceBuildYAMLUsesPinnedDeclaredGitHubSource(t *testing.T) {
	registryServer := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	defer registryServer.Close()
	service := newImageLookupTestService(t, registryServer)

	revision := "9c6326c89e3f89c092ff3a80c02bbde96195bccb"
	image := newImageLookupFixtureImage(t, "amd64", "", time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC), map[string]string{
		"README.txt": "source fallback fixture",
	})
	config, err := image.ConfigFile()
	if err != nil {
		t.Fatalf("read source fixture config: %v", err)
	}
	config.Config.Labels[imageLookupSourceLabel] = "https://github.com/rancher/rancher-prime.git"
	config.Config.Labels[imageLookupRevisionLabel] = revision
	image, err = mutate.ConfigFile(image, config)
	if err != nil {
		t.Fatalf("write source fixture config: %v", err)
	}
	referenceText := imageLookupTestServerHost(t, registryServer) + "/rancher/rancher:v2.16.0-rcs-0844.1"
	reference, err := name.NewTag(referenceText, name.StrictValidation, name.Insecure)
	if err != nil {
		t.Fatalf("parse source fixture reference: %v", err)
	}
	if err := remote.Write(reference, image,
		remote.WithTransport(registryServer.Client().Transport),
		remote.WithAuth(authn.Anonymous),
	); err != nil {
		t.Fatalf("push source fixture image: %v", err)
	}
	digest, err := image.Digest()
	if err != nil {
		t.Fatalf("source fixture digest: %v", err)
	}

	t.Setenv("GH_DEBUG", "api")
	t.Setenv("GH_PROMPT_DISABLED", "not-sanitized")
	t.Setenv("GIT_TERMINAL_PROMPT", "not-sanitized")
	t.Setenv("GH_PAGER", "not-sanitized")
	var commandCalls int
	var commandName string
	var commandArguments, commandEnvironment []string
	var commandLimit int64
	service.runCommand = func(ctx context.Context, executable string, arguments, environment []string, outputLimit int64) ([]byte, error) {
		commandCalls++
		commandName = executable
		commandArguments = append([]string(nil), arguments...)
		commandEnvironment = append([]string(nil), environment...)
		commandLimit = outputLimit
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > imageLookupGHTimeout+time.Second {
			t.Fatalf("command context deadline = %v, want a live deadline within %s", deadline, imageLookupGHTimeout)
		}
		return []byte("fleetVersion: 110.0.0+up0.16.0-rc.5\nwebhookVersion: 110.0.0+up0.11.0\n"), nil
	}

	response, err := service.FetchSourceBuildYAML(context.Background(), imageLookupSourceBuildYAMLRequest{
		Reference:      referenceText,
		Platform:       "linux/amd64",
		ExpectedDigest: digest.String(),
	})
	if err != nil {
		t.Fatalf("FetchSourceBuildYAML: %v", err)
	}
	if commandCalls != 1 || commandName != "gh" || commandLimit != imageLookupMaxSourceBuildYAML {
		t.Fatalf("command invocation = calls %d name %q limit %d", commandCalls, commandName, commandLimit)
	}
	wantArguments := []string{
		"api",
		"--hostname", "github.com",
		"--method", http.MethodGet,
		"-H", "Accept:application/vnd.github.raw+json",
		"/repos/rancher/rancher-prime/contents/build.yaml?ref=" + revision,
	}
	if !slices.Equal(commandArguments, wantArguments) {
		t.Fatalf("gh arguments = %#v, want %#v", commandArguments, wantArguments)
	}
	environment := map[string]string{}
	for _, entry := range commandEnvironment {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) == 2 {
			environment[parts[0]] = parts[1]
		}
	}
	if environment["GH_PROMPT_DISABLED"] != "1" || environment["GIT_TERMINAL_PROMPT"] != "0" || environment["GH_PAGER"] != "cat" {
		t.Fatalf("sanitized gh environment = %#v", environment)
	}
	if _, ok := environment["GH_DEBUG"]; ok {
		t.Fatalf("sanitized gh environment retained GH_DEBUG: %#v", environment)
	}
	if !response.Found || response.Path != "build.yaml" || response.Origin != "declared-source" {
		t.Fatalf("source build.yaml identity = %#v", response)
	}
	if response.Data["fleetVersion"] != "110.0.0+up0.16.0-rc.5" || response.Data["webhookVersion"] != "110.0.0+up0.11.0" {
		t.Fatalf("source build.yaml data = %#v", response.Data)
	}
	if response.Provenance.RepositoryURL != "https://github.com/rancher/rancher-prime" || response.Provenance.Revision != revision || response.Provenance.Path != "build.yaml" || response.Provenance.ImageReference != referenceText || response.Provenance.ImageDigest != digest.String() || response.Provenance.Platform != "linux/amd64" {
		t.Fatalf("source build.yaml provenance = %#v", response.Provenance)
	}
	if response.Provenance.SourceLabel != imageLookupSourceLabel || response.Provenance.RevisionLabel != imageLookupRevisionLabel {
		t.Fatalf("source build.yaml provenance labels = %#v", response.Provenance)
	}

	_, err = service.FetchSourceBuildYAML(context.Background(), imageLookupSourceBuildYAMLRequest{
		Reference:      referenceText,
		Platform:       "linux/amd64",
		ExpectedDigest: "sha256:" + strings.Repeat("a", 64),
	})
	var conflictErr *imageLookupConflictError
	if !errors.As(err, &conflictErr) || imageLookupHTTPStatus(err) != http.StatusConflict {
		t.Fatalf("moved digest error = %T %v, want conflict", err, err)
	}
	if commandCalls != 1 {
		t.Fatalf("moved digest invoked gh; command calls = %d", commandCalls)
	}

	service.runCommand = func(context.Context, string, []string, []string, int64) ([]byte, error) {
		return nil, errors.New("secret-token-from-gh-stderr")
	}
	_, err = service.FetchSourceBuildYAML(context.Background(), imageLookupSourceBuildYAMLRequest{
		Reference:      referenceText,
		Platform:       "linux/amd64",
		ExpectedDigest: digest.String(),
	})
	if err == nil || strings.Contains(err.Error(), "secret-token") || !strings.Contains(err.Error(), "confirm GitHub CLI authentication") {
		t.Fatalf("sanitized gh failure = %v", err)
	}
}

func TestImageLookupFetchSourceBuildYAMLFallsBackToPinnedOSSRevisionOnlyForRancherPrime(t *testing.T) {
	registryServer := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	defer registryServer.Close()
	service := newImageLookupTestService(t, registryServer)

	privateRevision := "aa818cc35ef525f237c376c136703263113082fa"
	ossRevision := "f7821ed280af6d120b93917cbded7b6946d77d35"
	referenceText, digest := pushImageLookupSourceFixture(t, registryServer, "v2.11.16-alpha6", map[string]string{
		imageLookupSourceLabel:      "https://github.com/rancher/rancher-prime.git",
		imageLookupRevisionLabel:    privateRevision,
		imageLookupOSSRevisionLabel: ossRevision,
	})

	var commandArguments [][]string
	service.runCommand = func(_ context.Context, executable string, arguments, _ []string, outputLimit int64) ([]byte, error) {
		if executable != "gh" || outputLimit != imageLookupMaxSourceBuildYAML {
			t.Fatalf("command = %q limit = %d", executable, outputLimit)
		}
		commandArguments = append(commandArguments, append([]string(nil), arguments...))
		if len(commandArguments) == 1 {
			return nil, errors.New("private repository returned 404 with sensitive details")
		}
		return []byte("fleetVersion: 110.0.0+up0.16.0-rc.5\nwebhookVersion: 110.0.0+up0.11.0\n"), nil
	}

	response, err := service.FetchSourceBuildYAML(context.Background(), imageLookupSourceBuildYAMLRequest{
		Reference:      referenceText,
		Platform:       "linux/amd64",
		ExpectedDigest: digest,
	})
	if err != nil {
		t.Fatalf("FetchSourceBuildYAML with OSS fallback: %v", err)
	}
	if len(commandArguments) != 2 {
		t.Fatalf("gh command calls = %d, want private source then OSS source", len(commandArguments))
	}
	wantPrivateEndpoint := "/repos/rancher/rancher-prime/contents/build.yaml?ref=" + privateRevision
	wantOSSEndpoint := "/repos/rancher/rancher/contents/build.yaml?ref=" + ossRevision
	if commandArguments[0][len(commandArguments[0])-1] != wantPrivateEndpoint || commandArguments[1][len(commandArguments[1])-1] != wantOSSEndpoint {
		t.Fatalf("gh endpoints = %q then %q, want %q then %q", commandArguments[0][len(commandArguments[0])-1], commandArguments[1][len(commandArguments[1])-1], wantPrivateEndpoint, wantOSSEndpoint)
	}
	if !response.Found || response.Origin != "declared-oss-source" || response.Data["webhookVersion"] != "110.0.0+up0.11.0" {
		t.Fatalf("OSS fallback response = %#v", response)
	}
	if response.Provenance.RepositoryURL != "https://github.com/rancher/rancher" || response.Provenance.Revision != ossRevision || response.Provenance.RevisionLabel != imageLookupOSSRevisionLabel || response.Provenance.SourceLabel != imageLookupSourceLabel {
		t.Fatalf("OSS fallback provenance = %#v", response.Provenance)
	}

	arbitraryReference, arbitraryDigest := pushImageLookupSourceFixture(t, registryServer, "v2.11.16-alpha7", map[string]string{
		imageLookupSourceLabel:      "https://github.com/rancher/embargoed-security",
		imageLookupRevisionLabel:    privateRevision,
		imageLookupOSSRevisionLabel: ossRevision,
	})
	var arbitraryCalls int
	service.runCommand = func(context.Context, string, []string, []string, int64) ([]byte, error) {
		arbitraryCalls++
		return nil, errors.New("secret arbitrary source failure")
	}
	_, err = service.FetchSourceBuildYAML(context.Background(), imageLookupSourceBuildYAMLRequest{
		Reference:      arbitraryReference,
		Platform:       "linux/amd64",
		ExpectedDigest: arbitraryDigest,
	})
	if err == nil || strings.Contains(err.Error(), "secret") || !strings.Contains(err.Error(), "confirm GitHub CLI authentication") {
		t.Fatalf("arbitrary source failure = %v", err)
	}
	if arbitraryCalls != 1 {
		t.Fatalf("arbitrary source gh calls = %d, want no OSS fallback", arbitraryCalls)
	}
}

func TestImageLookupParseGitHubSourceRequiresExactPinnedGitHubLabels(t *testing.T) {
	revision := "9c6326c89e3f89c092ff3a80c02bbde96195bccb"
	for _, source := range []string{
		"https://github.com/rancher/embargoed-security",
		"https://github.com/rancher/rancher-prime.git",
	} {
		owner, repository, err := imageLookupParseGitHubSource(source, revision)
		wantRepository := "embargoed-security"
		if strings.HasSuffix(source, "/rancher-prime.git") {
			wantRepository = "rancher-prime"
		}
		if err != nil || owner != "rancher" || repository != wantRepository {
			t.Errorf("valid GitHub source %q = owner %q repository %q error %v", source, owner, repository, err)
		}
	}
	for _, testCase := range []struct {
		source   string
		revision string
	}{
		{"", revision},
		{"https://github.com/rancher/embargoed-security", ""},
		{"https://github.com/rancher/embargoed-security", "9c6326c"},
		{" https://github.com/rancher/embargoed-security", revision},
		{"https://github.com/rancher/embargoed-security", revision + " "},
		{"http://github.com/rancher/embargoed-security", revision},
		{"https://user@github.com/rancher/embargoed-security", revision},
		{"https://github.com/rancher/embargoed-security/", revision},
		{"https://github.com/rancher/embargoed-security?ref=main", revision},
		{"https://github.com/rancher/embargoed-security#build", revision},
		{"https://github.com/rancher/embargoed-security/extra", revision},
		{"https://github.com/rancher/rancher-prime.git/", revision},
		{"https://github.com/rancher/rancher-prime.git?ref=main", revision},
		{"https://github.com/rancher/rancher-prime.git#build", revision},
		{"https://github.com/rancher/.git", revision},
		{"https://github.com/rancher/rancher-prime.git.git", revision},
		{"https://github.com/rancher/rancher-prime.GIT", revision},
		{"https://gitlab.com/rancher/embargoed-security", revision},
	} {
		_, _, err := imageLookupParseGitHubSource(testCase.source, testCase.revision)
		var metadataErr *imageLookupSourceMetadataError
		if !errors.As(err, &metadataErr) || imageLookupHTTPStatus(err) != http.StatusUnprocessableEntity {
			t.Errorf("source %q revision %q error = %T %v, want source metadata error", testCase.source, testCase.revision, err, err)
		}
	}
}

func TestImageLookupSafeTransportRejectsPrivateAddresses(t *testing.T) {
	for _, address := range []string{
		"0.0.0.0",
		"10.0.0.1",
		"100.64.0.1",
		"127.0.0.1",
		"169.254.169.254",
		"172.16.0.1",
		"192.168.1.1",
		"::1",
		"fc00::1",
		"fe80::1",
		"2001:db8::1",
		"::ffff:127.0.0.1",
	} {
		if imageLookupPublicIP(netip.MustParseAddr(address)) {
			t.Errorf("private or reserved address %s was allowed", address)
		}
	}
	for _, address := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if !imageLookupPublicIP(netip.MustParseAddr(address)) {
			t.Errorf("public address %s was blocked", address)
		}
	}

	request := httptest.NewRequest(http.MethodGet, "https://127.0.0.1/v2/", nil)
	_, err := newImageLookupSafeTransport().RoundTrip(request)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "private or reserved") {
		t.Fatalf("safe transport error = %v, want private/reserved rejection", err)
	}
}

func TestImageLookupServiceClosesWrappedIdleConnections(t *testing.T) {
	inner := &imageLookupCloseTrackingRoundTripper{}
	service := &imageLookupService{
		transport: &imageLookupSafeRoundTripper{inner: inner},
	}

	service.closeIdleConnections()

	if got := inner.closes.Load(); got != 1 {
		t.Fatalf("idle connection close calls = %d, want 1", got)
	}
}

func TestImageLookupHandlersEnforceAuthMethodAndStrictJSON(t *testing.T) {
	panel := &localControlPanel{
		token: "test-token",
		imageLookup: &imageLookupService{
			transport:  http.DefaultTransport,
			keychain:   imageLookupAnonymousTestKeychain{},
			allowHTTP:  true,
			maxTagScan: 10,
		},
	}
	tests := []struct {
		name        string
		handler     http.HandlerFunc
		method      string
		body        string
		authorized  bool
		wantStatus  int
		wantBody    string
		wantAllowed string
	}{
		{
			name:       "search rejects unauthenticated request",
			handler:    panel.handleImageLookupSearch,
			method:     http.MethodPost,
			body:       `{}`,
			wantStatus: http.StatusForbidden,
			wantBody:   "invalid control panel token",
		},
		{
			name:        "search only accepts POST",
			handler:     panel.handleImageLookupSearch,
			method:      http.MethodGet,
			authorized:  true,
			wantStatus:  http.StatusMethodNotAllowed,
			wantBody:    "method not allowed",
			wantAllowed: http.MethodPost,
		},
		{
			name:       "search rejects unknown field",
			handler:    panel.handleImageLookupSearch,
			method:     http.MethodPost,
			body:       `{"registry":"docker.io","repository":"rancher/rancher","surprise":true}`,
			authorized: true,
			wantStatus: http.StatusBadRequest,
			wantBody:   "unknown field",
		},
		{
			name:       "search rejects trailing JSON",
			handler:    panel.handleImageLookupSearch,
			method:     http.MethodPost,
			body:       `{} {}`,
			authorized: true,
			wantStatus: http.StatusBadRequest,
			wantBody:   "exactly one JSON object",
		},
		{
			name:       "inspect rejects unknown field",
			handler:    panel.handleImageLookupInspect,
			method:     http.MethodPost,
			body:       `{"reference":"rancher/rancher:v2.16.0","unknown":"value"}`,
			authorized: true,
			wantStatus: http.StatusBadRequest,
			wantBody:   "unknown field",
		},
		{
			name:       "inspect maps validation error to bad request",
			handler:    panel.handleImageLookupInspect,
			method:     http.MethodPost,
			body:       `{"reference":"rancher/rancher"}`,
			authorized: true,
			wantStatus: http.StatusBadRequest,
			wantBody:   "tag or digest",
		},
		{
			name:       "source build yaml rejects unauthenticated request",
			handler:    panel.handleImageLookupSourceBuildYAML,
			method:     http.MethodPost,
			body:       `{}`,
			wantStatus: http.StatusForbidden,
			wantBody:   "invalid control panel token",
		},
		{
			name:        "source build yaml only accepts POST",
			handler:     panel.handleImageLookupSourceBuildYAML,
			method:      http.MethodGet,
			authorized:  true,
			wantStatus:  http.StatusMethodNotAllowed,
			wantBody:    "method not allowed",
			wantAllowed: http.MethodPost,
		},
		{
			name:       "source build yaml rejects unknown field",
			handler:    panel.handleImageLookupSourceBuildYAML,
			method:     http.MethodPost,
			body:       `{"reference":"rancher/rancher:v2.16.0","platform":"linux/amd64","expectedDigest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","unknown":true}`,
			authorized: true,
			wantStatus: http.StatusBadRequest,
			wantBody:   "unknown field",
		},
		{
			name:       "source build yaml requires inspected digest",
			handler:    panel.handleImageLookupSourceBuildYAML,
			method:     http.MethodPost,
			body:       `{"reference":"rancher/rancher:v2.16.0","platform":"linux/amd64"}`,
			authorized: true,
			wantStatus: http.StatusBadRequest,
			wantBody:   "expectedDigest",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(testCase.method, "/api/images/test", strings.NewReader(testCase.body))
			request.RemoteAddr = "198.51.100.5:12345"
			if testCase.authorized {
				request.Header.Set("X-Control-Panel-Token", "test-token")
			}
			recorder := httptest.NewRecorder()
			testCase.handler(recorder, request)

			if recorder.Code != testCase.wantStatus {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, testCase.wantStatus, recorder.Body.String())
			}
			if !strings.Contains(recorder.Body.String(), testCase.wantBody) {
				t.Fatalf("body = %q, want containing %q", recorder.Body.String(), testCase.wantBody)
			}
			if recorder.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("Cache-Control = %q, want no-store", recorder.Header().Get("Cache-Control"))
			}
			if testCase.wantAllowed != "" && recorder.Header().Get("Allow") != testCase.wantAllowed {
				t.Fatalf("Allow = %q, want %q", recorder.Header().Get("Allow"), testCase.wantAllowed)
			}
		})
	}
}

func newImageLookupTestService(t *testing.T, server *httptest.Server) *imageLookupService {
	t.Helper()
	return &imageLookupService{
		transport:     server.Client().Transport,
		keychain:      imageLookupAnonymousTestKeychain{},
		allowHTTP:     true,
		now:           func() time.Time { return time.Date(2026, time.August, 5, 18, 0, 0, 0, time.UTC) },
		maxTagScan:    100,
		maxBuildYML:   1 << 20,
		maxBuildLayer: 4 << 20,
		maxLayerScan:  8 << 20,
	}
}

type imageLookupAnonymousTestKeychain struct{}

func (imageLookupAnonymousTestKeychain) Resolve(authn.Resource) (authn.Authenticator, error) {
	return authn.Anonymous, nil
}

type imageLookupTestRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn imageLookupTestRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

type imageLookupCloseTrackingRoundTripper struct {
	closes atomic.Int32
}

func (*imageLookupCloseTrackingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("unexpected RoundTrip call")
}

func (t *imageLookupCloseTrackingRoundTripper) CloseIdleConnections() {
	t.closes.Add(1)
}

func imageLookupTestServerHost(t *testing.T, server *httptest.Server) string {
	t.Helper()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	return parsed.Host
}

func newImageLookupFixtureImage(t *testing.T, architecture, variant string, created time.Time, files map[string]string) v1.Image {
	t.Helper()
	layer := newImageLookupFixtureLayer(t, files)
	image, err := mutate.AppendLayers(empty.Image, layer)
	if err != nil {
		t.Fatalf("append fixture image layer: %v", err)
	}
	config, err := image.ConfigFile()
	if err != nil {
		t.Fatalf("read fixture config: %v", err)
	}
	config.OS = "linux"
	config.Architecture = architecture
	config.Variant = variant
	config.Created = v1.Time{Time: created}
	config.Config.Labels = map[string]string{"org.opencontainers.image.version": "fixture-" + architecture}
	config.Config.Env = []string{"FIXTURE=yes"}
	config.Config.Entrypoint = []string{"/usr/bin/rancher"}
	config.Config.Cmd = []string{"server"}
	config.History = []v1.History{
		{
			Created:   v1.Time{Time: created},
			CreatedBy: "RUN fixture-build --arch=" + architecture,
			Comment:   "fixture layer",
		},
		{
			Created:    v1.Time{Time: created.Add(time.Second)},
			CreatedBy:  "LABEL org.opencontainers.image.version=fixture-" + architecture,
			Comment:    "fixture metadata",
			EmptyLayer: true,
		},
	}
	image, err = mutate.ConfigFile(image, config)
	if err != nil {
		t.Fatalf("write fixture config: %v", err)
	}
	image = mutate.MediaType(image, types.OCIManifestSchema1)
	return mutate.ConfigMediaType(image, types.OCIConfigJSON)
}

func pushImageLookupSourceFixture(t *testing.T, server *httptest.Server, tag string, labels map[string]string) (string, string) {
	t.Helper()
	image := newImageLookupFixtureImage(t, "amd64", "", time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC), map[string]string{
		"README.txt": "source fallback fixture",
	})
	config, err := image.ConfigFile()
	if err != nil {
		t.Fatalf("read source fixture config: %v", err)
	}
	for key, value := range labels {
		config.Config.Labels[key] = value
	}
	image, err = mutate.ConfigFile(image, config)
	if err != nil {
		t.Fatalf("write source fixture config: %v", err)
	}
	referenceText := imageLookupTestServerHost(t, server) + "/rancher/rancher:" + tag
	reference, err := name.NewTag(referenceText, name.StrictValidation, name.Insecure)
	if err != nil {
		t.Fatalf("parse source fixture reference: %v", err)
	}
	if err := remote.Write(reference, image,
		remote.WithTransport(server.Client().Transport),
		remote.WithAuth(authn.Anonymous),
	); err != nil {
		t.Fatalf("push source fixture image: %v", err)
	}
	digest, err := image.Digest()
	if err != nil {
		t.Fatalf("source fixture digest: %v", err)
	}
	return referenceText, digest.String()
}

func pushImageLookupPrimeFixture(t *testing.T, server *httptest.Server, repository, tag, ossRevision string, created time.Time) {
	t.Helper()
	architecture, _ := imageLookupTagArchitecture(tag)
	if architecture == "multi" {
		architecture = "amd64"
	}
	image := newImageLookupFixtureImage(t, architecture, "", created, map[string]string{
		"README.txt": "Prime pair fixture",
	})
	config, err := image.ConfigFile()
	if err != nil {
		t.Fatalf("read Prime fixture config: %v", err)
	}
	config.Config.Labels[imageLookupSourceLabel] = "https://github.com/rancher/rancher-prime"
	config.Config.Labels[imageLookupRevisionLabel] = strings.Repeat("f", 40)
	config.Config.Labels[imageLookupOSSRevisionLabel] = ossRevision
	config.Config.Labels[imageLookupCanonicalReferenceLabel] = "stgregistry.suse.com/" + repository + ":" + tag
	config.Config.Labels[imageLookupVersionLabel] = tag
	image, err = mutate.ConfigFile(image, config)
	if err != nil {
		t.Fatalf("write Prime fixture config: %v", err)
	}
	referenceText := imageLookupTestServerHost(t, server) + "/" + repository + ":" + tag
	reference, err := name.NewTag(referenceText, name.StrictValidation, name.Insecure)
	if err != nil {
		t.Fatalf("parse Prime fixture reference: %v", err)
	}
	if err := remote.Write(reference, image,
		remote.WithTransport(server.Client().Transport),
		remote.WithAuth(authn.Anonymous),
	); err != nil {
		t.Fatalf("push Prime fixture image: %v", err)
	}
}

func newImageLookupRewritingTestService(t *testing.T, server *httptest.Server, failPath string) *imageLookupService {
	t.Helper()
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse rewriting registry URL: %v", err)
	}
	base := server.Client().Transport
	transport := imageLookupTestRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if failPath != "" && request.URL.Path == failPath {
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Status:     "503 Service Unavailable",
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"errors":[{"code":"UNAVAILABLE","message":"try later"}]}`)),
				Request:    request,
			}, nil
		}
		clone := request.Clone(request.Context())
		urlCopy := *request.URL
		urlCopy.Scheme = target.Scheme
		urlCopy.Host = target.Host
		clone.URL = &urlCopy
		clone.Host = target.Host
		return base.RoundTrip(clone)
	})
	return &imageLookupService{
		transport:     transport,
		keychain:      imageLookupAnonymousTestKeychain{},
		allowHTTP:     true,
		now:           time.Now,
		maxTagScan:    imageLookupMaxTagScan,
		maxBuildYML:   1 << 20,
		maxBuildLayer: 4 << 20,
		maxLayerScan:  8 << 20,
	}
}

func newImageLookupFixtureLayer(t *testing.T, files map[string]string) v1.Layer {
	t.Helper()
	var layerArchive bytes.Buffer
	archive := tar.NewWriter(&layerArchive)
	paths := make([]string, 0, len(files))
	for filePath := range files {
		paths = append(paths, filePath)
	}
	sort.Strings(paths)
	for _, filePath := range paths {
		content := []byte(files[filePath])
		if err := archive.WriteHeader(&tar.Header{
			Name:     filePath,
			Mode:     0o644,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatalf("write fixture layer header: %v", err)
		}
		if _, err := archive.Write(content); err != nil {
			t.Fatalf("write fixture layer content: %v", err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("close fixture layer: %v", err)
	}
	return static.NewLayer(layerArchive.Bytes(), types.OCIUncompressedLayer)
}
