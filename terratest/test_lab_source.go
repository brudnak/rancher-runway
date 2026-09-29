package test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

type testLabReadme struct {
	Path    string `json:"path"`
	Title   string `json:"title"`
	Content string `json:"content"`
}
type testLabSourceFile struct {
	tree      *ast.File
	pkg, name string
	imports   map[string]string
}
type testLabDefinition struct {
	expr ast.Expr
	file *testLabSourceFile
}
type testLabSourceIndex struct {
	SHA       string
	Docs      []testLabReadme
	files     []*testLabSourceFile
	types     map[string]testLabDefinition
	constants map[string]testLabDefinition
	positions *token.FileSet
}
type testLabConfigFinding struct {
	Level   string `json:"level"`
	Path    string `json:"path"`
	Message string `json:"message"`
	File    string `json:"file,omitempty"`
	Line    int    `json:"line,omitempty"`
}
type testLabPreflight struct {
	SHA        string                 `json:"sha"`
	Findings   []testLabConfigFinding `json:"findings"`
	Sections   []string               `json:"sections"`
	Packages   []string               `json:"packages"`
	Readmes    []string               `json:"readmes"`
	Unresolved int                    `json:"unresolved"`
	Coverage   string                 `json:"coverage"`
}

func (s *testLabService) sourceAction(req testLabRequest) (any, error) {
	s.mu.Lock()
	sha := s.library.Catalog.SHA
	entries := append([]testLabEntry(nil), s.library.Catalog.Entries...)
	s.mu.Unlock()
	if !testLabSHA.MatchString(req.SHA) || req.SHA != sha {
		return nil, fmt.Errorf("load the selected catalog revision before reading its documentation or checking configuration")
	}
	s.sourceMu.Lock()
	defer s.sourceMu.Unlock()
	if s.sourceIndex == nil || s.sourceIndex.SHA != sha {
		dir, err := os.MkdirTemp(s.root, ".source-review-*")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(dir)
		if err = testLabExtract(filepath.Join(s.root, sha+".tar.gz"), dir); err != nil {
			return nil, fmt.Errorf("cached test source is unavailable; refresh the catalog")
		}
		index, err := testLabIndexSource(dir, sha)
		if err != nil {
			return nil, err
		}
		s.sourceIndex = index
	}
	if req.Action == "source-docs" {
		return map[string]any{"sha": sha, "documents": s.sourceIndex.Docs}, nil
	}
	packages := map[string]bool{}
	for _, id := range req.Selection {
		found := false
		for _, e := range entries {
			if e.ID == id {
				packages[e.Package] = true
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("selection is no longer in this catalog")
		}
	}
	return s.sourceIndex.check(req.Config, packages), nil
}
func testLabIndexSource(root, sha string) (*testLabSourceIndex, error) {
	index := &testLabSourceIndex{SHA: sha, Docs: []testLabReadme{}, types: map[string]testLabDefinition{}, constants: map[string]testLabDefinition{}, positions: token.NewFileSet()}
	totalDocs := 0
	err := filepath.WalkDir(root, func(name string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		relative, _ := filepath.Rel(root, name)
		relative = filepath.ToSlash(relative)
		base := strings.ToLower(d.Name())
		if strings.HasPrefix(relative, "validation/") && (base == "readme" || strings.HasPrefix(base, "readme.")) {
			b, err := testLabPrivateRead(name, 512<<10)
			if err != nil {
				return fmt.Errorf("cannot read documentation %s", relative)
			}
			totalDocs += len(b)
			if totalDocs > 12<<20 {
				return fmt.Errorf("documentation exceeds workspace limit")
			}
			title := relative
			for _, line := range strings.Split(string(b), "\n") {
				if strings.HasPrefix(line, "# ") {
					title = strings.TrimSpace(strings.TrimPrefix(line, "# "))
					break
				}
			}
			index.Docs = append(index.Docs, testLabReadme{Path: relative, Title: title, Content: string(b)})
		}
		if !strings.HasSuffix(relative, ".go") {
			return nil
		}
		tree, err := parser.ParseFile(index.positions, name, nil, 0)
		if err != nil {
			return nil
		}
		f := &testLabSourceFile{tree: tree, pkg: path.Dir(relative), name: relative, imports: map[string]string{}}
		for _, i := range tree.Imports {
			p, _ := strconv.Unquote(i.Path.Value)
			alias := path.Base(p)
			if i.Name != nil {
				alias = i.Name.Name
			}
			f.imports[alias] = strings.TrimPrefix(p, "github.com/rancher/tests/")
		}
		index.files = append(index.files, f)
		for _, decl := range tree.Decls {
			g, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range g.Specs {
				switch x := spec.(type) {
				case *ast.TypeSpec:
					index.types[f.pkg+"."+x.Name.Name] = testLabDefinition{x.Type, f}
				case *ast.ValueSpec:
					if g.Tok == token.CONST {
						for j, n := range x.Names {
							if j < len(x.Values) {
								index.constants[f.pkg+"."+n.Name] = testLabDefinition{x.Values[j], f}
							}
						}
					}
				}
			}
		}
		return nil
	})
	return index, err
}
func sourceSymbol(e ast.Expr, f *testLabSourceFile) string {
	switch x := e.(type) {
	case *ast.Ident:
		return f.pkg + "." + x.Name
	case *ast.SelectorExpr:
		if p, ok := x.X.(*ast.Ident); ok {
			return f.imports[p.Name] + "." + x.Sel.Name
		}
	case *ast.StarExpr:
		return sourceSymbol(x.X, f)
	case *ast.UnaryExpr:
		return sourceSymbol(x.X, f)
	}
	return ""
}
func (i *testLabSourceIndex) constant(e ast.Expr, f *testLabSourceFile, depth int) string {
	if depth > 10 {
		return ""
	}
	if x, ok := e.(*ast.BasicLit); ok && x.Kind == token.STRING {
		s, _ := strconv.Unquote(x.Value)
		return s
	}
	if d, ok := i.constants[sourceSymbol(e, f)]; ok {
		return i.constant(d.expr, d.file, depth+1)
	}
	return ""
}
func sourceVariable(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.UnaryExpr:
		return sourceVariable(x.X)
	case *ast.SelectorExpr:
		return sourceVariable(x.X) + "." + x.Sel.Name
	}
	return ""
}
func sourceNewType(e ast.Expr) ast.Expr {
	switch x := e.(type) {
	case *ast.UnaryExpr:
		return sourceNewType(x.X)
	case *ast.CompositeLit:
		return x.Type
	case *ast.CallExpr:
		if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "new" && len(x.Args) == 1 {
			return x.Args[0]
		}
	}
	return nil
}
func sourceFieldName(f *ast.Field) string {
	if f.Tag != nil {
		tag, _ := strconv.Unquote(f.Tag.Value)
		for _, kind := range []string{"yaml", "json"} {
			name := strings.Split(reflect.StructTag(tag).Get(kind), ",")[0]
			if name != "" {
				return name
			}
		}
	}
	if len(f.Names) > 0 {
		return strings.ToLower(f.Names[0].Name)
	}
	return ""
}
func (i *testLabSourceIndex) resolve(e ast.Expr, f *testLabSourceFile, depth int) (ast.Expr, *testLabSourceFile) {
	if depth > 12 {
		return nil, f
	}
	if p, ok := e.(*ast.StarExpr); ok {
		return i.resolve(p.X, f, depth+1)
	}
	if d, ok := i.types[sourceSymbol(e, f)]; ok {
		return i.resolve(d.expr, d.file, depth+1)
	}
	return e, f
}
func (i *testLabSourceIndex) check(raw string, packages map[string]bool) testLabPreflight {
	out := testLabPreflight{SHA: i.SHA, Findings: []testLabConfigFinding{}, Sections: []string{}, Packages: []string{}, Readmes: []string{}, Coverage: "Static review of config loads in selected packages, including suite setup and other tests in those packages. Missing fields may be conditional or supplied by defaults. External types, helper call graphs, runtime values, and provider access cannot be fully checked. This is not a guarantee that tests will pass."}
	add := func(level, key, msg string, f *testLabSourceFile, pos token.Pos) {
		finding := testLabConfigFinding{Level: level, Path: key, Message: msg}
		if f != nil {
			finding.File = f.name
			finding.Line = i.positions.Position(pos).Line
		}
		out.Findings = append(out.Findings, finding)
	}
	cfg, err := testLabYAML(raw)
	if err != nil {
		add("error", "cattle-config.yml", err.Error(), nil, 0)
		return out
	}
	if _, _, err = testLabConfig(raw); err != nil {
		add("error", "rancher", err.Error(), nil, 0)
	}
	for p := range packages {
		out.Packages = append(out.Packages, p)
	}
	sort.Strings(out.Packages)
	for _, doc := range i.Docs {
		for p := range packages {
			dir := path.Dir(doc.Path)
			if dir == p || strings.HasPrefix(p, dir+"/") {
				out.Readmes = append(out.Readmes, doc.Path)
				break
			}
		}
	}
	sections := map[string]bool{}
	seen := map[string]bool{}
	for _, f := range i.files {
		if !packages[f.pkg] {
			continue
		}
		for _, decl := range f.tree.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			vars := map[string]ast.Expr{}
			if fn.Recv != nil && len(fn.Recv.List) > 0 && len(fn.Recv.List[0].Names) > 0 {
				r := fn.Recv.List[0]
				e, rf := i.resolve(r.Type, f, 0)
				if st, ok := e.(*ast.StructType); ok && rf.pkg == f.pkg {
					for _, field := range st.Fields.List {
						for _, n := range field.Names {
							vars[r.Names[0].Name+"."+n.Name] = field.Type
						}
					}
				}
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.ValueSpec:
					for j, v := range x.Names {
						if x.Type != nil {
							vars[v.Name] = x.Type
						} else if j < len(x.Values) {
							vars[v.Name] = sourceNewType(x.Values[j])
						}
					}
				case *ast.AssignStmt:
					for j, v := range x.Lhs {
						if j < len(x.Rhs) {
							if typ := sourceNewType(x.Rhs[j]); typ != nil {
								vars[sourceVariable(v)] = typ
							}
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
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				alias, ok := sel.X.(*ast.Ident)
				if !ok {
					return true
				}
				pkg := f.imports[alias.Name]
				// Only recognize the config APIs, not unrelated methods with similar names.
				if !(pkg == "github.com/rancher/shepherd/pkg/config" && sel.Sel.Name == "LoadConfig" && len(call.Args) == 2 || pkg == "github.com/rancher/shepherd/pkg/config/operations" && sel.Sel.Name == "LoadObjectFromMap" && len(call.Args) == 3) {
					return true
				}
				key := i.constant(call.Args[0], f, 0)
				if key == "" {
					out.Unresolved++
					return true
				}
				sections[key] = true
				variable := sourceVariable(call.Args[len(call.Args)-1])
				typ := vars[variable]
				if typ == nil {
					typ = sourceNewType(call.Args[len(call.Args)-1])
				}
				value, present := cfg[key]
				if !present {
					if !seen[key] {
						add("warning", key, "This section is loaded by the selected package. Check its README and defaults before running.", f, call.Pos())
						seen[key] = true
					}
					return true
				}
				if typ == nil {
					out.Unresolved++
					return true
				}
				var checkType func(any, ast.Expr, *testLabSourceFile, string, int)
				checkType = func(value any, typ ast.Expr, tf *testLabSourceFile, at string, depth int) {
					if depth > 12 || len(out.Findings) > 200 {
						return
					}
					typ, tf = i.resolve(typ, tf, 0)
					expected := ""
					valid := true
					switch t := typ.(type) {
					case *ast.StructType:
						expected = "a mapping"
						m, ok := value.(map[string]any)
						valid = ok
						if ok {
							for _, field := range t.Fields.List {
								name := sourceFieldName(field)
								if name == "" || name == "-" {
									continue
								}
								if v, exists := m[name]; exists {
									checkType(v, field.Type, tf, at+"."+name, depth+1)
								}
							}
						}
					case *ast.ArrayType:
						expected = "a list"
						list, ok := value.([]any)
						valid = ok
						if ok {
							for j, v := range list {
								if j >= 50 {
									out.Unresolved++
									break
								}
								checkType(v, t.Elt, tf, fmt.Sprintf("%s[%d]", at, j), depth+1)
							}
						}
					case *ast.MapType:
						expected = "a mapping"
						_, valid = value.(map[string]any)
					case *ast.Ident:
						switch t.Name {
						case "string":
							expected = "a string"
							_, valid = value.(string)
						case "bool":
							expected = "true or false"
							_, valid = value.(bool)
						case "int", "int32", "int64", "uint", "uint32", "uint64":
							expected = "an integer"
							v := reflect.ValueOf(value)
							valid = v.IsValid() && (v.Kind() >= reflect.Int && v.Kind() <= reflect.Uint64)
						case "any":
						default:
							out.Unresolved++
						}
					case *ast.InterfaceType:
					default:
						out.Unresolved++
					}
					if !valid && !seen[at+"type"] {
						add("warning", at, "Go configuration expects "+expected+" here. Review the value’s YAML type.", f, call.Pos())
						seen[at+"type"] = true
					}
				}
				checkType(value, typ, f, key, 0)
				// NotEmpty is evidence of an expectation, not proof of an unconditional
				// requirement. Keep it advisory when static control flow is uncertain.
				resolved, _ := i.resolve(typ, f, 0)
				st, ok := resolved.(*ast.StructType)
				if !ok {
					return true
				}
				fields := map[string]string{}
				for _, field := range st.Fields.List {
					for _, name := range field.Names {
						fields[name.Name] = sourceFieldName(field)
					}
				}
				ast.Inspect(fn.Body, func(node ast.Node) bool {
					c, ok := node.(*ast.CallExpr)
					if !ok || len(c.Args) < 2 {
						return true
					}
					assert, ok := c.Fun.(*ast.SelectorExpr)
					if !ok || assert.Sel.Name != "NotEmpty" {
						return true
					}
					a, ok := assert.X.(*ast.Ident)
					if !ok || !(f.imports[a.Name] == "github.com/stretchr/testify/require" || f.imports[a.Name] == "github.com/stretchr/testify/assert") {
						return true
					}
					field, ok := c.Args[1].(*ast.SelectorExpr)
					if !ok || sourceVariable(field.X) != variable {
						return true
					}
					name := fields[field.Sel.Name]
					if name == "" || name == "-" {
						return true
					}
					m, _ := value.(map[string]any)
					v := reflect.ValueOf(m[name])
					empty := !v.IsValid()
					if v.IsValid() {
						switch v.Kind() {
						case reflect.Array, reflect.Slice, reflect.Map, reflect.String:
							empty = v.Len() == 0
						default:
							empty = v.IsZero()
						}
					}
					at := key + "." + name
					if empty && !seen[at] {
						add("warning", at, "The source asserts this field is not empty. Supply a value or verify the condition/default that provides it.", f, c.Pos())
						seen[at] = true
					}
					return true
				})
				return true
			})
		}
	}
	for key := range sections {
		out.Sections = append(out.Sections, key)
	}
	sort.Strings(out.Sections)
	if len(packages) == 0 {
		add("info", "selection", "Select tests to check their package-specific configuration.", nil, 0)
	} else if len(sections) == 0 {
		add("info", "coverage", "No direct configuration loads were resolved in these packages. Read their documentation; helpers may load additional fields.", nil, 0)
	}
	return out
}
