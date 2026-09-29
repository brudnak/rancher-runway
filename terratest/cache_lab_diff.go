package test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	sqlite "modernc.org/sqlite"
)

// A temporary disk index keeps comparisons bounded in memory. Database pages
// and implicit ROWIDs never participate in identity. Counts cover the complete
// scan; only the returned details are capped. An exceeded budget is an error,
// never an apparently complete partial comparison.
func cacheLabCompareRows(ctx context.Context, beforeDB, afterDB *sql.DB, a, b cacheLabTable, ae, be bool, req cacheLabRequest, result cacheLabDiff) (cacheLabDiff, error) {
	source := a
	if !ae {
		source = b
	}
	pk := []cacheLabColumn{}
	for _, c := range source.Columns {
		if c.PK > 0 {
			pk = append(pk, c)
		}
	}
	sort.Slice(pk, func(i, j int) bool { return pk[i].PK < pk[j].PK })
	keys := []string{}
	for _, c := range pk {
		keys = append(keys, c.Name)
	}
	if len(keys) == 0 {
		for _, candidate := range []string{"key", "id", "uid", "name"} {
			for _, c := range source.Columns {
				if strings.EqualFold(c.Name, candidate) {
					keys = []string{c.Name}
					if candidate == "name" {
						for _, n := range source.Columns {
							if strings.EqualFold(n.Name, "namespace") {
								keys = append([]string{n.Name}, keys...)
							}
						}
					}
					break
				}
			}
			if len(keys) > 0 {
				break
			}
		}
	}
	temporary, err := os.MkdirTemp("", "runway-cache-diff-")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(temporary)
	index, err := sql.Open("sqlite", filepath.Join(temporary, "index.db")+"?_pragma=journal_mode(OFF)&_pragma=synchronous(OFF)")
	if err != nil {
		return result, err
	}
	defer index.Close()
	index.SetMaxOpenConns(1)
	if _, err = index.ExecContext(ctx, "CREATE TABLE records(side INTEGER, key TEXT, payload TEXT); CREATE INDEX record_keys ON records(side,key)"); err != nil {
		return result, err
	}
	var totalBytes int64
	keyed := len(keys) > 0
	stage := func(db *sql.DB, table cacheLabTable, exists bool, side int) (int, error) {
		if !exists {
			return 0, nil
		}
		conn, err := db.Conn(ctx)
		if err != nil {
			return 0, err
		}
		defer conn.Close()
		if _, err = sqlite.Limit(conn, 0, 8<<20); err != nil {
			return 0, err
		}
		rows, err := conn.QueryContext(ctx, "SELECT * FROM "+cacheLabIdent(table.Name))
		if err != nil {
			return 0, err
		}
		defer rows.Close()
		columns, err := rows.Columns()
		if err != nil {
			return 0, err
		}
		tx, err := index.BeginTx(ctx, nil)
		if err != nil {
			return 0, err
		}
		defer tx.Rollback()
		insert, err := tx.PrepareContext(ctx, "INSERT INTO records(side,key,payload) VALUES(?,?,?)")
		if err != nil {
			return 0, err
		}
		defer insert.Close()
		count := 0
		for rows.Next() {
			count++
			if count > 1000000 {
				return 0, fmt.Errorf("comparison exceeds one million rows per table; narrow the investigation with SQL")
			}
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for i := range pointers {
				pointers[i] = &values[i]
			}
			if err = rows.Scan(pointers...); err != nil {
				return 0, err
			}
			record := map[string]any{}
			for i, name := range columns {
				record[name] = cacheLabCanonical(cacheLabValue(values[i]), req.IgnoreVolatile)
			}
			payload := cacheLabJSON(record)
			parts := []any{}
			for _, key := range keys {
				value, present := record[key]
				if !present || value == nil {
					keyed = false
				}
				parts = append(parts, value)
			}
			totalBytes += int64(len(payload))
			if totalBytes > 512<<20 {
				return 0, fmt.Errorf("comparison exceeds 512 MiB of decoded table data; narrow the investigation with SQL")
			}
			if _, err = insert.ExecContext(ctx, side, cacheLabJSON(parts), payload); err != nil {
				return 0, err
			}
		}
		if err = rows.Err(); err != nil {
			return 0, err
		}
		return count, tx.Commit()
	}
	result.BeforeRows, err = stage(beforeDB, a, ae, 0)
	if err != nil {
		return result, err
	}
	result.AfterRows, err = stage(afterDB, b, be, 1)
	if err != nil {
		return result, err
	}
	if keyed {
		var duplicate int
		err = index.QueryRowContext(ctx, "SELECT 1 FROM records GROUP BY side,key HAVING count(*)>1 LIMIT 1").Scan(&duplicate)
		if err == nil {
			keyed = false
		} else if err != sql.ErrNoRows {
			return result, err
		}
	}
	if keyed {
		result.KeyColumns = keys
	} else {
		result.Warnings = append(result.Warnings, "No shared unique record key was found. Comparing complete rows as a multiset; edits appear as a removal and an addition.")
		if _, err = index.ExecContext(ctx, "UPDATE records SET key=payload"); err != nil {
			return result, err
		}
	}
	for _, side := range []struct {
		name   string
		number int
	}{{"before_rows", 0}, {"after_rows", 1}} {
		if _, err = index.ExecContext(ctx, "CREATE TABLE "+side.name+" (key TEXT PRIMARY KEY,payload TEXT,n INTEGER) WITHOUT ROWID"); err != nil {
			return result, err
		}
		if _, err = index.ExecContext(ctx, "INSERT INTO "+side.name+" SELECT key,payload,count(*) FROM records WHERE side=? GROUP BY key", side.number); err != nil {
			return result, err
		}
	}
	rows, err := index.QueryContext(ctx, `SELECT a.key,a.payload,b.payload,a.n,coalesce(b.n,0) FROM before_rows a LEFT JOIN after_rows b ON a.key=b.key
 UNION ALL SELECT b.key,NULL,b.payload,0,b.n FROM after_rows b LEFT JOIN before_rows a ON a.key=b.key WHERE a.key IS NULL
 ORDER BY 1`)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	detailBytes := 0
	add := func(kind, key, before, after string) error {
		if len(result.Changes) >= 300 || detailBytes+len(before)+len(after) > 8<<20 {
			result.Truncated = true
			return nil
		}
		detailBytes += len(before) + len(after)
		change := cacheLabChange{Kind: kind, Key: key}
		decode := func(raw string, target *map[string]any) error {
			if raw == "" {
				return nil
			}
			decoder := json.NewDecoder(strings.NewReader(raw))
			decoder.UseNumber()
			return decoder.Decode(target)
		}
		if err := decode(before, &change.Before); err != nil {
			return err
		}
		if err := decode(after, &change.After); err != nil {
			return err
		}
		if kind == "changed" {
			change.Fields = []cacheLabFieldChange{}
			cacheLabFields("", change.Before, change.After, &change.Fields, 0)
		}
		result.Changes = append(result.Changes, change)
		return nil
	}
	for rows.Next() {
		var key string
		var ap, bp sql.NullString
		var an, bn int
		if err = rows.Scan(&key, &ap, &bp, &an, &bn); err != nil {
			return result, err
		}
		if !keyed {
			common := an
			if bn < common {
				common = bn
			}
			result.Unchanged += common
			if an > bn {
				result.Removed += an - bn
				err = add("removed", fmt.Sprintf("%d occurrence(s)", an-bn), ap.String, "")
			} else if bn > an {
				result.Added += bn - an
				err = add("added", fmt.Sprintf("%d occurrence(s)", bn-an), "", bp.String)
			}
		} else if an == 0 {
			result.Added++
			err = add("added", key, "", bp.String)
		} else if bn == 0 {
			result.Removed++
			err = add("removed", key, ap.String, "")
		} else if ap.String != bp.String {
			result.Changed++
			err = add("changed", key, ap.String, bp.String)
		} else {
			result.Unchanged++
		}
		if err != nil {
			return result, err
		}
	}
	return result, rows.Err()
}
