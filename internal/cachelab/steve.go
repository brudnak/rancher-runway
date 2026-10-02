package cachelab

import (
	"os"
	"path/filepath"
)

func (s *Service) CaptureSteve(record SteveSource, clusterID string) (any, error) {
	workspace, err := s.PrepareSteveWorkspace(record, clusterID)
	if err != nil {
		return nil, err
	}
	ctx, id, err := s.BeginJob(workspace, "Snapshotting Steve cache")
	if err != nil {
		return nil, err
	}
	go func() {
		path := filepath.Join(s.Root(), workspace, id+".partial")
		defer os.Remove(path)
		var saved Snapshot
		err := Vacuum(ctx, filepath.Join(record.SourceDir, "informer_object_cache.db"), path)
		if err == nil {
			saved, err = s.AddSnapshot(ctx, workspace, path, Snapshot{Source: record.RunID, Image: record.SteveRef, Method: "VACUUM INTO · local Steve"})
		}
		s.FinishJob(saved, err)
	}()
	return map[string]string{"workspace": workspace, "job": id}, nil
}
