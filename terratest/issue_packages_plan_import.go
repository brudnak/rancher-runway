package test

import (
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var issuePackagePlanCaseAnchor = regexp.MustCompile(`(?m)^<a name="test-([0-9]+)"></a>\s*$`)
var issuePackagePlanCaseHeading = regexp.MustCompile(`(?m)^# [0-9]+ / (.*) Status: (?:✅ PASS|❌ FAIL|🚧 BLOCKED|⏭️ SKIPPED|⏸️ NOT TESTED YET)\s*$`)
var issuePackagePlanNumberedLine = regexp.MustCompile(`^\s*[0-9]+[.)]\s+(.+)$`)
var issuePackagePlanEscapedCharacter = regexp.MustCompile("\\\\([\\\\`*_\\[\\]#!|])")

func decodeIssuePackagePlanText(value string) string {
	lines := strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "  ")
	}
	// Decode entities after Markdown escapes: authored &lt; must remain text.
	return html.UnescapeString(issuePackagePlanEscapedCharacter.ReplaceAllString(strings.TrimSpace(strings.Join(lines, "\n")), "$1"))
}

func issuePackagePlanSection(text, heading, end string) (string, bool) {
	_, rest, found := strings.Cut(text, heading+"\n")
	if !found {
		return "", false
	}
	value, _, ended := strings.Cut(rest, end)
	return strings.TrimSpace(value), ended
}

// Visible Markdown is authoritative on import. Only explicit case definitions
// are imported; rendered results and environment observations are never claimed
// as a new executed session. Generic numbered title lists are also accepted.
func parseIssuePackagePlanMarkdown(raw []byte) (issuePackageDraftPlan, error) {
	plan := issuePackageDraftPlan{Title: "Imported test plan", Status: "planning", Cases: []issuePackageCase{}}
	if len(raw) > issuePackageManifestLimit || !utf8.Valid(raw) || strings.ContainsRune(string(raw), 0) {
		return plan, fmt.Errorf("test plan Markdown must be valid text under 8 MiB")
	}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	_, body, generated := strings.Cut(text, "### 🧪 Test Cases\n")
	anchors := issuePackagePlanCaseAnchor.FindAllStringSubmatchIndex(body, -1)
	if generated {
		header, _, hasTable := strings.Cut(body, "| # | Priority | Description & Link | PASS/FAIL |")
		if !hasTable {
			return plan, fmt.Errorf("the test plan table heading is missing; preserve generated section headings when editing")
		}
		for _, line := range strings.Split(strings.TrimSpace(header), "\n") {
			if strings.HasPrefix(line, "**") && strings.HasSuffix(line, "**") && !strings.HasPrefix(line, "**Issue:") && line != "**Plan summary**" {
				plan.Title = decodeIssuePackagePlanText(strings.TrimSuffix(strings.TrimPrefix(line, "**"), "**"))
				break
			}
		}
		for _, line := range strings.Split(header, "\n") {
			if value, found := strings.CutPrefix(line, "**Issue:** "); found {
				plan.IssueURL = decodeIssuePackagePlanText(value)
			}
			if value, found := strings.CutPrefix(line, "**Fix:** "); found {
				plan.FixURL = decodeIssuePackagePlanText(value)
			}
		}
		if _, value, found := strings.Cut(header, "**Plan summary**\n"); found {
			value, _, _ = strings.Cut(value, "\nResults from **")
			plan.Summary = decodeIssuePackagePlanText(value)
		}
		seen := map[int]bool{}
		for i, anchor := range anchors {
			number, _ := strconv.Atoi(body[anchor[2]:anchor[3]])
			if number < 1 || seen[number] {
				return plan, fmt.Errorf("test plan has a duplicate or invalid case anchor")
			}
			seen[number] = true
			end := len(body)
			if i+1 < len(anchors) {
				end = anchors[i+1][0]
			}
			block := body[anchor[1]:end]
			heading := issuePackagePlanCaseHeading.FindStringSubmatch(block)
			if heading == nil {
				return plan, fmt.Errorf("test %d is missing its title heading", number)
			}
			c := issuePackageCase{Title: decodeIssuePackagePlanText(heading[1]), Automation: "manual", Steps: []issuePackageStep{}, Selection: []string{}}
			if value, found := issuePackagePlanSection(block, "**Preconditions**", "**Test Steps for Validation**"); found {
				c.Preconditions = decodeIssuePackagePlanText(value)
			}
			steps, found := issuePackagePlanSection(block, "**Test Steps for Validation**", "**✅ Expected Outcome**")
			if !found {
				return plan, fmt.Errorf("test %d is missing its validation steps section", number)
			}
			if steps != "_Add steps as you test._" {
				var step *issuePackageStep
				expected := false
				for _, line := range strings.Split(steps, "\n") {
					if match := issuePackagePlanNumberedLine.FindStringSubmatch(line); match != nil && !strings.HasPrefix(line, "   ") {
						c.Steps = append(c.Steps, issuePackageStep{Instruction: line[strings.Index(line, match[1]):]})
						step, expected = &c.Steps[len(c.Steps)-1], false
					} else if value, found := strings.CutPrefix(line, "   - Expected: "); found && step != nil {
						step.Expected, expected = value, true
					} else if step != nil && strings.HasPrefix(line, "   ") {
						if expected {
							step.Expected += "\n" + strings.TrimPrefix(line, "     ")
						} else {
							step.Instruction += "\n" + strings.TrimPrefix(line, "   ")
						}
					} else if strings.TrimSpace(line) != "" {
						return plan, fmt.Errorf("test %d contains a step without a number; use one numbered step per paragraph", number)
					}
				}
				for n := range c.Steps {
					c.Steps[n].Instruction = decodeIssuePackagePlanText(c.Steps[n].Instruction)
					c.Steps[n].Expected = decodeIssuePackagePlanText(c.Steps[n].Expected)
				}
			}
			value, found := issuePackagePlanSection(block, "**✅ Expected Outcome**", "**✅ Actual Outcome**")
			if !found {
				return plan, fmt.Errorf("test %d is missing its expected outcome section", number)
			}
			if value != "_Add the expected outcome._" {
				c.Expected = decodeIssuePackagePlanText(value)
			}
			plan.Cases = append(plan.Cases, c)
		}
		if len(anchors) == 0 && !strings.Contains(body, "No test cases have been added yet.") {
			return plan, fmt.Errorf("no generated case anchors were found; preserve case anchors when editing")
		}
	} else {
		for _, line := range strings.Split(text, "\n") {
			if value, found := strings.CutPrefix(line, "# "); found && plan.Title == "Imported test plan" {
				plan.Title = decodeIssuePackagePlanText(value)
				continue
			}
			if match := issuePackagePlanNumberedLine.FindStringSubmatch(line); match != nil {
				plan.Cases = append(plan.Cases, issuePackageCase{Title: decodeIssuePackagePlanText(match[1]), Automation: "manual", Steps: []issuePackageStep{}, Selection: []string{}})
			} else if strings.TrimSpace(line) != "" && !strings.HasPrefix(line, "# ") {
				return plan, fmt.Errorf("use a generated Runway plan or a numbered list of test case titles")
			}
		}
		if len(plan.Cases) == 0 {
			return plan, fmt.Errorf("the Markdown file contains no test case titles")
		}
	}
	if !packageText(plan.Title, 240, true) || !packageURL(plan.IssueURL) || !packageURL(plan.FixURL) || !packageText(plan.Summary, 32000, false) {
		return plan, fmt.Errorf("imported plan title, issue URL, or summary is invalid")
	}
	if err := validateIssuePackageCases(generatedPackageCases(plan.Cases)); err != nil {
		return plan, err
	}
	return plan, nil
}
