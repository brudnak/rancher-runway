package imagelookup

import (
	"github.com/google/go-containerregistry/pkg/name"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
)

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
		architecture, base := TagArchitecture(testCase.tag)
		if architecture != testCase.wantArchitecture || base != testCase.wantBase {
			t.Errorf("TagArchitecture(%q) = (%q, %q), want (%q, %q)", testCase.tag, architecture, base, testCase.wantArchitecture, testCase.wantBase)
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
		return NaturalCompare(tags[i], tags[j]) > 0
	})
	wantOrder := []string{"v2.10.0-rc10", "v2.10.0-rc2", "v2.10.0", "v2.9.0"}
	if !slices.Equal(tags, wantOrder) {
		t.Fatalf("natural descending order = %v, want %v", tags, wantOrder)
	}
	alphaTags := []string{"v2.17.0-alpha2", "v2.17.0-alpha10", "v2.17.0-alpha1"}
	sort.Slice(alphaTags, func(i, j int) bool {
		return NaturalCompare(alphaTags[i], alphaTags[j]) > 0
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
	if !imageLookupTagMatchesOptions(immutable, searchOptions{query: "v2.15.1-head", primeHead: "only", headKind: "all", channel: "all", architecture: "all"}) {
		t.Fatal("moving patch selector did not match its immutable candidate")
	}
	if imageLookupTagMatchesOptions(immutable, searchOptions{query: "v2.15.2-head", primeHead: "only", headKind: "all", channel: "all", architecture: "all"}) {
		t.Fatal("moving patch selector crossed patch versions")
	}
	if !imageLookupTagMatchesOptions(immutable, searchOptions{primeHead: "only", headKind: "immutable", versionLine: "2.15.1", commit: sha[:12], channel: "all", architecture: "all"}) {
		t.Fatal("normalized Prime metadata filters rejected an exact candidate")
	}
}

func TestImageLookupPrimeTargetNarrowingAndValidation(t *testing.T) {
	service := &Service{}
	targets, options, err := service.searchParameters(SearchRequest{
		Registry:   "all",
		Repository: "all",
		Query:      "primehead",
	})
	if err != nil {
		t.Fatalf("primehead parameters: %v", err)
	}
	wantTargets := []searchTarget{
		{registry: "stgregistry.suse.com", repository: "rancher/rancher"},
		{registry: "stgregistry.suse.com", repository: "rancher/rancher-agent"},
	}
	if !slices.Equal(targets, wantTargets) || options.query != "prime-head" || options.primeHead != "only" || options.verifyPrimePairs || !options.fullScan {
		t.Fatalf("primehead parameters = targets %#v options %#v", targets, options)
	}

	sha := strings.Repeat("b", 40)
	targets, options, err = service.searchParameters(SearchRequest{
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

	_, options, err = service.searchParameters(SearchRequest{
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

	_, _, err = service.searchParameters(SearchRequest{
		Registry:   "all",
		Repository: "all",
		Query:      "docker.io/rancher/rancher:v2.15.1-" + sha + "-head",
	})
	if err == nil || !strings.Contains(err.Error(), "only in stgregistry.suse.com") {
		t.Fatalf("explicit non-staging Prime reference was not rejected: %v", err)
	}

	architectureTag := "v2.15.1-" + sha + "-head-arm64"
	targets, options, err = service.searchParameters(SearchRequest{
		Registry:   "all",
		Repository: "all",
		Query:      "stgregistry.suse.com/rancher/rancher:" + architectureTag,
	})
	if err != nil {
		t.Fatalf("architecture-suffixed Prime parameters: %v", err)
	}
	if len(targets) != 1 || targets[0] != (searchTarget{registry: "stgregistry.suse.com", repository: "rancher/rancher"}) || !options.exactLookup || !options.verifyPrimePairs || options.primeVersion != "2.15.1" {
		t.Fatalf("architecture-suffixed Prime parameters = targets %#v options %#v", targets, options)
	}

	defaultTargets, _, err := service.searchParameters(SearchRequest{Registry: "all", Repository: "rancher/rancher"})
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

	invalid := []SearchRequest{
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

func TestImageLookupRecentDaysValidationAndExplicitDefaultsStayBounded(t *testing.T) {
	service := &Service{}
	_, defaults, err := service.searchParameters(SearchRequest{
		Registry:     "stgregistry.suse.com",
		Repository:   "rancher/rancher",
		Channel:      "all",
		Architecture: "all",
		PrimeHead:    "all",
		HeadKind:     "all",
		PairStatus:   "all",
		SortBy:       "natural",
		SortOrder:    "desc",
	})
	if err != nil {
		t.Fatalf("explicit default parameters: %v", err)
	}
	if defaults.fullScan {
		t.Fatalf("UI-supplied default filters unexpectedly enabled a full registry scan: %#v", defaults)
	}
	_, bounded, err := service.searchParameters(SearchRequest{
		Registry:   "stgregistry.suse.com",
		Repository: "rancher/rancher",
		SortBy:     "version",
		ScanMode:   "bounded",
	})
	if err != nil {
		t.Fatalf("bounded parameters: %v", err)
	}
	if bounded.scanMode != "bounded" || bounded.fullScan {
		t.Fatalf("bounded version sort unexpectedly enabled a full scan: %#v", bounded)
	}
	for _, sortBy := range []string{"version", "uploaded"} {
		_, globallySorted, err := service.searchParameters(SearchRequest{
			Registry:   "stgregistry.suse.com",
			Repository: "rancher/rancher",
			SortBy:     sortBy,
		})
		if err != nil {
			t.Fatalf("auto %s sort parameters: %v", sortBy, err)
		}
		if !globallySorted.fullScan {
			t.Fatalf("auto %s sort did not retain complete-scan semantics: %#v", sortBy, globallySorted)
		}
	}
	_, complete, err := service.searchParameters(SearchRequest{
		Registry:   "stgregistry.suse.com",
		Repository: "rancher/rancher",
		ScanMode:   "complete",
	})
	if err != nil {
		t.Fatalf("complete parameters: %v", err)
	}
	if complete.scanMode != "complete" || !complete.fullScan {
		t.Fatalf("complete scan parameters = %#v", complete)
	}

	_, recent, err := service.searchParameters(SearchRequest{
		Registry:   "stgregistry.suse.com",
		Repository: "rancher/rancher",
		RecentDays: 30,
		ScanMode:   "bounded",
	})
	if err != nil {
		t.Fatalf("recent parameters: %v", err)
	}
	if recent.recentDays != 30 || recent.scanMode != "bounded" || !recent.fullScan {
		t.Fatalf("recent parameters = %#v", recent)
	}

	if _, _, err := service.searchParameters(SearchRequest{
		Registry:   "stgregistry.suse.com",
		Repository: "rancher/rancher",
		ScanMode:   "quickish",
	}); err == nil {
		t.Fatalf("invalid scanMode was not rejected: %v", err)
	}

	for _, recentDays := range []int{-1, imageLookupMaxRecentDays + 1} {
		if _, _, err := service.searchParameters(SearchRequest{
			Registry:   "stgregistry.suse.com",
			Repository: "rancher/rancher",
			RecentDays: recentDays,
		}); err == nil || !strings.Contains(err.Error(), "recentDays") {
			t.Fatalf("recentDays=%d was not rejected: %v", recentDays, err)
		}
	}
}

func TestImageLookupRecentFilterUsesOnlyKnownEvidence(t *testing.T) {
	cutoff := time.Date(2026, time.July, 26, 12, 0, 0, 0, time.UTC)
	tags := []Tag{
		{Name: "pair-recent", PairCompletedAt: imageLookupFormatTime(cutoff.Add(time.Hour)), CreatedAt: imageLookupFormatTime(cutoff.Add(-24 * time.Hour))},
		{Name: "upload-at-cutoff", UploadedAt: imageLookupFormatTime(cutoff)},
		{Name: "created-old", CreatedAt: imageLookupFormatTime(cutoff.Add(-time.Nanosecond))},
		{Name: "unknown"},
	}
	filtered, excluded, unknown := imageLookupFilterRecentTags(tags, cutoff)
	if got, want := []string{filtered[0].Name, filtered[1].Name}, []string{"pair-recent", "upload-at-cutoff"}; !slices.Equal(got, want) {
		t.Fatalf("recent evidence filter = %v, want %v", got, want)
	}
	if excluded != 1 || unknown != 1 {
		t.Fatalf("recent filter accounting: excluded=%d unknown=%d", excluded, unknown)
	}
}

func TestImageLookupInspectPrimeHeadProvenanceClassification(t *testing.T) {
	service := &Service{}
	sha := strings.Repeat("c", 40)
	canonicalTag := "v2.15.1-" + sha + "-head"
	labels := map[string]string{
		SourceLabel:             "https://github.com/rancher/rancher-prime.git",
		RevisionLabel:           strings.Repeat("d", 40),
		OSSRevisionLabel:        sha,
		CanonicalReferenceLabel: "stgregistry.suse.com/rancher/rancher:" + canonicalTag,
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
		CanonicalReferenceLabel: "stgregistry.suse.com/rancher/rancher-agent:" + canonicalTag,
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
	badLabels[OSSRevisionLabel] = strings.Repeat("e", 40)
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
