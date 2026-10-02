package prbuild

import (
	"context"
	"encoding/json"
	"fmt"

	"strings"
	"testing"
)

const readinessQAFixture = `## QA Testing
### Root cause
User creation used a different naming prefix.
### What was fixed, or what changes have occurred
Moved naming into Steve.
### Areas or cases that should be tested
- Create a user and check metadata.name.
### What areas could experience regressions?
General object creation.
### Repro steps
Create a user and inspect its name.
`

func TestReadinessQATemplate(t *testing.T) {
	for _, tc := range []struct {
		name, body        string
		detected, content bool
	}{
		{"GitHub QA Testing", readinessQAFixture, true, true},
		{"blank", "## QA Testing\n### Root cause\n<!--\nPlease fill this in\n-->\n### Repro steps\nTBD", true, false},
		{"ordinary issue", "### Describe the bug\nBroken\n### Reproduction steps\nTry it", false, false},
		{"quoted example", "> ## QA Testing\n> ### Root cause\n> example", false, false},
		{"code example", "```markdown\n" + readinessQAFixture + "```", false, false},
		{"code guidance", "## QA Testing\n### Repro steps\n```sh\nkubectl get users\n```", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := detectReadinessQA(tc.body, "https://github.com/rancher/rancher/issues/1#issuecomment-2", "author", "2026-09-30")
			if ok != tc.detected || ok && got.HasContent != tc.content {
				t.Fatalf("detected=%v template=%+v", ok, got)
			}
		})
	}
	if got := summarizeReadinessQA(nil, false); got.State != "unknown" {
		t.Fatalf("incomplete absence: %+v", got)
	}
	if got := summarizeReadinessQA(nil, true); got.State != "not_found" {
		t.Fatalf("complete absence: %+v", got)
	}
}
func readinessTestGraphResponse(key string, nodes any, next bool, cursor string) []byte {
	raw, _ := json.Marshal(map[string]any{"data": map[string]any{"repository": map[string]any{"issue": map[string]any{key: map[string]any{"nodes": nodes, "pageInfo": map[string]any{"hasNextPage": next, "endCursor": cursor}}}}}})
	return raw
}
func TestReadinessDiscoveryIsolatesPermissionsAndPaginatesQA(t *testing.T) {
	service := &Service{runCommand: func(_ context.Context, _ string, args, _ []string, _ int64) ([]byte, error) {
		query := strings.Join(args, " ")
		if strings.Contains(query, "graphql") && strings.Count(query, "{") != strings.Count(query, "}") {
			t.Fatalf("unbalanced GraphQL query: %s", query)
		}
		if strings.Contains(query, "projectItems(") {
			return []byte(`{"data":null,"errors":[{"message":"permission denied"}]}`), nil
		}
		if strings.Contains(query, "comments(") {
			if !strings.Contains(query, "after:") {
				return readinessTestGraphResponse("comments", []any{}, true, "page2"), nil
			}
			return readinessTestGraphResponse("comments", []any{map[string]any{"url": "https://github.com/rancher/rancher/issues/1#issuecomment-2", "body": readinessQAFixture + "\nhttps://github.com/rancher/rancher/pull/42", "author": map[string]string{"login": "qa"}}}, false, ""), nil
		}
		if strings.Contains(query, "closedByPullRequestsReferences(") {
			return readinessTestGraphResponse("closedByPullRequestsReferences", []any{map[string]string{"url": "https://github.com/Rancher/Rancher/pull/42"}}, false, ""), nil
		}
		return readinessTestGraphResponse("timelineItems", []any{}, false, ""), nil
	}}
	got := service.readinessLinks(context.Background(), "rancher", "rancher", 1)
	if !got.linksComplete || !got.qaComplete || got.workflowComplete || len(got.templates) != 1 || got.refs["https://github.com/rancher/rancher/pull/42"] != "fix" {
		t.Fatalf("discovery=%+v", got)
	}
	service.runCommand = func(_ context.Context, _ string, args, _ []string, _ int64) ([]byte, error) {
		query := strings.Join(args, " ")
		if strings.Contains(query, "graphql") && strings.Count(query, "{") != strings.Count(query, "}") {
			t.Fatalf("unbalanced GraphQL query: %s", query)
		}
		for _, key := range []string{"closedByPullRequestsReferences", "timelineItems", "comments", "projectItems"} {
			if strings.Contains(query, key+"(") {
				return readinessTestGraphResponse(key, []any{}, key == "comments", "same"), nil
			}
		}
		panic("unexpected query")
	}
	got = service.readinessLinks(context.Background(), "rancher", "rancher", 1)
	if got.linksComplete || got.qaComplete || !got.workflowComplete {
		t.Fatalf("stalled pagination=%+v", got)
	}
}
func readinessTestPR(number int, repo, branch string) issueReadinessPR {
	return issueReadinessPR{Relationship: "fix", Pull: PullRequest{Number: number, URL: fmt.Sprintf("https://github.com/%s/pull/%d", repo, number), Repository: repo, BaseRef: branch, Merged: true, State: "closed"}}
}
func readinessTestMatch(pr issueReadinessPR, server, agent string) issueReadinessMatch {
	return issueReadinessMatch{PR: pr.Pull.URL, Server: CommitMatch{Verdict: server}, Agent: CommitMatch{Verdict: agent}}
}
func TestReadinessRequiresEveryFixInSameServerAgentPair(t *testing.T) {
	one, two := readinessTestPR(1, "rancher/rancher", "release-v2.14"), readinessTestPR(2, "rancher/rancher", "release-v2.14")
	base := Report{Issue: Issue{URL: "https://github.com/rancher/rancher/issues/1"}, PRs: []issueReadinessPR{one, two}, LinksComplete: true, PRsComplete: true, Complete: true, TargetLine: "2.14"}
	build := issueReadinessBuild{Line: "2.14", Server: ImageResult{Found: true}, Agent: ImageResult{Found: true}}
	for _, tc := range []struct {
		name    string
		matches []issueReadinessMatch
		ready   bool
	}{
		{"all", []issueReadinessMatch{readinessTestMatch(one, "included", "included"), readinessTestMatch(two, "included", "included")}, true},
		{"missing agent", []issueReadinessMatch{readinessTestMatch(one, "included", "included"), readinessTestMatch(two, "included", "not_included")}, false},
		{"unknown fix", []issueReadinessMatch{readinessTestMatch(one, "included", "included"), readinessTestMatch(two, "unknown", "unknown")}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := base
			b := build
			b.Matches = tc.matches
			r.Builds = []issueReadinessBuild{b}
			summarizeIssueReadiness(&r)
			if (r.Verdict == "ready") != tc.ready {
				t.Fatalf("report=%+v", r)
			}
			if tc.ready {
				r.LinksComplete = false
				summarizeIssueReadiness(&r)
				if r.Verdict == "ready" {
					t.Fatal("incomplete links marked ready")
				}
				r.LinksComplete = true
				r.PRsComplete = false
				summarizeIssueReadiness(&r)
				if r.Verdict == "ready" {
					t.Fatal("incomplete PRs marked ready")
				}
			}
		})
	}
	r := base
	b1, b2 := build, build
	b1.Matches = []issueReadinessMatch{readinessTestMatch(one, "included", "included")}
	b2.Matches = []issueReadinessMatch{readinessTestMatch(two, "included", "included")}
	r.Builds = []issueReadinessBuild{b1, b2}
	summarizeIssueReadiness(&r)
	if r.Verdict == "ready" {
		t.Fatal("combined fixes across different builds")
	}
}
func TestReadinessBackportsAndDependencies(t *testing.T) {
	original := readinessTestPR(1, "rancher/rancher", "main")
	backport := readinessTestPR(2, "rancher/rancher", "release-v2.14")
	backport.BackportOf = []string{original.Pull.URL}
	backport.Relationship = "reference"
	dependency := readinessTestPR(3, "rancher/steve", "main")
	build := issueReadinessBuild{Line: "2.14", Matches: []issueReadinessMatch{readinessTestMatch(backport, "included", "included")}}
	report := Report{Issue: Issue{URL: "https://github.com/rancher/rancher/issues/1"}, PRs: []issueReadinessPR{original, backport}}
	if required, included := readinessBuildCoverage(report, build); required != 1 || included != 1 {
		t.Fatalf("backport=%d/%d", included, required)
	}
	report.PRs = append(report.PRs, dependency)
	if required, included := readinessBuildCoverage(report, build); required != 2 || included != 1 {
		t.Fatalf("dependency skipped: %d/%d", included, required)
	}
	report.PRs[2] = readinessTestPR(3, "rancher/rancher", "feature/unknown")
	if required, _ := readinessBuildCoverage(report, build); required != 2 {
		t.Fatal("unknown branch skipped")
	}
	target := Target{owner: "rancher", repository: "rancher", number: 2}
	for _, body := range []string{"Not a backport of #1", "> Backport of #1", "```\nBackport of #1\n```", "Backport of #1 and #3"} {
		if refs := readinessBackportLinks(body, target); len(refs) != 0 {
			t.Fatalf("ambiguous reference %q accepted: %v", body, refs)
		}
	}
	if refs := readinessBackportLinks("Backport of https://github.com/rancher/rancher/pull/1", target); len(refs) != 1 || refs[0] != original.Pull.URL {
		t.Fatalf("explicit backport missing: %v", refs)
	}
}

func TestReadinessEndToEndKeepsQAAndBlocksIncompletePRs(t *testing.T) {
	for _, count := range []int{1, 31} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			service := &Service{runCommand: func(_ context.Context, _ string, args, _ []string, _ int64) ([]byte, error) {
				query := strings.Join(args, " ")
				if strings.Contains(query, "graphql") && strings.Count(query, "{") != strings.Count(query, "}") {
					t.Fatalf("unbalanced GraphQL query: %s", query)
				}
				if !strings.Contains(query, "graphql") {
					return []byte(`{"number":1,"title":"Issue","state":"open","body":"https://github.com/Rancher/Rancher/pull/1","labels":[{"name":"status/in-review"}],"milestone":{"title":"v2.14"}}`), nil
				}
				for _, key := range []string{"closedByPullRequestsReferences", "timelineItems", "comments", "projectItems"} {
					if !strings.Contains(query, key+"(") {
						continue
					}
					nodes := []any{}
					if key == "closedByPullRequestsReferences" {
						for i := 1; i <= count; i++ {
							nodes = append(nodes, map[string]string{"url": fmt.Sprintf("https://github.com/rancher/rancher/pull/%d", i)})
						}
					}
					if key == "comments" {
						nodes = append(nodes, map[string]string{"url": "https://github.com/rancher/rancher/issues/1#issuecomment-2", "body": readinessQAFixture})
					}
					return readinessTestGraphResponse(key, nodes, false, ""), nil
				}
				panic("unexpected query")
			}, fetchPull: func(_ context.Context, target Target) (GitHubPull, error) {
				pull := testPRBuildPull(target.number)
				pull.Merged = true
				pull.MergeCommitSHA = prBuildTestMergeSHA
				return pull, nil
			}}
			image := ImageResult{Found: true, Revision: prBuildTestMergeSHA, SourceURL: "https://github.com/rancher/rancher"}
			shared := []issueReadinessBuild{{Line: "2.14", Server: image, Agent: image}}
			report, err := service.checkIssue(context.Background(), Request{IssueURL: "https://github.com/rancher/rancher/issues/1"}, func(context.Context) ([]issueReadinessBuild, []string) { return shared, nil })
			if err != nil {
				t.Fatal(err)
			}
			if report.QA.State != "found" || len(report.Statuses) != 1 || len(shared[0].Matches) != 0 {
				t.Fatalf("QA/status/shared images: %+v", report)
			}
			if count == 1 && (report.Verdict != "ready" || len(report.PRs) != 1 || !strings.Contains(report.Recommendation, "status/in-review")) {
				t.Fatalf("positive=%+v", report)
			}
			if count == 31 && (report.Verdict == "ready" || report.LinksComplete || report.Complete) {
				t.Fatal("truncated PR scan declared ready")
			}
		})
	}
}
