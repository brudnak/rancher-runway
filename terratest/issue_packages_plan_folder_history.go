package test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type issuePackagePlanHistory struct {
	ID      string    `json:"id"`
	SavedAt time.Time `json:"savedAt"`
	Title   string    `json:"title"`
	Path    string    `json:"path"`
}

var issuePackagePlanHistoryName = regexp.MustCompile(`^[a-f0-9]{64}\.md$`)

func (s *issuePackageService) planHistoryDirectory(id string) (string, error) {
	directory := filepath.Join(s.root, id, ".plan-history")
	for _, path := range []string{filepath.Join(s.root, id), directory} {
		if info, err := os.Lstat(path); err == nil && !info.IsDir() {
			return "", fmt.Errorf("plan history contains a link or non-directory")
		} else if err != nil && !os.IsNotExist(err) {
			return "", err
		}
	}
	return directory, nil
}

func (s *issuePackageService) planFolderHistoryLocked(id string) ([]issuePackagePlanHistory, error) {
	directory, err := s.planHistoryDirectory(id)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return []issuePackagePlanHistory{}, nil
	}
	if err != nil {
		return nil, err
	}
	history := []issuePackagePlanHistory{}
	for _, entry := range entries {
		if !issuePackagePlanHistoryName.MatchString(entry.Name()) {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		raw, err := issuePackageReadPlanFile(path)
		if err != nil || issuePackagePlanDigest(raw)+".md" != entry.Name() {
			continue // A modified snapshot is preserved but is not trusted history.
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		title := "Saved test plan"
		if plan, err := parseIssuePackagePlanMarkdown(raw); err == nil {
			title = plan.Title
		}
		history = append(history, issuePackagePlanHistory{ID: strings.TrimSuffix(entry.Name(), ".md"), SavedAt: info.ModTime().UTC(), Title: title, Path: path})
	}
	sort.Slice(history, func(i, j int) bool { return history[i].SavedAt.After(history[j].SavedAt) })
	return history, nil
}

func (s *issuePackageService) savePlanHistoryLocked(pkg issuePackage, markdown []byte) error {
	directory, err := s.planHistoryDirectory(pkg.ID)
	if err != nil {
		return err
	}
	path := filepath.Join(directory, issuePackagePlanDigest(markdown)+".md")
	if existing, err := issuePackageReadPlanFile(path); err == nil {
		if string(existing) != string(markdown) {
			return fmt.Errorf("a saved plan history file changed; existing history was preserved")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := writePrivateConfigAtomically(path, markdown); err != nil {
		return err
	}
	history, err := s.planFolderHistoryLocked(pkg.ID)
	if err != nil {
		return err
	}
	for _, entry := range history[min(50, len(history)):] {
		if raw, err := issuePackageReadPlanFile(entry.Path); err == nil && issuePackagePlanDigest(raw) == entry.ID {
			_ = os.Remove(entry.Path)
		}
	}
	return nil
}
