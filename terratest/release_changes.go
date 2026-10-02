package test

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

func releaseObserved(t releaseTracker) map[string]issueRadarIssue {
	out := map[string]issueRadarIssue{}
	for _, m := range t.Milestones {
		if m.Snapshot != nil {
			for _, issue := range m.Snapshot.Issues {
				out[issue.URL] = issue
			}
		}
	}
	for _, i := range t.Issues {
		if i.Snapshot != nil {
			out[i.Snapshot.URL] = *i.Snapshot
		}
	}
	return out
}
func releaseOwners(issue issueRadarIssue) string {
	users := []string{}
	for _, u := range issue.Assignees {
		users = append(users, "@"+strings.ToLower(u.Login))
	}
	sort.Strings(users)
	if len(users) == 0 {
		return "unassigned"
	}
	return strings.Join(users, ", ")
}
func releaseDepartureReason(issue issueRadarIssue, tracker releaseTracker) string {
	reasons := []string{}
	milestoneMatches := false
	for _, m := range tracker.Milestones {
		if issue.Milestone != nil && issue.Milestone.Number == m.Config.Milestone && strings.HasPrefix(strings.ToLower(issue.URL), "https://github.com/"+m.Config.Repo+"/") {
			milestoneMatches = true
		}
	}
	if !milestoneMatches {
		title := "no milestone"
		if issue.Milestone != nil {
			title = issue.Milestone.Title
		}
		reasons = append(reasons, "Milestone changed to "+title)
	}
	labels := map[string]bool{}
	for _, label := range issue.Labels {
		labels[strings.ToLower(label.Name)] = true
	}
	for _, label := range strings.Split(tracker.IssueLabel, ",") {
		label = strings.TrimSpace(label)
		if label != "" && !labels[strings.ToLower(label)] {
			reasons = append(reasons, "Label removed: "+label)
		}
	}
	if len(reasons) == 0 {
		reasons = append(reasons, "No longer matches the saved scope; current owners: "+releaseOwners(issue))
	}
	return strings.Join(reasons, " · ")
}
func (p *localControlPanel) releaseChanges(ctx context.Context, before, after releaseTracker) []releaseChange {
	old, current := releaseObserved(before), releaseObserved(after)
	out := []releaseChange{}
	for url, issue := range current {
		previous, exists := old[url]
		reason := ""
		if !exists {
			reason = "Added to this release view"
		} else {
			changes := []string{}
			if releaseOwners(previous) != releaseOwners(issue) {
				changes = append(changes, "Owners changed: "+releaseOwners(previous)+" → "+releaseOwners(issue))
			}
			if previous.State != issue.State {
				changes = append(changes, "Status changed: "+previous.State+" → "+issue.State)
			}
			previousMilestone, currentMilestone := "none", "none"
			if previous.Milestone != nil {
				previousMilestone = previous.Milestone.Title
			}
			if issue.Milestone != nil {
				currentMilestone = issue.Milestone.Title
			}
			if previousMilestone != currentMilestone {
				changes = append(changes, "Milestone changed: "+previousMilestone+" → "+currentMilestone)
			}
			reason = strings.Join(changes, " · ")
		}
		if reason != "" {
			out = append(out, releaseChange{url, issue.Number, issue.Title, reason})
		}
	}
	for url, issue := range old {
		if _, exists := current[url]; exists {
			continue
		}
		reason := "No longer returned by the release scan; current details could not be verified"
		// Only use repository identities already validated in the saved tracker.
		for _, m := range before.Milestones {
			if !strings.HasPrefix(strings.ToLower(url), "https://github.com/"+m.Config.Repo+"/issues/") {
				continue
			}
			var fresh issueRadarIssue
			err := p.issueRadarBackend().githubGet(ctx, fmt.Sprintf("repos/%s/issues/%d", m.Config.Repo, issue.Number), "{number,title,html_url,state,labels:[.labels[]|{name}],assignees:[.assignees[]|{login}],milestone:(.milestone|if . == null then null else {number,title} end)}", &fresh)
			if err == nil && fresh.Number == issue.Number {
				fresh.URL = url
				reason = releaseDepartureReason(fresh, after)
			}
			break
		}
		if before.IssueLabel != after.IssueLabel {
			reason = "Scan labels changed · " + reason
		}
		out = append(out, releaseChange{url, issue.Number, issue.Title, reason})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].URL < out[j].URL })
	return out
}
