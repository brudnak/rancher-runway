package test

// Test Packages preserve a local, revisioned test plan and its execution history.
// Cluster inspection and log collection are explicit, read-only actions. Existing lab evidence is copied
// into the package so ordinary lab cleanup cannot erase an investigation.
import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
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
	root, err := absoluteFromWorkingDir(filepath.Join(automationOutputDir(), "control-panel", "test-packages"))
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
		if !entry.IsDir() || !cacheLabIDPattern.MatchString(entry.Name()) {
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
func packageEnum(value string, options ...string) bool {
	for _, option := range options {
		if value == option {
			return true
		}
	}
	return false
}
func packageText(value string, max int, required bool) bool {
	return utf8.ValidString(value) && len(value) <= max && !strings.ContainsRune(value, 0) && (!required || strings.TrimSpace(value) != "")
}
func packageURL(value string) bool {
	if value == "" {
		return true
	}
	u, e := url.Parse(value)
	return e == nil && packageEnum(u.Scheme, "http", "https") && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && len(value) <= 2048
}
func validateTestPackageCases(cases []testPackageCase) error {
	if len(cases) > 250 {
		return fmt.Errorf("a package can contain at most 250 cases")
	}
	ids := map[string]bool{}
	for _, c := range cases {
		if !cacheLabIDPattern.MatchString(c.ID) || ids[c.ID] || !packageText(c.Title, 240, true) || !packageText(c.Preconditions, 16000, false) || !packageText(c.Expected, 16000, false) || !packageEnum(c.Automation, "manual", "planned", "automated") || !packageURL(c.AutomationURL) || len(c.Steps) > 100 || len(c.Selection) > 500 {
			return fmt.Errorf("invalid test case")
		}
		if err := validatePackageAutomation(c); err != nil {
			return err
		}
		ids[c.ID] = true
		stepIDs := map[string]bool{}
		for _, step := range c.Steps {
			if !cacheLabIDPattern.MatchString(step.ID) || stepIDs[step.ID] || !packageText(step.Instruction, 16000, true) || !packageText(step.Expected, 16000, false) {
				return fmt.Errorf("invalid or duplicate test step")
			}
			stepIDs[step.ID] = true
		}
		for _, selection := range c.Selection {
			if !packageText(selection, 1024, true) {
				return fmt.Errorf("invalid automated test selection")
			}
		}
	}
	return nil
}
func validateTestPackageEnvironment(e testPackageEnvironment) error {
	if parsed, err := url.Parse(e.URL); err != nil || parsed.Fragment != "" {
		return fmt.Errorf("environment URL must not contain fragments or credentials")
	}
	if !packageText(e.ClusterID, 512, false) || !packageText(e.ClusterName, 240, false) || !packageURL(e.URL) || !packageText(e.RancherVersion, 240, false) || !packageText(e.KubernetesVersion, 240, false) || !packageText(e.Configuration, 16000, false) || !packageEnum(e.Source, "recorded", "manual", "imported") || len(e.Images) > 500 || e.RecordedAt.IsZero() {
		return fmt.Errorf("invalid session environment")
	}
	for _, image := range e.Images {
		if !packageText(image, 4096, true) {
			return fmt.Errorf("invalid environment image")
		}
	}
	if !packageText(e.HelmCommand, 64000, false) || len(e.Details) > 500 || len(e.Warnings) > 30 || (e.ObservedAt != nil && e.ObservedAt.IsZero()) {
		return fmt.Errorf("invalid environment details")
	}
	for _, detail := range e.Details {
		if !packageText(detail.Label, 500, true) || !packageText(detail.Value, 8192, true) || !packageEnum(detail.Source, "recorded", "observed", "manual") || (detail.ObservedAt != nil && detail.ObservedAt.IsZero()) {
			return fmt.Errorf("invalid environment detail")
		}
	}
	for _, warning := range e.Warnings {
		if !packageText(warning, 1000, true) {
			return fmt.Errorf("invalid environment warning")
		}
	}
	return nil
}
func validPackageArtifactName(name string) bool {
	ext := filepath.Ext(name)
	return packageEnum(ext, ".log", ".db") && cacheLabIDPattern.MatchString(strings.TrimSuffix(name, ext))
}
func validateTestPackage(pkg testPackage) error {
	if !cacheLabIDPattern.MatchString(pkg.ID) || !cacheLabIDPattern.MatchString(pkg.Revision) || (pkg.OriginID != "" && !cacheLabIDPattern.MatchString(pkg.OriginID)) || !packageText(pkg.Title, 240, true) || !packageURL(pkg.IssueURL) || !packageText(pkg.IssueTitle, 500, false) || !packageURL(pkg.FixURL) || !packageText(pkg.FixTitle, 500, false) || !packageText(pkg.Summary, 32000, false) || !packageText(pkg.Notes, 100000, false) || !packageEnum(pkg.Status, "planning", "reproducing", "awaiting-fix", "validating", "verified", "archived") || pkg.CreatedAt.IsZero() || pkg.UpdatedAt.IsZero() || len(pkg.Sessions) > 500 {
		return fmt.Errorf("invalid test package")
	}
	if err := validateTestPackageCases(pkg.Cases); err != nil {
		return err
	}
	ids := map[string]bool{}
	artifactNames := map[string]bool{}
	var total int64
	for _, session := range pkg.Sessions {
		if !cacheLabIDPattern.MatchString(session.ID) || ids[session.ID] || !packageText(session.Name, 240, true) || !packageEnum(session.Purpose, "reproduction", "validation", "exploration") || !packageURL(session.FixURL) || !packageText(session.FixTitle, 500, false) || !packageCommitSHA(session.FixCommit) || !packageEnum(session.Status, "active", "completed") || !packageEnum(session.Finding, "inconclusive", "reproduced", "not-reproduced", "validated", "not-validated") || session.StartedAt.IsZero() || !packageText(session.Conclusion, 32000, false) || len(session.Cases) == 0 || len(session.Evidence) > 250 {
			return fmt.Errorf("invalid test session")
		}
		ids[session.ID] = true
		if err := validateTestPackageBaseline(session); err != nil {
			return err
		}
		if session.Status == "completed" && (session.FinishedAt.IsZero() || session.FinishedAt.Before(session.StartedAt)) {
			return fmt.Errorf("completed session has invalid timestamps")
		}
		if err := validateTestPackageCases(session.Cases); err != nil {
			return err
		}
		if err := validateTestPackageEnvironment(session.Environment); err != nil {
			return err
		}
		cases := map[string]testPackageCase{}
		for _, c := range session.Cases {
			cases[c.ID] = c
		}
		results := map[string]bool{}
		for _, result := range session.Results {
			c, ok := cases[result.CaseID]
			if !ok || results[result.CaseID] || !packageEnum(result.Outcome, "not-run", "passed", "failed", "blocked", "skipped") || !packageText(result.Notes, 32000, false) {
				return fmt.Errorf("invalid session result")
			}
			results[result.CaseID] = true
			steps := map[string]bool{}
			for _, step := range c.Steps {
				steps[step.ID] = true
			}
			seen := map[string]bool{}
			for _, step := range result.Steps {
				if !steps[step.StepID] || seen[step.StepID] || (step.Done && step.At.IsZero()) {
					return fmt.Errorf("invalid session step marker")
				}
				seen[step.StepID] = true
			}
			if len(seen) != len(steps) {
				return fmt.Errorf("session result is missing step markers")
			}
		}
		if len(results) != len(cases) {
			return fmt.Errorf("session is missing case results")
		}
		evidenceIDs := map[string]bool{}
		for _, e := range session.Evidence {
			if !cacheLabIDPattern.MatchString(e.ID) || evidenceIDs[e.ID] || !packageEnum(e.Kind, "test-run", "cache-snapshot", "note", "pod-logs") || !packageText(e.Name, 500, true) || !packageText(e.Description, 32000, false) || !packageText(e.SourceID, 512, false) || !packageText(e.WorkspaceID, 512, false) || !packageText(e.ClusterID, 512, false) || e.AttachedAt.IsZero() || e.CapturedAt.IsZero() || len(e.Metadata) > 1<<20 || !json.Valid(e.Metadata) {
				return fmt.Errorf("invalid session evidence")
			}
			evidenceIDs[e.ID] = true
			if err := validateTestPackageEvidenceMetadata(e); err != nil {
				return err
			}
			if e.CaseID != "" {
				c, ok := cases[e.CaseID]
				if !ok {
					return fmt.Errorf("evidence refers to a missing case")
				}
				if e.StepID != "" {
					found := false
					for _, step := range c.Steps {
						found = found || step.ID == e.StepID
					}
					if !found {
						return fmt.Errorf("evidence refers to a missing step")
					}
				}
			} else if e.StepID != "" {
				return fmt.Errorf("step evidence needs a case")
			}
			if e.Kind == "note" && e.Artifact != nil {
				return fmt.Errorf("written observations cannot contain binary artifacts")
			}
			if a := e.Artifact; a != nil {
				if (packageEnum(e.Kind, "test-run", "pod-logs") && (a.MediaType != "text/plain" || filepath.Ext(a.Name) != ".log")) || (e.Kind == "cache-snapshot" && (a.MediaType != "application/vnd.sqlite3" || filepath.Ext(a.Name) != ".db")) {
					return fmt.Errorf("artifact format does not match its evidence kind")
				}
				if !validPackageArtifactName(a.Name) || artifactNames[a.Name] || a.Bytes < 0 || a.Bytes > testPackageArtifactLimit || len(a.SHA256) != 64 || !packageEnum(a.MediaType, "text/plain", "application/vnd.sqlite3") {
					return fmt.Errorf("invalid evidence artifact")
				}
				if _, err := hex.DecodeString(a.SHA256); err != nil {
					return fmt.Errorf("invalid artifact checksum")
				}
				artifactNames[a.Name] = true
				total += a.Bytes
			}
		}
	}
	if total > testPackageTotalLimit {
		return fmt.Errorf("package evidence exceeds 64 MiB; attach metadata or use a separate package")
	}
	return nil
}

func (s *testPackageService) artifactPath(packageID, artifactName string) (string, error) {
	if !cacheLabIDPattern.MatchString(packageID) || !validPackageArtifactName(artifactName) {
		return "", fmt.Errorf("invalid package artifact identity")
	}
	path := filepath.Join(s.root, packageID, "artifacts", artifactName)
	for _, dir := range []string{filepath.Join(s.root, packageID), filepath.Join(s.root, packageID, "artifacts")} {
		if info, err := os.Lstat(dir); err == nil && !info.IsDir() {
			return "", fmt.Errorf("package artifact directory is not a regular directory")
		} else if err != nil && !os.IsNotExist(err) {
			return "", err
		}
	}
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return "", fmt.Errorf("package artifact is not a regular file")
	} else if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return path, nil
}
func (s *testPackageService) importPackage(pkg testPackage, artifacts map[string][]byte) (testPackage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.packages) >= 500 {
		return testPackage{}, fmt.Errorf("package library limit reached (500)")
	}
	pkg = cloneTestPackage(pkg)
	if err := validateTestPackage(pkg); err != nil {
		return testPackage{}, err
	}
	if pkg.OriginID == "" {
		pkg.OriginID = pkg.ID
	}
	pkg.ID = cacheLabID()
	pkg.Revision = cacheLabID()
	pkg.UpdatedAt = time.Now().UTC()
	referenced := map[string]testPackageArtifact{}
	for i := range pkg.Sessions {
		session := &pkg.Sessions[i]
		session.Environment.Source = "imported"
		if session.Baseline != nil {
			session.Baseline.Environment.Source = "imported"
		}
		if session.Status == "active" {
			session.Status = "completed"
			session.FinishedAt = time.Now().UTC()
			session.Finding = "inconclusive"
			session.Conclusion = strings.TrimSpace(session.Conclusion + "\n\nImported while active; execution was not resumed. This is a preserved, incomplete attempt.")
		}
		for j := range session.Evidence {
			e := &session.Evidence[j]
			if e.Artifact != nil {
				referenced[e.Artifact.Name] = *e.Artifact
				_, present := artifacts[e.Artifact.Name]
				e.Artifact.Omitted = !present
			}
		}
	}
	for name, raw := range artifacts {
		a, ok := referenced[name]
		sum := sha256.Sum256(raw)
		if !ok || a.Bytes != int64(len(raw)) || a.SHA256 != hex.EncodeToString(sum[:]) {
			return testPackage{}, fmt.Errorf("bundle artifact does not match its evidence record")
		}
	}
	// An omitted artifact remains visibly described but is not available locally;
	// the transfer layer reports omission rather than inventing a copied file.
	if err := validateTestPackage(pkg); err != nil {
		return testPackage{}, err
	}
	written := false
	defer func() {
		if !written {
			_ = os.RemoveAll(filepath.Join(s.root, pkg.ID))
		}
	}()
	for name, raw := range artifacts {
		path, err := s.artifactPath(pkg.ID, name)
		if err != nil {
			return testPackage{}, err
		}
		if err = writePrivateConfigAtomically(path, raw); err != nil {
			return testPackage{}, err
		}
	}
	if err := s.saveLocked(pkg); err != nil {
		return testPackage{}, err
	}
	written = true
	return cloneTestPackage(pkg), nil
}
func generatedPackageCases(cases []testPackageCase) []testPackageCase {
	raw, _ := json.Marshal(cases)
	var out []testPackageCase
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = []testPackageCase{}
	}
	for i := range out {
		c := &out[i]
		if c.ID == "" {
			c.ID = cacheLabID()
		}
		if c.Automation == "" {
			c.Automation = "manual"
		}
		if c.Selection == nil {
			c.Selection = []string{}
		}
		if c.Steps == nil {
			c.Steps = []testPackageStep{}
		}
		for j := range c.Steps {
			if c.Steps[j].ID == "" {
				c.Steps[j].ID = cacheLabID()
			}
		}
	}
	return out
}
func (s *testPackageService) mutate(req testPackageRequest, environment *testPackageEnvironment, evidence *testPackageEvidence, artifact []byte) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	if req.Action == "create" {
		if len(s.packages) >= 500 {
			return nil, fmt.Errorf("package library limit reached (500)")
		}
		pkg := testPackage{ID: cacheLabID(), Revision: cacheLabID(), Title: strings.TrimSpace(req.Title), IssueURL: strings.TrimSpace(req.IssueURL), IssueTitle: req.IssueTitle, FixURL: strings.TrimSpace(req.FixURL), FixTitle: req.FixTitle, Summary: req.Summary, Status: "planning", Cases: []testPackageCase{}, Sessions: []testPackageSession{}, CreatedAt: now, UpdatedAt: now}
		if err := s.saveLocked(pkg); err != nil {
			_ = os.RemoveAll(filepath.Join(s.root, pkg.ID))
			return nil, err
		}
		return map[string]any{"package": cloneTestPackage(pkg), "currentPlanFingerprint": testPackagePlanFingerprint(testPackageEditablePlan(pkg))}, nil
	}
	current, ok := s.packages[req.ID]
	if !ok {
		return nil, fmt.Errorf("test package not found")
	}
	if req.Revision == "" || req.Revision != current.Revision {
		return nil, errTestPackageConflict
	}
	pkg := cloneTestPackage(current)
	if req.Action == "delete" {
		if req.Confirm != typedConfirmationPhrase {
			return nil, fmt.Errorf("type confirm to delete this package and its local evidence")
		}
		dir := filepath.Join(s.root, pkg.ID)
		if info, err := os.Lstat(dir); err != nil || !info.IsDir() {
			return nil, fmt.Errorf("package directory is unavailable")
		}
		tombstone := filepath.Join(s.root, ".deleted-"+pkg.ID+"-"+cacheLabID())
		if err := os.Rename(dir, tombstone); err != nil {
			return nil, err
		}
		delete(s.packages, pkg.ID)
		if err := os.RemoveAll(tombstone); err != nil {
			return map[string]any{"ok": true, "warning": "Package removed; some hidden local files could not be removed."}, nil
		}
		return map[string]bool{"ok": true}, nil
	}
	var cleanupArtifacts []string
	switch req.Action {
	case "update":
		pkg.Title = strings.TrimSpace(req.Title)
		pkg.IssueURL = strings.TrimSpace(req.IssueURL)
		pkg.IssueTitle = req.IssueTitle
		pkg.FixURL = strings.TrimSpace(req.FixURL)
		pkg.FixTitle = req.FixTitle
		pkg.Summary = req.Summary
		pkg.Status = req.Status
		pkg.Notes = req.Notes
		pkg.Cases = generatedPackageCases(req.Cases)
	case "start-session":
		if environment == nil {
			return nil, fmt.Errorf("session environment is missing")
		}
		if len(pkg.Sessions) >= 500 {
			return nil, fmt.Errorf("session limit reached")
		}
		wanted := map[string]bool{}
		caseSource := pkg.Cases
		var baseline *testPackageBaseline
		if req.BaselineSessionID != "" {
			if len(req.CaseIDs) != 0 {
				return nil, fmt.Errorf("validation starts with every frozen baseline case; a partial baseline cannot be selected")
			}
			var err error
			baseline, err = testPackageSnapshotBaseline(pkg.Sessions, req.BaselineSessionID, now)
			if err != nil {
				return nil, err
			}
			caseSource = baseline.Cases
			req.Purpose = "validation"
		}
		for _, id := range req.CaseIDs {
			if wanted[id] {
				return nil, fmt.Errorf("duplicate selected case")
			}
			wanted[id] = true
		}
		cases := []testPackageCase{}
		for _, c := range caseSource {
			if len(req.CaseIDs) == 0 || wanted[c.ID] {
				cases = append(cases, c)
				delete(wanted, c.ID)
			}
		}
		if len(wanted) > 0 {
			return nil, fmt.Errorf("selected case no longer exists")
		}
		if len(cases) == 0 {
			return nil, fmt.Errorf("write at least one case before starting a session")
		}
		session := testPackageSession{ID: cacheLabID(), Name: strings.TrimSpace(req.Name), Purpose: req.Purpose, FixURL: strings.TrimSpace(req.FixURL), FixTitle: req.FixTitle, FixCommit: strings.ToLower(strings.TrimSpace(req.FixCommit)), Status: "active", StartedAt: now, Finding: "inconclusive", Environment: *environment, Cases: cases, Results: []testPackageResult{}, Evidence: []testPackageEvidence{}, Baseline: baseline}
		for _, c := range cases {
			result := testPackageResult{CaseID: c.ID, Outcome: "not-run", Steps: []testPackageStepResult{}}
			for _, step := range c.Steps {
				result.Steps = append(result.Steps, testPackageStepResult{StepID: step.ID})
			}
			session.Results = append(session.Results, result)
		}
		pkg.Sessions = append(pkg.Sessions, session)
	default:
		idx := -1
		for i := range pkg.Sessions {
			if pkg.Sessions[i].ID == req.SessionID {
				idx = i
				break
			}
		}
		if idx < 0 {
			return nil, fmt.Errorf("test session not found")
		}
		session := &pkg.Sessions[idx]
		if req.Action == "delete-session" {
			if req.Confirm != typedConfirmationPhrase {
				return nil, fmt.Errorf("type confirm to delete this session")
			}
			for _, e := range session.Evidence {
				if e.Artifact != nil {
					cleanupArtifacts = append(cleanupArtifacts, e.Artifact.Name)
				}
			}
			pkg.Sessions = append(pkg.Sessions[:idx], pkg.Sessions[idx+1:]...)
		} else {
			if session.Status != "active" {
				return nil, fmt.Errorf("completed sessions are preserved; start a new session to record another attempt")
			}
			switch req.Action {
			case "mark-step", "case-result":
				resultIndex := -1
				for i := range session.Results {
					if session.Results[i].CaseID == req.CaseID {
						resultIndex = i
						break
					}
				}
				if resultIndex < 0 {
					return nil, fmt.Errorf("case does not belong to this session")
				}
				result := &session.Results[resultIndex]
				if req.Action == "case-result" {
					result.Outcome = req.Outcome
					result.Notes = req.Notes
				} else {
					found := false
					for i := range result.Steps {
						step := &result.Steps[i]
						if step.StepID == req.StepID {
							step.Done = req.Done
							step.At = now
							found = true
							break
						}
					}
					if !found {
						return nil, fmt.Errorf("step does not belong to this case")
					}
				}
			case "finish-session":
				for _, link := range s.pendingAutomation {
					if link.PackageID == pkg.ID && link.SessionID == session.ID {
						return nil, fmt.Errorf("wait for the linked Test Lab run to finish before preserving this session")
					}
				}
				if !packageEnum(req.Finding, "inconclusive", "reproduced", "not-reproduced", "validated", "not-validated") {
					return nil, fmt.Errorf("choose a finding before completing this session")
				}
				session.Status = "completed"
				session.FinishedAt = now
				session.Finding = req.Finding
				session.Conclusion = req.Conclusion
			case "attach-evidence":
				if evidence == nil {
					return nil, fmt.Errorf("evidence is missing")
				}
				if evidence.ClusterID != "" && session.Environment.ClusterID != "" && evidence.ClusterID != session.Environment.ClusterID {
					return nil, fmt.Errorf("this evidence belongs to a different cluster; attach evidence from this session's target")
				}
				session.Evidence = append(session.Evidence, *evidence)
			case "remove-evidence":
				if req.Confirm != typedConfirmationPhrase {
					return nil, fmt.Errorf("type confirm to remove this evidence")
				}
				found := false
				for i, e := range session.Evidence {
					if e.ID == req.SourceID {
						if e.Artifact != nil {
							cleanupArtifacts = append(cleanupArtifacts, e.Artifact.Name)
						}
						session.Evidence = append(session.Evidence[:i], session.Evidence[i+1:]...)
						found = true
						break
					}
				}
				if !found {
					return nil, fmt.Errorf("evidence not found")
				}
			default:
				return nil, fmt.Errorf("unknown Test Packages action")
			}
		}
	}
	pkg.Revision = cacheLabID()
	pkg.UpdatedAt = now
	if err := validateTestPackage(pkg); err != nil {
		return nil, err
	}
	artifactPath := ""
	if evidence != nil && evidence.Artifact != nil {
		var err error
		artifactPath, err = s.artifactPath(pkg.ID, evidence.Artifact.Name)
		if err != nil {
			return nil, err
		}
		if err = writePrivateConfigAtomically(artifactPath, artifact); err != nil {
			return nil, err
		}
	}
	if err := s.saveLocked(pkg); err != nil {
		if artifactPath != "" {
			_ = os.Remove(artifactPath)
		}
		return nil, err
	}
	for _, name := range cleanupArtifacts {
		if path, err := s.artifactPath(pkg.ID, name); err == nil {
			_ = os.Remove(path)
		}
	}
	return map[string]any{"package": cloneTestPackage(pkg), "currentPlanFingerprint": testPackagePlanFingerprint(testPackageEditablePlan(pkg))}, nil
}

type testPackageTestRunMetadata struct {
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
type testPackageCacheMetadata struct {
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

func (p *localControlPanel) testPackageEnvironment(input testPackageEnvironment) (testPackageEnvironment, error) {
	e := testPackageEnvironment{ClusterID: input.ClusterID, ClusterName: input.ClusterName, URL: input.URL, RancherVersion: input.RancherVersion, KubernetesVersion: input.KubernetesVersion, Images: append([]string{}, input.Images...), Configuration: input.Configuration, Source: "manual", RecordedAt: time.Now().UTC()}
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
		p.enrichTestPackageEnvironment(&e, cluster, known)
	}
	return e, validateTestPackageEnvironment(e)
}

func (p *localControlPanel) testPackageEvidence(req testPackageRequest) (testPackageEvidence, []byte, error) {
	now := time.Now().UTC()
	e := testPackageEvidence{ID: cacheLabID(), Kind: req.Kind, Name: req.Name, Description: req.Description, SourceID: req.SourceID, WorkspaceID: req.WorkspaceID, CaseID: req.CaseID, StepID: req.StepID, CapturedAt: now, AttachedAt: now, Metadata: json.RawMessage(`{}`)}
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
			if !cacheLabIDPattern.MatchString(run.ID) {
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
			raw, err = testPackageReadBounded(logPath, 8<<20)
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
			metadata := testPackageTestRunMetadata{ID: run.ID, Name: run.Name, ClusterID: run.ClusterID, SHA: run.SHA, Ref: run.Ref, Selection: append([]string{}, run.Selection...), Tags: run.Tags, Timeout: run.Timeout, Status: run.Status, Stage: run.Stage, StartedAt: run.StartedAt, FinishedAt: run.FinishedAt, Results: append([]testLabResult{}, run.Results...), LogAvailable: len(raw) > 0}
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
		s.mu.Lock()
		defer s.mu.Unlock()
		found := false
		for _, snapshot := range s.library.Snapshots {
			if snapshot.ID != req.SourceID || snapshot.Workspace != req.WorkspaceID {
				continue
			}
			if !cacheLabIDPattern.MatchString(snapshot.ID) || !cacheLabIDPattern.MatchString(snapshot.Workspace) {
				return e, nil, fmt.Errorf("invalid cache snapshot identity")
			}
			found = true
			e.Name = snapshot.Name
			if e.Name == "" {
				e.Name = "Cache snapshot " + snapshot.ID
			}
			e.CapturedAt = snapshot.CreatedAt
			for _, workspace := range s.library.Workspaces {
				if workspace.ID == snapshot.Workspace {
					e.ClusterID = workspace.ClusterID
					break
				}
			}
			metadata := testPackageCacheMetadata{ID: snapshot.ID, Name: snapshot.Name, WorkspaceID: snapshot.Workspace, CreatedAt: snapshot.CreatedAt, Bytes: snapshot.Bytes, SHA256: snapshot.SHA256, Pod: snapshot.Pod, PodUID: snapshot.PodUID, Restarts: snapshot.Restarts, Image: snapshot.Image, Method: snapshot.Method, Tables: snapshot.Tables, DatabaseIncluded: req.IncludeDatabase}
			if req.IncludeDatabase {
				_, path, err := s.snapshotLocked(snapshot.ID)
				if err != nil {
					return e, nil, err
				}
				raw, err = testPackageReadBounded(path, testPackageArtifactLimit)
				if err != nil {
					return e, nil, fmt.Errorf("database cannot be preserved (32 MiB maximum); attach metadata only and export the database separately: %w", err)
				}
				if len(raw) < 16 || string(raw[:16]) != "SQLite format 3\x00" {
					return e, nil, fmt.Errorf("snapshot is not a SQLite database")
				}
				e.Artifact = packageArtifact(e.ID+".db", "application/vnd.sqlite3", raw)
				if snapshot.SHA256 != "" && snapshot.SHA256 != e.Artifact.SHA256 {
					return e, nil, fmt.Errorf("snapshot checksum changed; evidence was not attached")
				}
			}
			e.Metadata, _ = json.Marshal(metadata)
			break
		}
		if !found {
			return e, nil, fmt.Errorf("cache snapshot no longer exists")
		}
	default:
		return e, nil, fmt.Errorf("choose a test result, cache snapshot, or observation")
	}
	return e, raw, nil
}
func packageArtifact(name, mediaType string, raw []byte) *testPackageArtifact {
	sum := sha256.Sum256(raw)
	return &testPackageArtifact{Name: name, MediaType: mediaType, Bytes: int64(len(raw)), SHA256: hex.EncodeToString(sum[:])}
}
func (p *localControlPanel) handleTestPackages(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", 405)
		return
	}
	if (r.Method == http.MethodGet && !p.authorizedLocalBrowserRead(r)) || (r.Method == http.MethodPost && !p.authorizedLocalAction(r)) {
		http.Error(w, "unauthorized", 401)
		return
	}
	s, err := p.testPackageService()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if r.Method == http.MethodGet {
		p.syncPackageRuns()
		packages, err := s.list()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		fingerprints := map[string]string{}
		for _, pkg := range packages {
			fingerprints[pkg.ID] = testPackagePlanFingerprint(testPackageEditablePlan(pkg))
		}
		writeJSON(w, map[string]any{"packages": packages, "planFingerprints": fingerprints, "library": s.packageLibrarySnapshot()})
		return
	}
	var req testPackageRequest
	rawRequest, readErr := io.ReadAll(http.MaxBytesReader(w, r.Body, 96<<20))
	if readErr != nil || decodeTestPackage(rawRequest, &req) != nil {
		http.Error(w, "invalid Test Packages request", 400)
		return
	}
	var result any
	var environment *testPackageEnvironment
	var evidence *testPackageEvidence
	var artifact []byte
	switch req.Action {
	case "start-session":
		var value testPackageEnvironment
		value, err = p.testPackageEnvironment(req.Environment)
		environment = &value
	case "attach-evidence":
		var value testPackageEvidence
		value, artifact, err = p.testPackageEvidence(req)
		evidence = &value
	}
	if err == nil {
		switch req.Action {
		case "library-save":
			result, err = s.savePackageLibrary(req)
		case "library-export":
			result, err = s.exportPackageLibrary(req)
		case "automation-prepare":
			result, err = s.preparePackageAutomation(req)
		case "draft-read", "draft-write", "draft-clear":
			result, err = s.handleWritingDraft(req)
		case "read-evidence":
			result, err = s.readEvidence(req)
		case "preview-environment":
			var value testPackageEnvironment
			value, err = p.testPackageEnvironment(req.Environment)
			result = map[string]any{"environment": value}
		case "refresh-environment":
			var value testPackageEnvironment
			value, err = p.refreshTestPackageEnvironment(r.Context(), req.Environment)
			result = map[string]any{"environment": value}
		case "log-targets", "capture-logs":
			result, err = p.handleTestPackageLogs(r.Context(), s, req)
		case "fix-lookup":
			result, err = p.testPackageFixLookup(r.Context(), req)
		case "create", "update", "delete", "start-session", "mark-step", "case-result", "finish-session", "delete-session", "attach-evidence", "remove-evidence":
			result, err = s.mutate(req, environment, evidence, artifact)
		default:
			result, err = p.handleTestPackageTransfer(r.Context(), s, req)
		}
	}
	if err != nil {
		code := 400
		if errors.Is(err, errTestPackageConflict) {
			code = 409
		}
		http.Error(w, err.Error(), code)
		return
	}
	writeJSON(w, result)
}

func (s *testPackageService) readEvidence(req testPackageRequest) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pkg, ok := s.packages[req.ID]
	if !ok {
		return nil, fmt.Errorf("test package not found")
	}
	for _, session := range pkg.Sessions {
		if session.ID != req.SessionID {
			continue
		}
		for _, e := range session.Evidence {
			if e.ID != req.SourceID {
				continue
			}
			if e.Artifact == nil || e.Artifact.Omitted {
				return nil, fmt.Errorf("this evidence has metadata only; no log file was included")
			}
			if e.Artifact.MediaType != "text/plain" {
				return nil, fmt.Errorf("only text evidence can be opened in the log reader")
			}
			path, err := s.artifactPath(pkg.ID, e.Artifact.Name)
			if err != nil {
				return nil, err
			}
			raw, err := testPackageReadBounded(path, 8<<20)
			if err != nil {
				return nil, fmt.Errorf("preserved log is unavailable: %w", err)
			}
			sum := sha256.Sum256(raw)
			if int64(len(raw)) != e.Artifact.Bytes || hex.EncodeToString(sum[:]) != e.Artifact.SHA256 {
				return nil, fmt.Errorf("preserved log checksum changed; the file was not opened")
			}
			return map[string]any{"text": string(raw), "name": e.Name, "mediaType": e.Artifact.MediaType}, nil
		}
	}
	return nil, fmt.Errorf("evidence not found")
}
