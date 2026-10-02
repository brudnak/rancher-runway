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

func testPackageReadBounded(path string, max int64) ([]byte, error) {
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

const testPackageManifestLimit = 8 << 20

const testPackageArtifactLimit = 32 << 20

const testPackageTotalLimit = 64 << 20

type testPackage struct {
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
	Cases      []testPackageCase    `json:"cases"`
	Sessions   []testPackageSession `json:"sessions"`
	CreatedAt  time.Time            `json:"createdAt"`
	UpdatedAt  time.Time            `json:"updatedAt"`
}

type testPackageCase struct {
	ID               string                       `json:"id"`
	Title            string                       `json:"title"`
	Preconditions    string                       `json:"preconditions"`
	Expected         string                       `json:"expected"`
	Automation       string                       `json:"automation"`
	AutomationURL    string                       `json:"automationUrl,omitempty"`
	Selection        []string                     `json:"selection"`
	AutomationSource *testPackageAutomationSource `json:"automationSource,omitempty"`
	Steps            []testPackageStep            `json:"steps"`
}

type testPackageStep struct {
	ID          string `json:"id"`
	Instruction string `json:"instruction"`
	Expected    string `json:"expected"`
}

type testPackageSession struct {
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
	Environment testPackageEnvironment `json:"environment"`
	Cases       []testPackageCase      `json:"cases"`
	Results     []testPackageResult    `json:"results"`
	Evidence    []testPackageEvidence  `json:"evidence"`
	Baseline    *testPackageBaseline   `json:"baseline,omitempty"`
}

type testPackageResult struct {
	CaseID  string                  `json:"caseId"`
	Outcome string                  `json:"outcome"`
	Notes   string                  `json:"notes"`
	Steps   []testPackageStepResult `json:"steps"`
}

type testPackageStepResult struct {
	StepID string    `json:"stepId"`
	Done   bool      `json:"done"`
	At     time.Time `json:"at,omitempty"`
}

type testPackageEnvironment struct {
	ClusterID         string                         `json:"clusterId"`
	ClusterName       string                         `json:"clusterName"`
	URL               string                         `json:"url"`
	RancherVersion    string                         `json:"rancherVersion"`
	KubernetesVersion string                         `json:"kubernetesVersion"`
	Images            []string                       `json:"images"`
	Configuration     string                         `json:"configuration"`
	Source            string                         `json:"source"`
	RecordedAt        time.Time                      `json:"recordedAt"`
	Details           []testPackageEnvironmentDetail `json:"details,omitempty"`
	HelmCommand       string                         `json:"helmCommand,omitempty"`
	ObservedAt        *time.Time                     `json:"observedAt,omitempty"`
	Warnings          []string                       `json:"warnings,omitempty"`
}

type testPackageEnvironmentDetail struct {
	Label      string     `json:"label"`
	Value      string     `json:"value"`
	Source     string     `json:"source"`
	ObservedAt *time.Time `json:"observedAt,omitempty"`
}

type testPackageArtifact struct {
	Omitted   bool   `json:"omitted,omitempty"`
	Name      string `json:"name"`
	MediaType string `json:"mediaType"`
	Bytes     int64  `json:"bytes"`
	SHA256    string `json:"sha256"`
}

type testPackageEvidence struct {
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
	Artifact    *testPackageArtifact `json:"artifact,omitempty"`
}

type testPackageRequest struct {
	Library           *testPackageLibrary      `json:"library,omitempty"`
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
	Cases             []testPackageCase        `json:"cases"`
	SessionID         string                   `json:"sessionId"`
	BaselineSessionID string                   `json:"baselineSessionId"`
	DraftRevision     string                   `json:"draftRevision"`
	Draft             *testPackageWritingDraft `json:"draft,omitempty"`
	Name              string                   `json:"name"`
	Purpose           string                   `json:"purpose"`
	Environment       testPackageEnvironment   `json:"environment"`
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
	Bundle            *testPackageBundle       `json:"bundle,omitempty"`
	ArtifactNames     []string                 `json:"artifactNames"`
	Pod               string                   `json:"pod"`
	PodUID            string                   `json:"podUid"`
	Container         string                   `json:"container"`
	TailLines         int64                    `json:"tailLines"`
	SinceSeconds      int64                    `json:"sinceSeconds"`
	Previous          bool                     `json:"previous"`
}

type testPackageService struct {
	mu                sync.Mutex
	root              string
	exportRoot        string
	packages          map[string]testPackage
	library           testPackageLibrary
	pendingAutomation map[string]testPackageRunLink
}

var errTestPackageConflict = errors.New("this package changed in another view; reload it before saving")

func (p *localControlPanel) testPackageService() (*testPackageService, error) {
	p.testPackagesMu.Lock()
	defer p.testPackagesMu.Unlock()
	if p.testPackages != nil {
		return p.testPackages, nil
	}
	root, err := absoluteFromWorkingDir(durableDataPath("test-packages"))
	if err != nil {
		return nil, err
	}
	s, err := newTestPackageService(root)
	if err == nil {
		p.testPackages = s
	}
	return s, err
}

func newTestPackageService(root string) (*testPackageService, error) {
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	s := &testPackageService{root: root, packages: map[string]testPackage{}, pendingAutomation: map[string]testPackageRunLink{}}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if !entry.IsDir() || !cachelab.IDPattern.MatchString(entry.Name()) {
			return nil, fmt.Errorf("unrecognized Test Package data %q; existing files were preserved", entry.Name())
		}
		raw, err := testPackageReadBounded(filepath.Join(root, entry.Name(), "package.json"), testPackageManifestLimit)
		if err != nil {
			return nil, fmt.Errorf("Test Package %s could not be read; existing files were preserved: %w", entry.Name(), err)
		}
		var pkg testPackage
		if err = decodeTestPackage(raw, &pkg); err != nil {
			return nil, fmt.Errorf("Test Package %s is invalid; existing files were preserved: %w", entry.Name(), err)
		}
		if pkg.ID != entry.Name() {
			return nil, fmt.Errorf("Test Package identity does not match its directory; existing files were preserved")
		}
		if err = validateTestPackage(pkg); err != nil {
			return nil, fmt.Errorf("Test Package %s is invalid; existing files were preserved: %w", entry.Name(), err)
		}
		s.packages[pkg.ID] = normalizeTestPackage(pkg)
	}
	if err := s.loadPackageLibrary(); err != nil {
		return nil, err
	}
	return s, nil
}

func decodeTestPackage(raw []byte, target any) error {
	if err := testPackageRejectDuplicateKeys(raw); err != nil {
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

func normalizeTestPackage(pkg testPackage) testPackage {
	if pkg.Cases == nil {
		pkg.Cases = []testPackageCase{}
	}
	if pkg.Sessions == nil {
		pkg.Sessions = []testPackageSession{}
	}
	normalizeCases := func(cases []testPackageCase) {
		for i := range cases {
			if cases[i].Steps == nil {
				cases[i].Steps = []testPackageStep{}
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
				session.Baseline.Evidence = []testPackageBaselineEvidence{}
			}
			if session.Baseline.Environment.Images == nil {
				session.Baseline.Environment.Images = []string{}
			}
			for j := range session.Baseline.Results {
				if session.Baseline.Results[j].Steps == nil {
					session.Baseline.Results[j].Steps = []testPackageStepResult{}
				}
			}
		}
		normalizeCases(session.Cases)
		if session.Results == nil {
			session.Results = []testPackageResult{}
		}
		if session.Evidence == nil {
			session.Evidence = []testPackageEvidence{}
		}
		if session.Environment.Images == nil {
			session.Environment.Images = []string{}
		}
		for j := range session.Results {
			if session.Results[j].Steps == nil {
				session.Results[j].Steps = []testPackageStepResult{}
			}
		}
	}
	return pkg
}

func cloneTestPackage(pkg testPackage) testPackage {
	raw, _ := json.Marshal(pkg)
	var out testPackage
	_ = json.Unmarshal(raw, &out)
	return normalizeTestPackage(out)
}

func (s *testPackageService) list() ([]testPackage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]testPackage, 0, len(s.packages))
	for _, pkg := range s.packages {
		out = append(out, cloneTestPackage(pkg))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

func (s *testPackageService) get(id string) (testPackage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pkg, ok := s.packages[id]
	if !ok {
		return testPackage{}, fmt.Errorf("test package not found")
	}
	return cloneTestPackage(pkg), nil
}

func (s *testPackageService) saveLocked(pkg testPackage) error {
	if err := validateTestPackage(pkg); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return err
	}
	if len(raw) > testPackageManifestLimit {
		return fmt.Errorf("package exceeds 8 MiB; export and split this investigation")
	}
	if err = writePrivateConfigAtomically(filepath.Join(s.root, pkg.ID, "package.json"), raw); err != nil {
		return err
	}
	s.packages[pkg.ID] = cloneTestPackage(pkg)
	return nil
}
