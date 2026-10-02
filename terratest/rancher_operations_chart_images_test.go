package test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
)

func TestRancherUpgradeChartImageSettings(t *testing.T) {
	for _, tc := range []struct {
		name, values, override, repository, tag string
		modern, invalid                         bool
	}{
		{name: "2.15.2 defaults", values: "image:\n  repository: rancher/rancher\n  tag: ''\nsystemDefaultRegistry:\n", repository: "docker.io/rancher/rancher", tag: "v2.15.2", modern: true},
		{name: "Prime defaults", values: "image:\n  repository: rancher/rancher\n  tag: v2.15.2\nsystemDefaultRegistry: registry.rancher.com\n", repository: "registry.rancher.com/rancher/rancher", tag: "v2.15.2", modern: true},
		{name: "image registry takes precedence", values: "image:\n  registry: stgregistry.suse.com\n  repository: rancher/rancher\n  tag: v2.15.3-head\nsystemDefaultRegistry: registry.rancher.com\n", repository: "stgregistry.suse.com/rancher/rancher", tag: "v2.15.3-head", modern: true},
		{name: "legacy defaults", values: "rancherImage: rancher/rancher\nrancherImageTag: ''\n", repository: "rancher/rancher", tag: "v2.15.2"},
		{name: "legacy defaults in transitional chart", values: "rancherImage: mirror.test/rancher/rancher\nrancherImageTag: v2.15.1\nimage:\n  repository: rancher/rancher\n  tag: v2.15.2\n", repository: "mirror.test/rancher/rancher", tag: "v2.15.1", modern: true},
		{name: "head overrides empty modern default", values: "image:\n  repository: ''\n  tag: ''\n", override: "docker.io/rancher/rancher:v2.15-head", repository: "docker.io/rancher/rancher", tag: "v2.15-head", modern: true},
		{name: "head overrides empty legacy default", values: "rancherImage: ''\n", override: "stgregistry.suse.com/rancher/rancher:head", repository: "stgregistry.suse.com/rancher/rancher", tag: "head"},
		{name: "empty defaults require a target", values: "image:\n  repository: ''\n  tag: ''\n", invalid: true},
		{name: "unsupported chart fails closed", values: "replicas: 3\n", override: "docker.io/rancher/rancher:head", invalid: true},
		{name: "malformed override not ignored", values: "rancherImage: rancher/rancher\n", override: "head", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chart, err := readHelmLabChart(helmLabArchive(t, "2.15.2", tc.values), "2.15.2")
			if err != nil {
				t.Fatal(err)
			}
			repository, tag, modern, err := rancherUpgradeImageSettings(chart, "v2.15.2", tc.override)
			if tc.invalid {
				if err == nil {
					t.Fatal("invalid chart/override accepted")
				}
				return
			}
			if err != nil || repository != tc.repository || tag != tc.tag || modern != tc.modern {
				t.Fatalf("got %s:%s modern=%v err=%v", repository, tag, modern, err)
			}
		})
	}
}

// Exercise the entire planner, including real archive decoding and its dry-run,
// so a supported chart cannot fail before the chosen head image is inspected.
func TestRancherUpgradePlannerChartSchemas(t *testing.T) {
	for _, tc := range []struct {
		name, values, chartVersion, override, wantImage, wantTag, resolved string
		modern, failDryRun                                                 bool
	}{
		{"community minor head", "image:\n  repository: rancher/rancher\n  tag: ''\n", "2.15.2", "docker.io/rancher/rancher:v2.15-head", "docker.io/rancher/rancher", "v2.15-head", "v2.15-19c92983f6f9d7f455de668e62fbfe55c045cde2-head", true, false},
		{"modern stable", "image:\n  repository: rancher/rancher\n  tag: ''\n", "2.15.3", "", "docker.io/rancher/rancher", "v2.15.3", "v2.15.3", true, false},
		{"modern staging exact image", "image:\n  repository: rancher/rancher\n  tag: ''\n", "2.15.2", "stgregistry.suse.com/rancher/rancher:v2.15.3-head", "stgregistry.suse.com/rancher/rancher", "v2.15.3-head", "v2.15.3-head", true, false},
		{"legacy stable", "rancherImage: rancher/rancher\n", "2.15.3", "", "rancher/rancher", "v2.15.3", "v2.15.3", false, false},
		{"failed preflight not saved", "image:\n  repository: rancher/rancher\n  tag: ''\n", "2.15.2", "docker.io/rancher/rancher:head", "docker.io/rancher/rancher", "head", "v2.16.0-head", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			archive := helmLabArchive(t, tc.chartVersion, tc.values)
			sum := sha256.Sum256(archive)
			item := helmLabChartVersion{Version: tc.chartVersion, AppVersion: "v" + tc.chartVersion, Digest: hex.EncodeToString(sum[:]), URLs: []string{"rancher.tgz"}}
			repo := helmLabRepositories["community/ga"]
			expires := time.Now().Add(time.Minute)
			panel := &localControlPanel{helmCatalog: helmLabCatalogService{cache: map[string]helmLabCatalogCache{
				repo + "/index.yaml":  {data: helmLabIndex(t, []helmLabChartVersion{item}), expires: expires},
				repo + "/rancher.tgz": {data: archive, expires: expires},
			}, client: &http.Client{Transport: helmLabTransport(func(*http.Request) (*http.Response, error) {
				t.Fatal("unexpected network request")
				return nil, fmt.Errorf("offline")
			})}}}
			var inspected []string
			var dryRuns int
			runtime := rancherUpgradeRuntime{
				installed: func(context.Context, string) (rancherInstalledState, error) {
					return rancherInstalledState{Version: "v2.15.2", Revision: 1}, nil
				},
				inspectImage: func(_ context.Context, image string) (imagelookup.Provenance, bool, error) {
					inspected = append(inspected, image)
					if tc.name == "community minor head" {
						return imagelookup.Provenance{Digest: "sha256:fixture", BuildVersion: "release-v2.15", ServerVersion: tc.resolved}, true, nil
					}
					return imagelookup.Provenance{Digest: "sha256:fixture", BuildVersion: tc.resolved}, true, nil
				},
				command: func(_ context.Context, _ []byte, name string, args ...string) ([]byte, error) {
					command := strings.Join(args, " ")
					if strings.Contains(command, "get values") {
						return []byte(`{"rancherImage":"old.test/rancher/rancher","rancherImageTag":"old"}`), nil
					}
					if name != "helm" || !strings.Contains(command, "--dry-run=server") {
						t.Fatalf("unexpected mutation: %s", command)
					}
					dryRuns++
					settings := map[string]string{}
					for i, arg := range args {
						if arg == "--set-string" {
							key, value, _ := strings.Cut(args[i+1], "=")
							settings[key] = value
						}
					}
					if tc.modern {
						registry, repository, _ := splitRegistryRepository(tc.wantImage)
						if settings["image.registry"] != registry || settings["image.repository"] != repository || settings["image.tag"] != tc.wantTag {
							t.Fatalf("wrong modern image overrides: %v", settings)
						}
						for _, key := range []string{"rancherImage", "rancherImageTag"} {
							if value, exists := settings[key]; !exists || value != "" {
								t.Fatalf("retained legacy override was not cleared: %v", settings)
							}
						}
					} else if settings["rancherImage"] != tc.wantImage || settings["rancherImageTag"] != tc.wantTag {
						t.Fatalf("wrong legacy overrides: %v", settings)
					}
					if tc.failDryRun {
						return nil, fmt.Errorf("fixture dry-run failed")
					}
					return nil, nil
				},
			}
			plan, err := panel.planRancherUpgradeWithRuntime(context.Background(), clusterView{ID: "fixture", KubeconfigPath: "fixture"}, rancherOperationRequest{Distribution: "community", Channel: "ga", Version: tc.chartVersion, Image: tc.override}, runtime)
			if tc.failDryRun {
				if err == nil || plan != nil || len(panel.rancherOps.plans) != 0 {
					t.Fatalf("failed preflight plan saved: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if dryRuns != 1 || plan.Image != tc.wantImage || plan.ImageTag != tc.wantTag || plan.TargetVersion != tc.resolved || panel.rancherOps.plans[plan.ID] != plan {
				t.Fatalf("incorrect plan: %+v", plan)
			}
			if tc.name == "community minor head" {
				if plan.TargetVersionLabel != "release-v2.15" || !strings.Contains(plan.TargetVersionSource, "CATTLE_SERVER_VERSION") || !strings.Contains(strings.Join(plan.Warnings, " "), "Patch-level") {
					t.Fatalf("missing version evidence or uncertainty warning: %+v", plan)
				}
			}
			agent := strings.TrimSuffix(tc.wantImage, "/rancher") + "/rancher-agent:" + tc.wantTag
			if len(inspected) != 2 || inspected[0] != tc.wantImage+":"+tc.wantTag || inspected[1] != agent {
				t.Fatalf("wrong images inspected: %v", inspected)
			}
		})
	}
}
