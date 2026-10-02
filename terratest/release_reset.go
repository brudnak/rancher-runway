package test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/brudnak/ha-rancher-rke2/internal/cachelab"
)

// Stage removals until the empty library is written; clearing never touches GitHub.
func (s *issuePackageService) clearReleaseWork(revision string, clearPackages bool) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	trackers, err := s.releaseTrackersLocked()
	if err != nil {
		return "", err
	}
	if trackers.Revision != revision {
		return "", errIssuePackageConflict
	}
	if len(s.pendingAutomation) > 0 {
		return "", fmt.Errorf("finish pending package automation before clearing work")
	}
	if clearPackages {
		for _, pkg := range s.packages {
			for _, session := range pkg.Sessions {
				if session.Status == "active" {
					return "", fmt.Errorf("finish active test sessions before clearing packages")
				}
			}
		}
	}
	backup := filepath.Join(s.root, ".reset-backup-"+cachelab.ID())
	if err = os.Mkdir(backup, 0700); err != nil {
		return "", err
	}
	names := []string{".my-work.json", ".release-trackers.json", ".library.json", ".daily-readiness.json"}
	if clearPackages {
		entries, e := os.ReadDir(s.root)
		if e != nil {
			return "", e
		}
		names = nil
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name(), ".reset-backup-") {
				names = append(names, entry.Name())
			}
		}
	}
	moved := []string{}
	rollback := func() {
		for _, name := range moved {
			_ = os.Rename(filepath.Join(backup, name), filepath.Join(s.root, name))
		}
	}
	for _, name := range names {
		err = os.Rename(filepath.Join(s.root, name), filepath.Join(backup, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			rollback()
			return "", err
		}
		moved = append(moved, name)
	}
	library := issuePackageLibrary{Version: 1, Revision: cachelab.ID(), Buckets: []issuePackageBucket{}, Unfiled: []string{}}
	if !clearPackages {
		for id := range s.packages {
			library.Unfiled = append(library.Unfiled, id)
		}
		sort.Strings(library.Unfiled)
	}
	raw, _ := json.Marshal(library)
	if err = writePrivateConfigAtomically(filepath.Join(s.root, ".library.json"), raw); err != nil {
		rollback()
		return "", err
	}
	s.library = library
	if clearPackages {
		s.packages = map[string]issuePackage{}
	}
	if err = os.RemoveAll(backup); err != nil {
		return "", fmt.Errorf("work cleared but cleanup failed: %w", err)
	}
	return "", nil
}
