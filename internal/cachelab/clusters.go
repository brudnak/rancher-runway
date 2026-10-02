package cachelab

import (
	"encoding/json"
	"fmt"
	"time"
)

func (s *Service) resolveClusterID(id string, profile Workspace) (string, error) {
	if s.resolveCluster == nil {
		if id != "" {
			return "", fmt.Errorf("cluster workspace lookup is unavailable")
		}
		return "", nil
	}
	host, path, context := profile.URL, profile.Kubeconfig, profile.Context
	// A kubeconfig URL is the Kubernetes API server, not necessarily Rancher's
	// browser hostname. Its pinned file/context is the reliable identity.
	if profile.Kind == "kubeconfig" {
		host = ""
	}
	if profile.Kind == "import" || profile.Kind == "steve" {
		host, path, context = "", "", ""
	}
	return s.resolveCluster(id, host, path, context)
}
func (s *Service) workspaceHasSnapshotsLocked(id string) bool {
	for _, snapshot := range s.library.Snapshots {
		if snapshot.Workspace == id {
			return true
		}
	}
	return false
}
func (s *Service) BackfillClusters(match func(Workspace) string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	before, _ := json.Marshal(s.library)
	changed := false
	for i := range s.library.Workspaces {
		w := &s.library.Workspaces[i]
		if w.ClusterID != "" || w.Kind == "import" || s.library.Job.Running && s.library.Job.Workspace == w.ID {
			continue
		}
		if id := match(*w); id != "" {
			w.ClusterID = id
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

func (s *Service) PrepareSteveWorkspace(record SteveSource, clusterID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.library.Job.Running {
		return "", fmt.Errorf("wait for the current capture before snapshotting Steve")
	}
	before, _ := json.Marshal(s.library)
	workspace := ""
	changed := false
	for i := range s.library.Workspaces {
		w := &s.library.Workspaces[i]
		if w.Kind == "steve" && w.URL == record.RunID {
			workspace = w.ID
			if w.ClusterID == "" {
				w.ClusterID = clusterID
				changed = true
			}
			break
		}
	}
	if workspace == "" {
		if len(s.library.Workspaces) >= 100 {
			return "", fmt.Errorf("workspace limit reached")
		}
		workspace = ID()
		s.library.Workspaces = append(s.library.Workspaces, Workspace{ID: workspace, ClusterID: clusterID, Kind: "steve", URL: record.RunID, Name: "Steve · " + record.SteveRef, Connected: true, CreatedAt: time.Now().UTC(), Folders: []string{}, Queries: []SavedQuery{}})
		changed = true
	}
	if changed {
		if err := s.persistLocked(); err != nil {
			_ = json.Unmarshal(before, &s.library)
			return "", err
		}
	}
	return workspace, nil
}
