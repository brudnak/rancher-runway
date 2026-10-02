package prbuild

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	"net/http"
	"strconv"
	"strings"
)

func (s *Service) fetchPullFromGitHub(ctx context.Context, target Target) (GitHubPull, error) {
	endpoint := fmt.Sprintf("/repos/%s/%s/pulls/%d", target.owner, target.repository, target.number)
	jq := `{body:((.body // "")[0:16000]),number:.number,html_url:.html_url,title:.title,state:.state,draft:.draft,merged:.merged,merged_at:.merged_at,merge_commit_sha:.merge_commit_sha,head:{sha:.head.sha,ref:.head.ref,repo:{full_name:.head.repo.full_name}},base:{sha:.base.sha,ref:.base.ref,repo:{full_name:.base.repo.full_name}}}`
	var result GitHubPull
	if err := s.runGitHubJSON(ctx, endpoint, jq, &result, "pull request"); err != nil {
		return GitHubPull{}, err
	}
	return result, nil
}

func (s *Service) compareCommitsOnGitHub(ctx context.Context, target Target, baseSHA, headSHA string) (GitHubCompare, error) {
	if !imagelookup.GitRevisionPattern.MatchString(baseSHA) || !imagelookup.GitRevisionPattern.MatchString(headSHA) {
		return GitHubCompare{}, errors.New("GitHub ancestry check requires full commit SHAs")
	}
	endpoint := fmt.Sprintf("/repos/%s/%s/compare/%s...%s", target.owner, target.repository, strings.ToLower(baseSHA), strings.ToLower(headSHA))
	jq := `{status:.status,ahead_by:.ahead_by,behind_by:.behind_by,html_url:.html_url,merge_base_sha:.merge_base_commit.sha}`
	var result GitHubCompare
	if err := s.runGitHubJSON(ctx, endpoint, jq, &result, "commit ancestry"); err != nil {
		return GitHubCompare{}, err
	}
	result.Status = strings.ToLower(strings.TrimSpace(result.Status))
	result.MergeBaseSHA = strings.ToLower(strings.TrimSpace(result.MergeBaseSHA))
	return result, nil
}

func (s *Service) runGitHubJSON(ctx context.Context, endpoint, jq string, destination any, operation string) error {
	commandCtx, cancel := context.WithTimeout(ctx, prBuildGitHubTimeout)
	defer cancel()
	arguments := []string{
		"api",
		"--include",
		"--hostname", "github.com",
		"--method", http.MethodGet,
		"-H", "Accept:application/vnd.github+json",
		endpoint,
		"--jq", jq,
	}
	raw, err := s.runCommand(commandCtx, "gh", arguments, imagelookup.SanitizedGHEnvironment(), prBuildGitHubOutputLimit)
	status, payload := ParsePRBuildGitHubIncludedResponse(raw)
	if err != nil {
		switch {
		case commandCtx.Err() != nil:
			return fmt.Errorf("GitHub %s lookup failed: %w", operation, commandCtx.Err())
		case errors.Is(err, imagelookup.ErrImageLookupCommandOutputLimit):
			return fmt.Errorf("GitHub %s response exceeded the safe output limit", operation)
		case status >= 400:
			return &GitHubHTTPError{Status: status, Operation: operation}
		default:
			return fmt.Errorf("could not read GitHub %s; confirm GitHub CLI authentication and repository access", operation)
		}
	}
	if status != 0 && (status < 200 || status >= 300) {
		return &GitHubHTTPError{Status: status, Operation: operation}
	}
	if err := json.Unmarshal(payload, destination); err != nil {
		return fmt.Errorf("GitHub returned invalid %s metadata", operation)
	}
	return nil
}

func ParsePRBuildGitHubIncludedResponse(raw []byte) (int, []byte) {
	remaining := string(raw)
	status := 0
	for strings.HasPrefix(remaining, "HTTP/") {
		headerEnd := strings.Index(remaining, "\r\n\r\n")
		separatorLength := len("\r\n\r\n")
		if headerEnd < 0 {
			headerEnd = strings.Index(remaining, "\n\n")
			separatorLength = len("\n\n")
		}
		if headerEnd < 0 {
			return 0, raw
		}
		header := remaining[:headerEnd]
		firstLine := header
		if lineEnd := strings.IndexByte(firstLine, '\n'); lineEnd >= 0 {
			firstLine = firstLine[:lineEnd]
		}
		fields := strings.Fields(strings.TrimSpace(firstLine))
		if len(fields) < 2 {
			return 0, raw
		}
		parsedStatus, err := strconv.Atoi(fields[1])
		if err != nil || parsedStatus < 100 || parsedStatus > 599 {
			return 0, raw
		}
		status = parsedStatus
		remaining = remaining[headerEnd+separatorLength:]
	}
	return status, []byte(strings.TrimSpace(remaining))
}

func safePRBuildGitHubError(err error, operation string) string {
	var githubErr *GitHubHTTPError
	if errors.As(err, &githubErr) {
		return githubErr.Error()
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return fmt.Sprintf("GitHub %s lookup timed out or was cancelled", operation)
	}
	return fmt.Sprintf("GitHub could not check %s; confirm GitHub CLI authentication and repository access", operation)
}
