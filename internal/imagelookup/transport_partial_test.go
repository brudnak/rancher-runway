package imagelookup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
)

func TestImageLookupFullResponseTransport(t *testing.T) {
	for _, test := range []struct {
		name          string
		method        string
		url           string
		blobRedirect  bool
		requestRange  []string
		status        int
		contentRange  []string
		contentLength int64
		wantComplete  bool
	}{
		{name: "complete", contentRange: []string{"bytes 0-9/10"}, contentLength: 10, wantComplete: true},
		{name: "single byte", contentRange: []string{"bytes 0-0/1"}, contentLength: 1, wantComplete: true},
		{name: "redirected blob", url: "https://storage.example/storage/config", blobRedirect: true, contentRange: []string{"bytes 0-9/10"}, contentLength: 10, wantComplete: true},
		{name: "nonblob response", url: "https://registry.example/v2/rancher/rancher/manifests/latest", contentRange: []string{"bytes 0-9/10"}, contentLength: 10},
		{name: "partial prefix", contentRange: []string{"bytes 0-4/10"}, contentLength: 5},
		{name: "partial suffix", contentRange: []string{"bytes 5-9/10"}, contentLength: 5},
		{name: "missing range", contentLength: 10},
		{name: "unknown total", contentRange: []string{"bytes 0-9/*"}, contentLength: 10},
		{name: "missing total", contentRange: []string{"bytes 0-9/"}, contentLength: 10},
		{name: "missing end", contentRange: []string{"bytes 0-/10"}, contentLength: 10},
		{name: "wrong unit", contentRange: []string{"items 0-9/10"}, contentLength: 10},
		{name: "multiple ranges", contentRange: []string{"bytes 0-9/10", "bytes 0-9/10"}, contentLength: 10},
		{name: "combined ranges", contentRange: []string{"bytes 0-9/10, bytes 0-9/10"}, contentLength: 10},
		{name: "signed end", contentRange: []string{"bytes 0-+9/10"}, contentLength: 10},
		{name: "signed total", contentRange: []string{"bytes 0-9/+10"}, contentLength: 10},
		{name: "overflow end", contentRange: []string{"bytes 0-9223372036854775808/10"}, contentLength: 10},
		{name: "overflow total", contentRange: []string{"bytes 0-9/9223372036854775808"}, contentLength: 10},
		{name: "wrong length", contentRange: []string{"bytes 0-9/10"}, contentLength: 9},
		{name: "unknown length", contentRange: []string{"bytes 0-9/10"}, contentLength: -1},
		{name: "zero length", contentRange: []string{"bytes 0-0/0"}, contentLength: 0},
		{name: "requested range", requestRange: []string{"bytes=0-"}, contentRange: []string{"bytes 0-9/10"}, contentLength: 10},
		{name: "empty requested range", requestRange: []string{""}, contentRange: []string{"bytes 0-9/10"}, contentLength: 10},
		{name: "head", method: http.MethodHead, contentRange: []string{"bytes 0-9/10"}, contentLength: 10},
		{name: "ordinary success", status: http.StatusOK, contentRange: []string{"bytes 0-9/10"}, contentLength: 10},
		{name: "registry failure", status: http.StatusServiceUnavailable, contentRange: []string{"bytes 0-9/10"}, contentLength: 10},
	} {
		t.Run(test.name, func(t *testing.T) {
			method := test.method
			if method == "" {
				method = http.MethodGet
			}
			status := test.status
			if status == 0 {
				status = http.StatusPartialContent
			}
			blobURL := "https://registry.example/v2/rancher/rancher-agent/blobs/sha256:" + strings.Repeat("a", 64)
			requestURL := test.url
			if requestURL == "" {
				requestURL = blobURL
			}
			request := httptest.NewRequest(method, requestURL, nil)
			if test.blobRedirect {
				request.Response = &http.Response{Request: httptest.NewRequest(http.MethodGet, blobURL, nil)}
			}
			if test.requestRange != nil {
				request.Header["Range"] = test.requestRange
			}
			body := io.NopCloser(strings.NewReader("0123456789"))
			defer body.Close()
			original := &http.Response{
				StatusCode: status, Status: fmt.Sprintf("%d %s", status, http.StatusText(status)),
				Header: http.Header{"Content-Range": test.contentRange}, ContentLength: test.contentLength,
				Body: body, Request: request,
			}
			tr := imageLookupFullResponseTransport{inner: imageLookupTestRoundTripFunc(func(got *http.Request) (*http.Response, error) {
				if got != request {
					t.Fatal("transport replaced the request")
				}
				return original, nil
			})}
			response, err := tr.RoundTrip(request)
			if err != nil {
				t.Fatal(err)
			}
			if test.wantComplete {
				if response.StatusCode != http.StatusOK || response.Status != "200 OK" || len(response.Header.Values("Content-Range")) != 0 {
					t.Fatalf("complete response was not normalized: %#v", response)
				}
				if original.StatusCode != http.StatusPartialContent || len(original.Header.Values("Content-Range")) != 1 {
					t.Fatal("transport mutated the underlying response")
				}
			} else if response != original {
				t.Fatal("transport replaced an ineligible response")
			}
			if response.Body != body || response.ContentLength != original.ContentLength {
				t.Fatal("transport changed the body or length")
			}
		})
	}
}

func TestImageLookupInspectProvenanceCompletePartialContentRedirect(t *testing.T) {
	for _, mode := range []string{"complete", "partial", "truncated", "corrupt"} {
		t.Run(mode, func(t *testing.T) {
			created := time.Date(2026, time.October, 21, 18, 0, 0, 0, time.UTC)
			fixture := newImageLookupPrimeFixtureImage(t, "rancher/rancher-agent", "v2.15.3-6bc4fe6-head", "6bc4fe6"+strings.Repeat("0", 33), created)
			config, err := fixture.RawConfigFile()
			if err != nil {
				t.Fatal(err)
			}
			configDigest, err := fixture.ConfigName()
			if err != nil {
				t.Fatal(err)
			}
			var storageReads atomic.Int32
			storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/storage/config" {
					http.NotFound(w, r)
					return
				}
				storageReads.Add(1)
				body := config
				length := len(config)
				end := length - 1
				switch mode {
				case "partial":
					body = config[:length/2]
					length = len(body)
					end = length - 1
				case "truncated":
					body = config[:length-1]
				case "corrupt":
					body = bytes.Replace(config, []byte("linux"), []byte("linuz"), 1)
				}
				w.Header().Set("Content-Type", "application/octet-stream")
				w.Header().Set("Content-Length", strconv.Itoa(length))
				w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", end, len(config)))
				w.WriteHeader(http.StatusPartialContent)
				_, _ = w.Write(body)
			}))
			defer storage.Close()
			registryHandler := registry.New(registry.Logger(log.New(io.Discard, "", 0)))
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && r.URL.Path == "/v2/rancher/rancher-agent/blobs/"+configDigest.String() {
					http.Redirect(w, r, storage.URL+"/storage/config", http.StatusTemporaryRedirect)
					return
				}
				registryHandler.ServeHTTP(w, r)
			}))
			defer server.Close()
			reference, err := name.NewTag(imageLookupTestServerHost(t, server)+"/rancher/rancher-agent:v2.15.3-6bc4fe6-head", name.Insecure)
			if err != nil {
				t.Fatal(err)
			}
			options := []remote.Option{remote.WithTransport(server.Client().Transport), remote.WithAuth(authn.Anonymous)}
			if err := remote.Write(reference, fixture, options...); err != nil {
				t.Fatal(err)
			}
			if mode == "complete" {
				// Demonstrate the original failure without Runway's compatibility transport.
				image, err := remote.Image(reference, options...)
				if err != nil {
					t.Fatal(err)
				}
				_, err = image.ConfigFile()
				var registryError *transport.Error
				if !errors.As(err, &registryError) || registryError.StatusCode != http.StatusPartialContent {
					t.Fatalf("unwrapped client error = %v, want registry 206", err)
				}
			}
			if mode == "corrupt" {
				corrupt := bytes.Replace(config, []byte("linux"), []byte("linuz"), 1)
				if !json.Valid(corrupt) || len(corrupt) != len(config) || bytes.Equal(corrupt, config) {
					t.Fatal("corrupt fixture must be changed valid JSON of the original length")
				}
			}
			got, found, err := InspectProvenance(context.Background(), newImageLookupTestService(t, server), reference.Name())
			if storageReads.Load() == 0 {
				t.Fatal("inspection did not follow the registry's blob redirect")
			}
			if mode == "complete" {
				if err != nil || !found || got.Digest == "" || !got.CreatedAt.Equal(created) || !strings.HasSuffix(got.CanonicalReference, "rancher/rancher-agent:v2.15.3-6bc4fe6-head") {
					t.Fatalf("complete redirected config: provenance=%+v found=%v error=%v", got, found, err)
				}
				return
			}
			if err == nil || found {
				t.Fatalf("%s config was accepted: provenance=%+v found=%v error=%v", mode, got, found, err)
			}
			switch mode {
			case "partial":
				if !strings.Contains(err.Error(), "206 Partial Content") {
					t.Fatalf("partial config error = %v, want original registry 206", err)
				}
			case "corrupt":
				if !strings.Contains(err.Error(), "checksum") {
					t.Fatalf("corrupt config error = %v, want digest verification failure", err)
				}
			}
		})
	}
}
