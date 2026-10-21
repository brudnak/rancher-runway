package test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func (s *issuePackageService) planFolderEnvironmentLocked(pkg issuePackage) (issuePackageEnvironment, error) {
	environment := issuePackageEnvironment{Source: "manual", RecordedAt: pkg.UpdatedAt}
	raw, err := issuePackageReadBounded(filepath.Join(s.root, pkg.ID, ".plan-environment.json"), 2<<20)
	if os.IsNotExist(err) {
		return environment, nil
	}
	if err != nil {
		return environment, err
	}
	if decodeIssuePackage(raw, &environment) != nil || validateIssuePackageEnvironment(environment) != nil {
		return environment, fmt.Errorf("saved plan environment is invalid; existing generated files were preserved")
	}
	environment.URL = ""
	environment.HelmCommand = sanitizeIssuePackageHelmCommand(environment.HelmCommand)
	return environment, nil
}

func (s *issuePackageService) rememberPlanEnvironment(id, revision string, environment issuePackageEnvironment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	pkg, found := s.packages[id]
	if !found {
		return fmt.Errorf("issue package not found")
	}
	if revision != "" && revision != pkg.Revision {
		return errIssuePackageConflict
	}
	if err := validateIssuePackageEnvironment(environment); err != nil {
		return err
	}
	// This is only the public template snapshot. The private target endpoint
	// and credentials are never required to rebuild Markdown or sync a plan.
	environment.URL = ""
	environment.HelmCommand = sanitizeIssuePackageHelmCommand(environment.HelmCommand)
	raw, err := json.MarshalIndent(environment, "", "  ")
	if err != nil {
		return err
	}
	if err := writePrivateConfigAtomically(filepath.Join(s.root, id, ".plan-environment.json"), raw); err != nil {
		return err
	}
	s.rememberPlanFolderLocked(pkg)
	return nil
}

func (p *localControlPanel) issuePackagePlanEnvironment(s *issuePackageService, req issuePackageRequest) (issuePackageEnvironment, error) {
	if req.Environment.ClusterID == "" {
		s.mu.Lock()
		pkg, found := s.packages[req.ID]
		cached, cacheErr := s.planFolderEnvironmentLocked(pkg)
		s.mu.Unlock()
		if found && cacheErr != nil {
			return cached, cacheErr
		}
		if found && cached.Source == "imported" {
			return cached, nil
		}
	}
	return p.issuePackageEnvironment(req.Environment)
}

func (p *localControlPanel) handleIssuePackagePlanFolder(s *issuePackageService, req issuePackageRequest, open func(string, bool) error) (any, error) {
	if req.Action != "plan-folder-history-open" {
		environment, err := p.issuePackagePlanEnvironment(s, req)
		if err != nil {
			return nil, err
		}
		if err := s.rememberPlanEnvironment(req.ID, req.Revision, environment); err != nil {
			return nil, err
		}
	}
	return s.handleIssuePackagePlanFolder(req, open)
}
