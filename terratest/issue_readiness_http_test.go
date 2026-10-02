package test

import (
	"context"
	"encoding/json"
	"github.com/brudnak/ha-rancher-rke2/internal/prbuild"

	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadinessQANoneStreamsFreshStateWithoutImageScan(t *testing.T) {
	p := clusterWorkspaceTestPanel(t)
	p.prBuildVerifier = prbuild.NewWithOptions(prbuild.Options{RunCommand: func(_ context.Context, _ string, args, _ []string, _ int64) ([]byte, error) {
		if !strings.Contains(strings.Join(args, " "), "repos/rancher/rancher/issues/50186") {
			t.Fatalf("QA/None performed unnecessary scan: %v", args)
		}
		return []byte(`{"number":50186,"state":"open","title":"No QA needed","labels":[{"name":"QA/None"}]}`), nil
	}})
	request := httptest.NewRequest(http.MethodPost, "/api/issue-readiness", nil)
	response := httptest.NewRecorder()
	p.streamIssueReadiness(response, request, prbuild.Request{IssueURL: "https://github.com/rancher/rancher/issues/50186"})
	var events []struct {
		Type   string         `json:"type"`
		Report prbuild.Report `json:"report"`
	}
	for _, line := range strings.Split(strings.TrimSpace(response.Body.String()), "\n") {
		var event struct {
			Type   string         `json:"type"`
			Report prbuild.Report `json:"report"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
	if len(events) < 2 || events[0].Type != "progress" || events[len(events)-1].Report.Verdict != "qa_not_required" || !response.Flushed {
		t.Fatalf("stream=%s", response.Body.String())
	}
}
