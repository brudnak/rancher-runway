package imagelookup

import (
	"strings"
	"testing"
)

func TestImageLookupParseReferenceNormalizesAndRejectsUnsafeInput(t *testing.T) {
	service := &Service{}
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
	service := &Service{}

	targets, query, limit, err := service.searchTargets(SearchRequest{
		Registry:   "stgregistry.suse.com",
		Repository: "rancher/rancher",
		Query:      "head",
		Limit:      200,
	})
	if err != nil {
		t.Fatalf("searchTargets accepted UI limit: %v", err)
	}
	if limit != 200 || query != "head" || len(targets) != 1 || targets[0] != (searchTarget{registry: "stgregistry.suse.com", repository: "rancher/rancher"}) {
		t.Fatalf("unexpected explicit search target: targets=%#v query=%q limit=%d", targets, query, limit)
	}

	targets, query, _, err = service.searchTargets(SearchRequest{
		Registry:   "all",
		Repository: "stgregistry.suse.com/rancher/rancher:v2.16.0-rcs-0844.1",
	})
	if err != nil {
		t.Fatalf("searchTargets full reference: %v", err)
	}
	if len(targets) != 1 || targets[0].registry != "stgregistry.suse.com" || targets[0].repository != "rancher/rancher" || query != "v2.16.0-rcs-0844.1" {
		t.Fatalf("full reference was not narrowed correctly: targets=%#v query=%q", targets, query)
	}

	targets, _, limit, err = service.searchTargets(SearchRequest{Registry: "all", Repository: "all"})
	if err != nil {
		t.Fatalf("searchTargets all known sources: %v", err)
	}
	if want := len(imageLookupKnownRegistries) * len(imageLookupKnownRepositories); len(targets) != want {
		t.Fatalf("all-source target count = %d, want %d", len(targets), want)
	}
	if limit != imageLookupDefaultResultLimit {
		t.Fatalf("default limit = %d, want %d", limit, imageLookupDefaultResultLimit)
	}

	for _, request := range []SearchRequest{
		{Registry: "docker.io", Repository: "rancher/rancher", Limit: 201},
		{Registry: "docker.io", Repository: "rancher/rancher", Limit: -1},
		{Registry: "docker.io", Repository: "rancher/rancher", Query: "two words"},
	} {
		if _, _, _, err := service.searchTargets(request); err == nil {
			t.Fatalf("searchTargets(%#v) unexpectedly succeeded", request)
		}
	}
}
