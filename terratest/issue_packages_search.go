package test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	issuePackageSearchMatchLimit = 200
	issuePackageSearchByteLimit  = 64 << 20
	issuePackageSearchFileLimit  = 4000
	issuePackageSearchFileBytes  = 1 << 20
)

type issuePackageSearchMatch struct {
	ID           string     `json:"id"`
	Kind         string     `json:"kind"`
	PackageID    string     `json:"packageId"`
	PackageTitle string     `json:"packageTitle"`
	Title        string     `json:"title"`
	Field        string     `json:"field"`
	Snippet      string     `json:"snippet"`
	Status       string     `json:"status"`
	Archived     bool       `json:"archived"`
	CaseID       string     `json:"caseId,omitempty"`
	SessionID    string     `json:"sessionId,omitempty"`
	SourceID     string     `json:"sourceId,omitempty"`
	GitPath      string     `json:"gitPath,omitempty"`
	Path         string     `json:"path,omitempty"`
	Repository   string     `json:"repository,omitempty"`
	GitBranch    string     `json:"gitBranch,omitempty"`
	SavedAt      *time.Time `json:"savedAt,omitempty"`
	UpdatedAt    *time.Time `json:"updatedAt,omitempty"`
	fieldPrefix  string
}
type issuePackageSearchResponse struct {
	Query     string                    `json:"query"`
	Matches   []issuePackageSearchMatch `json:"matches"`
	Truncated bool                      `json:"truncated"`
	Warnings  []string                  `json:"warnings"`
}
type issuePackageSearch struct {
	ctx          context.Context
	out          issuePackageSearchResponse
	needle       string
	bytes, files int
}

// Search reads saved authored text and managed Markdown only. It never reads
// evidence, environment connections, credentials, databases, or working drafts,
// and never launches Git or refreshes a repository.
func (s *issuePackageService) searchPlans(ctx context.Context, query string) (issuePackageSearchResponse, error) {
	query = strings.TrimSpace(query)
	search := issuePackageSearch{ctx: ctx, needle: strings.ToLower(query), out: issuePackageSearchResponse{Query: query, Matches: []issuePackageSearchMatch{}, Warnings: []string{}}}
	if !utf8.ValidString(query) || utf8.RuneCountInString(query) > 200 || strings.ContainsRune(query, 0) {
		return search.out, fmt.Errorf("search for a phrase of up to 200 characters")
	}
	if query == "" {
		return search.out, nil
	}
	// Stored packages are replaced with private clones on every mutation, so
	// these value snapshots stay immutable while scanning outside the lock.
	s.mu.Lock()
	packages := make([]issuePackage, 0, len(s.packages))
	for _, pkg := range s.packages {
		packages = append(packages, pkg)
	}
	s.mu.Unlock()
	sort.Slice(packages, func(i, j int) bool {
		if packages[i].UpdatedAt.Equal(packages[j].UpdatedAt) {
			return packages[i].ID < packages[j].ID
		}
		return packages[i].UpdatedAt.After(packages[j].UpdatedAt)
	})
	for _, pkg := range packages {
		if search.stopped() {
			break
		}
		search.packageText(pkg)
	}
	for _, pkg := range packages {
		if search.stopped() {
			break
		}
		search.history(s, pkg)
	}
	if !search.stopped() {
		search.repositories(s, packages)
	}
	return search.out, ctx.Err()
}
func (s *issuePackageSearch) stopped() bool {
	return s.out.Truncated || s.ctx.Err() != nil
}
func (s *issuePackageSearch) warn(message string) {
	for _, existing := range s.out.Warnings {
		if existing == message {
			return
		}
	}
	s.out.Warnings = append(s.out.Warnings, message)
}
func (s *issuePackageSearch) reserve(n int) bool {
	if s.stopped() {
		return false
	}
	if s.bytes+n > issuePackageSearchByteLimit {
		s.out.Truncated = true
		s.warn("Search reached its reading limit; use a more specific phrase.")
		return false
	}
	s.bytes += n
	return true
}
func (s *issuePackageSearch) add(match issuePackageSearchMatch, field, text string) {
	if text == "" || !s.reserve(len(text)) {
		return
	}
	s.addReserved(match, field, text)
}
func (s *issuePackageSearch) addReserved(match issuePackageSearchMatch, field, text string) {
	if s.stopped() {
		return
	}
	// Match the redacted text too, so searching for a credential cannot reveal
	// its presence in an otherwise unrelated plan.
	safe, _ := redactIssuePackageLog([]byte(text), nil)
	text = string(safe)
	lower := strings.ToLower(text)
	index := strings.Index(lower, s.needle)
	if index < 0 {
		return
	}
	if len(s.out.Matches) >= issuePackageSearchMatchLimit {
		s.out.Truncated = true
		return
	}
	field = match.fieldPrefix + field
	match.Field = field
	match.Snippet = issuePackageSearchSnippet(text, utf8.RuneCountInString(lower[:index]), utf8.RuneCountInString(s.needle))
	match.Title = issuePackageSearchLabel(match.Title)
	match.PackageTitle = issuePackageSearchLabel(match.PackageTitle)
	match.ID = issuePackageDigest([]byte(strings.Join([]string{match.Kind, match.PackageID, match.CaseID, match.SessionID, match.SourceID, match.Repository, match.GitBranch, match.GitPath, field}, "\x00")))[:24]
	s.out.Matches = append(s.out.Matches, match)
}
func issuePackageSearchLabel(value string) string {
	raw, _ := redactIssuePackageLog([]byte(value), nil)
	runes := []rune(strings.Join(strings.Fields(string(raw)), " "))
	if len(runes) > 240 {
		return string(runes[:240]) + "…"
	}
	return string(runes)
}
func issuePackageSearchSnippet(value string, index, length int) string {
	runes := []rune(value)
	start, end := max(0, index-75), min(len(runes), index+length+110)
	text := strings.Join(strings.Fields(string(runes[start:end])), " ")
	if start > 0 {
		text = "…" + text
	}
	if end < len(runes) {
		text += "…"
	}
	return text
}
func issuePackageSearchBase(pkg issuePackage) issuePackageSearchMatch {
	result := issuePackageSearchMatch{Kind: "plan", PackageID: pkg.ID, PackageTitle: pkg.Title, Title: pkg.Title, Status: pkg.Status, Archived: pkg.Status == "archived"}
	if !pkg.UpdatedAt.IsZero() {
		result.UpdatedAt = &pkg.UpdatedAt
	}
	return result
}
func (s *issuePackageSearch) packageText(pkg issuePackage) {
	base := issuePackageSearchBase(pkg)
	for _, value := range []struct{ label, text string }{{"Plan title", pkg.Title}, {"Issue title", pkg.IssueTitle}, {"Issue link", pkg.IssueURL}, {"Fix title", pkg.FixTitle}, {"Fix link", pkg.FixURL}, {"Summary", pkg.Summary}, {"Plan notes", pkg.Notes}} {
		s.add(base, value.label, value.text)
	}
	for _, c := range pkg.Cases {
		s.caseText(base, c)
	}
	for _, session := range pkg.Sessions {
		if s.stopped() {
			return
		}
		m := base
		m.Kind, m.SessionID, m.Title = "session", session.ID, session.Name
		for _, value := range []struct{ label, text string }{{"Session name", session.Name}, {"Session purpose", session.Purpose}, {"Session fix", session.FixTitle}, {"Session fix link", session.FixURL}, {"Session fix commit", session.FixCommit}, {"Conclusion", session.Conclusion}, {"Finding", session.Finding}} {
			s.add(m, value.label, value.text)
		}
		caseTitles := map[string]string{}
		for _, c := range session.Cases {
			s.caseText(m, c)
			caseTitles[c.ID] = c.Title
		}
		for _, result := range session.Results {
			m.CaseID = result.CaseID
			m.Title = session.Name + " · " + caseTitles[result.CaseID]
			s.add(m, "Session notes", result.Notes)
			s.add(m, "Result", result.Outcome)
		}
		if session.Baseline != nil {
			s.baselineText(base, session)
		}
	}
}
func (s *issuePackageSearch) baselineText(base issuePackageSearchMatch, owner issuePackageSession) {
	baseline := owner.Baseline
	base.Kind, base.SessionID, base.SourceID = "session", owner.ID, baseline.SessionID
	base.Title = owner.Name + " · Baseline: " + baseline.Name
	base.fieldPrefix = "Baseline "
	for _, value := range []struct{ label, text string }{{"session name", baseline.Name}, {"purpose", baseline.Purpose}, {"conclusion", baseline.Conclusion}, {"finding", baseline.Finding}} {
		s.add(base, value.label, value.text)
	}
	caseTitles := map[string]string{}
	for _, c := range baseline.Cases {
		s.caseText(base, c)
		caseTitles[c.ID] = c.Title
	}
	for _, result := range baseline.Results {
		m := base
		m.CaseID = result.CaseID
		m.Title += " · " + caseTitles[result.CaseID]
		s.add(m, "notes", result.Notes)
		s.add(m, "result", result.Outcome)
	}
}
func (s *issuePackageSearch) caseText(base issuePackageSearchMatch, c issuePackageCase) {
	if base.Kind == "plan" {
		base.Kind = "case"
		base.Title = c.Title
	} else {
		base.Title += " · " + c.Title
	}
	base.CaseID = c.ID
	for _, value := range []struct{ label, text string }{{"Case title", c.Title}, {"Preconditions", c.Preconditions}, {"Expected outcome", c.Expected}} {
		s.add(base, value.label, value.text)
	}
	for i, step := range c.Steps {
		s.add(base, fmt.Sprintf("Step %d", i+1), step.Instruction)
		s.add(base, fmt.Sprintf("Step %d expected outcome", i+1), step.Expected)
	}
}
func (s *issuePackageSearch) readFile(path string) ([]byte, bool) {
	if s.stopped() {
		return nil, false
	}
	s.files++
	if s.files > issuePackageSearchFileLimit {
		s.out.Truncated = true
		return nil, false
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > issuePackageSearchFileBytes {
		s.warn("Some Markdown files were unavailable, linked, or larger than 1 MiB and were skipped.")
		return nil, false
	}
	if !s.reserve(int(info.Size())) {
		return nil, false
	}
	raw, err := issuePackageReadBounded(path, issuePackageSearchFileBytes)
	if err != nil || !utf8.Valid(raw) || strings.ContainsRune(string(raw), 0) {
		s.warn("Some Markdown files were unreadable and were skipped.")
		return nil, false
	}
	return raw, true
}
func (s *issuePackageSearch) history(store *issuePackageService, pkg issuePackage) {
	directory, err := store.planHistoryDirectory(pkg.ID)
	if err != nil {
		s.warn("Some saved Markdown history was unavailable and was skipped.")
		return
	}
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		s.warn("Some saved Markdown history was unavailable and was skipped.")
		return
	}
	type snapshot struct {
		name string
		at   time.Time
	}
	files := []snapshot{}
	for _, entry := range entries {
		if !issuePackagePlanHistoryName.MatchString(entry.Name()) {
			continue
		}
		if info, err := entry.Info(); err == nil {
			files = append(files, snapshot{entry.Name(), info.ModTime()})
		}
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].at.Equal(files[j].at) {
			return files[i].name < files[j].name
		}
		return files[i].at.After(files[j].at)
	})
	if len(files) > 50 {
		files = files[:50]
		s.warn("Only the latest 50 saved Markdown versions per plan were searched.")
	}
	for _, file := range files {
		if s.stopped() {
			return
		}
		path := filepath.Join(directory, file.name)
		raw, ok := s.readFile(path)
		if !ok {
			continue
		}
		if issuePackagePlanDigest(raw)+".md" != file.name {
			s.warn("Changed history files were preserved and excluded from search.")
			continue
		}
		m := issuePackageSearchBase(pkg)
		m.Kind, m.SourceID, m.Path = "history", strings.TrimSuffix(file.name, ".md"), path
		savedAt := file.at.UTC()
		m.SavedAt = &savedAt
		m.Title = issuePackageSearchMarkdownTitle(raw, pkg.Title)
		s.addReserved(m, "Saved Markdown", string(raw))
	}
}
func issuePackageSearchMarkdownTitle(raw []byte, fallback string) string {
	for _, line := range strings.SplitN(string(raw), "\n", 50) {
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return fallback
}
func (s *issuePackageSearch) repositories(store *issuePackageService, packages []issuePackage) {
	partialListing := false
	defer func() { s.out.Truncated = s.out.Truncated || partialListing }()
	value, _ := issuePackageGitServices.LoadOrStore(store, &issuePackageGitService{packages: store, runner: runIssuePackageGit, reviews: map[string]issuePackageGitPreview{}, now: time.Now})
	g := value.(*issuePackageGitService)
	g.mu.Lock()
	defer g.mu.Unlock()
	// Validate the entire managed parent chain before opening state or checkout.
	if err := issuePackageGitCheckPath(store.root, ".plan-git", true); os.IsNotExist(err) {
		return
	} else if err != nil {
		s.warn("Cached repository files were unavailable and were skipped.")
		return
	}
	state, err := g.loadState()
	if err != nil {
		s.warn("Saved repository settings could not be read; local plans were still searched.")
		return
	}
	type source struct {
		connection issuePackageGitConnection
		pkg        issuePackage
	}
	sources := []source{{connection: state.DefaultConnection}}
	for _, pkg := range packages {
		c, scope, _ := issuePackageGitEffective(state, pkg.ID)
		if scope == "plan" {
			sources = append(sources, source{c, pkg})
		}
	}
	seen := map[string]bool{}
	for _, source := range sources {
		if s.stopped() {
			return
		}
		if source.connection.Repository == "" {
			continue
		}
		c, err := issuePackageGitRemote(source.connection.Repository, source.connection.Branch)
		if err != nil {
			s.warn("An invalid saved repository connection was skipped.")
			continue
		}
		checkout := g.checkout(c)
		if seen[checkout] {
			continue
		}
		seen[checkout] = true
		if len(seen) > 32 {
			s.out.Truncated = true
			return
		}
		relative, _ := filepath.Rel(store.root, checkout)
		if err := issuePackageGitCheckPath(store.root, relative, true); os.IsNotExist(err) {
			continue
		} else if err != nil {
			s.warn("Some cached repository paths were unavailable or linked and were skipped.")
			continue
		}
		files, err := issuePackageGitMarkdownFiles(checkout)
		if err != nil {
			partialListing = true
			s.warn("Some cached repository Markdown exceeded its listing limit or was unavailable.")
		}
		for _, file := range files {
			if s.stopped() {
				return
			}
			if err := issuePackageGitCheckPath(checkout, filepath.FromSlash(file.Path), true); err != nil {
				s.warn("Some cached repository paths were unavailable or linked and were skipped.")
				continue
			}
			path := filepath.Join(checkout, filepath.FromSlash(file.Path))
			raw, ok := s.readFile(path)
			if !ok {
				continue
			}
			m := issuePackageSearchBase(source.pkg)
			m.Kind, m.GitPath, m.Path = "repository", file.Path, path
			m.Repository, m.GitBranch = c.Repository, c.Branch
			m.Title = issuePackageSearchMarkdownTitle(raw, file.Path)
			s.addReserved(m, "Repository Markdown", string(raw))
		}
	}
}
