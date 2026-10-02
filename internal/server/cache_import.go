package server

import (
	"context"

	"github.com/brudnak/ha-rancher-rke2/internal/cachelab"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

func (h CacheHandlers) Import(s *cachelab.Service, w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, cachelab.MaxDB+(2<<20))
	reader, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "choose a standalone SQLite .db file", 400)
		return
	}
	workspace := r.URL.Query().Get("workspace")
	ctx, id, err := s.BeginJob(workspace, "Importing SQLite file")
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	var record cachelab.Snapshot
	stopBody := context.AfterFunc(ctx, func() { _ = r.Body.Close() })
	defer stopBody()
	defer func() { s.FinishJob(record, err) }()
	path := filepath.Join(s.Root(), workspace, id+".partial")
	defer os.Remove(path)
	part, err := reader.NextPart()
	if err != nil {
		http.Error(w, "no database file received", 400)
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	_, err = io.Copy(f, io.LimitReader(part, cachelab.MaxDB+1))
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	// Normalize imported standalone databases using the same snapshot primitive.
	vacuumPath := filepath.Join(s.Root(), workspace, cachelab.ID()+".partial")
	defer os.Remove(vacuumPath)
	s.Stage("Creating VACUUM INTO copy")
	err = cachelab.Vacuum(ctx, path, vacuumPath)
	if err == nil {
		record, err = s.AddSnapshot(ctx, workspace, vacuumPath, cachelab.Snapshot{Name: cachelab.Text(filepath.Base(part.FileName()), 140), Source: "Imported file", Method: "VACUUM INTO · imported SQLite"})
	}
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	WriteJSON(w, record)
}
