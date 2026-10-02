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
	tracker := releaseTracker{ID: cachelab.ID(), Name: "October 2026", Month: "2026-10", Holidays: []string{"2026-10-12"}, Checkpoints: []releaseCheckpoint{{ID: "freeze", Name: "Code freeze", Date: "2026-10-16"}}}
	lib, err := s.saveReleaseTracker("initial", tracker, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.saveReleaseTracker("initial", tracker, false); err == nil {
		t.Fatal("stale write accepted")
	}
	restarted, err := newTestPackageService(s.root)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := restarted.releaseTrackers()
	if err != nil || len(restored.Trackers) != 1 || restored.Trackers[0].Checkpoints[0].Date != "2026-10-16" || restored.Trackers[0].Month != "2026-10" {
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
	p.testPackages = s
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
	closed = true
	w = post("refresh", lib.Revision, releaseTracker{ID: tracker.ID}, true)
	if w.Code != 200 {
		t.Fatalf("refresh: %s", w.Body.String())
	}
	_ = json.Unmarshal(w.Body.Bytes(), &lib)
	if lib.Trackers[0].Checkpoints[0].Date != "2026-10-20" || lib.Trackers[0].Milestones[0].Snapshot.Issues[0].State != "closed" {
		t.Fatal("refresh lost local dates or did not update GitHub state")
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
	p.testPackages = s
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
