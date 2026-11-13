package test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
)

const (
	preflightChartsCommit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	preflightServerCommit = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	preflightWebhookPin   = "111.0.0+up0.12.1-rc.17"
	preflightFleetPin     = "111.0.0+up0.17.0-rc.2"
	preflightTurtlesPin   = "111.0.0+up0.28.0-rc.5"
	preflightServerImage  = "stgregistry.suse.com/rancher/rancher-server:v2.16.0-e3599b9-head"
	preflightAssetsImage  = "stgregistry.suse.com/rancher/rancher-assets:fixture"
)

type preflightImages map[string]imagelookup.ImageConfig

func (f preflightImages) Inspect(_ context.Context, req imagelookup.InspectRequest) (imagelookup.InspectResponse, error) {
	cfg, ok := f[req.Reference]
	if !ok {
		return imagelookup.InspectResponse{}, &transport.Error{StatusCode: http.StatusNotFound}
	}
	if req.IncludeBuildYAML || req.Platform != "linux/amd64" {
		return imagelookup.InspectResponse{}, fmt.Errorf("unexpected image request %s", req.Reference)
	}
	return imagelookup.InspectResponse{Reference: req.Reference, Digest: "sha256:" + strings.Repeat("c", 64), Config: cfg, Platform: "linux/amd64"}, nil
}
func (preflightImages) FetchSourceBuildYAML(context.Context, imagelookup.SourceBuildYAMLRequest) (imagelookup.SourceBuildYAMLResponse, error) {
	return imagelookup.SourceBuildYAMLResponse{}, errors.New("no private GitHub credentials")
}

func TestRancherAssetsPreflight(t *testing.T) {
	const build = "webhookVersion: " + preflightWebhookPin + "\nfleetVersion: " + preflightFleetPin + "\nturtlesVersion: " + preflightTurtlesPin + "\n"
	const index = `apiVersion: v1
entries:
  rancher-webhook: [{version: 111.0.0+up0.12.1-rc.17}]
  fleet-crd: [{version: 111.0.0+up0.17.0-rc.2}]
  fleet: [{version: 111.0.0+up0.17.0-rc.2}]
  rancher-turtles: [{version: 111.0.0+up0.28.0-rc.5}]
  system-upgrade-controller: [{version: 111.0.0}]
`
	stale := strings.NewReplacer(preflightWebhookPin, "111.0.0+up0.12.1-rc.13", preflightFleetPin, "111.0.0+up0.17.0-beta.2", preflightTurtlesPin, "111.0.0+up0.28.0-rc.3").Replace(index)
	for _, tc := range []struct {
		name, chartIndex, buildFile, want string
		mismatch                          bool
	}{
		{"matches", index, build, "Verified", false},
		{"stale assets", stale, build, "fleet-crd@" + preflightFleetPin, true},
		{"missing assets image", index, build, "registry could not find the rancher-assets image", true},
		{"missing Fleet CRD alone", strings.ReplaceAll(index, "fleet-crd:", "other-crd:"), build, "fleet-crd@" + preflightFleetPin, true},
		{"missing source", index, "", "Inconclusive", false},
		{"unreachable index", "", build, "Inconclusive", false},
		{"malformed index", "entries: null", build, "Inconclusive", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			renderer := `#!/bin/sh
case "$*" in
  "template rancher the-chart --set hostname=fixture.test --kube-version 1.36.0 --show-only templates/deployment.yaml") ;;
  *) exit 1;;
esac
cat <<'YAML'
kind: Deployment
spec:
  template:
    spec:
      initContainers:
        - name: rancher-charts-copy
          image: ` + preflightAssetsImage + `
      containers:
        - name: rancher
          image: ` + preflightServerImage + `
          env:
            - name: CATTLE_SYSTEM_UPGRADE_CONTROLLER_CHART_VERSION
              value: 111.0.0
YAML
`
			if err := os.WriteFile(filepath.Join(dir, "helm"), []byte(renderer), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			images := preflightImages{
				preflightServerImage: {Labels: map[string]string{imagelookup.SourceLabel: "https://github.com/rancher/rancher-prime", imagelookup.RevisionLabel: strings.Repeat("d", 40), imagelookup.OSSRevisionLabel: preflightServerCommit}},
				preflightAssetsImage: {Env: []string{"CHART_COMMIT=" + preflightChartsCommit}},
			}
			if tc.name == "missing assets image" {
				delete(images, preflightAssetsImage)
			}
			catalog := &helmLabCatalogService{client: &http.Client{Transport: rancherPlanRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				var body string
				switch req.URL.String() {
				case "https://raw.githubusercontent.com/rancher/rancher/" + preflightServerCommit + "/build.yaml":
					body = tc.buildFile
				case "https://raw.githubusercontent.com/rancher/charts/" + preflightChartsCommit + "/index.yaml":
					body = tc.chartIndex
				default:
					t.Fatalf("unexpected source %s", req.URL)
				}
				if body == "" {
					return nil, errors.New("service unavailable")
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}}
			summary, err := checkPlannedRancherAssets(t.Context(), "helm install rancher the-chart --set hostname=fixture.test", "1.36.0", images, catalog)
			if (err != nil) != tc.mismatch {
				t.Fatalf("summary=%s err=%v", summary, err)
			}
			if err != nil {
				summary = err.Error()
			}
			if !strings.Contains(summary, tc.want) {
				t.Fatalf("want %q in %q", tc.want, summary)
			}
			if tc.name == "stale assets" {
				for _, want := range []string{"fleet@" + preflightFleetPin, "rancher-turtles@" + preflightTurtlesPin, "rancher-webhook@" + preflightWebhookPin, preflightAssetsImage, preflightServerImage, preflightChartsCommit} {
					if !strings.Contains(summary, want) {
						t.Errorf("missing %q in %q", want, summary)
					}
				}
			}
		})
	}
}
