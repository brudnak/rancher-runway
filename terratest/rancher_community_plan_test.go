package test

import (
	"context"
	"strings"
	"testing"

	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	"github.com/spf13/viper"
)

func TestCommunityHeadAutoPlanPreservesVerifiedImages(t *testing.T) {
	const tag = "v2.16-abcdef0123456789-head"
	previousInspector := inspectPreferredRancherImage
	t.Cleanup(func() { inspectPreferredRancherImage = previousInspector; viper.Reset() })
	inspectPreferredRancherImage = func(_ context.Context, reference string) (imagelookup.Provenance, bool, error) {
		if strings.HasSuffix(reference, ":v2.16-head") {
			return imagelookup.Provenance{}, false, nil
		}
		repository := "rancher/rancher"
		if strings.Contains(reference, "rancher-agent:") {
			repository = "rancher/rancher-agent"
		}
		return imagelookup.Provenance{
			Reference: reference, Digest: "sha256:" + strings.Repeat("a", 64),
			ServerVersion: tag, BuildVersion: "main", SourceURL: "https://github.com/rancher/rancher",
			CanonicalReference: repository + ":" + tag,
		}, true, nil
	}
	installFakeHelm(t, `#!/bin/sh
case "$1" in
  repo) exit 0 ;;
  search) printf '%s\n' '[{"name":"rancher-latest/rancher","version":"2.15.0","app_version":"v2.15.0"}]' ;;
  show) printf 'image:\n  registry: docker.io\n  repository: rancher/rancher\n  tag: ""\n' ;;
  *) exit 1 ;;
esac
`)
	for _, distro := range []string{"auto", "community"} {
		for _, preference := range []string{"default", "docker.io"} {
			t.Run(distro+"/"+preference, func(t *testing.T) {
				viper.Reset()
				viper.Set("deployment.type", deploymentTypeHostedTenantK3S)
				viper.Set("rancher.version", "2.16-head")
				viper.Set("rancher.distro", distro)
				viper.Set("rancher.bootstrap_password", "test-password")
				if preference != "default" {
					viper.Set("rancher.preferred_image_registries", []string{preference})
				}
				plans, err := resolveAutoRancherPlans(1)
				if err != nil {
					t.Fatal(err)
				}
				plan := plans[0]
				if plan.RequestedVersion != "2.16-head" || plan.ResolvedDistro != "community" || plan.ResolvedImageRegistry != "docker.io" || plan.RancherImageTag != tag || plan.AgentImage != "docker.io/rancher/rancher-agent:"+tag {
					t.Fatalf("unexpected resolved plan: %+v", plan)
				}
				if plan.CompatibilityBaseline != "2.15.0" || plan.ChartRepoAlias != "rancher-latest" {
					t.Fatalf("unexpected chart fallback: %+v", plan)
				}
				command := strings.Join(plan.HelmCommands, "\n")
				for _, fragment := range []string{"image.registry=docker.io", "image.repository=rancher/rancher", "image.tag=" + tag, "docker.io/rancher/rancher-agent:" + tag} {
					if !strings.Contains(command, fragment) {
						t.Fatalf("missing %q in install command: %s", fragment, command)
					}
				}
				if strings.Contains(command, "stgregistry") || strings.Contains(command, "image.tag=head") || strings.Contains(command, "image.tag=v2.16-head") {
					t.Fatalf("install did not preserve the verified Community pair: %s", command)
				}
			})
		}
	}
}
