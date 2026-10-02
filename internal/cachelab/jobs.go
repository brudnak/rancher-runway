package cachelab

import (
	"context"
	"errors"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/operations"
	"os"
	"path/filepath"
	"time"
)

func (s *Service) BeginJob(workspace, stage string) (context.Context, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.library.Job.Running {
		return nil, "", fmt.Errorf("a capture is already in progress")
	}
	if _, err := s.workspaceLocked(workspace); err != nil {
		return nil, "", err
	}
	if err := os.MkdirAll(filepath.Join(s.root, workspace), 0700); err != nil {
		return nil, "", err
	}
	if s.workers == nil {
		s.workers = &operations.Workers{}
	}
	parent, done, err := s.workers.Begin()
	if err != nil {
		return nil, "", err
	}
	ctx, cancel := context.WithTimeout(parent, 8*time.Minute)
	s.jobDone = done
	s.cancel = cancel
	s.library.Job = Job{ID: ID(), Workspace: workspace, Running: true, Stage: stage, StartedAt: time.Now().UTC()}
	if err := s.persistLocked(); err != nil {
		cancel()
		done()
		s.jobDone = nil
		s.cancel = nil
		s.library.Job.Running = false
		return nil, "", err
	}
	return ctx, s.library.Job.ID, nil
}

func (s *Service) Stage(stage string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.library.Job.Stage = stage
}

func (s *Service) FinishJob(record Snapshot, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.jobDone != nil {
		defer s.jobDone()
		s.jobDone = nil
	}
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.library.Job.Running = false
	s.library.Job.Stage = "Snapshot ready"
	s.library.Job.Snapshot = ""
	if err == nil {
		s.library.Job.Snapshot = record.ID
	}
	if err != nil {
		s.library.Job.Stage = "Capture stopped"
		s.library.Job.Error = err.Error()
	}
	if persistErr := s.persistLocked(); persistErr != nil {
		s.library.Job.Error = "Could not save capture status: " + persistErr.Error()
	}
}

// Kept here to make callers treat cancellation as a user action rather than a
// successful empty snapshot.
func cacheLabContextError(err error) error {
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("capture canceled; no incomplete snapshot was saved")
	}
	return err
}
