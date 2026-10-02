package cachelab

import (
	"github.com/brudnak/ha-rancher-rke2/internal/operations"
	"slices"
)

type Options struct {
	Workers        *operations.Workers
	ResolveCluster func(string, string, string, string) (string, error)
	Runner         CommandRunner
}
type SteveSource struct{ RunID, SteveRef, SourceDir string }

// Root is the service-owned data directory supplied during construction.
func (s *Service) Root() string             { return s.root }
func (s *Service) HasClusterResolver() bool { return s.resolveCluster != nil }

// Library returns a detached snapshot; callers cannot mutate service state.
func (s *Service) Library() Library {
	s.mu.Lock()
	defer s.mu.Unlock()
	lib := s.library
	lib.Workspaces = slices.Clone(lib.Workspaces)
	lib.Snapshots = slices.Clone(lib.Snapshots)
	for i := range lib.Workspaces {
		lib.Workspaces[i].Folders = slices.Clone(lib.Workspaces[i].Folders)
		lib.Workspaces[i].Queries = slices.Clone(lib.Workspaces[i].Queries)
	}
	return lib
}
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}

// ReadEvidence keeps the selected record and optional database read under one lock.
// The callback must treat the library as read-only and must not call back into
// this service. The supplied lookup is valid only for the duration of the callback.
func (s *Service) ReadEvidence(fn func(Library, func(string) (Snapshot, string, error)) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn(s.library, s.snapshotLocked)
}
