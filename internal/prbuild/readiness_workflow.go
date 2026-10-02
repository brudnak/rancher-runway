package prbuild

import "strings"

// Build availability and workflow readiness are separate observations. A green
// light requires both, and cannot be inferred from a PR merge alone.
type Workflow struct {
	State         string `json:"state"`
	Title         string `json:"title"`
	Detail        string `json:"detail"`
	GreenLight    bool   `json:"greenLight"`
	AccessCommand string `json:"accessCommand,omitempty"`
}

func readinessWorkflowState(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.TrimPrefix(normalized, "status/")
	normalized = strings.Join(strings.Fields(strings.NewReplacer("-", " ", "_", " ").Replace(normalized)), " ")
	switch normalized {
	case "to test", "qa working":
		return "qa_ready"
	case "in progress", "in development", "development":
		return "in_progress"
	case "in review", "review", "code review":
		return "in_review"
	case "blocked", "on hold":
		return "blocked"
	default:
		return "other"
	}
}

func summarizeReadinessWorkflow(r *Report) {
	w := Workflow{State: "unknown", Title: "Workflow status unverified", Detail: "No current workflow status was returned. Confirm the issue is To Test or QA Working in GitHub."}
	defer func() { r.Workflow = w }()
	if r.Verdict == "qa_not_required" {
		w.State = "not_required"
		w.Title = "QA not required"
		w.Detail = "GitHub labels this issue QA/None."
		return
	}
	if r.Issue.State == "closed" {
		w.State = "closed"
		w.Title = "Issue closed on GitHub"
		w.Detail = "Build evidence is retained for reference; this issue is not active QA work."
		if r.Verdict == "ready" {
			r.Title = "Fix detected · issue already closed."
			r.Recommendation = w.Detail
		}
		return
	}
	states := map[string]bool{}
	labels := []string{}
	for _, status := range r.Statuses {
		states[readinessWorkflowState(status.Status)] = true
		labels = append(labels, status.Status)
	}
	if len(states) > 0 {
		w.State = "other"
		w.Title = "Confirm workflow before testing"
		w.Detail = "Current status: " + strings.Join(labels, " · ") + ". A green light requires To Test or QA Working."
		// A conflicting development/review state takes precedence over a QA-ready state.
		for _, entry := range []struct{ state, title string }{{"in_progress", "Issue is still in progress"}, {"in_review", "Issue is still in review"}, {"blocked", "Issue is blocked"}} {
			if states[entry.state] {
				w.State = entry.state
				w.Title = entry.title
				w.Detail = "Current status: " + strings.Join(labels, " · ") + ". Confirm the workflow before starting validation, even if the fix is in a build."
			}
		}
		if len(states) == 1 && states["qa_ready"] {
			w.State = "qa_ready"
			w.Title = "Workflow is ready for QA"
			w.Detail = "Current status: " + strings.Join(labels, " · ") + ". Build inclusion must also be proven."
		} else if states["qa_ready"] {
			w.Detail += " The reported workflow statuses conflict."
		}
	}
	if !r.WorkflowComplete {
		w.Detail += " Project status lookup was incomplete; unseen statuses may differ."
		if w.State == "qa_ready" {
			w.State = "unknown"
			w.Title = "Workflow status only partially verified"
		}
		for _, warning := range r.Warnings {
			if strings.Contains(warning, "requires read:project permission") {
				w.AccessCommand = "gh auth refresh -h github.com -s read:project"
				w.Detail = "GitHub denied project status access: the current CLI token needs read:project permission. Complete authorization, then check again."
			}
		}
	}
	w.GreenLight = r.Verdict == "ready" && r.Issue.State == "open" && r.WorkflowComplete && w.State == "qa_ready"
	if r.Verdict != "ready" {
		return
	}
	r.Title = "Build available · confirm workflow status."
	if w.GreenLight {
		r.Title = "Green light to test."
		r.Recommendation = "The issue is To Test or QA Working, and every required fix is proven in a matching Rancher build. Review the QA guidance and saved test cases below."
		w.Detail = "Workflow and build evidence agree. Current status: " + strings.Join(labels, " · ") + "."
	} else {
		if w.State == "in_progress" {
			r.Title = "Build available · issue still in progress."
		}
		if w.State == "in_review" {
			r.Title = "Build available · issue still in review."
		}
		if w.State == "blocked" {
			r.Title = "Build available · issue is blocked."
		}
		r.Recommendation = w.Detail
	}
}
