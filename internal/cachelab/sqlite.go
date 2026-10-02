package cachelab

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	sqlite "modernc.org/sqlite"
)

func cacheLabOpen(path string) (*sql.DB, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("SQLite file is unavailable")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	header := make([]byte, 16)
	_, err = f.Read(header)
	f.Close()
	if err != nil || string(header) != "SQLite format 3\x00" {
		return nil, fmt.Errorf("choose a valid, standalone SQLite database; WAL and SHM sidecars are not snapshots")
	}
	u := url.URL{Scheme: "file", Path: path}
	q := url.Values{"mode": {"ro"}, "_pragma": {"query_only(1)", "trusted_schema(0)", "busy_timeout(3000)"}}
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}
func Vacuum(ctx context.Context, source, target string) error {
	// query_only disallows VACUUM INTO's destination writes. The source itself is
	// opened with mode=ro; only this fixed statement gets a separate connection.
	info, err := os.Stat(source)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("source cache is unavailable; make sure SQL caching is enabled and initialized")
	}
	if info.Size() > MaxDB {
		return fmt.Errorf("source database exceeds the 2 GiB limit")
	}
	u := url.URL{Scheme: "file", Path: source}
	u.RawQuery = "mode=ro&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err = db.ExecContext(ctx, "VACUUM INTO ?", target); err != nil {
		return fmt.Errorf("VACUUM INTO failed: %w", err)
	}
	return nil
}
func cacheLabIdent(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }

// Queries are embedded as a subquery. Reject statement separators outside SQL
// strings/comments, then let SQLite parse it. This supports CTEs and semicolons
// inside literals without allowing PRAGMA, ATTACH, or a second statement.
func cacheLabSelect(query string) (string, error) {
	query = strings.TrimSpace(query)
	if len(query) == 0 || len(query) > 64000 {
		return "", fmt.Errorf("enter a SELECT query under 64 KB")
	}
	var quote byte
	line, block := false, false
	lastSemicolon := -1
	for i := 0; i < len(query); i++ {
		c := query[i]
		if line {
			if c == '\n' {
				line = false
			}
			continue
		}
		if block {
			if c == '*' && i+1 < len(query) && query[i+1] == '/' {
				block = false
				i++
			}
			continue
		}
		if quote != 0 {
			end := quote
			if quote == '[' {
				end = ']'
			}
			if c == end {
				if quote != '[' && i+1 < len(query) && query[i+1] == end {
					i++
				} else {
					quote = 0
				}
			}
			continue
		}
		if c == '-' && i+1 < len(query) && query[i+1] == '-' {
			line = true
			i++
			continue
		}
		if c == '/' && i+1 < len(query) && query[i+1] == '*' {
			block = true
			i++
			continue
		}
		if c == '\'' || c == '"' || c == '`' || c == '[' {
			quote = c
			continue
		}
		if c == ';' {
			if lastSemicolon >= 0 {
				return "", fmt.Errorf("run one read-only SELECT at a time")
			}
			lastSemicolon = i
		}
	}
	if quote != 0 || block {
		return "", fmt.Errorf("close SQL strings and comments before running")
	}
	if lastSemicolon >= 0 {
		if strings.TrimSpace(query[lastSemicolon+1:]) != "" {
			return "", fmt.Errorf("run one read-only SELECT at a time; remove the statement separator")
		}
		query = strings.TrimSpace(query[:lastSemicolon])
	}
	return query, nil
}

type cacheLabColumn struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	PK       int    `json:"pk"`
	Nullable bool   `json:"nullable"`
}
type cacheLabTable struct {
	Name    string           `json:"name"`
	SQL     string           `json:"sql"`
	Columns []cacheLabColumn `json:"columns"`
	Indexes []string         `json:"indexes"`
}

func cacheLabSchema(ctx context.Context, db *sql.DB) ([]cacheLabTable, error) {
	rows, err := db.QueryContext(ctx, "SELECT name, coalesce(sql,'') FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name LIMIT 1001")
	if err != nil {
		return nil, err
	}
	tables := []cacheLabTable{}
	for rows.Next() {
		var t cacheLabTable
		if err = rows.Scan(&t.Name, &t.SQL); err != nil {
			rows.Close()
			return nil, err
		}
		tables = append(tables, t)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(tables) > 1000 {
		return nil, fmt.Errorf("database has more than 1,000 tables; inspection limit reached")
	}
	for i := range tables {
		table := &tables[i]
		table.Columns = []cacheLabColumn{}
		table.Indexes = []string{}
		cols, err := db.QueryContext(ctx, "SELECT name,type,pk,\"notnull\" FROM pragma_table_info(?)", table.Name)
		if err != nil {
			return nil, err
		}
		for cols.Next() {
			var c cacheLabColumn
			var required int
			if err = cols.Scan(&c.Name, &c.Type, &c.PK, &required); err != nil {
				cols.Close()
				return nil, err
			}
			c.Nullable = required == 0
			table.Columns = append(table.Columns, c)
		}
		err = cols.Err()
		cols.Close()
		if err != nil {
			return nil, err
		}
		idx, err := db.QueryContext(ctx, "SELECT coalesce(sql,name) FROM sqlite_schema WHERE type='index' AND tbl_name=? ORDER BY name", table.Name)
		if err != nil {
			return nil, err
		}
		for idx.Next() {
			var v string
			if err = idx.Scan(&v); err != nil {
				idx.Close()
				return nil, err
			}
			table.Indexes = append(table.Indexes, v)
		}
		err = idx.Err()
		idx.Close()
		if err != nil {
			return nil, err
		}
	}
	return tables, nil
}

type cacheLabRows struct {
	Columns   []string `json:"columns"`
	Rows      [][]any  `json:"rows"`
	More      bool     `json:"more"`
	ElapsedMS int64    `json:"elapsedMs"`
}

func cacheLabValue(v any) any {
	switch n := v.(type) {
	case float64:
		if math.IsInf(n, 1) {
			return "Infinity"
		}
		if math.IsInf(n, -1) {
			return "-Infinity"
		}
		if math.IsNaN(n) {
			return "NaN"
		}
	case []byte:
		return map[string]any{"type": "blob", "bytes": len(n), "base64": base64.StdEncoding.EncodeToString(n)}
	case int64:
		if n > 9007199254740991 || n < -9007199254740991 {
			return strconv.FormatInt(n, 10)
		}
	}
	return v
}
func cacheLabReadRows(ctx context.Context, db *sql.DB, query string, args []any, limit int) (cacheLabRows, error) {
	result := cacheLabRows{Columns: []string{}, Rows: [][]any{}}
	start := time.Now()
	conn, err := db.Conn(ctx)
	if err != nil {
		return result, err
	}
	defer conn.Close()
	for _, setting := range [][2]int{{0, 8 << 20}, {1, 128000}, {2, 256}, {3, 100}, {4, 20}, {6, 100}, {7, 0}} {
		if _, err = sqlite.Limit(conn, setting[0], setting[1]); err != nil {
			return result, err
		}
	}
	rows, err := conn.QueryContext(ctx, query, args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	result.Columns, err = rows.Columns()
	if err != nil {
		return result, err
	}
	budget := 0
	for rows.Next() {
		if len(result.Rows) >= limit {
			result.More = true
			break
		}
		values := make([]any, len(result.Columns))
		pointers := make([]any, len(values))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err = rows.Scan(pointers...); err != nil {
			return result, err
		}
		for i := range values {
			values[i] = cacheLabValue(values[i])
		}
		encoded, _ := json.Marshal(values)
		budget += len(encoded)
		if budget > 16<<20 {
			return result, fmt.Errorf("result exceeds 16 MiB; select fewer columns or narrow the query")
		}
		result.Rows = append(result.Rows, values)
	}
	result.ElapsedMS = time.Since(start).Milliseconds()
	return result, rows.Err()
}
func (s *Service) Inspect(ctx context.Context, req Request) (any, error) {
	timeout := 20 * time.Second
	if req.Action == "diff" {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	s.mu.Lock()
	record, path, err := s.snapshotLocked(req.Snapshot)
	var other Snapshot
	var otherPath string
	if err == nil && req.Action == "diff" {
		other, otherPath, err = s.snapshotLocked(req.Other)
	}
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	db, err := cacheLabOpen(path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if req.Action == "query" {
		query, err := cacheLabSelect(req.SQL)
		if err != nil {
			return nil, err
		}
		return cacheLabReadRows(ctx, db, "SELECT * FROM (\n"+query+"\n) LIMIT 501", nil, 500)
	}
	schema, err := cacheLabSchema(ctx, db)
	if err != nil {
		return nil, err
	}
	if req.Action == "schema" {
		return map[string]any{"tables": schema, "snapshot": record}, nil
	}
	if req.Action == "diff" {
		return cacheLabCompare(ctx, db, schema, otherPath, record, other, req)
	}
	var table *cacheLabTable
	for i := range schema {
		if schema[i].Name == req.Table {
			table = &schema[i]
		}
	}
	if table == nil {
		return nil, fmt.Errorf("table not found")
	}
	if req.Offset < 0 || req.Offset > 1000000 {
		return nil, fmt.Errorf("page offset is outside the supported range")
	}
	limit := req.Limit
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	query := "SELECT * FROM " + cacheLabIdent(table.Name)
	args := []any{}
	if req.Search != "" {
		if len(req.Search) > 1000 {
			return nil, fmt.Errorf("search is too long")
		}
		parts := []string{}
		for _, c := range table.Columns {
			parts = append(parts, "instr(lower(CAST("+cacheLabIdent(c.Name)+" AS TEXT)),lower(?))>0")
			args = append(args, req.Search)
		}
		query += " WHERE " + strings.Join(parts, " OR ")
	}
	order := []string{}
	if req.Sort != "" {
		found := false
		for _, c := range table.Columns {
			found = found || c.Name == req.Sort
		}
		if !found {
			return nil, fmt.Errorf("sort column not found")
		}
		direction := " ASC"
		if req.Desc {
			direction = " DESC"
		}
		order = append(order, cacheLabIdent(req.Sort)+direction)
	}
	for _, c := range table.Columns {
		if c.PK > 0 {
			order = append(order, cacheLabIdent(c.Name))
		}
	}
	if len(order) > 0 {
		query += " ORDER BY " + strings.Join(order, ",")
	}
	query += " LIMIT ? OFFSET ?"
	args = append(args, limit+1, req.Offset)
	return cacheLabReadRows(ctx, db, query, args, limit)
}

type cacheLabChange struct {
	Kind   string                `json:"kind"`
	Key    string                `json:"key"`
	Before map[string]any        `json:"before,omitempty"`
	After  map[string]any        `json:"after,omitempty"`
	Fields []cacheLabFieldChange `json:"fields,omitempty"`
}
type cacheLabFieldChange struct {
	Path   string `json:"path"`
	Before any    `json:"before"`
	After  any    `json:"after"`
	Kind   string `json:"kind"`
}
type cacheLabDiff struct {
	Table          string                `json:"table"`
	Tables         []cacheLabTableChange `json:"tables"`
	Added          int                   `json:"added"`
	Removed        int                   `json:"removed"`
	Changed        int                   `json:"changed"`
	Unchanged      int                   `json:"unchanged"`
	Changes        []cacheLabChange      `json:"changes"`
	Truncated      bool                  `json:"truncated"`
	KeyColumns     []string              `json:"keyColumns"`
	Warnings       []string              `json:"warnings"`
	BeforeRows     int                   `json:"beforeRows"`
	AfterRows      int                   `json:"afterRows"`
	BeforeSQL      string                `json:"beforeSql"`
	AfterSQL       string                `json:"afterSql"`
	IgnoreVolatile bool                  `json:"ignoreVolatile"`
	BeforeIndexes  []string              `json:"beforeIndexes"`
	AfterIndexes   []string              `json:"afterIndexes"`
}
type cacheLabTableChange struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

func cacheLabCanonical(v any, ignore bool) any {
	if text, ok := v.(string); ok {
		var parsed any
		decoder := json.NewDecoder(strings.NewReader(text))
		decoder.UseNumber()
		if decoder.Decode(&parsed) == nil && decoder.Decode(new(any)) == io.EOF {
			switch parsed.(type) {
			case map[string]any, []any:
				v = parsed
			}
		}
	}
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, value := range x {
			if ignore && k == "metadata" {
				if metadata, ok := value.(map[string]any); ok {
					copy := map[string]any{}
					for key, v := range metadata {
						if key != "resourceVersion" && key != "managedFields" {
							copy[key] = v
						}
					}
					value = copy
				}
			}
			out[k] = cacheLabCanonical(value, ignore)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, v := range x {
			out[i] = cacheLabCanonical(v, ignore)
		}
		return out
	}
	return v
}
func cacheLabJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
func cacheLabFields(path string, a, b any, out *[]cacheLabFieldChange, depth int) {
	if len(*out) >= 200 || depth > 30 {
		return
	}
	if cacheLabJSON(a) == cacheLabJSON(b) {
		return
	}
	am, aok := a.(map[string]any)
	bm, bok := b.(map[string]any)
	if aok && bok {
		keys := map[string]bool{}
		for k := range am {
			keys[k] = true
		}
		for k := range bm {
			keys[k] = true
		}
		ordered := []string{}
		for k := range keys {
			ordered = append(ordered, k)
		}
		sort.Strings(ordered)
		for _, k := range ordered {
			next := path + "/" + strings.ReplaceAll(strings.ReplaceAll(k, "~", "~0"), "/", "~1")
			av, ae := am[k]
			bv, be := bm[k]
			if !ae || !be {
				kind := "added"
				if !be {
					kind = "removed"
				}
				if len(*out) < 200 {
					*out = append(*out, cacheLabFieldChange{next, av, bv, kind})
				}
			} else {
				cacheLabFields(next, av, bv, out, depth+1)
			}
		}
		return
	}
	*out = append(*out, cacheLabFieldChange{path, a, b, "changed"})
}
func cacheLabCompare(ctx context.Context, beforeDB *sql.DB, beforeSchema []cacheLabTable, afterPath string, before, after Snapshot, req Request) (cacheLabDiff, error) {
	result := cacheLabDiff{Table: req.Table, Tables: []cacheLabTableChange{}, Changes: []cacheLabChange{}, Warnings: []string{}, KeyColumns: []string{}, IgnoreVolatile: req.IgnoreVolatile}
	db, err := cacheLabOpen(afterPath)
	if err != nil {
		return result, err
	}
	defer db.Close()
	afterSchema, err := cacheLabSchema(ctx, db)
	if err != nil {
		return result, err
	}
	if before.Workspace != after.Workspace || before.Source != after.Source {
		result.Warnings = append(result.Warnings, "These snapshots come from different sources.")
	}
	if before.PodUID != after.PodUID || before.Restarts != after.Restarts {
		result.Warnings = append(result.Warnings, "The source replica or restart count differs. Cache warm-up may explain some changes.")
	}
	if before.Image != after.Image {
		result.Warnings = append(result.Warnings, "The recorded Rancher / Steve versions differ.")
	}
	left, right := map[string]cacheLabTable{}, map[string]cacheLabTable{}
	names := map[string]bool{}
	for _, t := range beforeSchema {
		left[t.Name] = t
		names[t.Name] = true
	}
	for _, t := range afterSchema {
		right[t.Name] = t
		names[t.Name] = true
	}
	ordered := []string{}
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	for _, name := range ordered {
		a, ae := left[name]
		b, be := right[name]
		kind := "same"
		if !ae {
			kind = "added"
		} else if !be {
			kind = "removed"
		} else if a.SQL != b.SQL || cacheLabJSON(a.Indexes) != cacheLabJSON(b.Indexes) {
			kind = "schema changed"
		}
		result.Tables = append(result.Tables, cacheLabTableChange{name, kind})
	}
	if req.Table == "" {
		return result, nil
	}
	a, ae := left[req.Table]
	b, be := right[req.Table]
	if !ae && !be {
		return result, fmt.Errorf("table not found in either snapshot")
	}
	result.BeforeSQL = a.SQL
	result.AfterSQL = b.SQL
	result.BeforeIndexes = a.Indexes
	result.AfterIndexes = b.Indexes
	return cacheLabCompareRows(ctx, beforeDB, db, a, b, ae, be, req, result)
}
