package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareRuntimeDataExcludesCachedModules(t *testing.T) {
	requireBash(t)
	repo := t.TempDir()
	files := map[string]string{
		filepath.Join(repo, "go.mod"):  "module example.invalid/runway\n\ngo 1.20\n",
		filepath.Join(repo, "main.go"): "package runway\n",
	}
	for _, output := range []string{"automation-output", "terratest/automation-output"} {
		root := filepath.Join(repo, output)
		files[filepath.Join(root, "control-panel", "test-lab", "go-modules", "example.invalid", "legacy@v1.0.0", "source.go")] = "package legacy\n"
		files[filepath.Join(root, "saved-result.json")] = "{\"result\":\"keep\"}\n"
	}
	for path, content := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for attempt := 0; attempt < 2; attempt++ {
		cmd := exec.Command("bash", "prepare-runtime-data.sh", repo)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("prepare runtime data: %v\n%s", err, output)
		}
		list := exec.Command("go", "list", "-buildvcs=false", "./...")
		list.Dir = repo
		list.Env = append(os.Environ(), "GOFLAGS=-buildvcs=false", "GOWORK=off", "GOENV=off", "GOTOOLCHAIN=local", "GOPROXY=off", "GOVCS=*:off")
		output, err := list.CombinedOutput()
		if err != nil || strings.TrimSpace(string(output)) != "example.invalid/runway" {
			t.Fatalf("cached modules entered package discovery: %v\n%s", err, output)
		}
		for path, content := range files {
			if data, err := os.ReadFile(path); err != nil || string(data) != content {
				t.Fatalf("existing file changed: %s (%v)", path, err)
			}
		}
		// Existing boundaries belong to their workspace; repeated preparation
		// must preserve them as well as the cache and result contents.
		boundary := filepath.Join(repo, "automation-output", "go.mod")
		if attempt == 0 {
			files[boundary] = "module example.invalid/custom-runtime-data\n"
			if err := os.WriteFile(boundary, []byte(files[boundary]), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
}
