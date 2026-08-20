package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRenderReportIncludesNonSecretMetadata(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "downstream-ha-1.json"), `{
  "ha_index": 1,
  "cluster_name": "ha-test",
  "management_cluster_id": "c-m-abc",
  "k3s_version": "v1.33.4+k3s1",
  "linode_region": "us-ord",
  "linode_type": "g6-standard-2"
}`)
	mustWrite(t, filepath.Join(dir, "webhook-override-downstream-ha-1.json"), `{
  "scope": "downstream",
  "ha_index": 1,
  "cluster_name": "ha-test",
  "namespace": "cattle-system",
  "deployment": "rancher-webhook",
  "container": "rancher-webhook",
  "previous_image": "old",
  "candidate_image": "new",
  "rollout_complete": true
}`)
	mustWrite(t, filepath.Join(dir, "rancher-test-results.json"), `{
  "repo": "https://github.com/rancher/tests.git",
  "ref": "main",
  "lane": "webhook-fresh-install",
  "rancher_version": "v2.14.1-alpha6",
  "results": [
    {
      "suite": "charts-webhook",
      "package": "./validation/charts",
      "test_run": "TestWebhookTestSuite",
      "junit": "test-results/charts-webhook.xml",
      "conclusion": "success"
    }
  ]
}`)
	mustWrite(t, filepath.Join(dir, "webhook-signing.json"), `{
  "target_version": "v2.14.1-alpha6",
  "webhook_image": "stgregistry.suse.com/rancher/rancher-webhook:v0.10.1-rc.5",
  "signing_policy": "required",
  "enforced": true,
  "signature_verified": false,
  "provenance_verified": false,
  "sbom_verified": false,
  "verification_error": "no signatures found"
}`)

	report, err := renderReport(signoffPlan{
		TargetVersion:      "v2.14.1-alpha6",
		PreviousVersion:    "v2.14.0",
		TargetWebhookTag:   "v0.10.1-rc.5",
		PreviousWebhookTag: "v0.10.0",
		WebhookChanged:     true,
		WebhookImage:       "stgregistry.suse.com/rancher/rancher-webhook:v0.10.1-rc.5",
		SigningPolicy:      "required",
		SigningRegistry:    "stgregistry.suse.com",
		Lanes: []signoffLane{{
			Name:                "webhook-fresh-install",
			InstallRancher:      "v2.14.1-alpha6",
			ProvisionDownstream: true,
		}},
	}, dir, "webhook-fresh-install", time.Date(2026, 4, 24, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# v2.14.1-alpha6 Sign-Off Report", "`v1.33.4+k3s1`", "`stgregistry.suse.com/rancher/rancher-webhook:v0.10.1-rc.5`", "## Webhook Signing", "`no signatures found`", "## Rancher Test Results", "`charts-webhook`", "`success`"} {
		if !strings.Contains(report, want) {
			t.Fatalf("expected report to contain %q:\n%s", want, report)
		}
	}
	for _, omitted := range []string{"`ha-test`", "`c-m-abc`", "https://github.com/rancher/tests.git", "`cattle-system`", "`rancher-webhook`", "`old`", "`new`"} {
		if strings.Contains(report, omitted) {
			t.Fatalf("expected report to omit %q:\n%s", omitted, report)
		}
	}
}

func TestRenderReportIncludesMutableHeadResolutionIdentity(t *testing.T) {
	dir := t.TempDir()
	revision := strings.Repeat("a", 40)
	ossRevision := strings.Repeat("b", 40)
	rancherDigest := "sha256:" + strings.Repeat("c", 64)
	agentDigest := "sha256:" + strings.Repeat("d", 64)
	tag := "v2.14.5-a2770149753c8e4a48aec2c1e2598bb30cbb2652-head"

	mustWrite(t, filepath.Join(dir, "rancher-resolution-install-ha-1.json"), `{
  "phase": "install",
  "ha_index": 1,
  "requested_version": "v2.14-head",
  "preferred_image_registries": ["stgregistry.suse.com", "docker.io"],
  "resolved_image_registry": "stgregistry.suse.com",
  "rancher_image": "stgregistry.suse.com/rancher/rancher",
  "rancher_image_tag": "v2.14-head",
  "agent_image": "stgregistry.suse.com/rancher/rancher-agent:v2.14-head",
  "rancher_image_digest": "sha256:`+strings.Repeat("e", 64)+`",
  "agent_image_digest": "sha256:`+strings.Repeat("f", 64)+`",
  "image_source_revision": "`+strings.Repeat("1", 40)+`",
  "image_source_oss_revision": "`+strings.Repeat("2", 40)+`"
}`)
	mustWrite(t, filepath.Join(dir, "rancher-resolution-upgrade-ha-1.json"), `{
  "phase": "upgrade",
  "ha_index": 1,
  "requested_version": "2.14.5-a2770149753c8e4a48aec2c1e2598bb30cbb2652-head",
  "preferred_image_registries": ["stgregistry.suse.com", "docker.io"],
  "resolved_image_registry": "stgregistry.suse.com",
  "rancher_image": "stgregistry.suse.com/rancher/rancher",
  "rancher_image_tag": "`+tag+`",
  "agent_image": "stgregistry.suse.com/rancher/rancher-agent:`+tag+`",
  "rancher_image_digest": "`+rancherDigest+`",
  "agent_image_digest": "`+agentDigest+`",
  "image_source_revision": "`+revision+`",
  "image_source_oss_revision": "`+ossRevision+`"
}`)

	report, err := renderReport(signoffPlan{TargetVersion: "v2.14-head"}, dir, "", time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"## Rancher Install Resolution",
		"## Rancher Upgrade Resolution",
		"`v2.14-head`",
		"`stgregistry.suse.com/rancher/rancher:v2.14-head`",
		"`2.14.5-a2770149753c8e4a48aec2c1e2598bb30cbb2652-head`",
		"`stgregistry.suse.com/rancher/rancher:" + tag + "`",
		"`stgregistry.suse.com/rancher/rancher-agent:" + tag + "`",
		"`" + rancherDigest + "`",
		"`" + agentDigest + "`",
		"`" + revision + "`",
		"`" + ossRevision + "`",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("expected report to contain %q:\n%s", want, report)
		}
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
