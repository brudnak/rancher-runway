package test

import (
	"github.com/brudnak/ha-rancher-rke2/internal/cachelab"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// An isolated visual fixture: no user library or remote services are touched.
func TestRunwayReleaseTrackersPreview(t *testing.T) {
	if os.Getenv("RUNWAY_RELEASE_PREVIEW") != "1" {
		t.Skip("preview opt-in")
	}
	p := clusterWorkspaceTestPanel(t)
	s := packageTestService(t)
	p.issuePackages = s
	p.trustedLocalOrigin = true
	snap := workSnapshot(workIssue(52858, "open"), workIssue(54281, "open"))
	snap.Milestone.Title = "v2.15.3"
	snap.Issues[0].Title = "Filter System, Obscure Namespaces Server-Side"
	snap.Issues[1].Title = "Installed Apps and CronJobs table is occasionally empty"
	snap, err := s.prepareMyWork(snap)
	if err != nil {
		t.Fatal(err)
	}
	tracker := releaseTracker{ID: cachelab.ID(), Name: "October 2026", Notes: "Preview fixture", Holidays: []string{"2026-10-12"}, Milestones: []releaseMilestone{{Config: snap.Config, Snapshot: &snap}}, Checkpoints: []releaseCheckpoint{{ID: "complete", Name: "Bug complete", Date: "2026-10-09"}, {ID: "freeze", Name: "Code freeze", Date: "2026-10-16"}, {ID: "signoff", Name: "Charts QA sign-off", Date: "2026-10-22"}, {ID: "release", Name: "Release", Date: "2026-10-28", Tentative: true}}}
	if _, err = s.saveReleaseTracker("initial", tracker, false); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", p.handleIndex)
	mux.HandleFunc("/static/", p.handleControlPanelStaticAsset)
	mux.HandleFunc("/api/release-trackers", p.handleReleaseTrackers)
	mux.HandleFunc("/api/issue-packages", p.handleIssuePackages)
	mux.HandleFunc("/api/my-work", p.handleMyWork)
	mux.HandleFunc("/api/my-work/refresh", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, map[string]any{"snapshot": snap}) })
	mux.HandleFunc("/api/issue-readiness/daily", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"enabled": false, "report": nil})
	})
	mux.HandleFunc("/api/cluster-workspaces", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, map[string]any{"clusters": []any{}}) })
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"runs": []any{}, "clusters": []any{}, "awsResources": []any{}, "operations": map[string]any{}, "workspace": map[string]any{"setupAllowed": true}, "panel": map[string]any{"build": map[string]any{"version": "preview"}}})
	})
	mux.HandleFunc("/api/preflight", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ready": true, "summary": "Isolated release preview", "items": []any{}})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	if err = os.WriteFile("/tmp/runway-release-preview.url", []byte(server.URL), 0600); err != nil {
		t.Fatal(err)
	}
	t.Log("Release preview ready")
	for deadline := time.Now().Add(30 * time.Minute); time.Now().Before(deadline); {
		if _, err := os.Stat("/tmp/runway-release-preview.stop"); err == nil {
			return
		}
		time.Sleep(time.Second)
	}
}
