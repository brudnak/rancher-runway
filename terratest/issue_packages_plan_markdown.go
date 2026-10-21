package test

import (
	"fmt"
	"strings"
)

func (p *localControlPanel) issuePackagePlanMarkdown(s *issuePackageService, req issuePackageRequest) (any, error) {
	pkg, err := s.get(req.ID)
	if err != nil {
		return nil, err
	}
	if req.Revision != "" && req.Revision != pkg.Revision {
		return nil, fmt.Errorf("the test plan changed before it could be copied: %w", errIssuePackageConflict)
	}
	var session *issuePackageSession
	if req.SessionID != "" {
		for i := range pkg.Sessions {
			if pkg.Sessions[i].ID == req.SessionID {
				session = &pkg.Sessions[i]
				break
			}
		}
		if session == nil {
			return nil, fmt.Errorf("session not found")
		}
	}
	var environment issuePackageEnvironment
	if session != nil {
		if req.Environment.ClusterID != "" && req.Environment.ClusterID != session.Environment.ClusterID {
			return nil, fmt.Errorf("session results must use their recorded environment")
		}
		environment = session.Environment
		packageEnvironmentInstallation(&environment)
	} else {
		environment, err = p.issuePackagePlanEnvironment(s, req)
		if err != nil {
			return nil, err
		}
	}
	markdown, err := issuePackageGitHubPlanMarkdown(pkg, environment, session)
	if err == nil && session == nil {
		err = s.rememberPlanEnvironment(pkg.ID, pkg.Revision, environment)
	}
	return map[string]any{"markdown": markdown, "environment": environment}, err
}

// A copied plan describes the current saved cases. Results are included only
// when a session was explicitly selected and its case definition still matches.
func issuePackagePlanResults(pkg issuePackage, session *issuePackageSession) map[string]issuePackageResult {
	results := map[string]issuePackageResult{}
	if session == nil {
		return results
	}
	definitions := map[string]string{}
	for _, c := range session.Cases {
		definitions[c.ID] = issuePackageCaseContent(c)
	}
	for _, c := range pkg.Cases {
		if definitions[c.ID] != issuePackageCaseContent(c) {
			continue
		}
		for _, result := range session.Results {
			if result.CaseID == c.ID {
				results[c.ID] = result
				break
			}
		}
	}
	return results
}

func issuePackagePlanStatus(outcome string) string {
	switch outcome {
	case "passed":
		return "✅ PASS"
	case "failed":
		return "❌ FAIL"
	case "blocked":
		return "🚧 BLOCKED"
	case "skipped":
		return "⏭️ SKIPPED"
	default:
		return "⏸️ NOT TESTED YET"
	}
}

func issuePackagePlanInline(value string) string {
	return issuePackageMarkdownText(strings.Join(strings.Fields(value), " "))
}

func issuePackageGitHubPlanMarkdown(pkg issuePackage, environment issuePackageEnvironment, session *issuePackageSession) (string, error) {
	if err := validateIssuePackage(pkg); err != nil {
		return "", err
	}
	if err := validateIssuePackageEnvironment(environment); err != nil {
		return "", err
	}
	var out strings.Builder
	out.WriteString("<!-- -->\n<!-- -->\n\n")
	issuePackageWritePlanEnvironment(&out, environment)
	out.WriteString("<a name=\"top\"></a>\n\n### 🧪 Test Cases\n\n")
	fmt.Fprintf(&out, "**%s**\n\n", issuePackagePlanInline(pkg.Title))
	if pkg.IssueURL != "" {
		fmt.Fprintf(&out, "**Issue:** %s\n\n", issuePackagePlanInline(pkg.IssueURL))
	}
	if pkg.FixURL != "" {
		fmt.Fprintf(&out, "**Fix:** %s\n\n", issuePackagePlanInline(pkg.FixURL))
	}
	if pkg.Summary != "" {
		fmt.Fprintf(&out, "**Plan summary**\n\n%s\n\n", issuePackageMarkdownText(pkg.Summary))
	}
	if session != nil {
		fmt.Fprintf(&out, "Results from **%s**, started %s. Cases added or changed since that session are marked not tested.\n\n", issuePackagePlanInline(session.Name), issuePackageReportTime(session.StartedAt))
	}
	out.WriteString("| # | Priority | Description & Link | PASS/FAIL |\n| --- | --- | --- | --- |\n")
	results := issuePackagePlanResults(pkg, session)
	for index, c := range pkg.Cases {
		fmt.Fprintf(&out, "| %d | P0 | [%s](#test-%d) | %s |\n", index+1, issuePackagePlanInline(c.Title), index+1, issuePackagePlanStatus(results[c.ID].Outcome))
	}
	if len(pkg.Cases) == 0 {
		out.WriteString("\nNo test cases have been added yet.\n")
	}
	caseLabel := "test cases"
	if len(pkg.Cases) == 1 {
		caseLabel = "test case"
	}
	fmt.Fprintf(&out, "\n<details>\n<summary>📋 %d %s — CLICK TO EXPAND! ⬅️ (required for table links)</summary>\n\n", len(pkg.Cases), caseLabel)
	for index, c := range pkg.Cases {
		number, result := index+1, results[c.ID]
		fmt.Fprintf(&out, "<a name=\"test-%d\"></a>\n\n# %d / %s Status: %s\n\n**:small_red_triangle: [back to top](#top)**\n\n<details>\n<summary>Test %d details... CLICK TO EXPAND! ⬅️</summary>\n\n", number, number, issuePackagePlanInline(c.Title), issuePackagePlanStatus(result.Outcome), number)
		if c.Preconditions != "" {
			fmt.Fprintf(&out, "**Preconditions**\n\n%s\n\n", issuePackageMarkdownText(c.Preconditions))
		}
		out.WriteString("**Test Steps for Validation**\n\n")
		if len(c.Steps) == 0 {
			out.WriteString("_Add steps as you test._\n\n")
		}
		for n, step := range c.Steps {
			text := strings.ReplaceAll(issuePackageMarkdownText(step.Instruction), "\n", "\n   ")
			fmt.Fprintf(&out, "%d. %s\n", n+1, text)
			if step.Expected != "" {
				fmt.Fprintf(&out, "   - Expected: %s\n", strings.ReplaceAll(issuePackageMarkdownText(step.Expected), "\n", "\n     "))
			}
		}
		if len(c.Steps) > 0 {
			out.WriteString("\n")
		}
		out.WriteString("**✅ Expected Outcome**\n\n")
		if c.Expected == "" {
			out.WriteString("_Add the expected outcome._\n\n")
		} else {
			fmt.Fprintf(&out, "%s\n\n", issuePackageMarkdownText(c.Expected))
		}
		out.WriteString("**✅ Actual Outcome**\n\n")
		fmt.Fprintf(&out, "%s\n\n", issuePackagePlanStatus(result.Outcome))
		if result.Notes != "" {
			fmt.Fprintf(&out, "%s\n\n", issuePackageMarkdownText(result.Notes))
		}
		out.WriteString("</details>\n\n<hr>\n\n")
	}
	out.WriteString("</details>\n")
	return out.String(), nil
}

func issuePackageWritePlanEnvironment(out *strings.Builder, e issuePackageEnvironment) {
	details := map[string]issuePackageEnvironmentDetail{}
	for _, detail := range e.Details {
		details[detail.Label] = detail
	}
	distribution := details["Kubernetes distribution"].Value
	localVersionLabel := "Local K8s Version"
	if distribution != "" {
		localVersionLabel = distribution + " Local K8s Version"
	}
	installationOption := details["Installation option"].Value
	switch details["Installation type"].Value {
	case "Rancher Docker installation":
		installationOption = "Docker"
	case "Rancher on RKE2 HA":
		installationOption = "Helm (high availability)"
	default:
		if e.HelmCommand != "" {
			installationOption = "Helm"
		}
	}
	rancherVersion := e.RancherVersion
	if rancherVersion != "" && details["Rancher server version"].Source != "observed" && e.Source == "recorded" {
		rancherVersion += " (saved configuration)"
	}
	localVersion, downstreamType, downstreamVersion := e.KubernetesVersion, details["Downstream cluster type"].Value, details["Downstream K8s version"].Value
	if details["Cluster role"].Value == "downstream" || details["Cluster type"].Value == "downstream" {
		localVersion, downstreamType, downstreamVersion = "", distribution, e.KubernetesVersion
		localVersionLabel = "Local K8s Version"
	}
	out.WriteString("<details>\n<summary>🧪 Test Environment... CLICK TO EXPAND! ⬅️</summary>\n\n| Component | Version / Type |\n| --- | --- |\n")
	rows := [][2]string{
		{"Rancher instance", e.ClusterName},
		{"Installation type", details["Installation type"].Value},
		{"Infrastructure provider", details["Infrastructure provider"].Value},
		{"Rancher version starting", rancherVersion},
		{"Rancher version upgraded", details["Rancher version upgraded"].Value},
		{localVersionLabel, localVersion},
		{"Installation option", installationOption},
		{"Cert Details", details["Cert Details"].Value},
		{"Downstream cluster type", downstreamType},
		{"Downstream K8s version", downstreamVersion},
		{"Authentication providers enabled", details["Authentication providers enabled"].Value},
		{"Logged in user role", details["Logged in user role"].Value},
		{"Browser type", details["Browser type"].Value},
		{"Browser version", details["Browser version"].Value},
		{"Dashboard", details["Dashboard"].Value},
		{"Webhook version", details["Webhook chart version"].Value},
	}
	for _, row := range rows {
		value := "Not recorded"
		if row[1] != "" {
			value = issuePackageReportCell(row[1])
		}
		fmt.Fprintf(out, "| %s | %s |\n", issuePackageReportCell(row[0]), value)
	}
	out.WriteString("\n")
	if e.Configuration != "" {
		fmt.Fprintf(out, "**Configuration notes**\n\n%s\n\n", issuePackageMarkdownText(e.Configuration))
	}
	if len(e.Details) > 0 || len(e.Images) > 0 {
		out.WriteString("<details>\n<summary>Recorded environment details</summary>\n\n| Detail | Value | Source | Observed |\n| --- | --- | --- | --- |\n")
		for _, detail := range e.Details {
			at := "Not recorded"
			if detail.ObservedAt != nil {
				at = issuePackageReportTime(*detail.ObservedAt)
			}
			fmt.Fprintf(out, "| %s | %s | %s | %s |\n", issuePackageReportCell(detail.Label), issuePackageReportCell(detail.Value), issuePackageReportCell(detail.Source), at)
		}
		out.WriteString("\n")
		for _, image := range e.Images {
			fmt.Fprintf(out, "- Image: %s\n", issuePackageMarkdownText(image))
		}
		out.WriteString("\n</details>\n\n")
	}
	if command := sanitizeIssuePackageHelmCommand(e.HelmCommand); command != "" {
		fmt.Fprintf(out, "**Recorded Helm install command** (sensitive values redacted):\n\n```sh\n%s\n```\n\n", command)
	}
	for _, warning := range e.Warnings {
		fmt.Fprintf(out, "> %s\n\n", issuePackageMarkdownText(warning))
	}
	out.WriteString("</details>\n\n")
}
