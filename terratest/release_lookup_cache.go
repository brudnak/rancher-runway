package test

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	goversion "github.com/hashicorp/go-version"
)

const (
	releaseLookupCacheSchemaVersion = 1
	releaseLookupCachePathEnv       = "HA_RANCHER_RELEASE_LOOKUP_CACHE"
)

var releaseLookupCacheMu sync.Mutex

type releaseLookupCache struct {
	SchemaVersion int                                     `json:"schema_version"`
	UpdatedAt     string                                  `json:"updated_at"`
	Products      map[string]map[string]releaseCacheEntry `json:"products"`
	SupportRanges map[string]supportRangeCacheEntry       `json:"support_ranges,omitempty"`
}

type releaseCacheEntry struct {
	Minor           int      `json:"minor"`
	ReleaseNotesURL string   `json:"release_notes_url"`
	LatestVersion   string   `json:"latest_version"`
	Versions        []string `json:"versions"`
	UpdatedAt       string   `json:"updated_at"`
}

type supportRangeCacheEntry struct {
	Product   string `json:"product"`
	SourceURL string `json:"source_url"`
	Range     string `json:"range"`
	MinMinor  int    `json:"min_minor"`
	MaxMinor  int    `json:"max_minor"`
	UpdatedAt string `json:"updated_at"`
}

type releaseProductConfig struct {
	ProductName              string
	CacheKey                 string
	Pattern                  *regexp.Regexp
	ReleaseNotesFallbackURLs []string
	GitHubTagRefsURL         string
	GitHubBuildPrefix        string
	GitHubReleaseURL         string
	GitHubAssetNames         []string
}

type httpStatusError struct {
	URL        string
	StatusCode int
}

type githubTagRef struct {
	Ref string `json:"ref"`
}

type githubRelease struct {
	Assets []struct {
		Name string `json:"name"`
	} `json:"assets"`
}

func (e httpStatusError) Error() string {
	return fmt.Sprintf("HTTP %d from %s", e.StatusCode, e.URL)
}

func releaseLookupCachePath() string {
	if path := strings.TrimSpace(os.Getenv(releaseLookupCachePathEnv)); path != "" {
		return path
	}
	return filepath.Join(automationOutputDir(), "release-lookup-cache.json")
}

func newReleaseLookupCache() releaseLookupCache {
	return releaseLookupCache{
		SchemaVersion: releaseLookupCacheSchemaVersion,
		Products:      map[string]map[string]releaseCacheEntry{},
		SupportRanges: map[string]supportRangeCacheEntry{},
	}
}

func loadReleaseLookupCache(path string) (releaseLookupCache, error) {
	cache := newReleaseLookupCache()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cache, nil
	}
	if err != nil {
		return cache, err
	}
	if err := json.Unmarshal(data, &cache); err != nil {
		return cache, err
	}
	if cache.SchemaVersion != releaseLookupCacheSchemaVersion {
		return cache, fmt.Errorf("unsupported schema version %d", cache.SchemaVersion)
	}
	if cache.Products == nil {
		cache.Products = map[string]map[string]releaseCacheEntry{}
	}
	if cache.SupportRanges == nil {
		cache.SupportRanges = map[string]supportRangeCacheEntry{}
	}
	return cache, nil
}

func saveReleaseLookupCache(path string, cache releaseLookupCache) error {
	cache.SchemaVersion = releaseLookupCacheSchemaVersion
	cache.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if cache.Products == nil {
		cache.Products = map[string]map[string]releaseCacheEntry{}
	}
	if cache.SupportRanges == nil {
		cache.SupportRanges = map[string]supportRangeCacheEntry{}
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func updateReleaseCache(config releaseProductConfig, highestMinor int, releaseNotesURL, latest string, versions []string) {
	path := releaseLookupCachePath()
	releaseLookupCacheMu.Lock()
	defer releaseLookupCacheMu.Unlock()

	cache, err := loadReleaseLookupCache(path)
	if err != nil {
		log.Printf("[resolver] Warning: could not read release lookup cache %s: %v", path, err)
		cache = newReleaseLookupCache()
	}
	if cache.Products[config.CacheKey] == nil {
		cache.Products[config.CacheKey] = map[string]releaseCacheEntry{}
	}
	cache.Products[config.CacheKey][releaseMinorCacheKey(highestMinor)] = releaseCacheEntry{
		Minor:           highestMinor,
		ReleaseNotesURL: releaseNotesURL,
		LatestVersion:   latest,
		Versions:        versions,
		UpdatedAt:       time.Now().UTC().Format(time.RFC3339),
	}
	if err := saveReleaseLookupCache(path, cache); err != nil {
		log.Printf("[resolver] Warning: could not write release lookup cache %s: %v", path, err)
	}
}

func cachedRelease(config releaseProductConfig, highestMinor int) (releaseCacheEntry, string, error) {
	path := releaseLookupCachePath()
	releaseLookupCacheMu.Lock()
	defer releaseLookupCacheMu.Unlock()

	cache, err := loadReleaseLookupCache(path)
	if err != nil {
		return releaseCacheEntry{}, path, err
	}
	productEntries := cache.Products[config.CacheKey]
	if productEntries == nil {
		return releaseCacheEntry{}, path, os.ErrNotExist
	}
	entry, ok := productEntries[releaseMinorCacheKey(highestMinor)]
	if !ok {
		return releaseCacheEntry{}, path, os.ErrNotExist
	}
	if err := validateReleaseCacheEntry(config, highestMinor, entry); err != nil {
		return releaseCacheEntry{}, path, err
	}
	return entry, path, nil
}

func updateSupportRangeCache(product, supportMatrixURL, rangeText string, minMinor, maxMinor int) {
	path := releaseLookupCachePath()
	releaseLookupCacheMu.Lock()
	defer releaseLookupCacheMu.Unlock()

	cache, err := loadReleaseLookupCache(path)
	if err != nil {
		log.Printf("[resolver] Warning: could not read release lookup cache %s: %v", path, err)
		cache = newReleaseLookupCache()
	}
	cache.SupportRanges[supportRangeCacheKey(product, supportMatrixURL)] = supportRangeCacheEntry{
		Product:   product,
		SourceURL: supportMatrixURL,
		Range:     rangeText,
		MinMinor:  minMinor,
		MaxMinor:  maxMinor,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := saveReleaseLookupCache(path, cache); err != nil {
		log.Printf("[resolver] Warning: could not write release lookup cache %s: %v", path, err)
	}
}

func cachedSupportRange(product, supportMatrixURL string) (supportRangeCacheEntry, string, error) {
	path := releaseLookupCachePath()
	releaseLookupCacheMu.Lock()
	defer releaseLookupCacheMu.Unlock()

	cache, err := loadReleaseLookupCache(path)
	if err != nil {
		return supportRangeCacheEntry{}, path, err
	}
	entry, ok := cache.SupportRanges[supportRangeCacheKey(product, supportMatrixURL)]
	if !ok {
		return supportRangeCacheEntry{}, path, os.ErrNotExist
	}
	if err := validateSupportRangeCacheEntry(product, supportMatrixURL, entry); err != nil {
		return supportRangeCacheEntry{}, path, err
	}
	return entry, path, nil
}

func releaseMinorCacheKey(highestMinor int) string {
	return fmt.Sprintf("v1.%d", highestMinor)
}

func supportRangeCacheKey(product, supportMatrixURL string) string {
	return strings.ToLower(strings.TrimSpace(product)) + "|" + strings.TrimSpace(supportMatrixURL)
}

func validateReleaseCacheEntry(config releaseProductConfig, highestMinor int, entry releaseCacheEntry) error {
	if entry.Minor != highestMinor {
		return fmt.Errorf("cached minor is %d, expected %d", entry.Minor, highestMinor)
	}
	if strings.TrimSpace(entry.ReleaseNotesURL) == "" {
		return fmt.Errorf("cached release notes URL is empty")
	}
	if !config.Pattern.MatchString(entry.LatestVersion) {
		return fmt.Errorf("cached latest %q does not look like a %s v1.%d release", entry.LatestVersion, config.ProductName, highestMinor)
	}
	if len(entry.Versions) == 0 {
		return fmt.Errorf("cached version list is empty")
	}
	if !slices.Contains(entry.Versions, entry.LatestVersion) {
		return fmt.Errorf("cached latest %q is not present in cached version list", entry.LatestVersion)
	}
	for _, version := range entry.Versions {
		if !config.Pattern.MatchString(version) {
			return fmt.Errorf("cached version %q does not look like a %s v1.%d release", version, config.ProductName, highestMinor)
		}
	}
	if _, err := time.Parse(time.RFC3339, entry.UpdatedAt); err != nil {
		return fmt.Errorf("cached updated_at %q is invalid: %w", entry.UpdatedAt, err)
	}
	return nil
}

func validateSupportRangeCacheEntry(product, supportMatrixURL string, entry supportRangeCacheEntry) error {
	if entry.Product != product {
		return fmt.Errorf("cached product is %q, expected %q", entry.Product, product)
	}
	if entry.SourceURL != supportMatrixURL {
		return fmt.Errorf("cached source URL is %q, expected %q", entry.SourceURL, supportMatrixURL)
	}
	if entry.Range == "" {
		return fmt.Errorf("cached support range is empty")
	}
	if entry.MinMinor <= 0 || entry.MaxMinor <= 0 || entry.MinMinor > entry.MaxMinor {
		return fmt.Errorf("cached support range minors are invalid: %d-%d", entry.MinMinor, entry.MaxMinor)
	}
	if _, err := time.Parse(time.RFC3339, entry.UpdatedAt); err != nil {
		return fmt.Errorf("cached updated_at %q is invalid: %w", entry.UpdatedAt, err)
	}
	return nil
}

func resolveLatestCachedReleasePatch(config releaseProductConfig, highestMinor int, releaseNotesURL string, selectLatest func([]string) (string, error)) (string, error) {
	releaseNotesURLs := append([]string{releaseNotesURL}, config.ReleaseNotesFallbackURLs...)
	var err error
	for _, candidateURL := range releaseNotesURLs {
		body, fetchErr := fetchURLBody(candidateURL)
		if fetchErr != nil {
			err = fetchErr
			continue
		}
		versions := uniqueStrings(config.Pattern.FindAllString(body, -1))
		if len(versions) == 0 {
			return "", fmt.Errorf("could not find any %s v1.%d patch releases in %s. The docs page loaded, but its release-note format may have changed.", config.ProductName, highestMinor, candidateURL)
		}
		latest, err := selectLatest(versions)
		if err != nil {
			return "", fmt.Errorf("could not select a %s v1.%d patch release from %s: %w", config.ProductName, highestMinor, candidateURL, err)
		}
		updateReleaseCache(config, highestMinor, candidateURL, latest, versions)
		return latest, nil
	}

	entry, cachePath, cacheErr := cachedRelease(config, highestMinor)
	if cacheErr == nil {
		log.Printf("[resolver] Warning: using cached %s release lookup for v1.%d from %s because live docs lookup failed: %v", config.ProductName, highestMinor, cachePath, err)
		return entry.LatestVersion, nil
	}
	if config.GitHubTagRefsURL != "" {
		latest, fallbackErr := resolveLatestGitHubTagRelease(config, highestMinor)
		if fallbackErr == nil {
			log.Printf("[resolver] Warning: using %s GitHub tags for v1.%d because docs lookup failed and no cached lookup was available: %v", config.ProductName, highestMinor, err)
			return latest, nil
		}
		log.Printf("[resolver] Warning: %s GitHub tag fallback failed for v1.%d after docs lookup failed: %v", config.ProductName, highestMinor, fallbackErr)
	}
	return "", releaseLookupError(config.ProductName, releaseNotesURL, err, cachePath, cacheErr)
}

func resolveLatestGitHubTagRelease(config releaseProductConfig, highestMinor int) (string, error) {
	body, err := fetchURLBody(config.GitHubTagRefsURL)
	if err != nil {
		return "", err
	}
	var refs []githubTagRef
	if err := json.Unmarshal([]byte(body), &refs); err != nil {
		return "", fmt.Errorf("failed to parse GitHub tag refs from %s: %w", config.GitHubTagRefsURL, err)
	}
	versions := make([]string, 0, len(refs))
	for _, ref := range refs {
		version := strings.TrimPrefix(strings.TrimSpace(ref.Ref), "refs/tags/")
		if config.Pattern.MatchString(version) {
			versions = append(versions, version)
		}
	}
	versions = uniqueStrings(versions)
	if len(versions) == 0 {
		return "", fmt.Errorf("could not find any %s v1.%d patch tags in %s", config.ProductName, highestMinor, config.GitHubTagRefsURL)
	}
	if len(config.GitHubAssetNames) > 0 {
		versions, err = githubTagReleasesWithAssets(config, versions)
		if err != nil {
			return "", err
		}
		if len(versions) == 0 {
			return "", fmt.Errorf("could not find any %s v1.%d patch tags with required assets %s", config.ProductName, highestMinor, strings.Join(config.GitHubAssetNames, ", "))
		}
	}
	latest, err := highestSemverReleaseVersion(versions, config.GitHubBuildPrefix)
	if err != nil {
		return "", fmt.Errorf("could not select a %s v1.%d patch release from GitHub tags: %w", config.ProductName, highestMinor, err)
	}
	updateReleaseCache(config, highestMinor, config.GitHubTagRefsURL, latest, versions)
	return latest, nil
}

func githubTagReleasesWithAssets(config releaseProductConfig, versions []string) ([]string, error) {
	if strings.TrimSpace(config.GitHubReleaseURL) == "" {
		return nil, fmt.Errorf("%s GitHub release asset validation requires GitHubReleaseURL", config.ProductName)
	}
	filtered := make([]string, 0, len(versions))
	for _, version := range versions {
		releaseURL := fmt.Sprintf(config.GitHubReleaseURL, url.PathEscape(version))
		body, err := fetchURLBody(releaseURL)
		if err != nil {
			log.Printf("[resolver] Warning: skipping %s tag %s because release metadata lookup failed: %v", config.ProductName, version, err)
			continue
		}
		var release githubRelease
		if err := json.Unmarshal([]byte(body), &release); err != nil {
			log.Printf("[resolver] Warning: skipping %s tag %s because release metadata could not be parsed: %v", config.ProductName, version, err)
			continue
		}
		if githubReleaseHasAssets(release, config.GitHubAssetNames) {
			filtered = append(filtered, version)
		}
	}
	return filtered, nil
}

func githubReleaseHasAssets(release githubRelease, requiredNames []string) bool {
	if len(requiredNames) == 0 {
		return true
	}
	assets := map[string]bool{}
	for _, asset := range release.Assets {
		assets[asset.Name] = true
	}
	for _, name := range requiredNames {
		if !assets[name] {
			return false
		}
	}
	return true
}

func releaseLookupError(productName, url string, liveErr error, cachePath string, cacheErr error) error {
	cacheMessage := "no valid cached lookup was available"
	if cacheErr != nil && !errors.Is(cacheErr, os.ErrNotExist) {
		cacheMessage = fmt.Sprintf("cached lookup at %s could not be used: %v", cachePath, cacheErr)
	} else if cachePath != "" {
		cacheMessage = fmt.Sprintf("no cached lookup was found at %s", cachePath)
	}

	var statusErr httpStatusError
	if errors.As(liveErr, &statusErr) {
		return fmt.Errorf("%s release-note lookup is unavailable: docs returned HTTP %d for %s, and %s. This may be a missing docs page for that Kubernetes minor or a temporary docs outage; try a supported minor that has published release notes, or rerun after a successful lookup has populated the cache", productName, statusErr.StatusCode, url, cacheMessage)
	}
	return fmt.Errorf("%s release-note lookup is unavailable: failed to fetch %s (%v), and %s. Check network access to the docs site or rerun after a successful lookup has populated the cache", productName, url, liveErr, cacheMessage)
}

func resolveCachedSupportRange(productName, supportMatrixURL string, liveErr error) (int, string, error) {
	entry, cachePath, cacheErr := cachedSupportRange(productName, supportMatrixURL)
	if cacheErr == nil {
		log.Printf("[resolver] Warning: using cached %s support range from %s because live support matrix lookup failed: %v", productName, cachePath, liveErr)
		return entry.MaxMinor, entry.Range, nil
	}
	cacheMessage := "no valid cached support range was available"
	if cacheErr != nil && !errors.Is(cacheErr, os.ErrNotExist) {
		cacheMessage = fmt.Sprintf("cached support range at %s could not be used: %v", cachePath, cacheErr)
	} else if cachePath != "" {
		cacheMessage = fmt.Sprintf("no cached support range was found at %s", cachePath)
	}
	return 0, "", fmt.Errorf("%s support matrix lookup is unavailable: failed to resolve %s (%v), and %s. Check access to the SUSE support matrix or rerun after a successful lookup has populated the cache", productName, supportMatrixURL, liveErr, cacheMessage)
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func firstReleaseVersion(matches []string) (string, error) {
	if len(matches) == 0 {
		return "", fmt.Errorf("no releases found")
	}
	return matches[0], nil
}

func highestSemverReleaseVersion(matches []string, buildSeparator string) (string, error) {
	var bestVersion *goversion.Version
	bestOriginal := ""
	for _, match := range matches {
		normalized := strings.TrimPrefix(strings.Replace(match, buildSeparator, "-"+strings.TrimPrefix(buildSeparator, "+"), 1), "v")
		parsed, err := goversion.NewVersion(normalized)
		if err != nil {
			continue
		}
		if bestVersion == nil || parsed.GreaterThan(bestVersion) {
			bestVersion = parsed
			bestOriginal = match
		}
	}
	if bestOriginal == "" {
		return "", fmt.Errorf("cached/live release strings could not be parsed")
	}
	return bestOriginal, nil
}
