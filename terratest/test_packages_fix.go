package test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// A fix lookup is an explicit, read-only GitHub request made through the Test
// Lab connection. The lookup saves nothing: the tester reviews the result and
// chooses what the package or session records (label, linked issue, commit).
var testPackagePullRequestPattern = regexp.MustCompile(`^https://github\.com/([A-Za-z0-9](?:[A-Za-z0-9-]{0,38})?)/([A-Za-z0-9_.-]{1,100})/pull/([1-9][0-9]{0,9})(?:/(?:files|commits|checks)?)?$`)

// Closing keywords as GitHub links them: "Fixes #12", "closes org/repo#12", or a
// full issue URL. Only the first 64 KiB of the description is scanned.
var testPackageClosingIssuePattern = regexp.MustCompile(`(?i)\b(?:close|closes|closed|fix|fixes|fixed|resolve|resolves|resolved)\b[:\s]+(?:https://github\.com/([A-Za-z0-9-]{1,39})/([A-Za-z0-9_.-]{1,100})/issues/([1-9][0-9]{0,9})|([A-Za-z0-9-]{1,39})/([A-Za-z0-9_.-]{1,100})#([1-9][0-9]{0,9})|#([1-9][0-9]{0,9}))`)

var testPackageCommitPattern = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

type testPackagePullRequest struct {
	Owner, Repo string
	Number      int
}

func (r testPackagePullRequest) URL() string {
	return fmt.Sprintf("https://github.com/%s/%s/pull/%d", r.Owner, r.Repo, r.Number)
}

func parseTestPackagePullRequest(raw string) (testPackagePullRequest, bool) {
	match := testPackagePullRequestPattern.FindStringSubmatch(strings.TrimSpace(raw))
	if match == nil {
		return testPackagePullRequest{}, false
	}
	number, err := strconv.Atoi(match[3])
	if err != nil || number <= 0 {
		return testPackagePullRequest{}, false
	}
	return testPackagePullRequest{Owner: match[1], Repo: match[2], Number: number}, true
}

func packageCommitSHA(value string) bool {
	return value == "" || testPackageCommitPattern.MatchString(value)
}

// testPackageFixIssues lists the issues a pull request says it closes, as
// https URLs in order of appearance, without duplicates. It reads text only;
// it never resolves whether those issues exist.
func testPackageFixIssues(owner, repo, body string) []string {
	if len(body) > 64<<10 {
		body = body[:64<<10]
	}
	issues := []string{}
	seen := map[string]bool{}
	for _, match := range testPackageClosingIssuePattern.FindAllStringSubmatch(body, -1) {
		var url string
		switch {
		case match[3] != "":
			url = fmt.Sprintf("https://github.com/%s/%s/issues/%s", match[1], match[2], match[3])
		case match[6] != "":
			url = fmt.Sprintf("https://github.com/%s/%s/issues/%s", match[4], match[5], match[6])
		default:
			url = fmt.Sprintf("https://github.com/%s/%s/issues/%s", owner, repo, match[7])
		}
		if seen[url] {
			continue
		}
		seen[url] = true
		issues = append(issues, url)
		if len(issues) == 10 {
			break
		}
	}
	return issues
}

type testPackageFixLookup struct {
	URL        string     `json:"url"`
	Repository string     `json:"repository"`
	Number     int        `json:"number"`
	Title      string     `json:"title"`
	State      string     `json:"state"` // open, closed, merged
	Draft      bool       `json:"draft"`
	HeadSHA    string     `json:"headSha"`
	HeadRef    string     `json:"headRef"`
	BaseRef    string     `json:"baseRef"`
	MergedAt   *time.Time `json:"mergedAt,omitempty"`
	MergeSHA   string     `json:"mergeSha,omitempty"`
	Issues     []string   `json:"issues"`
	Label      string     `json:"label"`
	FetchedAt  time.Time  `json:"fetchedAt"`
}

type testPackageGitHubPull struct {
	Title          string     `json:"title"`
	State          string     `json:"state"`
	Draft          bool       `json:"draft"`
	Merged         bool       `json:"merged"`
	MergedAt       *time.Time `json:"merged_at"`
	MergeCommitSHA string     `json:"merge_commit_sha"`
	Body           string     `json:"body"`
	Head           struct {
		SHA string `json:"sha"`
		Ref string `json:"ref"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

func testPackageFixText(value string, max int) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > max {
		value = strings.TrimSpace(value[:max])
	}
	return value
}

func (p *localControlPanel) testPackageFixLookup(ctx context.Context, req testPackageRequest) (any, error) {
	reference, ok := parseTestPackagePullRequest(req.FixURL)
	if !ok {
		return nil, errors.New("enter a github.com pull request link such as https://github.com/owner/repo/pull/123")
	}
	github, err := p.testLabService()
	if err != nil {
		return nil, err
	}
	github.githubMu.Lock()
	defer github.githubMu.Unlock()
	github.mu.Lock()
	connected := github.library.GitHub.Connected
	github.mu.Unlock()
	if !connected {
		return nil, errors.New("connect GitHub in Test Lab before looking up a pull request")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var pull testPackageGitHubPull
	if err := github.githubAPI(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/%s/pulls/%d", reference.Owner, reference.Repo, reference.Number), nil, &pull); err != nil {
		return nil, err
	}
	headSHA := strings.ToLower(strings.TrimSpace(pull.Head.SHA))
	if !testPackageCommitPattern.MatchString(headSHA) || !packageEnum(pull.State, "open", "closed") {
		return nil, errors.New("GitHub returned an unexpected pull request record")
	}
	lookup := testPackageFixLookup{URL: reference.URL(), Repository: reference.Owner + "/" + reference.Repo, Number: reference.Number, Title: testPackageFixText(pull.Title, 240), State: pull.State, Draft: pull.Draft, HeadSHA: headSHA, HeadRef: testPackageFixText(pull.Head.Ref, 240), BaseRef: testPackageFixText(pull.Base.Ref, 240), Issues: testPackageFixIssues(reference.Owner, reference.Repo, pull.Body), FetchedAt: time.Now().UTC()}
	if pull.Merged {
		lookup.State = "merged"
		lookup.MergedAt = pull.MergedAt
		if merge := strings.ToLower(strings.TrimSpace(pull.MergeCommitSHA)); testPackageCommitPattern.MatchString(merge) {
			lookup.MergeSHA = merge
		}
	}
	lookup.Label = testPackageFixText(fmt.Sprintf("%s#%d · %s", lookup.Repository, lookup.Number, lookup.Title), 500)
	return map[string]any{"fix": lookup}, nil
}
