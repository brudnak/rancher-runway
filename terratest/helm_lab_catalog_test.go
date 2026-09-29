package test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

type helmLabTransport func(*http.Request) (*http.Response, error)

func (f helmLabTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func helmLabArchive(t *testing.T, chartVersion, values string) []byte {
	t.Helper()
	var out bytes.Buffer
	zip := gzip.NewWriter(&out)
	tarball := tar.NewWriter(zip)
	for _, file := range []struct{ name, body string }{
		{"rancher/Chart.yaml", "name: rancher\nversion: " + chartVersion + "\n"},
		{"rancher/values.yaml", values},
		{"rancher/values.schema.json", `{"type":"object","properties":{"replicas":{"type":"integer","minimum":1}}}`},
	} {
		if err := tarball.WriteHeader(&tar.Header{Name: file.name, Mode: 0600, Size: int64(len(file.body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tarball.Write([]byte(file.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarball.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zip.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
func helmLabIndex(t *testing.T, items []helmLabChartVersion) []byte {
	t.Helper()
	data, err := yaml.Marshal(map[string]any{"entries": map[string]any{"rancher": items}})
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func helmLabFixture(t *testing.T) (helmLabChartVersion, []byte) {
	archive := helmLabArchive(t, "2.15.2", "# Run this many Rancher pods.\nreplicas: 3\nimage:\n  # Image pull behavior.\n  pullPolicy: IfNotPresent\n")
	sum := sha256.Sum256(archive)
	return helmLabChartVersion{Version: "2.15.2", AppVersion: "v2.15.2", Digest: hex.EncodeToString(sum[:]), URLs: []string{"rancher-2.15.2.tgz"}}, archive
}
func helmLabFake(t *testing.T, index, archive []byte, calls *atomic.Int32) *helmLabCatalogService {
	t.Helper()
	return &helmLabCatalogService{client: &http.Client{Transport: helmLabTransport(func(r *http.Request) (*http.Response, error) {
		if calls != nil {
			calls.Add(1)
		}
		if r.URL.Host != "charts.example.test" || r.Method != "GET" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		body := archive
		if strings.HasSuffix(r.URL.Path, "index.yaml") {
			body = index
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
	})}}
}
func TestHelmLabCatalogExactChartAndCache(t *testing.T) {
	item, archive := helmLabFixture(t)
	var calls atomic.Int32
	service := helmLabFake(t, helmLabIndex(t, []helmLabChartVersion{item}), archive, &calls)
	for i := 0; i < 2; i++ {
		result, err := service.load(context.Background(), "https://charts.example.test/charts", "ga", item.Version, false)
		if err != nil {
			t.Fatal(err)
		}
		if result.Selected.Version != item.Version || result.Chart.Values["replicas"] != 3 || result.Chart.Descriptions["image.pullPolicy"] != "Image pull behavior." || result.Chart.Schema["type"] != "object" {
			t.Fatalf("wrong chart: %+v", result)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("cache missed: %d calls", calls.Load())
	}
	if _, err := service.load(context.Background(), "https://charts.example.test/charts", "ga", item.Version, true); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 4 {
		t.Fatalf("refresh did not bypass cache: %d", calls.Load())
	}
	if _, err := service.load(context.Background(), "https://charts.example.test/charts", "ga", "2.99.0", false); err == nil || !strings.Contains(err.Error(), "not published") {
		t.Fatalf("must not silently substitute values: %v", err)
	}
}
func TestHelmLabCatalogSortingAndChannels(t *testing.T) {
	for raw, want := range map[string]string{"2.15.2": "ga", "2.15.2-rc2": "rc", "2.16.0-alpha1": "alpha", "2.16.0-abcdef-head": "head", "bad": "", "2.16.0-beta1": ""} {
		if got := helmLabVersionChannel(raw); got != want {
			t.Errorf("%s: got %q want %q", raw, got, want)
		}
	}
	for _, tc := range []struct {
		channel  string
		versions []string
		want     string
	}{
		{"ga", []string{"2.9.9", "2.15.1", "2.15.2-rc1", "2.15.2", "2.15.2"}, "2.15.2"},
		{"rc", []string{"2.15.2-rc2", "2.15.2-rc10", "2.15.2"}, "2.15.2-rc10"},
		{"head", []string{"2.16.0-ffff-head", "2.16.0-aaaa-head", "2.15.0-head"}, "2.16.0-aaaa-head"},
	} {
		t.Run(tc.channel, func(t *testing.T) {
			archive := helmLabArchive(t, tc.want, "replicas: 3\n")
			sum := sha256.Sum256(archive)
			var items []helmLabChartVersion
			for i, v := range tc.versions {
				items = append(items, helmLabChartVersion{Version: v, Digest: hex.EncodeToString(sum[:]), Created: time.Unix(int64(i), 0), URLs: []string{"rancher.tgz"}})
			}
			result, err := helmLabFake(t, helmLabIndex(t, items), archive, nil).load(context.Background(), "https://charts.example.test/charts", tc.channel, "", false)
			if err != nil {
				t.Fatal(err)
			}
			if result.Selected.Version != tc.want {
				t.Fatalf("got %s want %s", result.Selected.Version, tc.want)
			}
		})
	}
}
func TestHelmLabCatalogRejectsInvalidArchives(t *testing.T) {
	for _, tc := range []struct{ name, location, digest, identity, values, want string }{
		{name: "external URL", location: "https://other.example/chart.tgz", want: "outside"},
		{name: "outside repository", location: "../other.tgz", want: "outside"},
		{name: "insecure URL", location: "http://charts.example.test/charts/rancher.tgz", want: "outside"},
		{name: "credentials URL", location: "https://user:password@charts.example.test/charts/rancher.tgz", want: "outside"},
		{name: "checksum mismatch", digest: strings.Repeat("0", 64), want: "checksum"},
		{name: "archive version mismatch", identity: "2.15.1", want: "identity"},
		{name: "invalid defaults", values: "[wrong, shape]", want: "mapping"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item, archive := helmLabFixture(t)
			if tc.identity != "" {
				archive = helmLabArchive(t, tc.identity, "replicas: 3")
			}
			if tc.values != "" {
				archive = helmLabArchive(t, item.Version, tc.values)
			}
			sum := sha256.Sum256(archive)
			item.Digest = hex.EncodeToString(sum[:])
			if tc.location != "" {
				item.URLs = []string{tc.location}
			}
			if tc.digest != "" {
				item.Digest = tc.digest
			}
			_, err := helmLabFake(t, helmLabIndex(t, []helmLabChartVersion{item}), archive, nil).load(context.Background(), "https://charts.example.test/charts", "ga", "", false)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %s, got %v", tc.want, err)
			}
		})
	}
	if _, err := readHelmLabChart([]byte("not gzip"), "2.15.2"); err == nil {
		t.Fatal("invalid archive accepted")
	}
}
func TestHelmLabCatalogFetchBoundaries(t *testing.T) {
	service := helmLabFake(t, []byte("12345"), nil, nil)
	if _, err := service.fetch(context.Background(), "https://charts.example.test/index.yaml", 4, false); err == nil {
		t.Fatal("download size limit ignored")
	}
	service.client.Transport = helmLabTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://outside.example/index.yaml"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	if _, err := service.fetch(context.Background(), "https://charts.example.test/index.yaml", 100, false); err == nil || !strings.Contains(err.Error(), "outside its origin") {
		t.Fatalf("redirect allowed: %v", err)
	}
	service.client.Transport = helmLabTransport(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := service.fetch(ctx, "https://charts.example.test/index.yaml", 100, false); err == nil {
		t.Fatal("cancellation ignored")
	}
}
func TestHelmLabCatalogRouteValidation(t *testing.T) {
	panel := &localControlPanel{token: "test-token"}
	for _, tc := range []struct {
		method, query, token string
		status               int
	}{
		{"GET", "distribution=prime&channel=ga", "", 403},
		{"POST", "distribution=prime&channel=ga", "test-token", 405},
		{"GET", "distribution=community&channel=head", "test-token", 400},
		{"GET", "distribution=other&channel=ga", "test-token", 400},
	} {
		req := httptest.NewRequest(tc.method, "/api/helm-lab/catalog?"+tc.query, nil)
		req.Header.Set("X-Control-Panel-Token", tc.token)
		rec := httptest.NewRecorder()
		panel.handler().ServeHTTP(rec, req)
		if rec.Code != tc.status {
			t.Errorf("%s: %d %s", tc.query, rec.Code, rec.Body.String())
		}
	}
	item, archive := helmLabFixture(t)
	service := helmLabFake(t, helmLabIndex(t, []helmLabChartVersion{item}), archive, nil)
	// Exercise the route with a transport that serves fixtures at the official origin.
	transport := service.client.Transport
	service.client.Transport = helmLabTransport(func(r *http.Request) (*http.Response, error) {
		clone := r.Clone(r.Context())
		u := *r.URL
		clone.URL = &u
		clone.URL.Host = "charts.example.test"
		return transport.RoundTrip(clone)
	})
	panel.helmCatalog.client = service.client
	req := httptest.NewRequest("GET", "/api/helm-lab/catalog?distribution=prime&channel=ga", nil)
	req.Header.Set("X-Control-Panel-Token", "test-token")
	rec := httptest.NewRecorder()
	panel.handler().ServeHTTP(rec, req)
	var result helmLabCatalogResponse
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &result) != nil || result.Selected.Version != item.Version {
		t.Fatalf("catalog route failed: %d %s", rec.Code, rec.Body.String())
	}
}
