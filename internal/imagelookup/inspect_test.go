package imagelookup

import (
	"context"
	"errors"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestImageLookupInspectOCIIndexConfigLayersAndBuildYAML(t *testing.T) {
	registryServer := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	defer registryServer.Close()
	service := newImageLookupTestService(t, registryServer)

	created := time.Date(2026, time.August, 5, 14, 30, 0, 0, time.UTC)
	amd64Image := newImageLookupFixtureImage(t, "amd64", "", created, map[string]string{
		"usr/src/rancher/build.yaml": "webhookVersion: v0.12.1-rcs-0844.1\nrelease: security\n",
	})
	arm64Image := newImageLookupFixtureImage(t, "arm64", "v8", created.Add(time.Minute), map[string]string{
		"README.txt": "arm64 fixture",
	})
	index := mutate.AppendManifests(empty.Index,
		mutate.IndexAddendum{
			Add: amd64Image,
			Descriptor: v1.Descriptor{Platform: &v1.Platform{
				OS:           "linux",
				Architecture: "amd64",
			}},
		},
		mutate.IndexAddendum{
			Add: arm64Image,
			Descriptor: v1.Descriptor{Platform: &v1.Platform{
				OS:           "linux",
				Architecture: "arm64",
				Variant:      "v8",
			}},
		},
	)
	index = mutate.IndexMediaType(index, types.OCIImageIndex)

	referenceText := imageLookupTestServerHost(t, registryServer) + "/rancher/rancher:v2.16.0-rcs-0844.1"
	reference, err := name.NewTag(referenceText, name.StrictValidation, name.Insecure)
	if err != nil {
		t.Fatalf("parse fixture reference: %v", err)
	}
	if err := remote.WriteIndex(reference, index,
		remote.WithTransport(registryServer.Client().Transport),
		remote.WithAuth(authn.Anonymous),
	); err != nil {
		t.Fatalf("push fixture image index: %v", err)
	}

	result, err := service.Inspect(context.Background(), InspectRequest{
		Reference:        referenceText,
		Platform:         "linux/amd64",
		IncludeBuildYAML: true,
	})
	if err != nil {
		t.Fatalf("Inspect local OCI index: %v", err)
	}
	if result.Reference != referenceText || result.Registry != imageLookupTestServerHost(t, registryServer) || result.Repository != "rancher/rancher" || result.Tag != "v2.16.0-rcs-0844.1" {
		t.Fatalf("unexpected normalized inspection identity: %#v", result)
	}
	if result.MediaType != string(types.OCIImageIndex) || len(result.Platforms) != 2 {
		t.Fatalf("unexpected index metadata: mediaType=%q platforms=%#v", result.MediaType, result.Platforms)
	}
	if result.Platform != "linux/amd64" || result.Config.OS != "linux" || result.Config.Architecture != "amd64" {
		t.Fatalf("unexpected selected configuration: platform=%q config=%#v", result.Platform, result.Config)
	}
	if result.CreatedAt != created.Format(time.RFC3339Nano) || result.Config.CreatedAt != created.Format(time.RFC3339Nano) {
		t.Fatalf("created timestamp = response %q config %q, want %q", result.CreatedAt, result.Config.CreatedAt, created.Format(time.RFC3339Nano))
	}
	if result.Config.Labels["org.opencontainers.image.version"] != "fixture-amd64" || !slices.Equal(result.Config.Env, []string{"FIXTURE=yes"}) || !slices.Equal(result.Config.Entrypoint, []string{"/usr/bin/rancher"}) || !slices.Equal(result.Config.Cmd, []string{"server"}) {
		t.Fatalf("unexpected image config details: %#v", result.Config)
	}
	if len(result.Config.History) != 2 || result.Config.History[0].Created != created.Format(time.RFC3339Nano) || result.Config.History[0].CreatedBy != "RUN fixture-build --arch=amd64" || result.Config.History[0].Comment != "fixture layer" || result.Config.History[0].EmptyLayer {
		t.Fatalf("unexpected image config history: %#v", result.Config.History)
	}
	if !result.Config.History[1].EmptyLayer || result.Config.History[1].CreatedBy != "LABEL org.opencontainers.image.version=fixture-amd64" {
		t.Fatalf("unexpected metadata history entry: %#v", result.Config.History[1])
	}
	if len(result.Layers) != 1 || result.Layers[0].MediaType != string(types.OCIUncompressedLayer) || result.Size <= result.Config.Size {
		t.Fatalf("unexpected layer/size metadata: layers=%#v size=%d configSize=%d", result.Layers, result.Size, result.Config.Size)
	}
	if !result.BuildYAML.Found || result.BuildYAML.Skipped || result.BuildYAML.Path != "usr/src/rancher/build.yaml" {
		t.Fatalf("unexpected build.yaml result: %#v warnings=%v", result.BuildYAML, result.Warnings)
	}
	if result.BuildYAML.Data["webhookVersion"] != "v0.12.1-rcs-0844.1" || !strings.Contains(result.BuildYAML.Raw, "release: security") {
		t.Fatalf("unexpected build.yaml content: %#v", result.BuildYAML)
	}
}

func TestImageLookupInspectCanSkipOptionalDockerHubTagMetadata(t *testing.T) {
	registryServer := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	defer registryServer.Close()
	pushImageLookupSourceFixture(t, registryServer, "v2.14-head", nil)

	target, err := url.Parse(registryServer.URL)
	if err != nil {
		t.Fatalf("parse registry fixture URL: %v", err)
	}
	var hubRequests atomic.Int32
	service := newImageLookupTestService(t, registryServer)
	service.allowHTTP = false
	service.transport = imageLookupTestRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Hostname() == "hub.docker.com" {
			hubRequests.Add(1)
			return nil, errors.New("optional Docker Hub metadata is unavailable")
		}
		cloned := request.Clone(request.Context())
		cloned.URL.Scheme = target.Scheme
		cloned.URL.Host = target.Host
		cloned.Host = target.Host
		return registryServer.Client().Transport.RoundTrip(cloned)
	})

	result, err := service.Inspect(context.Background(), InspectRequest{
		Reference:       "docker.io/rancher/rancher:v2.14-head",
		Platform:        "linux/amd64",
		SkipTagMetadata: true,
	})
	if err != nil {
		t.Fatalf("Inspect with tag metadata disabled: %v", err)
	}
	if result.Digest == "" || hubRequests.Load() != 0 {
		t.Fatalf("manifest inspection result=%#v optional metadata requests=%d", result, hubRequests.Load())
	}
}

func TestImageLookupBoundedHistoryKeepsLatestEntriesAndBoundsText(t *testing.T) {
	created := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	history := make([]v1.History, imageLookupMaxHistoryEntries+2)
	for index := range history {
		history[index] = v1.History{
			Created:    v1.Time{Time: created.Add(time.Duration(index) * time.Minute)},
			CreatedBy:  "RUN ordinary-build-step",
			Comment:    "ordinary comment",
			EmptyLayer: index == len(history)-1,
		}
	}
	history[2].CreatedBy = strings.Repeat("c", imageLookupMaxHistoryText+50)
	history[2].Comment = strings.Repeat("m", imageLookupMaxHistoryText+75)

	bounded := imageLookupBoundedHistory(history)
	if len(bounded) != imageLookupMaxHistoryEntries {
		t.Fatalf("bounded history length = %d, want %d", len(bounded), imageLookupMaxHistoryEntries)
	}
	if bounded[0].Created != imageLookupFormatTime(history[2].Created.Time) {
		t.Fatalf("first retained history timestamp = %q, want latest bounded window beginning at %q", bounded[0].Created, imageLookupFormatTime(history[2].Created.Time))
	}
	if len(bounded[0].CreatedBy) != imageLookupMaxHistoryText || len(bounded[0].Comment) != imageLookupMaxHistoryText {
		t.Fatalf("history text bounds = createdBy %d comment %d, want %d each", len(bounded[0].CreatedBy), len(bounded[0].Comment), imageLookupMaxHistoryText)
	}
	if !bounded[len(bounded)-1].EmptyLayer {
		t.Fatal("bounded history did not preserve empty-layer metadata")
	}
	if empty := imageLookupBoundedHistory(nil); empty == nil || len(empty) != 0 {
		t.Fatalf("empty history = %#v, want non-nil empty slice", empty)
	}
}

func TestImageLookupBuildYAMLScanSkipsLargeLayersAndContinues(t *testing.T) {
	buildLayer := newImageLookupFixtureLayer(t, map[string]string{
		"usr/src/rancher/build.yaml": "release: bounded-layer-scan\n",
	})
	largeLayer := newImageLookupFixtureLayer(t, map[string]string{
		"large.bin": strings.Repeat("x", 32<<10),
	})
	buildSize, err := buildLayer.Size()
	if err != nil {
		t.Fatalf("build fixture layer size: %v", err)
	}
	largeSize, err := largeLayer.Size()
	if err != nil {
		t.Fatalf("large fixture layer size: %v", err)
	}
	if largeSize <= buildSize {
		t.Fatalf("large fixture layer size %d must exceed build layer size %d", largeSize, buildSize)
	}
	image, err := mutate.AppendLayers(empty.Image, buildLayer, largeLayer)
	if err != nil {
		t.Fatalf("append build.yaml fixture layers: %v", err)
	}
	service := &Service{
		maxBuildYML:   imageLookupMaxBuildYAML,
		maxBuildLayer: buildSize,
		maxLayerScan:  8 << 20,
	}

	result, warnings := service.findBuildYAML(context.Background(), image, nil)
	if !result.Found || result.Skipped || result.Error != "" || result.Path != "usr/src/rancher/build.yaml" {
		t.Fatalf("build.yaml scan result = %#v, warnings=%v", result, warnings)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "Skipped 1 layer larger than") {
		t.Fatalf("large-layer warning = %v, want one aggregate skip warning", warnings)
	}
}

func TestImageLookupBuildYAMLScanReturnsNonErrorReasonWhenLargeLayersWereSkipped(t *testing.T) {
	smallLayer := newImageLookupFixtureLayer(t, map[string]string{
		"README.txt": "eligible smaller layer without build metadata",
	})
	largeBuildLayer := newImageLookupFixtureLayer(t, map[string]string{
		"usr/src/rancher/build.yaml": "release: " + strings.Repeat("x", 32<<10) + "\n",
	})
	smallSize, err := smallLayer.Size()
	if err != nil {
		t.Fatalf("small fixture layer size: %v", err)
	}
	largeSize, err := largeBuildLayer.Size()
	if err != nil {
		t.Fatalf("large build fixture layer size: %v", err)
	}
	if largeSize <= smallSize {
		t.Fatalf("large build fixture layer size %d must exceed small layer size %d", largeSize, smallSize)
	}
	image, err := mutate.AppendLayers(empty.Image, smallLayer, largeBuildLayer, largeBuildLayer)
	if err != nil {
		t.Fatalf("append skipped build.yaml fixture layers: %v", err)
	}
	service := &Service{
		maxBuildYML:   imageLookupMaxBuildYAML,
		maxBuildLayer: smallSize,
		maxLayerScan:  8 << 20,
	}

	result, warnings := service.findBuildYAML(context.Background(), image, nil)
	if result.Found || !result.Skipped || result.Error != "" {
		t.Fatalf("large-layer-only build.yaml result = %#v, warnings=%v", result, warnings)
	}
	if !strings.Contains(result.Reason, "Skipped 2 layers larger than") || !strings.Contains(result.Reason, "safe scan limit") {
		t.Fatalf("non-error skip reason = %q", result.Reason)
	}
	if len(warnings) != 1 || warnings[0] != result.Reason {
		t.Fatalf("skip warnings = %v, want reason %q", warnings, result.Reason)
	}
	if got, want := imageLookupBuildYAMLSkipReason(6, 0, imageLookupMaxBuildYAMLLayer), "Skipped 6 layers larger than the 16 MiB safe scan limit."; got != want {
		t.Fatalf("default large-layer reason = %q, want %q", got, want)
	}
}

func TestImageLookupBuildYAMLScanLimitReturnsNonErrorReason(t *testing.T) {
	layer := newImageLookupFixtureLayer(t, map[string]string{
		"large-readme.txt": strings.Repeat("x", 8<<10),
	})
	compressedSize, err := layer.Size()
	if err != nil {
		t.Fatalf("scan-limit fixture layer size: %v", err)
	}
	image, err := mutate.AppendLayers(empty.Image, layer)
	if err != nil {
		t.Fatalf("append scan-limit fixture layer: %v", err)
	}
	service := &Service{
		maxBuildYML:   imageLookupMaxBuildYAML,
		maxBuildLayer: compressedSize,
		maxLayerScan:  512,
	}

	result, warnings := service.findBuildYAML(context.Background(), image, nil)
	if result.Found || !result.Skipped || result.Error != "" {
		t.Fatalf("cumulative scan-limit result = %#v, warnings=%v", result, warnings)
	}
	if !strings.Contains(result.Reason, "512 bytes cumulative uncompressed safe scan limit") || !strings.Contains(result.Reason, "remaining image layer data was not scanned") {
		t.Fatalf("cumulative scan-limit reason = %q", result.Reason)
	}
	if len(warnings) != 1 || warnings[0] != result.Reason {
		t.Fatalf("scan-limit warnings = %v, want reason %q", warnings, result.Reason)
	}
	if got, want := imageLookupBuildYAMLScanLimitReason(imageLookupMaxLayerScan), "Stopped after reaching the 256 MiB cumulative uncompressed safe scan limit; remaining image layer data was not scanned."; got != want {
		t.Fatalf("default cumulative scan-limit reason = %q, want %q", got, want)
	}
}
