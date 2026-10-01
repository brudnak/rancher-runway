package test

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestReadinessWorkflowRequiresStatusAndBuildEvidence(t *testing.T) {
	for _, tc := range []struct {
		name                string
		statuses            []string
		complete, build     bool
		issueState, verdict string
		green               bool
		state               string
	}{
		{"to test with build", []string{"To Test"}, true, true, "open", "", true, "qa_ready"},
		{"QA working with build", []string{"QA-Working"}, true, true, "open", "", true, "qa_ready"},
		{"label alias", []string{"status/to-test"}, true, true, "open", "", true, "qa_ready"},
		{"agreement", []string{"To Test", "QA Working"}, true, true, "open", "", true, "qa_ready"},
		{"no build", []string{"To Test"}, true, false, "open", "", false, "qa_ready"},
		{"in progress", []string{"In Progress"}, true, true, "open", "", false, "in_progress"},
		{"in review", []string{"In Review"}, true, true, "open", "", false, "in_review"},
		{"blocked", []string{"Blocked"}, true, true, "open", "", false, "blocked"},
		{"conflicting projects", []string{"To Test", "In Review"}, true, true, "open", "", false, "in_review"},
		{"unrecognized status", []string{"To Test", "Backlog"}, true, true, "open", "", false, "other"},
		{"blank project status", []string{"To Test", "Not set"}, true, true, "open", "", false, "other"},
		{"partial lookup", []string{"To Test"}, false, true, "open", "", false, "unknown"},
		{"no status", nil, true, true, "open", "", false, "unknown"},
		{"closed", []string{"To Test"}, true, true, "closed", "", false, "closed"},
		{"QA none", []string{"To Test"}, true, true, "open", "qa_not_required", false, "not_required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := issueReadinessReport{Issue: issueRadarIssue{State: tc.issueState}, WorkflowComplete: tc.complete, Verdict: "awaiting_build"}
			if tc.build {
				r.Verdict = "ready"
			}
			if tc.verdict != "" {
				r.Verdict = tc.verdict
			}
			for i, status := range tc.statuses {
				r.Statuses = append(r.Statuses, issueReadinessStatus{Project: fmt.Sprintf("Project %d", i), Status: status})
			}
			summarizeReadinessWorkflow(&r)
			if r.Workflow.GreenLight != tc.green || r.Workflow.State != tc.state {
				t.Fatalf("workflow=%+v", r.Workflow)
			}
			if tc.green && !strings.Contains(r.Title, "Green light") {
				t.Fatal("missing green headline")
			}
			if !tc.green && strings.Contains(r.Title, "Green light") {
				t.Fatal("false green headline")
			}
			if tc.build && tc.verdict == "" && r.Verdict != "ready" {
				t.Fatal("workflow erased separately proven build evidence")
			}
		})
	}
}
func TestReadinessGraphQLExplainsMissingProjectScopeOnCommandFailure(t *testing.T) {
	s := &prBuildVerifierService{runCommand: func(context.Context, string, []string, []string, int64) ([]byte, error) {
		return []byte(`{"errors":[{"type":"INSUFFICIENT_SCOPES","message":"The name field requires read:project"}]}`), fmt.Errorf("exit status 1")
	}}
	var result any
	err := s.readinessGraphQL(context.Background(), "query {}", &result)
	if err == nil || !strings.Contains(err.Error(), "requires read:project permission") {
		t.Fatalf("missing actionable scope error: %v", err)
	}
	r := issueReadinessReport{Issue: issueRadarIssue{State: "open"}, Verdict: "ready", Warnings: []string{"projectItems: " + err.Error()}}
	summarizeReadinessWorkflow(&r)
	if r.Workflow.GreenLight || r.Workflow.AccessCommand != "gh auth refresh -h github.com -s read:project" {
		t.Fatalf("workflow=%+v", r.Workflow)
	}
}
func TestReadinessProjectWithoutStatusIsNotDropped(t *testing.T) {
	s := &prBuildVerifierService{runCommand: func(_ context.Context, _ string, args, _ []string, _ int64) ([]byte, error) {
		for _, key := range []string{"closedByPullRequestsReferences", "timelineItems", "comments", "projectItems"} {
			if strings.Contains(strings.Join(args, " "), key+"(") {
				nodes := []any{}
				if key == "projectItems" {
					nodes = append(nodes, map[string]any{"project": map[string]string{"title": "Platform Team"}, "fieldValueByName": nil})
				}
				return readinessTestGraphResponse(key, nodes, false, ""), nil
			}
		}
		return nil, fmt.Errorf("unexpected query")
	}}
	got := s.readinessLinks(context.Background(), "rancher", "rancher", 56397)
	if !got.workflowComplete || len(got.statuses) != 1 || got.statuses[0].Status != "Not set" {
		t.Fatalf("lost unset status: %+v", got)
	}
}
