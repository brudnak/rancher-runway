package test

import (
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/cachelab"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (s *issuePackageService) mutate(req issuePackageRequest, environment *issuePackageEnvironment, evidence *issuePackageEvidence, artifact []byte) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	if req.Action == "create" {
		if len(s.packages) >= 500 {
			return nil, fmt.Errorf("package library limit reached (500)")
		}
		pkg := issuePackage{ID: cachelab.ID(), Revision: cachelab.ID(), Title: strings.TrimSpace(req.Title), IssueURL: strings.TrimSpace(req.IssueURL), IssueTitle: req.IssueTitle, FixURL: strings.TrimSpace(req.FixURL), FixTitle: req.FixTitle, Summary: req.Summary, Status: "planning", Cases: []issuePackageCase{}, Sessions: []issuePackageSession{}, CreatedAt: now, UpdatedAt: now}
		if err := s.saveLocked(pkg); err != nil {
			_ = os.RemoveAll(filepath.Join(s.root, pkg.ID))
			return nil, err
		}
		return map[string]any{"package": cloneIssuePackage(pkg), "currentPlanFingerprint": issuePackagePlanFingerprint(issuePackageEditablePlan(pkg))}, nil
	}
	current, ok := s.packages[req.ID]
	if !ok {
		return nil, fmt.Errorf("issue package not found")
	}
	if req.Revision == "" || req.Revision != current.Revision {
		return nil, errIssuePackageConflict
	}
	pkg := cloneIssuePackage(current)
	if req.Action == "delete" {
		if req.Confirm != typedConfirmationPhrase {
			return nil, fmt.Errorf("type confirm to delete this package and its local evidence")
		}
		dir := filepath.Join(s.root, pkg.ID)
		if info, err := os.Lstat(dir); err != nil || !info.IsDir() {
			return nil, fmt.Errorf("package directory is unavailable")
		}
		tombstone := filepath.Join(s.root, ".deleted-"+pkg.ID+"-"+cachelab.ID())
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
		var baseline *issuePackageBaseline
		if req.BaselineSessionID != "" {
			if len(req.CaseIDs) != 0 {
				return nil, fmt.Errorf("validation starts with every frozen baseline case; a partial baseline cannot be selected")
			}
			var err error
			baseline, err = issuePackageSnapshotBaseline(pkg.Sessions, req.BaselineSessionID, now)
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
		cases := []issuePackageCase{}
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
		session := issuePackageSession{ID: cachelab.ID(), Name: strings.TrimSpace(req.Name), Purpose: req.Purpose, FixURL: strings.TrimSpace(req.FixURL), FixTitle: req.FixTitle, FixCommit: strings.ToLower(strings.TrimSpace(req.FixCommit)), Status: "active", StartedAt: now, Finding: "inconclusive", Environment: *environment, Cases: cases, Results: []issuePackageResult{}, Evidence: []issuePackageEvidence{}, Baseline: baseline}
		for _, c := range cases {
			result := issuePackageResult{CaseID: c.ID, Outcome: "not-run", Steps: []issuePackageStepResult{}}
			for _, step := range c.Steps {
				result.Steps = append(result.Steps, issuePackageStepResult{StepID: step.ID})
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
				return nil, fmt.Errorf("unknown Issue Packages action")
			}
		}
	}
	pkg.Revision = cachelab.ID()
	pkg.UpdatedAt = now
	if err := validateIssuePackage(pkg); err != nil {
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
	return map[string]any{"package": cloneIssuePackage(pkg), "currentPlanFingerprint": issuePackagePlanFingerprint(issuePackageEditablePlan(pkg))}, nil
}
