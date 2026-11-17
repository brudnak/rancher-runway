package test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
)

func TestInteractiveSetupBuildSelectors(t *testing.T) {
	const prime = "v2.17.0-a123456-head"
	const community = "v2.17-b123456-head"
	tags := []string{prime, community, "head", "v2.17-head", "v2.17.1-head", "v2.17.0", "v2.170-head", "v2.17.0-a123456-head-amd64", "v2.17.0-a123456-head.sig"}
	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"v2.17-head", []string{prime, community, "v2.17-head", "v2.17.1-head"}},
		{"2.17.0-head", []string{prime}},
		{prime, []string{prime}},
		{"2.17.0", []string{"v2.17.0"}},
		{"2.17", []string{prime, community, "v2.17-head", "v2.17.1-head", "v2.17.0"}},
		{"head", []string{prime, community, "head", "v2.17-head", "v2.17.1-head", "v2.170-head"}},
		{"", tags[:7]},
	} {
		match, err := setupBuildMatcher(tc.query)
		if err != nil {
			t.Fatal(err)
		}
		got := slices.DeleteFunc(slices.Clone(tags), func(tag string) bool { return !match(tag) })
		if !slices.Equal(got, tc.want) {
			t.Errorf("%q matched %v, want %v", tc.query, got, tc.want)
		}
	}
}

func TestInteractiveSetupBuildRegistryIsolation(t *testing.T) {
	list := func(_ context.Context, registry, repository string) ([]string, error) {
		switch {
		case registry == "registry.rancher.com":
			return nil, errors.New("registry unavailable")
		case repository == "rancher/rancher-server" && registry == "stgregistry.suse.com":
			return []string{"v2.16.0-feedbee-head", "v2.16.0-head", "v2.16.0-feedbee-head", "v2.16.1-head"}, nil
		case repository == "rancher/rancher" && registry == "docker.io":
			return []string{"v2.16.0-head"}, nil
		}
		return nil, nil
	}
	for _, docker := range []bool{false, true} {
		groups, err := findSetupBuilds(t.Context(), "2.16.0-head", docker, list)
		if err != nil {
			t.Fatal(err)
		}
		matches, failures := 0, 0
		for _, group := range groups {
			matches += group.Matched
			if group.Error != "" {
				failures++
			}
			if group.Matched == 2 && !slices.Equal(group.Tags, []string{"v2.16.0-head", "v2.16.0-feedbee-head"}) {
				t.Errorf("unexpected tag order: %v", group.Tags)
			}
			if docker && group.Repository != "rancher/rancher" {
				t.Fatalf("offered split server image for Docker: %+v", group)
			}
		}
		if (docker && (matches != 1 || failures != 1)) || (!docker && (matches != 3 || failures != 2)) {
			t.Errorf("docker=%t: got %d matches, %d errors", docker, matches, failures)
		}
	}
}

type setupPickerImages struct {
	preflightImages
	digests map[string]string
}

func (s setupPickerImages) Inspect(ctx context.Context, req imagelookup.InspectRequest) (imagelookup.InspectResponse, error) {
	img, err := s.preflightImages.Inspect(ctx, req)
	img.CreatedAt = "2026-11-16T12:34:56Z"
	if digest := s.digests[req.Reference]; digest != "" {
		img.Digest = digest
	}
	return img, err
}

func TestInteractiveSetupBuildMetadata(t *testing.T) {
	const alias = "stgregistry.suse.com/rancher/rancher-server:v2.16.0-head"
	const pin = "stgregistry.suse.com/rancher/rancher-server:v2.16.0-e3599b9-head"
	const assets = "stgregistry.suse.com/rancher/rancher-assets:v2.16.0-e3599b9-head"
	config := imagelookup.ImageConfig{
		Env:    []string{"CATTLE_SERVER_VERSION=v2.16.0-e3599b9-head", "CATTLE_RANCHER_WEBHOOK_VERSION=" + preflightWebhookPin},
		Labels: map[string]string{imagelookup.SourceLabel: "https://github.com/rancher/rancher", imagelookup.RevisionLabel: preflightServerCommit},
	}
	images := setupPickerImages{preflightImages{alias: config, pin: config, assets: {Env: []string{"CHART_COMMIT=" + preflightChartsCommit}}}, map[string]string{}}
	catalog := &helmLabCatalogService{client: &http.Client{Transport: rancherPlanRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body string
		switch req.URL.Path {
		case "/rancher/rancher/" + preflightServerCommit + "/build.yaml":
			body = "webhookVersion: ignored\nfleetVersion: " + preflightFleetPin + "\nturtlesVersion: " + preflightTurtlesPin
		case "/rancher/charts/" + preflightChartsCommit + "/index.yaml":
			body = "apiVersion: v1\nentries:\n  fleet: [{version: " + preflightFleetPin + "}]\n  rancher-webhook: [{version: older}]"
		default:
			t.Fatalf("unexpected request %s", req.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}}
	got, err := inspectSetupBuild(t.Context(), alias, images, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if got.Reference != pin || got.Moving || got.Revision != preflightServerCommit || got.Created != "2026-11-16T12:34:56Z" || got.Digest == "" {
		t.Fatalf("incorrect build identity: %+v", got)
	}
	if got.Charts["rancher-webhook"] != preflightWebhookPin || got.Charts["fleet"] != preflightFleetPin || got.Charts["rancher-turtles"] != preflightTurtlesPin ||
		got.CatalogImage != assets || got.ChartRevision != preflightChartsCommit || !slices.Equal(got.CatalogVersions["rancher-webhook"], []string{"older"}) {
		t.Fatalf("incorrect chart evidence: %+v", got)
	}
	// An alias may move independently while the picker is open.
	images.digests[pin] = "sha256:" + strings.Repeat("a", 64)
	got, err = inspectSetupBuild(t.Context(), alias, images, catalog)
	if err != nil || got.Reference != alias || !got.Moving || got.CatalogVersions != nil || got.CatalogStatus == "" || got.Charts["fleet"] != preflightFleetPin {
		t.Fatalf("changed alias or unknown catalog presented as verified: %+v, %v", got, err)
	}
}

func TestInteractiveSetupBuildRoutes(t *testing.T) {
	for _, base := range []string{"", "/setup-editor"} {
		mux := http.NewServeMux()
		(&interactiveServer{token: "fixture"}).registerHandlersAt(mux, nil, base)
		for _, tc := range []struct {
			method, path string
			status       int
		}{
			{"GET", "/api/rancher-builds", 403},
			{"POST", "/api/rancher-builds?token=fixture", 405},
			{"GET", "/api/rancher-builds?token=fixture&version=not-a-selector", 400},
			{"GET", "/api/rancher-builds?token=fixture&reference=example.org/rancher/rancher:v2.17-head", 400},
			{"GET", "/static/setup_build_picker.js?token=fixture", 200},
		} {
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest(tc.method, base+tc.path, nil))
			if response.Code != tc.status {
				t.Errorf("%s %s returned %d, want %d", tc.method, base+tc.path, response.Code, tc.status)
			}
		}
	}
}
