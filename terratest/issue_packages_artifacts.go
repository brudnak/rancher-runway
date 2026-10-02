package test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/cachelab"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (s *issuePackageService) artifactPath(packageID, artifactName string) (string, error) {
	if !cachelab.IDPattern.MatchString(packageID) || !validPackageArtifactName(artifactName) {
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

func (s *issuePackageService) importPackage(pkg issuePackage, artifacts map[string][]byte) (issuePackage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.packages) >= 500 {
		return issuePackage{}, fmt.Errorf("package library limit reached (500)")
	}
	pkg = cloneIssuePackage(pkg)
	if err := validateIssuePackage(pkg); err != nil {
		return issuePackage{}, err
	}
	if pkg.OriginID == "" {
		pkg.OriginID = pkg.ID
	}
	pkg.ID = cachelab.ID()
	pkg.Revision = cachelab.ID()
	pkg.UpdatedAt = time.Now().UTC()
	referenced := map[string]issuePackageArtifact{}
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
			return issuePackage{}, fmt.Errorf("bundle artifact does not match its evidence record")
		}
	}
	// An omitted artifact remains visibly described but is not available locally;
	// the transfer layer reports omission rather than inventing a copied file.
	if err := validateIssuePackage(pkg); err != nil {
		return issuePackage{}, err
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
			return issuePackage{}, err
		}
		if err = writePrivateConfigAtomically(path, raw); err != nil {
			return issuePackage{}, err
		}
	}
	if err := s.saveLocked(pkg); err != nil {
		return issuePackage{}, err
	}
	written = true
	return cloneIssuePackage(pkg), nil
}

func generatedPackageCases(cases []issuePackageCase) []issuePackageCase {
	raw, _ := json.Marshal(cases)
	var out []issuePackageCase
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = []issuePackageCase{}
	}
	for i := range out {
		c := &out[i]
		if c.ID == "" {
			c.ID = cachelab.ID()
		}
		if c.Automation == "" {
			c.Automation = "manual"
		}
		if c.Selection == nil {
			c.Selection = []string{}
		}
		if c.Steps == nil {
			c.Steps = []issuePackageStep{}
		}
		for j := range c.Steps {
			if c.Steps[j].ID == "" {
				c.Steps[j].ID = cachelab.ID()
			}
		}
	}
	return out
}
