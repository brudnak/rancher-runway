package test

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Cluster linkage is metadata only. Test configuration remains the authority
// for the target, and the resolver checks explicit links against that target.
func (s *testLabService) resolveClusterID(id, host string) (string, error) {
	if s.resolveCluster == nil {
		if id != "" {
			return "", fmt.Errorf("cluster workspace lookup is unavailable")
		}
		return "", nil
	}
	return s.resolveCluster(id, host, "", "")
}
func (s *testLabService) updateClusterRecordLocked(req testLabRequest) (any, error) {
	if req.Action == "assign-cluster" && strings.TrimSpace(req.ClusterID) == "" {
		return nil, fmt.Errorf("choose a cluster workspace")
	}
	scope := req.Scope
	if req.Action == "rename-run" {
		scope = "run"
	}
	if req.Action == "rename-plan" {
		scope = "plan"
	}
	if scope != "run" && scope != "plan" {
		return nil, fmt.Errorf("choose a run or saved plan")
	}
	name := strings.TrimSpace(req.Name)
	if req.Action != "assign-cluster" && (name == "" || len([]rune(name)) > 100 || strings.ContainsAny(name, "\x00\r\n")) {
		return nil, fmt.Errorf("use a name between 1 and 100 characters")
	}
	before, _ := json.Marshal(s.library)
	update := func(clusterID *string, recordName *string, host string) error {
		if req.Action == "assign-cluster" {
			id, err := s.resolveClusterID(req.ClusterID, host)
			if err != nil {
				return err
			}
			*clusterID = id
		} else {
			*recordName = name
		}
		if err := s.persistLocked(); err != nil {
			_ = json.Unmarshal(before, &s.library)
			return err
		}
		return nil
	}
	if scope == "run" {
		for i := range s.library.Runs {
			run := &s.library.Runs[i]
			if run.ID != req.ID {
				continue
			}
			if run.Status == "running" {
				return nil, fmt.Errorf("wait for this test run to finish before changing its saved record")
			}
			if err := update(&run.ClusterID, &run.Name, run.Host); err != nil {
				return nil, err
			}
			return *run, nil
		}
		return nil, fmt.Errorf("run not found")
	}
	for i := range s.library.Plans {
		plan := &s.library.Plans[i]
		if plan.ID != req.ID {
			continue
		}
		if err := update(&plan.ClusterID, &plan.Name, plan.Host); err != nil {
			return nil, err
		}
		return *plan, nil
	}
	return nil, fmt.Errorf("saved plan not found")
}

type clusterLabTestRun struct {
	SHA        string          `json:"sha"`
	Ref        string          `json:"ref"`
	Stage      string          `json:"stage"`
	Results    []testLabResult `json:"results"`
	ID         string          `json:"id"`
	ClusterID  string          `json:"clusterId"`
	Name       string          `json:"name"`
	Host       string          `json:"host"`
	Status     string          `json:"status"`
	StartedAt  time.Time       `json:"startedAt"`
	FinishedAt time.Time       `json:"finishedAt,omitempty"`
}
type clusterLabTestPlan struct {
	SHA       string    `json:"sha"`
	Ref       string    `json:"ref"`
	Selection []string  `json:"selection"`
	ID        string    `json:"id"`
	ClusterID string    `json:"clusterId"`
	Name      string    `json:"name"`
	Host      string    `json:"host"`
	SavedAt   time.Time `json:"savedAt"`
}
type clusterLabCacheWorkspace struct {
	ID            string `json:"id"`
	ClusterID     string `json:"clusterId"`
	Name          string `json:"name"`
	URL           string `json:"url"`
	Kind          string `json:"kind"`
	SnapshotCount int    `json:"snapshotCount"`
	Bytes         int64  `json:"bytes"`
	Active        bool   `json:"active"`
}
type clusterLabSnapshot struct {
	Tables    int       `json:"tables"`
	Folder    string    `json:"folder"`
	Notes     string    `json:"notes"`
	Favorite  bool      `json:"favorite"`
	Source    string    `json:"source"`
	Pod       string    `json:"pod,omitempty"`
	Image     string    `json:"image,omitempty"`
	Method    string    `json:"method"`
	ID        string    `json:"id"`
	Workspace string    `json:"workspace"`
	ClusterID string    `json:"clusterId"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
	Bytes     int64     `json:"bytes"`
}
type clusterLabData struct {
	TestRuns        []clusterLabTestRun        `json:"testRuns"`
	TestPlans       []clusterLabTestPlan       `json:"testPlans"`
	CacheWorkspaces []clusterLabCacheWorkspace `json:"cacheWorkspaces"`
	Snapshots       []clusterLabSnapshot       `json:"snapshots"`
}

// Matching legacy records must never create descriptors or guess among two
// clusters. Explicit existing associations always win over later discovery.
func uniqueLabCluster(candidates []clusterWorkspaceRecord, host, path, context string) string {
	hostKey, hostErr := clusterWorkspaceHostKey(host)
	pathKey := ""
	if path != "" {
		pathKey = clusterWorkspaceKubeconfigKey(path, context)
	}
	matched := map[string]bool{}
	for _, candidate := range candidates {
		match := false
		if pathKey != "" && candidate.Kubeconfig != "" {
			match = pathKey == clusterWorkspaceKubeconfigKey(candidate.Kubeconfig, candidate.Context)
		}
		if hostErr == nil && hostKey != "" && candidate.URL != "" {
			key, err := clusterWorkspaceHostKey(candidate.URL)
			match = match || err == nil && key == hostKey
		}
		if match {
			matched[candidate.ID] = true
		}
	}
	if len(matched) != 1 {
		return ""
	}
	for id := range matched {
		return id
	}
	return ""
}
func (s *testLabService) backfillClusters(candidates []clusterWorkspaceRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	before, _ := json.Marshal(s.library)
	changed := false
	for i := range s.library.Runs {
		record := &s.library.Runs[i]
		if record.ClusterID != "" || record.Status == "running" {
			continue
		}
		if id := uniqueLabCluster(candidates, record.Host, "", ""); id != "" {
			record.ClusterID = id
			changed = true
		}
	}
	for i := range s.library.Plans {
		record := &s.library.Plans[i]
		if record.ClusterID != "" {
			continue
		}
		if id := uniqueLabCluster(candidates, record.Host, "", ""); id != "" {
			record.ClusterID = id
			changed = true
		}
	}
	if !changed {
		return nil
	}
	if err := s.persistLocked(); err != nil {
		_ = json.Unmarshal(before, &s.library)
		return err
	}
	return nil
}
func (p *localControlPanel) labClusterRecords() (map[string]clusterLabData, error) {
	candidates, err := p.labClusterCandidates()
	if err != nil {
		return nil, err
	}
	tests, err := p.testLabService()
	if err != nil {
		return nil, err
	}
	cache, err := p.cacheLabService()
	if err != nil {
		return nil, err
	}
	if err = tests.backfillClusters(candidates); err != nil {
		return nil, err
	}
	if err = cache.backfillClusters(candidates); err != nil {
		return nil, err
	}
	records := map[string]clusterLabData{}
	tests.mu.Lock()
	for _, r := range tests.library.Runs {
		if r.ClusterID == "" {
			continue
		}
		data := records[r.ClusterID]
		data.TestRuns = append(data.TestRuns, clusterLabTestRun{ID: r.ID, ClusterID: r.ClusterID, Name: r.Name, Host: r.Host, Status: r.Status, StartedAt: r.StartedAt, FinishedAt: r.FinishedAt, SHA: r.SHA, Ref: r.Ref, Stage: r.Stage, Results: append([]testLabResult{}, r.Results...)})
		records[r.ClusterID] = data
	}
	for _, r := range tests.library.Plans {
		if r.ClusterID == "" {
			continue
		}
		data := records[r.ClusterID]
		data.TestPlans = append(data.TestPlans, clusterLabTestPlan{ID: r.ID, ClusterID: r.ClusterID, Name: r.Name, Host: r.Host, SavedAt: r.SavedAt, SHA: r.SHA, Ref: r.Ref, Selection: append([]string{}, r.Selection...)})
		records[r.ClusterID] = data
	}
	tests.mu.Unlock()
	cache.mu.Lock()
	for _, w := range cache.library.Workspaces {
		if w.ClusterID == "" {
			continue
		}
		data := records[w.ClusterID]
		record := clusterLabCacheWorkspace{ID: w.ID, ClusterID: w.ClusterID, Name: w.Name, URL: w.URL, Kind: w.Kind, Active: cache.library.Job.Running && cache.library.Job.Workspace == w.ID}
		for _, snapshot := range cache.library.Snapshots {
			if snapshot.Workspace != w.ID {
				continue
			}
			record.SnapshotCount++
			record.Bytes += snapshot.Bytes
			data.Snapshots = append(data.Snapshots, clusterLabSnapshot{ID: snapshot.ID, Workspace: w.ID, ClusterID: w.ClusterID, Name: snapshot.Name, CreatedAt: snapshot.CreatedAt, Bytes: snapshot.Bytes, Folder: snapshot.Folder, Notes: snapshot.Notes, Favorite: snapshot.Favorite, Source: snapshot.Source, Pod: snapshot.Pod, Image: snapshot.Image, Method: snapshot.Method, Tables: snapshot.Tables})
		}
		data.CacheWorkspaces = append(data.CacheWorkspaces, record)
		records[w.ClusterID] = data
	}
	cache.mu.Unlock()
	return records, nil
}

// Called only for an explicitly reviewed successful infrastructure cleanup.
// Existing deletion paths enforce ownership, active-job guards, and persistence.
// Plans and cattle-config templates are deliberately retained.
func (p *localControlPanel) cleanupLabClusters(ids []string, cleanupTests, cleanupCache bool) []string {
	selected := map[string]bool{}
	for _, id := range ids {
		if id != "" {
			selected[id] = true
		}
	}
	warnings := []string{}
	if len(selected) == 0 {
		return warnings
	}
	if cleanupTests {
		s, err := p.testLabService()
		if err != nil {
			warnings = append(warnings, "Test Lab cleanup unavailable: "+err.Error())
		} else {
			s.mu.Lock()
			runs := append([]testLabRun(nil), s.library.Runs...)
			s.mu.Unlock()
			for _, run := range runs {
				if !selected[run.ClusterID] {
					continue
				}
				if _, err := s.mutate(testLabRequest{Action: "delete-run", ID: run.ID, ClusterID: run.ClusterID, Confirm: typedConfirmationPhrase}); err != nil {
					warnings = append(warnings, fmt.Sprintf("Test result %s retained: %v", run.Name, err))
				}
			}
		}
	}
	if cleanupCache {
		s, err := p.cacheLabService()
		if err != nil {
			warnings = append(warnings, "Cache Lab cleanup unavailable: "+err.Error())
		} else {
			s.mu.Lock()
			workspaces := append([]cacheLabWorkspace(nil), s.library.Workspaces...)
			s.mu.Unlock()
			for _, workspace := range workspaces {
				if !selected[workspace.ClusterID] {
					continue
				}
				if _, err := s.mutate(cacheLabRequest{Action: "delete-workspace", Workspace: workspace.ID, ClusterID: workspace.ClusterID, Confirm: typedConfirmationPhrase}); err != nil {
					warnings = append(warnings, fmt.Sprintf("Cache workspace %s retained: %v", workspace.Name, err))
				}
			}
		}
	}
	return warnings
}
