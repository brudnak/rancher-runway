package test

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/cachelab"
	"net/url"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

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
		if !cachelab.IDPattern.MatchString(c.ID) || ids[c.ID] || !packageText(c.Title, 240, true) || !packageText(c.Preconditions, 16000, false) || !packageText(c.Expected, 16000, false) || !packageEnum(c.Automation, "manual", "planned", "automated") || !packageURL(c.AutomationURL) || len(c.Steps) > 100 || len(c.Selection) > 500 {
			return fmt.Errorf("invalid test case")
		}
		if err := validatePackageAutomation(c); err != nil {
			return err
		}
		ids[c.ID] = true
		stepIDs := map[string]bool{}
		for _, step := range c.Steps {
			if !cachelab.IDPattern.MatchString(step.ID) || stepIDs[step.ID] || !packageText(step.Instruction, 16000, true) || !packageText(step.Expected, 16000, false) {
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
	return packageEnum(ext, ".log", ".db") && cachelab.IDPattern.MatchString(strings.TrimSuffix(name, ext))
}

func validateTestPackage(pkg testPackage) error {
	if !cachelab.IDPattern.MatchString(pkg.ID) || !cachelab.IDPattern.MatchString(pkg.Revision) || (pkg.OriginID != "" && !cachelab.IDPattern.MatchString(pkg.OriginID)) || !packageText(pkg.Title, 240, true) || !packageURL(pkg.IssueURL) || !packageText(pkg.IssueTitle, 500, false) || !packageURL(pkg.FixURL) || !packageText(pkg.FixTitle, 500, false) || !packageText(pkg.Summary, 32000, false) || !packageText(pkg.Notes, 100000, false) || !packageEnum(pkg.Status, "planning", "reproducing", "awaiting-fix", "validating", "verified", "archived") || pkg.CreatedAt.IsZero() || pkg.UpdatedAt.IsZero() || len(pkg.Sessions) > 500 {
		return fmt.Errorf("invalid test package")
	}
	if err := validateTestPackageCases(pkg.Cases); err != nil {
		return err
	}
	ids := map[string]bool{}
	artifactNames := map[string]bool{}
	var total int64
	for _, session := range pkg.Sessions {
		if !cachelab.IDPattern.MatchString(session.ID) || ids[session.ID] || !packageText(session.Name, 240, true) || !packageEnum(session.Purpose, "reproduction", "validation", "exploration") || !packageURL(session.FixURL) || !packageText(session.FixTitle, 500, false) || !packageCommitSHA(session.FixCommit) || !packageEnum(session.Status, "active", "completed") || !packageEnum(session.Finding, "inconclusive", "reproduced", "not-reproduced", "validated", "not-validated") || session.StartedAt.IsZero() || !packageText(session.Conclusion, 32000, false) || len(session.Cases) == 0 || len(session.Evidence) > 250 {
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
			if !cachelab.IDPattern.MatchString(e.ID) || evidenceIDs[e.ID] || !packageEnum(e.Kind, "test-run", "cache-snapshot", "note", "pod-logs") || !packageText(e.Name, 500, true) || !packageText(e.Description, 32000, false) || !packageText(e.SourceID, 512, false) || !packageText(e.WorkspaceID, 512, false) || !packageText(e.ClusterID, 512, false) || e.AttachedAt.IsZero() || e.CapturedAt.IsZero() || len(e.Metadata) > 1<<20 || !json.Valid(e.Metadata) {
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
