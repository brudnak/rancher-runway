package test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTestLabConfigLibraryCopiesAndCleanup(t *testing.T) {
	s := testLabFixture(t)
	s.configRoot = filepath.Join(t.TempDir(), "private-configs")
	act := func(req testLabRequest) any {
		t.Helper()
		v, e := s.configAction(req)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	v := act(testLabRequest{Action: "folder-save", Name: "Tenant environments"}).(testLabConfigLibrary)
	folder := v.Folders[0].ID
	raw := "# retain all comments\n" + testLabFixtureConfig + "tenantRanchers:\n  clients: []\n"
	f := act(testLabRequest{Action: "config-save", Name: "Hosted tenant", Folder: folder, Config: raw}).(testLabConfigFile)
	// Neither credentials nor actual YAML belong in the list response or metadata.
	state := act(testLabRequest{Action: "config-library"})
	b, _ := json.Marshal(state)
	if strings.Contains(string(b), "verysecretvalue") || strings.Contains(string(b), "adminToken") {
		t.Fatal("config leaked in listing")
	}
	load := act(testLabRequest{Action: "config-load", ID: f.ID}).(map[string]any)
	copy := load["config"].(string) + "provider: aws\n"
	s2 := testLabFixture(t)
	s2.configRoot = s.configRoot
	v2, err := s2.configAction(testLabRequest{Action: "config-load", ID: f.ID})
	if err != nil || v2.(map[string]any)["config"] != raw {
		t.Fatal("restart/copy isolation failed", err)
	}
	if _, err = s.configAction(testLabRequest{Action: "folder-delete", ID: folder, Confirm: typedConfirmationPhrase}); err == nil {
		t.Fatal("deleted populated folder")
	}
	req := testLabRequest{Action: "config-save", ID: f.ID, Name: f.Name, Folder: folder, Revision: f.Revision, Config: copy}
	if _, err = s.configAction(req); err == nil {
		t.Fatal("overwrote without explicit confirmation")
	}
	req.Confirm = "UPDATE TEMPLATE"
	updated := act(req).(testLabConfigFile)
	if _, err = s.configAction(req); err == nil {
		t.Fatal("stale edit overwrote newer file")
	}
	files, _ := filepath.Glob(filepath.Join(s.configRoot, "*.yml"))
	if len(files) != 1 {
		t.Fatalf("retained old credential versions: %v", files)
	}
	info, _ := os.Stat(files[0])
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	info, _ = os.Stat(s.configRoot)
	if info.Mode().Perm() != 0700 {
		t.Fatal(info.Mode())
	}
	if _, err = s.configAction(testLabRequest{Action: "config-load", ID: "../../etc/passwd"}); err == nil {
		t.Fatal("accepted traversal")
	}
	if _, err = s.configAction(testLabRequest{Action: "config-delete", ID: f.ID, Revision: updated.Revision}); err == nil {
		t.Fatal("deleted without confirmation")
	}
	act(testLabRequest{Action: "config-delete", ID: f.ID, Revision: updated.Revision, Confirm: typedConfirmationPhrase})
	act(testLabRequest{Action: "folder-delete", ID: folder, Confirm: typedConfirmationPhrase})
	files, _ = filepath.Glob(filepath.Join(s.configRoot, "*.yml"))
	if len(files) != 0 {
		t.Fatal("credential file remains")
	}
}
func TestTestLabConfigLibraryInvalidYAMLAndSymlink(t *testing.T) {
	s := testLabFixture(t)
	s.configRoot = filepath.Join(t.TempDir(), "configs")
	for _, raw := range []string{"key: a\nkey: b\n", "a: 1\n---\nb: 2", "[]", strings.Repeat("x", 129<<10)} {
		if _, err := s.configAction(testLabRequest{Action: "config-save", Name: "Bad", Config: raw}); err == nil {
			t.Fatal("accepted invalid YAML")
		}
	}
	outside := filepath.Join(t.TempDir(), "secret")
	os.WriteFile(outside, []byte("private"), 0600)
	os.Symlink(outside, filepath.Join(s.configRoot, "index.json"))
	if _, err := s.configAction(testLabRequest{Action: "config-library"}); err == nil {
		t.Fatal("followed manifest symlink")
	}
}
func TestTestLabSourceConfigEvidence(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"validation/README.md":         "# Validation\n",
		"validation/tenants/README.md": "# Tenants\n\n```yaml\ntenantRanchers:\n  clients: []\n```",
		"actions/hostedtenant/config.go": `package hostedtenant
const ConfigurationFileKey="tenantRanchers"
type Client struct { Host string ` + "`yaml:\"host\"`" + ` }
type Config struct { Clients []Client ` + "`json:\"clients\" yaml:\"clients\"`" + `; Enabled bool ` + "`yaml:\"enabled\"`" + ` }`,
		"validation/tenants/tenant_test.go": `package tenants
import("github.com/rancher/shepherd/pkg/config";"github.com/rancher/tests/actions/hostedtenant";"github.com/stretchr/testify/require")
func SetupSuite(){var tenantConfig hostedtenant.Config; config.LoadConfig(hostedtenant.ConfigurationFileKey,&tenantConfig);require.NotEmpty(t,tenantConfig.Clients)}
func init(){panic("source indexing must never execute code")}`,
		"actions/defaults/config.go": `package defaults
const ClusterConfigKey="clusterConfig"
type Cluster struct { Count int ` + "`yaml:\"count\"`" + ` }`,
		"validation/provisioning/setup.go": `package provisioning
import("github.com/rancher/shepherd/pkg/config/operations";"github.com/rancher/tests/actions/defaults")
func Setup(){clusterConfig:=new(defaults.Cluster);operations.LoadObjectFromMap(defaults.ClusterConfigKey,source,clusterConfig)}`,
	}
	for name, body := range files {
		p := filepath.Join(root, name)
		os.MkdirAll(filepath.Dir(p), 0700)
		os.WriteFile(p, []byte(body), 0600)
	}
	index, err := testLabIndexSource(root, testLabFixtureSHA)
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Docs) != 2 {
		t.Fatal(index.Docs)
	}
	result := index.check(testLabFixtureConfig, map[string]bool{"validation/tenants": true})
	if len(result.Sections) != 1 || result.Sections[0] != "tenantRanchers" || len(result.Readmes) != 2 {
		t.Fatalf("%+v", result)
	}
	check := func(raw, expected string) {
		t.Helper()
		r := index.check(raw, map[string]bool{"validation/tenants": true, "validation/provisioning": true})
		found := false
		for _, f := range r.Findings {
			if f.Path == expected && f.Level == "warning" && f.File != "" && f.Line > 0 {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing evidence %s: %+v", expected, r)
		}
	}
	check(testLabFixtureConfig+"tenantRanchers:\n  clients: []\n", "tenantRanchers.clients")
	check(testLabFixtureConfig+"tenantRanchers:\n  clients:\n    - host: 12\n", "tenantRanchers.clients[0].host")
	check(testLabFixtureConfig+"clusterConfig:\n  count: nope\n", "clusterConfig.count")
	r := index.check(testLabFixtureConfig+"tenantRanchers:\n  clients:\n    - host: tenant.test\n", map[string]bool{"validation/tenants": true})
	if len(r.Findings) != 0 {
		t.Fatal(r.Findings)
	}
}
func TestTestLabPinnedWorkspaceSource(t *testing.T) {
	s := testLabFixture(t)
	testLabArchive(t, filepath.Join(s.root, testLabFixtureSHA+".tar.gz"), map[string]string{"validation/README.md": "# Fixture", "validation/example/source.go": "package example"})
	if _, err := s.sourceAction(testLabRequest{Action: "source-docs", SHA: strings.Repeat("f", 40)}); err == nil {
		t.Fatal("read unpinned source")
	}
	value, err := s.sourceAction(testLabRequest{Action: "source-docs", SHA: testLabFixtureSHA})
	if err != nil || len(value.(map[string]any)["documents"].([]testLabReadme)) != 1 {
		t.Fatal(value, err)
	}
	if _, err = s.sourceAction(testLabRequest{Action: "preflight", SHA: testLabFixtureSHA, Selection: []string{"bogus"}}); err == nil {
		t.Fatal("checked stale selection")
	}
}
