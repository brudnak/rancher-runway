package test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/cachelab"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type issuePackageTestRunMetadata struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	ClusterID    string          `json:"clusterId"`
	SHA          string          `json:"sha"`
	Ref          string          `json:"ref"`
	Selection    []string        `json:"selection"`
	Tags         string          `json:"tags"`
	Timeout      int             `json:"timeout"`
	Status       string          `json:"status"`
	Stage        string          `json:"stage"`
	StartedAt    time.Time       `json:"startedAt"`
	FinishedAt   time.Time       `json:"finishedAt"`
	Results      []testLabResult `json:"results"`
	LogAvailable bool            `json:"logAvailable"`
}

type issuePackageCacheMetadata struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	WorkspaceID      string    `json:"workspaceId"`
	CreatedAt        time.Time `json:"createdAt"`
	Bytes            int64     `json:"bytes"`
	SHA256           string    `json:"sha256"`
	Pod              string    `json:"pod"`
	PodUID           string    `json:"podUid"`
	Restarts         int       `json:"restarts"`
	Image            string    `json:"image"`
	Method           string    `json:"method"`
	Tables           int       `json:"tables"`
	DatabaseIncluded bool      `json:"databaseIncluded"`
}

func (p *localControlPanel) issuePackageEnvironment(input issuePackageEnvironment) (issuePackageEnvironment, error) {
	e := issuePackageEnvironment{ClusterID: input.ClusterID, ClusterName: input.ClusterName, URL: input.URL, RancherVersion: input.RancherVersion, KubernetesVersion: input.KubernetesVersion, Images: append([]string{}, input.Images...), Configuration: input.Configuration, Source: "manual", RecordedAt: time.Now().UTC()}
	if e.ClusterID != "" {
		records, err := p.labClusterCandidates()
		if err != nil {
			return e, err
		}
		p.mu.Lock()
		cluster, known := p.clusterSnapshot[e.ClusterID]
		cluster = cloneClusterDeploymentView(cluster)
		p.mu.Unlock()
		found := false
		for _, record := range records {
			if record.ID != e.ClusterID {
				continue
			}
			found = true
			e.ClusterName = record.Name
			if record.Nickname != "" {
				e.ClusterName = record.Nickname
			}
			e.URL = record.URL
			e.Source = "recorded"
			packageEnvironmentDetail(&e, "Configured version", record.Version, "recorded", nil)
			packageEnvironmentDetail(&e, "Cluster role", record.Role, "recorded", nil)
			packageEnvironmentDetail(&e, "Run", record.RunID, "recorded", nil)
			role := record.Role
			if known && cluster.Type == "downstream" {
				role = "downstream"
			}
			if role == "" && strings.Contains(record.ID, "-downstream-") {
				role = "downstream"
			}
			if record.Version != "" {
				switch {
				case role == "k3d" || role == "downstream":
					e.KubernetesVersion = record.Version
				case role == "steve":
					e.Configuration = strings.TrimSpace(e.Configuration + "\nSteve reference (local record): " + record.Version)
				case packageEnum(role, "local", "docker", "host", "tenant") || (known && cluster.Type == "local") || (role == "" && record.URL != "" && !strings.HasPrefix(strings.TrimPrefix(record.Version, "v"), "1.")):
					e.RancherVersion = record.Version
				}
			}
			break
		}
		if !found {
			return e, fmt.Errorf("cluster workspace no longer exists")
		}
		// Copy cached observations only. Never probe Kubernetes, and do not discard
		// manually recorded image details when the local record has no observations.
		if known {
			seen := map[string]bool{}
			images := []string{}
			for _, pod := range cluster.Pods {
				for _, image := range pod.Images {
					for _, value := range []string{image.Image, image.ImageID} {
						if value != "" && !seen[value] {
							images = append(images, value)
							seen[value] = true
						}
					}
				}
			}
			if len(images) > 0 {
				sort.Strings(images)
				e.Images = images
			}
		}
		p.enrichIssuePackageEnvironment(&e, cluster, known)
	}
	return e, validateIssuePackageEnvironment(e)
}

func (p *localControlPanel) issuePackageEvidence(req issuePackageRequest) (issuePackageEvidence, []byte, error) {
	now := time.Now().UTC()
	e := issuePackageEvidence{ID: cachelab.ID(), Kind: req.Kind, Name: req.Name, Description: req.Description, SourceID: req.SourceID, WorkspaceID: req.WorkspaceID, CaseID: req.CaseID, StepID: req.StepID, CapturedAt: now, AttachedAt: now, Metadata: json.RawMessage(`{}`)}
	var raw []byte
	switch req.Kind {
	case "note":
		if !packageText(req.Name, 500, true) || !packageText(req.Description, 32000, true) {
			return e, nil, fmt.Errorf("give the observation a title and description")
		}
		e.SourceID = ""
		e.WorkspaceID = ""
	case "test-run":
		s, err := p.testLabService()
		if err != nil {
			return e, nil, err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		found := false
		for _, run := range s.library.Runs {
			if run.ID != req.SourceID {
				continue
			}
			if !cachelab.IDPattern.MatchString(run.ID) {
				return e, nil, fmt.Errorf("invalid test run identity")
			}
			if run.Status == "running" || run.FinishedAt.IsZero() {
				return e, nil, fmt.Errorf("wait for this test run to finish before preserving its evidence")
			}
			found = true
			e.Name = run.Name
			if e.Name == "" {
				e.Name = "Test run " + run.ID
			}
			e.ClusterID = run.ClusterID
			e.CapturedAt = run.FinishedAt
			logPath := filepath.Join(s.root, run.ID+".log")
			raw, err = issuePackageReadBounded(logPath, 8<<20)
			if os.IsNotExist(err) {
				if log, ok := s.logs[run.ID]; ok {
					if len(log) > 8<<20 {
						return e, nil, fmt.Errorf("test log exceeds 8 MiB; attach a note and export the log separately")
					}
					raw = []byte(log)
				}
				err = nil
			}
			if err != nil {
				return e, nil, fmt.Errorf("could not preserve test log: %w", err)
			}
			// Test Lab already strips known configuration values. This additional pass
			// masks recognizable API tokens; arbitrary user text still needs review.
			raw = []byte(testLabTokenPattern.ReplaceAllString(string(raw), "[redacted]"))
			metadata := issuePackageTestRunMetadata{ID: run.ID, Name: run.Name, ClusterID: run.ClusterID, SHA: run.SHA, Ref: run.Ref, Selection: append([]string{}, run.Selection...), Tags: run.Tags, Timeout: run.Timeout, Status: run.Status, Stage: run.Stage, StartedAt: run.StartedAt, FinishedAt: run.FinishedAt, Results: append([]testLabResult{}, run.Results...), LogAvailable: len(raw) > 0}
			e.Metadata, _ = json.Marshal(metadata)
			break
		}
		if !found {
			return e, nil, fmt.Errorf("test run no longer exists")
		}
		if len(raw) > 0 {
			e.Artifact = packageArtifact(e.ID+".log", "text/plain", raw)
		}
	case "cache-snapshot":
		s, err := p.cacheLabService()
		if err != nil {
			return e, nil, err
		}
		err = s.ReadEvidence(func(library cachelab.Library, snapshotPath func(string) (cachelab.Snapshot, string, error)) error {
			found := false
			for _, snapshot := range library.Snapshots {
				if snapshot.ID != req.SourceID || snapshot.Workspace != req.WorkspaceID {
					continue
				}
				if !cachelab.IDPattern.MatchString(snapshot.ID) || !cachelab.IDPattern.MatchString(snapshot.Workspace) {
					return fmt.Errorf("invalid cache snapshot identity")
				}
				found = true
				e.Name = snapshot.Name
				if e.Name == "" {
					e.Name = "Cache snapshot " + snapshot.ID
				}
				e.CapturedAt = snapshot.CreatedAt
				for _, workspace := range library.Workspaces {
					if workspace.ID == snapshot.Workspace {
						e.ClusterID = workspace.ClusterID
						break
					}
				}
				metadata := issuePackageCacheMetadata{ID: snapshot.ID, Name: snapshot.Name, WorkspaceID: snapshot.Workspace, CreatedAt: snapshot.CreatedAt, Bytes: snapshot.Bytes, SHA256: snapshot.SHA256, Pod: snapshot.Pod, PodUID: snapshot.PodUID, Restarts: snapshot.Restarts, Image: snapshot.Image, Method: snapshot.Method, Tables: snapshot.Tables, DatabaseIncluded: req.IncludeDatabase}
				if req.IncludeDatabase {
					_, path, err := snapshotPath(snapshot.ID)
					if err != nil {
						return err
					}
					raw, err = issuePackageReadBounded(path, issuePackageArtifactLimit)
					if err != nil {
						return fmt.Errorf("database cannot be preserved (32 MiB maximum); attach metadata only and export the database separately: %w", err)
					}
					if len(raw) < 16 || string(raw[:16]) != "SQLite format 3\x00" {
						return fmt.Errorf("snapshot is not a SQLite database")
					}
					e.Artifact = packageArtifact(e.ID+".db", "application/vnd.sqlite3", raw)
					if snapshot.SHA256 != "" && snapshot.SHA256 != e.Artifact.SHA256 {
						return fmt.Errorf("snapshot checksum changed; evidence was not attached")
					}
				}
				e.Metadata, _ = json.Marshal(metadata)
				break
			}
			if !found {
				return fmt.Errorf("cache snapshot no longer exists")
			}
			return nil
		})
		if err != nil {
			return e, nil, err
		}

	default:
		return e, nil, fmt.Errorf("choose a test result, cache snapshot, or observation")
	}
	return e, raw, nil
}

func packageArtifact(name, mediaType string, raw []byte) *issuePackageArtifact {
	sum := sha256.Sum256(raw)
	return &issuePackageArtifact{Name: name, MediaType: mediaType, Bytes: int64(len(raw)), SHA256: hex.EncodeToString(sum[:])}
}
