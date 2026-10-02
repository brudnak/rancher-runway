// Package history stores immutable observations independently of application state.
// Callers own cluster identity, retention policy, locking, and deduplication.
package history

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Immutable, individually atomic observations avoid rewriting a growing archive.
// Event preserves the original JSON payload and observation timestamp.
type Event struct {
	ID   string          `json:"id"`
	At   time.Time       `json:"at"`
	Kind string          `json:"kind"`
	Data json.RawMessage `json:"data"`
}

// WriteEvent atomically publishes an event after syncing its contents.
// The directory and event ID must be supplied by trusted application code.
func WriteEvent(dir string, event Event) error {
	data, err := json.MarshalIndent(event, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".event-")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(dir, event.At.Format("20060102T150405.000000000")+"-"+event.ID+".json"))
}

// ReadEvents returns observations in timestamp order. A missing directory is empty;
// malformed event files are reported rather than silently discarded.
func ReadEvents(dir string) ([]Event, error) {
	events := []Event{}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return events, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var event Event
		if err = json.Unmarshal(raw, &event); err != nil {
			return nil, fmt.Errorf("history event could not be read: %w", err)
		}
		events = append(events, event)
	}
	sort.Slice(events, func(i, j int) bool { return events[i].At.Before(events[j].At) })
	return events, nil
}
