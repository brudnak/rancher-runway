package cachelab

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (s *Service) AddSnapshot(ctx context.Context, workspace, path string, record Snapshot) (Snapshot, error) {
	info, err := os.Stat(path)
	if err != nil {
		return record, err
	}
	if info.Size() > MaxDB {
		return record, fmt.Errorf("snapshot exceeds the 2 GiB limit")
	}
	db, err := cacheLabOpen(path)
	if err != nil {
		return record, err
	}
	defer db.Close()
	var check string
	if err = db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&check); err != nil {
		return record, fmt.Errorf("snapshot validation failed: %w", err)
	}
	if check != "ok" {
		return record, fmt.Errorf("snapshot validation failed: %s", check)
	}
	if err = db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&record.Tables); err != nil {
		return record, err
	}
	f, err := os.Open(path)
	if err != nil {
		return record, err
	}
	hash := sha256.New()
	_, err = io.Copy(hash, f)
	f.Close()
	if err != nil {
		return record, err
	}
	if err := ctx.Err(); err != nil {
		return record, err
	}
	record.SHA256 = hex.EncodeToString(hash.Sum(nil))
	record.Bytes = info.Size()
	record.Workspace = workspace
	record.CreatedAt = time.Now().UTC()
	if record.ID == "" {
		record.ID = ID()
	}
	if record.Name == "" {
		record.Name = "Snapshot · " + record.CreatedAt.Local().Format("Jan 2, 15:04:05")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	w, err := s.workspaceLocked(workspace)
	if err != nil {
		return record, err
	}
	destination := filepath.Join(s.root, workspace, record.ID+".db")
	if err = os.Chmod(path, 0600); err != nil {
		return record, err
	}
	if err = os.Rename(path, destination); err != nil {
		return record, err
	}
	previousView := w.View
	previousActive := s.library.Active
	// Publish only after validation and the atomic file rename.
	s.library.Snapshots = append(s.library.Snapshots, record)
	w.View.Snapshot = record.ID
	s.library.Active = w.ID
	if err = s.persistLocked(); err != nil {
		s.library.Snapshots = s.library.Snapshots[:len(s.library.Snapshots)-1]
		w.View = previousView
		s.library.Active = previousActive
		_ = os.Remove(destination)
		return record, err
	}
	return record, nil
}

func (s *Service) ExportSnapshot(id string) (any, error) {
	s.mu.Lock()
	record, path, err := s.snapshotLocked(id)
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	directory := filepath.Join(home, "Downloads")
	if err = os.MkdirAll(directory, 0755); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	filename := FileLabel(SourceLabel(record.Source)) + "--" + FileLabel(record.Name) + "--" + record.CreatedAt.Format("20060102-150405") + "-" + ID()[:6] + ".db"
	destination := filepath.Join(directory, filename)
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	_, err = io.Copy(out, f)
	closeErr := out.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(destination)
		return nil, err
	}
	return map[string]string{"path": destination}, nil
}

func FileLabel(value string) string {
	var b strings.Builder
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
			b.WriteByte('-')
		}
		if b.Len() >= 48 {
			break
		}
	}
	if b.Len() == 0 {
		return "snapshot"
	}
	return strings.Trim(b.String(), "-")
}
