package test

import (
	"fmt"
	"strings"
	"time"
)

func splitIssuePackageMarkdownRow(line string) []string {
	cells := []string{}
	start, escapes := 0, 0
	for index, r := range line {
		if r == '|' && escapes%2 == 0 {
			cells = append(cells, strings.TrimSpace(line[start:index]))
			start = index + 1
		}
		if r == '\\' {
			escapes++
		} else {
			escapes = 0
		}
	}
	cells = append(cells, strings.TrimSpace(line[start:]))
	if len(cells) > 0 && cells[0] == "" {
		cells = cells[1:]
	}
	if len(cells) > 0 && cells[len(cells)-1] == "" {
		cells = cells[:len(cells)-1]
	}
	return cells
}

// An imported environment is documentary context, never a newly observed
// deployment or a new execution session. Private endpoints cannot be imported.
func parseIssuePackagePlanMarkdownEnvironment(raw []byte) (issuePackageEnvironment, bool, error) {
	e := issuePackageEnvironment{Source: "imported", RecordedAt: time.Now().UTC(), Images: []string{}}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	_, table, found := strings.Cut(text, "| Component | Version / Type |\n")
	if !found {
		return e, false, nil
	}
	values := map[string]string{}
	for _, line := range strings.Split(table, "\n") {
		if !strings.HasPrefix(line, "|") {
			break
		}
		cells := splitIssuePackageMarkdownRow(line)
		if len(cells) != 2 {
			return e, false, fmt.Errorf("the imported environment table must have two columns")
		}
		label, value := decodeIssuePackagePlanText(cells[0]), decodeIssuePackagePlanText(cells[1])
		if label == "---" || value == "Not recorded" || value == "Unknown" || value == "" {
			continue
		}
		values[label] = value
	}
	if len(values) == 0 {
		return e, false, nil
	}
	e.ClusterName = values["Rancher instance"]
	e.RancherVersion = strings.TrimSuffix(values["Rancher version starting"], " (saved configuration)")
	for label, value := range values {
		switch label {
		case "Rancher instance", "Rancher version starting":
			continue
		case "Local K8s Version":
			e.KubernetesVersion = value
		case "RKE2 Local K8s Version", "K3s Local K8s Version":
			e.KubernetesVersion = value
			packageEnvironmentDetail(&e, "Kubernetes distribution", strings.TrimSuffix(label, " Local K8s Version"), "manual", nil)
		case "Webhook version":
			packageEnvironmentDetail(&e, "Webhook chart version", value, "manual", nil)
		default:
			for _, allowed := range []string{"Installation type", "Infrastructure provider", "Installation option", "Rancher version upgraded", "Cert Details", "Downstream cluster type", "Downstream K8s version", "Authentication providers enabled", "Logged in user role", "Browser type", "Browser version", "Dashboard"} {
				if label == allowed {
					packageEnvironmentDetail(&e, label, value, "manual", nil)
				}
			}
		}
	}
	e.Warnings = []string{"Environment imported from Markdown. These documented values were not refreshed from a cluster."}
	if err := validateIssuePackageEnvironment(e); err != nil {
		return e, false, err
	}
	return e, true, nil
}
