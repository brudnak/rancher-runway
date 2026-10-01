package test

import (
	"regexp"
	"sort"
	"strings"
)

type readinessQASection struct {
	Name string `json:"name"`
	Text string `json:"text"`
}
type readinessQATemplate struct {
	URL        string               `json:"url"`
	Author     string               `json:"author,omitempty"`
	UpdatedAt  string               `json:"updatedAt,omitempty"`
	Sections   []readinessQASection `json:"sections"`
	HasContent bool                 `json:"hasContent"`
	Truncated  bool                 `json:"truncated"`
}
type readinessQA struct {
	State     string                `json:"state"`
	Complete  bool                  `json:"complete"`
	Templates []readinessQATemplate `json:"templates"`
}

var readinessHTMLComment = regexp.MustCompile(`(?s)<!--.*?(?:-->|$)`)

// Detection is structural evidence of QA guidance, never an assertion that local cases are written.
func detectReadinessQA(body, url, author, updatedAt string) (readinessQATemplate, bool) {
	t := readinessQATemplate{URL: url, Author: author, UpdatedAt: updatedAt, Sections: []readinessQASection{}}
	marker := false
	fence := ""
	section := -1
	seen := map[string]bool{}
	for _, line := range strings.Split(readinessHTMLComment.ReplaceAllString(body, ""), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, ">") {
			continue
		}
		appendText := func() {
			if section < 0 {
				return
			}
			value := &t.Sections[section]
			if len(value.Text)+len(line)+1 <= 3000 {
				value.Text += line + "\n"
			} else {
				t.Truncated = true
			}
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			if fence == "" {
				fence = trimmed[:3]
			} else if strings.HasPrefix(trimmed, fence) {
				fence = ""
			}
			appendText()
			continue
		}
		if fence != "" {
			appendText()
			continue
		}
		heading := strings.HasPrefix(trimmed, "#") || (strings.HasPrefix(trimmed, "**") && strings.HasSuffix(trimmed, "**"))
		if heading {
			name := strings.ToLower(strings.Trim(strings.TrimSpace(trimmed), "#*_ :?"))
			section = -1
			if name == "qa testing" || name == "qa template" || name == "qa test plan" {
				marker = true
				continue
			}
			canonical := ""
			switch {
			case name == "root cause":
				canonical = "Root cause"
			case strings.HasPrefix(name, "what was fixed") || name == "what changes have occurred":
				canonical = "What changed"
			case name == "areas or cases that should be tested" || name == "test cases" || name == "areas to test":
				canonical = "Areas to test"
			case strings.HasPrefix(name, "what areas could experience regressions") || name == "regression areas":
				canonical = "Regression areas"
			case name == "repro steps" || name == "reproduction steps":
				canonical = "Reproduction steps"
			}
			if canonical != "" {
				t.Sections = append(t.Sections, readinessQASection{Name: canonical})
				section = len(t.Sections) - 1
				seen[canonical] = true
			}
			continue
		}
		appendText()
	}
	if !(marker && len(seen) > 0 || len(seen) >= 3 && (seen["Areas to test"] || seen["Reproduction steps"])) {
		return t, false
	}
	for i := range t.Sections {
		t.Sections[i].Text = strings.TrimSpace(t.Sections[i].Text)
		value := strings.ToLower(strings.Trim(t.Sections[i].Text, "- *_<>.\n\t "))
		if value != "" && value != "todo" && value != "tbd" && value != "n/a" && value != "none" && value != "fill in" {
			t.HasContent = true
		}
	}
	return t, true
}
func summarizeReadinessQA(templates []readinessQATemplate, complete bool) readinessQA {
	if templates == nil {
		templates = []readinessQATemplate{}
	}
	sort.SliceStable(templates, func(i, j int) bool { return templates[i].UpdatedAt > templates[j].UpdatedAt })
	state := "not_found"
	if !complete {
		state = "unknown"
	}
	if len(templates) > 0 {
		state = "draft"
		for _, t := range templates {
			if t.HasContent {
				state = "found"
				break
			}
		}
	}
	return readinessQA{State: state, Complete: complete, Templates: templates}
}

var readinessBackportMarker = regexp.MustCompile(`(?i)^(?:backport(?:s|ed)?(?:\s+of|\s+from)?|cherry[- ]pick(?:ed)?(?:\s+of|\s+from)?)\s*:?\s+(.+)`)
var readinessShortPR = regexp.MustCompile(`(?:([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+))?#([1-9][0-9]*)`)

func readinessBackportLinks(body string, target prBuildTarget) []string {
	out := []string{}
	seen := map[string]bool{}
	fence := ""
	for _, line := range strings.Split(readinessHTMLComment.ReplaceAllString(body, ""), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			if fence == "" {
				fence = line[:3]
			} else if strings.HasPrefix(line, fence) {
				fence = ""
			}
			continue
		}
		if fence != "" || strings.HasPrefix(line, ">") {
			continue
		}
		line = strings.TrimLeft(line, "#*- ")
		line = strings.ReplaceAll(line, "**", "")
		m := readinessBackportMarker.FindStringSubmatch(line)
		if len(m) != 2 {
			continue
		}
		urls := readinessPRLinkPattern.FindAllString(m[1], -1)
		for _, ref := range readinessShortPR.FindAllStringSubmatch(m[1], -1) {
			repo := ref[1]
			if repo == "" {
				repo = target.owner + "/" + target.repository
			}
			urls = append(urls, "https://github.com/"+repo+"/pull/"+ref[2])
		}
		for _, raw := range urls {
			pr, err := parsePRBuildPullRequestURL(raw)
			if err != nil || !strings.EqualFold(pr.owner+"/"+pr.repository, target.owner+"/"+target.repository) || pr.number == target.number {
				continue
			}
			key := strings.ToLower(pr.url)
			if !seen[key] {
				out = append(out, key)
				seen[key] = true
			}
		}
	}
	// Multiple originals can represent multiple independent fixes, not interchangeable commits.
	if len(out) != 1 {
		return nil
	}
	return out
}

func readinessKnownRelease(base string) bool {
	line := readinessLine(base)
	return line != "" && prBuildBaseRefMatchesMinorLine(base, line)
}

// Explicit backport references group alternate integration commits for the same fix.
// Unrelated fixes, including upstream dependencies, must all have their own evidence.
func readinessBuildCoverage(report issueReadinessReport, build issueReadinessBuild) (int, int) {
	rows := report.PRs
	index := map[string]int{}
	parent := make([]int, len(rows))
	for i, p := range rows {
		index[strings.ToLower(p.Pull.URL)] = i
		parent[i] = i
	}
	var root func(int) int
	root = func(i int) int {
		if parent[i] != i {
			parent[i] = root(parent[i])
		}
		return parent[i]
	}
	for i, p := range rows {
		if len(p.BackportOf) != 1 || !readinessKnownRelease(p.Pull.BaseRef) {
			continue
		}
		for _, url := range p.BackportOf {
			if j, ok := index[strings.ToLower(url)]; ok && strings.EqualFold(p.Pull.Repository, rows[j].Pull.Repository) && p.Pull.BaseRef != rows[j].Pull.BaseRef {
				parent[root(i)] = root(j)
			}
		}
	}
	groups := map[int][]issueReadinessPR{}
	confirmed := map[int]bool{}
	for i, p := range rows {
		key := root(i)
		groups[key] = append(groups[key], p)
		if p.Relationship == "fix" || p.Relationship == "declared_fix" {
			confirmed[key] = true
		}
	}
	issueRepo := ""
	if m := readinessIssuePattern.FindStringSubmatch(report.Issue.URL); len(m) == 4 {
		issueRepo = m[1] + "/" + m[2]
	}
	required, included := 0, 0
	for key, group := range groups {
		if !confirmed[key] {
			continue
		}
		candidates := []issueReadinessPR{}
		for _, p := range group {
			if !strings.EqualFold(p.Pull.Repository, "rancher/rancher") && !strings.EqualFold(p.Pull.Repository, "rancher/rancher-prime") {
				candidates = append(candidates, p)
				continue
			}
			if p.Error != "" || !readinessKnownRelease(p.Pull.BaseRef) && !readinessApplicable(p.Pull, "") || readinessApplicable(p.Pull, build.Line) {
				candidates = append(candidates, p)
			}
		}
		if len(candidates) == 0 {
			// A default-branch fix still needs evidence in release heads unless an explicit backport covers it.
			for _, p := range group {
				if p.Error != "" || readinessApplicable(p.Pull, "") || !strings.EqualFold(p.Pull.Repository, issueRepo) {
					candidates = append(candidates, p)
				}
			}
		}
		if len(candidates) == 0 {
			continue
		}
		required++
		covered := false
		for _, p := range candidates {
			if p.Error != "" || !p.Pull.Merged {
				continue
			}
			for _, m := range build.Matches {
				if strings.EqualFold(m.PR, p.Pull.URL) && m.Server.Verdict == "included" && (m.Agent.Verdict == "included" || strings.EqualFold(p.Pull.Repository, "rancher/webhook") && m.Agent.Verdict == "not_applicable") {
					covered = true
				}
			}
		}
		if covered {
			included++
		}
	}
	return required, included
}

// Explicit PR issue declarations are common in Steve/webhook, where GitHub's Development link is absent.
var readinessDeclaredIssueURL = regexp.MustCompile(`https://github\.com/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+/issues/[1-9][0-9]*`)
var readinessIssueDeclaration = regexp.MustCompile(`(?i)^(?:issue(?:s)?|fix(?:es|ed)?|close[sd]?|resolve[sd]?)\s*:?\s+(.+)$`)

func readinessDeclaredIssue(body, issueURL string, target prBuildTarget) bool {
	fence := ""
	issueHeading := false
	for _, line := range strings.Split(readinessHTMLComment.ReplaceAllString(body, ""), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			if fence == "" {
				fence = line[:3]
			} else if strings.HasPrefix(line, fence) {
				fence = ""
			}
			continue
		}
		if fence != "" || strings.HasPrefix(line, ">") {
			continue
		}
		if line == "" {
			continue
		}
		plain := strings.TrimSpace(strings.TrimLeft(line, "#*- "))
		if strings.EqualFold(strings.TrimSuffix(plain, ":"), "issue") || strings.EqualFold(strings.TrimSuffix(plain, ":"), "issues") {
			issueHeading = true
			continue
		}
		declaration := readinessIssueDeclaration.FindStringSubmatch(plain)
		if issueHeading && (strings.HasPrefix(plain, "https://github.com/") || strings.HasPrefix(plain, "[")) {
			declaration = []string{plain, plain}
		}
		issueHeading = false
		if len(declaration) != 2 {
			continue
		}
		for _, raw := range readinessDeclaredIssueURL.FindAllString(declaration[1], -1) {
			if strings.EqualFold(raw, issueURL) {
				return true
			}
		}
		m := readinessIssuePattern.FindStringSubmatch(issueURL)
		if len(m) == 4 && strings.EqualFold(m[1]+"/"+m[2], target.owner+"/"+target.repository) {
			for _, ref := range readinessShortPR.FindAllStringSubmatch(declaration[1], -1) {
				if ref[1] == "" && ref[2] == m[3] {
					return true
				}
			}
		}
	}
	return false
}
