package prbuild

import (
	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	"regexp"
	"strings"
)

type Milestone struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
	DueOn  string `json:"due_on"`
}

type Issue struct {
	Number      int       `json:"number"`
	Title       string    `json:"title"`
	URL         string    `json:"html_url"`
	State       string    `json:"state"`
	StateReason string    `json:"state_reason,omitempty"`
	Body        string    `json:"body,omitempty"`
	CreatedAt   string    `json:"created_at"`
	UpdatedAt   string    `json:"updated_at"`
	ClosedAt    string    `json:"closed_at,omitempty"`
	PullRequest *struct{} `json:"pull_request,omitempty"`
	Labels      []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Assignees []struct {
		Login string `json:"login"`
	} `json:"assignees"`
	Milestone *Milestone `json:"milestone"`
}

var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9_.-]{1,100}$`)

func NormalizeRepository(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !repositoryPattern.MatchString(value) || strings.HasSuffix(value, "/.") || strings.HasSuffix(value, "/..") {
		return "", &imagelookup.InputError{Message: "Enter a GitHub repository as owner/repo."}
	}
	return value, nil
}

func IssueQANone(issue Issue) bool {
	for _, label := range issue.Labels {
		if strings.EqualFold(strings.TrimSpace(label.Name), "QA/None") {
			return true
		}
	}
	return false
}
