package test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/cachelab"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type releaseCheckpoint struct {
	Kind      string `json:"kind,omitempty"`
	Disabled  bool   `json:"disabled,omitempty"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	Date      string `json:"date"`
	Milestone string `json:"milestone"` // Empty means shared across the release.
	Tentative bool   `json:"tentative"`
	Source    string `json:"source"`
}
type releaseMilestone struct {
	Config   myWorkConfig    `json:"config"`
	Snapshot *myWorkSnapshot `json:"snapshot,omitempty"`
}
type releaseTracker struct {
	Month       string              `json:"month,omitempty"`
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Notes       string              `json:"notes"`
	Archived    bool                `json:"archived"`
	Holidays    []string            `json:"holidays"`
	Checkpoints []releaseCheckpoint `json:"checkpoints"`
	Milestones  []releaseMilestone  `json:"milestones"`
}
type releaseTrackerLibrary struct {
	Revision string           `json:"revision"`
	Trackers []releaseTracker `json:"trackers"`
}

func (s *testPackageService) releaseTrackersLocked() (releaseTrackerLibrary, error) {
	out := releaseTrackerLibrary{Revision: "initial", Trackers: []releaseTracker{}}
	raw, err := testPackageReadBounded(filepath.Join(s.root, ".release-trackers.json"), 64<<20)
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	err = decodeTestPackage(raw, &out)
	return out, err
}
func (s *testPackageService) releaseTrackers() (releaseTrackerLibrary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.releaseTrackersLocked()
}
func releaseMilestoneKey(c myWorkConfig) string { return fmt.Sprintf("%s#%d", c.Repo, c.Milestone) }
func validateReleaseTracker(t *releaseTracker) error {
	t.Name = strings.TrimSpace(t.Name)
	if t.Month != "" {
		if _, err := time.Parse("2006-01", t.Month); err != nil {
			return fmt.Errorf("choose a valid release month")
		}
	}
	if !cachelab.IDPattern.MatchString(t.ID) || !packageText(t.Name, 120, true) || len(t.Notes) > 10000 || len(t.Milestones) > 30 || len(t.Checkpoints) > 100 || len(t.Holidays) > 366 {
		return fmt.Errorf("invalid tracker or tracker limit exceeded")
	}
	date := func(v string) bool {
		d, e := time.Parse("2006-01-02", v)
		return e == nil && d.Year() >= 2000 && d.Year() <= 2200
	}
	seen := map[string]bool{}
	for i := range t.Milestones {
		m := &t.Milestones[i]
		c, e := normalizeMyWorkConfig(m.Config)
		if e != nil {
			return e
		}
		m.Config = c
		k := releaseMilestoneKey(c)
		if seen[k] {
			return fmt.Errorf("milestone already linked")
		}
		seen[k] = true
	}
	ids := map[string]bool{}
	for _, c := range t.Checkpoints {
		if c.ID == "" || len(c.ID) > 100 || ids[c.ID] || !packageText(c.Name, 120, true) || (!(c.Disabled && c.Date == "") && !date(c.Date)) || len(c.Kind) > 100 || len(c.Source) > 2000 || (c.Milestone != "" && !seen[c.Milestone]) {
			return fmt.Errorf("check checkpoint names, dates, and milestone assignments")
		}
		ids[c.ID] = true
	}
	for _, h := range t.Holidays {
		if !date(h) {
			return fmt.Errorf("holidays must use YYYY-MM-DD")
		}
	}
	return nil
}
func (s *testPackageService) saveReleaseTracker(revision string, tracker releaseTracker, remove bool) (releaseTrackerLibrary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	lib, err := s.releaseTrackersLocked()
	if err != nil {
		return lib, err
	}
	if revision != lib.Revision {
		return lib, fmt.Errorf("trackers changed in another view; reload before saving")
	}
	index := -1
	for i, t := range lib.Trackers {
		if t.ID == tracker.ID {
			index = i
			break
		}
	}
	if remove {
		if index < 0 {
			return lib, fmt.Errorf("tracker no longer exists")
		}
		lib.Trackers = append(lib.Trackers[:index], lib.Trackers[index+1:]...)
	} else {
		if err = validateReleaseTracker(&tracker); err != nil {
			return lib, err
		}
		if index < 0 {
			if len(lib.Trackers) >= 100 {
				return lib, fmt.Errorf("tracker limit reached (100)")
			}
			lib.Trackers = append(lib.Trackers, tracker)
		} else {
			lib.Trackers[index] = tracker
		}
	}
	lib.Revision = cachelab.ID()
	raw, err := json.MarshalIndent(lib, "", "  ")
	if err == nil && len(raw) > 64<<20 {
		return lib, fmt.Errorf("tracker storage exceeds 64 MB; reduce linked scopes")
	}
	if err == nil {
		err = writePrivateConfigAtomically(filepath.Join(s.root, ".release-trackers.json"), raw)
	}
	return lib, err
}
func (p *localControlPanel) handleReleaseTrackers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		if !p.authorizedLocalBrowserRead(r) {
			http.Error(w, "unauthorized", 401)
			return
		}
		s, e := p.testPackageService()
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		lib, e := s.releaseTrackers()
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		writeJSON(w, lib)
		return
	}
	var req struct {
		Action   string         `json:"action"`
		Revision string         `json:"revision"`
		Tracker  releaseTracker `json:"tracker"`
	}
	if !p.issueRadarRequest(w, r, &req, 2<<20) {
		return
	}
	if !packageEnum(req.Action, "save", "refresh", "delete", "attach-saved-work") {
		http.Error(w, "unknown tracker action", 400)
		return
	}
	s, err := p.testPackageService()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	lib, err := s.releaseTrackers()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if lib.Revision != req.Revision {
		http.Error(w, "Trackers changed; reload before saving.", 409)
		return
	}
	var old *releaseTracker
	for i := range lib.Trackers {
		if lib.Trackers[i].ID == req.Tracker.ID {
			old = &lib.Trackers[i]
		}
	}
	if req.Action == "attach-saved-work" {
		if old == nil {
			http.Error(w, "tracker not found", 404)
			return
		}
		saved, e := s.myWork()
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		if saved.Config.Milestone == 0 {
			http.Error(w, "No saved My Work milestone to attach", 400)
			return
		}
		req.Tracker = *old
		link := releaseMilestone{Config: saved.Config, Snapshot: &saved}
		found := false
		for i, m := range req.Tracker.Milestones {
			if releaseMilestoneKey(m.Config) == releaseMilestoneKey(saved.Config) {
				req.Tracker.Milestones[i] = link
				found = true
				break
			}
		}
		if !found {
			req.Tracker.Milestones = append(req.Tracker.Milestones, link)
		}
		out, e := s.saveReleaseTracker(req.Revision, req.Tracker, false)
		if e != nil {
			http.Error(w, e.Error(), 409)
			return
		}
		writeJSON(w, out)
		return
	}
	if req.Action == "refresh" {
		if old == nil {
			http.Error(w, "tracker not found", 404)
			return
		}
		req.Tracker = *old
	}
	if req.Tracker.ID == "" {
		req.Tracker.ID = cachelab.ID()
	}
	if req.Action != "delete" {
		if err = validateReleaseTracker(&req.Tracker); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
		defer cancel()
		for i := range req.Tracker.Milestones {
			m := &req.Tracker.Milestones[i]
			m.Snapshot = nil
			if old != nil {
				for _, previous := range old.Milestones {
					if previous.Config == m.Config {
						m.Snapshot = previous.Snapshot
						break
					}
				}
			}
			if m.Snapshot == nil && req.Action == "save" {
				saved, e := s.myWork()
				if e != nil {
					http.Error(w, e.Error(), 500)
					return
				}
				if saved.Config == m.Config && saved.Config.Milestone > 0 {
					m.Snapshot = &saved
				}
			}
			if m.Snapshot != nil && req.Action != "refresh" {
				continue
			}
			fresh, e := p.issueRadarBackend().pullMyWork(ctx, m.Config)
			if e != nil {
				issueRadarError(w, e)
				return
			}
			previous := m.Snapshot
			if previous == nil {
				fresh, e = s.prepareTrackedWork(fresh, false)
				if e != nil {
					http.Error(w, e.Error(), 500)
					return
				}
			} else {
				fresh.BucketID = previous.BucketID
			}
			if previous != nil {
				fresh = mergeMyWorkSnapshot(*previous, fresh)
			}
			m.Config = fresh.Config
			m.Snapshot = &fresh
		}
	}
	out, err := s.saveReleaseTracker(req.Revision, req.Tracker, req.Action == "delete")
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	writeJSON(w, out)
}
