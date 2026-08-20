package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLedgerRecordsSuccessfulLane(t *testing.T) {
	tempDir := t.TempDir()
	planPath := filepath.Join(tempDir, "signoff-plan.json")
	ledgerPath := filepath.Join(tempDir, "signoff-ledger.json")
	planJSON := `{
  "target_version": "v2.14.1-alpha7",
  "release_line": "v2.14",
  "previous_version": "v2.14.0",
  "target_webhook_build": "109.0.1+up0.10.1-rc.5",
  "target_webhook_tag": "v0.10.1-rc.5",
  "previous_webhook_build": "109.0.0+up0.10.0",
  "previous_webhook_tag": "v0.10.0",
  "webhook_changed": true,
  "webhook_image": "stgregistry.suse.com/rancher/rancher-webhook:v0.10.1-rc.5",
  "signing_policy": "required",
  "signing_registry": "stgregistry.suse.com",
  "lanes": [
    {
      "name": "webhook-fresh-install",
      "install_rancher": "v2.14.1-alpha7",
      "install_rancher_distro": "auto"
    }
  ]
}`
	if err := os.WriteFile(planPath, []byte(planJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	plan, err := readPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	lane, err := findLane(plan, "webhook-fresh-install")
	if err != nil {
		t.Fatal(err)
	}
	l, err := readLedger(ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	l.Entries[plan.TargetVersion] = map[string]entry{
		lane.Name: {
			Status:               "success",
			CoveragePolicy:       currentCoveragePolicy,
			RunID:                "123",
			Lane:                 lane.Name,
			ReleaseLine:          plan.ReleaseLine,
			TargetVersion:        plan.TargetVersion,
			InstallRancher:       lane.InstallRancher,
			InstallRancherDistro: lane.InstallRancherDistro,
			WebhookImage:         plan.WebhookImage,
			PreviousWebhookBuild: plan.PreviousWebhookBuild,
			PreviousWebhookTag:   plan.PreviousWebhookTag,
			TargetWebhookBuild:   plan.TargetWebhookBuild,
			TargetWebhookTag:     plan.TargetWebhookTag,
			SigningPolicy:        plan.SigningPolicy,
			SigningRegistry:      plan.SigningRegistry,
			CompletedAt:          "2026-04-25T00:00:00Z",
		},
	}
	if err := writeLedger(ledgerPath, l); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{
		`"schema_version": 2`,
		`"coverage_policy": "alpha-webhook-signoff-v2"`,
		`"v2.14.1-alpha7"`,
		`"webhook-fresh-install"`,
		`"status": "success"`,
		`"webhook_image": "stgregistry.suse.com/rancher/rancher-webhook:v0.10.1-rc.5"`,
		`"target_webhook_build": "109.0.1+up0.10.1-rc.5"`,
		`"previous_webhook_build": "109.0.0+up0.10.0"`,
		`"signing_policy": "required"`,
		`"signing_registry": "stgregistry.suse.com"`,
		`"install_rancher_distro": "auto"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected ledger to contain %s:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{
		`"signing_verification"`,
		`"rancher_install_resolution"`,
		`"rancher_upgrade_resolution"`,
	} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("expected compact ledger not to contain %s:\n%s", unwanted, got)
		}
	}
}

func TestPruneLedgerTargetsKeepsMostRecentTargets(t *testing.T) {
	l := ledger{Entries: map[string]map[string]entry{
		"v2.14.1-alpha1": {"webhook-fresh-install": {CompletedAt: "2026-04-01T00:00:00Z"}},
		"v2.14.1-alpha2": {"webhook-fresh-install": {CompletedAt: "2026-04-02T00:00:00Z"}},
		"v2.14.1-alpha3": {"webhook-fresh-install": {CompletedAt: "2026-04-03T00:00:00Z"}},
	}}

	pruneLedgerTargets(&l, 2)

	if _, ok := l.Entries["v2.14.1-alpha1"]; ok {
		t.Fatalf("expected oldest target to be pruned: %#v", l.Entries)
	}
	if _, ok := l.Entries["v2.14.1-alpha2"]; !ok {
		t.Fatalf("expected alpha2 to remain: %#v", l.Entries)
	}
	if _, ok := l.Entries["v2.14.1-alpha3"]; !ok {
		t.Fatalf("expected alpha3 to remain: %#v", l.Entries)
	}
}

func TestReadSigningResultMissingPathIsOptional(t *testing.T) {
	result, err := readSigningResult(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil result, got %+v", result)
	}
}

func TestReadRancherResolutionMissingPathIsOptional(t *testing.T) {
	result, err := readRancherResolution(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil result, got %+v", result)
	}
}

func TestRancherHeadResolutionIdentitySurvivesLedgerRoundTrip(t *testing.T) {
	tempDir := t.TempDir()
	resolutionPath := filepath.Join(tempDir, "rancher-resolution-upgrade-ha-1.json")
	ledgerPath := filepath.Join(tempDir, "signoff-ledger.json")
	tag := "v2.14.5-a2770149753c8e4a48aec2c1e2598bb30cbb2652-head"
	revision := strings.Repeat("a", 40)
	ossRevision := strings.Repeat("b", 40)
	rancherDigest := "sha256:" + strings.Repeat("c", 64)
	agentDigest := "sha256:" + strings.Repeat("d", 64)
	sourceURL := "https://github.com/rancher/rancher-prime"
	commitURL := "https://github.com/rancher/rancher/commit/" + ossRevision

	resolutionJSON := `{
  "phase": "upgrade",
  "ha_index": 1,
  "requested_version": "2.14.5-a2770149753c8e4a48aec2c1e2598bb30cbb2652-head",
  "requested_distro": "auto",
  "preferred_image_registries": ["stgregistry.suse.com", "docker.io"],
  "build_type": "head",
  "resolved_distro": "prime-staging",
  "resolved_image_registry": "stgregistry.suse.com",
  "chart_repo_alias": "rancher-prime",
  "chart_version": "2.14.5",
  "chart_source": "rancher-prime/rancher@2.14.5",
  "rancher_image": "stgregistry.suse.com/rancher/rancher",
  "rancher_image_tag": "v2.14.5-a2770149753c8e4a48aec2c1e2598bb30cbb2652-head",
  "agent_image": "stgregistry.suse.com/rancher/rancher-agent:v2.14.5-a2770149753c8e4a48aec2c1e2598bb30cbb2652-head",
  "rancher_image_digest": "` + rancherDigest + `",
  "agent_image_digest": "` + agentDigest + `",
  "image_build_version": "v2.14.5-prime-head-build-42",
  "image_source_url": "https://github.com/rancher/rancher-prime",
  "image_source_revision": "` + revision + `",
  "image_source_oss_revision": "` + ossRevision + `",
  "image_source_commit_url": "https://github.com/rancher/rancher/commit/` + ossRevision + `",
  "use_rancher_image_fields": true,
  "compatibility_baseline": "2.14.5",
  "recommended_rke2_version": "v1.33.4+rke2r1",
  "resolution_notes": ["selected the exact Prime head image pair"]
}`
	if err := os.WriteFile(resolutionPath, []byte(resolutionJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	resolution, err := readRancherResolution(resolutionPath)
	if err != nil {
		t.Fatal(err)
	}
	if resolution == nil {
		t.Fatal("expected Rancher resolution")
	}
	if resolution.ResolvedImageRegistry != "stgregistry.suse.com" || resolution.RancherImageDigest != rancherDigest || resolution.AgentImageDigest != agentDigest {
		t.Fatalf("resolution image identity was not decoded: %+v", resolution)
	}
	if len(resolution.PreferredImageRegistries) != 2 || resolution.ImageSourceRevision != revision || resolution.ImageSourceOSSRevision != ossRevision || !resolution.UseRancherImageFields {
		t.Fatalf("resolution provenance was not decoded: %+v", resolution)
	}

	l := ledger{SchemaVersion: ledgerSchemaVersion, Entries: map[string]map[string]entry{
		"v2.14.5-a2770149753c8e4a48aec2c1e2598bb30cbb2652-head": {
			"webhook-upgrade": {
				Status:            "success",
				CoveragePolicy:    currentCoveragePolicy,
				RunID:             "123",
				Lane:              "webhook-upgrade",
				ReleaseLine:       "v2.14",
				TargetVersion:     "v2.14.5-a2770149753c8e4a48aec2c1e2598bb30cbb2652-head",
				InstallRancher:    "v2.14.5",
				UpgradeToRancher:  "v2.14.5-a2770149753c8e4a48aec2c1e2598bb30cbb2652-head",
				UpgradeResolution: resolution,
				CompletedAt:       "2026-08-20T17:00:00Z",
			},
		},
	}}
	if err := writeLedger(ledgerPath, l); err != nil {
		t.Fatal(err)
	}

	roundTripped, err := readLedger(ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	got := roundTripped.Entries["v2.14.5-a2770149753c8e4a48aec2c1e2598bb30cbb2652-head"]["webhook-upgrade"].UpgradeResolution
	if got == nil {
		t.Fatal("expected upgrade resolution in ledger")
	}
	if got.RequestedVersion != "2.14.5-a2770149753c8e4a48aec2c1e2598bb30cbb2652-head" {
		t.Fatalf("requested head version was lost: %+v", got)
	}
	if got.RancherImage != "stgregistry.suse.com/rancher/rancher" || got.RancherImageTag != tag || got.AgentImage != "stgregistry.suse.com/rancher/rancher-agent:"+tag {
		t.Fatalf("exact Rancher image pair was lost: %+v", got)
	}
	if got.RancherImageDigest != rancherDigest || got.AgentImageDigest != agentDigest || got.ImageBuildVersion != "v2.14.5-prime-head-build-42" {
		t.Fatalf("image digests/build identity were lost: %+v", got)
	}
	if got.ImageSourceURL != sourceURL || got.ImageSourceRevision != revision || got.ImageSourceOSSRevision != ossRevision || got.ImageSourceCommitURL != commitURL {
		t.Fatalf("image provenance was lost: %+v", got)
	}
	if got.ResolvedImageRegistry != "stgregistry.suse.com" || strings.Join(got.PreferredImageRegistries, ",") != "stgregistry.suse.com,docker.io" || !got.UseRancherImageFields {
		t.Fatalf("image resolution settings were lost: %+v", got)
	}

	ledgerJSON, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{
		`"preferred_image_registries"`,
		`"resolved_image_registry"`,
		`"rancher_image_tag"`,
		`"rancher_image_digest"`,
		`"agent_image"`,
		`"agent_image_digest"`,
		`"image_build_version"`,
		`"image_source_url"`,
		`"image_source_revision"`,
		`"image_source_oss_revision"`,
		`"image_source_commit_url"`,
		`"use_rancher_image_fields"`,
	} {
		if !strings.Contains(string(ledgerJSON), field) {
			t.Fatalf("expected ledger JSON to retain %s:\n%s", field, ledgerJSON)
		}
	}
}
