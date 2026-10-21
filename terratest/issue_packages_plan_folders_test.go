package test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brudnak/ha-rancher-rke2/internal/cachelab"
)

func TestIssuePackagePlanFilesAreAutomaticPortableAndReadable(t *testing.T) {
	s := packageTestService(t)
	pkg := packageTestPlan(t, s)
	pkg.Notes = "PRIVATE-NOTE-DO-NOT-EXPORT"
	pkg.Summary = "A visible summary with <tags> & **formatting**."
	pkg.Cases[0].Preconditions = "A user has access\nand another condition"
	pkg.Cases[0].Expected = "The resource appears\nwithout duplication"
	pkg.Cases[0].Steps[0].Instruction = "Open [the project]\nthen inspect `the list` & <labels>"
	pkg.Cases[0].Steps[0].Expected = "Visible\nwithout <errors>"
	pkg.Cases[0].Selection = []string{"/Users/private/local/selection"}
	s.mu.Lock()
	err := s.saveLocked(pkg)
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	folder, err := s.preparePlanFolder(pkg.ID)
	if err != nil || folder.Warning != "" {
		t.Fatalf("plan folder unavailable: %+v %v", folder, err)
	}
	if !strings.Contains(folder.RelativePath, "unfiled/plan-") {
		t.Fatalf("unlinked plan has wrong location: %s", folder.RelativePath)
	}
	for _, name := range folder.Files {
		raw, err := os.ReadFile(filepath.Join(folder.Path, name))
		if err != nil || len(raw) == 0 {
			t.Fatalf("missing automatic %s: %v", name, err)
		}
		for _, private := range []string{"PRIVATE-NOTE-DO-NOT-EXPORT", "/Users/private/local/selection", `"sessions"`, `"evidence"`} {
			if strings.Contains(string(raw), private) {
				t.Fatalf("portable file exposed %q", private)
			}
		}
		if info, _ := os.Stat(filepath.Join(folder.Path, name)); info.Mode().Perm() != 0600 {
			t.Fatal("plan file permissions were not private")
		}
	}
	imported, err := parseIssuePackagePlanMarkdown(folder.Contents["test-plan.md"])
	if err != nil {
		t.Fatal(err)
	}
	if imported.Title != pkg.Title || imported.Summary != pkg.Summary || len(imported.Cases) != 1 {
		t.Fatalf("Markdown did not round trip title/summary/cases: %+v", imported)
	}
	got, want := imported.Cases[0], pkg.Cases[0]
	if got.Title != want.Title || got.Preconditions != want.Preconditions || got.Expected != want.Expected || got.Steps[0].Instruction != want.Steps[0].Instruction || got.Steps[0].Expected != want.Steps[0].Expected {
		t.Fatalf("Markdown lost authored case content: got %+v want %+v", got, want)
	}
	if len(folder.History) < 2 {
		t.Fatal("automatic local Markdown history was not recorded")
	}
	restarted, err := newIssuePackageService(s.root)
	if err != nil {
		t.Fatal(err)
	}
	after, err := restarted.preparePlanFolder(pkg.ID)
	if err != nil || after.Path != folder.Path || len(after.History) != len(folder.History) {
		t.Fatal("restart changed stable path or added duplicate history")
	}
}

func TestIssuePackagePlanFoldersFollowMilestonesWithoutCollisions(t *testing.T) {
	s := packageTestService(t)
	pkg := packageTestPlan(t, s)
	pkg = packageTestMutation(t, s, issuePackageRequest{Action: "update", ID: pkg.ID, Revision: pkg.Revision, Title: pkg.Title, IssueURL: "https://github.com/rancher/rancher/issues/123", Status: "planning", Cases: pkg.Cases}, nil, nil, nil)
	before, err := s.preparePlanFolder(pkg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(before.RelativePath, "github.com/rancher/rancher/unfiled/issue-123--") {
		t.Fatalf("issue identity missing: %s", before.RelativePath)
	}
	if err := os.WriteFile(filepath.Join(before.Path, "personal.txt"), []byte("keep my file"), 0600); err != nil {
		t.Fatal(err)
	}
	lib := s.packageLibrarySnapshot()
	bucket := issuePackageBucket{ID: cachelab.ID(), Name: "v2.15 / ../release", SourceRepo: "rancher/rancher", SourceMilestone: 23, PackageIDs: []string{pkg.ID}}
	lib.Buckets, lib.Unfiled = []issuePackageBucket{bucket}, []string{}
	if _, err := s.savePackageLibrary(issuePackageRequest{Library: &lib}); err != nil {
		t.Fatal(err)
	}
	moved, err := s.preparePlanFolder(pkg.ID)
	if err != nil || moved.Path == before.Path || !strings.Contains(moved.RelativePath, "v2.15-..-release--") {
		t.Fatalf("milestone did not move owned files: %+v %v", moved, err)
	}
	if _, err := os.Stat(filepath.Join(before.Path, "test-plan.md")); !os.IsNotExist(err) {
		t.Fatal("stale managed Markdown remained in old milestone")
	}
	if raw, err := os.ReadFile(filepath.Join(before.Path, "personal.txt")); err != nil || string(raw) != "keep my file" {
		t.Fatal("milestone move touched an unrelated file")
	}
	lib = s.packageLibrarySnapshot()
	lib.Buckets[0].Name = "Renamed release"
	if _, err := s.savePackageLibrary(issuePackageRequest{Library: &lib}); err != nil {
		t.Fatal(err)
	}
	renamed, err := s.preparePlanFolder(pkg.ID)
	if err != nil || renamed.Path == moved.Path || renamed.IssueNumber != 123 {
		t.Fatal("milestone rename lost stable issue identity")
	}
	other := pkg
	other.ID = cachelab.ID()
	if issuePackagePlanLocation(other, lib).RelativePath == renamed.RelativePath {
		t.Fatal("two plans for one issue collide")
	}
	other.ID, other.IssueURL = pkg.ID, "https://github.com/another/repository/issues/123"
	if issuePackagePlanLocation(other, lib).RelativePath == renamed.RelativePath {
		t.Fatal("same issue number in another repository collides")
	}
}

func TestIssuePackagePlanFoldersPreserveLocalEditsAndRejectLinks(t *testing.T) {
	s := packageTestService(t)
	pkg := packageTestPlan(t, s)
	folder, err := s.preparePlanFolder(pkg.ID)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(folder.Contents["test-plan.md"]), "Project member sees the resource", "A manually edited case", -1)
	if err := os.WriteFile(filepath.Join(folder.Path, "test-plan.md"), []byte(edited), 0600); err != nil {
		t.Fatal(err)
	}
	pkg = packageTestMutation(t, s, issuePackageRequest{Action: "update", ID: pkg.ID, Revision: pkg.Revision, Title: "Changed in the app", Status: "planning", Cases: pkg.Cases}, nil, nil, nil)
	after, err := s.preparePlanFolder(pkg.ID)
	if err != nil || !strings.Contains(after.Warning, "local edits") || string(after.Contents["test-plan.md"]) != edited {
		t.Fatal("an app save overwrote local Markdown edits")
	}
	imported, err := parseIssuePackagePlanMarkdown(after.Contents["test-plan.md"])
	if err != nil || imported.Cases[0].Title != "A manually edited case" {
		t.Fatal("visible Markdown edits were not authoritative on import")
	}
	if s.packagePlanFolderWarnings()[pkg.ID] == "" {
		t.Fatal("automatic generation conflict was not surfaced")
	}
	outside := t.TempDir()
	if err := os.Remove(filepath.Join(after.Path, "test-plan.md")); err != nil {
		t.Fatal(err)
	}
	outsideFile := filepath.Join(outside, "private.md")
	if err := os.WriteFile(outsideFile, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(after.Path, "test-plan.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.preparePlanFolder(pkg.ID); err == nil {
		t.Fatal("symlinked Markdown was read or overwritten")
	}
	if raw, _ := os.ReadFile(outsideFile); string(raw) != "private" {
		t.Fatal("a link crossed the plan storage boundary")
	}
	for _, path := range []string{"../outside", "/tmp/outside", "a/../../outside", "a\\outside", "a//b", "."} {
		if _, err := s.planFolderPath(path); err == nil {
			t.Fatalf("unsafe relative path accepted: %q", path)
		}
	}
}

func TestIssuePackagePlanFolderSelectedEnvironmentAndHistoryOpen(t *testing.T) {
	p := clusterWorkspaceTestPanel(t)
	s := packageTestService(t)
	pkg := packageTestPlan(t, s)
	p.rememberClusterSnapshot([]clusterView{{ID: "selected", Name: "Tenant Rancher", Type: "local", Role: "tenant", DeploymentType: deploymentTypeHostedTenantK3S, Version: "head", RancherURL: "https://private.endpoint.test"}})
	result, err := p.handleIssuePackagePlanFolder(s, issuePackageRequest{Action: "plan-folder", ID: pkg.ID, Revision: pkg.Revision, Environment: issuePackageEnvironment{ClusterID: "selected"}}, func(string, bool) error { t.Fatal("folder info opened an app"); return nil })
	if err != nil {
		t.Fatal(err)
	}
	folder := result.(map[string]any)["folder"].(issuePackagePlanFolder)
	if !strings.Contains(string(folder.Contents["test-plan.md"]), "Hosted tenant Rancher on K3s") {
		t.Fatal("selected environment was not materialized")
	}
	raw, err := os.ReadFile(filepath.Join(s.root, pkg.ID, ".plan-environment.json"))
	if err != nil || strings.Contains(string(raw), "private.endpoint.test") {
		t.Fatal("private endpoint leaked into public environment cache")
	}
	pkg = packageTestMutation(t, s, issuePackageRequest{Action: "update", ID: pkg.ID, Revision: pkg.Revision, Title: "Subsequent save", Status: "planning", Cases: pkg.Cases}, nil, nil, nil)
	folder, err = s.preparePlanFolder(pkg.ID)
	if err != nil || !strings.Contains(string(folder.Contents["test-plan.md"]), "Hosted tenant Rancher on K3s") {
		t.Fatal("automatic save forgot selected environment")
	}
	opened := ""
	_, err = s.handleIssuePackagePlanFolder(issuePackageRequest{Action: "plan-folder-history-open", ID: pkg.ID, SourceID: folder.History[0].ID}, func(path string, reveal bool) error {
		if !reveal {
			t.Fatal("history did not reveal the Markdown file")
		}
		opened = path
		return nil
	})
	if err != nil || opened != folder.History[0].Path {
		t.Fatal("history open used an unverified path")
	}
	if _, err := s.handleIssuePackagePlanFolder(issuePackageRequest{Action: "plan-folder-history-open", ID: pkg.ID, SourceID: "../../private"}, func(string, bool) error { t.Fatal("untrusted history path opened"); return nil }); err == nil {
		t.Fatal("unrecognized history id accepted")
	}
}

func TestIssuePackagePlanMarkdownImportBoundsAndVisibleEdits(t *testing.T) {
	plan, err := parseIssuePackagePlanMarkdown([]byte("# Small test plan\n\n1. First case\n2) Second case\n"))
	if err != nil || plan.Title != "Small test plan" || len(plan.Cases) != 2 {
		t.Fatalf("numbered title import failed: %+v %v", plan, err)
	}
	for _, raw := range []string{"arbitrary prose", "1. " + strings.Repeat("x", 241), strings.Repeat("1. A case\n", 251), "### 🧪 Test Cases\n\n**Broken**\n"} {
		if _, err := parseIssuePackagePlanMarkdown([]byte(raw)); err == nil {
			t.Fatal("invalid plan accepted")
		}
	}
}

func TestIssuePackagePlanFolderDeleteOnlyRemovesGeneratedFiles(t *testing.T) {
	for _, edited := range []bool{false, true} {
		t.Run(fmt.Sprint(edited), func(t *testing.T) {
			s := packageTestService(t)
			pkg := packageTestPlan(t, s)
			folder, err := s.preparePlanFolder(pkg.ID)
			if err != nil {
				t.Fatal(err)
			}
			if edited {
				if err := os.WriteFile(filepath.Join(folder.Path, "test-plan.md"), []byte("my edited file"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			result, err := s.mutate(issuePackageRequest{Action: "delete", ID: pkg.ID, Revision: pkg.Revision, Confirm: typedConfirmationPhrase}, nil, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if edited {
				if raw, err := os.ReadFile(filepath.Join(folder.Path, "test-plan.md")); err != nil || string(raw) != "my edited file" {
					t.Fatal("delete removed externally edited Markdown")
				}
				if result.(map[string]any)["warning"] == "" {
					t.Fatal("preserved files were not disclosed")
				}
			} else if _, err := os.Stat(folder.Path); !os.IsNotExist(err) {
				t.Fatal("delete left generated Markdown behind")
			}
		})
	}
}

func TestIssuePackagePlanLocalHistoryIsBounded(t *testing.T) {
	s := packageTestService(t)
	pkg := packageTestPlan(t, s)
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := 0; i < 55; i++ {
		if err := s.savePlanHistoryLocked(pkg, []byte(fmt.Sprintf("# Version %d\n\n1. A case\n", i))); err != nil {
			t.Fatal(err)
		}
	}
	history, err := s.planFolderHistoryLocked(pkg.ID)
	if err != nil || len(history) != 50 {
		t.Fatalf("history was not bounded to 50 saved versions: %d %v", len(history), err)
	}
}

func TestIssuePackagePlanImportedEnvironmentIsDocumentaryAndPreserved(t *testing.T) {
	s := packageTestService(t)
	pkg := packageTestPlan(t, s)
	pkg.FixURL = "https://github.com/rancher/rancher/pull/456"
	e := issuePackageEnvironment{Source: "manual", RecordedAt: pkg.UpdatedAt, ClusterName: "Tenant | alpha", RancherVersion: "v2.15.2", KubernetesVersion: "v1.35.3+k3s1"}
	packageEnvironmentDetail(&e, "Kubernetes distribution", "K3s", "manual", nil)
	packageEnvironmentDetail(&e, "Installation type", "Hosted tenant Rancher on K3s", "manual", nil)
	packageEnvironmentDetail(&e, "Installation option", "Helm", "manual", nil)
	packageEnvironmentDetail(&e, "Cert Details", "external TLS", "manual", nil)
	packageEnvironmentDetail(&e, "Webhook chart version", "v0.12.1", "manual", nil)
	files, err := renderIssuePackagePlanFilesWithEnvironment(pkg, s.packageLibrarySnapshot(), e)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := parseIssuePackagePlanMarkdown(files["test-plan.md"])
	if err != nil || plan.FixURL != pkg.FixURL {
		t.Fatal("visible fix reference was lost")
	}
	imported, found, err := parseIssuePackagePlanMarkdownEnvironment(files["test-plan.md"])
	if err != nil || !found || imported.Source != "imported" || imported.ClusterName != e.ClusterName || imported.KubernetesVersion != e.KubernetesVersion || imported.URL != "" || imported.ClusterID != "" || imported.ObservedAt != nil {
		t.Fatalf("imported environment claimed a live target or lost documentary values: %+v %v", imported, err)
	}
	if err := s.rememberPlanEnvironment(pkg.ID, pkg.Revision, imported); err != nil {
		t.Fatal(err)
	}
	p := clusterWorkspaceTestPanel(t)
	result, err := p.handleIssuePackagePlanFolder(s, issuePackageRequest{Action: "plan-folder", ID: pkg.ID}, func(string, bool) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	folder := result.(map[string]any)["folder"].(issuePackagePlanFolder)
	for _, text := range []string{"Tenant \\| alpha", "| Installation option | Helm |", "| Cert Details | external TLS |", "v0.12.1", "not refreshed from a cluster"} {
		if !strings.Contains(string(folder.Contents["test-plan.md"]), text) {
			t.Fatalf("imported documentary environment lost %q", text)
		}
	}
	copyResult, err := p.issuePackagePlanMarkdown(s, issuePackageRequest{Action: "plan-markdown", ID: pkg.ID, Revision: pkg.Revision})
	if err != nil || !strings.Contains(copyResult.(map[string]any)["markdown"].(string), "Tenant \\| alpha") {
		t.Fatal("copying imported Markdown discarded its documented environment")
	}
}
