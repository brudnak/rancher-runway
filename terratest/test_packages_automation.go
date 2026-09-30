package test

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strings"
	"time"
)

type testPackageAutomationSource struct {
	Name    string `json:"name"`
	SHA     string `json:"sha"`
	Ref     string `json:"ref"`
	Tags    string `json:"tags"`
	Timeout int    `json:"timeout"`
}
type testPackageRunLink struct {
	PackageID string `json:"packageId"`
	SessionID string `json:"sessionId"`
	CaseID    string `json:"caseId"`
}

func validatePackageAutomation(c testPackageCase) error {
	if c.AutomationSource == nil {
		return nil
	}
	a := c.AutomationSource
	if !packageText(a.Name, 240, true) || len(a.SHA) != 40 || !packageCommitSHA(a.SHA) || !packageText(a.Ref, 240, true) || !packageText(a.Tags, 500, false) || a.Timeout < 1 || a.Timeout > 240 || len(c.Selection) == 0 {
		return fmt.Errorf("invalid pinned automation source")
	}
	return nil
}
func (s *testPackageService) automationCaseLocked(link testPackageRunLink) (testPackageSession, testPackageCase, error) {
	pkg, ok := s.packages[link.PackageID]
	if !ok {
		return testPackageSession{}, testPackageCase{}, fmt.Errorf("test package no longer exists")
	}
	for _, session := range pkg.Sessions {
		if session.ID != link.SessionID {
			continue
		}
		if session.Status != "active" {
			return session, testPackageCase{}, fmt.Errorf("start an active session before running linked automation")
		}
		for _, c := range session.Cases {
			if c.ID == link.CaseID {
				if c.AutomationSource == nil || len(c.Selection) == 0 {
					return session, c, fmt.Errorf("link a saved Test Lab plan before starting this session")
				}
				return session, c, validatePackageAutomation(c)
			}
		}
	}
	return testPackageSession{}, testPackageCase{}, fmt.Errorf("linked case or session no longer exists")
}
func (s *testPackageService) preparePackageAutomation(req testPackageRequest) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	link := testPackageRunLink{PackageID: req.ID, SessionID: req.SessionID, CaseID: req.CaseID}
	session, c, err := s.automationCaseLocked(link)
	if err != nil {
		return nil, err
	}
	return map[string]any{"packageLink": link, "source": c.AutomationSource, "selection": c.Selection, "environment": session.Environment, "caseTitle": c.Title}, nil
}
func packageAutomationHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Host)
}
func (p *localControlPanel) startPackageLinkedRun(lab *testLabService, req testLabRequest) (any, error) {
	if req.PackageLink == nil {
		return lab.startRun(req)
	}
	packages, err := p.testPackageService()
	if err != nil {
		return nil, err
	}
	packages.mu.Lock()
	defer packages.mu.Unlock()
	session, c, err := packages.automationCaseLocked(*req.PackageLink)
	if err != nil {
		return nil, err
	}
	host, _, err := testLabConfig(req.Config)
	if err != nil {
		return nil, err
	}
	if packageAutomationHost(session.Environment.URL) == "" || packageAutomationHost(session.Environment.URL) != packageAutomationHost(host) || (session.Environment.ClusterID != "" && req.ClusterID != session.Environment.ClusterID) {
		return nil, fmt.Errorf("the automation target must match this session's recorded Rancher and cluster")
	}
	a := c.AutomationSource
	if req.SHA != a.SHA || !reflect.DeepEqual(req.Selection, c.Selection) || req.Tags != a.Tags || req.Timeout != a.Timeout {
		return nil, fmt.Errorf("the linked run must use this case's frozen source, selections, tags, and timeout; detach it to run another experiment")
	}
	result, err := lab.startRun(req)
	if err == nil {
		run := result.(testLabRun)
		packages.pendingAutomation[run.ID] = *req.PackageLink
	}
	return result, err
}

// Called after Test Lab releases its lock. Retry is idempotent, including after restart.
func (p *localControlPanel) preservePackageRun(run testLabRun) {
	if run.PackageLink == nil || run.Status == "running" {
		return
	}
	s, err := p.testPackageService()
	if err == nil {
		req := testPackageRequest{Kind: "test-run", SourceID: run.ID, CaseID: run.PackageLink.CaseID, Description: "Automated run of the session's linked case. Review the recorded test results before choosing the case outcome."}
		var evidence testPackageEvidence
		var raw []byte
		evidence, raw, err = p.testPackageEvidence(req)
		if err == nil {
			err = s.preserveAutomationEvidence(run, evidence, raw)
		}
		s.mu.Lock()
		delete(s.pendingAutomation, run.ID)
		s.mu.Unlock()
	}
	lab, labErr := p.testLabService()
	if labErr != nil {
		return
	}
	lab.mu.Lock()
	defer lab.mu.Unlock()
	saved := lab.runLocked(run.ID)
	if saved == nil {
		return
	}
	saved.PackageEvidenceSaved = err == nil
	saved.PackageEvidenceError = ""
	if err != nil {
		saved.PackageEvidenceError = err.Error()
	}
	_ = lab.persistLocked()
}
func (s *testPackageService) preserveAutomationEvidence(run testLabRun, e testPackageEvidence, raw []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	link := run.PackageLink
	if link == nil {
		return fmt.Errorf("missing package link")
	}
	pkg, ok := s.packages[link.PackageID]
	if !ok {
		return fmt.Errorf("package was removed; result remains in Test Lab")
	}
	pkg = cloneTestPackage(pkg)
	for i := range pkg.Sessions {
		session := &pkg.Sessions[i]
		if session.ID != link.SessionID {
			continue
		}
		for _, existing := range session.Evidence {
			if existing.Kind == "test-run" && existing.SourceID == run.ID && existing.CaseID == link.CaseID {
				return nil
			}
		}
		if session.Status != "active" {
			return fmt.Errorf("session already preserved; result remains in Test Lab")
		}
		found := false
		for _, c := range session.Cases {
			if c.ID == link.CaseID {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("case no longer exists")
		}
		if (session.Environment.ClusterID != "" && session.Environment.ClusterID != run.ClusterID) || packageAutomationHost(session.Environment.URL) != packageAutomationHost(run.Host) {
			return fmt.Errorf("run target changed; result remains in Test Lab")
		}
		session.Evidence = append(session.Evidence, e)
		pkg.Revision = cacheLabID()
		pkg.UpdatedAt = time.Now().UTC()
		if err := validateTestPackage(pkg); err != nil {
			return err
		}
		path := ""
		if e.Artifact != nil {
			var err error
			path, err = s.artifactPath(pkg.ID, e.Artifact.Name)
			if err != nil {
				return err
			}
			if err = writePrivateConfigAtomically(path, raw); err != nil {
				return err
			}
		}
		if err := s.saveLocked(pkg); err != nil {
			if path != "" {
				_ = os.Remove(path)
			}
			return err
		}
		return nil
	}
	return fmt.Errorf("session was removed; result remains in Test Lab")
}
func (p *localControlPanel) syncPackageRuns() {
	lab, err := p.testLabService()
	if err != nil {
		return
	}
	lab.mu.Lock()
	runs := []testLabRun{}
	for _, r := range lab.library.Runs {
		if r.PackageLink != nil && r.Status != "running" && !r.PackageEvidenceSaved {
			raw, _ := json.Marshal(r)
			var copy testLabRun
			_ = json.Unmarshal(raw, &copy)
			runs = append(runs, copy)
		}
	}
	lab.mu.Unlock()
	for _, r := range runs {
		p.preservePackageRun(r)
	}
}
