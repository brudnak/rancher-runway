package test

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/cachelab"
	"go/ast"
	"go/build"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

var testLabSHA = regexp.MustCompile(`^[a-f0-9]{40}$`)
var testLabTag = regexp.MustCompile(`^[A-Za-z0-9_.]+$`)

func (s *testLabService) startCatalog(ref string) (any, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		ref = "main"
	}
	if len(ref) > 200 || strings.ContainsAny(ref, "\x00\r\n") {
		return nil, fmt.Errorf("enter a branch, tag, or commit")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy {
		return nil, fmt.Errorf("catalog refresh is already running")
	}
	if s.workers == nil {
		s.workers = &panelWorkers{}
	}
	parent, done, err := s.workers.Begin()
	if err != nil {
		return nil, err
	}
	s.busy = true
	s.catalogError = ""
	go func() {
		defer done()
		ctx, cancel := context.WithTimeout(parent, 3*time.Minute)
		defer cancel()
		catalog, err := s.fetchCatalog(ctx, ref)
		s.mu.Lock()
		defer s.mu.Unlock()
		s.busy = false
		if err != nil {
			s.catalogError = err.Error()
			return
		}
		previous := s.library.Catalog
		s.library.Catalog = catalog
		if err = s.persistLocked(); err != nil {
			s.library.Catalog = previous
			s.catalogError = err.Error()
		}
	}()
	return map[string]bool{"started": true}, nil
}
func (s *testLabService) publicGET(ctx context.Context, endpoint string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Rancher-Runway-Test-Lab")
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach the public test catalog")
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		return nil, fmt.Errorf("catalog request returned HTTP %d; check the ref or retry after GitHub's rate limit resets", resp.StatusCode)
	}
	return resp, nil
}
func (s *testLabService) fetchCatalog(ctx context.Context, ref string) (testLabCatalog, error) {
	var result testLabCatalog
	resp, err := s.publicGET(ctx, "https://api.github.com/repos/rancher/tests/commits/"+url.PathEscape(ref))
	if err != nil {
		return result, err
	}
	var commit struct {
		SHA string `json:"sha"`
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&commit)
	resp.Body.Close()
	if err != nil || !testLabSHA.MatchString(commit.SHA) {
		return result, fmt.Errorf("GitHub did not return a valid source revision")
	}
	archive := filepath.Join(s.root, commit.SHA+".tar.gz")
	if _, err = os.Stat(archive); os.IsNotExist(err) {
		resp, err = s.publicGET(ctx, "https://codeload.github.com/rancher/tests/tar.gz/"+commit.SHA)
		if err != nil {
			return result, err
		}
		defer resp.Body.Close()
		f, err := os.CreateTemp(s.root, ".archive-*")
		if err != nil {
			return result, err
		}
		defer os.Remove(f.Name())
		n, copyErr := io.Copy(f, io.LimitReader(resp.Body, 64<<20+1))
		closeErr := f.Close()
		if copyErr != nil || closeErr != nil || n > 64<<20 {
			return result, fmt.Errorf("test archive could not be downloaded within the 64 MiB limit")
		}
		if err = os.Rename(f.Name(), archive); err != nil {
			return result, err
		}
	}
	dir, err := os.MkdirTemp(s.root, ".catalog-*")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(dir)
	if err = testLabExtract(archive, dir); err != nil {
		return result, err
	}
	entries, err := testLabDiscover(dir)
	if err != nil {
		return result, err
	}
	if len(entries) == 0 {
		return result, fmt.Errorf("no static validation tests were found at this revision")
	}
	mod, _ := os.ReadFile(filepath.Join(dir, "go.mod"))
	version := ""
	for _, line := range strings.Split(string(mod), "\n") {
		if strings.HasPrefix(line, "go ") {
			version = strings.TrimSpace(strings.TrimPrefix(line, "go "))
			break
		}
	}
	return testLabCatalog{Ref: ref, SHA: commit.SHA, GoVersion: version, FetchedAt: time.Now(), Entries: entries}, nil
}
func testLabExtract(archive, dir string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("invalid test archive")
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var total int64
	count := 0
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("test archive is incomplete")
		}
		count++
		if count > 30000 {
			return fmt.Errorf("too many archive entries")
		}
		parts := strings.SplitN(h.Name, "/", 2)
		if len(parts) != 2 || parts[1] == "" {
			continue
		}
		name := parts[1]
		if path.IsAbs(name) || path.Clean(name) != strings.TrimSuffix(name, "/") || strings.Contains(name, "\\") || strings.HasPrefix(name, "../") {
			return fmt.Errorf("unsafe archive path")
		}
		// Repository metadata and links are never needed by the runner.
		if strings.HasPrefix(name, ".git/") || name == ".git" {
			continue
		}
		target := filepath.Join(dir, filepath.FromSlash(name))
		switch h.Typeflag {
		case tar.TypeDir:
			if err = os.MkdirAll(target, 0700); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			total += h.Size
			if h.Size < 0 || h.Size > 16<<20 || total > 256<<20 {
				return fmt.Errorf("expanded test archive is too large")
			}
			if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				return err
			}
			_, err = io.CopyN(out, tr, h.Size)
			closeErr := out.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
		case tar.TypeSymlink, tar.TypeLink:
			return fmt.Errorf("test archive contains a link; inspect this revision before using it")
		}
	}
	return nil
}
func testLabReceiver(expr ast.Expr) string {
	switch x := expr.(type) {
	case *ast.StarExpr:
		return testLabReceiver(x.X)
	case *ast.Ident:
		return x.Name
	case *ast.CompositeLit:
		return testLabReceiver(x.Type)
	case *ast.UnaryExpr:
		return testLabReceiver(x.X)
	case *ast.CallExpr:
		if ident, ok := x.Fun.(*ast.Ident); ok && ident.Name == "new" && len(x.Args) == 1 {
			return testLabReceiver(x.Args[0])
		}
	}
	return ""
}
func testLabDiscover(root string) ([]testLabEntry, error) {
	type parsed struct {
		entry    testLabEntry
		receiver string
		types    []string
	}
	var parsedTests []parsed
	err := filepath.WalkDir(filepath.Join(root, "validation"), func(file string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(file, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, file)
		src, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		fs := token.NewFileSet()
		node, err := parser.ParseFile(fs, file, src, parser.ParseComments)
		if err != nil {
			return fmt.Errorf("could not parse %s", rel)
		}
		build := ""
		for _, line := range strings.Split(string(src), "\n") {
			if strings.HasPrefix(line, "//go:build ") {
				build = strings.TrimPrefix(line, "//go:build ")
				break
			}
		}
		for _, decl := range node.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !strings.HasPrefix(fn.Name.Name, "Test") || fn.Name.Name == "TestMain" || fn.Body == nil {
				continue
			}
			row := parsed{entry: testLabEntry{Package: filepath.ToSlash(filepath.Dir(rel)), Test: fn.Name.Name, File: filepath.ToSlash(rel), Line: fs.Position(fn.Pos()).Line, Constraint: build}}
			if fn.Doc != nil {
				row.entry.Description = cachelab.Text(fn.Doc.Text(), 600)
			}
			if fn.Recv != nil {
				row.receiver = testLabReceiver(fn.Recv.List[0].Type)
				if len(fn.Type.Params.List) > 0 {
					continue
				}
			} else {
				// Top-level Go test functions take exactly one *testing.T parameter.
				if len(fn.Type.Params.List) != 1 {
					continue
				}
				star, ok := fn.Type.Params.List[0].Type.(*ast.StarExpr)
				if !ok {
					continue
				}
				sel, ok := star.X.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "T" {
					continue
				}
				locals := map[string]string{}
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					if assign, ok := n.(*ast.AssignStmt); ok {
						for i, left := range assign.Lhs {
							if id, ok := left.(*ast.Ident); ok && i < len(assign.Rhs) {
								locals[id.Name] = testLabReceiver(assign.Rhs[i])
							}
						}
					}
					if decl, ok := n.(*ast.ValueSpec); ok {
						for i, id := range decl.Names {
							if i < len(decl.Values) {
								locals[id.Name] = testLabReceiver(decl.Values[i])
							}
						}
					}
					return true
				})
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					selector, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || selector.Sel.Name != "Run" || len(call.Args) != 2 {
						return true
					}
					if typ := testLabReceiver(call.Args[1]); typ != "" {
						if resolved := locals[typ]; resolved != "" {
							typ = resolved
						}
						row.types = append(row.types, typ)
					}
					return true
				})
			}
			parsedTests = append(parsedTests, row)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	entries := []testLabEntry{}
	for _, row := range parsedTests {
		if row.receiver != "" {
			continue
		}
		suite := row.entry
		suite.Suite = suite.Test
		suite.Test = ""
		suite.ID = suite.Package + "::" + suite.Suite
		entries = append(entries, suite)
		for _, method := range parsedTests {
			if method.entry.Package != suite.Package {
				continue
			}
			found := false
			for _, typ := range row.types {
				found = found || typ == method.receiver
			}
			if !found || method.receiver == "" {
				continue
			}
			entry := method.entry
			entry.Suite = suite.Suite
			entry.ID = suite.ID + "/" + entry.Test
			if suite.Constraint != "" && entry.Constraint != suite.Constraint {
				if entry.Constraint == "" {
					entry.Constraint = suite.Constraint
				} else {
					entry.Constraint = "(" + suite.Constraint + ") && (" + entry.Constraint + ")"
				}
			}
			entries = append(entries, entry)
		}
	}
	indexes := map[string]int{}
	unique := []testLabEntry{}
	for _, e := range entries {
		if index, ok := indexes[e.ID]; ok {
			old := unique[index].Constraint
			if old == "" || e.Constraint == "" {
				unique[index].Constraint = ""
			} else if old != e.Constraint {
				unique[index].Constraint = "(" + old + ") || (" + e.Constraint + ")"
			}
			continue
		}
		indexes[e.ID] = len(unique)
		unique = append(unique, e)
	}
	sort.Slice(unique, func(i, j int) bool { return unique[i].ID < unique[j].ID })
	return unique, nil
}

type testLabCommand struct {
	Package  string   `json:"package"`
	Pattern  string   `json:"pattern"`
	Args     []string `json:"args"`
	Expected []string `json:"expected"`
}

func (s *testLabService) commandsLocked(req testLabRequest) ([]testLabCommand, error) {
	if !testLabSHA.MatchString(req.SHA) || req.SHA != s.library.Catalog.SHA {
		return nil, fmt.Errorf("catalog revision changed; review and select tests again")
	}
	if len(req.Selection) == 0 || len(req.Selection) > 500 {
		return nil, fmt.Errorf("choose between 1 and 500 catalog entries")
	}
	if req.Timeout < 1 || req.Timeout > 240 {
		return nil, fmt.Errorf("timeout must be between 1 and 240 minutes per suite")
	}
	tags := map[string]bool{}
	for _, tag := range strings.Split(req.Tags, ",") {
		tag = strings.TrimSpace(tag)
		if !testLabTag.MatchString(tag) {
			return nil, fmt.Errorf("build tags must be comma-separated names")
		}
		tags[tag] = true
	}
	tags["cgo"] = true
	tags["gc"] = true
	tags[runtime.GOOS] = true
	tags[runtime.GOARCH] = true
	for _, tag := range build.Default.ReleaseTags {
		tags[tag] = true
	}
	entries := map[string]testLabEntry{}
	for _, e := range s.library.Catalog.Entries {
		entries[e.ID] = e
	}
	selected := map[string]bool{}
	for _, id := range req.Selection {
		selected[id] = true
	}
	groups := map[string][]testLabEntry{}
	for _, id := range req.Selection {
		e, ok := entries[id]
		if !ok {
			return nil, fmt.Errorf("a selected test is no longer in this catalog")
		}
		if e.Test != "" && selected[e.Package+"::"+e.Suite] {
			continue
		}
		if e.Constraint != "" {
			expr, err := constraint.Parse("//go:build " + e.Constraint)
			if err != nil || !expr.Eval(func(tag string) bool { return tags[tag] }) {
				return nil, fmt.Errorf("%s is excluded by the chosen tags: %s", e.Test+e.Suite, e.Constraint)
			}
		}
		key := e.Package + "::" + e.Suite
		groups[key] = append(groups[key], e)
	}
	keys := []string{}
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	commands := []testLabCommand{}
	for _, key := range keys {
		rows := groups[key]
		pattern := "^" + regexp.QuoteMeta(rows[0].Suite) + "$"
		expected := []string{}
		if rows[0].Test != "" {
			methods := []string{}
			for _, e := range rows {
				methods = append(methods, regexp.QuoteMeta(e.Test))
				expected = append(expected, e.Suite+"/"+e.Test)
			}
			sort.Strings(methods)
			pattern += "/^(" + strings.Join(methods, "|") + ")$"
		} else {
			expected = append(expected, rows[0].Suite)
		}
		commands = append(commands, testLabCommand{Package: rows[0].Package, Pattern: pattern, Expected: expected, Args: []string{"test", "-buildvcs=false", "-json", "-count=1", "-p=2", "-parallel=1", "-tags", req.Tags, "-timeout", fmt.Sprintf("%dm", req.Timeout), "-run", pattern, "./" + rows[0].Package}})
	}
	return commands, nil
}
