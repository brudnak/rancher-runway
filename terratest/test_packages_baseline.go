package test

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// A candidate owns its baseline snapshot. It does not depend on another session
// remaining in the library, and never borrows ownership of that session's files.
type testPackageBaseline struct {
	SessionID   string                        `json:"sessionId"`
	Name        string                        `json:"name"`
	Purpose     string                        `json:"purpose"`
	Finding     string                        `json:"finding"`
	Conclusion  string                        `json:"conclusion"`
	StartedAt   time.Time                     `json:"startedAt"`
	FinishedAt  time.Time                     `json:"finishedAt"`
	CapturedAt  time.Time                     `json:"capturedAt"`
	Environment testPackageEnvironment        `json:"environment"`
	Cases       []testPackageCase             `json:"cases"`
	Results     []testPackageResult           `json:"results"`
	Evidence    []testPackageBaselineEvidence `json:"evidence"`
}

// HasArtifact records that an artifact was attached at baseline capture time.
// It makes no assertion about whether that file still exists or was shared.
type testPackageBaselineEvidence struct {
	ID          string    `json:"id"`
	Kind        string    `json:"kind"`
	Name        string    `json:"name"`
	CaseID      string    `json:"caseId,omitempty"`
	StepID      string    `json:"stepId,omitempty"`
	CapturedAt  time.Time `json:"capturedAt"`
	HasArtifact bool      `json:"hasArtifact"`
}

func testPackageSnapshotBaseline(sessions []testPackageSession, id string, now time.Time) (*testPackageBaseline, error) {
	for _, session := range sessions {
		if session.ID != id {
			continue
		}
		if session.Status != "completed" {
			return nil, fmt.Errorf("finish the baseline session before validating a fix; its observations must be preserved first")
		}
		baseline := &testPackageBaseline{SessionID: session.ID, Name: session.Name, Purpose: session.Purpose, Finding: session.Finding, Conclusion: session.Conclusion, StartedAt: session.StartedAt, FinishedAt: session.FinishedAt, CapturedAt: now, Environment: session.Environment, Cases: session.Cases, Results: session.Results, Evidence: []testPackageBaselineEvidence{}}
		for _, evidence := range session.Evidence {
			baseline.Evidence = append(baseline.Evidence, testPackageBaselineEvidence{ID: evidence.ID, Kind: evidence.Kind, Name: evidence.Name, CaseID: evidence.CaseID, StepID: evidence.StepID, CapturedAt: evidence.CapturedAt, HasArtifact: evidence.Artifact != nil})
		}
		// Make ownership explicit, including nested step slices and environment rows.
		raw, err := json.Marshal(baseline)
		if err != nil {
			return nil, err
		}
		var frozen testPackageBaseline
		if err = json.Unmarshal(raw, &frozen); err != nil {
			return nil, err
		}
		return &frozen, nil
	}
	return nil, fmt.Errorf("baseline session not found")
}

func validateTestPackageBaseline(candidate testPackageSession) error {
	b := candidate.Baseline
	if b == nil {
		return nil
	}
	if candidate.Purpose != "validation" || b.SessionID == candidate.ID || b.CapturedAt.IsZero() || b.CapturedAt.Before(b.FinishedAt) || b.CapturedAt.After(candidate.StartedAt) || len(b.Evidence) > 250 {
		return fmt.Errorf("invalid preserved baseline")
	}
	// Reuse the same strict environment/case/result checks as a real session.
	// Baselines deliberately cannot nest, or own artifact references.
	session := testPackageSession{ID: b.SessionID, Name: b.Name, Purpose: b.Purpose, Status: "completed", Finding: b.Finding, Conclusion: b.Conclusion, StartedAt: b.StartedAt, FinishedAt: b.FinishedAt, Environment: b.Environment, Cases: b.Cases, Results: b.Results}
	for _, e := range b.Evidence {
		session.Evidence = append(session.Evidence, testPackageEvidence{ID: e.ID, Kind: "note", Name: e.Name, CaseID: e.CaseID, StepID: e.StepID, CapturedAt: e.CapturedAt, AttachedAt: b.CapturedAt, Metadata: json.RawMessage(`{}`)})
		if !packageEnum(e.Kind, "test-run", "cache-snapshot", "note", "pod-logs") || (e.Kind == "note" && e.HasArtifact) {
			return fmt.Errorf("invalid baseline evidence summary")
		}
	}
	const fixtureID = "000000000000000000000000"
	pkg := testPackage{ID: fixtureID, Revision: fixtureID, Title: "Baseline validation", Status: "planning", CreatedAt: b.CapturedAt, UpdatedAt: b.CapturedAt, Sessions: []testPackageSession{session}}
	if err := validateTestPackage(pkg); err != nil {
		return fmt.Errorf("invalid preserved baseline: %w", err)
	}
	return nil
}

// Identity determines which cases correspond. Content determines whether their
// results can be compared directly; a retained ID never hides an edited case.
func testPackageCaseContent(c testPackageCase) string {
	c.ID = ""
	if c.Selection == nil {
		c.Selection = []string{}
	}
	if c.Steps == nil {
		c.Steps = []testPackageStep{}
	}
	raw, _ := json.Marshal(c)
	return string(raw)
}

type testPackageCaseComparison struct {
	CaseID            string
	Title             string
	Content           string
	BaselineOutcome   string
	CandidateOutcome  string
	BaselineEvidence  int
	CandidateEvidence int
}

func testPackageCompareCases(session testPackageSession) []testPackageCaseComparison {
	b := session.Baseline
	if b == nil {
		return nil
	}
	baselineResults, candidateResults := map[string]string{}, map[string]string{}
	for _, r := range b.Results {
		baselineResults[r.CaseID] = r.Outcome
	}
	for _, r := range session.Results {
		candidateResults[r.CaseID] = r.Outcome
	}
	baselineEvidence, candidateEvidence := map[string]int{}, map[string]int{}
	for _, e := range b.Evidence {
		baselineEvidence[e.CaseID]++
	}
	for _, e := range session.Evidence {
		candidateEvidence[e.CaseID]++
	}
	candidateCases := map[string]testPackageCase{}
	for _, c := range session.Cases {
		candidateCases[c.ID] = c
	}
	rows := []testPackageCaseComparison{}
	seen := map[string]bool{}
	for _, c := range b.Cases {
		row := testPackageCaseComparison{CaseID: c.ID, Title: c.Title, Content: "Same case", BaselineOutcome: baselineResults[c.ID], CandidateOutcome: "Missing", BaselineEvidence: baselineEvidence[c.ID], CandidateEvidence: candidateEvidence[c.ID]}
		if candidate, ok := candidateCases[c.ID]; ok {
			row.CandidateOutcome = candidateResults[c.ID]
			if testPackageCaseContent(c) != testPackageCaseContent(candidate) {
				row.Content = "Changed case — review differences"
			}
		} else {
			row.Content = "Missing from candidate"
		}
		rows = append(rows, row)
		seen[c.ID] = true
	}
	for _, c := range session.Cases {
		if !seen[c.ID] {
			rows = append(rows, testPackageCaseComparison{CaseID: c.ID, Title: c.Title, Content: "Added in candidate", BaselineOutcome: "Missing", CandidateOutcome: candidateResults[c.ID], CandidateEvidence: candidateEvidence[c.ID]})
		}
	}
	return rows
}

type testPackageEnvironmentDifference struct {
	Label, Baseline, Candidate string
}

// Values only: observation timestamps are shown in the environment snapshot,
// but collecting the same value later must not appear as an environment change.
func testPackageCompareEnvironments(baseline, candidate testPackageEnvironment) []testPackageEnvironmentDifference {
	values := func(env testPackageEnvironment) map[string]string {
		images := append([]string(nil), env.Images...)
		sort.Strings(images)
		out := map[string]string{"Cluster": env.ClusterName, "Cluster identity": env.ClusterID, "Rancher": env.RancherVersion, "Kubernetes": env.KubernetesVersion, "Images": strings.Join(images, "\n"), "Configuration notes": env.Configuration, "Helm command": sanitizeTestPackageHelmCommand(env.HelmCommand)}
		details := map[string][]string{}
		for _, row := range env.Details {
			details[row.Label] = append(details[row.Label], row.Value)
		}
		for label, items := range details {
			sort.Strings(items)
			out["Detail: "+label] = strings.Join(items, "\n")
		}
		return out
	}
	b, c := values(baseline), values(candidate)
	labels := map[string]bool{}
	for label := range b {
		labels[label] = true
	}
	for label := range c {
		labels[label] = true
	}
	keys := []string{}
	for label := range labels {
		keys = append(keys, label)
	}
	sort.Strings(keys)
	rows := []testPackageEnvironmentDifference{}
	for _, label := range keys {
		if b[label] != c[label] {
			rows = append(rows, testPackageEnvironmentDifference{Label: label, Baseline: b[label], Candidate: c[label]})
		}
	}
	return rows
}

func testPackageWriteBaselineReport(out *strings.Builder, candidate testPackageSession) {
	b := candidate.Baseline
	if b == nil {
		return
	}
	fmt.Fprintf(out, "### Baseline → candidate\n\nBaseline: **%s**, completed %s · Finding: **%s**. This comparison uses the baseline preserved when this candidate began.\n\n", testPackageMarkdownText(b.Name), testPackageReportTime(b.FinishedAt), testPackageMarkdownText(b.Finding))
	if b.Conclusion != "" {
		fmt.Fprintf(out, "**Baseline conclusion:** %s\n\n", testPackageMarkdownText(b.Conclusion))
	}
	fmt.Fprintf(out, "| Environment | Baseline | Candidate |\n| --- | --- | --- |\n| Rancher | %s | %s |\n| Kubernetes | %s | %s |\n\n", testPackageReportCell(b.Environment.RancherVersion), testPackageReportCell(candidate.Environment.RancherVersion), testPackageReportCell(b.Environment.KubernetesVersion), testPackageReportCell(candidate.Environment.KubernetesVersion))
	out.WriteString("| Case | Case content | Baseline | Candidate | Case evidence: baseline / candidate |\n| --- | --- | --- | --- | --- |\n")
	for _, row := range testPackageCompareCases(candidate) {
		fmt.Fprintf(out, "| %s | %s | %s | %s | %d / %d |\n", testPackageReportCell(row.Title), testPackageReportCell(row.Content), testPackageReportCell(row.BaselineOutcome), testPackageReportCell(row.CandidateOutcome), row.BaselineEvidence, row.CandidateEvidence)
	}
	fmt.Fprintf(out, "\nEvidence summaries: %d baseline / %d candidate. Case counts include only explicitly attached case evidence; session evidence is not assigned to every case. Baseline summaries do not include copied artifact bytes.\n\n", len(b.Evidence), len(candidate.Evidence))
	out.WriteString("Missing, not-run, blocked, or skipped results are coverage gaps. Changed cases need review before comparing outcomes. An outcome change does not by itself prove the fix caused it.\n\n")
	differences := testPackageCompareEnvironments(b.Environment, candidate.Environment)
	if len(differences) == 0 {
		out.WriteString("**Environment differences:** No differences in recorded comparison fields. Missing details remain unknown.\n\n")
	} else {
		out.WriteString("**Environment differences** (recorded values; missing values remain unknown):\n\n| Detail | Baseline | Candidate |\n| --- | --- | --- |\n")
		for _, row := range differences {
			fmt.Fprintf(out, "| %s | %s | %s |\n", testPackageReportCell(row.Label), testPackageReportCell(row.Baseline), testPackageReportCell(row.Candidate))
		}
		out.WriteString("\n")
	}
}
