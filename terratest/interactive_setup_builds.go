package test

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	"github.com/brudnak/ha-rancher-rke2/internal/registrycatalog"
)

type setupBuildGroup struct {
	Registry   string   `json:"registry"`
	Repository string   `json:"repository"`
	Tags       []string `json:"tags"`
	Matched    int      `json:"matched"`
	Error      string   `json:"error,omitempty"`
}

var setupMinorLine = regexp.MustCompile(`^\d+\.\d+$`)
var setupZeroPatchHead = regexp.MustCompile(`^v?2\.(\d+)\.0(?:-[a-fA-F0-9]{7,40})?-head$`)

// A minor head includes Community commit tags and patch-qualified Prime builds.
// Match full version components so, for example, 2.1 never includes 2.17.
func setupBuildMatcher(query string) (func(string) bool, error) {
	query = imagelookup.NormalizeVersionInput(query)
	if query == "latest-heads" || query == "2.x.0-head" || query == "2.*.0-head" {
		return setupZeroPatchHead.MatchString, nil
	}
	minor := setupMinorLine.MatchString(query)
	if len(query) > 127 {
		return nil, fmt.Errorf("Rancher version search is limited to 127 characters")
	}
	if query != "" && !minor {
		if _, _, err := classifyRancherVersion(query); err != nil {
			return nil, fmt.Errorf("enter a Rancher version or head selector such as 2.17-head or 2.16.0-head")
		}
	}
	selector := imagelookup.ReadinessHeadPattern.MatchString(query)
	line := strings.TrimSuffix(query, "-head")
	return func(tag string) bool {
		version := imagelookup.NormalizeVersionInput(tag)
		kind, minorLine, err := classifyRancherVersion(version)
		if err != nil {
			return false
		}
		if query == "" || version == query || (minor && query == minorLine) {
			return true
		}
		if !selector || kind != "head" {
			return false
		}
		buildLine, _, _ := strings.Cut(version, "-")
		return query == "head" || line == minorLine || line == buildLine
	}, nil
}

func findSetupBuilds(ctx context.Context, query string, docker bool, list rancherImageTagListFunc) ([]setupBuildGroup, error) {
	match, err := setupBuildMatcher(query)
	if err != nil {
		return nil, err
	}
	exact := imagelookup.NormalizeVersionInput(query)
	// Prefer the requested alias, then the other moving heads on the line.
	rank := func(tag string) int {
		if imagelookup.NormalizeVersionInput(tag) == exact {
			return 0
		}
		if imagelookup.ReadinessHeadPattern.MatchString(tag) {
			return 1
		}
		return 2
	}
	var groups []setupBuildGroup
	for _, registry := range registrycatalog.Preferred {
		for _, repository := range imagelookup.RancherServerRepositories() {
			if docker && repository != "rancher/rancher" {
				continue // Standalone Docker requires the full Rancher image.
			}
			groups = append(groups, setupBuildGroup{Registry: registry, Repository: repository})
		}
	}
	// Start every official repository within the same deadline, including legacy
	// repositories when their newer counterpart is slow or unavailable.
	var workers sync.WaitGroup
	for i := range groups {
		group := &groups[i]
		workers.Go(func() {
			tags, err := list(ctx, group.Registry, group.Repository)
			if err != nil {
				group.Error = imagelookup.SafeError(err)
				return
			}
			for _, tag := range tags {
				// Linode constructs Docker tags from the normalized version field.
				if docker && imagelookup.NormalizeDockerRancherTag(imagelookup.NormalizeVersionInput(tag)) != tag {
					continue
				}
				if match(tag) {
					group.Tags = append(group.Tags, tag)
				}
			}
			slices.SortFunc(group.Tags, func(a, b string) int {
				return cmp.Or(cmp.Compare(rank(a), rank(b)), imagelookup.NaturalCompare(b, a), strings.Compare(b, a))
			})
			group.Tags = slices.Compact(group.Tags)
			group.Matched = len(group.Tags)
		})
	}
	workers.Wait()
	if query == "latest-heads" {
		// Browse the newest three published minor lines and inspect every
		// candidate's date, including immutable builds newer than stale aliases.
		// A SHA's alphabetical order does not identify the newest build.
		lines := []string{}
		for _, group := range groups {
			for _, tag := range group.Tags {
				lines = append(lines, setupZeroPatchHead.FindStringSubmatch(tag)[1])
			}
		}
		slices.SortFunc(lines, func(a, b string) int { return imagelookup.NaturalCompare(b, a) })
		lines = slices.Compact(lines)
		lines = lines[:min(3, len(lines))]
		for i := range groups {
			groups[i].Tags = slices.DeleteFunc(groups[i].Tags, func(tag string) bool {
				line := setupZeroPatchHead.FindStringSubmatch(tag)[1]
				return !slices.Contains(lines, line)
			})
			groups[i].Matched = len(groups[i].Tags)
		}
	}
	return groups, nil
}

type setupBuildDetails struct {
	Reference       string              `json:"reference"`
	Digest          string              `json:"digest"`
	Created         string              `json:"created"`
	Platform        string              `json:"platform"`
	Version         string              `json:"version"`
	Revision        string              `json:"revision"`
	OSSRevision     string              `json:"ossRevision"`
	Moving          bool                `json:"moving"`
	Charts          map[string]string   `json:"charts"`
	CatalogImage    string              `json:"catalogImage,omitempty"`
	ChartRevision   string              `json:"chartRevision,omitempty"`
	CatalogStatus   string              `json:"catalogStatus,omitempty"`
	CatalogVersions map[string][]string `json:"catalogVersions,omitempty"`
}

func inspectSetupBuild(ctx context.Context, ref string, images systemChartImageLookup, catalog *helmLabCatalogService) (setupBuildDetails, error) {
	return inspectSetupBuildWithDetails(ctx, ref, images, catalog, true)
}

func inspectSetupBuildWithDetails(ctx context.Context, ref string, images systemChartImageLookup, catalog *helmLabCatalogService, details bool) (setupBuildDetails, error) {
	registry, repository, tag, err := imagelookup.ParseRegistryImage(ref)
	if err != nil || !slices.Contains(registrycatalog.Preferred, registry) || !imagelookup.IsRancherServerRepository(repository) {
		return setupBuildDetails{}, &imagelookup.InputError{Message: "choose a tagged Rancher server image in a listed registry"}
	}
	inspect := func(reference string) (imagelookup.InspectResponse, error) {
		return images.Inspect(ctx, imagelookup.InspectRequest{Reference: reference, Platform: "linux/amd64", SkipTagMetadata: true})
	}
	image, err := inspect(registry + "/" + repository + ":" + tag)
	if err != nil {
		return setupBuildDetails{}, err
	}
	env := imageEnvironment(image.Config.Env)
	labels := image.Config.Labels
	result := setupBuildDetails{
		Reference: image.Reference, Digest: image.Digest, Created: image.CreatedAt, Platform: image.Platform,
		Version:  cmp.Or(env["CATTLE_SERVER_VERSION"], labels[imagelookup.VersionLabel]),
		Revision: labels[imagelookup.RevisionLabel], OSSRevision: labels[imagelookup.OSSRevisionLabel],
		Moving: imagelookup.ReadinessHeadPattern.MatchString(tag),
	}

	if result.Created == "" {
		if created, err := time.Parse(time.RFC3339Nano, labels["org.opencontainers.image.created"]); err == nil {
			result.Created = created.UTC().Format(time.RFC3339)
		}
	}
	if !details {
		return result, nil
	}
	result.Charts = rancherSystemChartPins(ctx, image, env, images, catalog)

	// A moving alias may identify its immutable build in either the canonical
	// OCI label or CATTLE_SERVER_VERSION. Use it only after checking that the
	// exact tag in this repository still points at the inspected manifest.
	if result.Moving {
		_, canonicalRepo, canonicalTag, _ := imagelookup.ParseRegistryImage(labels[imagelookup.CanonicalReferenceLabel])
		if canonicalRepo != repository {
			canonicalTag = ""
		}
		for _, candidate := range []string{canonicalTag, result.Version} {
			build := imagelookup.NormalizeVersionInput(candidate)
			if !isCommitHeadRancherVersion(build) {
				continue
			}
			line := strings.TrimSuffix(imagelookup.NormalizeVersionInput(tag), "-head")
			buildLine, _, _ := strings.Cut(build, "-")
			if tag != "head" && buildLine != line && !strings.HasPrefix(buildLine, line+".") {
				continue
			}
			pinnedTag := imagelookup.NormalizeDockerRancherTag(build)
			pinned, err := inspect(registry + "/" + repository + ":" + pinnedTag)
			if err == nil && image.Digest != "" && pinned.Digest == image.Digest {
				result.Reference, result.Moving, tag = pinned.Reference, false, pinnedTag
				break
			}
		}
	}

	// The selected Helm chart determines the deployment's assets. Only compare
	// with an identified catalog, and report exactly which image supplied it.
	revision := rancherChartsRevision(image.Config)
	result.CatalogImage = result.Reference
	if !imagelookup.GitRevisionPattern.MatchString(revision) {
		result.CatalogImage = registry + "/rancher/rancher-assets:" + tag
		assets, err := inspect(result.CatalogImage)
		if err != nil {
			result.CatalogStatus = "Could not inspect the same-tag assets catalog."
			if imagelookup.RegistryNotFound(err) {
				result.CatalogStatus = "No same-tag assets image was published."
			}
			return result, nil
		}
		revision = rancherChartsRevision(assets.Config)
	}
	if !imagelookup.GitRevisionPattern.MatchString(revision) {
		result.CatalogStatus = "The image did not publish a pinned chart index."
		return result, nil
	}
	result.ChartRevision = revision
	index, err := readSystemChartIndex(ctx, revision, catalog)
	if err != nil {
		result.CatalogStatus = "Could not read the pinned chart index."
		return result, nil
	}
	result.CatalogVersions = make(map[string][]string, len(result.Charts))
	for chart := range result.Charts {
		versions := []string{}
		for _, entry := range index[chart] {
			versions = append(versions, entry.Version)
		}
		result.CatalogVersions[chart] = versions
	}
	return result, nil
}

func (s *interactiveServer) handleRancherBuilds(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.authorized(r) {
		http.Error(w, "invalid interactive setup token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	query := r.URL.Query()
	if query.Has("reference") {
		ctx, cancel := context.WithTimeout(r.Context(), imagelookup.InspectTimeout)
		defer cancel()
		images := newPreferredRancherImageLookupService()
		defer images.CloseIdleConnections()
		result, err := inspectSetupBuildWithDetails(ctx, query.Get("reference"), images, &helmLabCatalogService{}, query.Get("summary") != "1")
		if err != nil {
			http.Error(w, imagelookup.SafeError(err), imagelookup.HTTPStatus(err))
			return
		}
		writeJSON(w, result)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), imagelookup.SearchTimeout)
	defer cancel()
	groups, err := findSetupBuilds(ctx, query.Get("version"), query.Get("docker") == "1", listRancherImageTags)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, groups)
}
