package test

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/cachelab"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

func issuePackageReadBounded(path string, max int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > max {
		return nil, fmt.Errorf("package file is not a regular file or exceeds its size limit")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, fmt.Errorf("package file changed while opening")
	}
	raw, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > max {
		return nil, fmt.Errorf("package file exceeds its size limit")
	}
	return raw, nil
}

const issuePackageManifestLimit = 8 << 20

const issuePackageArtifactLimit = 32 << 20

const issuePackageTotalLimit = 64 << 20

type issuePackage struct {
	ID         string               `json:"id"`
	OriginID   string               `json:"originId,omitempty"`
	Revision   string               `json:"revision"`
	Title      string               `json:"title"`
	IssueURL   string               `json:"issueUrl"`
	IssueTitle string               `json:"issueTitle"`
	FixURL     string               `json:"fixUrl"`
	FixTitle   string               `json:"fixTitle"`
	Summary    string               `json:"summary"`
	Status     string               `json:"status"`
	Notes      string               `json:"notes"`
	Cases      []issuePackageCase    `json:"cases"`
	Sessions   []issuePackageSession `json:"sessions"`
	CreatedAt  time.Time            `json:"createdAt"`
	UpdatedAt  time.Time            `json:"updatedAt"`
}

type issuePackageCase struct {
	ID               string                       `json:"id"`
	Title            string                       `json:"title"`
	Preconditions    string                       `json:"preconditions"`
	Expected         string                       `json:"expected"`
	Automation       string                       `json:"automation"`
	AutomationURL    string                       `json:"automationUrl,omitempty"`
	Selection        []string                     `json:"selection"`
	AutomationSource *issuePackageAutomationSource `json:"automationSource,omitempty"`
	Steps            []issuePackageStep            `json:"steps"`
}

type issuePackageStep struct {
	ID          string `json:"id"`
	Instruction string `json:"instruction"`
	Expected    string `json:"expected"`
}

type issuePackageSession struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Purpose     string                 `json:"purpose"`
	FixURL      string                 `json:"fixUrl,omitempty"`
	FixTitle    string                 `json:"fixTitle,omitempty"`
	FixCommit   string                 `json:"fixCommit,omitempty"`
	Status      string                 `json:"status"`
	StartedAt   time.Time              `json:"startedAt"`
	FinishedAt  time.Time              `json:"finishedAt,omitempty"`
	Conclusion  string                 `json:"conclusion"`
	Finding     string                 `json:"finding"`
	Environment issuePackageEnvironment `json:"environment"`
	Cases       []issuePackageCase      `json:"cases"`
	Results     []issuePackageResult    `json:"results"`
	Evidence    []issuePackageEvidence  `json:"evidence"`
	Baseline    *issuePackageBaseline   `json:"baseline,omitempty"`
}

type issuePackageResult struct {
	CaseID  string                  `json:"caseId"`
	Outcome string                  `json:"outcome"`
	Notes   string                  `json:"notes"`
	Steps   []issuePackageStepResult `json:"steps"`
}

type issuePackageStepResult struct {
	StepID string    `json:"stepId"`
	Done   bool      `json:"done"`
	At     time.Time `json:"at,omitempty"`
}

type issuePackageEnvironment struct {
	ClusterID         string                         `json:"clusterId"`
	ClusterName       string                         `json:"clusterName"`
	URL               string                         `json:"url"`
	RancherVersion    string                         `json:"rancherVersion"`
	KubernetesVersion string                         `json:"kubernetesVersion"`
	Images            []string                       `json:"images"`
	Configuration     string                         `json:"configuration"`
	Source            string                         `json:"source"`
	RecordedAt        time.Time                      `json:"recordedAt"`
	Details           []issuePackageEnvironmentDetail `json:"details,omitempty"`
	HelmCommand       string                         `json:"helmCommand,omitempty"`
	ObservedAt        *time.Time                     `json:"observedAt,omitempty"`
	Warnings          []string                       `json:"warnings,omitempty"`
}

type issuePackageEnvironmentDetail struct {
	Label      string     `json:"label"`
	Value      string     `json:"value"`
	Source     string     `json:"source"`
	ObservedAt *time.Time `json:"observedAt,omitempty"`
}

type issuePackageArtifact struct {
	Omitted   bool   `json:"omitted,omitempty"`
	Name      string `json:"name"`
	MediaType string `json:"mediaType"`
	Bytes     int64  `json:"bytes"`
	SHA256    string `json:"sha256"`
}

type issuePackageEvidence struct {
	ID          string               `json:"id"`
	Kind        string               `json:"kind"`
	Name        string               `json:"name"`
	Description string               `json:"description"`
	SourceID    string               `json:"sourceId"`
	WorkspaceID string               `json:"workspaceId"`
	ClusterID   string               `json:"clusterId"`
	CapturedAt  time.Time            `json:"capturedAt"`
	AttachedAt  time.Time            `json:"attachedAt"`
	CaseID      string               `json:"caseId,omitempty"`
	StepID      string               `json:"stepId,omitempty"`
	Metadata    json.RawMessage      `json:"metadata"`
	Artifact    *issuePackageArtifact `json:"artifact,omitempty"`
}

type issuePackageRequest struct {
	Library           *issuePackageLibrary      `json:"library,omitempty"`
	BucketID          string                   `json:"bucketId"`
	IncludeEvidence   bool                     `json:"includeEvidence"`
	Action            string                   `json:"action"`
	ID                string                   `json:"id"`
	Revision          string                   `json:"revision"`
	Confirm           string                   `json:"confirm"`
	Title             string                   `json:"title"`
	IssueURL          string                   `json:"issueUrl"`
	IssueTitle        string                   `json:"issueTitle"`
	FixURL            string                   `json:"fixUrl"`
	FixTitle          string                   `json:"fixTitle"`
	FixCommit         string                   `json:"fixCommit"`
	Summary           string                   `json:"summary"`
	Status            string                   `json:"status"`
	Notes             string                   `json:"notes"`
	Cases             []issuePackageCase        `json:"cases"`
	SessionID         string                   `json:"sessionId"`
	BaselineSessionID string                   `json:"baselineSessionId"`
	DraftRevision     string                   `json:"draftRevision"`
	Draft             *issuePackageWritingDraft `json:"draft,omitempty"`
	Name              string                   `json:"name"`
	Purpose           string                   `json:"purpose"`
	Environment       issuePackageEnvironment   `json:"environment"`
	CaseIDs           []string                 `json:"caseIds"`
	CaseID            string                   `json:"caseId"`
	StepID            string                   `json:"stepId"`
	Done              bool                     `json:"done"`
	Outcome           string                   `json:"outcome"`
	Conclusion        string                   `json:"conclusion"`
	Finding           string                   `json:"finding"`
	Kind              string                   `json:"kind"`
	SourceID          string                   `json:"sourceId"`
	WorkspaceID       string                   `json:"workspaceId"`
	Description       string                   `json:"description"`
	IncludeDatabase   bool                     `json:"includeDatabase"`
	Repository        string                   `json:"repository"`
	RemoteSHA         string                   `json:"remoteSha"`
	ReviewToken       string                   `json:"reviewToken"`
	IncludeNotes      bool                     `json:"includeNotes"`
	Bundle            *issuePackageBundle       `json:"bundle,omitempty"`
	ArtifactNames     []string                 `json:"artifactNames"`
	Pod               string                   `json:"pod"`
	PodUID            string                   `json:"podUid"`
	Container         string                   `json:"container"`
	TailLines         int64                    `json:"tailLines"`
	SinceSeconds      int64                    `json:"sinceSeconds"`
	Previous          bool                     `json:"previous"`
}

type issuePackageService struct {
	mu                sync.Mutex
	root              string
	exportRoot        string
	packages          map[string]issuePackage
	library           issuePackageLibrary
	pendingAutomation map[string]issuePackageRunLink
}

var errIssuePackageConflict = errors.New("this package changed in another view; reload it before saving")

func (p *localControlPanel) issuePackageService() (*issuePackageService, error) {
	p.issuePackagesMu.Lock()
	defer p.issuePackagesMu.Unlock()
	if p.issuePackages != nil {
		return p.issuePackages, nil
	}
	root, err := absoluteFromWorkingDir(durableDataPath("issue-packages"))
	if err != nil {
		return nil, err
	}
	s, err := newIssuePackageService(root)
	if err == nil {
		p.issuePackages = s
	}
	return s, err
}

func newIssuePackageService(root string) (*issuePackageService, error) {
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	s := &issuePackageService{root: root, packages: map[string]issuePackage{}, pendingAutomation: map[string]issuePackageRunLink{}}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if !entry.IsDir() || !cachelab.IDPattern.MatchString(entry.Name()) {
			return nil, fmt.Errorf("unrecognized Issue Package data %q; existing files were preserved", entry.Name())
		}
		raw, err := issuePackageReadBounded(filepath.Join(root, entry.Name(), "package.json"), issuePackageManifestLimit)
		if err != nil {
			return nil, fmt.Errorf("Issue Package %s could not be read; existing files were preserved: %w", entry.Name(), err)
		}
		var pkg issuePackage
		if err = decodeIssuePackage(raw, &pkg); err != nil {
			return nil, fmt.Errorf("Issue Package %s is invalid; existing files were preserved: %w", entry.Name(), err)
		}
		if pkg.ID != entry.Name() {
			return nil, fmt.Errorf("Issue Package identity does not match its directory; existing files were preserved")
		}
		if err = validateIssuePackage(pkg); err != nil {
			return nil, fmt.Errorf("Issue Package %s is invalid; existing files were preserved: %w", entry.Name(), err)
		}
		s.packages[pkg.ID] = normalizeIssuePackage(pkg)
	}
	if err := s.loadPackageLibrary(); err != nil {
		return nil, err
	}
	return s, nil
}

func decodeIssuePackage(raw []byte, target any) error {
	if err := issuePackageRejectDuplicateKeys(raw); err != nil {
		return err
	}
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return fmt.Errorf("unexpected extra JSON data")
	}
	return nil
}

func normalizeIssuePackage(pkg issuePackage) issuePackage {
	if pkg.Cases == nil {
		pkg.Cases = []issuePackageCase{}
	}
	if pkg.Sessions == nil {
		pkg.Sessions = []issuePackageSession{}
	}
	normalizeCases := func(cases []issuePackageCase) {
		for i := range cases {
			if cases[i].Steps == nil {
				cases[i].Steps = []issuePackageStep{}
			}
			if cases[i].Selection == nil {
				cases[i].Selection = []string{}
			}
		}
	}
	normalizeCases(pkg.Cases)
	for i := range pkg.Sessions {
		session := &pkg.Sessions[i]
		if session.Baseline != nil {
			normalizeCases(session.Baseline.Cases)
			if session.Baseline.Evidence == nil {
				session.Baseline.Evidence = []issuePackageBaselineEvidence{}
			}
			if session.Baseline.Environment.Images == nil {
				session.Baseline.Environment.Images = []string{}
			}
			for j := range session.Baseline.Results {
				if session.Baseline.Results[j].Steps == nil {
					session.Baseline.Results[j].Steps = []issuePackageStepResult{}
				}
			}
		}
		normalizeCases(session.Cases)
		if session.Results == nil {
			session.Results = []issuePackageResult{}
		}
		if session.Evidence == nil {
			session.Evidence = []issuePackageEvidence{}
		}
		if session.Environment.Images == nil {
			session.Environment.Images = []string{}
		}
		for j := range session.Results {
			if session.Results[j].Steps == nil {
				session.Results[j].Steps = []issuePackageStepResult{}
			}
		}
	}
	return pkg
}

func cloneIssuePackage(pkg issuePackage) issuePackage {
	raw, _ := json.Marshal(pkg)
	var out issuePackage
	_ = json.Unmarshal(raw, &out)
	return normalizeIssuePackage(out)
}

func (s *issuePackageService) list() ([]issuePackage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]issuePackage, 0, len(s.packages))
	for _, pkg := range s.packages {
		out = append(out, cloneIssuePackage(pkg))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

func (s *issuePackageService) get(id string) (issuePackage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pkg, ok := s.packages[id]
	if !ok {
		return issuePackage{}, fmt.Errorf("issue package not found")
	}
	return cloneIssuePackage(pkg), nil
}

func (s *issuePackageService) saveLocked(pkg issuePackage) error {
	if err := validateIssuePackage(pkg); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return err
	}
	if len(raw) > issuePackageManifestLimit {
		return fmt.Errorf("package exceeds 8 MiB; export and split this investigation")
	}
	if err = writePrivateConfigAtomically(filepath.Join(s.root, pkg.ID, "package.json"), raw); err != nil {
		return err
	}
	s.packages[pkg.ID] = cloneIssuePackage(pkg)
	return nil
}
