package test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	version "github.com/hashicorp/go-version"
	"gopkg.in/yaml.v3"
)

// Helm Lab reads the published index and chart archives directly. No generated
// catalog, external editor, local Helm installation, or cluster is involved.
var helmLabRepositories = map[string]string{
	"prime/ga":        rancherHelmRepoURLs["rancher-prime"],
	"prime/rc":        rancherHelmRepoURLs["optimus-rancher-latest"],
	"prime/head":      rancherHelmRepoURLs["optimus-rancher-latest"],
	"prime/alpha":     rancherHelmRepoURLs["optimus-rancher-alpha"],
	"community/ga":    rancherHelmRepoURLs["rancher-latest"],
	"community/rc":    rancherHelmRepoURLs["rancher-latest"],
	"community/alpha": rancherHelmRepoURLs["rancher-alpha"],
}

type helmLabChartVersion struct {
	Version     string    `json:"version" yaml:"version"`
	AppVersion  string    `json:"appVersion" yaml:"appVersion"`
	KubeVersion string    `json:"kubeVersion,omitempty" yaml:"kubeVersion"`
	Created     time.Time `json:"created" yaml:"created"`
	Digest      string    `json:"digest" yaml:"digest"`
	URLs        []string  `json:"-" yaml:"urls"`
}

type helmLabChart struct {
	Values       map[string]any    `json:"values"`
	Schema       map[string]any    `json:"schema"`
	Descriptions map[string]string `json:"descriptions"`
}

type helmLabCatalogResponse struct {
	Repository string                `json:"repository"`
	FetchedAt  time.Time             `json:"fetchedAt"`
	Versions   []helmLabChartVersion `json:"versions"`
	Selected   helmLabChartVersion   `json:"selected"`
	Chart      helmLabChart          `json:"chart"`
}

type helmLabCatalogCache struct {
	data    []byte
	expires time.Time
}
type helmLabCatalogService struct {
	mu     sync.Mutex
	cache  map[string]helmLabCatalogCache
	client *http.Client
}

func (p *localControlPanel) handleHelmLabCatalog(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !p.authorizedReadOnly(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	repo := helmLabRepositories[r.URL.Query().Get("distribution")+"/"+r.URL.Query().Get("channel")]
	if repo == "" {
		http.Error(w, "Choose a supported distribution and channel.", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	result, err := p.helmCatalog.load(ctx, repo, r.URL.Query().Get("channel"), strings.TrimSpace(r.URL.Query().Get("version")), r.URL.Query().Get("refresh") == "1")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, result)
}

func (s *helmLabCatalogService) fetch(ctx context.Context, address string, limit int64, refresh bool) ([]byte, error) {
	s.mu.Lock()
	cached, ok := s.cache[address]
	s.mu.Unlock()
	if ok && !refresh && time.Now().Before(cached.expires) {
		return cached.data, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	client := http.Client{Timeout: 20 * time.Second}
	if s.client != nil {
		client = *s.client
	}
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= 3 || next.URL.Scheme != "https" || next.URL.Host != req.URL.Host {
			return fmt.Errorf("chart repository redirected outside its origin")
		}
		return nil
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Could not reach the Rancher chart repository: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Rancher chart repository returned HTTP %d. Retry or choose another channel.", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("Read chart metadata: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("Chart metadata exceeds the download limit.")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cache == nil {
		s.cache = map[string]helmLabCatalogCache{}
	}
	if len(s.cache) >= 16 {
		clear(s.cache)
	}
	s.cache[address] = helmLabCatalogCache{data: data, expires: time.Now().Add(5 * time.Minute)}
	return data, nil
}

func (s *helmLabCatalogService) load(ctx context.Context, repo, channel, wanted string, refresh bool) (helmLabCatalogResponse, error) {
	result := helmLabCatalogResponse{Repository: repo, Versions: []helmLabChartVersion{}}
	data, err := s.fetch(ctx, repo+"/index.yaml", 16<<20, refresh)
	if err != nil {
		return result, err
	}
	var index struct {
		Entries map[string][]helmLabChartVersion `yaml:"entries"`
	}
	if err := yaml.Unmarshal(data, &index); err != nil {
		return result, fmt.Errorf("Read Rancher chart index: %w", err)
	}
	seen := map[string]bool{}
	for _, item := range index.Entries["rancher"] {
		if !seen[item.Version] && helmLabVersionChannel(item.Version) == channel {
			seen[item.Version] = true
			result.Versions = append(result.Versions, item)
		}
	}
	// Head hashes have no meaningful semver ordering; the publication date is
	// the useful ordering within a release line. Released charts use semver.
	sort.SliceStable(result.Versions, func(i, j int) bool {
		left, _ := version.NewVersion(result.Versions[i].Version)
		right, _ := version.NewVersion(result.Versions[j].Version)
		if channel == "head" && left.Core().Equal(right.Core()) {
			return result.Versions[i].Created.After(result.Versions[j].Created)
		}
		// Rancher publishes rc10/alpha10 identifiers without the semver dot.
		// Compare their numeric sequence so rc10 follows rc2 in the picker.
		if (channel == "rc" || channel == "alpha") && left.Core().Equal(right.Core()) {
			a, aErr := strconv.ParseUint(strings.TrimPrefix(left.Prerelease(), channel), 10, 64)
			b, bErr := strconv.ParseUint(strings.TrimPrefix(right.Prerelease(), channel), 10, 64)
			if aErr == nil && bErr == nil && a != b {
				return a > b
			}
		}
		return left.GreaterThan(right)
	})
	if len(result.Versions) == 0 {
		return result, fmt.Errorf("No %s charts are published in this repository.", channel)
	}
	result.Selected = result.Versions[0]
	if wanted != "" {
		found := false
		for _, item := range result.Versions {
			if item.Version == wanted {
				result.Selected = item
				found = true
				break
			}
		}
		if !found {
			return result, fmt.Errorf("Chart %s is not published in this channel. Choose a listed version; values from a different chart will not be substituted.", wanted)
		}
	}
	if len(result.Selected.URLs) == 0 {
		return result, fmt.Errorf("Selected chart has no download URL.")
	}
	base, _ := url.Parse(repo + "/")
	reference, err := url.Parse(result.Selected.URLs[0])
	if err != nil {
		return result, fmt.Errorf("Invalid chart archive URL.")
	}
	archiveURL := base.ResolveReference(reference)
	if archiveURL.Scheme != "https" || archiveURL.Host != base.Host || archiveURL.User != nil || !strings.HasPrefix(archiveURL.Path, base.Path) {
		return result, fmt.Errorf("Chart archive is outside the selected Rancher repository.")
	}
	archive, err := s.fetch(ctx, archiveURL.String(), 8<<20, refresh)
	if err != nil {
		return result, err
	}
	digest := sha256.Sum256(archive)
	if len(result.Selected.Digest) != 64 || !strings.EqualFold(hex.EncodeToString(digest[:]), result.Selected.Digest) {
		return result, fmt.Errorf("Chart checksum does not match the published index. Refresh the catalog and retry.")
	}
	result.Chart, err = readHelmLabChart(archive, result.Selected.Version)
	result.FetchedAt = time.Now().UTC()
	return result, err
}

func helmLabVersionChannel(raw string) string {
	v, err := version.NewVersion(raw)
	if err != nil {
		return ""
	}
	pre := strings.ToLower(v.Prerelease())
	switch {
	case pre == "":
		return "ga"
	case strings.HasSuffix(pre, "head"):
		return "head"
	case strings.HasPrefix(pre, "rc"):
		return "rc"
	case strings.HasPrefix(pre, "alpha"):
		return "alpha"
	default:
		return ""
	}
}

func readHelmLabChart(data []byte, expectedVersion string) (helmLabChart, error) {
	chart := helmLabChart{Descriptions: map[string]string{}}
	compressed, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return chart, fmt.Errorf("Open chart archive: %w", err)
	}
	defer compressed.Close()
	reader := tar.NewReader(io.LimitReader(compressed, 32<<20))
	files := map[string][]byte{}
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return chart, fmt.Errorf("Read chart archive: %w", err)
		}
		switch header.Name {
		case "rancher/Chart.yaml", "rancher/values.yaml", "rancher/values.schema.json":
			if header.Typeflag != tar.TypeReg || header.Size > 2<<20 {
				return chart, fmt.Errorf("Invalid chart metadata file.")
			}
			if _, duplicate := files[header.Name]; duplicate {
				return chart, fmt.Errorf("Duplicate chart metadata file.")
			}
			files[header.Name], err = io.ReadAll(reader)
			if err != nil {
				return chart, err
			}
		}
	}
	var identity struct {
		Name    string `yaml:"name"`
		Version string `yaml:"version"`
	}
	if yaml.Unmarshal(files["rancher/Chart.yaml"], &identity) != nil || identity.Name != "rancher" || identity.Version != expectedVersion {
		return chart, fmt.Errorf("Archive identity does not match the selected Rancher chart.")
	}
	var values yaml.Node
	if err := yaml.Unmarshal(files["rancher/values.yaml"], &values); err != nil {
		return chart, fmt.Errorf("Read chart defaults: %w", err)
	}
	if len(values.Content) != 1 || values.Content[0].Kind != yaml.MappingNode {
		return chart, fmt.Errorf("Chart defaults must be a YAML mapping.")
	}
	if err := values.Decode(&chart.Values); err != nil {
		return chart, err
	}
	if schema, ok := files["rancher/values.schema.json"]; ok {
		if err := json.Unmarshal(schema, &chart.Schema); err != nil {
			return chart, fmt.Errorf("Read chart schema: %w", err)
		}
	}
	var describe func(*yaml.Node, []string)
	describe = func(node *yaml.Node, parents []string) {
		if node.Kind != yaml.MappingNode {
			return
		}
		for i := 0; i+1 < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			parts := append(append([]string{}, parents...), key.Value)
			comment := strings.TrimSpace(strings.Join([]string{key.HeadComment, value.LineComment}, "\n"))
			var lines []string
			for _, line := range strings.Split(comment, "\n") {
				lines = append(lines, strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "#")))
			}
			if comment != "" {
				chart.Descriptions[strings.Join(parts, ".")] = strings.Join(lines, " ")
			}
			describe(value, parts)
		}
	}
	describe(values.Content[0], nil)
	return chart, nil
}
