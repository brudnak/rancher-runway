package test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const testLabBundleFormat = "rancher-runway/cattle-configs"
const testLabBundleLimit = 80 << 20

type testLabBundledConfig struct {
	Name   string `json:"name"`
	Folder string `json:"folder"`
	YAML   string `json:"yaml"`
}
type testLabConfigBundle struct {
	Format     string                 `json:"format"`
	Version    int                    `json:"version"`
	ExportedAt time.Time              `json:"exportedAt,omitempty"`
	Folders    []testLabConfigFolder  `json:"folders"`
	Configs    []testLabBundledConfig `json:"configs"`
}
type testLabImportEntry struct {
	Kind     string `json:"kind"`
	Original string `json:"original"`
	Name     string `json:"name"`
	Folder   string `json:"folder,omitempty"`
	Renamed  bool   `json:"renamed"`
}
type testLabImportPreview struct {
	Revision string               `json:"revision"`
	Folders  int                  `json:"folders"`
	Configs  int                  `json:"configs"`
	Renamed  int                  `json:"renamed"`
	Entries  []testLabImportEntry `json:"entries"`
}
type testLabImportPlan struct {
	Preview testLabImportPreview
	Library testLabConfigLibrary
	Content map[string]string
}

func testLabConfigName(name string) bool {
	return strings.TrimSpace(name) == name && name != "" && len(name) <= 100 && utf8.ValidString(name) && !strings.ContainsAny(name, "\x00\r\n\t")
}
func testLabImportedName(name string, used map[string]bool) string {
	candidate := name
	for n := 2; used[strings.ToLower(candidate)]; n++ {
		suffix := fmt.Sprintf(" (imported %d)", n)
		base := name
		for len(base)+len(suffix) > 100 {
			_, size := utf8.DecodeLastRuneInString(base)
			base = base[:len(base)-size]
		}
		candidate = base + suffix
	}
	used[strings.ToLower(candidate)] = true
	return candidate
}
func testLabPlanConfigImport(lib testLabConfigLibrary, req testLabRequest) (testLabImportPlan, error) {
	plan := testLabImportPlan{Library: testLabConfigLibrary{Version: 1, Folders: append([]testLabConfigFolder{}, lib.Folders...), Files: append([]testLabConfigFile{}, lib.Files...)}, Content: map[string]string{}, Preview: testLabImportPreview{Entries: []testLabImportEntry{}}}
	b := req.Bundle
	if b == nil || b.Format != testLabBundleFormat || b.Version != 1 {
		return plan, fmt.Errorf("choose a Runway config bundle (format version 1) or a YAML file")
	}
	if len(b.Configs) > 500 || len(b.Folders) > 100 || len(b.Configs)+len(b.Folders) == 0 {
		return plan, fmt.Errorf("bundle must contain 1–500 configs or up to 100 folders")
	}
	if len(lib.Files)+len(b.Configs) > 500 || len(lib.Folders)+len(b.Folders) > 100 {
		return plan, fmt.Errorf("import would exceed the library limit of 500 configs or 100 folders")
	}
	folderExists := req.Folder == ""
	for _, f := range lib.Folders {
		if f.ID == req.Folder {
			folderExists = true
		}
	}
	if !folderExists {
		return plan, fmt.Errorf("destination folder no longer exists")
	}
	// A fingerprint binds the review to both the exact input and current library.
	fingerprint, _ := json.Marshal(struct {
		Library testLabConfigLibrary
		Bundle  *testLabConfigBundle
		Folder  string
	}{lib, b, req.Folder})
	if len(fingerprint) > testLabBundleLimit {
		return plan, fmt.Errorf("bundle exceeds 80 MiB; import smaller folders")
	}
	hash := sha256.Sum256(fingerprint)
	plan.Preview.Revision = hex.EncodeToString(hash[:])
	usedFolders := map[string]bool{}
	folderMap := map[string]string{}
	folderLabels := map[string]string{"": "Library root"}
	for _, f := range lib.Folders {
		usedFolders[strings.ToLower(f.Name)] = true
		folderLabels[f.ID] = f.Name
	}
	for _, f := range b.Folders {
		if !testLabConfigName(f.Name) || f.ID == "" || len(f.ID) > 100 {
			return plan, fmt.Errorf("bundle contains an invalid folder")
		}
		if _, ok := folderMap[f.ID]; ok {
			return plan, fmt.Errorf("bundle contains duplicate folder identifiers")
		}
		name := testLabImportedName(f.Name, usedFolders)
		id := cacheLabID()
		folderMap[f.ID] = id
		folderLabels[id] = name
		plan.Library.Folders = append(plan.Library.Folders, testLabConfigFolder{ID: id, Name: name})
		plan.Preview.Entries = append(plan.Preview.Entries, testLabImportEntry{Kind: "folder", Original: f.Name, Name: name, Renamed: name != f.Name})
		plan.Preview.Folders++
	}
	usedFiles := map[string]map[string]bool{}
	for _, f := range lib.Files {
		if usedFiles[f.Folder] == nil {
			usedFiles[f.Folder] = map[string]bool{}
		}
		usedFiles[f.Folder][strings.ToLower(f.Name)] = true
	}
	for idx, f := range b.Configs {
		if !testLabConfigName(f.Name) {
			return plan, fmt.Errorf("config %d has an invalid name", idx+1)
		}
		if _, err := testLabYAML(f.YAML); err != nil {
			return plan, fmt.Errorf("config %d: %w", idx+1, err)
		}
		folder := req.Folder
		if f.Folder != "" {
			var ok bool
			folder, ok = folderMap[f.Folder]
			if !ok {
				return plan, fmt.Errorf("config %d refers to a folder missing from the bundle", idx+1)
			}
		}
		if usedFiles[folder] == nil {
			usedFiles[folder] = map[string]bool{}
		}
		name := testLabImportedName(f.Name, usedFiles[folder])
		saved := testLabConfigFile{ID: cacheLabID(), Revision: cacheLabID(), Name: name, Folder: folder, UpdatedAt: time.Now()}
		plan.Library.Files = append(plan.Library.Files, saved)
		plan.Content[saved.ID+"-"+saved.Revision+".yml"] = f.YAML
		plan.Preview.Entries = append(plan.Preview.Entries, testLabImportEntry{Kind: "config", Original: f.Name, Name: name, Folder: folderLabels[folder], Renamed: name != f.Name})
		plan.Preview.Configs++
	}
	for _, entry := range plan.Preview.Entries {
		if entry.Renamed {
			plan.Preview.Renamed++
		}
	}
	return plan, nil
}

// Publish all metadata in one atomic write, or remove every newly staged file.
// Existing configs are never rewritten, including when a bundle has collisions.
func testLabCommitConfigImport(root string, plan testLabImportPlan, persist func() error) error {
	written := []string{}
	ok := false
	defer func() {
		if !ok {
			for _, name := range written {
				_ = os.Remove(filepath.Join(root, name))
			}
		}
	}()
	for name, content := range plan.Content {
		if err := testLabAtomicFile(root, name, []byte(content)); err != nil {
			return err
		}
		written = append(written, name)
	}
	if err := persist(); err != nil {
		return err
	}
	ok = true
	return nil
}
func testLabBuildConfigExport(root string, lib testLabConfigLibrary, scope, id string) ([]byte, string, error) {
	name := "runway-cattle-configs"
	bundle := testLabConfigBundle{Format: testLabBundleFormat, Version: 1, ExportedAt: time.Now(), Folders: []testLabConfigFolder{}, Configs: []testLabBundledConfig{}}
	found := scope == "library"
	switch scope {
	case "library":
		bundle.Folders = append(bundle.Folders, lib.Folders...)
	case "folder":
		for _, f := range lib.Folders {
			if f.ID == id {
				bundle.Folders = append(bundle.Folders, f)
				name = f.Name
				found = true
				break
			}
		}
	case "config":
		for _, f := range lib.Files {
			if f.ID == id {
				raw, err := testLabPrivateRead(filepath.Join(root, f.ID+"-"+f.Revision+".yml"), 128<<10)
				label := f.Name
				for _, extension := range []string{".yaml", ".yml"} {
					if strings.HasSuffix(strings.ToLower(label), extension) {
						label = label[:len(label)-len(extension)]
						break
					}
				}
				name := cacheLabFileLabel(label)
				if name == "snapshot" {
					name = "cattle-config"
				}
				return raw, name + ".yml", err
			}
		}
	default:
		return nil, "", fmt.Errorf("choose a config, folder, or the entire library")
	}
	if !found {
		return nil, "", fmt.Errorf("export selection no longer exists")
	}
	for _, f := range lib.Files {
		if scope == "folder" && f.Folder != id {
			continue
		}
		raw, err := testLabPrivateRead(filepath.Join(root, f.ID+"-"+f.Revision+".yml"), 128<<10)
		if err != nil {
			return nil, "", fmt.Errorf("could not read every selected config; no export was written")
		}
		bundle.Configs = append(bundle.Configs, testLabBundledConfig{Name: f.Name, Folder: f.Folder, YAML: string(raw)})
	}
	if len(bundle.Folders)+len(bundle.Configs) == 0 {
		return nil, "", fmt.Errorf("add a cattle-config or folder before exporting the library")
	}
	raw, err := json.MarshalIndent(bundle, "", "  ")
	if len(raw) > testLabBundleLimit {
		return nil, "", fmt.Errorf("export exceeds 80 MiB; export smaller folders")
	}
	filename := cacheLabFileLabel(name) + ".runway-cattle-configs.json"
	if scope == "library" {
		filename = "runway-cattle-configs.json"
	}
	return raw, filename, err
}
func (s *testLabService) saveConfigExport(raw []byte, name string) (any, error) {
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
	// O_EXCL avoids overwriting a download or following an existing symlink.
	f, err := os.OpenFile(filepath.Join(directory, cacheLabID()[:8]+"-"+name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(f.Name())
		return nil, err
	}
	return map[string]any{"path": f.Name(), "filename": filepath.Base(f.Name()), "bytes": len(raw)}, nil
}
