package test

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTestLabSourceFilePinnedAPI(t *testing.T) {
	s := testLabFixture(t)
	name := "validation/steve/vai/vai_test.go"
	content := "package vai\n\nfunc init() { panic(\"source must never execute\") }\n"
	testLabArchive(t, filepath.Join(s.root, testLabFixtureSHA+".tar.gz"), map[string]string{name: content})
	p := &localControlPanel{testLab: s, token: "test-panel-token"}
	body, _ := json.Marshal(testLabRequest{Action: "source-file", SHA: testLabFixtureSHA, Path: name})
	req := httptest.NewRequest("POST", "http://localhost/api/test-lab", strings.NewReader(string(body)))
	req.Header.Set("X-Control-Panel-Token", "test-panel-token")
	w := httptest.NewRecorder()
	p.handleTestLab(w, req)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("source read failed: %d %s", w.Code, w.Body.String())
	}
	var got map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["sha"] != testLabFixtureSHA || got["path"] != name || got["content"] != content || got["language"] != "go" {
		t.Fatalf("source identity or content changed: %#v", got)
	}
	if _, err := s.sourceAction(testLabRequest{Action: "source-file", SHA: strings.Repeat("a", 40), Path: name}); err == nil {
		t.Fatal("accepted a revision other than the displayed catalog")
	}
	if _, err := s.sourceAction(testLabRequest{Action: "source-file", SHA: testLabFixtureSHA, Path: "validation/steve/missing.go"}); err == nil {
		t.Fatal("read a missing source file")
	}
	entries, err := os.ReadDir(s.root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatalf("source viewer unexpectedly extracted source: %s", entry.Name())
		}
	}
}

func TestTestLabSourceFilePathBoundaries(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "source.tar.gz")
	testLabArchive(t, archive, map[string]string{"validation/example/test.go": "package example", "go.mod": "module example"})
	for _, name := range []string{"", "/validation/example/test.go", "../validation/example/test.go", "validation/../go.mod", "validation/example/../example/test.go", "validation//example/test.go", "validation\\example\\test.go", "validation/example/test.go\x00", "validation/example/test.go\n", "validation/example/README.md", "actions/example/config.go", "go.mod", "validation/example/test.go/", "validation/" + strings.Repeat("a", 1024) + ".go"} {
		t.Run(name, func(t *testing.T) {
			if _, err := testLabReadSourceFile(archive, name); err == nil {
				t.Fatalf("accepted out-of-scope source path %q", name)
			}
		})
	}
	link := filepath.Join(t.TempDir(), "linked.tar.gz")
	if err := os.Symlink(archive, link); err != nil {
		t.Fatal(err)
	}
	if _, err := testLabReadSourceFile(link, "validation/example/test.go"); err == nil {
		t.Fatal("followed an archive symlink")
	}
}

func TestTestLabSourceFileArchiveBoundaries(t *testing.T) {
	name := "validation/example/test.go"
	for _, scenario := range []string{"traversal", "absolute", "backslash", "symlink", "hardlink", "duplicate", "multiple-roots", "directory", "oversized", "invalid-utf8", "nul"} {
		t.Run(scenario, func(t *testing.T) {
			archive := filepath.Join(t.TempDir(), "source.tar.gz")
			f, err := os.Create(archive)
			if err != nil {
				t.Fatal(err)
			}
			gz := gzip.NewWriter(f)
			tw := tar.NewWriter(gz)
			write := func(header tar.Header, content string) {
				t.Helper()
				header.Mode = 0600
				header.Size = int64(len(content))
				if err := tw.WriteHeader(&header); err != nil {
					t.Fatal(err)
				}
				if _, err := tw.Write([]byte(content)); err != nil {
					t.Fatal(err)
				}
			}
			header := tar.Header{Name: "tests-fixture/" + name, Typeflag: tar.TypeReg}
			content := "package example\n"
			switch scenario {
			case "traversal":
				header.Name = "tests-fixture/validation/../" + name
			case "absolute":
				header.Name = "/tests-fixture/" + name
			case "backslash":
				header.Name = "tests-fixture/validation\\example/test.go"
			case "symlink":
				header.Typeflag, header.Linkname, content = tar.TypeSymlink, "/etc/passwd", ""
			case "hardlink":
				header.Typeflag, header.Linkname, content = tar.TypeLink, "../outside.go", ""
			case "directory":
				header.Typeflag, content = tar.TypeDir, ""
			case "oversized":
				content = strings.Repeat("x", 2<<20+1)
			case "invalid-utf8":
				content = "package example\n\xff"
			case "nul":
				content = "package example\n\x00"
			}
			write(header, content)
			if scenario == "duplicate" {
				write(header, content)
			}
			if scenario == "multiple-roots" {
				header.Name = "other-source/" + name
				write(header, content)
			}
			if err := tw.Close(); err != nil {
				t.Fatal(err)
			}
			if err := gz.Close(); err != nil {
				t.Fatal(err)
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := testLabReadSourceFile(archive, name); err == nil {
				t.Fatalf("accepted %s archive", scenario)
			}
		})
	}
}
