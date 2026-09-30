package test

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type testLabConfigFolder struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type testLabConfigFile struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Folder    string    `json:"folder"`
	Revision  string    `json:"revision"`
	UpdatedAt time.Time `json:"updatedAt"`
}
type testLabConfigLibrary struct {
	Version int                   `json:"version"`
	Folders []testLabConfigFolder `json:"folders"`
	Files   []testLabConfigFile   `json:"files"`
}

// Config templates intentionally live outside any Runway checkout. Folder names
// are metadata, never filesystem paths; config contents never enter the manifest.
func (s *testLabService) configDirectory() (string, error) {
	if s.configRoot != "" {
		return s.configRoot, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "Rancher Runway", "Test Lab", "configs"), nil
}
func testLabYAML(raw string) (map[string]any, error) {
	if len(raw) > 128<<10 {
		return nil, fmt.Errorf("configuration exceeds 128 KiB")
	}
	var value map[string]any
	d := yaml.NewDecoder(strings.NewReader(raw))
	if err := d.Decode(&value); err != nil || value == nil {
		return nil, fmt.Errorf("use a valid YAML mapping; check indentation, duplicate keys, and value types")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return nil, fmt.Errorf("use a single YAML document")
	}
	return value, nil
}
func testLabPrivateRead(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("invalid or oversized workspace file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("workspace file exceeds size limit")
	}
	return b, err
}
func testLabAtomicFile(dir, name string, b []byte) error {
	f, err := os.CreateTemp(dir, ".config-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(dir, name))
}
func (s *testLabService) configAction(req testLabRequest) (any, error) {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	root, err := s.configDirectory()
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("config library must be a private directory")
	}
	if err = os.Chmod(root, 0700); err != nil {
		return nil, err
	}
	lib := testLabConfigLibrary{Version: 1, Folders: []testLabConfigFolder{}, Files: []testLabConfigFile{}}
	b, err := testLabPrivateRead(filepath.Join(root, "index.json"), 2<<20)
	if err == nil {
		if json.Unmarshal(b, &lib) != nil || lib.Version != 1 {
			return nil, fmt.Errorf("config library cannot be read; existing files were preserved")
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	for _, f := range lib.Files {
		if !cacheLabIDPattern.MatchString(f.ID) || !cacheLabIDPattern.MatchString(f.Revision) {
			return nil, fmt.Errorf("invalid config library entry")
		}
	}
	for _, f := range lib.Folders {
		if !cacheLabIDPattern.MatchString(f.ID) {
			return nil, fmt.Errorf("invalid config folder")
		}
	}
	save := func() error {
		b, e := json.MarshalIndent(lib, "", "  ")
		if e != nil {
			return e
		}
		return testLabAtomicFile(root, "index.json", b)
	}
	filename := func(f testLabConfigFile) string { return f.ID + "-" + f.Revision + ".yml" }
	index := -1
	for i, f := range lib.Files {
		if f.ID == req.ID {
			index = i
			break
		}
	}
	folderExists := req.Folder == ""
	for _, f := range lib.Folders {
		if f.ID == req.Folder {
			folderExists = true
		}
	}
	name := strings.TrimSpace(req.Name)
	validName := func() error {
		if name == "" || len(name) > 100 || strings.ContainsAny(name, "\x00\r\n") {
			return fmt.Errorf("use a name between 1 and 100 characters")
		}
		return nil
	}
	switch req.Action {
	case "config-export":
		if req.Confirm != "EXPORT WITH CREDENTIALS" {
			return nil, fmt.Errorf("acknowledge that the export includes all configuration values, including credentials")
		}
		raw, name, err := testLabBuildConfigExport(root, lib, req.Scope, req.ID)
		if err != nil {
			return nil, err
		}
		return s.saveConfigExport(raw, name)
	case "config-import-preview", "config-import":
		plan, err := testLabPlanConfigImport(lib, req)
		if err != nil {
			return nil, err
		}
		if req.Action == "config-import-preview" {
			return plan.Preview, nil
		}
		if req.Confirm != "IMPORT COPIES" || req.Revision != plan.Preview.Revision {
			return nil, fmt.Errorf("the library or import changed; preview the import again before saving copies")
		}
		lib = plan.Library
		if err = testLabCommitConfigImport(root, plan, save); err != nil {
			return nil, err
		}
		return plan.Preview, nil
	case "config-library":
		return map[string]any{"library": lib, "path": root}, nil
	case "folder-save":
		if err = validName(); err != nil {
			return nil, err
		}
		for _, f := range lib.Folders {
			if strings.EqualFold(f.Name, name) && f.ID != req.ID {
				return nil, fmt.Errorf("a folder with that name already exists")
			}
		}
		if req.ID == "" {
			if len(lib.Folders) >= 100 {
				return nil, fmt.Errorf("folder limit reached")
			}
			lib.Folders = append(lib.Folders, testLabConfigFolder{ID: cacheLabID(), Name: name})
		} else {
			found := false
			for i, f := range lib.Folders {
				if f.ID == req.ID {
					lib.Folders[i].Name = name
					found = true
				}
			}
			if !found {
				return nil, fmt.Errorf("folder not found")
			}
		}
		if err = save(); err != nil {
			return nil, err
		}
		return lib, nil
	case "folder-delete":
		if req.Confirm != typedConfirmationPhrase {
			return nil, fmt.Errorf("type confirm to remove this empty folder")
		}
		for _, f := range lib.Files {
			if f.Folder == req.ID {
				return nil, fmt.Errorf("move or delete the configs in this folder first")
			}
		}
		found := false
		for i, f := range lib.Folders {
			if f.ID == req.ID {
				lib.Folders = append(lib.Folders[:i], lib.Folders[i+1:]...)
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("folder not found")
		}
		if err = save(); err != nil {
			return nil, err
		}
		return lib, nil
	case "config-load":
		if index < 0 {
			return nil, fmt.Errorf("saved config not found")
		}
		f := lib.Files[index]
		b, err := testLabPrivateRead(filepath.Join(root, filename(f)), 128<<10)
		if err != nil {
			return nil, fmt.Errorf("saved YAML cannot be read; existing files were preserved")
		}
		return map[string]any{"file": f, "config": string(b)}, nil
	case "config-save":
		if err = validName(); err != nil {
			return nil, err
		}
		if !folderExists {
			return nil, fmt.Errorf("folder no longer exists")
		}
		if _, err = testLabYAML(req.Config); err != nil {
			return nil, err
		}
		for _, f := range lib.Files {
			if f.ID != req.ID && f.Folder == req.Folder && strings.EqualFold(f.Name, name) {
				return nil, fmt.Errorf("a config with that name already exists in this folder")
			}
		}
		var old testLabConfigFile
		if req.ID != "" {
			if index < 0 {
				return nil, fmt.Errorf("saved config no longer exists")
			}
			old = lib.Files[index]
			if req.Revision != old.Revision {
				return nil, fmt.Errorf("this template changed since it was opened; reopen it or save a new copy")
			}
			if req.Confirm != "UPDATE TEMPLATE" {
				return nil, fmt.Errorf("confirm UPDATE TEMPLATE to replace a saved template")
			}
		} else if len(lib.Files) >= 500 {
			return nil, fmt.Errorf("saved config limit reached")
		}
		f := testLabConfigFile{ID: req.ID, Name: name, Folder: req.Folder, Revision: cacheLabID(), UpdatedAt: time.Now()}
		if f.ID == "" {
			f.ID = cacheLabID()
		}
		if err = testLabAtomicFile(root, filename(f), []byte(req.Config)); err != nil {
			return nil, err
		}
		if index >= 0 {
			lib.Files[index] = f
		} else {
			lib.Files = append(lib.Files, f)
		}
		if err = save(); err != nil {
			os.Remove(filepath.Join(root, filename(f)))
			return nil, err
		}
		if old.ID != "" {
			if err = os.Remove(filepath.Join(root, filename(old))); err != nil && !os.IsNotExist(err) {
				return nil, fmt.Errorf("template saved, but the previous YAML could not be removed from the config directory")
			}
		}
		return f, nil
	case "config-delete":
		if index < 0 {
			return nil, fmt.Errorf("saved config not found")
		}
		f := lib.Files[index]
		if req.Confirm != typedConfirmationPhrase || req.Revision != f.Revision {
			return nil, fmt.Errorf("reload this config and type confirm to delete it")
		}
		// Remove the bytes first: a failed manifest write must not retain credentials
		// while reporting successful cleanup. A retry handles a missing file.
		if err = os.Remove(filepath.Join(root, filename(f))); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		lib.Files = append(lib.Files[:index], lib.Files[index+1:]...)
		if err = save(); err != nil {
			return nil, err
		}
		return map[string]bool{"ok": true}, nil
	}
	return nil, fmt.Errorf("unknown config library action")
}
