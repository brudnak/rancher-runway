package test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	"gopkg.in/yaml.v3"
)

const setupIndexSource = "https://github.com/brudnak/webhook-ranch-hand"
const setupIndexRaw = "https://raw.githubusercontent.com/brudnak/webhook-ranch-hand/main/"

var setupReportPath = regexp.MustCompile(`^reports/(v2\.\d+)/(v2\.\d+\.\d+-[a-fA-F0-9]{7,40}-head)\.md$`)
var setupReportLink = regexp.MustCompile(`\((reports/v2\.\d+/v2\.\d+\.\d+-[a-fA-F0-9]{7,40}-head\.md)\)`)
var setupIndexLine = regexp.MustCompile(`^2\.\d+$`)

type setupIndexedBuild struct {
	Line          string `json:"line"`
	Version       string `json:"version"`
	Reference     string `json:"reference,omitempty"`
	Created       string `json:"created"`
	DatePrecision string `json:"datePrecision,omitempty"`
	Webhook       string `json:"webhook"`
	Checked       string `json:"checked,omitempty"`
	ReportPath    string `json:"reportPath"`
	ReportURL     string `json:"reportURL"`
}

type setupBuildIndexResponse struct {
	Builds     []setupIndexedBuild `json:"builds"`
	SourceURL  string              `json:"sourceURL"`
	FetchedAt  time.Time           `json:"fetchedAt"`
	Stale      bool                `json:"stale"`
	Warnings   []string            `json:"warnings"`
	Total      int                 `json:"total"`
	NextOffset int                 `json:"nextOffset"`
	More       bool                `json:"more"`
}

type setupIndexCacheEntry struct {
	data []byte
	at   time.Time
}

// Browsing consumes Ranch Hand's precomputed, dated index. It never enumerates
// registry images. Only the chosen build is inspected live before selection.
type setupBuildIndex struct {
	mu     sync.Mutex
	cache  map[string]setupIndexCacheEntry
	client *http.Client
}

func (s *setupBuildIndex) fetch(ctx context.Context, path string, refresh bool) (setupIndexCacheEntry, bool, error) {
	s.mu.Lock()
	cached, found := s.cache[path]
	s.mu.Unlock()
	if found && !refresh && time.Since(cached.at) < 5*time.Minute {
		return cached, false, nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, setupIndexRaw+path, nil)
	if err != nil {
		return setupIndexCacheEntry{}, false, err
	}
	client := http.Client{Timeout: 8 * time.Second}
	if s.client != nil {
		client = *s.client
	}
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) > 2 || next.URL.Scheme != "https" || next.URL.Host != "raw.githubusercontent.com" {
			return fmt.Errorf("build index redirected outside its source")
		}
		return nil
	}
	response, err := client.Do(request)
	var data []byte
	if err == nil {
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			err = fmt.Errorf("build index returned HTTP %d", response.StatusCode)
		} else {
			data, err = io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
			if len(data) > 2<<20 {
				err = fmt.Errorf("build index exceeds download limit")
			}
		}
	}
	if err != nil {
		if found && ctx.Err() == nil {
			return cached, true, nil
		}
		return setupIndexCacheEntry{}, false, fmt.Errorf("could not load Webhook Ranch Hand's build index: %w", err)
	}
	entry := setupIndexCacheEntry{data: data, at: time.Now().UTC()}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cache == nil {
		s.cache = map[string]setupIndexCacheEntry{}
	}
	if len(s.cache) >= 64 {
		// Keep the newest cached files, including a fallback during outages.
		oldest := path
		at := entry.at
		for key, value := range s.cache {
			if value.at.Before(at) {
				oldest, at = key, value.at
			}
		}
		delete(s.cache, oldest)
	}
	s.cache[path] = entry
	return entry, false, nil
}

func parseSetupBuildReport(path string, data []byte) (setupIndexedBuild, error) {
	parts := setupReportPath.FindStringSubmatch(path)
	if parts == nil || !strings.HasPrefix(parts[2], parts[1]+".") {
		return setupIndexedBuild{}, fmt.Errorf("invalid build report path")
	}
	meta, body, ok := strings.Cut(string(data), "-->")
	if !ok || body == "" || !strings.HasPrefix(meta, "<!-- meta\n") {
		return setupIndexedBuild{}, fmt.Errorf("build report has no metadata")
	}
	var fields map[string]string
	if err := yaml.Unmarshal([]byte(strings.TrimPrefix(meta, "<!-- meta\n")), &fields); err != nil {
		return setupIndexedBuild{}, fmt.Errorf("invalid build report metadata")
	}
	registry, repository, tag, err := imagelookup.ParseRegistryImage(fields["rancher_image"])
	created, dateErr := time.Parse(time.RFC3339Nano, fields["rancher_date"])
	if err != nil || registry != "stgregistry.suse.com" || !imagelookup.IsRancherServerRepository(repository) || tag != parts[2] || fields["version"] != tag || fields["build_stream"] != "prime-head" || fields["rancher_date_source"] != "image" || dateErr != nil {
		return setupIndexedBuild{}, fmt.Errorf("build report does not identify a dated Prime image")
	}
	return setupIndexedBuild{Line: strings.TrimPrefix(parts[1], "v"), Version: tag,
		Reference: registry + "/" + repository + ":" + tag, Created: created.UTC().Format(time.RFC3339),
		Webhook: fields["webhook"], Checked: fields["generated"], ReportPath: path,
		ReportURL: setupIndexSource + "/blob/main/" + path}, nil
}

func (s *setupBuildIndex) report(ctx context.Context, path string, refresh bool) (setupIndexedBuild, bool, error) {
	if !setupReportPath.MatchString(path) {
		return setupIndexedBuild{}, false, fmt.Errorf("invalid build report path")
	}
	entry, stale, err := s.fetch(ctx, path, refresh)
	if err != nil {
		return setupIndexedBuild{}, stale, err
	}
	build, err := parseSetupBuildReport(path, entry.data)
	return build, stale, err
}

func (s *setupBuildIndex) latest(ctx context.Context, refresh bool) (setupBuildIndexResponse, error) {
	result := setupBuildIndexResponse{Builds: []setupIndexedBuild{}, Warnings: []string{}, SourceURL: setupIndexSource}
	entry, stale, err := s.fetch(ctx, "README.md", refresh)
	if err != nil {
		return result, err
	}
	result.FetchedAt, result.Stale = entry.at, stale
	_, section, ok := strings.Cut(string(entry.data), "### Prime heads")
	if !ok {
		return result, fmt.Errorf("build index is missing its Prime heads section")
	}
	section, _, _ = strings.Cut(section, "### ")
	paths := []string{}
	for _, match := range setupReportLink.FindAllStringSubmatch(section, -1) {
		if !slices.Contains(paths, match[1]) {
			paths = append(paths, match[1])
		}
	}
	if len(paths) == 0 || len(paths) > 16 {
		return result, fmt.Errorf("build index has an unexpected number of release lines")
	}
	var mu sync.Mutex
	var workers sync.WaitGroup
	for _, path := range paths {
		workers.Go(func() {
			build, saved, err := s.report(ctx, path, refresh)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				result.Warnings = append(result.Warnings, path+": "+err.Error())
				return
			}
			result.Stale = result.Stale || saved
			result.Builds = append(result.Builds, build)
		})
	}
	workers.Wait()
	if len(result.Builds) == 0 {
		return result, fmt.Errorf("could not read any dated builds from the index")
	}
	slices.SortFunc(result.Builds, func(a, b setupIndexedBuild) int { return imagelookup.NaturalCompare(b.Line, a.Line) })
	result.Total = len(result.Builds)
	return result, nil
}

func parseSetupBuildHistory(line string, data []byte) []setupIndexedBuild {
	builds := []setupIndexedBuild{}
	seen := map[string]bool{}
	for _, row := range strings.Split(string(data), "\n") {
		cells := strings.Split(row, "|")
		if len(cells) < 11 {
			continue
		}
		value := func(i int) string { return strings.Trim(strings.TrimSpace(cells[i]), "`") }
		tag := value(1)
		path := "reports/v" + line + "/" + tag + ".md"
		if value(2) != "prime-head" || value(4) != "Image built" || !setupReportPath.MatchString(path) || !strings.HasPrefix(tag, "v"+line+".") || seen[tag] {
			continue
		}
		if _, err := time.Parse("2006-01-02", value(3)); err != nil {
			continue
		}
		seen[tag] = true
		builds = append(builds, setupIndexedBuild{Line: line, Version: tag, Created: value(3), DatePrecision: "day",
			Webhook: value(6), Checked: value(8), ReportPath: path, ReportURL: setupIndexSource + "/blob/main/" + path})
	}
	// Preserve the source index's full-timestamp ordering within a calendar day.
	slices.SortStableFunc(builds, func(a, b setupIndexedBuild) int { return strings.Compare(b.Created, a.Created) })
	return builds
}

func (s *setupBuildIndex) history(ctx context.Context, line string, refresh bool) (setupBuildIndexResponse, error) {
	result := setupBuildIndexResponse{Builds: []setupIndexedBuild{}, Warnings: []string{}, SourceURL: setupIndexSource}
	if !setupIndexLine.MatchString(line) {
		return result, fmt.Errorf("choose a release line such as 2.17")
	}
	entry, stale, err := s.fetch(ctx, "reports/v"+line+"/README.md", refresh)
	if err != nil {
		return result, err
	}
	result.FetchedAt, result.Stale = entry.at, stale
	result.Builds = parseSetupBuildHistory(line, entry.data)
	if len(result.Builds) == 0 {
		return result, fmt.Errorf("no dated Prime builds were found in this line's index")
	}
	return result, nil
}

func filterSetupBuildIndex(result setupBuildIndexResponse, days, offset int, now time.Time) setupBuildIndexResponse {
	if days > 0 {
		cutoff := now.UTC().AddDate(0, 0, -days).Format("2006-01-02")
		result.Builds = slices.DeleteFunc(result.Builds, func(build setupIndexedBuild) bool { return build.Created[:10] < cutoff })
	}
	result.Total = len(result.Builds)
	offset = min(offset, result.Total)
	end := min(offset+20, result.Total)
	result.More, result.NextOffset = end < result.Total, end
	result.Builds = result.Builds[offset:end]
	return result
}

func (s *interactiveServer) handleSetupBuildIndex(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	days, err := strconv.Atoi(query.Get("days"))
	if query.Get("days") == "" {
		days, err = 30, nil
	}
	offset, offsetErr := strconv.Atoi(query.Get("offset"))
	if query.Get("offset") == "" {
		offset, offsetErr = 0, nil
	}
	line := query.Get("line")
	text := strings.ToLower(strings.TrimSpace(query.Get("q")))
	if err != nil || offsetErr != nil || days < 0 || days > 3650 || offset < 0 || len(text) > 127 || (line != "" && !setupIndexLine.MatchString(line)) {
		http.Error(w, "invalid build date range or release line", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	var result setupBuildIndexResponse
	if line == "" {
		result, err = s.buildIndex.latest(ctx, query.Get("refresh") == "1")
	} else {
		result, err = s.buildIndex.history(ctx, line, query.Get("refresh") == "1")
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if text != "" {
		words := strings.Fields(text)
		result.Builds = slices.DeleteFunc(result.Builds, func(build setupIndexedBuild) bool {
			haystack := strings.ToLower(build.Line + " " + build.Version + " " + build.Webhook + " " + build.Reference + " SUSE staging")
			return slices.ContainsFunc(words, func(word string) bool { return !strings.Contains(haystack, word) })
		})
	}
	writeJSON(w, filterSetupBuildIndex(result, days, offset, time.Now()))
}
