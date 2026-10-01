package test

import (
	"context"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

func workIssue(n int, state string) issueRadarIssue {
	return issueRadarIssue{Number: n, Title: fmt.Sprintf("Issue %d", n), URL: fmt.Sprintf("https://github.com/rancher/rancher/issues/%d", n), State: state}
}
func workSnapshot(issues ...issueRadarIssue) myWorkSnapshot {
	return myWorkSnapshot{Config: myWorkConfig{Repo: "rancher/rancher", Milestone: 19, Scope: "all"}, Milestone: issueRadarMilestone{Number: 19, Title: "v2.16.0"}, GeneratedAt: time.Now().UTC(), Issues: issues, Departed: []issueRadarIssue{}, History: []myWorkPoint{}}
}
func TestMyWorkIntakePreservesPlansMovesAndRestart(t *testing.T) {
	s := packageTestService(t)
	existing := packageTestPlan(t, s)
	existing = packageTestMutation(t, s, testPackageRequest{Action: "update", ID: existing.ID, Revision: existing.Revision, Title: existing.Title, IssueURL: workIssue(123, "open").URL, Status: existing.Status, Cases: existing.Cases}, nil, nil, nil)
	existing = packageTestStart(t, s, existing)
	out, err := s.prepareMyWork(workSnapshot(workIssue(123, "open"), workIssue(456, "open"), workIssue(789, "closed")))
	if err != nil {
		t.Fatal(err)
	}
	lib := s.packageLibrarySnapshot()
	if out.Created != 1 || len(lib.Buckets) != 1 || len(lib.Buckets[0].PackageIDs) != 2 || len(lib.Unfiled) != 0 {
		t.Fatalf("wrong intake: %+v %+v", out, lib)
	}
	pkg, _ := s.get(existing.ID)
	if !reflect.DeepEqual(pkg, existing) {
		t.Fatal("existing frozen history changed")
	}
	starter, _ := s.get(lib.Buckets[0].PackageIDs[1])
	if len(starter.Cases) != 0 || starter.Status != "planning" {
		t.Fatal("starter claims preparation")
	}
	lib.Buckets[0].Name = "Renamed release"
	lib.Buckets[0].PackageIDs = []string{existing.ID}
	lib.Unfiled = []string{starter.ID}
	if _, err = s.savePackageLibrary(testPackageRequest{Library: &lib}); err != nil {
		t.Fatal(err)
	}
	out, err = s.prepareMyWork(workSnapshot(workIssue(123, "closed"), workIssue(456, "open")))
	if err != nil {
		t.Fatal(err)
	}
	lib = s.packageLibrarySnapshot()
	if out.Created != 0 || len(out.Departed) != 1 || len(out.History) != 2 || lib.Buckets[0].Name != "Renamed release" || len(lib.Unfiled) != 1 || lib.Unfiled[0] != starter.ID {
		t.Fatalf("refresh overwrote organization or lost scope changes: %+v %+v", out, lib)
	}
	reopened, err := newTestPackageService(s.root)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := reopened.myWork()
	if err != nil || saved.BucketID != out.BucketID || len(saved.History) != 2 {
		t.Fatal("snapshot lost on restart", err)
	}
	other := workSnapshot(workIssue(456, "open"))
	other.Config.Scope = "unassigned"
	out, err = reopened.prepareMyWork(other)
	if err != nil || len(out.History) != 1 || len(out.Departed) != 0 || out.Created != 0 {
		t.Fatal("scope change reused unrelated trend", err)
	}
}
func TestMyWorkDoesNotMovePackagesFromManualBuckets(t *testing.T) {
	s := packageTestService(t)
	pkg := packageTestPlan(t, s)
	pkg = packageTestMutation(t, s, testPackageRequest{Action: "update", ID: pkg.ID, Revision: pkg.Revision, Title: pkg.Title, IssueURL: workIssue(123, "open").URL, Status: pkg.Status, Cases: pkg.Cases}, nil, nil, nil)
	lib := s.packageLibrarySnapshot()
	lib.Buckets = []testPackageBucket{{ID: cacheLabID(), Name: "Manual regression", PackageIDs: []string{pkg.ID}}}
	lib.Unfiled = []string{}
	if _, err := s.savePackageLibrary(testPackageRequest{Library: &lib}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.prepareMyWork(workSnapshot(workIssue(123, "open"))); err != nil {
		t.Fatal(err)
	}
	lib = s.packageLibrarySnapshot()
	if len(lib.Buckets) != 2 || len(lib.Buckets[0].PackageIDs) != 1 || len(lib.Buckets[1].PackageIDs) != 0 {
		t.Fatal("manual membership changed")
	}
}
func TestMyWorkFailedIntakeRollsBackStarters(t *testing.T) {
	s := packageTestService(t)
	bad := workIssue(2, "open")
	bad.Title = ""
	_, err := s.prepareMyWork(workSnapshot(workIssue(1, "open"), bad))
	if err == nil {
		t.Fatal("invalid starter accepted")
	}
	pkgs, _ := s.list()
	if len(pkgs) != 0 || len(s.packageLibrarySnapshot().Buckets) != 0 {
		t.Fatal("failed intake left partial starters")
	}
}
func TestMyWorkReadsClosedAndOpenWithExactMilestoneAndOwner(t *testing.T) {
	for _, scope := range []string{"mine", "unassigned", "all"} {
		t.Run(scope, func(t *testing.T) {
			s := &issueRadarService{runCommand: radarRunner(t, func(endpoint *url.URL) ([]byte, error) {
				if endpoint.Path == "user" {
					return radarResponse(t, "alice"), nil
				}
				if strings.HasSuffix(endpoint.Path, "/milestones/19") {
					return radarResponse(t, issueRadarMilestone{Number: 19, Title: "v2.16.0"}), nil
				}
				q := endpoint.Query()
				want := ""
				if scope == "mine" {
					want = "alice"
				}
				if scope == "unassigned" {
					want = "none"
				}
				if q.Get("milestone") != "19" || q.Get("state") != "all" || q.Get("assignee") != want || q.Get("labels") != "" {
					t.Fatalf("wrong scope: %v", q)
				}
				pr := workIssue(4, "open")
				pr.PullRequest = &struct{}{}
				return radarResponse(t, []issueRadarIssue{workIssue(1, "open"), workIssue(2, "closed"), pr}), nil
			})}
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			out, err := s.pullMyWork(ctx, myWorkConfig{Repo: "rancher/rancher", Milestone: 19, Scope: scope})
			if err != nil || len(out.Issues) != 2 {
				t.Fatal("bad read", err)
			}
			if scope == "mine" && out.Config.User != "alice" {
				t.Fatal("signed-in username missing")
			}
		})
	}
}
func TestMyWorkInvalidScope(t *testing.T) {
	for _, c := range []myWorkConfig{{Repo: "evil/../x", Milestone: 1, Scope: "all"}, {Repo: "rancher/rancher", Scope: "all"}, {Repo: "rancher/rancher", Milestone: 1, Scope: "unknown"}, {Repo: "rancher/rancher", Milestone: 1, Scope: "mine", User: "--token"}} {
		if _, err := normalizeMyWorkConfig(c); err == nil {
			t.Fatal("invalid scope accepted", c)
		}
	}
}

func TestMyWorkMetadataRefreshDropsDepartedAndPreservesLibrary(t *testing.T) {
	s := packageTestService(t)
	old, err := s.prepareMyWork(workSnapshot(workIssue(50186, "open"), workIssue(56397, "open")))
	if err != nil {
		t.Fatal(err)
	}
	before := s.packageLibrarySnapshot()
	fresh := workSnapshot(workIssue(56397, "open"), workIssue(99, "open"))
	fresh.Issues[1].Labels = []struct {
		Name string `json:"name"`
	}{{Name: "qa/none"}}
	got, err := s.refreshMyWorkSnapshot(fresh)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Departed) != 1 || got.Departed[0].Number != 50186 || got.History[len(got.History)-1].Open != 1 || got.Created != 0 {
		t.Fatalf("stale scope %+v", got)
	}
	if !reflect.DeepEqual(before, s.packageLibrarySnapshot()) {
		t.Fatal("refresh changed saved plans or organization")
	}
	packages, _ := s.list()
	if len(packages) != 2 {
		t.Fatal("refresh created or deleted packages")
	}
	// Older responses cannot bring an issue back into the current scope.
	stale, err := s.refreshMyWorkSnapshot(old)
	if err != nil || stale.GeneratedAt != got.GeneratedAt {
		t.Fatal("stale refresh overwrote current observation", err)
	}
	fresh.Config.Scope = "mine"
	if _, err = s.refreshMyWorkSnapshot(fresh); err == nil {
		t.Fatal("refresh overwrote a different scope")
	}
}
func TestMyWorkQANoneDoesNotCreateStarter(t *testing.T) {
	s := packageTestService(t)
	issue := workIssue(50186, "open")
	issue.Labels = []struct {
		Name string `json:"name"`
	}{{Name: "QA/None"}}
	out, err := s.prepareMyWork(workSnapshot(issue))
	if err != nil {
		t.Fatal(err)
	}
	if out.Created != 0 || out.History[0].Open != 0 {
		t.Fatalf("QA/None requires preparation %+v", out)
	}
	packages, _ := s.list()
	if len(packages) != 0 {
		t.Fatal("unnecessary starter created")
	}
}
