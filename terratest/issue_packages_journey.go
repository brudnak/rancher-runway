package test

import (
	"fmt"
	"sort"
	"strings"
)

// The journey is a derived, read-only view of a package: plan → reproduction →
// fix → validation. It is computed from saved records whenever it is shown and
// never persisted, so it cannot drift from the sessions it summarizes. It is
// advisory only. It does not change the package status, pick a finding, or
// claim that any case passed.
type issuePackageJourneyStage struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	State  string `json:"state"` // pending, active, done, attention
	Detail string `json:"detail"`
}

type issuePackageJourney struct {
	Stages []issuePackageJourneyStage `json:"stages"`
	Next   string                    `json:"next"`
	Action string                    `json:"action"`
}

// issuePackageJourneyLatest returns the newest session with the purpose. Active
// sessions win over preserved ones because unfinished work is what the tester
// must resolve next; among preserved sessions the most recently started wins.
func issuePackageJourneyLatest(sessions []issuePackageSession, purpose string) (issuePackageSession, bool) {
	matching := make([]issuePackageSession, 0, len(sessions))
	for _, session := range sessions {
		if session.Purpose == purpose {
			matching = append(matching, session)
		}
	}
	if len(matching) == 0 {
		return issuePackageSession{}, false
	}
	sort.SliceStable(matching, func(i, j int) bool {
		if (matching[i].Status == "active") != (matching[j].Status == "active") {
			return matching[i].Status == "active"
		}
		return matching[i].StartedAt.After(matching[j].StartedAt)
	})
	return matching[0], true
}

func issuePackageJourneyFinding(finding string) string {
	switch finding {
	case "reproduced":
		return "Issue reproduced"
	case "not-reproduced":
		return "Issue not reproduced"
	case "validated":
		return "Fix validated"
	case "not-validated":
		return "Fix not validated"
	}
	return "Inconclusive"
}

func issuePackageJourneySessionStage(id, label string, sessions []issuePackageSession, positive string) issuePackageJourneyStage {
	stage := issuePackageJourneyStage{ID: id, Label: label, State: "pending", Detail: "Not started"}
	session, ok := issuePackageJourneyLatest(sessions, id)
	if !ok {
		return stage
	}
	name := strings.TrimSpace(session.Name)
	if session.FixCommit != "" {
		name += " · commit " + issuePackageShortCommit(session.FixCommit)
	}
	if session.Status == "active" {
		stage.State = "active"
		stage.Detail = "In progress · " + name
		return stage
	}
	stage.Detail = issuePackageJourneyFinding(session.Finding) + " · " + name
	if session.Baseline != nil {
		stage.Detail += " · against baseline " + strings.TrimSpace(session.Baseline.Name)
	}
	if session.Finding == positive {
		stage.State = "done"
	} else {
		stage.State = "attention"
	}
	return stage
}

func issuePackageJourneyFor(pkg issuePackage) issuePackageJourney {
	plan := issuePackageJourneyStage{ID: "plan", Label: "Test plan", State: "pending", Detail: "No cases saved yet"}
	if len(pkg.Cases) > 0 {
		plan.State = "done"
		plan.Detail = fmt.Sprintf("%d saved case", len(pkg.Cases))
		if len(pkg.Cases) != 1 {
			plan.Detail += "s"
		}
	}
	reproduction := issuePackageJourneySessionStage("reproduction", "Reproduction", pkg.Sessions, "reproduced")
	fix := issuePackageJourneyStage{ID: "fix", Label: "Fix", State: "pending", Detail: "No fix linked"}
	if pkg.FixURL != "" {
		fix.State = "done"
		fix.Detail = strings.TrimSpace(pkg.FixTitle)
		if fix.Detail == "" {
			fix.Detail = pkg.FixURL
		}
	}
	validation := issuePackageJourneySessionStage("validation", "Validation", pkg.Sessions, "validated")
	if latest, ok := issuePackageJourneyLatest(pkg.Sessions, "validation"); ok && validation.State == "done" && pkg.FixURL != "" && !issuePackageSameFix(pkg.FixURL, latest.FixURL) {
		validation.State = "attention"
		validation.Detail = "Current fix has not been validated · " + validation.Detail
	}
	journey := issuePackageJourney{Stages: []issuePackageJourneyStage{plan, reproduction, fix, validation}}
	switch {
	case plan.State != "done":
		journey.Next, journey.Action = "Write and save the first case so a session has something to preserve.", "plan"
	case reproduction.State == "active":
		journey.Next, journey.Action = "Finish the active reproduction session with an explicit finding.", "finish-reproduction"
	case validation.State == "active":
		journey.Next, journey.Action = "Finish the active validation session with an explicit finding.", "finish-validation"
	case validation.State == "done":
		journey.Next, journey.Action = "Share the report. Marking the package Verified stays your decision.", "share"
	case reproduction.State == "pending":
		journey.Next, journey.Action = "Start a reproduction session to preserve the original behavior before any fix.", "start-reproduction"
	case reproduction.State == "attention":
		journey.Next, journey.Action = "Reproduction was not established. Review the cases or record another attempt before validating.", "start-reproduction"
	case fix.State != "done":
		journey.Next, journey.Action = "Link the fix pull request so validation records what it tested.", "link-fix"
	case validation.State == "pending":
		journey.Next, journey.Action = "Validate the fix against the preserved reproduction baseline.", "start-validation"
	default:
		journey.Next, journey.Action = "The fix was not validated. Record what remains open or validate another candidate.", "start-validation"
	}
	return journey
}

func issuePackageSameFix(a, b string) bool {
	canonical := func(value string) string {
		if ref, ok := parseIssuePackagePullRequest(value); ok {
			return strings.ToLower(ref.URL())
		}
		return strings.TrimSpace(value)
	}
	return canonical(a) == canonical(b)
}

func issuePackageShortCommit(sha string) string {
	if len(sha) > 10 {
		return sha[:10]
	}
	return sha
}

func issuePackageJourneyState(state string) string {
	switch state {
	case "done":
		return "Done"
	case "active":
		return "In progress"
	case "attention":
		return "Needs attention"
	}
	return "Not started"
}

// issuePackageWriteJourneyReport summarizes where the package stands. The table
// is derived from preserved sessions; it does not decide outcomes or status.
func issuePackageWriteJourneyReport(out *strings.Builder, pkg issuePackage) {
	journey := issuePackageJourneyFor(pkg)
	out.WriteString("## Progress\n\n| Stage | State | Detail |\n| --- | --- | --- |\n")
	for _, stage := range journey.Stages {
		fmt.Fprintf(out, "| %s | %s | %s |\n", issuePackageReportCell(stage.Label), issuePackageReportCell(issuePackageJourneyState(stage.State)), issuePackageReportCell(stage.Detail))
	}
	fmt.Fprintf(out, "\n**Next:** %s Stages are derived from preserved sessions and the linked fix; they do not set the package status or any case outcome.\n\n", issuePackageMarkdownText(journey.Next))
}
