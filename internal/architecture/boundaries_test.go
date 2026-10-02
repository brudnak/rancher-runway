package architecture_test

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// Services must remain independently usable; app state and HTTP are adapters.
func TestServiceImportBoundaries(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate architecture tests")
	}
	root := filepath.Dir(filepath.Dir(source))
	prefix := "github.com/brudnak/ha-rancher-rke2/"
	domains := []string{"imagelookup", "prbuild", "cachelab", "server", "workspace", "history", "operations", "awspricing", "localtools", "registrycatalog", "linodeinventory"}
	for _, domain := range domains {
		files, err := filepath.Glob(filepath.Join(root, domain, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		if len(files) == 0 {
			t.Fatalf("no source files for %s", domain)
		}
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, imp := range f.Imports {
				path, err := strconv.Unquote(imp.Path.Value)
				if err != nil {
					t.Fatal(err)
				}
				if strings.HasPrefix(path, prefix+"terratest") {
					t.Errorf("%s imports application package %s", file, path)
				}
				if domain != "server" && path == prefix+"internal/server" {
					t.Errorf("service %s imports its HTTP adapter", domain)
				}
				if domain == "imagelookup" && (path == prefix+"internal/prbuild" || path == prefix+"internal/cachelab") {
					t.Errorf("image lookup imports higher-level service %s", path)
				}
			}
		}
	}
}
