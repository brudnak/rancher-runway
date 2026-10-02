package prbuild

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"

	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/brudnak/ha-rancher-rke2/internal/registrycatalog"
)

var readinessIssuePattern = regexp.MustCompile(`^https://github\.com/([A-Za-z0-9][A-Za-z0-9-]{0,38})/([A-Za-z0-9_.-]{1,100})/issues/([1-9][0-9]{0,9})/?$`)
var readinessPRLinkPattern = regexp.MustCompile(`https://github\.com/[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9_.-]{1,100}/pull/[1-9][0-9]{0,9}`)

var readinessReleasePattern = regexp.MustCompile(`(?:^|[^0-9])v?([0-9]+\.[0-9]+)(?:\.[0-9]+)?(?:[^0-9]|$)`)

type Request struct {
	IssueURL string `json:"issueUrl"`
	Expanded bool   `json:"expanded,omitempty"`
}
type Status struct {
	Project string `json:"project"`
	Status  string `json:"status"`
}
type issueReadinessPR struct {
	Pull         PullRequest `json:"pull"`
	Relationship string      `json:"relationship"`
	Error        string      `json:"error,omitempty"`
	BackportOf   []string    `json:"backportOf,omitempty"`
}
type issueReadinessMatch struct {
	PR         string      `json:"pr"`
	Applicable bool        `json:"applicable"`
	Server     CommitMatch `json:"server"`
	Agent      CommitMatch `json:"agent"`
}
type issueReadinessBuild struct {
	Registry      string                `json:"registry"`
	Label         string                `json:"label"`
	Tag           string                `json:"tag"`
	Line          string                `json:"line"`
	Server        ImageResult           `json:"server"`
	Agent         ImageResult           `json:"agent"`
	Matches       []issueReadinessMatch `json:"matches"`
	Ready         bool                  `json:"ready"`
	RequiredFixes int                   `json:"requiredFixes"`
	IncludedFixes int                   `json:"includedFixes"`
}
type Report struct {
	CheckedAt        time.Time             `json:"checkedAt"`
	Issue            Issue                 `json:"issue"`
	Statuses         []Status              `json:"statuses"`
	PRs              []issueReadinessPR    `json:"prs"`
	Builds           []issueReadinessBuild `json:"builds"`
	Warnings         []string              `json:"warnings"`
	Complete         bool                  `json:"complete"`
	LinksComplete    bool                  `json:"linksComplete"`
	PRsComplete      bool                  `json:"prsComplete"`
	WorkflowComplete bool                  `json:"workflowComplete"`
	Workflow         Workflow              `json:"workflow"`
	ImagesComplete   bool                  `json:"imagesComplete"`
	QA               QA                    `json:"qa"`
	Verdict          string                `json:"verdict"`
	Title            string                `json:"title"`
	Detail           string                `json:"detail"`
	Recommendation   string                `json:"recommendation"`
	TargetLine       string                `json:"targetLine"`
	SearchScope      string                `json:"searchScope"`
	Platform         string                `json:"platform"`
}
type readinessPage struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}
type readinessConnection struct {
	Nodes []struct {
		URL       string `json:"url"`
		Body      string `json:"body"`
		UpdatedAt string `json:"updatedAt"`
		Author    struct {
			Login string `json:"login"`
		} `json:"author"`
		Source struct {
			URL string `json:"url"`
		} `json:"source"`
		Project struct {
			Title string `json:"title"`
		} `json:"project"`
		Field struct {
			Name string `json:"name"`
		} `json:"fieldValueByName"`
	} `json:"nodes"`
	Page readinessPage `json:"pageInfo"`
}
type readinessDiscovery struct {
	refs                                        map[string]string
	statuses                                    []Status
	templates                                   []readinessQATemplate
	warnings                                    []string
	linksComplete, workflowComplete, qaComplete bool
}

func (s *Service) readinessGraphQL(ctx context.Context, query string, dst any) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	raw, err := s.runCommand(ctx, "gh", []string{"api", "--include", "--hostname", "github.com", "--method", "POST", "graphql", "-f", "query=" + query}, imagelookup.SanitizedGHEnvironment(), 2<<20)
	status, payload := ParsePRBuildGitHubIncludedResponse(raw)
	if ctx.Err() != nil {
		return fmt.Errorf("GitHub evidence lookup stopped: %w", ctx.Err())
	}
	if status >= 400 {
		return &GitHubHTTPError{Status: status, Operation: "issue evidence"}
	}
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	decodeErr := json.Unmarshal(payload, &envelope)
	for _, item := range envelope.Errors {
		if item.Type == "INSUFFICIENT_SCOPES" && strings.Contains(item.Message, "read:project") {
			return fmt.Errorf("GitHub project status requires read:project permission. Run gh auth refresh -h github.com -s read:project, complete GitHub authorization, then check again")
		}
	}
	if err != nil {
		return fmt.Errorf("GitHub could not return this connection; check GitHub CLI authentication and connectivity")
	}
	if decodeErr != nil {
		return decodeErr
	}
	if len(envelope.Errors) > 0 {
		return fmt.Errorf("GitHub did not allow this connection or returned an incomplete response")
	}
	return json.Unmarshal(envelope.Data, dst)
}
func (s *Service) readinessLinks(ctx context.Context, owner, repo string, number int) readinessDiscovery {
	out := readinessDiscovery{refs: map[string]string{}, statuses: []Status{}, templates: []readinessQATemplate{}, warnings: []string{}, linksComplete: true, workflowComplete: true, qaComplete: true}
	rank := map[string]int{"mention": 1, "reference": 2, "fix": 3}
	add := func(raw, kind string) {
		target, err := parsePRBuildPullRequestURL(raw)
		if err == nil {
			key := strings.ToLower(target.url)
			if rank[kind] > rank[out.refs[key]] {
				out.refs[key] = kind
			}
		}
	}
	encodedOwner, _ := json.Marshal(owner)
	encodedRepo, _ := json.Marshal(repo)
	for _, key := range []string{"closedByPullRequestsReferences", "timelineItems", "comments", "projectItems"} {
		Notify(ctx, map[string]string{"closedByPullRequestsReferences": "Reading closing PR links…", "timelineItems": "Reading cross-referenced PRs…", "comments": "Scanning comments for QA templates and PR links…", "projectItems": "Reading workflow status…"}[key])
		cursor := ""
		complete := false
		failure := "scan exceeded 1,000 items"
		for page := 0; page < 10; page++ {
			args := "first:100"
			if cursor != "" {
				encoded, _ := json.Marshal(cursor)
				args += ",after:" + string(encoded)
			}
			fragment := ""
			switch key {
			case "closedByPullRequestsReferences":
				args += ",includeClosedPrs:true"
				fragment = "nodes { url }"
			case "timelineItems":
				args += ",itemTypes:[CROSS_REFERENCED_EVENT]"
				fragment = "nodes { ... on CrossReferencedEvent { source { ... on PullRequest { url } } } }"
			case "comments":
				fragment = "nodes { body url updatedAt author { login } }"
			case "projectItems":
				fragment = `nodes { project { title } fieldValueByName(name:"Status") { ... on ProjectV2ItemFieldSingleSelectValue { name } } }`
			}
			query := fmt.Sprintf("query { repository(owner:%s,name:%s) { issue(number:%d) { %s(%s){ %s pageInfo{hasNextPage endCursor} } } } }", encodedOwner, encodedRepo, number, key, args, fragment)
			var data struct {
				Repository struct {
					Issue map[string]*readinessConnection `json:"issue"`
				} `json:"repository"`
			}
			if err := s.readinessGraphQL(ctx, query, &data); err != nil {
				failure = err.Error()
				break
			}
			connection := data.Repository.Issue[key]
			if connection == nil {
				failure = "connection was not accessible"
				break
			}
			for _, node := range connection.Nodes {
				switch key {
				case "closedByPullRequestsReferences":
					add(node.URL, "fix")
				case "timelineItems":
					add(node.Source.URL, "reference")
				case "comments":
					for _, raw := range readinessPRLinkPattern.FindAllString(node.Body, -1) {
						add(raw, "mention")
					}
					if template, ok := detectReadinessQA(node.Body, node.URL, node.Author.Login, node.UpdatedAt); ok {
						out.templates = append(out.templates, template)
					}
				case "projectItems":
					if node.Field.Name != "" || node.Project.Title != "" {
						if node.Field.Name == "" {
							node.Field.Name = "Not set"
						}
						out.statuses = append(out.statuses, Status{Project: node.Project.Title, Status: node.Field.Name})
					}
				}
			}
			if !connection.Page.HasNextPage {
				complete = true
				break
			}
			if connection.Page.EndCursor == "" || connection.Page.EndCursor == cursor {
				failure = "pagination did not advance"
				break
			}
			cursor = connection.Page.EndCursor
		}
		if !complete {
			out.warnings = append(out.warnings, key+": "+failure)
			if key == "projectItems" {
				out.workflowComplete = false
			} else {
				out.linksComplete = false
			}
			if key == "comments" {
				out.qaComplete = false
			}
		}
	}
	return out
}
func readinessLine(value string) string {
	if match := readinessReleasePattern.FindStringSubmatch(value); len(match) > 1 {
		return match[1]
	}
	return ""
}
func readinessApplicable(p PullRequest, line string) bool {
	if line != "" {
		return prBuildBaseRefMatchesMinorLine(p.BaseRef, line)
	}
	return p.BaseRef == "master" || p.BaseRef == "main" || p.BaseRef == "head"
}
func (s *Service) readinessBuilds(ctx context.Context) ([]issueReadinessBuild, []string) {
	scope, _ := ctx.Value(readinessScopeKey{}).(readinessSearchScope)
	if scope.Line != "" && !scope.Expanded {
		return s.readinessTargetBuilds(ctx, scope)
	}
	Notify(ctx, "Discovering moving head tags across all release lines…")
	builds := []issueReadinessBuild{}
	warnings := []string{}
	type result struct{ group imagelookup.SearchGroup }
	jobs := make(chan string)
	results := make(chan result, len(registrycatalog.Preferred))
	var workers sync.WaitGroup
	for i := 0; i < 4; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for registry := range jobs {
				scanCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
				group := s.imageLookup.SearchMovingHeads(scanCtx, registry, "rancher/rancher", 100)
				cancel()
				results <- result{group}
			}
		}()
	}
	go func() {
		for _, registry := range registrycatalog.Preferred {
			jobs <- registry
		}
		close(jobs)
		workers.Wait()
		close(results)
	}()
	for result := range results {
		group := result.group
		if group.Error != "" {
			warnings = append(warnings, group.Registry+": "+group.Error)
		}
		if group.Truncated {
			warnings = append(warnings, group.Registry+": head selector discovery was incomplete")
		}
		for _, tag := range group.Tags {
			if !imagelookup.ReadinessHeadPattern.MatchString(tag.Name) {
				continue
			}
			builds = append(builds, issueReadinessBuild{Registry: group.Registry, Label: imagelookup.PreferredRegistryLabel(group.Registry), Tag: tag.Name, Line: readinessLine(tag.Name), Matches: []issueReadinessMatch{}})
		}
	}
	sort.Slice(builds, func(i, j int) bool {
		if builds[i].Registry == builds[j].Registry {
			return imagelookup.NaturalCompare(builds[i].Tag, builds[j].Tag) > 0
		}
		return builds[i].Registry < builds[j].Registry
	})
	// Inspect each observed mutable selector once, shared by every PR comparison.
	type imageJob struct {
		Index int
		Agent bool
	}
	imageJobs := make(chan imageJob)
	var imageWorkers sync.WaitGroup
	for i := 0; i < 4; i++ {
		imageWorkers.Add(1)
		go func() {
			defer imageWorkers.Done()
			for job := range imageJobs {
				b := &builds[job.Index]
				repository := "rancher/rancher"
				if job.Agent {
					repository = "rancher/rancher-agent"
				}
				lookupCtx, cancel := context.WithTimeout(ctx, prBuildImageLookupTimeout)
				Notify(ctx, "Inspecting "+b.Registry+"/"+repository+":"+b.Tag)
				image := s.inspectImage(lookupCtx, b.Registry+"/"+repository+":"+b.Tag)
				cancel()
				if job.Agent {
					b.Agent = image
				} else {
					b.Server = image
				}
			}
		}()
	}
	for i := range builds {
		if ctx.Err() != nil {
			warnings = append(warnings, "Build inspection timed out; uninspected selectors remain unknown.")
			break
		}
		imageJobs <- imageJob{Index: i}
		imageJobs <- imageJob{Index: i, Agent: true}
	}
	close(imageJobs)
	imageWorkers.Wait()
	for i := range builds {
		s.readinessAssignHeadLine(ctx, &builds[i])
	}
	return builds, warnings
}
func (s *Service) CheckIssue(ctx context.Context, req Request) (Report, error) {
	return s.checkIssue(ctx, req, s.readinessBuilds)
}
func (s *Service) checkIssue(ctx context.Context, req Request, discoverBuilds func(context.Context) ([]issueReadinessBuild, []string)) (Report, error) {
	s.defaults()
	report := Report{CheckedAt: s.now().UTC(), Statuses: []Status{}, PRs: []issueReadinessPR{}, Builds: []issueReadinessBuild{}, Warnings: []string{}, Complete: true, LinksComplete: true, PRsComplete: true, WorkflowComplete: true, ImagesComplete: true, Platform: prBuildPlatform}
	match := readinessIssuePattern.FindStringSubmatch(strings.TrimSpace(req.IssueURL))
	if len(match) != 4 {
		return report, &imagelookup.InputError{Message: "choose a full github.com issue URL"}
	}
	repo := match[1] + "/" + match[2]
	if _, err := NormalizeRepository(repo); err != nil {
		return report, err
	}
	number, _ := strconv.Atoi(match[3])
	Notify(ctx, "Reading current issue state, ownership, and QA labels…")
	if err := s.runGitHubJSON(ctx, fmt.Sprintf("repos/%s/issues/%d", repo, number), "{number,title,html_url,state,state_reason,body,updated_at,labels:[.labels[]|{name}],assignees:[.assignees[]|{login}],milestone:(.milestone|if . == null then null else {number,title,state,due_on} end),pull_request}", &report.Issue, "issue"); err != nil {
		return report, err
	}
	if report.Issue.PullRequest != nil || report.Issue.Number != number {
		return report, fmt.Errorf("the selected URL is not an issue")
	}
	report.Issue.URL = fmt.Sprintf("https://github.com/%s/issues/%d", repo, number)
	if IssueQANone(report.Issue) {
		report.Verdict = "qa_not_required"
		report.Title = "QA is not required for this issue."
		report.Detail = "GitHub currently labels this issue QA/None. It is excluded from remaining QA work and missing-test-case counts."
		report.Recommendation = "Existing test packages remain available as history."
		report.QA = QA{State: "not_required", Complete: true, Templates: []readinessQATemplate{}}
		summarizeReadinessWorkflow(&report)
		return report, nil
	}
	discovery := s.readinessLinks(ctx, match[1], match[2], number)
	refs := discovery.refs
	report.Statuses = discovery.statuses
	for _, label := range report.Issue.Labels {
		if strings.HasPrefix(strings.ToLower(label.Name), "status/") {
			report.Statuses = append(report.Statuses, Status{Project: "Issue label", Status: label.Name})
		}
	}
	report.Warnings = append(report.Warnings, discovery.warnings...)
	report.LinksComplete = discovery.linksComplete
	report.WorkflowComplete = discovery.workflowComplete
	if template, ok := detectReadinessQA(report.Issue.Body, report.Issue.URL, "", report.Issue.UpdatedAt); ok {
		discovery.templates = append(discovery.templates, template)
	}
	report.QA = summarizeReadinessQA(discovery.templates, discovery.qaComplete)
	for _, raw := range readinessPRLinkPattern.FindAllString(report.Issue.Body, -1) {
		target, err := parsePRBuildPullRequestURL(raw)
		if err == nil && refs[strings.ToLower(target.url)] == "" {
			refs[strings.ToLower(target.url)] = "mention"
		}
	}
	if report.Issue.Milestone != nil {
		report.TargetLine = readinessLine(report.Issue.Milestone.Title)
	}
	urls := []string{}
	for raw := range refs {
		urls = append(urls, raw)
	}
	sort.Strings(urls)
	if len(urls) > 30 {
		urls = urls[:30]
		report.LinksComplete = false
		report.Warnings = append(report.Warnings, "More than 30 PRs were discovered; the remaining links were not checked.")
	}
	for index, raw := range urls {
		Notify(ctx, fmt.Sprintf("Reading PR %d of %d · %s", index+1, len(urls), raw))
		if ctx.Err() != nil {
			report.PRsComplete = false
			report.Warnings = append(report.Warnings, "PR lookup timed out.")
			break
		}
		target, _ := parsePRBuildPullRequestURL(raw)
		row := issueReadinessPR{Relationship: refs[raw], Pull: PullRequest{URL: raw, Repository: target.owner + "/" + target.repository, Number: target.number}}
		pull, err := s.fetchPull(ctx, target)
		if err == nil {
			row.Pull, err = normalizePRBuildPullRequest(target, pull)
			row.BackportOf = readinessBackportLinks(pull.Body, target)
			if row.Relationship != "fix" && readinessDeclaredIssue(pull.Body, report.Issue.URL, target) {
				row.Relationship = "declared_fix"
			}
		}
		if err != nil {
			row.Error = imagelookup.SafeError(err)
			report.PRsComplete = false
		}
		report.PRs = append(report.PRs, row)
	}
	merged := false
	for _, pr := range report.PRs {
		if pr.Error == "" && pr.Pull.Merged {
			merged = true
		}
	}
	scope := readinessSearchScope{Line: report.TargetLine, Expanded: req.Expanded}
	if report.Issue.Milestone != nil {
		scope.Version = readinessPatchVersion(report.Issue.Milestone.Title)
	}
	if scope.Line == "" {
		lines := map[string]bool{}
		for _, pr := range report.PRs {
			if pr.Pull.Repository == "rancher/rancher" && readinessKnownRelease(pr.Pull.BaseRef) {
				lines[readinessLine(pr.Pull.BaseRef)] = true
			}
		}
		if len(lines) == 1 {
			for line := range lines {
				scope.Line = line
			}
			report.TargetLine = scope.Line
		}
	}
	ctx = context.WithValue(ctx, readinessScopeKey{}, scope)
	report.SearchScope = "Target release and default head"
	if scope.Expanded || scope.Line == "" {
		report.SearchScope = "All release heads"
	}
	ctx = readinessWithEvidenceCache(ctx)
	if merged && ctx.Err() == nil {
		var warnings []string
		report.Builds, warnings = discoverBuilds(ctx)
		report.Builds = append([]issueReadinessBuild{}, report.Builds...)
		for i := range report.Builds {
			report.Builds[i].Matches = []issueReadinessMatch{}
		}
		report.Warnings = append(report.Warnings, warnings...)
		if len(warnings) > 0 {
			report.ImagesComplete = false
		}
		for _, pr := range report.PRs {
			if pr.Error != "" || !pr.Pull.Merged {
				continue
			}
			registries := make([]RegistryResult, len(report.Builds))
			for i, b := range report.Builds {
				registries[i] = RegistryResult{Registry: b.Registry, Label: b.Label, Server: b.Server, Agent: b.Agent}
			}
			target, _ := parsePRBuildPullRequestURL(pr.Pull.URL)
			Notify(ctx, "Checking fix inclusion · "+pr.Pull.Repository+"#"+strconv.Itoa(pr.Pull.Number))
			err := s.compareReadinessRevisions(ctx, target, pr.Pull, registries)
			if err != nil {
				report.ImagesComplete = false
				report.Warnings = append(report.Warnings, "Some commit comparisons did not finish.")
				break
			}
			for i, result := range registries {
				if result.Server.Match.Verdict == "unknown" || result.Agent.Match.Verdict == "unknown" || result.Server.Error != "" || result.Agent.Error != "" {
					report.ImagesComplete = false
				}
				report.Builds[i].Matches = append(report.Builds[i].Matches, issueReadinessMatch{PR: pr.Pull.URL, Applicable: readinessApplicable(pr.Pull, report.Builds[i].Line), Server: result.Server.Match, Agent: result.Agent.Match})
			}
		}
	}
	if ctx.Err() != nil {
		report.PRsComplete = false
		report.ImagesComplete = false
	}
	report.Complete = report.LinksComplete && report.PRsComplete && report.ImagesComplete && report.WorkflowComplete && report.QA.Complete
	summarizeIssueReadiness(&report)
	return report, nil
}
func summarizeIssueReadiness(r *Report) {
	defer summarizeReadinessWorkflow(r)
	fixes := []issueReadinessPR{}
	for _, pr := range r.PRs {
		if pr.Relationship == "fix" || pr.Relationship == "declared_fix" {
			fixes = append(fixes, pr)
		}
	}
	readyCount := 0
	for i := range r.Builds {
		b := &r.Builds[i]
		b.RequiredFixes, b.IncludedFixes = readinessBuildCoverage(*r, *b)
		b.Ready = r.LinksComplete && r.PRsComplete && b.RequiredFixes > 0 && b.IncludedFixes == b.RequiredFixes && b.Server.Found && b.Agent.Found && b.Server.Error == "" && b.Agent.Error == ""
		if b.Ready && (r.TargetLine == "" || r.TargetLine == b.Line) {
			readyCount++
		}
	}
	r.Verdict = "unknown"
	r.Title = "More evidence is needed."
	r.Detail = "Read the PR and build evidence below before choosing an environment."
	r.Recommendation = "Review the missing evidence, then check again."
	if len(r.PRs) == 0 && r.Complete {
		r.Verdict = "no_prs"
		r.Title = "No linked PRs found yet."
		r.Detail = "The scanned issue links, description, and comments did not identify a pull request."
		r.Recommendation = "Keep preparing the test plan; check again when a fix is linked."
		return
	}
	if len(fixes) == 0 && len(r.PRs) > 0 {
		r.Verdict = "review_links"
		r.Title = "PRs found. Confirm which ones fix this issue."
		r.Detail = "These PRs reference the issue or appear in discussion, but GitHub did not identify them as closing links."
		r.Recommendation = "Review the discovered PRs and their build evidence before choosing what to test."
		return
	}
	open := 0
	discarded := 0
	for _, pr := range fixes {
		if pr.Error != "" {
			continue
		}
		if pr.Pull.Merged {
			continue
		}
		if pr.Pull.State == "open" {
			open++
		} else {
			discarded++
		}
	}
	if readyCount > 0 {
		r.Verdict = "ready"
		r.Title = "A build is ready for your test."
		r.Detail = fmt.Sprintf("Relevant linked fixes are merged and included together in %d observed server/agent head build pairs for the target release line.", readyCount)
		r.Recommendation = "Review the matching build and test plan, then consider moving the item to Test in GitHub. Runway has made no workflow changes."
		for _, status := range r.Statuses {
			normalized := strings.ToLower(status.Status)
			if strings.Contains(normalized, "progress") || strings.Contains(normalized, "blocked") || strings.Contains(normalized, "review") {
				r.Recommendation = "A matching build is available, but the project still reports " + status.Status + ". Review that status before moving the item to Test."
			}
		}
		return
	}
	if open > 0 {
		r.Verdict = "in_progress"
		r.Title = "Linked fix PRs are still open."
		r.Detail = fmt.Sprintf("%d linked PRs are unmerged; this includes any draft or review-stage PRs.", open)
		r.Recommendation = "Prepare the plan while the fix is in progress. Check again after merge."
		return
	}
	if discarded > 0 {
		r.Verdict = "unmerged_closed"
		r.Title = "Closed PRs do not prove a merged fix."
		r.Detail = fmt.Sprintf("%d linked PRs were closed without merging.", discarded)
		r.Recommendation = "Review whether those PRs were replaced or abandoned before treating this issue as ready."
		return
	}
	if len(fixes) > 0 && r.Complete {
		r.Verdict = "awaiting_build"
		r.Title = "Fixes merged. A matching build is not proven yet."
		r.Detail = "No observed pair contains every relevant fix in both server and agent images for the target release line."
		r.Recommendation = "Wait for the head build or inspect the missing provenance and backport evidence."
	}
}
