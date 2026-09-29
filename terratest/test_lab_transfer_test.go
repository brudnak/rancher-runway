package test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTestLabConfigTransferRoundTrip(t *testing.T) {
	s := testLabFixture(t)
	s.configRoot = filepath.Join(t.TempDir(), "configs")
	s.exportRoot = t.TempDir()
	act := func(req testLabRequest) any {
		t.Helper()
		v, err := s.configAction(req)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	folders := act(testLabRequest{Action: "folder-save", Name: "Provisioning"}).(testLabConfigLibrary)
	folder := folders.Folders[0].ID
	act(testLabRequest{Action: "folder-save", Name: "Empty folder"})
	raw := "# Preserve comments and quoted values exactly.\nrancher:\n  host: 'rancher.example.test'\n  adminToken: \"token-fixture:private\"\n"
	f := act(testLabRequest{Action: "config-save", Name: "AWS staging", Folder: folder, Config: raw}).(testLabConfigFile)
	root := act(testLabRequest{Action: "config-save", Name: "Root config", Config: raw}).(testLabConfigFile)
	if _, err := s.configAction(testLabRequest{Action: "config-export", Scope: "library"}); err == nil {
		t.Fatal("exported credentials without acknowledgment")
	}
	if entries, _ := os.ReadDir(s.exportRoot); len(entries) != 0 {
		t.Fatal("unconfirmed export wrote a file")
	}
	for _, scope := range []string{"config", "folder", "library"} {
		t.Run(scope, func(t *testing.T) {
			id := f.ID
			if scope == "folder" {
				id = folder
			}
			result := act(testLabRequest{Action: "config-export", Scope: scope, ID: id, Confirm: "EXPORT WITH CREDENTIALS"}).(map[string]any)
			path := result["path"].(string)
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			info, _ := os.Stat(path)
			if info.Mode().Perm() != 0600 {
				t.Fatal("export permissions", info.Mode())
			}
			if filepath.Dir(path) != s.exportRoot {
				t.Fatal("unexpected export destination")
			}
			if scope == "config" {
				if string(b) != raw || !strings.HasSuffix(path, ".yml") {
					t.Fatal("YAML changed")
				}
				return
			}
			if !strings.HasSuffix(path, "runway-cattle-configs.json") {
				t.Fatal(path)
			}
			var bundle testLabConfigBundle
			if err = json.Unmarshal(b, &bundle); err != nil {
				t.Fatal(err)
			}
			expectedConfigs, expectedFolders := 1, 1
			if scope == "library" {
				expectedConfigs, expectedFolders = 2, 2
			}
			if len(bundle.Configs) != expectedConfigs || len(bundle.Folders) != expectedFolders {
				t.Fatalf("wrong export scope: %+v", bundle)
			}
			destination := testLabFixture(t)
			destination.configRoot = filepath.Join(t.TempDir(), "configs")
			previewValue, err := destination.configAction(testLabRequest{Action: "config-import-preview", Bundle: &bundle})
			if err != nil {
				t.Fatal(err)
			}
			preview := previewValue.(testLabImportPreview)
			serialized, _ := json.Marshal(preview)
			if strings.Contains(string(serialized), "token-fixture") {
				t.Fatal("preview leaked credentials")
			}
			if entries, _ := os.ReadDir(destination.configRoot); len(entries) != 0 {
				t.Fatal("preview changed library")
			}
			req := testLabRequest{Action: "config-import", Bundle: &bundle, Revision: preview.Revision, Confirm: "IMPORT COPIES"}
			if _, err = destination.configAction(req); err != nil {
				t.Fatal(err)
			}
			state, err := destination.configAction(testLabRequest{Action: "config-library"})
			if err != nil {
				t.Fatal(err)
			}
			lib := state.(map[string]any)["library"].(testLabConfigLibrary)
			for _, imported := range lib.Files {
				v, err := destination.configAction(testLabRequest{Action: "config-load", ID: imported.ID})
				if err != nil || v.(map[string]any)["config"] != raw {
					t.Fatal("round trip lost YAML", err)
				}
				if imported.ID == f.ID || imported.ID == root.ID {
					t.Fatal("reused exported identity")
				}
			}
			if len(lib.Folders) != expectedFolders {
				t.Fatal("empty folder lost")
			}
			if _, err = destination.configAction(req); err == nil {
				t.Fatal("replayed old preview")
			}
			fresh, _ := destination.configAction(testLabRequest{Action: "config-import-preview", Bundle: &bundle})
			p := fresh.(testLabImportPreview)
			if p.Renamed == 0 {
				t.Fatal("collisions were not renamed")
			}
			req.Revision = p.Revision
			if _, err = destination.configAction(req); err != nil {
				t.Fatal(err)
			}
			files, _ := filepath.Glob(filepath.Join(destination.configRoot, "*.yml"))
			if len(files) != expectedConfigs*2 {
				t.Fatal("import overwrote an existing config")
			}
		})
	}
	// An empty folder is a useful portable artifact too.
	state := act(testLabRequest{Action: "config-library"}).(map[string]any)["library"].(testLabConfigLibrary)
	b, _, err := testLabBuildConfigExport(s.configRoot, state, "folder", state.Folders[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	var empty testLabConfigBundle
	json.Unmarshal(b, &empty)
	if len(empty.Folders) != 1 || len(empty.Configs) != 0 {
		t.Fatal("empty folder export")
	}
	if _, err = testLabPlanConfigImport(testLabConfigLibrary{Version: 1}, testLabRequest{Bundle: &empty}); err != nil {
		t.Fatal(err)
	}
}
func TestTestLabConfigImportValidationAndAtomicity(t *testing.T) {
	empty := testLabConfigLibrary{Version: 1, Folders: []testLabConfigFolder{}, Files: []testLabConfigFile{}}
	good := func() *testLabConfigBundle {
		return &testLabConfigBundle{Format: testLabBundleFormat, Version: 1, Folders: []testLabConfigFolder{}, Configs: []testLabBundledConfig{{Name: "Fixture", YAML: testLabFixtureConfig}}}
	}
	cases := map[string]func(*testLabConfigBundle){
		"unknown format": func(b *testLabConfigBundle) { b.Format = "other" },
		"newer version":  func(b *testLabConfigBundle) { b.Version = 2 },
		"no entries":     func(b *testLabConfigBundle) { b.Configs = nil },
		"missing folder": func(b *testLabConfigBundle) { b.Configs[0].Folder = "missing" },
		"duplicate folder ID": func(b *testLabConfigBundle) {
			b.Folders = []testLabConfigFolder{{ID: "same", Name: "One"}, {ID: "same", Name: "Two"}}
		},
		"invalid YAML":     func(b *testLabConfigBundle) { b.Configs[0].YAML = "key: 1\nkey: 2" },
		"oversized YAML":   func(b *testLabConfigBundle) { b.Configs[0].YAML = "key: " + strings.Repeat("x", 128<<10) },
		"empty name":       func(b *testLabConfigBundle) { b.Configs[0].Name = "" },
		"control name":     func(b *testLabConfigBundle) { b.Folders = []testLabConfigFolder{{ID: "a", Name: "A\nB"}} },
		"too many configs": func(b *testLabConfigBundle) { b.Configs = make([]testLabBundledConfig, 501) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			b := good()
			mutate(b)
			if _, err := testLabPlanConfigImport(empty, testLabRequest{Bundle: b}); err == nil {
				t.Fatal("accepted invalid import")
			}
		})
	}
	if _, err := testLabPlanConfigImport(empty, testLabRequest{Bundle: good(), Folder: "missing"}); err == nil {
		t.Fatal("accepted nonexistent destination")
	}
	full := empty
	full.Files = make([]testLabConfigFile, 500)
	if _, err := testLabPlanConfigImport(full, testLabRequest{Bundle: good()}); err == nil {
		t.Fatal("exceeded combined capacity")
	}
	// Names and foreign identifiers are metadata; imports always create opaque local paths.
	bundle := good()
	bundle.Folders = []testLabConfigFolder{{ID: "../../elsewhere", Name: "../Metadata folder"}}
	bundle.Configs[0].Folder = "../../elsewhere"
	bundle.Configs[0].Name = "../Metadata name"
	plan, err := testLabPlanConfigImport(empty, testLabRequest{Bundle: bundle})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "existing.yml")
	os.WriteFile(sentinel, []byte("unchanged"), 0600)
	if err = testLabCommitConfigImport(dir, plan, func() error { return errors.New("fixture manifest failure") }); err == nil {
		t.Fatal("ignored persistence failure")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "existing.yml" {
		t.Fatal("partial import remained")
	}
	if err = testLabCommitConfigImport(dir, plan, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	for name := range plan.Content {
		if filepath.Base(name) != name {
			t.Fatal("unsafe path")
		}
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("import permissions", err)
		}
	}
}
func TestTestLabConfigImportReviewTracksInputAndLibrary(t *testing.T) {
	s := testLabFixture(t)
	s.configRoot = t.TempDir()
	bundle := &testLabConfigBundle{Format: testLabBundleFormat, Version: 1, Configs: []testLabBundledConfig{{Name: "Config", YAML: testLabFixtureConfig}}}
	preview, err := s.configAction(testLabRequest{Action: "config-import-preview", Bundle: bundle})
	if err != nil {
		t.Fatal(err)
	}
	req := testLabRequest{Action: "config-import", Bundle: bundle, Revision: preview.(testLabImportPreview).Revision, Confirm: "IMPORT COPIES"}
	bundle.Configs[0].YAML += "extra: value\n"
	if _, err = s.configAction(req); err == nil {
		t.Fatal("accepted changed YAML")
	}
	bundle.Configs[0].YAML = testLabFixtureConfig
	s.configAction(testLabRequest{Action: "folder-save", Name: "New folder"})
	if _, err = s.configAction(req); err == nil {
		t.Fatal("accepted changed library")
	}
	files, _ := filepath.Glob(filepath.Join(s.configRoot, "*.yml"))
	if len(files) != 0 {
		t.Fatal("stale import wrote YAML")
	}
}
func TestTestLabConfigImportCollisions(t *testing.T) {
	lib := testLabConfigLibrary{Version: 1, Files: []testLabConfigFile{{Name: "Config", Folder: ""}, {Name: "Config (imported 2)", Folder: ""}}, Folders: []testLabConfigFolder{{ID: "existing", Name: "Staging"}}}
	b := &testLabConfigBundle{Format: testLabBundleFormat, Version: 1, Folders: []testLabConfigFolder{{ID: "foreign", Name: "staging"}}, Configs: []testLabBundledConfig{{Name: "Config", YAML: testLabFixtureConfig}, {Name: "Config", YAML: testLabFixtureConfig}, {Name: "Config", Folder: "foreign", YAML: testLabFixtureConfig}}}
	plan, err := testLabPlanConfigImport(lib, testLabRequest{Bundle: b})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Preview.Renamed != 3 || plan.Library.Files[2].Name != "Config (imported 3)" || plan.Library.Files[3].Name != "Config (imported 4)" || plan.Library.Files[4].Name != "Config" {
		t.Fatalf("bad collisions: %+v", plan.Preview)
	}
	name := strings.Repeat("界", 33)
	used := map[string]bool{strings.ToLower(name): true}
	renamed := testLabImportedName(name, used)
	if !testLabConfigName(renamed) || len(renamed) > 100 {
		t.Fatal("invalid unicode rename")
	}
}
