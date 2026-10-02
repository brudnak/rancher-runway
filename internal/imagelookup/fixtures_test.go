package imagelookup

import (
	"archive/tar"
	"bytes"
	"errors"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newImageLookupTestService(t *testing.T, server *httptest.Server) *Service {
	t.Helper()
	return &Service{
		transport:     server.Client().Transport,
		keychain:      imageLookupAnonymousTestKeychain{},
		allowHTTP:     true,
		now:           func() time.Time { return time.Date(2026, time.August, 5, 18, 0, 0, 0, time.UTC) },
		maxTagScan:    100,
		maxBuildYML:   1 << 20,
		maxBuildLayer: 4 << 20,
		maxLayerScan:  8 << 20,
	}
}

type imageLookupAnonymousTestKeychain struct{}

func (imageLookupAnonymousTestKeychain) Resolve(authn.Resource) (authn.Authenticator, error) {
	return authn.Anonymous, nil
}

type imageLookupTestRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn imageLookupTestRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

type imageLookupCloseTrackingRoundTripper struct {
	closes atomic.Int32
}

func (*imageLookupCloseTrackingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("unexpected RoundTrip call")
}

func (t *imageLookupCloseTrackingRoundTripper) CloseIdleConnections() {
	t.closes.Add(1)
}

func imageLookupTestServerHost(t *testing.T, server *httptest.Server) string {
	t.Helper()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	return parsed.Host
}

func newImageLookupFixtureImage(t *testing.T, architecture, variant string, created time.Time, files map[string]string) v1.Image {
	t.Helper()
	layer := newImageLookupFixtureLayer(t, files)
	image, err := mutate.AppendLayers(empty.Image, layer)
	if err != nil {
		t.Fatalf("append fixture image layer: %v", err)
	}
	config, err := image.ConfigFile()
	if err != nil {
		t.Fatalf("read fixture config: %v", err)
	}
	config.OS = "linux"
	config.Architecture = architecture
	config.Variant = variant
	config.Created = v1.Time{Time: created}
	config.Config.Labels = map[string]string{"org.opencontainers.image.version": "fixture-" + architecture}
	config.Config.Env = []string{"FIXTURE=yes"}
	config.Config.Entrypoint = []string{"/usr/bin/rancher"}
	config.Config.Cmd = []string{"server"}
	config.History = []v1.History{
		{
			Created:   v1.Time{Time: created},
			CreatedBy: "RUN fixture-build --arch=" + architecture,
			Comment:   "fixture layer",
		},
		{
			Created:    v1.Time{Time: created.Add(time.Second)},
			CreatedBy:  "LABEL org.opencontainers.image.version=fixture-" + architecture,
			Comment:    "fixture metadata",
			EmptyLayer: true,
		},
	}
	image, err = mutate.ConfigFile(image, config)
	if err != nil {
		t.Fatalf("write fixture config: %v", err)
	}
	image = mutate.MediaType(image, types.OCIManifestSchema1)
	return mutate.ConfigMediaType(image, types.OCIConfigJSON)
}

func pushImageLookupSourceFixture(t *testing.T, server *httptest.Server, tag string, labels map[string]string) (string, string) {
	t.Helper()
	image := newImageLookupFixtureImage(t, "amd64", "", time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC), map[string]string{
		"README.txt": "source fallback fixture",
	})
	config, err := image.ConfigFile()
	if err != nil {
		t.Fatalf("read source fixture config: %v", err)
	}
	for key, value := range labels {
		config.Config.Labels[key] = value
	}
	image, err = mutate.ConfigFile(image, config)
	if err != nil {
		t.Fatalf("write source fixture config: %v", err)
	}
	referenceText := imageLookupTestServerHost(t, server) + "/rancher/rancher:" + tag
	reference, err := name.NewTag(referenceText, name.StrictValidation, name.Insecure)
	if err != nil {
		t.Fatalf("parse source fixture reference: %v", err)
	}
	if err := remote.Write(reference, image,
		remote.WithTransport(server.Client().Transport),
		remote.WithAuth(authn.Anonymous),
	); err != nil {
		t.Fatalf("push source fixture image: %v", err)
	}
	digest, err := image.Digest()
	if err != nil {
		t.Fatalf("source fixture digest: %v", err)
	}
	return referenceText, digest.String()
}

func pushImageLookupPrimeFixture(t *testing.T, server *httptest.Server, repository, tag, ossRevision string, created time.Time) {
	t.Helper()
	image := newImageLookupPrimeFixtureImage(t, repository, tag, ossRevision, created)
	referenceText := imageLookupTestServerHost(t, server) + "/" + repository + ":" + tag
	reference, err := name.NewTag(referenceText, name.StrictValidation, name.Insecure)
	if err != nil {
		t.Fatalf("parse Prime fixture reference: %v", err)
	}
	if err := remote.Write(reference, image,
		remote.WithTransport(server.Client().Transport),
		remote.WithAuth(authn.Anonymous),
	); err != nil {
		t.Fatalf("push Prime fixture image: %v", err)
	}
}

func pushImageLookupPrimeIndexFixture(t *testing.T, server *httptest.Server, repository, tag, ossRevision string, created time.Time) {
	t.Helper()
	architecture, _ := TagArchitecture(tag)
	if architecture == "multi" {
		architecture = "amd64"
	}
	image := newImageLookupPrimeFixtureImage(t, repository, tag, ossRevision, created)
	index := mutate.AppendManifests(empty.Index, mutate.IndexAddendum{
		Add: image,
		Descriptor: v1.Descriptor{Platform: &v1.Platform{
			OS:           "linux",
			Architecture: architecture,
		}},
	})
	index = mutate.IndexMediaType(index, types.OCIImageIndex)
	referenceText := imageLookupTestServerHost(t, server) + "/" + repository + ":" + tag
	reference, err := name.NewTag(referenceText, name.StrictValidation, name.Insecure)
	if err != nil {
		t.Fatalf("parse Prime index fixture reference: %v", err)
	}
	if err := remote.WriteIndex(reference, index,
		remote.WithTransport(server.Client().Transport),
		remote.WithAuth(authn.Anonymous),
	); err != nil {
		t.Fatalf("push Prime fixture image index: %v", err)
	}
}

func newImageLookupPrimeFixtureImage(t *testing.T, repository, tag, ossRevision string, created time.Time) v1.Image {
	t.Helper()
	architecture, _ := TagArchitecture(tag)
	if architecture == "multi" {
		architecture = "amd64"
	}
	image := newImageLookupFixtureImage(t, architecture, "", created, map[string]string{
		"README.txt": "Prime pair fixture",
	})
	config, err := image.ConfigFile()
	if err != nil {
		t.Fatalf("read Prime fixture config: %v", err)
	}
	config.Config.Labels[SourceLabel] = "https://github.com/rancher/rancher-prime"
	config.Config.Labels[RevisionLabel] = strings.Repeat("f", 40)
	config.Config.Labels[OSSRevisionLabel] = ossRevision
	config.Config.Labels[CanonicalReferenceLabel] = "stgregistry.suse.com/" + repository + ":" + tag
	config.Config.Labels[VersionLabel] = tag
	image, err = mutate.ConfigFile(image, config)
	if err != nil {
		t.Fatalf("write Prime fixture config: %v", err)
	}
	return image
}

func newImageLookupRewritingTestService(t *testing.T, server *httptest.Server, failPath string) *Service {
	t.Helper()
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse rewriting registry URL: %v", err)
	}
	base := server.Client().Transport
	transport := imageLookupTestRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if failPath != "" && request.URL.Path == failPath {
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Status:     "503 Service Unavailable",
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"errors":[{"code":"UNAVAILABLE","message":"try later"}]}`)),
				Request:    request,
			}, nil
		}
		clone := request.Clone(request.Context())
		urlCopy := *request.URL
		urlCopy.Scheme = target.Scheme
		urlCopy.Host = target.Host
		clone.URL = &urlCopy
		clone.Host = target.Host
		return base.RoundTrip(clone)
	})
	return &Service{
		transport:     transport,
		keychain:      imageLookupAnonymousTestKeychain{},
		allowHTTP:     true,
		now:           time.Now,
		maxTagScan:    MaxTagScan,
		maxBuildYML:   1 << 20,
		maxBuildLayer: 4 << 20,
		maxLayerScan:  8 << 20,
	}
}

func newImageLookupFixtureLayer(t *testing.T, files map[string]string) v1.Layer {
	t.Helper()
	var layerArchive bytes.Buffer
	archive := tar.NewWriter(&layerArchive)
	paths := make([]string, 0, len(files))
	for filePath := range files {
		paths = append(paths, filePath)
	}
	sort.Strings(paths)
	for _, filePath := range paths {
		content := []byte(files[filePath])
		if err := archive.WriteHeader(&tar.Header{
			Name:     filePath,
			Mode:     0o644,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatalf("write fixture layer header: %v", err)
		}
		if _, err := archive.Write(content); err != nil {
			t.Fatalf("write fixture layer content: %v", err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("close fixture layer: %v", err)
	}
	return static.NewLayer(layerArchive.Bytes(), types.OCIUncompressedLayer)
}
