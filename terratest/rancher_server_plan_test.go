package test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	"github.com/spf13/viper"
)

func TestCustomRancherServerImagePreservesRepositoryAndAgent(t *testing.T) {
	for _, image := range []string{
		"stgregistry.suse.com/rancher/rancher-server:v2.16.0-d492998-head",
		"docker.io/developer/rancher-server:local-test",
	} {
		request, custom, err := parseCustomRancherImageRequest(image)
		if err != nil || !custom {
			t.Fatalf("parse %s: custom=%v err=%v", image, custom, err)
		}
		if request.serverRepository+":"+request.tag != image {
			t.Fatalf("custom server was substituted: %+v", request)
		}
		wantAgent := strings.Replace(image, "/rancher-server:", "/rancher-agent:", 1)
		if request.agentImage != wantAgent {
			t.Fatalf("agent=%s, want %s", request.agentImage, wantAgent)
		}
	}
}

func TestCommitHeadImageSettingsPreferProductionRepository(t *testing.T) {
	const tag = "v2.16-d492998ed3ff209b16f67e1ec94866bc8671199d-head"
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		switch r.URL.Path {
		case "/v2/rancher/rancher-server/manifests/" + tag, "/v2/rancher/rancher-agent/manifests/" + tag:
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected lookup %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	previous := rancherRegistryBaseURLs
	rancherRegistryBaseURLs = map[string]string{"stgregistry.suse.com": server.URL}
	t.Cleanup(func() { rancherRegistryBaseURLs = previous })
	image, gotTag, agent, _, err := resolveCommitHeadImageSettings(strings.TrimPrefix(tag, "v"))
	if err != nil || image != "stgregistry.suse.com/rancher/rancher-server" || gotTag != tag || agent != "stgregistry.suse.com/rancher/rancher-agent:"+tag {
		t.Fatalf("resolved %s:%s agent=%s err=%v; requests=%v", image, gotTag, agent, err, requests)
	}
}

func TestRancherServerAutoPlanKeepsExactBuild(t *testing.T) {
	previous := inspectPreferredRancherImage
	t.Cleanup(func() { inspectPreferredRancherImage = previous; viper.Reset() })
	installFakeHelm(t, `#!/bin/sh
case "$1" in
  repo) exit 0 ;;
  search) printf '%s\n' '[{"name":"rancher-latest/rancher","version":"2.16.0","app_version":"v2.16.0"},{"name":"rancher-latest/rancher","version":"2.15.0","app_version":"v2.15.0"}]' ;;
  show) printf 'image:\n  registry: docker.io\n  repository: rancher/rancher\n  tag: ""\n' ;;
  *) exit 1 ;;
esac
`)
	for _, requested := range []string{"2.16.0-d492998-head", "2.17.0-721d53e-head", "head"} {
		t.Run(requested, func(t *testing.T) {
			viper.Reset()
			viper.Set("deployment.type", deploymentTypeHostedTenantK3S)
			viper.Set("rancher.version", requested)
			viper.Set("rancher.distro", "auto")
			viper.Set("rancher.bootstrap_password", "fixture")
			tag := imagelookup.NormalizeDockerRancherTag(requested)
			registry := "stgregistry.suse.com"
			if requested == "head" {
				registry = "docker.io"
			}
			inspectPreferredRancherImage = func(_ context.Context, reference string) (imagelookup.Provenance, bool, error) {
				repository := "rancher/rancher-server"
				if strings.Contains(reference, "/rancher-agent:") {
					repository = "rancher/rancher-agent"
				}
				if reference != registry+"/"+repository+":"+tag {
					t.Fatalf("unexpected image lookup: %s", reference)
				}
				sha := ""
				if requested != "head" {
					sha = strings.Split(requested, "-")[1] + strings.Repeat("a", 33)
				}
				return imagelookup.Provenance{Reference: reference, Digest: "sha256:" + strings.Repeat("b", 64), BuildVersion: tag, SourceURL: "https://github.com/rancher/rancher-prime", OSSRevision: sha, CanonicalReference: repository + ":" + tag}, true, nil
			}
			plans, err := resolveAutoRancherPlans(1)
			if err != nil {
				t.Fatal(err)
			}
			plan := plans[0]
			if plan.RequestedVersion != requested || plan.RancherImage != registry+"/rancher/rancher-server" || plan.RancherImageTag != tag || plan.AgentImage != registry+"/rancher/rancher-agent:"+tag {
				t.Fatalf("plan changed the requested build: %+v", plan)
			}
			command := strings.Join(plan.HelmCommands, "\n")
			for _, expected := range []string{"image.repository=rancher/rancher-server", "image.registry=" + registry, "image.tag=" + tag, registry + "/rancher/rancher-agent:" + tag} {
				if !strings.Contains(command, expected) {
					t.Fatalf("Helm command omits %s: %s", expected, command)
				}
			}
		})
	}
}
