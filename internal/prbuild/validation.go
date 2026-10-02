package prbuild

import (
	"errors"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

func parsePRBuildPullRequestURL(input string) (Target, error) {
	hasControl := strings.IndexFunc(input, unicode.IsControl) >= 0
	input = strings.TrimSpace(input)
	invalid := func() (Target, error) {
		return Target{}, &imagelookup.InputError{Message: "pullRequest must be an exact https://github.com/{owner}/{repository}/pull/{number} URL"}
	}
	if input == "" || len(input) > 512 || hasControl || imagelookup.HasUnsafeCharacters(input) {
		return invalid()
	}
	parsed, err := url.Parse(input)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" || parsed.Hostname() != "github.com" || parsed.Port() != "" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.RawPath != "" {
		return invalid()
	}
	pathValue := strings.TrimSuffix(parsed.Path, "/")
	parts := strings.Split(strings.TrimPrefix(pathValue, "/"), "/")
	if len(parts) != 4 || parts[2] != "pull" || !imagelookup.GitHubPathComponent(parts[0]) || !imagelookup.GitHubPathComponent(parts[1]) {
		return invalid()
	}
	number, numberErr := strconv.Atoi(parts[3])
	if numberErr != nil || number < 1 || number > 1_000_000_000 || strconv.Itoa(number) != parts[3] {
		return invalid()
	}
	canonical := fmt.Sprintf("https://github.com/%s/%s/pull/%d", parts[0], parts[1], number)
	if input != canonical && input != canonical+"/" {
		return invalid()
	}
	return Target{owner: parts[0], repository: parts[1], number: number, url: canonical}, nil
}

func normalizePRBuildHeadTag(input string) (string, string, error) {
	input = strings.TrimSpace(input)
	if input == "" || len(input) > 128 || imagelookup.HasUnsafeCharacters(input) {
		return "", "", &imagelookup.InputError{Message: "tag must be head or a minor-line head tag such as 2.14-head"}
	}
	matches := prBuildHeadTagPattern.FindStringSubmatch(input)
	if matches == nil {
		return "", "", &imagelookup.InputError{Message: "tag must be head or a minor-line head tag such as 2.14-head"}
	}
	normalized := strings.ToLower(input)
	if normalized != "head" && !strings.HasPrefix(normalized, "v") {
		normalized = "v" + normalized
	}
	minorLine := ""
	if len(matches) > 1 {
		minorLine = strings.ToLower(matches[1])
	}
	return normalized, minorLine, nil
}

func prBuildBaseRefMatchesMinorLine(baseRef, minorLine string) bool {
	baseRef = strings.ToLower(strings.TrimSpace(baseRef))
	minorLine = strings.ToLower(strings.TrimSpace(minorLine))
	if baseRef == "" || minorLine == "" {
		return false
	}
	patterns := []string{
		minorLine,
		"v" + minorLine,
		"release-" + minorLine,
		"release-v" + minorLine,
		"release/" + minorLine,
		"release/v" + minorLine,
	}
	for _, pattern := range patterns {
		if baseRef == pattern {
			return true
		}
	}
	return false
}

func normalizePRBuildPullRequest(target Target, raw GitHubPull) (PullRequest, error) {
	githubError := func(message string) (PullRequest, error) {
		return PullRequest{}, errors.New(message)
	}
	wantRepository := target.owner + "/" + target.repository
	if raw.Number != target.number || !strings.EqualFold(raw.Base.Repo.FullName, wantRepository) {
		return githubError("GitHub returned PR metadata for a different repository or pull request")
	}
	headSHA := strings.ToLower(strings.TrimSpace(raw.Head.SHA))
	if !imagelookup.GitRevisionPattern.MatchString(headSHA) {
		return githubError("GitHub PR metadata did not include a valid full head commit SHA")
	}
	mergeSHA := strings.ToLower(strings.TrimSpace(raw.MergeCommitSHA))
	inclusionSHA := headSHA
	inclusionBasis := "pr_head"
	if raw.Merged {
		if !imagelookup.GitRevisionPattern.MatchString(mergeSHA) {
			return githubError("Merged GitHub PR metadata did not include a valid integration commit SHA")
		}
		inclusionSHA = mergeSHA
		inclusionBasis = "merged_commit"
	} else {
		mergeSHA = ""
	}
	state := strings.ToLower(imagelookup.SafeOCIProvenanceLabel(raw.State))
	if raw.Merged {
		state = "merged"
	} else if state != "open" && state != "closed" {
		state = "unknown"
	}
	return PullRequest{
		URL:                target.url,
		Repository:         wantRepository,
		Number:             target.number,
		Title:              imagelookup.SafeOCIProvenanceLabel(raw.Title),
		State:              state,
		Draft:              raw.Draft,
		Merged:             raw.Merged,
		MergedAt:           imagelookup.SafeOCIProvenanceLabel(raw.MergedAt),
		BaseRef:            imagelookup.SafeOCIProvenanceLabel(raw.Base.Ref),
		HeadRef:            imagelookup.SafeOCIProvenanceLabel(raw.Head.Ref),
		HeadRepository:     imagelookup.SafeOCIProvenanceLabel(raw.Head.Repo.FullName),
		HeadSHA:            headSHA,
		MergeCommitSHA:     mergeSHA,
		InclusionCommitSHA: inclusionSHA,
		InclusionCommitURL: fmt.Sprintf("https://github.com/%s/%s/commit/%s", target.owner, target.repository, inclusionSHA),
		InclusionBasis:     inclusionBasis,
	}, nil
}
