package test

// Cache Lab owns diagnostic copies only. It never changes a live cache or uses
// the infrastructure lifecycle lock. Library paths are generated, never supplied
// by the browser; connection secrets are kept out of the library manifest.
import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

const cacheLabMaxDB = int64(2 << 30)

var cacheLabIDPattern = regexp.MustCompile(`^[a-f0-9]{24}$`)

type cacheLabWorkspace struct {
	ID            string               `json:"id"`
	Name          string               `json:"name"`
	URL           string               `json:"url"`
	Kind          string               `json:"kind"`
	Kubeconfig    string               `json:"kubeconfig,omitempty"`
	Context       string               `json:"context,omitempty"`
	Cluster       string               `json:"cluster,omitempty"`
	Namespace     string               `json:"namespace"`
	Pod           string               `json:"pod,omitempty"`
	Container     string               `json:"container,omitempty"`
	HelperPath    string               `json:"helperPath,omitempty"`
	CAPEM         string               `json:"caPem,omitempty"`
	Insecure      bool                 `json:"insecure"`
	RememberToken bool                 `json:"rememberToken"`
	Connected     bool                 `json:"connected"`
	Notes         string               `json:"notes,omitempty"`
	Folders       []string             `json:"folders"`
	Queries       []cacheLabSavedQuery `json:"queries"`
	View          cacheLabView         `json:"view"`
	CreatedAt     time.Time            `json:"createdAt"`
}
type cacheLabSavedQuery struct {
	Name string `json:"name"`
	SQL  string `json:"sql"`
}
type cacheLabView struct {
	Snapshot       string `json:"snapshot"`
	Baseline       string `json:"baseline"`
	Comparison     string `json:"comparison"`
	Table          string `json:"table"`
	CompareTable   string `json:"compareTable"`
	Mode           string `json:"mode"`
	SQL            string `json:"sql"`
	Folder         string `json:"folder"`
	IgnoreVolatile bool   `json:"ignoreVolatile"`
}
type cacheLabSnapshot struct {
	ID        string    `json:"id"`
	Workspace string    `json:"workspace"`
	Name      string    `json:"name"`
	Folder    string    `json:"folder"`
	Notes     string    `json:"notes"`
	Favorite  bool      `json:"favorite"`
	CreatedAt time.Time `json:"createdAt"`
	Bytes     int64     `json:"bytes"`
	SHA256    string    `json:"sha256"`
	Source    string    `json:"source"`
	Pod       string    `json:"pod,omitempty"`
	PodUID    string    `json:"podUid,omitempty"`
	Restarts  int       `json:"restarts"`
	Image     string    `json:"image,omitempty"`
	Method    string    `json:"method"`
	Tables    int       `json:"tables"`
}
type cacheLabJob struct {
	ID        string    `json:"id"`
	Workspace string    `json:"workspace"`
	Running   bool      `json:"running"`
	Stage     string    `json:"stage"`
	Error     string    `json:"error,omitempty"`
	Snapshot  string    `json:"snapshot,omitempty"`
	StartedAt time.Time `json:"startedAt"`
}
type cacheLabLibrary struct {
	Version    int                 `json:"version"`
	Active     string              `json:"active"`
	Workspaces []cacheLabWorkspace `json:"workspaces"`
	Snapshots  []cacheLabSnapshot  `json:"snapshots"`
	Job        cacheLabJob         `json:"job"`
}
type cacheLabService struct {
	mu      sync.Mutex
	root    string
	library cacheLabLibrary
	tokens  map[string]string
	cancel  context.CancelFunc
	// Tests replace command execution; production always uses argument arrays.
	runner cacheLabCommandRunner
}

func cacheLabID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func (p *localControlPanel) cacheLabService() (*cacheLabService, error) {
	p.cacheLabMu.Lock()
	defer p.cacheLabMu.Unlock()
	if p.cacheLab != nil {
		return p.cacheLab, nil
	}
	root, err := absoluteFromWorkingDir(filepath.Join(automationOutputDir(), "control-panel", "cache-lab"))
	if err != nil {
		return nil, err
	}
	s, err := newCacheLabService(root)
	if err == nil {
		p.cacheLab = s
	}
	return s, err
}
func newCacheLabService(root string) (*cacheLabService, error) {
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	s := &cacheLabService{root: root, tokens: map[string]string{}, runner: cacheLabRunCommand, library: cacheLabLibrary{Version: 1, Workspaces: []cacheLabWorkspace{}, Snapshots: []cacheLabSnapshot{}}}
	b, err := os.ReadFile(filepath.Join(root, "library.json"))
	if err == nil {
		var restored cacheLabLibrary
		if err = json.Unmarshal(b, &restored); err != nil {
			return nil, fmt.Errorf("Cache Lab library could not be read; existing snapshots were preserved: %w", err)
		}
		s.library = restored
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if s.library.Workspaces == nil {
		s.library.Workspaces = []cacheLabWorkspace{}
	}
	if s.library.Snapshots == nil {
		s.library.Snapshots = []cacheLabSnapshot{}
	}
	if s.library.Version != 1 {
		return nil, fmt.Errorf("unsupported Cache Lab library version")
	}
	for i := range s.library.Workspaces {
		w := &s.library.Workspaces[i]
		if !cacheLabIDPattern.MatchString(w.ID) {
			return nil, fmt.Errorf("invalid workspace record")
		}
		w.Connected = w.Kind != "rancher"
		if w.RememberToken {
			if token, err := cacheLabKeychain("read", w.ID, ""); err == nil {
				s.tokens[w.ID] = token
				w.Connected = true
			}
		}
	}
	for _, v := range s.library.Snapshots {
		if !cacheLabIDPattern.MatchString(v.ID) || !cacheLabIDPattern.MatchString(v.Workspace) {
			return nil, fmt.Errorf("invalid snapshot record")
		}
	}
	deleting, _ := filepath.Glob(filepath.Join(root, "*", "*.db.delete"))
	for _, path := range deleting {
		ws := filepath.Base(filepath.Dir(path))
		id := strings.TrimSuffix(filepath.Base(path), ".db.delete")
		if !cacheLabIDPattern.MatchString(ws) || !cacheLabIDPattern.MatchString(id) {
			continue
		}
		indexed := false
		for _, snap := range s.library.Snapshots {
			indexed = indexed || (snap.ID == id && snap.Workspace == ws)
		}
		if indexed {
			if err := os.Rename(path, strings.TrimSuffix(path, ".delete")); err != nil {
				return nil, err
			}
		} else {
			if err := os.Remove(path); err != nil {
				return nil, err
			}
		}
	}

	if s.library.Job.Running {
		s.library.Job.Running = false
		s.library.Job.Error = "Capture was interrupted when Runway closed. Capture again; incomplete files are never added to the library."
		s.library.Job.Stage = "Interrupted"
	}
	// Only generated .partial files belong to interrupted local transfers.
	paths, _ := filepath.Glob(filepath.Join(root, "*", "*.partial"))
	for _, path := range paths {
		if cacheLabIDPattern.MatchString(filepath.Base(filepath.Dir(path))) {
			_ = os.Remove(path)
		}
	}
	return s, s.persistLocked()
}
func (s *cacheLabService) persistLocked() error {
	b, err := json.MarshalIndent(s.library, "", "  ")
	if err != nil {
		return err
	}
	if len(b) > 16<<20 {
		return fmt.Errorf("library metadata exceeds 16 MiB; remove unused saved queries or notes")
	}
	f, err := os.CreateTemp(s.root, ".library-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, filepath.Join(s.root, "library.json"))
}
func (s *cacheLabService) workspaceLocked(id string) (*cacheLabWorkspace, error) {
	for i := range s.library.Workspaces {
		if s.library.Workspaces[i].ID == id {
			return &s.library.Workspaces[i], nil
		}
	}
	return nil, fmt.Errorf("workspace not found")
}
func (s *cacheLabService) snapshotLocked(id string) (cacheLabSnapshot, string, error) {
	for _, v := range s.library.Snapshots {
		if v.ID == id {
			path := filepath.Join(s.root, v.Workspace, v.ID+".db")
			info, err := os.Lstat(path)
			if err != nil || !info.Mode().IsRegular() {
				return v, "", fmt.Errorf("snapshot file is missing or is not a regular file")
			}
			return v, path, nil
		}
	}
	return cacheLabSnapshot{}, "", fmt.Errorf("snapshot not found")
}
func cacheLabText(value string, max int) string {
	v := strings.TrimSpace(value)
	r := []rune(v)
	if len(r) > max {
		return string(r[:max])
	}
	return v
}

type cacheLabRequest struct {
	Action         string            `json:"action"`
	Workspace      string            `json:"workspace"`
	Snapshot       string            `json:"snapshot"`
	Other          string            `json:"other"`
	Name           string            `json:"name"`
	Notes          string            `json:"notes"`
	Folder         string            `json:"folder"`
	Favorite       bool              `json:"favorite"`
	Confirm        string            `json:"confirm"`
	Token          string            `json:"token"`
	Profile        cacheLabWorkspace `json:"profile"`
	View           cacheLabView      `json:"view"`
	SQL            string            `json:"sql"`
	Table          string            `json:"table"`
	Search         string            `json:"search"`
	Sort           string            `json:"sort"`
	Desc           bool              `json:"desc"`
	Offset         int               `json:"offset"`
	Limit          int               `json:"limit"`
	IgnoreVolatile bool              `json:"ignoreVolatile"`
	RunID          string            `json:"runId"`
}

func (p *localControlPanel) handleCacheLab(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	s, err := p.cacheLabService()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if r.Method == http.MethodGet {
		s.mu.Lock()
		defer s.mu.Unlock()
		writeJSON(w, map[string]any{"library": s.library, "root": s.root, "keychain": runtime.GOOS == "darwin"})
		return
	}
	if strings.HasSuffix(r.URL.Path, "/import") {
		p.handleCacheLabImport(s, w, r)
		return
	}
	var req cacheLabRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&req); err != nil {
		http.Error(w, "invalid Cache Lab request", 400)
		return
	}
	var result any
	switch req.Action {
	case "save-workspace":
		result, err = s.saveWorkspace(r.Context(), req)
	case "discover":
		result, err = s.discover(r.Context(), req.Workspace)
	case "capture":
		result, err = s.startCapture(req)
	case "steve":
		result, err = p.cacheLabSteve(s, req.RunID)
	case "schema", "rows", "query", "diff":
		result, err = s.inspect(r.Context(), req)
	case "export":
		result, err = s.exportSnapshot(req.Snapshot)
	default:
		result, err = s.mutate(req)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, result)
}
func (s *cacheLabService) mutate(req cacheLabRequest) (result any, returnedErr error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if req.Action == "cancel" {
		if s.cancel != nil {
			s.cancel()
		}
		return map[string]bool{"ok": true}, nil
	}
	w, err := s.workspaceLocked(req.Workspace)
	if err != nil {
		return nil, err
	}
	manifestBefore, _ := json.Marshal(s.library)
	defer func() {
		if returnedErr != nil {
			_ = json.Unmarshal(manifestBefore, &s.library)
		}
	}()
	switch req.Action {
	case "select-pod":
		if s.library.Job.Running && s.library.Job.Workspace == w.ID {
			return nil, fmt.Errorf("capture is in progress")
		}
		w.Pod = cacheLabText(req.Name, 253)
	case "view":
		w.View = req.View
		w.View.SQL = cacheLabText(w.View.SQL, 64000)
		s.library.Active = w.ID
	case "rename-workspace":
		if strings.TrimSpace(req.Name) == "" {
			return nil, fmt.Errorf("enter a workspace name")
		}
		w.Name = cacheLabText(req.Name, 120)
		w.Notes = cacheLabText(req.Notes, 8000)
	case "folder":
		name := cacheLabText(req.Name, 80)
		if name == "" || name == "__unfiled__" {
			return nil, fmt.Errorf("enter another folder name")
		}
		if len(w.Folders) >= 100 {
			return nil, fmt.Errorf("folder limit reached")
		}
		found := false
		for _, f := range w.Folders {
			found = found || f == name
		}
		if !found {
			w.Folders = append(w.Folders, name)
		}
	case "rename-folder":
		name := cacheLabText(req.Name, 80)
		if name == "" || name == "__unfiled__" {
			return nil, fmt.Errorf("enter another folder name")
		}
		exists := false
		for _, f := range w.Folders {
			if f == name {
				return nil, fmt.Errorf("a folder with this name already exists")
			}
			exists = exists || f == req.Folder
		}
		if !exists {
			return nil, fmt.Errorf("folder not found")
		}
		for i := range w.Folders {
			if w.Folders[i] == req.Folder {
				w.Folders[i] = name
			}
		}
		for i := range s.library.Snapshots {
			v := &s.library.Snapshots[i]
			if v.Workspace == w.ID && v.Folder == req.Folder {
				v.Folder = name
			}
		}
		if w.View.Folder == req.Folder {
			w.View.Folder = name
		}
	case "delete-folder":
		if req.Confirm != "remove folder" {
			return nil, fmt.Errorf("type remove folder to confirm")
		}
		for i := range s.library.Snapshots {
			v := &s.library.Snapshots[i]
			if v.Workspace == w.ID && v.Folder == req.Folder {
				v.Folder = ""
			}
		}
		folders := []string{}
		for _, f := range w.Folders {
			if f != req.Folder {
				folders = append(folders, f)
			}
		}
		w.Folders = folders
		if w.View.Folder == req.Folder {
			w.View.Folder = ""
		}
	case "save-query":
		name := cacheLabText(req.Name, 100)
		if name == "" {
			return nil, fmt.Errorf("enter a query name")
		}
		if _, err := cacheLabSelect(req.SQL); err != nil {
			return nil, err
		}
		found := false
		for i := range w.Queries {
			if w.Queries[i].Name == name {
				w.Queries[i].SQL = req.SQL
				found = true
			}
		}
		if !found {
			if len(w.Queries) >= 100 {
				return nil, fmt.Errorf("saved query limit reached")
			}
			w.Queries = append(w.Queries, cacheLabSavedQuery{name, req.SQL})
		}
	case "delete-query":
		queries := []cacheLabSavedQuery{}
		for _, q := range w.Queries {
			if q.Name != req.Name {
				queries = append(queries, q)
			}
		}
		w.Queries = queries
	case "snapshot":
		found := false
		for i := range s.library.Snapshots {
			v := &s.library.Snapshots[i]
			if v.ID == req.Snapshot && v.Workspace == w.ID {
				if req.Name != "" {
					v.Name = cacheLabText(req.Name, 140)
				}
				v.Notes = cacheLabText(req.Notes, 8000)
				v.Folder = cacheLabText(req.Folder, 80)
				v.Favorite = req.Favorite
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("snapshot not found")
		}
	case "delete-snapshot", "delete-workspace", "clear-workspace":
		if s.library.Job.Running && s.library.Job.Workspace == w.ID {
			return nil, fmt.Errorf("wait for the capture to finish or cancel it before cleanup")
		}
		phrase := "delete snapshot"
		if req.Action == "delete-workspace" {
			phrase = "delete workspace"
		}
		if req.Action == "clear-workspace" {
			phrase = "delete snapshots"
		}
		if req.Confirm != phrase {
			return nil, fmt.Errorf("type %s to confirm", phrase)
		}
		if req.Action == "delete-workspace" && w.RememberToken {
			if _, err := cacheLabKeychain("delete", w.ID, ""); err != nil {
				return nil, err
			}
		}
		previous, _ := json.Marshal(s.library)
		staged := map[string]string{}
		rollback := func() {
			for path, temporary := range staged {
				_ = os.Rename(temporary, path)
			}
			_ = json.Unmarshal(previous, &s.library)
		}
		keep := []cacheLabSnapshot{}
		removed := map[string]bool{}
		for _, v := range s.library.Snapshots {
			if v.Workspace == w.ID && (req.Action != "delete-snapshot" || v.ID == req.Snapshot) {
				path := filepath.Join(s.root, v.Workspace, v.ID+".db")
				if err := os.Rename(path, path+".delete"); err != nil && !os.IsNotExist(err) {
					rollback()
					return nil, err
				} else if err == nil {
					staged[path] = path + ".delete"
				}
				removed[v.ID] = true
			} else {
				keep = append(keep, v)
			}
		}
		s.library.Snapshots = keep
		if removed[w.View.Snapshot] {
			w.View.Snapshot = ""
		}
		if removed[w.View.Baseline] {
			w.View.Baseline = ""
		}
		if removed[w.View.Comparison] {
			w.View.Comparison = ""
		}
		if removed[s.library.Job.Snapshot] {
			s.library.Job.Snapshot = ""
		}
		if req.Action == "delete-workspace" {
			workspaces := []cacheLabWorkspace{}
			for _, v := range s.library.Workspaces {
				if v.ID != w.ID {
					workspaces = append(workspaces, v)
				}
			}
			s.library.Workspaces = workspaces
			if s.library.Active == req.Workspace {
				s.library.Active = ""
			}
		}
		if err := s.persistLocked(); err != nil {
			rollback()
			return nil, err
		}
		manifestBefore, _ = json.Marshal(s.library)
		for _, path := range staged {
			if err := os.Remove(path); err != nil {
				return nil, fmt.Errorf("library updated but a staged file could not be removed: %w", err)
			}
		}
		if req.Action == "delete-workspace" {
			delete(s.tokens, req.Workspace)
			_ = os.Remove(filepath.Join(s.root, req.Workspace))
		}
		return map[string]bool{"ok": true}, nil
	default:
		return nil, fmt.Errorf("unknown Cache Lab action")
	}
	return map[string]bool{"ok": true}, s.persistLocked()
}
func (s *cacheLabService) addSnapshot(ctx context.Context, workspace, path string, record cacheLabSnapshot) (cacheLabSnapshot, error) {
	info, err := os.Stat(path)
	if err != nil {
		return record, err
	}
	if info.Size() > cacheLabMaxDB {
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
		record.ID = cacheLabID()
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
func (s *cacheLabService) beginJob(workspace, stage string) (context.Context, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.library.Job.Running {
		return nil, "", fmt.Errorf("a capture is already in progress")
	}
	if _, err := s.workspaceLocked(workspace); err != nil {
		return nil, "", err
	}
	if err := os.MkdirAll(filepath.Join(s.root, workspace), 0700); err != nil {
		return nil, "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	s.cancel = cancel
	s.library.Job = cacheLabJob{ID: cacheLabID(), Workspace: workspace, Running: true, Stage: stage, StartedAt: time.Now().UTC()}
	if err := s.persistLocked(); err != nil {
		cancel()
		s.library.Job.Running = false
		return nil, "", err
	}
	return ctx, s.library.Job.ID, nil
}
func (s *cacheLabService) stage(stage string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.library.Job.Stage = stage
}
func (s *cacheLabService) finishJob(record cacheLabSnapshot, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.library.Job.Running = false
	s.library.Job.Stage = "Snapshot ready"
	s.library.Job.Snapshot = ""
	if err == nil {
		s.library.Job.Snapshot = record.ID
	}
	if err != nil {
		s.library.Job.Stage = "Capture stopped"
		s.library.Job.Error = err.Error()
	}
	if persistErr := s.persistLocked(); persistErr != nil {
		s.library.Job.Error = "Could not save capture status: " + persistErr.Error()
	}
}
func (p *localControlPanel) handleCacheLabImport(s *cacheLabService, w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, cacheLabMaxDB+(2<<20))
	reader, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "choose a standalone SQLite .db file", 400)
		return
	}
	workspace := r.URL.Query().Get("workspace")
	ctx, id, err := s.beginJob(workspace, "Importing SQLite file")
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	var record cacheLabSnapshot
	stopBody := context.AfterFunc(ctx, func() { _ = r.Body.Close() })
	defer stopBody()
	defer func() { s.finishJob(record, err) }()
	path := filepath.Join(s.root, workspace, id+".partial")
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
	_, err = io.Copy(f, io.LimitReader(part, cacheLabMaxDB+1))
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	// Normalize imported standalone databases using the same snapshot primitive.
	vacuumPath := filepath.Join(s.root, workspace, cacheLabID()+".partial")
	defer os.Remove(vacuumPath)
	s.stage("Creating VACUUM INTO copy")
	err = cacheLabVacuum(ctx, path, vacuumPath)
	if err == nil {
		record, err = s.addSnapshot(ctx, workspace, vacuumPath, cacheLabSnapshot{Name: cacheLabText(filepath.Base(part.FileName()), 140), Source: "Imported file", Method: "VACUUM INTO · imported SQLite"})
	}
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	writeJSON(w, record)
}
func (p *localControlPanel) cacheLabSteve(s *cacheLabService, runID string) (any, error) {
	record, ok := p.readSteveLabRunRecord(safeRunPathSegment(runID))
	if !ok {
		return nil, fmt.Errorf("Steve session not found")
	}
	s.mu.Lock()
	workspace := ""
	for _, v := range s.library.Workspaces {
		if v.Kind == "steve" && v.URL == record.RunID {
			workspace = v.ID
		}
	}
	if workspace == "" {
		workspace = cacheLabID()
		s.library.Workspaces = append(s.library.Workspaces, cacheLabWorkspace{ID: workspace, Kind: "steve", URL: record.RunID, Name: "Steve · " + record.SteveRef, Connected: true, CreatedAt: time.Now().UTC(), Folders: []string{}, Queries: []cacheLabSavedQuery{}})
	}
	s.mu.Unlock()
	ctx, id, err := s.beginJob(workspace, "Snapshotting Steve cache")
	if err != nil {
		return nil, err
	}
	go func() {
		path := filepath.Join(s.root, workspace, id+".partial")
		defer os.Remove(path)
		var saved cacheLabSnapshot
		err := cacheLabVacuum(ctx, filepath.Join(record.SourceDir, "informer_object_cache.db"), path)
		if err == nil {
			saved, err = s.addSnapshot(ctx, workspace, path, cacheLabSnapshot{Source: record.RunID, Image: record.SteveRef, Method: "VACUUM INTO · local Steve"})
		}
		s.finishJob(saved, err)
	}()
	return map[string]string{"workspace": workspace, "job": id}, nil
}
func (s *cacheLabService) exportSnapshot(id string) (any, error) {
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
	filename := cacheLabFileLabel(cacheLabSourceLabel(record.Source)) + "--" + cacheLabFileLabel(record.Name) + "--" + record.CreatedAt.Format("20060102-150405") + "-" + cacheLabID()[:6] + ".db"
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

// Kept here to make callers treat cancellation as a user action rather than a
// successful empty snapshot.
func cacheLabContextError(err error) error {
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("capture canceled; no incomplete snapshot was saved")
	}
	return err
}

func (p *localControlPanel) stopCacheLab() {
	p.cacheLabMu.Lock()
	s := p.cacheLab
	p.cacheLabMu.Unlock()
	if s != nil {
		s.mu.Lock()
		if s.cancel != nil {
			s.cancel()
		}
		s.mu.Unlock()
	}
}

func cacheLabFileLabel(value string) string {
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
