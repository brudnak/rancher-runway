package test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const issuePackagePlanDirectory = "test-plans"

type issuePackagePlanFolder struct {
	Path         string                    `json:"path"`
	RootPath     string                    `json:"rootPath"`
	RelativePath string                    `json:"relativePath"`
	Repository   string                    `json:"repository"`
	Milestone    string                    `json:"milestone"`
	IssueNumber  int                       `json:"issueNumber"`
	Files        []string                  `json:"files"`
	Warning      string                    `json:"warning,omitempty"`
	Contents     map[string][]byte         `json:"-"`
	History      []issuePackagePlanHistory `json:"history"`
}

type issuePackagePlanFolderRecord struct {
	Version      int               `json:"version"`
	RelativePath string            `json:"relativePath"`
	SHA256       map[string]string `json:"sha256"`
}

func issuePackagePlanSlug(value string) string {
	var out strings.Builder
	separator := false
	for _, r := range strings.ToLower(value) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '_' {
			if separator && out.Len() > 0 {
				out.WriteByte('-')
			}
			separator = false
			out.WriteRune(r)
		} else {
			separator = true
		}
		if out.Len() >= 70 {
			break
		}
	}
	name := strings.Trim(out.String(), ".-_")
	if name == "" {
		return "untitled"
	}
	return name
}

var issuePackageRepositorySegment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

func issuePackagePlanLocation(pkg issuePackage, lib issuePackageLibrary) issuePackagePlanFolder {
	folder := issuePackagePlanFolder{Repository: "Unlinked", Milestone: "Unfiled", Files: []string{"test-plan.md", "plan.json"}}
	repositoryParts := []string{"unlinked"}
	issuePart := "plan-" + pkg.ID
	if parsed, err := url.Parse(pkg.IssueURL); err == nil {
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(parts) == 4 && parts[2] == "issues" && issuePackageRepositorySegment.MatchString(parts[0]) && issuePackageRepositorySegment.MatchString(parts[1]) {
			if number, err := strconv.Atoi(parts[3]); err == nil && number > 0 {
				folder.IssueNumber = number
				folder.Repository = strings.ToLower(parsed.Host) + "/" + parts[0] + "/" + parts[1]
				repositoryParts = []string{issuePackagePlanSlug(parsed.Host), strings.ToLower(parts[0]), strings.ToLower(parts[1])}
				issuePart = fmt.Sprintf("issue-%d--%s", number, pkg.ID)
			}
		}
	}
	milestonePart := "unfiled"
	for _, bucket := range lib.Buckets {
		for _, id := range bucket.PackageIDs {
			if id == pkg.ID {
				folder.Milestone = bucket.Name
				milestonePart = issuePackagePlanSlug(bucket.Name) + "--" + bucket.ID
				break
			}
		}
	}
	folder.RelativePath = strings.Join(append(repositoryParts, milestonePart, issuePart), "/")
	return folder
}

// Only authored plan content is portable. Session observations, private notes,
// environment endpoints, local automation selections and evidence stay private.
func portableIssuePackagePlan(pkg issuePackage) issuePackageDraftPlan {
	plan := issuePackageDraftPlan{Title: pkg.Title, IssueURL: pkg.IssueURL, IssueTitle: pkg.IssueTitle, FixURL: pkg.FixURL, FixTitle: pkg.FixTitle, Summary: pkg.Summary, Status: pkg.Status, Cases: []issuePackageCase{}}
	for _, c := range pkg.Cases {
		plan.Cases = append(plan.Cases, issuePackageCase{ID: c.ID, Title: c.Title, Preconditions: c.Preconditions, Expected: c.Expected, Automation: "manual", Steps: append([]issuePackageStep{}, c.Steps...), Selection: []string{}})
	}
	return plan
}

func renderIssuePackagePlanFiles(pkg issuePackage, lib issuePackageLibrary) (map[string][]byte, error) {
	return renderIssuePackagePlanFilesWithEnvironment(pkg, lib, issuePackageEnvironment{Source: "manual", RecordedAt: pkg.UpdatedAt})
}

func renderIssuePackagePlanFilesWithEnvironment(pkg issuePackage, lib issuePackageLibrary, environment issuePackageEnvironment) (map[string][]byte, error) {
	if err := validateIssuePackage(pkg); err != nil {
		return nil, err
	}
	plan := portableIssuePackagePlan(pkg)
	portable := pkg
	portable.Cases, portable.Notes, portable.Sessions = plan.Cases, "", nil
	markdown, err := issuePackageGitHubPlanMarkdown(portable, environment, nil)
	if err != nil {
		return nil, err
	}
	folder := issuePackagePlanLocation(pkg, lib)
	metadata := struct {
		Format    string                `json:"format"`
		Version   int                   `json:"version"`
		PackageID string                `json:"packageId"`
		Milestone string                `json:"milestone"`
		Plan      issuePackageDraftPlan `json:"plan"`
	}{"rancher-runway/test-plan", 1, pkg.ID, folder.Milestone, plan}
	raw, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return nil, err
	}
	return map[string][]byte{"test-plan.md": []byte(markdown), "plan.json": append(raw, '\n')}, nil
}

func issuePackagePlanDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// Refuse symlinks at every managed component. No request accepts a filesystem
// path; paths are derived from validated IDs and normalized display metadata.
func (s *issuePackageService) planFolderPath(relative string) (string, error) {
	if relative == "" || strings.Contains(relative, "\\") || filepath.IsAbs(relative) || filepath.ToSlash(filepath.Clean(relative)) != relative || relative == "." || strings.HasPrefix(relative, "../") {
		return "", fmt.Errorf("invalid test plan folder path")
	}
	root, err := filepath.Abs(s.root)
	if err != nil {
		return "", err
	}
	path := root
	for _, component := range append([]string{issuePackagePlanDirectory}, strings.Split(relative, "/")...) {
		if component == ".." || component == "" {
			return "", fmt.Errorf("invalid test plan folder component")
		}
		if info, err := os.Lstat(path); err == nil && !info.IsDir() {
			return "", fmt.Errorf("test plan folder contains a link or non-directory")
		} else if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		path = filepath.Join(path, component)
	}
	if info, err := os.Lstat(path); err == nil && !info.IsDir() {
		return "", fmt.Errorf("test plan folder contains a link or non-directory")
	} else if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return path, nil
}

func issuePackageReadPlanFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("test plan file is not a regular file")
	}
	return issuePackageReadBounded(path, issuePackageManifestLimit)
}

func (s *issuePackageService) planFolderRecord(pkgID string) (issuePackagePlanFolderRecord, error) {
	record := issuePackagePlanFolderRecord{}
	raw, err := issuePackageReadBounded(filepath.Join(s.root, pkgID, ".plan-folder.json"), 32<<10)
	if os.IsNotExist(err) {
		return record, nil
	}
	if err != nil {
		return record, err
	}
	if err = decodeIssuePackage(raw, &record); err != nil || record.Version != 1 || len(record.SHA256) != 2 {
		return record, fmt.Errorf("saved test plan folder metadata is invalid; existing files were preserved")
	}
	if !strings.HasSuffix(record.RelativePath, "--"+pkgID) && !strings.HasSuffix(record.RelativePath, "/plan-"+pkgID) {
		return record, fmt.Errorf("saved test plan folder belongs to another package; existing files were preserved")
	}
	if _, err := s.planFolderPath(record.RelativePath); err != nil {
		return record, err
	}
	for _, name := range []string{"test-plan.md", "plan.json"} {
		if digest, err := hex.DecodeString(record.SHA256[name]); err != nil || len(digest) != sha256.Size {
			return record, fmt.Errorf("saved test plan file checksum is invalid")
		}
	}
	return record, nil
}

func (s *issuePackageService) syncPlanFolderLocked(pkg issuePackage, lib issuePackageLibrary) (issuePackagePlanFolder, error) {
	folder := issuePackagePlanLocation(pkg, lib)
	folder.RootPath, _ = filepath.Abs(filepath.Join(s.root, issuePackagePlanDirectory))
	var err error
	folder.Path, err = s.planFolderPath(folder.RelativePath)
	if err != nil {
		return folder, err
	}
	environment, err := s.planFolderEnvironmentLocked(pkg)
	if err != nil {
		return folder, err
	}
	files, err := renderIssuePackagePlanFilesWithEnvironment(pkg, lib, environment)
	if err != nil {
		return folder, err
	}
	record, err := s.planFolderRecord(pkg.ID)
	if err != nil {
		return folder, err
	}
	oldPath := ""
	if record.RelativePath != "" {
		oldPath, err = s.planFolderPath(record.RelativePath)
		if err != nil {
			return folder, err
		}
		for name, checksum := range record.SHA256 {
			raw, readErr := issuePackageReadPlanFile(filepath.Join(oldPath, name))
			if readErr != nil && !os.IsNotExist(readErr) {
				return folder, readErr
			}
			if readErr == nil && issuePackagePlanDigest(raw) != checksum && !(oldPath == folder.Path && string(raw) == string(files[name])) {
				folder.Path, folder.RelativePath = oldPath, record.RelativePath
				return folder, fmt.Errorf("%s has local edits; automatic file updates paused to preserve them. Import or move the edited file before saving over it", name)
			}
		}
	}
	for name, desired := range files {
		raw, readErr := issuePackageReadPlanFile(filepath.Join(folder.Path, name))
		if readErr != nil && !os.IsNotExist(readErr) {
			return folder, readErr
		}
		if readErr == nil && string(raw) != string(desired) && (oldPath != folder.Path || issuePackagePlanDigest(raw) != record.SHA256[name]) {
			return folder, fmt.Errorf("%s already exists with unrecognized contents; existing files were preserved", name)
		}
	}
	if err := os.MkdirAll(folder.Path, 0700); err != nil {
		return folder, err
	}
	checksums := map[string]string{}
	for name, raw := range files {
		if err := writePrivateConfigAtomically(filepath.Join(folder.Path, name), raw); err != nil {
			return folder, err
		}
		checksums[name] = issuePackagePlanDigest(raw)
	}
	newRecord := issuePackagePlanFolderRecord{Version: 1, RelativePath: folder.RelativePath, SHA256: checksums}
	raw, _ := json.MarshalIndent(newRecord, "", "  ")
	if err := writePrivateConfigAtomically(filepath.Join(s.root, pkg.ID, ".plan-folder.json"), raw); err != nil {
		return folder, err
	}
	if err := s.savePlanHistoryLocked(pkg, files["test-plan.md"]); err != nil {
		return folder, err
	}
	if oldPath != "" && oldPath != folder.Path {
		for name, expected := range record.SHA256 {
			// Check again before cleanup, preserving edits made during the move.
			if raw, err := issuePackageReadPlanFile(filepath.Join(oldPath, name)); err == nil && issuePackagePlanDigest(raw) == expected {
				_ = os.Remove(filepath.Join(oldPath, name))
			}
		}
		for path := oldPath; path != folder.RootPath && path != filepath.Dir(path); path = filepath.Dir(path) {
			if os.Remove(path) != nil {
				break // Non-owned files and their directory are intentionally kept.
			}
		}
	}
	folder.Contents = files
	folder.History, err = s.planFolderHistoryLocked(pkg.ID)
	if err != nil {
		return folder, err
	}
	return folder, nil
}

func (s *issuePackageService) rememberPlanFolderLocked(pkg issuePackage) {
	if s.planFolderErrors == nil {
		s.planFolderErrors = map[string]string{}
	}
	if _, err := s.syncPlanFolderLocked(pkg, s.libraryLocked()); err != nil {
		s.planFolderErrors[pkg.ID] = err.Error()
	} else {
		delete(s.planFolderErrors, pkg.ID)
	}
}

func (s *issuePackageService) packagePlanFolderWarnings() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.planFolderWarningsLocked()
}

func (s *issuePackageService) planFolderWarningsLocked() map[string]string {
	result := map[string]string{}
	for id, warning := range s.planFolderErrors {
		result[id] = warning
	}
	return result
}

func (s *issuePackageService) preparePlanFolder(id string) (issuePackagePlanFolder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pkg, ok := s.packages[id]
	if !ok {
		return issuePackagePlanFolder{}, fmt.Errorf("issue package not found")
	}
	folder, err := s.syncPlanFolderLocked(pkg, s.libraryLocked())
	if err != nil {
		folder.Warning = err.Error()
		// An existing managed folder remains available for inspection/import,
		// while unsafe paths and non-regular files are still rejected.
		if folder.Path == "" {
			return folder, err
		}
		if _, boundaryErr := s.planFolderPath(folder.RelativePath); boundaryErr != nil {
			return folder, boundaryErr
		}
		folder.Contents = map[string][]byte{}
		for _, name := range folder.Files {
			raw, readErr := issuePackageReadPlanFile(filepath.Join(folder.Path, name))
			if readErr != nil {
				return folder, err
			}
			folder.Contents[name] = raw
		}
		folder.History, _ = s.planFolderHistoryLocked(pkg.ID)
	}
	return folder, nil
}

func (s *issuePackageService) handleIssuePackagePlanFolder(req issuePackageRequest, open func(string, bool) error) (any, error) {
	if req.Revision != "" {
		pkg, err := s.get(req.ID)
		if err != nil {
			return nil, err
		}
		if pkg.Revision != req.Revision {
			return nil, errIssuePackageConflict
		}
	}
	folder, err := s.preparePlanFolder(req.ID)
	if err != nil {
		return nil, err
	}
	if req.Action == "plan-folder-history-open" {
		found := false
		for _, entry := range folder.History {
			if entry.ID == req.SourceID {
				if err := open(entry.Path, true); err != nil {
					return nil, err
				}
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("saved plan version not found")
		}
	} else if req.Action == "plan-folder-open" {
		if err := open(folder.Path, false); err != nil {
			return nil, err
		}
	}
	return map[string]any{"folder": folder}, nil
}

// Keep generated paths available for existing packages on service restart.
func (s *issuePackageService) initializePlanFolders() {
	ids := make([]string, 0, len(s.packages))
	for id := range s.packages {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		s.rememberPlanFolderLocked(s.packages[id])
	}
}

func (s *issuePackageService) removeGeneratedPlanFolder(record issuePackagePlanFolderRecord) string {
	if record.RelativePath == "" {
		return ""
	}
	directory, err := s.planFolderPath(record.RelativePath)
	if err != nil {
		return "Generated plan files were preserved because their folder could not be verified."
	}
	kept := false
	for name, checksum := range record.SHA256 {
		path := filepath.Join(directory, name)
		raw, err := issuePackageReadPlanFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || issuePackagePlanDigest(raw) != checksum {
			kept = true
			continue
		}
		if os.Remove(path) != nil {
			kept = true
		}
	}
	root, _ := filepath.Abs(filepath.Join(s.root, issuePackagePlanDirectory))
	for path := directory; path != root && path != filepath.Dir(path); path = filepath.Dir(path) {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			if path == directory {
				kept = true
			}
			break
		}
	}
	if kept {
		return "Edited or unrelated files were preserved in " + directory
	}
	return ""
}
