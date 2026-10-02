package test

import (
	"bytes"
	"encoding/json"
	"github.com/brudnak/ha-rancher-rke2/internal/cachelab"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestReleaseTrackerPersistenceConflictAndDelete(t *testing.T) {
	s := packageTestService(t)
	tracker := releaseTracker{ID: cachelab.ID(), Name: "October 2026", Month: "2026-10", PlanVersions: []string{"v2.15.3", "v2.14.7"}, Holidays: []string{"2026-10-12"}, Checkpoints: []releaseCheckpoint{{ID: "freeze", Name: "Code freeze", Date: "2026-10-16"}}}
	lib, err := s.saveReleaseTracker("initial", tracker, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.saveReleaseTracker("initial", tracker, false); err == nil {
		t.Fatal("stale write accepted")
	}
	restarted, err := newIssuePackageService(s.root)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := restarted.releaseTrackers()
	if err != nil || len(restored.Trackers) != 1 || restored.Trackers[0].Checkpoints[0].Date != "2026-10-16" || restored.Trackers[0].Month != "2026-10" || strings.Join(restored.Trackers[0].PlanVersions, ",") != "v2.15.3,v2.14.7" {
		t.Fatalf("restart lost dates: %+v %v", restored, err)
	}
	tracker.Checkpoints[0].Date = "2026-10-20"
	tracker.Archived = true
	lib, err = s.saveReleaseTracker(lib.Revision, tracker, false)
	if err != nil {
		t.Fatal(err)
	}
	if !lib.Trackers[0].Archived || lib.Trackers[0].Checkpoints[0].Date != "2026-10-20" {
		t.Fatal("edit not applied")
	}
	pkg := packageTestPlan(t, s)
	lib, err = s.saveReleaseTracker(lib.Revision, tracker, true)
	if err != nil || len(lib.Trackers) != 0 {
		t.Fatalf("delete: %v", err)
	}
	if _, err = s.get(pkg.ID); err != nil {
		t.Fatal("tracker deletion removed a package")
	}
}
func TestReleaseTrackerValidation(t *testing.T) {
	base := releaseTracker{ID: cachelab.ID(), Name: "October", Milestones: []releaseMilestone{{Config: myWorkConfig{Repo: "rancher/rancher", Milestone: 1, Scope: "all"}}}, Checkpoints: []releaseCheckpoint{{ID: "freeze", Name: "Freeze", Date: "2026-10-16", Milestone: "rancher/rancher#1"}}}
	if err := validateReleaseTracker(&base); err != nil {
		t.Fatal(err)
	}
	base.Checkpoints[0].Date = "2026-02-30"
	if validateReleaseTracker(&base) == nil {
		t.Fatal("invalid date accepted")
	}
	base.Checkpoints[0].Date = "2026-10-16"
	base.Checkpoints[0].Milestone = "rancher/rancher#2"
	if validateReleaseTracker(&base) == nil {
		t.Fatal("unlinked milestone accepted")
	}
	base.Checkpoints = nil
	base.Milestones = append(base.Milestones, base.Milestones[0])
	if validateReleaseTracker(&base) == nil {
		t.Fatal("duplicate milestone accepted")
	}
}
func TestReleaseTrackerPreparationPreservesActiveWork(t *testing.T) {
	s := packageTestService(t)
	original, err := s.prepareMyWork(workSnapshot(workIssue(1, "open")))
	if err != nil {
		t.Fatal(err)
	}
	other := workSnapshot(workIssue(2, "open"))
	other.Config.Milestone = 20
	other.Milestone.Number = 20
	prepared, err := s.prepareTrackedWork(other, false)
	if err != nil {
		t.Fatal(err)
	}
	active, err := s.myWork()
	if err != nil || active.Config != original.Config {
		t.Fatalf("tracker replaced active scan scope: %v", err)
	}
	if prepared.BucketID == original.BucketID || len(s.packageLibrarySnapshot().Buckets) != 2 {
		t.Fatal("milestone buckets not isolated")
	}
}

func TestReleaseTrackerAPIRefreshPreservesDatesAndPackages(t *testing.T) {
	p := clusterWorkspaceTestPanel(t)
	s := packageTestService(t)
	p.issuePackages = s
	calls := 0
	closed := false
	p.issueRadar = &issueRadarService{runCommand: radarRunner(t, func(endpoint *url.URL) ([]byte, error) {
		calls++
		if strings.Contains(endpoint.Path, "/milestones/") {
			return radarResponse(t, issueRadarMilestone{Number: 19, Title: "v2.16.0"}), nil
		}
		state := "open"
		if closed {
			state = "closed"
		}
		return radarResponse(t, []issueRadarIssue{workIssue(1, state)}), nil
	})}
	post := func(action, revision string, tracker releaseTracker, authorized bool) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(map[string]any{"action": action, "revision": revision, "tracker": tracker})
		r := httptest.NewRequest("POST", "http://localhost/api/release-trackers", bytes.NewReader(raw))
		r.RemoteAddr = "127.0.0.1:1"
		if authorized {
			r.Header.Set("Origin", "http://localhost")
			r.Header.Set("X-Control-Panel-Token", p.token)
		}
		w := httptest.NewRecorder()
		p.handleReleaseTrackers(w, r)
		return w
	}
	tracker := releaseTracker{ID: cachelab.ID(), Name: "October", Milestones: []releaseMilestone{{Config: myWorkConfig{Repo: "rancher/rancher", Milestone: 19, Scope: "all"}}}, Checkpoints: []releaseCheckpoint{{ID: "freeze", Name: "Freeze", Date: "2026-10-16"}}}
	denied := post("save", "initial", tracker, false)
	if denied.Code == 200 || calls != 0 {
		t.Fatal("unauthorized request reached GitHub")
	}
	w := post("save", "initial", tracker, true)
	if w.Code != 200 {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	var lib releaseTrackerLibrary
	if err := json.Unmarshal(w.Body.Bytes(), &lib); err != nil {
		t.Fatal(err)
	}
	if len(s.packageLibrarySnapshot().Buckets) != 1 {
		t.Fatal("verified milestone did not become a bucket")
	}
	tracker = lib.Trackers[0]
	tracker.Checkpoints[0].Date = "2026-10-20"
	before := calls
	w = post("save", lib.Revision, tracker, true)
	if w.Code != 200 || calls != before {
		t.Fatalf("local date edit refetched GitHub: %d %s", w.Code, w.Body.String())
	}
	_ = json.Unmarshal(w.Body.Bytes(), &lib)
	historyBefore := len(lib.Trackers[0].Milestones[0].Snapshot.History)
	bucketBefore := lib.Trackers[0].Milestones[0].Snapshot.BucketID
	closed = true
	w = post("refresh", lib.Revision, releaseTracker{ID: tracker.ID}, true)
	if w.Code != 200 {
		t.Fatalf("refresh: %s", w.Body.String())
	}
	_ = json.Unmarshal(w.Body.Bytes(), &lib)
	if lib.Trackers[0].Checkpoints[0].Date != "2026-10-20" || lib.Trackers[0].Milestones[0].Snapshot.Issues[0].State != "closed" {
		t.Fatal("refresh lost local dates or did not update GitHub state")
	}
	if snapshot := lib.Trackers[0].Milestones[0].Snapshot; len(snapshot.History) != historyBefore+1 || snapshot.BucketID != bucketBefore {
		t.Fatal("refresh discarded previous history or package bucket")
	}
	if len(s.packages) != 1 {
		t.Fatal("refresh changed package membership")
	}
}

func TestReleaseTrackerDisabledCheckpointPersistence(t *testing.T) {
	s := packageTestService(t)
	tracker := releaseTracker{ID: cachelab.ID(), Name: "Optional dates", Checkpoints: []releaseCheckpoint{{ID: "feature", Kind: "feature-complete", Name: "Feature complete", Disabled: true}, {ID: "freeze", Name: "Code freeze", Date: "2026-10-16", Disabled: true}}}
	lib, err := s.saveReleaseTracker("initial", tracker, false)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := s.releaseTrackers()
	if err != nil {
		t.Fatal(err)
	}
	if !restored.Trackers[0].Checkpoints[1].Disabled || restored.Trackers[0].Checkpoints[1].Date != "2026-10-16" {
		t.Fatal("disabled date was lost")
	}
	tracker.Checkpoints[0].Disabled = false
	if _, err = s.saveReleaseTracker(lib.Revision, tracker, false); err == nil {
		t.Fatal("enabled checkpoint without a date accepted")
	}
	tracker.Checkpoints[0].Date = "2026-10-09"
	if _, err = s.saveReleaseTracker(lib.Revision, tracker, false); err != nil {
		t.Fatal(err)
	}
}

func TestReleaseTrackerAttachSavedWorkPreservesScheduleAndPackages(t *testing.T) {
	p := clusterWorkspaceTestPanel(t)
	s := packageTestService(t)
	p.issuePackages = s
	saved, err := s.prepareMyWork(workSnapshot(workIssue(91, "open")))
	if err != nil {
		t.Fatal(err)
	}
	tracker := releaseTracker{ID: cachelab.ID(), Name: "October 2026", Checkpoints: []releaseCheckpoint{{ID: "freeze", Name: "Code freeze", Date: "2026-10-16"}}}
	lib, err := s.saveReleaseTracker("initial", tracker, false)
	if err != nil {
		t.Fatal(err)
	}
	before := s.packageLibrarySnapshot()
	raw, _ := json.Marshal(map[string]any{"action": "attach-saved-work", "revision": lib.Revision, "tracker": map[string]any{"id": tracker.ID, "name": "Untrusted replacement"}})
	r := httptest.NewRequest("POST", "http://localhost/api/release-trackers", bytes.NewReader(raw))
	r.RemoteAddr = "127.0.0.1:1"
	r.Header.Set("Origin", "http://localhost")
	r.Header.Set("X-Control-Panel-Token", p.token)
	w := httptest.NewRecorder()
	p.handleReleaseTrackers(w, r)
	if w.Code != 200 {
		t.Fatalf("attach: %d %s", w.Code, w.Body.String())
	}
	if err = json.Unmarshal(w.Body.Bytes(), &lib); err != nil {
		t.Fatal(err)
	}
	got := lib.Trackers[0]
	if got.Name != tracker.Name || got.Checkpoints[0].Date != "2026-10-16" || len(got.Milestones) != 1 || got.Milestones[0].Config != saved.Config || len(got.Milestones[0].Snapshot.Issues) != 1 {
		t.Fatalf("attachment lost scope, dates or issues: %+v", got)
	}
	after := s.packageLibrarySnapshot()
	a, _ := json.Marshal(before)
	b, _ := json.Marshal(after)
	if !bytes.Equal(a, b) {
		t.Fatal("attachment changed package library")
	}
	active, err := s.myWork()
	if err != nil || active.Config != saved.Config {
		t.Fatal("attachment changed active work")
	}
}

func TestReleaseTrackerIndividualIssueRefresh(t *testing.T) {
	p := clusterWorkspaceTestPanel(t)
	s := packageTestService(t)
	p.issuePackages = s
	tracker := releaseTracker{ID: cachelab.ID(), Name: "October", Checkpoints: []releaseCheckpoint{{ID: "freeze", Name: "Freeze", Date: "2026-10-16"}}}
	lib, err := s.saveReleaseTracker("initial", tracker, false)
	if err != nil {
		t.Fatal(err)
	}
	state := "open"
	calls := 0
	p.issueRadar = &issueRadarService{runCommand: radarRunner(t, func(endpoint *url.URL) ([]byte, error) {
		calls++
		if strings.TrimPrefix(endpoint.Path, "/") != "repos/rancher/rancher/issues/57584" {
			t.Fatalf("unexpected request: %s", endpoint)
		}
		issue := workIssue(57584, state)
		return radarResponse(t, issue), nil
	})}
	post := func(action string, issue releaseTrackedIssue) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(map[string]any{"action": action, "revision": lib.Revision, "tracker": tracker, "issue": issue})
		r := httptest.NewRequest("POST", "http://localhost/api/release-trackers", bytes.NewReader(raw))
		r.RemoteAddr = "127.0.0.1:1"
		r.Header.Set("Origin", "http://localhost")
		r.Header.Set("X-Control-Panel-Token", p.token)
		w := httptest.NewRecorder()
		p.handleReleaseTrackers(w, r)
		if w.Code == 200 {
			if err := json.Unmarshal(w.Body.Bytes(), &lib); err != nil {
				t.Fatal(err)
			}
		}
		return w
	}
	ref := releaseTrackedIssue{Repo: "rancher/rancher", Number: 57584}
	if w := post("add-issue", ref); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if len(lib.Trackers[0].Issues) != 1 || lib.Trackers[0].Issues[0].Snapshot.Number != 57584 {
		t.Fatal("issue not saved")
	}
	if w := post("add-issue", ref); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if len(lib.Trackers[0].Issues) != 1 || calls != 1 {
		t.Fatal("duplicate add refetched or duplicated issue")
	}
	state = "closed"
	if w := post("refresh", releaseTrackedIssue{}); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	got := lib.Trackers[0]
	if got.Issues[0].Snapshot.State != "closed" || got.Checkpoints[0].Date != "2026-10-16" || got.Issues[0].RefreshedAt.IsZero() {
		t.Fatal("refresh lost dates or failed to update issue")
	}
	restarted, err := newIssuePackageService(s.root)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := restarted.releaseTrackers()
	if err != nil || len(restored.Trackers[0].Issues) != 1 {
		t.Fatal("issue lost after restart")
	}
	before := calls
	if w := post("add-issue", releaseTrackedIssue{Repo: "../bad", Number: 1}); w.Code != 400 || calls != before {
		t.Fatal("invalid issue reached GitHub")
	}
}

func TestReleasePlanResolvesEveryVersion(t *testing.T) {
	tracker := releaseTracker{ID: cachelab.ID(), Name: "October", PlanRepo: "rancher/rancher", IssueLabel: "team/frameworks", PlanVersions: []string{"v2.15.3", "v2.14.7"}}
	available := []issueRadarMilestone{{Number: 15, Title: "2.15.3"}, {Number: 14, Title: "v2.14.7"}, {Number: 16, Title: "v2.16.0"}}
	if err := linkReleasePlan(&tracker, available); err != nil {
		t.Fatal(err)
	}
	if len(tracker.Milestones) != 2 {
		t.Fatal("wrong release lines linked")
	}
	for _, m := range tracker.Milestones {
		if m.Config.Scope != "all" || m.Config.Label != "team/frameworks" || m.Config.Milestone == 16 {
			t.Fatal("wrong release scope")
		}
	}
	if err := linkReleasePlan(&tracker, available); err != nil || len(tracker.Milestones) != 2 {
		t.Fatal("duplicate linking")
	}
	tracker.PlanVersions = append(tracker.PlanVersions, "v2.13.11")
	if err := linkReleasePlan(&tracker, available); err == nil || !strings.Contains(err.Error(), "v2.13.11") {
		t.Fatal("missing release line silently skipped")
	}
	tracker.PlanVersions = []string{"v2.15.3"}
	if err := linkReleasePlan(&tracker, append(available, issueRadarMilestone{Number: 50, Title: "v2.15.3"})); err == nil {
		t.Fatal("ambiguous version selected")
	}
}

func TestReleasePlanScanAllMilestonesAndCustomLabels(t *testing.T) {
	p := clusterWorkspaceTestPanel(t)
	s := packageTestService(t)
	p.issuePackages = s
	label := "team/frameworks"
	queries := 0
	p.issueRadar = &issueRadarService{runCommand: radarRunner(t, func(endpoint *url.URL) ([]byte, error) {
		path := strings.TrimPrefix(endpoint.Path, "/")
		if strings.HasSuffix(path, "/milestones") {
			return radarResponse(t, []issueRadarMilestone{{Number: 15, Title: "v2.15.3"}, {Number: 14, Title: "v2.14.7"}}), nil
		}
		if strings.Contains(path, "/milestones/") {
			n := 15
			if strings.HasSuffix(path, "/14") {
				n = 14
			}
			title := "v2.15.3"
			if n == 14 {
				title = "v2.14.7"
			}
			return radarResponse(t, issueRadarMilestone{Number: n, Title: title}), nil
		}
		if endpoint.Query().Get("labels") != label || endpoint.Query().Get("assignee") != "" {
			t.Fatalf("incorrect scan scope: %s", endpoint)
		}
		queries++
		n := 15
		if endpoint.Query().Get("milestone") == "14" {
			n = 14
		}
		return radarResponse(t, []issueRadarIssue{workIssue(n, "open")}), nil
	})}
	tracker := releaseTracker{ID: cachelab.ID(), Name: "October", PlanVersions: []string{"v2.15.3", "v2.14.7"}, IssueLabel: label}
	revision := "initial"
	post := func(action string) {
		raw, _ := json.Marshal(map[string]any{"action": action, "revision": revision, "tracker": tracker})
		r := httptest.NewRequest("POST", "http://localhost/api/release-trackers", bytes.NewReader(raw))
		r.RemoteAddr = "127.0.0.1:1"
		r.Header.Set("Origin", "http://localhost")
		r.Header.Set("X-Control-Panel-Token", p.token)
		w := httptest.NewRecorder()
		p.handleReleaseTrackers(w, r)
		if w.Code != 200 {
			t.Fatalf("%s: %s", action, w.Body.String())
		}
		var lib releaseTrackerLibrary
		if err := json.Unmarshal(w.Body.Bytes(), &lib); err != nil {
			t.Fatal(err)
		}
		revision = lib.Revision
		tracker = lib.Trackers[0]
	}
	post("save")
	if queries != 2 || len(tracker.Milestones) != 2 {
		t.Fatal("save did not pull entire release")
	}
	label = "my/custom-label"
	tracker.IssueLabel = label
	post("scan")
	if queries != 4 {
		t.Fatal("scan skipped a milestone")
	}
	for _, m := range tracker.Milestones {
		if m.Config.Label != label || m.Snapshot.Config.Label != label || len(m.Snapshot.Issues) != 1 {
			t.Fatal("custom scan not persisted")
		}
	}
	label = ""
	tracker.IssueLabel = ""
	post("scan")
	if queries != 6 {
		t.Fatal("clear label did not scan entire release")
	}
	if len(s.packages) != 2 {
		t.Fatal("scan duplicated packages")
	}
}
