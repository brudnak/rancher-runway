package test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/cachelab"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Organization is separate from investigation revisions and frozen sessions.
type issuePackageBucket struct {
	SourceRepo      string   `json:"sourceRepo,omitempty"`
	SourceMilestone int      `json:"sourceMilestone,omitempty"`
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	PackageIDs      []string `json:"packageIds"`
}
type issuePackageLibrary struct {
	Version  int                 `json:"version"`
	Revision string              `json:"revision"`
	Buckets  []issuePackageBucket `json:"buckets"`
	Unfiled  []string            `json:"unfiled"`
}

func (s *issuePackageService) loadPackageLibrary() error {
	s.library = issuePackageLibrary{Version: 1, Revision: cachelab.ID(), Buckets: []issuePackageBucket{}, Unfiled: []string{}}
	raw, err := issuePackageReadBounded(filepath.Join(s.root, ".library.json"), 1<<20)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = decodeIssuePackage(raw, &s.library); err != nil {
		return fmt.Errorf("package organization is invalid; existing files preserved: %w", err)
	}
	return validatePackageLibrary(s.library, nil)
}
func validatePackageLibrary(lib issuePackageLibrary, packages map[string]issuePackage) error {
	if lib.Version != 1 || !cachelab.IDPattern.MatchString(lib.Revision) || len(lib.Buckets) > 100 {
		return fmt.Errorf("invalid package library")
	}
	seen := map[string]bool{}
	buckets := map[string]bool{}
	check := func(ids []string) error {
		if len(ids) > 500 {
			return fmt.Errorf("too many package references")
		}
		for _, id := range ids {
			if !cachelab.IDPattern.MatchString(id) || seen[id] {
				return fmt.Errorf("invalid or duplicate package placement")
			}
			seen[id] = true
			if packages != nil {
				if _, ok := packages[id]; !ok {
					return fmt.Errorf("a package was removed; refresh the library")
				}
			}
		}
		return nil
	}
	for _, b := range lib.Buckets {

		if (b.SourceRepo == "") != (b.SourceMilestone == 0) || b.SourceMilestone < 0 {
			return fmt.Errorf("invalid bucket milestone source")
		}
		if b.SourceRepo != "" {
			if _, err := issueRadarRepo(b.SourceRepo); err != nil {
				return err
			}
		}
		if !cachelab.IDPattern.MatchString(b.ID) || buckets[b.ID] || !packageText(b.Name, 120, true) {
			return fmt.Errorf("invalid milestone bucket")
		}
		buckets[b.ID] = true
		if err := check(b.PackageIDs); err != nil {
			return err
		}
	}
	return check(lib.Unfiled)
}
func (s *issuePackageService) libraryLocked() issuePackageLibrary {
	raw, _ := json.Marshal(s.library)
	var lib issuePackageLibrary
	_ = json.Unmarshal(raw, &lib)
	if lib.Buckets == nil {
		lib.Buckets = []issuePackageBucket{}
	}
	if lib.Unfiled == nil {
		lib.Unfiled = []string{}
	}
	seen := map[string]bool{}
	clean := func(ids []string) []string {
		out := []string{}
		for _, id := range ids {
			if _, ok := s.packages[id]; ok && !seen[id] {
				out = append(out, id)
				seen[id] = true
			}
		}
		return out
	}
	for i := range lib.Buckets {
		lib.Buckets[i].PackageIDs = clean(lib.Buckets[i].PackageIDs)
	}
	lib.Unfiled = clean(lib.Unfiled)
	// New/imported packages have a deterministic placement even before organization is saved.
	extra := []issuePackage{}
	for id, pkg := range s.packages {
		if !seen[id] {
			extra = append(extra, pkg)
		}
	}
	sortPackagesByCreation(extra)
	for _, pkg := range extra {
		lib.Unfiled = append(lib.Unfiled, pkg.ID)
	}
	return lib
}
func sortPackagesByCreation(packages []issuePackage) {
	sort.Slice(packages, func(i, j int) bool {
		if packages[i].CreatedAt.Equal(packages[j].CreatedAt) {
			return packages[i].ID < packages[j].ID
		}
		return packages[i].CreatedAt.Before(packages[j].CreatedAt)
	})
}
func (s *issuePackageService) packageLibrarySnapshot() issuePackageLibrary {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.libraryLocked()
}
func (s *issuePackageService) savePackageLibrary(req issuePackageRequest) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if req.Library == nil || req.Library.Revision != s.library.Revision {
		return nil, errIssuePackageConflict
	}
	lib := *req.Library
	if err := validatePackageLibrary(lib, s.packages); err != nil {
		return nil, err
	}
	count := len(lib.Unfiled)
	for _, b := range lib.Buckets {
		count += len(b.PackageIDs)
	}
	if count != len(s.packages) {
		return nil, fmt.Errorf("library membership changed; refresh before organizing")
	}
	lib.Revision = cachelab.ID()
	raw, err := json.MarshalIndent(lib, "", "  ")
	if err != nil {
		return nil, err
	}
	if err = writePrivateConfigAtomically(filepath.Join(s.root, ".library.json"), raw); err != nil {
		return nil, err
	}
	s.library = lib
	return map[string]any{"library": s.libraryLocked()}, nil
}
func (s *issuePackageService) exportPackageLibrary(req issuePackageRequest) (any, error) {
	lib := s.packageLibrarySnapshot()
	if req.Library == nil {
		return nil, fmt.Errorf("refresh and review the library before exporting")
	}
	reviewed, _ := json.Marshal(req.Library)
	currentLibrary, _ := json.Marshal(lib)
	if !bytes.Equal(reviewed, currentLibrary) {
		return nil, errIssuePackageConflict
	}
	if req.Revision != lib.Revision {
		return nil, errIssuePackageConflict
	}
	title := "Issue package library"
	groups := append([]issuePackageBucket{}, lib.Buckets...)
	groups = append(groups, issuePackageBucket{ID: "unfiled", Name: "Unfiled", PackageIDs: lib.Unfiled})
	if req.BucketID != "" {
		found := false
		for _, b := range groups {
			if b.ID == req.BucketID {
				groups = []issuePackageBucket{b}
				title = b.Name
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("milestone bucket no longer exists")
		}
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	index := issuePackageLibrary{Version: 1, Revision: lib.Revision, Buckets: groups, Unfiled: []string{}}
	total := 0
	revisions := map[string]string{}
	count := 0
	report := "# " + issuePackageMarkdownText(title) + "\n\nOrdered package archive. Unzip and import the individual `.runway-issue-package.json` files in Runway. Only saved plans and observations are exported.\n\n"
	put := func(name string, raw []byte) error {
		total += len(raw)
		if total > 256<<20 {
			return fmt.Errorf("bulk export exceeds 256 MiB; export smaller buckets or omit evidence")
		}
		f, err := writer.Create(name)
		if err != nil {
			return err
		}
		_, err = f.Write(raw)
		return err
	}
	for i, b := range groups {
		report += "## " + issuePackageMarkdownText(b.Name) + "\n\n"
		for j, id := range b.PackageIDs {
			pkg, err := s.get(id)
			if err != nil {
				return nil, err
			}
			names := []string{}
			if req.IncludeEvidence {
				for _, session := range pkg.Sessions {
					for _, e := range session.Evidence {
						if e.Artifact != nil && !e.Artifact.Omitted {
							names = append(names, e.Artifact.Name)
						}
					}
				}
			}
			bundle, err := s.buildBundle(id, req.IncludeNotes, names)
			if err != nil {
				return nil, err
			}
			if bundle.Package.Revision != pkg.Revision {
				return nil, errIssuePackageConflict
			}
			revisions[id] = bundle.Package.Revision
			raw, err := json.MarshalIndent(bundle, "", "  ")
			if err != nil {
				return nil, err
			}
			prefix := fmt.Sprintf("%03d-%s/%03d-%s-%s", i+1, cachelab.FileLabel(b.Name), j+1, cachelab.FileLabel(pkg.Title), id[:8])
			if err = put(prefix+".runway-issue-package.json", raw); err != nil {
				return nil, err
			}
			markdown, err := issuePackageMarkdown(bundle.Package, "", req.IncludeNotes)
			if err != nil {
				return nil, err
			}
			if err = put(prefix+"-report.md", []byte(markdown)); err != nil {
				return nil, err
			}
			report += "- " + issuePackageMarkdownText(pkg.Title) + " · " + issuePackageMarkdownText(pkg.Status) + "\n"
			count++
		}
		report += "\n"
	}
	if count == 0 {
		return nil, fmt.Errorf("this selection contains no packages")
	}
	raw, _ := json.MarshalIndent(index, "", "  ")
	if err := put("library.json", raw); err != nil {
		return nil, err
	}
	if err := put("README.md", []byte(report)); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	// Verify organization and package membership remained unchanged before publishing.
	for id, revision := range revisions {
		pkg, err := s.get(id)
		if err != nil || pkg.Revision != revision {
			return nil, errIssuePackageConflict
		}
	}
	current := s.packageLibrarySnapshot()
	a, _ := json.Marshal(lib)
	b, _ := json.Marshal(current)
	if !bytes.Equal(a, b) {
		return nil, errIssuePackageConflict
	}
	directory := s.exportRoot
	if directory == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		directory = filepath.Join(home, "Downloads")
	}
	if err := os.MkdirAll(directory, 0755); err != nil {
		return nil, err
	}
	name := cachelab.ID()[:8] + "-" + cachelab.FileLabel(strings.TrimSpace(title)) + ".runway-issue-packages.zip"
	path := filepath.Join(directory, name)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	_, err = file.Write(buffer.Bytes())
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	return map[string]any{"filename": name, "path": path, "packages": count}, nil
}
