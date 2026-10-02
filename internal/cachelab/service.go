package cachelab

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/operations"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const MaxDB = int64(2 << 30)

var IDPattern = regexp.MustCompile(`^[a-f0-9]{24}$`)

type Workspace struct {
	ClusterID     string       `json:"clusterId,omitempty"`
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	URL           string       `json:"url"`
	Kind          string       `json:"kind"`
	Kubeconfig    string       `json:"kubeconfig,omitempty"`
	Context       string       `json:"context,omitempty"`
	Cluster       string       `json:"cluster,omitempty"`
	Namespace     string       `json:"namespace"`
	Pod           string       `json:"pod,omitempty"`
	Container     string       `json:"container,omitempty"`
	HelperPath    string       `json:"helperPath,omitempty"`
	CAPEM         string       `json:"caPem,omitempty"`
	Insecure      bool         `json:"insecure"`
	RememberToken bool         `json:"rememberToken"`
	Connected     bool         `json:"connected"`
	Notes         string       `json:"notes,omitempty"`
	Folders       []string     `json:"folders"`
	Queries       []SavedQuery `json:"queries"`
	View          cacheLabView `json:"view"`
	CreatedAt     time.Time    `json:"createdAt"`
}

type SavedQuery struct {
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

type Snapshot struct {
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

type Job struct {
	ID        string    `json:"id"`
	Workspace string    `json:"workspace"`
	Running   bool      `json:"running"`
	Stage     string    `json:"stage"`
	Error     string    `json:"error,omitempty"`
	Snapshot  string    `json:"snapshot,omitempty"`
	StartedAt time.Time `json:"startedAt"`
}

type Library struct {
	Version    int         `json:"version"`
	Active     string      `json:"active"`
	Workspaces []Workspace `json:"workspaces"`
	Snapshots  []Snapshot  `json:"snapshots"`
	Job        Job         `json:"job"`
}

func ID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func New(root string, options ...Options) (*Service, error) {
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	s := &Service{root: root, tokens: map[string]string{}, runner: cacheLabRunCommand, library: Library{Version: 1, Workspaces: []Workspace{}, Snapshots: []Snapshot{}}}
	if len(options) > 0 {
		o := options[0]
		s.workers = o.Workers
		s.resolveCluster = o.ResolveCluster
		if o.Runner != nil {
			s.runner = o.Runner
		}
	}
	b, err := os.ReadFile(filepath.Join(root, "library.json"))
	if err == nil {
		var restored Library
		if err = json.Unmarshal(b, &restored); err != nil {
			return nil, fmt.Errorf("Cache Lab library could not be read; existing snapshots were preserved: %w", err)
		}
		s.library = restored
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if s.library.Workspaces == nil {
		s.library.Workspaces = []Workspace{}
	}
	if s.library.Snapshots == nil {
		s.library.Snapshots = []Snapshot{}
	}
	if s.library.Version != 1 {
		return nil, fmt.Errorf("unsupported Cache Lab library version")
	}
	for i := range s.library.Workspaces {
		w := &s.library.Workspaces[i]
		if !IDPattern.MatchString(w.ID) {
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
		if !IDPattern.MatchString(v.ID) || !IDPattern.MatchString(v.Workspace) {
			return nil, fmt.Errorf("invalid snapshot record")
		}
	}
	deleting, _ := filepath.Glob(filepath.Join(root, "*", "*.db.delete"))
	for _, path := range deleting {
		ws := filepath.Base(filepath.Dir(path))
		id := strings.TrimSuffix(filepath.Base(path), ".db.delete")
		if !IDPattern.MatchString(ws) || !IDPattern.MatchString(id) {
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
		if IDPattern.MatchString(filepath.Base(filepath.Dir(path))) {
			_ = os.Remove(path)
		}
	}
	return s, s.persistLocked()
}

func (s *Service) persistLocked() error {
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

func (s *Service) workspaceLocked(id string) (*Workspace, error) {
	for i := range s.library.Workspaces {
		if s.library.Workspaces[i].ID == id {
			return &s.library.Workspaces[i], nil
		}
	}
	return nil, fmt.Errorf("workspace not found")
}

func (s *Service) snapshotLocked(id string) (Snapshot, string, error) {
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
	return Snapshot{}, "", fmt.Errorf("snapshot not found")
}

func Text(value string, max int) string {
	v := strings.TrimSpace(value)
	r := []rune(v)
	if len(r) > max {
		return string(r[:max])
	}
	return v
}

type Service struct {
	workers        *operations.Workers
	jobDone        func()
	resolveCluster func(string, string, string, string) (string, error)
	mu             sync.Mutex
	root           string
	library        Library
	tokens         map[string]string
	cancel         context.CancelFunc
	// Tests replace command execution; production always uses argument arrays.
	runner CommandRunner
}

const typedConfirmationPhrase = "confirm"
