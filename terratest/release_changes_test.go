package test

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestReleaseDepartureExplainsMilestoneAndLabel(t *testing.T) {
	issue := workIssue(57584, "open")
	issue.Milestone = &issueRadarMilestone{Number: 22, Title: "v2.16.0"}
	tracker := releaseTracker{IssueLabel: "team/frameworks", Milestones: []releaseMilestone{{Config: myWorkConfig{Repo: "rancher/rancher", Milestone: 15, Scope: "all"}}}}
	reason := releaseDepartureReason(issue, tracker)
	if !strings.Contains(reason, "Milestone changed to v2.16.0") || !strings.Contains(reason, "Label removed: team/frameworks") {
		t.Fatal(reason)
	}
}

func TestReleaseRefreshReportsDeparturesAndOwnership(t *testing.T) {
	p := clusterWorkspaceTestPanel(t)
	previous := workIssue(1, "open")
	current := previous
	_ = json.Unmarshal([]byte(`[{"login":"alice"}]`), &previous.Assignees)
	_ = json.Unmarshal([]byte(`[{"login":"bob"}]`), &current.Assignees)
	gone := workIssue(2, "open")
	before := releaseTracker{IssueLabel: "team/frameworks", Milestones: []releaseMilestone{{Config: myWorkConfig{Repo: "rancher/rancher", Milestone: 19, Scope: "all"}, Snapshot: func() *myWorkSnapshot { s := workSnapshot(previous, gone); return &s }()}}}
	after := before
	after.Milestones = []releaseMilestone{{Config: before.Milestones[0].Config, Snapshot: func() *myWorkSnapshot { s := workSnapshot(current); return &s }()}}
	p.issueRadar = &issueRadarService{runCommand: radarRunner(t, func(endpoint *url.URL) ([]byte, error) {
		if !strings.HasSuffix(endpoint.Path, "/issues/2") {
			t.Fatal(endpoint)
		}
		moved := gone
		moved.Milestone = &issueRadarMilestone{Number: 22, Title: "v2.16.0"}
		return radarResponse(t, moved), nil
	})}
	changes := p.releaseChanges(context.Background(), before, after)
	if len(changes) != 2 {
		t.Fatalf("missing changes: %+v", changes)
	}
	if !strings.Contains(changes[0].Reason, "@alice → @bob") {
		t.Fatal(changes)
	}
	if !strings.Contains(changes[1].Reason, "Milestone changed to v2.16.0") || !strings.Contains(changes[1].Reason, "Label removed: team/frameworks") {
		t.Fatal(changes)
	}
}
