package test

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"github.com/brudnak/ha-rancher-rke2/internal/cachelab"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTestPackageLibraryMovesPreserveHistoryAndRestart(t *testing.T) {
	s := packageTestService(t)
	a := packageTestStart(t, s, packageTestPlan(t, s))
	b := packageTestPlan(t, s)
	lib := s.packageLibrarySnapshot()
	lib.Buckets = []testPackageBucket{{ID: cachelab.ID(), Name: "v2.16.0", PackageIDs: []string{b.ID, a.ID}}}
	lib.Unfiled = []string{}
	old := lib
	result, err := s.savePackageLibrary(testPackageRequest{Library: &lib})
	if err != nil {
		t.Fatal(err)
	}
	saved := result.(map[string]any)["library"].(testPackageLibrary)
	if _, err = s.savePackageLibrary(testPackageRequest{Library: &old}); !errors.Is(err, errTestPackageConflict) {
		t.Fatal("stale organization accepted", err)
	}
	unchanged, _ := s.get(a.ID)
	if unchanged.Revision != a.Revision || len(unchanged.Sessions) != 1 {
		t.Fatal("organization rewrote history")
	}
	reopened, err := newTestPackageService(s.root)
	if err != nil {
		t.Fatal(err)
	}
	after := reopened.packageLibrarySnapshot()
	if after.Revision != saved.Revision || strings.Join(after.Buckets[0].PackageIDs, ",") != b.ID+","+a.ID {
		t.Fatal("lost order on restart")
	}
	after.Buckets[0].Name = "v2.17.0"
	after.Buckets[0].PackageIDs = []string{a.ID}
	after.Unfiled = []string{b.ID}
	if _, err = reopened.savePackageLibrary(testPackageRequest{Library: &after}); err != nil {
		t.Fatal(err)
	}
}
func TestTestPackageLibraryRejectsDuplicateAndMissingMembership(t *testing.T) {
	s := packageTestService(t)
	pkg := packageTestPlan(t, s)
	lib := s.packageLibrarySnapshot()
	lib.Buckets = []testPackageBucket{{ID: cachelab.ID(), Name: "Next", PackageIDs: []string{pkg.ID}}}
	if _, err := s.savePackageLibrary(testPackageRequest{Library: &lib}); err == nil {
		t.Fatal("duplicate placement accepted")
	}
	lib.Buckets = nil
	lib.Unfiled = nil
	if _, err := s.savePackageLibrary(testPackageRequest{Library: &lib}); err == nil {
		t.Fatal("missing package accepted")
	}
	if err := os.WriteFile(filepath.Join(s.root, ".library.json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := newTestPackageService(s.root); err == nil {
		t.Fatal("corrupt organization replaced")
	}
}
func TestTestPackageLibraryBulkExportPortableAndPrivate(t *testing.T) {
	s := packageTestService(t)
	s.exportRoot = t.TempDir()
	pkg := packageTestStart(t, s, packageTestPlan(t, s))
	pkg.Notes = "PRIVATE-NOTES"
	if err := s.saveLocked(pkg); err != nil {
		t.Fatal(err)
	}
	lib := s.packageLibrarySnapshot()
	lib.Buckets = []testPackageBucket{{ID: cachelab.ID(), Name: "v2.16.0", PackageIDs: []string{pkg.ID}}}
	lib.Unfiled = []string{}
	result, err := s.savePackageLibrary(testPackageRequest{Library: &lib})
	if err != nil {
		t.Fatal(err)
	}
	lib = result.(map[string]any)["library"].(testPackageLibrary)
	exported, err := s.exportPackageLibrary(testPackageRequest{Library: &lib, Revision: lib.Revision, BucketID: lib.Buckets[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	path := exported.(map[string]any)["path"].(string)
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("export not private")
	}
	archive, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	bundles := 0
	for _, file := range archive.File {
		r, _ := file.Open()
		raw, _ := io.ReadAll(r)
		r.Close()
		if strings.Contains(string(raw), "PRIVATE-NOTES") {
			t.Fatal("private notes leaked")
		}
		if strings.HasSuffix(file.Name, ".runway-test-package.json") {
			var bundle testPackageBundle
			if err = json.Unmarshal(raw, &bundle); err != nil {
				t.Fatal(err)
			}
			artifacts, _, err := validateTestPackageBundle(&bundle)
			if err != nil {
				t.Fatal(err)
			}
			other := packageTestService(t)
			imported, err := other.importPackage(bundle.Package, artifacts)
			if err != nil {
				t.Fatal(err)
			}
			if imported.ID == pkg.ID || imported.Sessions[0].Status != "completed" {
				t.Fatal("import changed semantics")
			}
			bundles++
		}
	}
	if bundles != 1 {
		t.Fatal("missing portable package")
	}
}

func TestTestPackageLibraryBulkEvidenceOptIn(t *testing.T) {
	s := packageTestService(t)
	s.exportRoot = t.TempDir()
	pkg := packageTestStart(t, s, packageTestPlan(t, s))
	raw := []byte("retained sample log")
	e := testPackageEvidence{ID: cachelab.ID(), Kind: "test-run", Name: "Regression result", SourceID: cachelab.ID(), CaseID: pkg.Cases[0].ID, CapturedAt: pkg.CreatedAt, AttachedAt: pkg.UpdatedAt, Metadata: json.RawMessage(`{}`), Artifact: packageArtifact(cachelab.ID()+".log", "text/plain", raw)}
	pkg = packageTestMutation(t, s, testPackageRequest{Action: "attach-evidence", ID: pkg.ID, Revision: pkg.Revision, SessionID: pkg.Sessions[0].ID}, nil, &e, raw)
	lib := s.packageLibrarySnapshot()
	for _, include := range []bool{false, true} {
		result, err := s.exportPackageLibrary(testPackageRequest{Library: &lib, Revision: lib.Revision, IncludeEvidence: include})
		if err != nil {
			t.Fatal(err)
		}
		archive, err := zip.OpenReader(result.(map[string]any)["path"].(string))
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range archive.File {
			if !strings.HasSuffix(file.Name, ".runway-test-package.json") {
				continue
			}
			r, _ := file.Open()
			encoded, _ := io.ReadAll(r)
			r.Close()
			var bundle testPackageBundle
			if err = json.Unmarshal(encoded, &bundle); err != nil {
				t.Fatal(err)
			}
			data, _, err := validateTestPackageBundle(&bundle)
			if err != nil {
				t.Fatal(err)
			}
			if include && string(data[e.Artifact.Name]) != string(raw) {
				t.Fatal("opted-in evidence missing")
			}
			if !include && len(data) != 0 {
				t.Fatal("artifact bytes exported without opt-in")
			}
		}
		archive.Close()
	}
}

func TestTestPackageLibraryExportRejectsUnreviewedMembership(t *testing.T) {
	s := packageTestService(t)
	s.exportRoot = t.TempDir()
	packageTestPlan(t, s)
	reviewed := s.packageLibrarySnapshot()
	packageTestPlan(t, s)
	if _, err := s.exportPackageLibrary(testPackageRequest{Library: &reviewed, Revision: reviewed.Revision}); !errors.Is(err, errTestPackageConflict) {
		t.Fatal("export silently included an unreviewed new package", err)
	}
	files, err := os.ReadDir(s.exportRoot)
	if err != nil || len(files) != 0 {
		t.Fatal("conflicted export published a file")
	}
}
