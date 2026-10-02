package test

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

func buildSupportMatrixURL(releasedVersion string) string {
	pathVersion := strings.ReplaceAll(releasedVersion, ".", "-")
	return fmt.Sprintf("https://www.suse.com/suse-rancher/support-matrix/all-supported-versions/rancher-v%s/", pathVersion)
}

type supportMatrixVersion struct {
	Major int
	Minor int
	Patch int
}

func (version supportMatrixVersion) String() string {
	return fmt.Sprintf("%d.%d.%d", version.Major, version.Minor, version.Patch)
}

func (version supportMatrixVersion) equal(other supportMatrixVersion) bool {
	return version.Major == other.Major && version.Minor == other.Minor && version.Patch == other.Patch
}

type resolvedSupportMatrixPage struct {
	RequestedURL string
	URL          string
	Body         string
	FallbackNote string
}

type supportMatrixRedirectError struct {
	Requested supportMatrixVersion
	Resolved  supportMatrixVersion
}

func (err supportMatrixRedirectError) Error() string {
	return fmt.Sprintf("support matrix request for Rancher %s resolved to Rancher %s", err.Requested, err.Resolved)
}

func resolveSupportMatrixPage(requestedURL string) (resolvedSupportMatrixPage, error) {
	requestedVersion, err := supportMatrixVersionFromURL(requestedURL)
	if err != nil {
		return resolvedSupportMatrixPage{RequestedURL: requestedURL, URL: requestedURL}, err
	}

	body, resolvedURL, fetchErr := fetchURLBodyWithResolvedURL(requestedURL)
	if fetchErr == nil {
		if resolvedVersion, resolvedErr := supportMatrixVersionFromURL(resolvedURL); resolvedErr == nil && !resolvedVersion.equal(requestedVersion) {
			fetchErr = supportMatrixRedirectError{Requested: requestedVersion, Resolved: resolvedVersion}
		} else {
			return resolvedSupportMatrixPage{RequestedURL: requestedURL, URL: requestedURL, Body: body}, nil
		}
	}
	if !isMissingSupportMatrixPage(fetchErr) {
		return resolvedSupportMatrixPage{RequestedURL: requestedURL, URL: requestedURL}, fetchErr
	}

	indexBody, resolvedIndexURL, indexErr := fetchURLBodyWithResolvedURL(supportMatrixIndexURL)
	if indexErr != nil {
		return resolvedSupportMatrixPage{RequestedURL: requestedURL, URL: requestedURL}, fmt.Errorf("%w; published support-matrix discovery failed: %v", fetchErr, indexErr)
	}
	versions := supportMatrixVersionsFromHTML(indexBody)
	if resolvedIndexVersion, resolvedErr := supportMatrixVersionFromURL(resolvedIndexURL); resolvedErr == nil {
		versions = append(versions, resolvedIndexVersion)
	}
	fallbackVersion, ok := selectSupportMatrixFallback(requestedVersion, versions)
	if !ok {
		return resolvedSupportMatrixPage{RequestedURL: requestedURL, URL: requestedURL}, fmt.Errorf("%w; no published Rancher support matrix at or before the %d.%d release line was listed by %s", fetchErr, requestedVersion.Major, requestedVersion.Minor, supportMatrixIndexURL)
	}

	fallbackURL := buildSupportMatrixURL(fallbackVersion.String())
	fallbackBody, resolvedFallbackURL, fallbackErr := fetchURLBodyWithResolvedURL(fallbackURL)
	if fallbackErr != nil {
		return resolvedSupportMatrixPage{RequestedURL: requestedURL, URL: requestedURL}, fmt.Errorf("%w; fallback support matrix %s also failed: %v", fetchErr, fallbackURL, fallbackErr)
	}
	if resolvedVersion, resolvedErr := supportMatrixVersionFromURL(resolvedFallbackURL); resolvedErr == nil && !resolvedVersion.equal(fallbackVersion) {
		return resolvedSupportMatrixPage{RequestedURL: requestedURL, URL: requestedURL}, fmt.Errorf("%w; fallback support matrix %s resolved to Rancher %s", fetchErr, fallbackURL, resolvedVersion)
	}

	return resolvedSupportMatrixPage{
		RequestedURL: requestedURL,
		URL:          fallbackURL,
		Body:         fallbackBody,
		FallbackNote: fmt.Sprintf("No SUSE support matrix is published for Rancher %s; using Rancher %s as the nearest published compatibility proxy (the requested build is not being reported as certified)", requestedVersion, fallbackVersion),
	}, nil
}

func supportMatrixVersionFromURL(value string) (supportMatrixVersion, error) {
	match := supportMatrixURLVersionPattern.FindStringSubmatch(strings.TrimSpace(value))
	if len(match) != 4 {
		return supportMatrixVersion{}, fmt.Errorf("could not determine Rancher version from support matrix URL %q", value)
	}
	major, majorErr := strconv.Atoi(match[1])
	minor, minorErr := strconv.Atoi(match[2])
	patch, patchErr := strconv.Atoi(match[3])
	if majorErr != nil || minorErr != nil || patchErr != nil {
		return supportMatrixVersion{}, fmt.Errorf("invalid Rancher version in support matrix URL %q", value)
	}
	return supportMatrixVersion{Major: major, Minor: minor, Patch: patch}, nil
}

func supportMatrixVersionsFromHTML(body string) []supportMatrixVersion {
	matches := supportMatrixURLVersionPattern.FindAllStringSubmatch(body, -1)
	versions := make([]supportMatrixVersion, 0, len(matches))
	seen := map[supportMatrixVersion]bool{}
	for _, match := range matches {
		if len(match) != 4 {
			continue
		}
		major, majorErr := strconv.Atoi(match[1])
		minor, minorErr := strconv.Atoi(match[2])
		patch, patchErr := strconv.Atoi(match[3])
		if majorErr != nil || minorErr != nil || patchErr != nil {
			continue
		}
		version := supportMatrixVersion{Major: major, Minor: minor, Patch: patch}
		if !seen[version] {
			seen[version] = true
			versions = append(versions, version)
		}
	}
	return versions
}

func selectSupportMatrixFallback(requested supportMatrixVersion, versions []supportMatrixVersion) (supportMatrixVersion, bool) {
	var sameMinor *supportMatrixVersion
	sameMinorDistance := 0
	for _, candidate := range versions {
		if candidate.equal(requested) || candidate.Major != requested.Major || candidate.Minor != requested.Minor {
			continue
		}
		distance := candidate.Patch - requested.Patch
		if distance < 0 {
			distance = -distance
		}
		if sameMinor == nil || distance < sameMinorDistance || (distance == sameMinorDistance && candidate.Patch < sameMinor.Patch) {
			candidateCopy := candidate
			sameMinor = &candidateCopy
			sameMinorDistance = distance
		}
	}
	if sameMinor != nil {
		return *sameMinor, true
	}

	var earlier *supportMatrixVersion
	for _, candidate := range versions {
		if candidate.Major != requested.Major || candidate.Minor >= requested.Minor {
			continue
		}
		if earlier == nil || candidate.Minor > earlier.Minor || (candidate.Minor == earlier.Minor && candidate.Patch > earlier.Patch) {
			candidateCopy := candidate
			earlier = &candidateCopy
		}
	}
	if earlier == nil {
		return supportMatrixVersion{}, false
	}
	return *earlier, true
}

func isMissingSupportMatrixPage(err error) bool {
	var statusErr httpStatusError
	if errors.As(err, &statusErr) {
		return statusErr.StatusCode == http.StatusNotFound || statusErr.StatusCode == http.StatusGone
	}
	var redirectErr supportMatrixRedirectError
	return errors.As(err, &redirectErr)
}

func supportMatrixSummary(page resolvedSupportMatrixPage, rangeText string) string {
	if page.FallbackNote == "" {
		return rangeText
	}
	return page.FallbackNote + ". " + rangeText
}

func cacheResolvedSupportMatrixRange(product string, page resolvedSupportMatrixPage, rangeText string, minMinor, maxMinor int) {
	updateSupportRangeCacheWithResolvedURL(product, page.URL, page.URL, rangeText, minMinor, maxMinor)
	if page.RequestedURL != "" && page.RequestedURL != page.URL {
		updateSupportRangeCacheWithResolvedURL(product, page.RequestedURL, page.URL, supportMatrixSummary(page, rangeText), minMinor, maxMinor)
	}
}
