package history_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/brudnak/ha-rancher-rke2/internal/history"
	_ "modernc.org/sqlite"
)

// Evaluation only: no production backend, schema migration, or user-data access.
// Matches FileStoreList's full-history response and payload. An indexed range or
// paginated read could do less work but would require a separate API change.
func BenchmarkSQLiteHistoryList(b *testing.B) {
	for _, count := range []int{100, 1000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			db, err := sql.Open("sqlite", filepath.Join(b.TempDir(), "history.db"))
			if err != nil {
				b.Fatal(err)
			}
			defer db.Close()
			db.SetMaxOpenConns(1)
			if _, err = db.Exec(`CREATE TABLE events (stream TEXT NOT NULL, at INTEGER NOT NULL, id TEXT NOT NULL, payload BLOB NOT NULL, PRIMARY KEY(stream,at,id))`); err != nil {
				b.Fatal(err)
			}
			at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
			for i := 0; i < count; i++ {
				event := history.Event{ID: fmt.Sprint(i), At: at.Add(time.Duration(i) * time.Second), Kind: "deployment", Data: json.RawMessage(`{"version":"v2.15.3","digest":"sha256:example"}`)}
				raw, err := json.MarshalIndent(event, "", "  ")
				if err != nil {
					b.Fatal(err)
				}
				if _, err = db.Exec(`INSERT INTO events VALUES (?,?,?,?)`, "cluster", event.At.UnixNano(), event.ID, raw); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				events, err := readSQLiteHistory(db)
				if err != nil || len(events) != count {
					b.Fatalf("read: %d %v", len(events), err)
				}
			}
		})
	}
}

func readSQLiteHistory(db *sql.DB) ([]history.Event, error) {
	rows, err := db.Query(`SELECT payload FROM events WHERE stream=? ORDER BY at,id`, "cluster")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []history.Event{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var event history.Event
		if err := json.Unmarshal(raw, &event); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}
