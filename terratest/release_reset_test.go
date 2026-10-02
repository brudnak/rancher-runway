package test

import (
	"github.com/brudnak/ha-rancher-rke2/internal/cachelab"
	"os"
	"path/filepath"
	"testing"
)

func TestReleaseResetKeepsPackagesUnlessSelected(t *testing.T) {
	for _, all := range []bool{false, true} {
		s := packageTestService(t)
		pkg := packageTestPlan(t, s)
		lib, err := s.saveReleaseTracker("initial", releaseTracker{ID: cachelab.ID(), Name: "Old release"}, false)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(s.root, ".my-work.json"), []byte(`{}`), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err = s.clearReleaseWork("stale", all); err == nil {
			t.Fatal("stale reset accepted")
		}
		if _, err = s.clearReleaseWork(lib.Revision, all); err != nil {
			t.Fatal(err)
		}
		restarted, err := newIssuePackageService(s.root)
		if err != nil {
			t.Fatal(err)
		}
		if all && len(restarted.packages) != 0 {
			t.Fatal("packages survived full clear")
		}
		if !all {
			if _, err = restarted.get(pkg.ID); err != nil {
				t.Fatal("package removed without checkbox")
			}
			if len(restarted.library.Unfiled) != 1 {
				t.Fatal("retained package not moved to unfiled")
			}
		}
		trackers, err := restarted.releaseTrackers()
		if err != nil || len(trackers.Trackers) != 0 {
			t.Fatal("trackers not cleared")
		}
		if _, err = os.Stat(filepath.Join(s.root, ".my-work.json")); !os.IsNotExist(err) {
			t.Fatal("old intake not cleared")
		}
	}
}
