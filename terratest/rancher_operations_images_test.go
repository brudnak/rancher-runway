package test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
)

func TestDeployedHeadTargetsSources(t *testing.T) {
	for _, source := range deployedImageSources {
		t.Run(source.Registry, func(t *testing.T) {
			var repositories []string
			result, err := deployedHeadTargets(context.Background(), source.Distribution, source.Registry, func(_ context.Context, registry, repository string) ([]string, error) {
				if registry != source.Registry || !imagelookup.IsRancherServerRepository(repository) {
					t.Fatalf("unexpected repository: %s/%s", registry, repository)
				}
				repositories = append(repositories, repository)
				return []string{"v2.15.2", "head", "v2.15.3-head", "v2.15.3-head-abcd"}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			images := result.(map[string]any)["images"].([]string)
			if len(images) != 6 || strings.Join(repositories, ",") != "rancher/rancher-server,rancher/rancher" {
				t.Fatalf("head targets: %v", images)
			}
			for i, image := range images {
				repository := "rancher/rancher-server"
				if i >= 3 {
					repository = "rancher/rancher"
				}
				if !strings.HasPrefix(image, source.Registry+"/"+repository+":") {
					t.Fatal(image)
				}
			}
		})
	}
	_, err := deployedHeadTargets(context.Background(), "community", "unlisted.test", func(context.Context, string, string) ([]string, error) {
		t.Fatal("unlisted registry queried")
		return nil, nil
	})
	if err == nil {
		t.Fatal("accepted unlisted source")
	}
	_, err = deployedHeadTargets(context.Background(), "prime", "", func(_ context.Context, registry, _ string) ([]string, error) {
		if registry != "stgregistry.suse.com" {
			t.Fatal("legacy Prime source changed")
		}
		return nil, fmt.Errorf("registry unavailable")
	})
	if err == nil || !strings.Contains(err.Error(), "stgregistry.suse.com") {
		t.Fatal("registry failure must identify its source")
	}
}

func TestDeployedHeadTargetsDoesNotHideRepositoryFailures(t *testing.T) {
	for _, failed := range []string{"rancher/rancher-server", "rancher/rancher"} {
		t.Run(failed, func(t *testing.T) {
			result, err := deployedHeadTargets(context.Background(), "community", "docker.io", func(_ context.Context, _, repository string) ([]string, error) {
				if repository == failed {
					return nil, fmt.Errorf("registry access failed")
				}
				return []string{"head"}, nil
			})
			if err == nil || result != nil || !strings.Contains(err.Error(), "docker.io/"+failed) {
				t.Fatalf("partial image list hid repository failure: result=%v err=%v", result, err)
			}
		})
	}
}

func TestDeployedUpgradeImageDetails(t *testing.T) {
	const sha = "19c92983f6f9d7f455de668e62fbfe55c045cde2"
	for _, registry := range []string{"docker.io", "stgregistry.suse.com"} {
		t.Run(registry, func(t *testing.T) {
			reference := registry + "/rancher/rancher:v2.15-head"
			details, err := deployedUpgradeImageDetails(context.Background(), reference, func(_ context.Context, got string) (imagelookup.Provenance, bool, error) {
				if got != reference {
					t.Fatalf("looked up %q instead of selected source %q", got, reference)
				}
				return imagelookup.Provenance{Digest: "sha256:" + strings.Repeat("a", 64), ServerVersion: "v2.15-" + sha + "-head", BuildVersion: "release-v2.15", Revision: sha, SourceURL: "https://github.com/rancher/rancher", CanonicalReference: "rancher/rancher:v2.15-" + sha + "-head"}, true, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			for key, want := range map[string]string{"registry": registry, "reference": reference, "revision": sha, "version": "v2.15-" + sha + "-head", "versionLabel": "release-v2.15", "digest": "sha256:" + strings.Repeat("a", 64), "commitURL": "https://github.com/rancher/rancher/commit/" + sha, "versionNote": ""} {
				if details[key] != want {
					t.Errorf("%s = %v, want %s", key, details[key], want)
				}
			}
		})
	}
	t.Run("unknown version retains metadata without claiming verification", func(t *testing.T) {
		details, err := deployedUpgradeImageDetails(context.Background(), "docker.io/rancher/rancher:head", func(context.Context, string) (imagelookup.Provenance, bool, error) {
			return imagelookup.Provenance{BuildVersion: "release-v2.15", Digest: "sha256:known", SourceURL: "javascript:alert(1)"}, true, nil
		})
		if err != nil || details["version"] != "" || details["versionNote"] == "" || details["digest"] != "sha256:known" || details["commitURL"] != "" {
			t.Fatalf("details=%v, err=%v", details, err)
		}
	})
	t.Run("missing image", func(t *testing.T) {
		_, err := deployedUpgradeImageDetails(context.Background(), "docker.io/rancher/rancher:head", func(context.Context, string) (imagelookup.Provenance, bool, error) {
			return imagelookup.Provenance{}, false, nil
		})
		if err == nil {
			t.Fatal("missing image treated as verified")
		}
	})
}
