package test

import "testing"

func TestCacheLabExactCleanupConfirmation(t *testing.T) {
	for _, action := range []string{"delete-folder", "delete-snapshot", "clear-workspace", "delete-workspace"} {
		t.Run(action, func(t *testing.T) {
			s, w := cacheTestService(t)
			if _, err := s.mutate(cacheLabRequest{Action: "folder", Workspace: w.ID, Name: "Investigation"}); err != nil {
				t.Fatal(err)
			}
			snapshot := cacheTestSnapshot(t, s, w, cacheTestDB(t, "CREATE TABLE records(id INTEGER)"), "Before")
			req := cacheLabRequest{Action: action, Workspace: w.ID, Snapshot: snapshot.ID, Folder: "Investigation"}
			for _, phrase := range []string{"", "CONFIRM", "Confirm", " confirm", "confirm ", "remove folder", "delete snapshot", "delete snapshots", "delete workspace"} {
				req.Confirm = phrase
				if _, err := s.mutate(req); err == nil {
					t.Fatalf("accepted %q", phrase)
				}
			}
			if len(s.library.Snapshots) != 1 || len(s.library.Workspaces) != 1 || len(s.library.Workspaces[0].Folders) != 1 {
				t.Fatal("changed local data without exact confirmation")
			}
			req.Confirm = "confirm"
			if _, err := s.mutate(req); err != nil {
				t.Fatal(err)
			}
			if action == "delete-folder" {
				if len(s.library.Workspaces[0].Folders) != 0 || len(s.library.Snapshots) != 1 {
					t.Fatal("folder cleanup should keep snapshots")
				}
			} else if len(s.library.Snapshots) != 0 {
				t.Fatal("did not remove selected snapshots")
			}
		})
	}
}
