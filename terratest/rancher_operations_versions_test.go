package test

import (
	"strings"
	"testing"

	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
)

func TestRancherUpgradeVersionEvidence(t *testing.T) {
	exact := "v2.15-19c92983f6f9d7f455de668e62fbfe55c045cde2-head"
	for _, tc := range []struct {
		name              string
		image             imagelookup.Provenance
		tag, want, source string
	}{
		{"runtime overrides branch", imagelookup.Provenance{ServerVersion: exact, BuildVersion: "release-v2.15"}, "v2.15-head", exact, "CATTLE_SERVER_VERSION"},
		{"runtime patch overrides tag", imagelookup.Provenance{ServerVersion: "v2.15.1", BuildVersion: "v2.15.3"}, "v2.15.3", "v2.15.1", "CATTLE_SERVER_VERSION"},
		{"exact label", imagelookup.Provenance{BuildVersion: "v2.16.0-alpha1"}, "head", "v2.16.0-alpha1", "OCI"},
		{"canonical fallback", imagelookup.Provenance{BuildVersion: "release-v2.15", CanonicalReference: "rancher/rancher:" + exact}, "v2.15-head", exact, "Canonical"},
		{"tag fallback", imagelookup.Provenance{}, "v2.15.3-head", "v2.15.3-head", "Explicit"},
		{"branch alone is not a version", imagelookup.Provenance{BuildVersion: "release-v2.15"}, "v2.15-head", "", ""},
		{"global head without version", imagelookup.Provenance{}, "head", "", ""},
		{"invalid runtime not hidden by fallback", imagelookup.Provenance{ServerVersion: "dev", BuildVersion: "v2.15.3"}, "v2.15.3", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, source, err := resolveUpgradeImageVersion(tc.image, tc.tag)
			if tc.want == "" {
				if err == nil {
					t.Fatal("unverifiable metadata accepted")
				}
				return
			}
			if err != nil || got != tc.want || !strings.Contains(source, tc.source) {
				t.Fatalf("got %q source=%q err=%v", got, source, err)
			}
		})
	}
}

func TestRancherUpgradeMinorHeadPaths(t *testing.T) {
	head := func(line string) string { return "v" + line + "-19c92983f6f9d7f455de668e62fbfe55c045cde2-head" }
	for _, tc := range []struct {
		from, to              string
		experimental, allowed bool
	}{
		{"v2.15.2", head("2.15"), true, true},
		{"v2.15.2", head("2.16"), true, true},
		{"v2.15.2", head("2.15"), false, false},
		{"v2.15.2", head("2.14"), true, false},
		{"v2.15.2", head("2.17"), true, false},
		{"v2.15.2", head("3.15"), true, false},
		{"v2.15.2", "v2.15.1-abcdef123-head", true, false},
		{"v2.15.2", "release-v2.15", true, false},
		{head("2.15"), "v2.15.3", true, true},
		{head("2.15"), "v2.14.9", true, false},
		{head("2.15"), head("2.15"), true, false},
	} {
		t.Run(tc.from+"_to_"+tc.to, func(t *testing.T) {
			err := validateUpgradeStep(tc.from, tc.to, tc.experimental)
			if (err == nil) != tc.allowed {
				t.Fatalf("allowed=%v err=%v", tc.allowed, err)
			}
		})
	}
}
