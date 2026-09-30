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
type testPackageJourneyStage struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	State  string `json:"state"` // pending, active, done, attention
	Detail string `json:"detail"`
}

type testPackageJourney struct {
	Stages []testPackageJourneyStage `json:"stages"`
	Next   string                    `json:"next"`
	Action string                    `json:"action"`
}

// testPackageJourneyLatest returns the newest session with the purpose. Active
// sessions win over preserved ones because unfinished work is what the tester
// must resolve next; among preserved sessions the most recently started wins.
func testPackageJourneyLatest(sessions []testPackageSession, purpose string) (testPackageSession, bool) {
	matching := make([]testPackageSession, 0, len(sessions))
	for _, session := range sessions {
		if session.Purpose == purpose {
			matching = append(matching, session)
		}
	}
	if len(matching) == 0 {
		return testPackageSession{}, false
	}
	sort.SliceStable(matching, func(i, j int) bool {
		if (matching[i].Status == "active") != (matching[j].Status == "active") {
			return matching[i].Status == "active"
		}
		return matching[i].StartedAt.After(matching[j].StartedAt)
	})
	return matching[0], true
}

func testPackageJourneyFinding(finding string) string {
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

func testPackageJourneySessionStage(id, label string, sessions []testPackageSession, positive string) testPackageJourneyStage {
	stage := testPackageJourneyStage{ID: id, Label: label, State: "pending", Detail: "Not started"}
	session, ok := testPackageJourneyLatest(sessions, id)
	if !ok {
		return stage
	}
	name := strings.TrimSpace(session.Name)
	if session.FixCommit != "" {
		name += " · commit " + testPackageShortCommit(session.FixCommit)
	}
	if session.Status == "active" {
		stage.State = "active"
		stage.Detail = "In progress · " + name
		return stage
	}
	stage.Detail = testPackageJourneyFinding(session.Finding) + " · " + name
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

func testPackageJourneyFor(pkg testPackage) testPackageJourney {
	plan := testPackageJourneyStage{ID: "plan", Label: "Test plan", State: "pending", Detail: "No cases saved yet"}
	if len(pkg.Cases) > 0 {
		plan.State = "done"
		plan.Detail = fmt.Sprintf("%d saved case", len(pkg.Cases))
		if len(pkg.Cases) != 1 {
			plan.Detail += "s"
		}
	}
	reproduction := testPackageJourneySessionStage("reproduction", "Reproduction", pkg.Sessions, "reproduced")
	fix := testPackageJourneyStage{ID: "fix", Label: "Fix", State: "pending", Detail: "No fix linked"}
	if pkg.FixURL != "" {
		fix.State = "done"
		fix.Detail = strings.TrimSpace(pkg.FixTitle)
		if fix.Detail == "" {
			fix.Detail = pkg.FixURL
		}
	}
	validation := testPackageJourneySessionStage("validation", "Validation", pkg.Sessions, "validated")
	if latest, ok := testPackageJourneyLatest(pkg.Sessions, "validation"); ok && validation.State == "done" && pkg.FixURL != "" && !testPackageSameFix(pkg.FixURL, latest.FixURL) {
		validation.State = "attention"
		validation.Detail = "Current fix has not been validated · " + validation.Detail
	}
	journey := testPackageJourney{Stages: []testPackageJourneyStage{plan, reproduction, fix, validation}}
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

func testPackageSameFix(a, b string) bool {
	canonical := func(value string) string {
		if ref, ok := parseTestPackagePullRequest(value); ok {
			return strings.ToLower(ref.URL())
		}
		return strings.TrimSpace(value)
	}
	return canonical(a) == canonical(b)
}

func testPackageShortCommit(sha string) string {
	if len(sha) > 10 {
		return sha[:10]
	}
	return sha
}

func testPackageJourneyState(state string) string {
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

// testPackageWriteJourneyReport summarizes where the package stands. The table
// is derived from preserved sessions; it does not decide outcomes or status.
func testPackageWriteJourneyReport(out *strings.Builder, pkg testPackage) {
	journey := testPackageJourneyFor(pkg)
	out.WriteString("## Progress\n\n| Stage | State | Detail |\n| --- | --- | --- |\n")
	for _, stage := range journey.Stages {
		fmt.Fprintf(out, "| %s | %s | %s |\n", testPackageReportCell(stage.Label), testPackageReportCell(testPackageJourneyState(stage.State)), testPackageReportCell(stage.Detail))
	}
	fmt.Fprintf(out, "\n**Next:** %s Stages are derived from preserved sessions and the linked fix; they do not set the package status or any case outcome.\n\n", testPackageMarkdownText(journey.Next))
}
