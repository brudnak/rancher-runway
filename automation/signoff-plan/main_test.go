package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWebhookTagFromBuild(t *testing.T) {
	tag, err := webhookTagFromBuild("109.0.1+up0.10.1-rc.5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tag != "v0.10.1-rc.5" {
		t.Fatalf("expected v0.10.1-rc.5, got %s", tag)
	}
}

func TestParseWebhookBuild(t *testing.T) {
	build, err := parseWebhookBuild(`
defaultShellVersion: rancher/shell:v0.7.0-rc.6
webhookVersion: "109.0.1+up0.10.1-rc.5"
`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if build != "109.0.1+up0.10.1-rc.5" {
		t.Fatalf("unexpected build: %s", build)
	}
}

func TestResolveSigningPolicy(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		registry string
		want     string
	}{
		{name: "suse auto", input: "auto", registry: "registry.suse.com", want: "report-only"},
		{name: "staging auto", input: "auto", registry: "stgregistry.suse.com", want: "report-only"},
		{name: "prime auto", input: "auto", registry: "registry.rancher.com", want: "report-only"},
		{name: "community auto", input: "auto", registry: "docker.io", want: "report-only"},
		{name: "manual required", input: "required", registry: "registry.suse.com", want: "required"},
		{name: "manual skip", input: "skip", registry: "stgregistry.suse.com", want: "skip"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveSigningPolicy(tt.input, tt.registry)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %s, got %s", tt.want, got)
			}
		})
	}
}

func TestWebhookImageCandidatesPreferReleasedSUSERegistryForStableTags(t *testing.T) {
	candidates := webhookImageCandidates("v0.9.3")
	want := "registry.suse.com/rancher/rancher-webhook"
	if len(candidates) == 0 || candidates[0] != want {
		t.Fatalf("expected first stable candidate %s, got %v", want, candidates)
	}
}

func TestWebhookImageCandidatesPreferStagingForPrereleaseTags(t *testing.T) {
	candidates := webhookImageCandidates("v0.10.1-rc.5")
	want := "stgregistry.suse.com/rancher/rancher-webhook"
	if len(candidates) == 0 || candidates[0] != want {
		t.Fatalf("expected first prerelease candidate %s, got %v", want, candidates)
	}
}

func TestParsePrereleaseVersionAcceptsAlphaRCAndRCS(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "alpha", value: "v2.14.1-alpha6", want: "v2.14.1-alpha6"},
		{name: "rc without prefix", value: "2.15.0-rc2", want: "v2.15.0-rc2"},
		{name: "rcs without prefix", value: "2.16.0-rcs-0844.1", want: "v2.16.0-rcs-0844.1"},
		{name: "compact rcs without prefix", value: "2.15.1-rcs-c936", want: "v2.15.1-rcs-c936"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePrereleaseVersion(tt.value)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Raw != tt.want {
				t.Fatalf("expected %s, got %s", tt.want, got.Raw)
			}
		})
	}
}

func TestParseTargetVersionAcceptsHeadForms(t *testing.T) {
	fullSHA := strings.Repeat("a", 40)
	tests := []struct {
		name           string
		value          string
		wantRaw        string
		wantMajor      int
		wantMinor      int
		wantPatch      int
		wantPatchKnown bool
		wantCommit     string
	}{
		{name: "plain", value: "head", wantRaw: "head"},
		{name: "minor alias", value: "2.16-head", wantRaw: "v2.16-head", wantMajor: 2, wantMinor: 16},
		{name: "patch alias", value: "2.15.1-head", wantRaw: "v2.15.1-head", wantMajor: 2, wantMinor: 15, wantPatch: 1, wantPatchKnown: true},
		{name: "community commit", value: "v2.14-abcdef0-head", wantRaw: "v2.14-abcdef0-head", wantMajor: 2, wantMinor: 14, wantCommit: "abcdef0"},
		{name: "prime patch commit", value: "2.14.5-" + fullSHA + "-head", wantRaw: "v2.14.5-" + fullSHA + "-head", wantMajor: 2, wantMinor: 14, wantPatch: 5, wantPatchKnown: true, wantCommit: fullSHA},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTargetVersion(tt.value)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Raw != tt.wantRaw || got.Kind != "head" || got.Major != tt.wantMajor || got.Minor != tt.wantMinor || got.Patch != tt.wantPatch || got.PatchSpecified != tt.wantPatchKnown || got.Commit != tt.wantCommit {
				t.Fatalf("parseTargetVersion(%q) = %#v", tt.value, got)
			}
		})
	}
}

func TestParseTargetVersionRejectsMalformedHeadCommit(t *testing.T) {
	for _, value := range []string{
		"v2.14-abc123-head",
		"v2.14-nothex00-head",
		"v2.14.5-xyz9876-head",
	} {
		t.Run(value, func(t *testing.T) {
			if _, err := parseTargetVersion(value); err == nil {
				t.Fatalf("expected %q to be rejected", value)
			}
		})
	}
}

func TestBuildPlanAcceptsCommunityCommitHeadAndPinsExactImages(t *testing.T) {
	const (
		tagSHA    = "abcdef0"
		fullSHA   = "abcdef0123456789abcdef0123456789abcdef01"
		targetTag = "v2.14-" + tagSHA + "-head"
		serverRef = "stgregistry.suse.com/rancher/rancher:" + targetTag
		agentRef  = "stgregistry.suse.com/rancher/rancher-agent:" + targetTag
	)
	client := fakeGitHubClient(t, map[string]string{
		"/rancher/rancher/" + fullSHA + "/build.yaml":             `webhookVersion: 109.0.6+up0.10.10-rc.3`,
		"/rancher/rancher/v2.14.4/build.yaml":                     `webhookVersion: 109.0.5+up0.10.9`,
		"/stg/v2/rancher/rancher-webhook/manifests/v0.10.10-rc.3": "ok",
	})
	client.inspectImage = fixtureImageInspector(map[string]imageInspectionFixture{
		serverRef: {
			Found: true,
			Metadata: rancherImageMetadata{
				Digest:             "sha256:" + strings.Repeat("1", 64),
				Source:             "https://github.com/rancher/rancher",
				Revision:           fullSHA,
				CanonicalReference: "rancher/rancher:" + targetTag,
			},
		},
		agentRef: {
			Found:    true,
			Metadata: rancherImageMetadata{Digest: "sha256:" + strings.Repeat("2", 64), CanonicalReference: "rancher/rancher-agent:" + targetTag},
		},
	})

	got, err := buildPlan(context.Background(), client, targetTag, "v2.14.4", "stgregistry.suse.com/rancher/rancher-webhook:v0.10.10-rc.3", "auto", "", "rancher-runway/signoff", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.TargetVersion != targetTag || got.ResolvedTargetVersion != "" || got.ReleaseLine != "v2.14" {
		t.Fatalf("unexpected target identity: %#v", got)
	}
	if got.RancherDistro != "auto" || got.RancherImageRegistry != "stgregistry.suse.com" || got.RancherImage != serverRef || got.RancherAgentImage != agentRef {
		t.Fatalf("unexpected Rancher image resolution: %#v", got)
	}
	if got.RancherImageDigest != "sha256:"+strings.Repeat("1", 64) || got.RancherAgentDigest != "sha256:"+strings.Repeat("2", 64) || got.RancherImageRevision != fullSHA {
		t.Fatalf("unexpected Rancher provenance: %#v", got)
	}
	if got.Lanes[0].InstallRancher != serverRef || got.Lanes[1].InstallRancher != serverRef {
		t.Fatalf("fresh target lanes were not pinned to %s: %#v", serverRef, got.Lanes)
	}
	if got.Lanes[2].InstallRancher != "v2.14.4" || got.Lanes[2].UpgradeToRancher != serverRef {
		t.Fatalf("upgrade lane did not keep the stable install and exact target image: %#v", got.Lanes[2])
	}
}

func TestBuildPlanMarksPatchQualifiedPrimeHead(t *testing.T) {
	const (
		ossSHA     = "97845ced7ee6df9a36cae65ded9bbb73e14500b5"
		privateSHA = "a4af84edd99705d3dc9b36a60fc06131e4afd6ee"
		targetTag  = "v2.14.5-" + ossSHA + "-head"
		serverRef  = "stgregistry.suse.com/rancher/rancher:" + targetTag
		agentRef   = "stgregistry.suse.com/rancher/rancher-agent:" + targetTag
	)
	client := fakeGitHubClient(t, map[string]string{
		"/rancher/rancher/" + ossSHA + "/build.yaml":              `webhookVersion: 109.0.6+up0.10.10-rc.3`,
		"/rancher/rancher/v2.14.4/build.yaml":                     `webhookVersion: 109.0.5+up0.10.9`,
		"/stg/v2/rancher/rancher-webhook/manifests/v0.10.10-rc.3": "ok",
	})
	client.inspectImage = fixtureImageInspector(map[string]imageInspectionFixture{
		serverRef: {
			Found: true,
			Metadata: rancherImageMetadata{
				Digest:             "sha256:" + strings.Repeat("3", 64),
				Source:             "https://github.com/rancher/rancher-prime.git",
				Revision:           privateSHA,
				OSSRevision:        ossSHA,
				CanonicalReference: "rancher/rancher:" + targetTag,
			},
		},
		agentRef: {Found: true, Metadata: rancherImageMetadata{Digest: "sha256:" + strings.Repeat("4", 64), CanonicalReference: "rancher/rancher-agent:" + targetTag}},
	})

	got, err := buildPlan(context.Background(), client, targetTag, "v2.14.4", "stgregistry.suse.com/rancher/rancher-webhook:v0.10.10-rc.3", "auto", "", "rancher-runway/signoff", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.RancherDistro != "prime" || got.RancherImageSource != "https://github.com/rancher/rancher-prime.git" || got.RancherImageRevision != privateSHA || got.RancherOSSRevision != ossSHA {
		t.Fatalf("unexpected Prime provenance: %#v", got)
	}
	if got.Lanes[0].InstallRancherDistro != "prime" || got.Lanes[1].InstallRancherDistro != "prime" {
		t.Fatalf("fresh Prime lanes did not use Prime distro: %#v", got.Lanes)
	}
	if got.Lanes[2].InstallRancherDistro != "auto" || got.Lanes[2].UpgradeToRancherDistro != "prime" {
		t.Fatalf("upgrade lane did not separate baseline and target distros: %#v", got.Lanes[2])
	}
	if len(got.Lanes) < 4 || got.Lanes[3].InstallRancherDistro != "auto" {
		t.Fatalf("candidate-on-previous lane did not use automatic baseline distro: %#v", got.Lanes)
	}
	if got.Lanes[2].UpgradeToRancher != serverRef {
		t.Fatalf("Prime upgrade target was not pinned: %#v", got.Lanes[2])
	}
}

func TestBuildPlanPinsMovingHeadAliasToCanonicalPair(t *testing.T) {
	const (
		sha          = "1234567890abcdef1234567890abcdef12345678"
		aliasTag     = "v2.14-head"
		canonicalTag = "v2.14-" + sha + "-head"
		aliasServer  = "stgregistry.suse.com/rancher/rancher:" + aliasTag
		aliasAgent   = "stgregistry.suse.com/rancher/rancher-agent:" + aliasTag
		exactServer  = "stgregistry.suse.com/rancher/rancher:" + canonicalTag
		exactAgent   = "stgregistry.suse.com/rancher/rancher-agent:" + canonicalTag
	)
	client := fakeGitHubClient(t, map[string]string{
		"/rancher/rancher/" + sha + "/build.yaml":                 `webhookVersion: 109.0.6+up0.10.10-rc.3`,
		"/rancher/rancher/v2.14.4/build.yaml":                     `webhookVersion: 109.0.5+up0.10.9`,
		"/stg/v2/rancher/rancher-webhook/manifests/v0.10.10-rc.3": "ok",
	})
	digest := "sha256:" + strings.Repeat("5", 64)
	var calls []string
	client.inspectImage = recordingFixtureImageInspector(&calls, map[string]imageInspectionFixture{
		aliasServer: {Found: true, Metadata: rancherImageMetadata{Digest: digest, Source: "https://github.com/rancher/rancher", Revision: sha, CanonicalReference: "rancher/rancher:" + canonicalTag}},
		aliasAgent:  {Found: true, Metadata: rancherImageMetadata{Digest: "sha256:" + strings.Repeat("6", 64), CanonicalReference: "rancher/rancher-agent:" + canonicalTag}},
		exactServer: {Found: true, Metadata: rancherImageMetadata{Digest: digest, Source: "https://github.com/rancher/rancher", Revision: sha, CanonicalReference: "rancher/rancher:" + canonicalTag}},
		exactAgent:  {Found: true, Metadata: rancherImageMetadata{Digest: "sha256:" + strings.Repeat("6", 64), CanonicalReference: "rancher/rancher-agent:" + canonicalTag}},
	})

	got, err := buildPlan(context.Background(), client, aliasTag, "v2.14.4", "stgregistry.suse.com/rancher/rancher-webhook:v0.10.10-rc.3", "auto", "", "rancher-runway/signoff", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.TargetVersion != aliasTag || got.ResolvedTargetVersion != canonicalTag {
		t.Fatalf("expected requested alias plus resolved tag, got target=%s resolved=%s", got.TargetVersion, got.ResolvedTargetVersion)
	}
	if got.RancherImage != exactServer || got.RancherAgentImage != exactAgent || got.Lanes[0].InstallRancher != exactServer || got.Lanes[2].UpgradeToRancher != exactServer {
		t.Fatalf("moving alias was not pinned to its exact pair: %#v", got)
	}
	for _, call := range calls {
		if strings.HasPrefix(call, "docker.io/") {
			t.Fatalf("staging pair was complete, but Docker Hub was inspected: %v", calls)
		}
	}
}

func TestBuildPlanResolvesPatchHeadAliasToNewestStagingPair(t *testing.T) {
	const (
		requestedTag = "v2.15.1-head"
		selectedSHA  = "1f680e71accf728c75478ff6b728d59c9f9a7b8b"
		olderSHA     = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		privateSHA   = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		selectedTag  = "v2.15.1-" + selectedSHA + "-head"
		olderTag     = "v2.15.1-" + olderSHA + "-head"
		serverRef    = "stgregistry.suse.com/rancher/rancher:" + selectedTag
		agentRef     = "stgregistry.suse.com/rancher/rancher-agent:" + selectedTag
	)
	client := fakeGitHubClient(t, map[string]string{
		"/rancher/rancher/" + selectedSHA + "/build.yaml":        `webhookVersion: 110.0.1+up0.11.1-rc.4`,
		"/rancher/rancher/v2.15.0/build.yaml":                    `webhookVersion: 110.0.0+up0.11.0`,
		"/stg/v2/rancher/rancher-webhook/manifests/v0.11.1-rc.4": "ok",
	})
	var listCalls []string
	client.listImageTags = func(_ context.Context, registry, repository string) ([]string, error) {
		listCalls = append(listCalls, registry+"/"+repository)
		return []string{
			requestedTag,
			olderTag,
			selectedTag,
			"v2.15.2-" + selectedSHA + "-head",
			"v2.15.1-rcs-c936",
		}, nil
	}
	var inspectCalls []string
	client.inspectImage = recordingFixtureImageInspector(&inspectCalls, map[string]imageInspectionFixture{
		"stgregistry.suse.com/rancher/rancher:" + olderTag: {
			Found: true,
			Metadata: rancherImageMetadata{
				Source:             "https://github.com/rancher/rancher-prime.git",
				Revision:           privateSHA,
				OSSRevision:        olderSHA,
				CanonicalReference: "rancher/rancher:" + olderTag,
				Created:            time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC),
			},
		},
		"stgregistry.suse.com/rancher/rancher-agent:" + olderTag: {
			Found: true,
			Metadata: rancherImageMetadata{
				CanonicalReference: "rancher/rancher-agent:" + olderTag,
				Created:            time.Date(2026, 8, 22, 12, 1, 0, 0, time.UTC),
			},
		},
		serverRef: {
			Found: true,
			Metadata: rancherImageMetadata{
				Digest:             "sha256:" + strings.Repeat("c", 64),
				Source:             "https://github.com/rancher/rancher-prime.git",
				Revision:           privateSHA,
				OSSRevision:        selectedSHA,
				CanonicalReference: "rancher/rancher:" + selectedTag,
				Created:            time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC),
			},
		},
		agentRef: {
			Found: true,
			Metadata: rancherImageMetadata{
				Digest:             "sha256:" + strings.Repeat("d", 64),
				CanonicalReference: "rancher/rancher-agent:" + selectedTag,
				Created:            time.Date(2026, 8, 23, 12, 1, 0, 0, time.UTC),
			},
		},
	})

	got, err := buildPlan(context.Background(), client, requestedTag, "v2.15.0", "stgregistry.suse.com/rancher/rancher-webhook:v0.11.1-rc.4", "auto", "", "rancher-runway/signoff", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.TargetVersion != requestedTag || got.ResolvedTargetVersion != selectedTag {
		t.Fatalf("expected requested patch alias %s to resolve to %s, got target=%s resolved=%s", requestedTag, selectedTag, got.TargetVersion, got.ResolvedTargetVersion)
	}
	if got.RancherImageRegistry != "stgregistry.suse.com" || got.RancherImage != serverRef || got.RancherAgentImage != agentRef || got.RancherDistro != "prime" {
		t.Fatalf("patch alias did not pin the staging Prime pair: %#v", got)
	}
	if got.RancherOSSRevision != selectedSHA || got.Lanes[2].UpgradeToRancher != serverRef {
		t.Fatalf("patch alias did not retain selected provenance and image reference: %#v", got)
	}
	if len(listCalls) != 1 || listCalls[0] != "stgregistry.suse.com/rancher/rancher" {
		t.Fatalf("expected one staging tag listing, got %v", listCalls)
	}
	for _, call := range inspectCalls {
		if strings.HasPrefix(call, "docker.io/") {
			t.Fatalf("patch-qualified alias must not inspect Docker Hub: %v", inspectCalls)
		}
		if strings.HasSuffix(call, ":"+requestedTag) {
			t.Fatalf("patch-qualified alias must discover immutable tags instead of requiring a literal alias image: %v", inspectCalls)
		}
		if strings.Contains(call, "/rancher/rancher:v2.15.2-") {
			t.Fatalf("a different patch was inspected: %v", inspectCalls)
		}
	}
}

func TestCanonicalHeadTargetRejectsPatchAliasMismatch(t *testing.T) {
	const sha = "1f680e71accf728c75478ff6b728d59c9f9a7b8b"
	requested, err := parseTargetVersion("v2.15.1-head")
	if err != nil {
		t.Fatal(err)
	}
	_, err = canonicalHeadTarget(requested, rancherImageMetadata{
		CanonicalReference: "rancher/rancher:v2.15.2-" + sha + "-head",
	})
	if err == nil || !strings.Contains(err.Error(), "different patch") {
		t.Fatalf("expected exact patch mismatch to be rejected, got %v", err)
	}
}

func TestResolvePatchHeadAliasRejectsCanonicalPatchMismatch(t *testing.T) {
	const (
		sha          = "1f680e71accf728c75478ff6b728d59c9f9a7b8b"
		requestedTag = "v2.15.1-head"
		listedTag    = "v2.15.1-" + sha + "-head"
	)
	var calls []string
	client := githubClient{
		listImageTags: func(_ context.Context, registry, repository string) ([]string, error) {
			if registry != "stgregistry.suse.com" || repository != rancherRepo {
				t.Fatalf("unexpected tag listing target %s/%s", registry, repository)
			}
			return []string{listedTag}, nil
		},
		inspectImage: recordingFixtureImageInspector(&calls, map[string]imageInspectionFixture{
			"stgregistry.suse.com/rancher/rancher:" + listedTag: {
				Found: true,
				Metadata: rancherImageMetadata{
					CanonicalReference: "rancher/rancher:v2.15.2-" + sha + "-head",
					Created:            time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC),
				},
			},
		}),
	}
	requested, err := parseTargetVersion(requestedTag)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = client.resolveHeadTarget(context.Background(), requested)
	if err == nil || !strings.Contains(err.Error(), "different patch") {
		t.Fatalf("expected discovered canonical patch mismatch to be rejected, got %v", err)
	}
	for _, call := range calls {
		if strings.HasPrefix(call, "docker.io/") {
			t.Fatalf("patch alias mismatch must not trigger Docker Hub fallback: %v", calls)
		}
	}
}

func TestResolvePatchHeadAliasRequiresBothCreationTimestamps(t *testing.T) {
	const (
		sha          = "1f680e71accf728c75478ff6b728d59c9f9a7b8b"
		requestedTag = "v2.15.1-head"
		resolvedTag  = "v2.15.1-" + sha + "-head"
	)
	for _, missingRole := range []string{"server", "agent"} {
		t.Run(missingRole, func(t *testing.T) {
			serverCreated := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
			agentCreated := serverCreated.Add(time.Minute)
			if missingRole == "server" {
				serverCreated = time.Time{}
			} else {
				agentCreated = time.Time{}
			}
			client := githubClient{
				listImageTags: func(_ context.Context, registry, repository string) ([]string, error) {
					return []string{resolvedTag}, nil
				},
				inspectImage: fixtureImageInspector(map[string]imageInspectionFixture{
					"stgregistry.suse.com/rancher/rancher:" + resolvedTag: {
						Found: true,
						Metadata: rancherImageMetadata{
							Source:             "https://github.com/rancher/rancher-prime",
							OSSRevision:        sha,
							CanonicalReference: "rancher/rancher:" + resolvedTag,
							Created:            serverCreated,
						},
					},
					"stgregistry.suse.com/rancher/rancher-agent:" + resolvedTag: {
						Found: true,
						Metadata: rancherImageMetadata{
							CanonicalReference: "rancher/rancher-agent:" + resolvedTag,
							Created:            agentCreated,
						},
					},
				}),
			}
			requested, err := parseTargetVersion(requestedTag)
			if err != nil {
				t.Fatal(err)
			}
			_, _, _, err = client.resolveHeadTarget(context.Background(), requested)
			if err == nil || !strings.Contains(err.Error(), missingRole+" image") || !strings.Contains(err.Error(), "creation time") {
				t.Fatalf("expected missing %s creation time to fail, got %v", missingRole, err)
			}
		})
	}
}

func TestResolvePatchHeadAliasRanksByCompletePairTime(t *testing.T) {
	const (
		requestedTag = "v2.15.1-head"
		serverNewSHA = "1f680e71accf728c75478ff6b728d59c9f9a7b8b"
		pairNewSHA   = "2e25858ec600c4f6ae1c005f9f4eb7683ec9fff0"
		serverNewTag = "v2.15.1-" + serverNewSHA + "-head"
		pairNewTag   = "v2.15.1-" + pairNewSHA + "-head"
	)
	serverNewTime := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	pairNewTime := serverNewTime.Add(24 * time.Hour)
	fixtures := map[string]imageInspectionFixture{}
	for _, candidate := range []struct {
		tag        string
		sha        string
		serverTime time.Time
		agentTime  time.Time
	}{
		{tag: serverNewTag, sha: serverNewSHA, serverTime: serverNewTime, agentTime: serverNewTime},
		{tag: pairNewTag, sha: pairNewSHA, serverTime: serverNewTime.Add(-24 * time.Hour), agentTime: pairNewTime},
	} {
		fixtures["stgregistry.suse.com/rancher/rancher:"+candidate.tag] = imageInspectionFixture{
			Found: true,
			Metadata: rancherImageMetadata{
				Source:             "https://github.com/rancher/rancher-prime",
				OSSRevision:        candidate.sha,
				CanonicalReference: "rancher/rancher:" + candidate.tag,
				Created:            candidate.serverTime,
			},
		}
		fixtures["stgregistry.suse.com/rancher/rancher-agent:"+candidate.tag] = imageInspectionFixture{
			Found: true,
			Metadata: rancherImageMetadata{
				CanonicalReference: "rancher/rancher-agent:" + candidate.tag,
				Created:            candidate.agentTime,
			},
		}
	}
	client := githubClient{
		listImageTags: func(_ context.Context, registry, repository string) ([]string, error) {
			return []string{serverNewTag, pairNewTag}, nil
		},
		inspectImage: fixtureImageInspector(fixtures),
	}
	requested, err := parseTargetVersion(requestedTag)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := client.resolvePatchHeadImagePair(context.Background(), requested)
	if err != nil {
		t.Fatalf("resolve patch alias: %v", err)
	}
	if want := "stgregistry.suse.com/rancher/rancher:" + pairNewTag; pair.Server.Reference != want {
		t.Fatalf("selected server = %q, want newest complete pair %q", pair.Server.Reference, want)
	}
}

func TestResolvePatchHeadAliasFailsClosedOnLookupError(t *testing.T) {
	const (
		requestedTag = "v2.15.1-head"
		newerSHA     = "1f680e71accf728c75478ff6b728d59c9f9a7b8b"
		olderSHA     = "2e25858ec600c4f6ae1c005f9f4eb7683ec9fff0"
		newerTag     = "v2.15.1-" + newerSHA + "-head"
		olderTag     = "v2.15.1-" + olderSHA + "-head"
	)
	client := githubClient{
		listImageTags: func(_ context.Context, registry, repository string) ([]string, error) {
			return []string{newerTag, olderTag}, nil
		},
		inspectImage: fixtureImageInspector(map[string]imageInspectionFixture{
			"stgregistry.suse.com/rancher/rancher:" + newerTag: {
				Err: errors.New("registry returned 429 Too Many Requests"),
			},
			"stgregistry.suse.com/rancher/rancher:" + olderTag: {
				Found: true,
				Metadata: rancherImageMetadata{
					Source:             "https://github.com/rancher/rancher-prime",
					OSSRevision:        olderSHA,
					CanonicalReference: "rancher/rancher:" + olderTag,
					Created:            time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC),
				},
			},
			"stgregistry.suse.com/rancher/rancher-agent:" + olderTag: {
				Found: true,
				Metadata: rancherImageMetadata{
					CanonicalReference: "rancher/rancher-agent:" + olderTag,
					Created:            time.Date(2026, 8, 22, 12, 1, 0, 0, time.UTC),
				},
			},
		}),
	}
	requested, err := parseTargetVersion(requestedTag)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = client.resolveHeadTarget(context.Background(), requested)
	if err == nil || !strings.Contains(err.Error(), "429 Too Many Requests") || !strings.Contains(err.Error(), "could not safely resolve") {
		t.Fatalf("expected lookup error to prevent stale fallback, got %v", err)
	}
}

func TestResolveHeadTargetUsesDockerOnlyForPlainHead(t *testing.T) {
	const (
		sha          = "abcdef0123456789abcdef0123456789abcdef01"
		canonicalTag = "v2.16-" + sha + "-head"
		aliasServer  = "docker.io/rancher/rancher:head"
		aliasAgent   = "docker.io/rancher/rancher-agent:head"
		exactServer  = "docker.io/rancher/rancher:" + canonicalTag
		exactAgent   = "docker.io/rancher/rancher-agent:" + canonicalTag
	)
	var calls []string
	client := githubClient{inspectImage: recordingFixtureImageInspector(&calls, map[string]imageInspectionFixture{
		aliasServer: {Found: true, Metadata: rancherImageMetadata{Digest: "sha256:plain", Source: "https://github.com/rancher/rancher", Revision: sha, Version: "main", CanonicalReference: "rancher/rancher:" + canonicalTag}},
		aliasAgent:  {Found: true, Metadata: rancherImageMetadata{Digest: "sha256:plain-agent", CanonicalReference: "rancher/rancher-agent:" + canonicalTag}},
		exactServer: {Found: true, Metadata: rancherImageMetadata{Digest: "sha256:plain", Source: "https://github.com/rancher/rancher", Revision: sha, CanonicalReference: "rancher/rancher:" + canonicalTag}},
		exactAgent:  {Found: true, Metadata: rancherImageMetadata{Digest: "sha256:plain-agent", CanonicalReference: "rancher/rancher-agent:" + canonicalTag}},
	})}
	requested, err := parseTargetVersion("head")
	if err != nil {
		t.Fatal(err)
	}
	resolved, pair, buildRef, err := client.resolveHeadTarget(context.Background(), requested)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved.Raw != canonicalTag || pair.Registry != "docker.io" || pair.Server.Reference != exactServer || buildRef != sha {
		t.Fatalf("unexpected plain head resolution: target=%#v pair=%#v buildRef=%s", resolved, pair, buildRef)
	}
	for _, call := range calls {
		if strings.HasPrefix(call, "stgregistry.suse.com/") {
			t.Fatalf("plain head must not inspect staging: %v", calls)
		}
	}
}

func TestResolveMovingPrimeHeadUsesOSSRevision(t *testing.T) {
	const (
		ossSHA       = "97845ced7ee6df9a36cae65ded9bbb73e14500b5"
		privateSHA   = "a4af84edd99705d3dc9b36a60fc06131e4afd6ee"
		aliasTag     = "v2.14-head"
		canonicalTag = "v2.14.5-" + ossSHA + "-head"
		aliasServer  = "stgregistry.suse.com/rancher/rancher:" + aliasTag
		aliasAgent   = "stgregistry.suse.com/rancher/rancher-agent:" + aliasTag
		exactServer  = "stgregistry.suse.com/rancher/rancher:" + canonicalTag
		exactAgent   = "stgregistry.suse.com/rancher/rancher-agent:" + canonicalTag
	)
	serverMetadata := rancherImageMetadata{
		Digest:             "sha256:" + strings.Repeat("8", 64),
		Source:             "https://github.com/rancher/rancher-prime.git",
		Revision:           privateSHA,
		OSSRevision:        ossSHA,
		CanonicalReference: "rancher/rancher:" + canonicalTag,
	}
	client := githubClient{inspectImage: fixtureImageInspector(map[string]imageInspectionFixture{
		aliasServer: {Found: true, Metadata: serverMetadata},
		aliasAgent:  {Found: true, Metadata: rancherImageMetadata{Digest: "sha256:" + strings.Repeat("9", 64), CanonicalReference: "rancher/rancher-agent:" + canonicalTag}},
		exactServer: {Found: true, Metadata: serverMetadata},
		exactAgent:  {Found: true, Metadata: rancherImageMetadata{Digest: "sha256:" + strings.Repeat("9", 64), CanonicalReference: "rancher/rancher-agent:" + canonicalTag}},
	})}
	requested, err := parseTargetVersion(aliasTag)
	if err != nil {
		t.Fatal(err)
	}
	resolved, pair, buildRef, err := client.resolveHeadTarget(context.Background(), requested)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved.Raw != canonicalTag || !resolved.PatchSpecified || resolved.Patch != 5 || pair.Server.Reference != exactServer || buildRef != ossSHA {
		t.Fatalf("unexpected Prime alias resolution: target=%#v pair=%#v buildRef=%s", resolved, pair, buildRef)
	}
}

func TestResolveHeadTargetDoesNotMixRegistries(t *testing.T) {
	const (
		tag     = "v2.15-abcdef0-head"
		fullSHA = "abcdef0123456789abcdef0123456789abcdef01"
	)
	client := githubClient{inspectImage: fixtureImageInspector(map[string]imageInspectionFixture{
		"stgregistry.suse.com/rancher/rancher:" + tag:       {Found: true},
		"stgregistry.suse.com/rancher/rancher-agent:" + tag: {Found: false},
		"docker.io/rancher/rancher:" + tag:                  {Found: true, Metadata: rancherImageMetadata{Source: "https://github.com/rancher/rancher", Revision: fullSHA, CanonicalReference: "rancher/rancher:" + tag}},
		"docker.io/rancher/rancher-agent:" + tag:            {Found: true, Metadata: rancherImageMetadata{CanonicalReference: "rancher/rancher-agent:" + tag}},
	})}
	requested, err := parseTargetVersion(tag)
	if err != nil {
		t.Fatal(err)
	}
	_, pair, _, err := client.resolveHeadTarget(context.Background(), requested)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pair.Registry != "docker.io" || !strings.HasPrefix(pair.Server.Reference, "docker.io/") || !strings.HasPrefix(pair.Agent.Reference, "docker.io/") {
		t.Fatalf("expected one complete Docker pair, got %#v", pair)
	}
}

func TestResolvePatchCommitHeadUsesStagingOnly(t *testing.T) {
	const (
		sha = "1f680e71accf728c75478ff6b728d59c9f9a7b8b"
		tag = "v2.15.1-" + sha + "-head"
	)
	var calls []string
	client := githubClient{inspectImage: recordingFixtureImageInspector(&calls, map[string]imageInspectionFixture{
		"docker.io/rancher/rancher:" + tag: {
			Found: true,
			Metadata: rancherImageMetadata{
				Source:             "https://github.com/rancher/rancher-prime.git",
				OSSRevision:        sha,
				CanonicalReference: "rancher/rancher:" + tag,
			},
		},
		"docker.io/rancher/rancher-agent:" + tag: {
			Found:    true,
			Metadata: rancherImageMetadata{CanonicalReference: "rancher/rancher-agent:" + tag},
		},
	})}
	requested, err := parseTargetVersion(tag)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = client.resolveHeadTarget(context.Background(), requested)
	if err == nil || !strings.Contains(err.Error(), "stgregistry.suse.com") {
		t.Fatalf("expected missing staging patch head to fail, got %v", err)
	}
	for _, call := range calls {
		if strings.HasPrefix(call, "docker.io/") {
			t.Fatalf("patch-qualified commit head must not fall back to Docker Hub: %v", calls)
		}
	}
}

func TestResolvePatchCommitHeadRequiresPrimeProvenance(t *testing.T) {
	const (
		sha = "1f680e71accf728c75478ff6b728d59c9f9a7b8b"
		tag = "v2.15.1-" + sha + "-head"
	)
	client := githubClient{inspectImage: fixtureImageInspector(map[string]imageInspectionFixture{
		"stgregistry.suse.com/rancher/rancher:" + tag: {
			Found: true,
			Metadata: rancherImageMetadata{
				Source:             "https://github.com/rancher/rancher",
				Revision:           sha,
				CanonicalReference: "rancher/rancher:" + tag,
			},
		},
		"stgregistry.suse.com/rancher/rancher-agent:" + tag: {
			Found:    true,
			Metadata: rancherImageMetadata{CanonicalReference: "rancher/rancher-agent:" + tag},
		},
	})}
	requested, err := parseTargetVersion(tag)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = client.resolveHeadTarget(context.Background(), requested)
	if err == nil || !strings.Contains(err.Error(), "canonical Rancher Prime source") {
		t.Fatalf("expected patch-qualified head to require Prime provenance, got %v", err)
	}
}

func TestValidateHeadImagePairRejectsWrongCanonicalRepositories(t *testing.T) {
	const tag = "v2.15.1-1f680e7-head"
	err := validateHeadImagePair(tag, rancherImagePair{
		Server: rancherImageMetadata{CanonicalReference: "rancher/rancher-agent:" + tag},
		Agent:  rancherImageMetadata{CanonicalReference: "rancher/rancher-agent:" + tag},
	})
	if err == nil || !strings.Contains(err.Error(), "unexpected canonical repositories") {
		t.Fatalf("expected canonical repository roles to be enforced, got %v", err)
	}
}

func TestResolveHeadTargetRejectsCommitProvenanceMismatch(t *testing.T) {
	const tag = "v2.15-abcdef0-head"
	client := githubClient{inspectImage: fixtureImageInspector(map[string]imageInspectionFixture{
		"stgregistry.suse.com/rancher/rancher:" + tag: {
			Found: true,
			Metadata: rancherImageMetadata{
				Source:             "https://github.com/rancher/rancher",
				Revision:           "1234567890abcdef1234567890abcdef12345678",
				CanonicalReference: "rancher/rancher:" + tag,
			},
		},
		"stgregistry.suse.com/rancher/rancher-agent:" + tag: {Found: true, Metadata: rancherImageMetadata{CanonicalReference: "rancher/rancher-agent:" + tag}},
	})}
	requested, err := parseTargetVersion(tag)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = client.resolveHeadTarget(context.Background(), requested)
	if err == nil || !strings.Contains(err.Error(), "image provenance identifies public revision") {
		t.Fatalf("expected provenance mismatch, got %v", err)
	}
}

func TestResolveHeadTargetRejectsMismatchedAgentCanonicalTag(t *testing.T) {
	const (
		tag     = "v2.15-abcdef0-head"
		fullSHA = "abcdef0123456789abcdef0123456789abcdef01"
	)
	client := githubClient{inspectImage: fixtureImageInspector(map[string]imageInspectionFixture{
		"stgregistry.suse.com/rancher/rancher:" + tag: {
			Found: true,
			Metadata: rancherImageMetadata{
				Source:             "https://github.com/rancher/rancher",
				Revision:           fullSHA,
				CanonicalReference: "rancher/rancher:" + tag,
			},
		},
		"stgregistry.suse.com/rancher/rancher-agent:" + tag: {
			Found: true,
			Metadata: rancherImageMetadata{
				CanonicalReference: "rancher/rancher-agent:v2.15-1234567-head",
			},
		},
	})}
	requested, err := parseTargetVersion(tag)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = client.resolveHeadTarget(context.Background(), requested)
	if err == nil || !strings.Contains(err.Error(), "mismatched canonical tags") {
		t.Fatalf("expected mismatched agent provenance to be rejected, got %v", err)
	}
}

func TestBuildPlanAcceptsRancherRCSTag(t *testing.T) {
	client := fakeGitHubClient(t, map[string]string{
		"/rancher/rancher/v2.16.0-rcs-0844.1/build.yaml":               `webhookVersion: 111.0.0+up0.12.1-rcs-0844.1`,
		"/rancher/rancher/v2.15.3/build.yaml":                          `webhookVersion: 110.0.3+up0.11.3`,
		"/stg/v2/rancher/rancher-webhook/manifests/v0.12.1-rcs-0844.1": "ok",
	})

	got, err := buildPlan(context.Background(), client, "2.16.0-rcs-0844.1", "v2.15.3", "stgregistry.suse.com/rancher/rancher-webhook:v0.12.1-rcs-0844.1", "auto", "", "rancher-runway/signoff", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.TargetVersion != "v2.16.0-rcs-0844.1" {
		t.Fatalf("expected RCS target version, got %s", got.TargetVersion)
	}
	if got.TargetWebhookTag != "v0.12.1-rcs-0844.1" {
		t.Fatalf("expected RCS webhook tag, got %s", got.TargetWebhookTag)
	}
	if got.Lanes[2].UpgradeToRancher != "v2.16.0-rcs-0844.1" {
		t.Fatalf("expected upgrade lane to target RCS, got %#v", got.Lanes[2])
	}
}

func TestBuildPlanAcceptsCompactRancherRCSTag(t *testing.T) {
	client := fakeGitHubClient(t, map[string]string{
		"/rancher/rancher/v2.15.1-rcs-c936/build.yaml":               `webhookVersion: 110.0.1+up0.11.1-rcs-c936`,
		"/rancher/rancher/v2.15.0/build.yaml":                        `webhookVersion: 110.0.0+up0.11.0`,
		"/stg/v2/rancher/rancher-webhook/manifests/v0.11.1-rcs-c936": "ok",
	})

	got, err := buildPlan(context.Background(), client, "2.15.1-rcs-c936", "v2.15.0", "stgregistry.suse.com/rancher/rancher-webhook:v0.11.1-rcs-c936", "auto", "", "rancher-runway/signoff", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.TargetVersion != "v2.15.1-rcs-c936" {
		t.Fatalf("expected compact RCS target version, got %s", got.TargetVersion)
	}
	if got.TargetWebhookTag != "v0.11.1-rcs-c936" {
		t.Fatalf("expected compact RCS webhook tag, got %s", got.TargetWebhookTag)
	}
	if got.Lanes[2].UpgradeToRancher != "v2.15.1-rcs-c936" {
		t.Fatalf("expected upgrade lane to target compact RCS, got %#v", got.Lanes[2])
	}
}

func TestParsePrereleaseVersionRejectsStableRelease(t *testing.T) {
	_, err := parsePrereleaseVersion("v2.15.0")
	if err == nil {
		t.Fatal("expected stable release to be rejected")
	}
}

func TestBuildPlanAcceptsRancherRCTag(t *testing.T) {
	client := fakeGitHubClient(t, map[string]string{
		"/repos/rancher/rancher/releases":                        `[{"tag_name":"v2.15.0-rc2","prerelease":true},{"tag_name":"v2.14.3","prerelease":false}]`,
		"/rancher/rancher/v2.15.0-rc2/build.yaml":                `webhookVersion: 110.0.0+up0.11.0-rc.1`,
		"/rancher/rancher/v2.14.3/build.yaml":                    `webhookVersion: 109.0.3+up0.10.3`,
		"/stg/v2/rancher/rancher-webhook/manifests/v0.11.0-rc.1": "ok",
	})

	plan, err := buildPlan(context.Background(), client, "v2.15.0-rc2", "", "stgregistry.suse.com/rancher/rancher-webhook:v0.11.0-rc.1", "auto", "", "rancher-runway/signoff", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if plan.TargetVersion != "v2.15.0-rc2" {
		t.Fatalf("expected RC target version, got %s", plan.TargetVersion)
	}
	if plan.PreviousVersion != "v2.14.3" {
		t.Fatalf("expected previous release v2.14.3, got %s", plan.PreviousVersion)
	}
	if plan.Lanes[2].UpgradeToRancher != "v2.15.0-rc2" {
		t.Fatalf("expected upgrade lane to target RC, got %#v", plan.Lanes[2])
	}
}

func TestBuildPlanAddsOldWebhookLaneWhenWebhookChanged(t *testing.T) {
	client := fakeGitHubClient(t, map[string]string{
		"/rancher/rancher/v2.14.1-alpha6/build.yaml":             `webhookVersion: 109.0.1+up0.10.1-rc.5`,
		"/rancher/rancher/v2.14.0/build.yaml":                    `webhookVersion: 109.0.0+up0.10.0`,
		"/stg/v2/rancher/rancher-webhook/manifests/v0.10.1-rc.5": "ok",
	})

	plan, err := buildPlan(context.Background(), client, "v2.14.1-alpha6", "v2.14.0", "stgregistry.suse.com/rancher/rancher-webhook:v0.10.1-rc.5", "auto", "123456789", "rancher-runway/signoff", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !plan.WebhookChanged {
		t.Fatal("expected webhook to be marked changed")
	}
	if plan.SigningPolicy != "report-only" {
		t.Fatalf("expected report-only signing policy, got %s", plan.SigningPolicy)
	}
	if len(plan.Lanes) != 4 {
		t.Fatalf("expected 4 lanes, got %d", len(plan.Lanes))
	}
	if plan.Lanes[0].Name != laneFrameworkRegression {
		t.Fatalf("expected framework regression lane first, got %s", plan.Lanes[0].Name)
	}
	if plan.Lanes[0].ProvisionDownstream {
		t.Fatal("expected framework regression lane to skip downstream provisioning")
	}
	if plan.Lanes[1].Name != laneWebhookFreshInstall {
		t.Fatalf("expected webhook fresh install lane second, got %s", plan.Lanes[1].Name)
	}
	if !plan.Lanes[1].ProvisionDownstream {
		t.Fatal("expected webhook fresh install lane to provision downstream")
	}
	if plan.Lanes[2].Name != laneWebhookUpgrade {
		t.Fatalf("expected webhook upgrade lane third, got %s", plan.Lanes[2].Name)
	}
	if !plan.Lanes[2].ProvisionDownstream || plan.Lanes[2].UpgradeToRancher == "" {
		t.Fatal("expected webhook upgrade lane to provision downstream and upgrade")
	}
	if plan.Lanes[3].Name != laneWebhookCandidateOnPrevious {
		t.Fatalf("expected candidate-on-previous webhook lane, got %s", plan.Lanes[3].Name)
	}
	if plan.Lanes[3].WebhookOverrideImage == "" {
		t.Fatal("expected webhook override image")
	}
	if plan.Lanes[3].TerraformStateKey != "rancher-runway/signoff/v2.14/v2.14.1-alpha6/123456789/webhook-candidate-on-previous/terraform.tfstate" {
		t.Fatalf("unexpected state key: %s", plan.Lanes[3].TerraformStateKey)
	}
	if plan.Lanes[3].AWSPrefix != "gha-23456789-wp" {
		t.Fatalf("unexpected AWS prefix: %s", plan.Lanes[3].AWSPrefix)
	}
}

func TestBuildPlanDiscoversStagingPrereleaseWebhookImageWhenNoOverride(t *testing.T) {
	client := fakeGitHubClient(t, map[string]string{
		"/rancher/rancher/v2.14.1-alpha6/build.yaml":                `webhookVersion: 109.0.1+up0.10.1-rc.5`,
		"/rancher/rancher/v2.14.0/build.yaml":                       `webhookVersion: 109.0.0+up0.10.0`,
		"/stg/v2/rancher/rancher-webhook/manifests/v0.10.1-rc.5":    "ok",
		"/suse/v2/rancher/rancher-webhook/manifests/v0.10.1-rc.5":   "missing",
		"/docker/v2/rancher/rancher-webhook/manifests/v0.10.1-rc.5": "ok",
	})
	client.registryBaseURLs = map[string]string{
		"stgregistry.suse.com": client.rawBaseURL + "/stg",
		"registry.rancher.com": client.rawBaseURL + "/prime",
		"registry.suse.com":    client.rawBaseURL + "/suse",
		"docker.io":            client.rawBaseURL + "/docker",
	}

	plan, err := buildPlan(context.Background(), client, "v2.14.1-alpha6", "v2.14.0", "", "auto", "", "rancher-runway/signoff", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantImage := "stgregistry.suse.com/rancher/rancher-webhook:v0.10.1-rc.5"
	if plan.WebhookImage != wantImage {
		t.Fatalf("expected %s, got %s", wantImage, plan.WebhookImage)
	}
	if plan.SigningPolicy != "report-only" {
		t.Fatalf("expected report-only signing policy, got %s", plan.SigningPolicy)
	}
	if plan.Lanes[3].WebhookOverrideImage != wantImage {
		t.Fatalf("expected candidate-on-previous webhook lane to use %s, got %s", wantImage, plan.Lanes[3].WebhookOverrideImage)
	}
}

func TestBuildPlanDiscoversReleasedWebhookImageForStableTagWhenNoOverride(t *testing.T) {
	client := fakeGitHubClient(t, map[string]string{
		"/rancher/rancher/v2.13.5-alpha6/build.yaml":          `webhookVersion: 108.0.3+up0.9.3`,
		"/rancher/rancher/v2.13.4/build.yaml":                 `webhookVersion: 108.0.3+up0.9.3`,
		"/suse/v2/rancher/rancher-webhook/manifests/v0.9.3":   "ok",
		"/stg/v2/rancher/rancher-webhook/manifests/v0.9.3":    "ok",
		"/docker/v2/rancher/rancher-webhook/manifests/v0.9.3": "ok",
	})
	client.registryBaseURLs = map[string]string{
		"stgregistry.suse.com": client.rawBaseURL + "/stg",
		"registry.rancher.com": client.rawBaseURL + "/prime",
		"registry.suse.com":    client.rawBaseURL + "/suse",
		"docker.io":            client.rawBaseURL + "/docker",
	}

	plan, err := buildPlan(context.Background(), client, "v2.13.5-alpha6", "v2.13.4", "", "auto", "", "rancher-runway/signoff", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantImage := "registry.suse.com/rancher/rancher-webhook:v0.9.3"
	if plan.WebhookImage != wantImage {
		t.Fatalf("expected %s, got %s", wantImage, plan.WebhookImage)
	}
	if plan.SigningPolicy != "report-only" {
		t.Fatalf("expected report-only signing policy, got %s", plan.SigningPolicy)
	}
}

func TestBuildPlanFallsBackToDockerHubWhenSUSERegistriesAreMissing(t *testing.T) {
	client := fakeGitHubClient(t, map[string]string{
		"/rancher/rancher/v2.14.1-alpha6/build.yaml":                `webhookVersion: 109.0.1+up0.10.1-rc.5`,
		"/rancher/rancher/v2.14.0/build.yaml":                       `webhookVersion: 109.0.0+up0.10.0`,
		"/docker/v2/rancher/rancher-webhook/manifests/v0.10.1-rc.5": "ok",
	})
	client.registryBaseURLs = map[string]string{
		"stgregistry.suse.com": client.rawBaseURL + "/stg",
		"registry.rancher.com": client.rawBaseURL + "/prime",
		"registry.suse.com":    client.rawBaseURL + "/suse",
		"docker.io":            client.rawBaseURL + "/docker",
	}

	plan, err := buildPlan(context.Background(), client, "v2.14.1-alpha6", "v2.14.0", "", "auto", "", "rancher-runway/signoff", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantImage := "docker.io/rancher/rancher-webhook:v0.10.1-rc.5"
	if plan.WebhookImage != wantImage {
		t.Fatalf("expected %s, got %s", wantImage, plan.WebhookImage)
	}
	if plan.SigningPolicy != "report-only" {
		t.Fatalf("expected report-only signing policy, got %s", plan.SigningPolicy)
	}
}

func TestBuildPlanFallsBackToPrimeBeforePublicSUSEAndDockerHub(t *testing.T) {
	client := fakeGitHubClient(t, map[string]string{
		"/rancher/rancher/v2.14.1-alpha6/build.yaml":                `webhookVersion: 109.0.1+up0.10.1-rc.5`,
		"/rancher/rancher/v2.14.0/build.yaml":                       `webhookVersion: 109.0.0+up0.10.0`,
		"/prime/v2/rancher/rancher-webhook/manifests/v0.10.1-rc.5":  "ok",
		"/suse/v2/rancher/rancher-webhook/manifests/v0.10.1-rc.5":   "ok",
		"/docker/v2/rancher/rancher-webhook/manifests/v0.10.1-rc.5": "ok",
	})

	plan, err := buildPlan(context.Background(), client, "v2.14.1-alpha6", "v2.14.0", "", "auto", "", "rancher-runway/signoff", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantImage := "registry.rancher.com/rancher/rancher-webhook:v0.10.1-rc.5"
	if plan.WebhookImage != wantImage {
		t.Fatalf("expected %s, got %s", wantImage, plan.WebhookImage)
	}
	if plan.SigningPolicy != "report-only" {
		t.Fatalf("expected report-only signing policy, got %s", plan.SigningPolicy)
	}
}

func TestBuildPlanFailsWhenExplicitWebhookImageTagMismatchesBuildYAML(t *testing.T) {
	client := fakeGitHubClient(t, map[string]string{
		"/rancher/rancher/v2.14.1-alpha6/build.yaml":             `webhookVersion: 109.0.1+up0.10.1-rc.5`,
		"/rancher/rancher/v2.14.0/build.yaml":                    `webhookVersion: 109.0.0+up0.10.0`,
		"/stg/v2/rancher/rancher-webhook/manifests/v0.10.0":      "ok",
		"/stg/v2/rancher/rancher-webhook/manifests/v0.10.1-rc.5": "ok",
	})

	_, err := buildPlan(context.Background(), client, "v2.14.1-alpha6", "v2.14.0", "stgregistry.suse.com/rancher/rancher-webhook:v0.10.0", "auto", "", "rancher-runway/signoff", "")
	if err == nil {
		t.Fatal("expected explicit mismatched webhook image tag to fail")
	}
	if !strings.Contains(err.Error(), "expected v0.10.1-rc.5") {
		t.Fatalf("expected tag mismatch error, got %v", err)
	}
}

func TestBuildPlanFailsWhenExplicitWebhookImageIsMissing(t *testing.T) {
	client := fakeGitHubClient(t, map[string]string{
		"/rancher/rancher/v2.14.1-alpha6/build.yaml": `webhookVersion: 109.0.1+up0.10.1-rc.5`,
		"/rancher/rancher/v2.14.0/build.yaml":        `webhookVersion: 109.0.0+up0.10.0`,
	})

	_, err := buildPlan(context.Background(), client, "v2.14.1-alpha6", "v2.14.0", "stgregistry.suse.com/rancher/rancher-webhook:v0.10.1-rc.5", "auto", "", "rancher-runway/signoff", "")
	if err == nil {
		t.Fatal("expected explicit missing webhook image to fail")
	}
	if !strings.Contains(err.Error(), "was not found") {
		t.Fatalf("expected missing image error, got %v", err)
	}
}

func TestRegistryImageTagExistsHandlesBearerChallenge(t *testing.T) {
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/auth":
			_, _ = w.Write([]byte(`{"token":"test-token"}`))
		case r.URL.Path == "/v2/rancher/rancher-webhook/manifests/v0.10.1-rc.5":
			if r.Header.Get("Authorization") != "Bearer test-token" {
				w.Header().Set("WWW-Authenticate", `Bearer realm="`+serverURL+`/auth",service="registry",scope="repository:rancher/rancher-webhook:pull"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte("ok"))
		default:
			http.NotFound(w, r)
		}
	}))
	serverURL = server.URL
	t.Cleanup(server.Close)

	client := githubClient{
		httpClient: server.Client(),
		registryBaseURLs: map[string]string{
			"stgregistry.suse.com": server.URL,
		},
	}
	found, err := client.registryImageTagExists(context.Background(), "stgregistry.suse.com", "rancher/rancher-webhook", "v0.10.1-rc.5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("expected tag to exist")
	}
}

func TestBuildPlanSkipsOldWebhookLaneWhenWebhookUnchanged(t *testing.T) {
	client := fakeGitHubClient(t, map[string]string{
		"/rancher/rancher/v2.14.1-alpha6/build.yaml":                `webhookVersion: 109.0.1+up0.10.1-rc.5`,
		"/rancher/rancher/v2.14.0/build.yaml":                       `webhookVersion: 109.0.1+up0.10.1-rc.5`,
		"/docker/v2/rancher/rancher-webhook/manifests/v0.10.1-rc.5": "ok",
	})

	plan, err := buildPlan(context.Background(), client, "v2.14.1-alpha6", "v2.14.0", "", "auto", "", "rancher-runway/signoff", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if plan.WebhookChanged {
		t.Fatal("expected webhook to be marked unchanged")
	}
	if plan.SigningPolicy != "report-only" {
		t.Fatalf("expected Docker Hub default to be report-only, got %s", plan.SigningPolicy)
	}
	if len(plan.Lanes) != 3 {
		t.Fatalf("expected 3 lanes, got %d", len(plan.Lanes))
	}
	if plan.Lanes[0].Name != laneFrameworkRegression {
		t.Fatalf("expected framework regression lane first, got %s", plan.Lanes[0].Name)
	}
	if plan.Lanes[0].ProvisionDownstream {
		t.Fatal("expected framework regression lane to skip downstream provisioning")
	}
	if plan.Lanes[1].Name != laneWebhookFreshInstall {
		t.Fatalf("expected webhook fresh install lane second, got %s", plan.Lanes[1].Name)
	}
	if plan.Lanes[2].Name != laneWebhookUpgrade {
		t.Fatalf("expected webhook upgrade lane third, got %s", plan.Lanes[2].Name)
	}
	if len(plan.SkippedLanes) != 1 || plan.SkippedLanes[0].Name != laneWebhookCandidateOnPrevious {
		t.Fatalf("expected skipped candidate-on-previous webhook lane, got %#v", plan.SkippedLanes)
	}
	if plan.Lanes[0].TerraformStateKey != "" {
		t.Fatalf("expected no state key without run id, got %s", plan.Lanes[0].TerraformStateKey)
	}
	if plan.Lanes[0].AWSPrefix != "local-fr" {
		t.Fatalf("unexpected local AWS prefix: %s", plan.Lanes[0].AWSPrefix)
	}
}

func TestBuildTerraformStateKey(t *testing.T) {
	got := buildTerraformStateKey("root/", "v2.14", "v2.14.1-alpha6", "123", laneWebhookFreshInstall)
	want := "root/v2.14/v2.14.1-alpha6/123/webhook-fresh-install/terraform.tfstate"
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestBuildLaneAWSPrefixIncludesOwnerBasePrefix(t *testing.T) {
	got := buildLaneAWSPrefix("123456789", laneWebhookUpgrade, "ATB")
	if got != "gha-atb-23456789-wu" {
		t.Fatalf("unexpected AWS prefix: %s", got)
	}
}

func TestBuildLaneAWSPrefixKeepsLegacyShapeWithoutOwnerBasePrefix(t *testing.T) {
	got := buildLaneAWSPrefix("123456789", laneWebhookUpgrade, "")
	if got != "gha-23456789-wu" {
		t.Fatalf("unexpected AWS prefix: %s", got)
	}
}

func TestLatestAlphasPerLineReturnsNewestRecentAlphaPerLine(t *testing.T) {
	targets := latestAlphasPerLineFromReleases([]release{
		{TagName: "v2.14.1-alpha7", Prerelease: true, PublishedAt: "2026-04-24T12:00:00Z"},
		{TagName: "v2.14.1-rc1", Prerelease: true, PublishedAt: "2026-04-25T12:00:00Z"},
		{TagName: "v2.13.5-alpha6", Prerelease: true, PublishedAt: "2026-04-24T11:00:00Z"},
		{TagName: "v2.14.1-alpha6", Prerelease: true, PublishedAt: "2026-04-23T12:00:00Z"},
		{TagName: "v2.12.9-alpha6", Prerelease: true, PublishedAt: "2026-04-24T10:00:00Z"},
		{TagName: "v2.15.0-alpha2", Prerelease: true, PublishedAt: "2026-03-01T12:00:00Z"},
		{TagName: "v2.14.0", Prerelease: false, PublishedAt: "2026-04-20T12:00:00Z"},
	}, time.Date(2026, 3, 26, 0, 0, 0, 0, time.UTC))
	want := []string{"v2.14.1-alpha7", "v2.13.5-alpha6", "v2.12.9-alpha6"}
	if strings.Join(targets, ",") != strings.Join(want, ",") {
		t.Fatalf("expected %v, got %v", want, targets)
	}
}

func TestLatestAlphasPerLineReturnsNoRecentAlphaError(t *testing.T) {
	client := fakeGitHubClient(t, map[string]string{
		"/repos/rancher/rancher/releases": `[
			{"tag_name":"v2.14.0","prerelease":false,"published_at":"2026-04-20T12:00:00Z"},
			{"tag_name":"v2.15.0-alpha2","prerelease":true,"published_at":"2026-03-01T12:00:00Z"}
		]`,
	})

	_, err := client.latestAlphasPerLine(context.Background(), 30*24*time.Hour)
	if !errors.Is(err, errNoRecentAlpha) {
		t.Fatalf("expected no recent alpha error, got %v", err)
	}
}

func TestResolvePreviousReleaseForUnpatchedHeadUsesLatestSameLine(t *testing.T) {
	client := fakeGitHubClient(t, map[string]string{
		"/repos/rancher/rancher/releases": `[
			{"tag_name":"v2.15.0","prerelease":false},
			{"tag_name":"v2.14.4","prerelease":false},
			{"tag_name":"v2.14.3","prerelease":false},
			{"tag_name":"v2.13.9","prerelease":false}
		]`,
	})
	target, err := parseTargetVersion("v2.14-head")
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.resolvePreviousRelease(context.Background(), target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "v2.14.4" {
		t.Fatalf("expected latest v2.14 release, got %s", got)
	}
}

func TestResolvePreviousReleaseForPatchHeadFallsBackToEarlierSameLine(t *testing.T) {
	client := fakeGitHubClient(t, map[string]string{
		"/repos/rancher/rancher/releases": `[
			{"tag_name":"v2.14.5","prerelease":false},
			{"tag_name":"v2.14.3","prerelease":false},
			{"tag_name":"v2.13.9","prerelease":false}
		]`,
	})
	target, err := parseTargetVersion("v2.14.5-abcdef0-head")
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.resolvePreviousRelease(context.Background(), target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "v2.14.3" {
		t.Fatalf("expected earlier same-line release v2.14.3, got %s", got)
	}
}

func TestResolvePreviousReleaseForPatchHeadPrefersPatchMinusOne(t *testing.T) {
	client := fakeGitHubClient(t, map[string]string{
		"/repos/rancher/rancher/releases/tags/v2.14.4": `{}`,
	})
	target, err := parseTargetVersion("v2.14.5-abcdef0-head")
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.resolvePreviousRelease(context.Background(), target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "v2.14.4" {
		t.Fatalf("expected patch-minus-one release v2.14.4, got %s", got)
	}
}

func TestNormalizeTargetListKeepsEnabledUniqueTargets(t *testing.T) {
	disabled := false
	targets := normalizeTargetList(targetList{Targets: []targetSpec{
		{RancherVersion: " 2.14.1-alpha7 ", PreviousRancherVersion: "2.14.0", SigningPolicy: "required"},
		{RancherVersion: "v2.14.1-alpha7"},
		{RancherVersion: "v2.15.0-alpha1", Enabled: &disabled},
		{RancherVersion: "  "},
	}})

	if len(targets.Targets) != 1 {
		t.Fatalf("expected one enabled unique target, got %#v", targets.Targets)
	}
	target := targets.Targets[0]
	if target.RancherVersion != "v2.14.1-alpha7" {
		t.Fatalf("unexpected target version: %#v", target)
	}
	if target.PreviousRancherVersion != "v2.14.0" {
		t.Fatalf("unexpected previous version: %#v", target)
	}
	if target.SigningPolicy != "required" {
		t.Fatalf("unexpected signing policy: %#v", target)
	}
}

func TestNormalizeTargetListPreservesPlainHead(t *testing.T) {
	targets := normalizeTargetList(targetList{Targets: []targetSpec{{RancherVersion: " head "}}})
	if len(targets.Targets) != 1 || targets.Targets[0].RancherVersion != "head" {
		t.Fatalf("plain head was not normalized correctly: %#v", targets.Targets)
	}
}

func TestApplyLedgerSkipsSuccessfulLanes(t *testing.T) {
	plan := plan{
		TargetVersion: "v2.14.1-alpha7",
		Lanes: []lane{
			{Name: laneWebhookFreshInstall},
			{Name: laneWebhookUpgrade},
		},
	}
	ledger := signoffLedger{Entries: map[string]map[string]ledgerEntry{
		"v2.14.1-alpha7": {
			laneWebhookFreshInstall: {
				Status:         "success",
				CoveragePolicy: currentCoveragePolicy,
				RunID:          "123",
				CompletedAt:    "2026-04-25T00:00:00Z",
			},
		},
	}}

	got := applyLedgerSkips(plan, ledger)
	if len(got.Lanes) != 1 || got.Lanes[0].Name != laneWebhookUpgrade {
		t.Fatalf("expected only upgrade lane to remain, got %#v", got.Lanes)
	}
	if len(got.SkippedLanes) != 1 || got.SkippedLanes[0].Name != laneWebhookFreshInstall {
		t.Fatalf("expected fresh lane skip, got %#v", got.SkippedLanes)
	}
}

func TestApplyLedgerDoesNotSkipStaleCoveragePolicy(t *testing.T) {
	plan := plan{
		TargetVersion: "v2.14.1-alpha7",
		Lanes: []lane{
			{Name: laneWebhookFreshInstall},
		},
	}
	ledger := signoffLedger{Entries: map[string]map[string]ledgerEntry{
		"v2.14.1-alpha7": {
			laneWebhookFreshInstall: {
				Status:         "success",
				CoveragePolicy: "alpha-webhook-signoff-v1",
				RunID:          "123",
				CompletedAt:    "2026-04-25T00:00:00Z",
			},
		},
	}}

	got := applyLedgerSkips(plan, ledger)
	if len(got.Lanes) != 1 || got.Lanes[0].Name != laneWebhookFreshInstall {
		t.Fatalf("expected stale coverage entry not to skip lane, got %#v", got.Lanes)
	}
	if len(got.SkippedLanes) != 0 {
		t.Fatalf("expected no skipped lanes, got %#v", got.SkippedLanes)
	}
}

func fakeGitHubClient(t *testing.T, responses map[string]string) githubClient {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		for suffix, body := range responses {
			if strings.HasSuffix(path, strings.TrimPrefix(suffix, "/")) {
				if body == "missing" {
					http.NotFound(w, r)
					return
				}
				_, _ = w.Write([]byte(body))
				return
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	return githubClient{
		httpClient: server.Client(),
		token:      "",
		rawBaseURL: server.URL,
		apiBaseURL: server.URL,
		registryBaseURLs: map[string]string{
			"stgregistry.suse.com": server.URL + "/stg",
			"registry.rancher.com": server.URL + "/prime",
			"registry.suse.com":    server.URL + "/suse",
			"docker.io":            server.URL + "/docker",
		},
	}
}

type imageInspectionFixture struct {
	Metadata rancherImageMetadata
	Found    bool
	Err      error
}

func fixtureImageInspector(fixtures map[string]imageInspectionFixture) rancherImageInspector {
	return recordingFixtureImageInspector(nil, fixtures)
}

func recordingFixtureImageInspector(calls *[]string, fixtures map[string]imageInspectionFixture) rancherImageInspector {
	var mu sync.Mutex
	return func(_ context.Context, reference string) (rancherImageMetadata, bool, error) {
		mu.Lock()
		defer mu.Unlock()
		if calls != nil {
			*calls = append(*calls, reference)
		}
		fixture, ok := fixtures[reference]
		if !ok {
			return rancherImageMetadata{Reference: reference}, false, nil
		}
		metadata := fixture.Metadata
		if metadata.Reference == "" {
			metadata.Reference = reference
		}
		return metadata, fixture.Found, fixture.Err
	}
}
