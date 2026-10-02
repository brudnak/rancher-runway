package cachelab

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (s *Service) Mutate(req Request) (result any, returnedErr error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if req.Action == "cancel" {
		if s.cancel != nil {
			s.cancel()
		}
		return map[string]bool{"ok": true}, nil
	}
	w, err := s.workspaceLocked(req.Workspace)
	if err != nil {
		return nil, err
	}
	manifestBefore, _ := json.Marshal(s.library)
	defer func() {
		if returnedErr != nil {
			_ = json.Unmarshal(manifestBefore, &s.library)
		}
	}()
	switch req.Action {
	case "assign-cluster":
		if strings.TrimSpace(req.ClusterID) == "" {
			return nil, fmt.Errorf("choose a cluster workspace")
		}
		if s.library.Job.Running && s.library.Job.Workspace == w.ID {
			return nil, fmt.Errorf("wait for capture before changing this association")
		}
		id, err := s.resolveClusterID(req.ClusterID, *w)
		if err != nil {
			return nil, err
		}
		if w.ClusterID != "" && id != w.ClusterID && s.workspaceHasSnapshotsLocked(w.ID) {
			return nil, fmt.Errorf("this workspace has snapshots; create another workspace for a different cluster")
		}
		w.ClusterID = id
	case "select-pod":
		if s.library.Job.Running && s.library.Job.Workspace == w.ID {
			return nil, fmt.Errorf("capture is in progress")
		}
		w.Pod = Text(req.Name, 253)
	case "view":
		w.View = req.View
		w.View.SQL = Text(w.View.SQL, 64000)
		s.library.Active = w.ID
	case "rename-workspace":
		if strings.TrimSpace(req.Name) == "" {
			return nil, fmt.Errorf("enter a workspace name")
		}
		w.Name = Text(req.Name, 120)
		w.Notes = Text(req.Notes, 8000)
	case "folder":
		name := Text(req.Name, 80)
		if name == "" || name == "__unfiled__" {
			return nil, fmt.Errorf("enter another folder name")
		}
		if len(w.Folders) >= 100 {
			return nil, fmt.Errorf("folder limit reached")
		}
		found := false
		for _, f := range w.Folders {
			found = found || f == name
		}
		if !found {
			w.Folders = append(w.Folders, name)
		}
	case "rename-folder":
		name := Text(req.Name, 80)
		if name == "" || name == "__unfiled__" {
			return nil, fmt.Errorf("enter another folder name")
		}
		exists := false
		for _, f := range w.Folders {
			if f == name {
				return nil, fmt.Errorf("a folder with this name already exists")
			}
			exists = exists || f == req.Folder
		}
		if !exists {
			return nil, fmt.Errorf("folder not found")
		}
		for i := range w.Folders {
			if w.Folders[i] == req.Folder {
				w.Folders[i] = name
			}
		}
		for i := range s.library.Snapshots {
			v := &s.library.Snapshots[i]
			if v.Workspace == w.ID && v.Folder == req.Folder {
				v.Folder = name
			}
		}
		if w.View.Folder == req.Folder {
			w.View.Folder = name
		}
	case "delete-folder":
		if req.Confirm != typedConfirmationPhrase {
			return nil, fmt.Errorf("type confirm to remove this folder")
		}
		for i := range s.library.Snapshots {
			v := &s.library.Snapshots[i]
			if v.Workspace == w.ID && v.Folder == req.Folder {
				v.Folder = ""
			}
		}
		folders := []string{}
		for _, f := range w.Folders {
			if f != req.Folder {
				folders = append(folders, f)
			}
		}
		w.Folders = folders
		if w.View.Folder == req.Folder {
			w.View.Folder = ""
		}
	case "save-query":
		name := Text(req.Name, 100)
		if name == "" {
			return nil, fmt.Errorf("enter a query name")
		}
		if _, err := cacheLabSelect(req.SQL); err != nil {
			return nil, err
		}
		found := false
		for i := range w.Queries {
			if w.Queries[i].Name == name {
				w.Queries[i].SQL = req.SQL
				found = true
			}
		}
		if !found {
			if len(w.Queries) >= 100 {
				return nil, fmt.Errorf("saved query limit reached")
			}
			w.Queries = append(w.Queries, SavedQuery{name, req.SQL})
		}
	case "delete-query":
		queries := []SavedQuery{}
		for _, q := range w.Queries {
			if q.Name != req.Name {
				queries = append(queries, q)
			}
		}
		w.Queries = queries
	case "snapshot", "rename-snapshot":
		if req.Action == "rename-snapshot" && strings.TrimSpace(req.Name) == "" {
			return nil, fmt.Errorf("enter a snapshot name")
		}
		found := false
		for i := range s.library.Snapshots {
			v := &s.library.Snapshots[i]
			if v.ID == req.Snapshot && v.Workspace == w.ID {
				if req.Name != "" {
					v.Name = Text(req.Name, 140)
				}
				if req.Action != "rename-snapshot" {
					v.Notes = Text(req.Notes, 8000)
					v.Folder = Text(req.Folder, 80)
					v.Favorite = req.Favorite
				}
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("snapshot not found")
		}
	case "delete-snapshot", "delete-workspace", "clear-workspace":
		if req.ClusterID != "" && w.ClusterID != req.ClusterID {
			return nil, fmt.Errorf("cache workspace cluster association changed; review cleanup again")
		}
		if s.library.Job.Running && s.library.Job.Workspace == w.ID {
			return nil, fmt.Errorf("wait for the capture to finish or cancel it before cleanup")
		}
		if req.Confirm != typedConfirmationPhrase {
			return nil, fmt.Errorf("type confirm to clean up the selected local data")
		}
		if req.Action == "delete-workspace" && w.RememberToken {
			if _, err := cacheLabKeychain("delete", w.ID, ""); err != nil {
				return nil, err
			}
		}
		previous, _ := json.Marshal(s.library)
		staged := map[string]string{}
		rollback := func() {
			for path, temporary := range staged {
				_ = os.Rename(temporary, path)
			}
			_ = json.Unmarshal(previous, &s.library)
		}
		keep := []Snapshot{}
		removed := map[string]bool{}
		for _, v := range s.library.Snapshots {
			if v.Workspace == w.ID && (req.Action != "delete-snapshot" || v.ID == req.Snapshot) {
				path := filepath.Join(s.root, v.Workspace, v.ID+".db")
				if err := os.Rename(path, path+".delete"); err != nil && !os.IsNotExist(err) {
					rollback()
					return nil, err
				} else if err == nil {
					staged[path] = path + ".delete"
				}
				removed[v.ID] = true
			} else {
				keep = append(keep, v)
			}
		}
		s.library.Snapshots = keep
		if removed[w.View.Snapshot] {
			w.View.Snapshot = ""
		}
		if removed[w.View.Baseline] {
			w.View.Baseline = ""
		}
		if removed[w.View.Comparison] {
			w.View.Comparison = ""
		}
		if removed[s.library.Job.Snapshot] {
			s.library.Job.Snapshot = ""
		}
		if req.Action == "delete-workspace" {
			workspaces := []Workspace{}
			for _, v := range s.library.Workspaces {
				if v.ID != w.ID {
					workspaces = append(workspaces, v)
				}
			}
			s.library.Workspaces = workspaces
			if s.library.Active == req.Workspace {
				s.library.Active = ""
			}
		}
		if err := s.persistLocked(); err != nil {
			rollback()
			return nil, err
		}
		manifestBefore, _ = json.Marshal(s.library)
		for _, path := range staged {
			if err := os.Remove(path); err != nil {
				return nil, fmt.Errorf("library updated but a staged file could not be removed: %w", err)
			}
		}
		if req.Action == "delete-workspace" {
			delete(s.tokens, req.Workspace)
			_ = os.Remove(filepath.Join(s.root, req.Workspace))
		}
		return map[string]bool{"ok": true}, nil
	default:
		return nil, fmt.Errorf("unknown Cache Lab action")
	}
	return map[string]bool{"ok": true}, s.persistLocked()
}
