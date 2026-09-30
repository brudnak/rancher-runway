package test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const testPackageBundleFormat = "rancher-runway/test-package"
const testPackageBundleVersion = 1

type testPackageBundledArtifact struct {
	Name      string `json:"name"`
	MediaType string `json:"mediaType"`
	SHA256    string `json:"sha256"`
	Data      string `json:"data"`
}
type testPackageBundle struct {
	Format     string                       `json:"format"`
	Version    int                          `json:"version"`
	ExportedAt time.Time                    `json:"exportedAt"`
	Package    testPackage                  `json:"package"`
	Artifacts  []testPackageBundledArtifact `json:"artifacts"`
}
type testPackageImportPreview struct {
	ReviewToken string   `json:"reviewToken"`
	Title       string   `json:"title"`
	IssueURL    string   `json:"issueUrl"`
	FixURL      string   `json:"fixUrl"`
	FixTitle    string   `json:"fixTitle"`
	Cases       int      `json:"cases"`
	Sessions    int      `json:"sessions"`
	Artifacts   int      `json:"artifacts"`
	Bytes       int64    `json:"bytes"`
	Warnings    []string `json:"warnings"`
}

func (b *testPackageBundle) UnmarshalJSON(raw []byte) error {
	if len(raw) > testPackageTotalLimit {
		return errors.New("package bundle exceeds 64 MiB")
	}
	if err := testPackageRejectDuplicateKeys(raw); err != nil {
		return err
	}
	type plain testPackageBundle
	var value plain
	if err := decodeTestPackage(raw, &value); err != nil {
		return err
	}
	*b = testPackageBundle(value)
	return nil
}

func testPackageRejectDuplicateKeys(raw []byte) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	var visit func() error
	visit = func() error {
		token, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			keys := map[string]bool{}
			for d.More() {
				token, err := d.Token()
				if err != nil {
					return err
				}
				key, ok := token.(string)
				if !ok || keys[key] {
					return errors.New("package JSON has duplicate or invalid fields")
				}
				keys[key] = true
				if err = visit(); err != nil {
					return err
				}
			}
		case '[':
			for d.More() {
				if err := visit(); err != nil {
					return err
				}
			}
		default:
			return errors.New("unexpected JSON delimiter")
		}
		_, err = d.Token()
		return err
	}
	if err := visit(); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("unexpected extra package JSON")
	}
	return nil
}

func testPackageDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func validateTestPackageBundle(bundle *testPackageBundle) (map[string][]byte, []string, error) {
	artifacts := map[string][]byte{}
	warnings := []string{}
	if bundle == nil || bundle.Format != testPackageBundleFormat || bundle.Version != testPackageBundleVersion || bundle.ExportedAt.IsZero() {
		return nil, nil, errors.New("choose a Runway Test Package bundle (format version 1)")
	}
	raw, err := json.Marshal(bundle)
	if err != nil || len(raw) > testPackageTotalLimit {
		return nil, nil, errors.New("package bundle exceeds 64 MiB")
	}
	if err = validateTestPackage(bundle.Package); err != nil {
		return nil, nil, err
	}
	declared := map[string]testPackageArtifact{}
	for _, session := range bundle.Package.Sessions {
		if session.Status == "active" {
			warnings = append(warnings, "Active session “"+session.Name+"” will import as completed and inconclusive; no tests will run.")
		}
		for _, e := range session.Evidence {
			if err := validateTestPackageEvidenceMetadata(e); err != nil {
				return nil, nil, err
			}
			if e.Artifact != nil {
				declared[e.Artifact.Name] = *e.Artifact
			}
		}
	}
	for _, artifact := range bundle.Artifacts {
		metadata, exists := declared[artifact.Name]
		if !exists || !validPackageArtifactName(artifact.Name) || artifacts[artifact.Name] != nil || metadata.MediaType != artifact.MediaType || metadata.SHA256 != artifact.SHA256 {
			return nil, nil, errors.New("bundle artifact is duplicated, undeclared, or does not match its evidence")
		}
		if len(artifact.Data) > base64.StdEncoding.EncodedLen(testPackageArtifactLimit) {
			return nil, nil, errors.New("bundle artifact exceeds 32 MiB")
		}
		data, err := base64.StdEncoding.Strict().DecodeString(artifact.Data)
		if err != nil || int64(len(data)) != metadata.Bytes || len(data) > testPackageArtifactLimit || testPackageDigest(data) != metadata.SHA256 {
			return nil, nil, errors.New("bundle artifact failed size or SHA-256 validation")
		}
		if (artifact.MediaType == "text/plain" && (!strings.HasSuffix(artifact.Name, ".log") || !utf8.Valid(data))) || (artifact.MediaType == "application/vnd.sqlite3" && (!strings.HasSuffix(artifact.Name, ".db") || len(data) < 16 || string(data[:16]) != "SQLite format 3\x00")) {
			return nil, nil, errors.New("bundle artifact content does not match its declared format")
		}
		artifacts[artifact.Name] = data
	}
	missing := 0
	for name := range declared {
		if _, ok := artifacts[name]; !ok {
			missing++
		}
	}
	if missing > 0 {
		warnings = append(warnings, fmt.Sprintf("%d evidence files were omitted; their metadata will be preserved without file contents.", missing))
	}
	warnings = append(warnings, "Import creates a separate local package. Source cluster IDs are preserved as historical metadata; no credentials or live connections are restored.")
	return artifacts, warnings, nil
}

// Strict allowlists prevent portable metadata from introducing hidden credentials
// or local paths into a package. Author-written prose remains unchanged.
func validateTestPackageEvidenceMetadata(e testPackageEvidence) error {
	raw := bytes.TrimSpace(e.Metadata)
	if len(raw) == 0 || raw[0] != '{' {
		return errors.New("evidence metadata must be an object")
	}
	if err := testPackageRejectDuplicateKeys(e.Metadata); err != nil {
		return err
	}
	switch e.Kind {
	case "test-run":
		var value testPackageTestRunMetadata
		return decodeTestPackage(e.Metadata, &value)
	case "cache-snapshot":
		var value testPackageCacheMetadata
		return decodeTestPackage(e.Metadata, &value)
	case "pod-logs":
		return validateTestPackageLogMetadata(e.Metadata)
	case "note":
		var value map[string]json.RawMessage
		if err := decodeTestPackage(e.Metadata, &value); err != nil || value == nil || len(value) > 0 {
			return errors.New("note evidence metadata must be an empty object")
		}
		return nil
	}
	return errors.New("unknown evidence kind")
}

func (s *testPackageService) buildBundle(id string, includeNotes bool, names []string) (testPackageBundle, error) {
	pkg, err := s.get(id)
	if err != nil {
		return testPackageBundle{}, err
	}
	if !includeNotes {
		pkg.Notes = ""
	}
	// Current collectors sanitize commands when recording them. Imported and
	// older packages may predate that boundary, so sanitize the export copy too.
	// Keep the preserved local record intact; a private backup uses this same path.
	for i := range pkg.Sessions {
		session := &pkg.Sessions[i]
		session.Environment.HelmCommand = sanitizeTestPackageHelmCommand(session.Environment.HelmCommand)
		if session.Baseline != nil {
			session.Baseline.Environment.HelmCommand = sanitizeTestPackageHelmCommand(session.Baseline.Environment.HelmCommand)
		}
	}
	bundle := testPackageBundle{Format: testPackageBundleFormat, Version: 1, ExportedAt: time.Now().UTC(), Package: pkg, Artifacts: []testPackageBundledArtifact{}}
	declared := map[string]testPackageArtifact{}
	for _, session := range pkg.Sessions {
		for _, e := range session.Evidence {
			if e.Artifact != nil {
				declared[e.Artifact.Name] = *e.Artifact
			}
		}
	}
	selected := map[string]bool{}
	for _, name := range names {
		if selected[name] {
			return bundle, errors.New("an artifact was selected more than once")
		}
		selected[name] = true
		metadata, ok := declared[name]
		if !ok {
			return bundle, errors.New("selected artifact no longer belongs to this package")
		}
		path, err := s.artifactPath(pkg.ID, name)
		if err != nil {
			return bundle, err
		}
		raw, err := testPackageReadBounded(path, testPackageArtifactLimit)
		if err != nil {
			return bundle, err
		}
		if int64(len(raw)) != metadata.Bytes || testPackageDigest(raw) != metadata.SHA256 {
			return bundle, errors.New("saved evidence changed; inspect the artifact before exporting")
		}
		bundle.Artifacts = append(bundle.Artifacts, testPackageBundledArtifact{Name: name, MediaType: metadata.MediaType, SHA256: metadata.SHA256, Data: base64.StdEncoding.EncodeToString(raw)})
	}
	for i := range bundle.Package.Sessions {
		for j := range bundle.Package.Sessions[i].Evidence {
			artifact := bundle.Package.Sessions[i].Evidence[j].Artifact
			if artifact != nil {
				artifact.Omitted = !selected[artifact.Name]
			}
		}
	}
	sort.Slice(bundle.Artifacts, func(i, j int) bool { return bundle.Artifacts[i].Name < bundle.Artifacts[j].Name })
	if _, _, err := validateTestPackageBundle(&bundle); err != nil {
		return bundle, err
	}
	return bundle, nil
}

func testPackageBundleFingerprint(bundle testPackageBundle) string {
	// Export time is descriptive, not package content; revision and all selected
	// evidence bytes remain covered so a changed plan cannot reuse a review.
	bundle.ExportedAt = time.Time{}
	raw, _ := json.Marshal(bundle)
	return testPackageDigest(raw)
}

type testPackageReview struct {
	Purpose     string `json:"purpose"`
	Fingerprint string `json:"fingerprint"`
	ExpiresAt   int64  `json:"expiresAt"`
}

func (p *localControlPanel) signTestPackageReview(purpose, fingerprint string) string {
	raw, _ := json.Marshal(testPackageReview{purpose, fingerprint, time.Now().Add(15 * time.Minute).Unix()})
	mac := hmac.New(sha256.New, []byte(p.token))
	mac.Write(raw)
	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func (p *localControlPanel) checkTestPackageReview(token, purpose, fingerprint string) error {
	fail := errors.New("the package review expired or its content changed; preview it again")
	pieces := strings.Split(token, ".")
	if len(pieces) != 2 || len(token) > 2048 {
		return fail
	}
	raw, err := base64.RawURLEncoding.DecodeString(pieces[0])
	if err != nil {
		return fail
	}
	signature, err := base64.RawURLEncoding.DecodeString(pieces[1])
	if err != nil {
		return fail
	}
	mac := hmac.New(sha256.New, []byte(p.token))
	mac.Write(raw)
	if !hmac.Equal(mac.Sum(nil), signature) {
		return fail
	}
	var review testPackageReview
	if json.Unmarshal(raw, &review) != nil || review.Purpose != purpose || review.Fingerprint != fingerprint || review.ExpiresAt < time.Now().Unix() {
		return fail
	}
	return nil
}

func (p *localControlPanel) previewTestPackageImport(bundle *testPackageBundle) (testPackageImportPreview, error) {
	data, warnings, err := validateTestPackageBundle(bundle)
	if err != nil {
		return testPackageImportPreview{}, err
	}
	preview := testPackageImportPreview{Title: bundle.Package.Title, IssueURL: bundle.Package.IssueURL, FixURL: bundle.Package.FixURL, FixTitle: bundle.Package.FixTitle, Cases: len(bundle.Package.Cases), Sessions: len(bundle.Package.Sessions), Artifacts: len(data), Warnings: warnings}
	for _, raw := range data {
		preview.Bytes += int64(len(raw))
	}
	// For import even the descriptive export timestamp is part of reviewed input.
	raw, _ := json.Marshal(bundle)
	preview.ReviewToken = p.signTestPackageReview("import", testPackageDigest(raw))
	return preview, nil
}

func (p *localControlPanel) handleTestPackageTransfer(ctx context.Context, s *testPackageService, req testPackageRequest) (any, error) {
	switch req.Action {
	case "report":
		pkg, err := s.get(req.ID)
		if err != nil {
			return nil, err
		}
		markdown, err := testPackageMarkdown(pkg, req.SessionID, req.IncludeNotes)
		return map[string]any{"markdown": markdown}, err
	case "export":
		bundle, err := s.buildBundle(req.ID, req.IncludeNotes, req.ArtifactNames)
		if err != nil {
			return nil, err
		}
		raw, err := json.MarshalIndent(bundle, "", "  ")
		if err != nil || len(raw) > testPackageTotalLimit {
			return nil, errors.New("export exceeds 64 MiB; select fewer artifacts")
		}
		report, err := testPackageMarkdown(bundle.Package, "", req.IncludeNotes)
		if err != nil {
			return nil, err
		}
		return s.saveTestPackageExport(bundle.Package.Title, raw, []byte(report))
	case "import-preview":
		return p.previewTestPackageImport(req.Bundle)
	case "import":
		if req.Confirm != typedConfirmationPhrase {
			return nil, errors.New("type confirm to import this package as a new copy")
		}
		data, _, err := validateTestPackageBundle(req.Bundle)
		if err != nil {
			return nil, err
		}
		raw, _ := json.Marshal(req.Bundle)
		if err = p.checkTestPackageReview(req.ReviewToken, "import", testPackageDigest(raw)); err != nil {
			return nil, err
		}
		pkg, err := s.importPackage(req.Bundle.Package, data)
		if err != nil {
			return nil, err
		}
		return map[string]any{"package": pkg}, nil
	case "archive-inspect", "archive-backup", "archive-list", "archive-load", "archive-create":
		return p.testPackageArchiveAction(ctx, s, req)
	}
	return nil, errors.New("unknown Test Package action")
}

func (s *testPackageService) saveTestPackageExport(title string, bundle, report []byte) (any, error) {
	directory := s.exportRoot
	if directory == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		directory = filepath.Join(home, "Downloads")
	}
	if err := os.MkdirAll(directory, 0755); err != nil {
		return nil, err
	}
	prefix := cacheLabID()[:8] + "-" + cacheLabFileLabel(title)
	names := []string{prefix + ".runway-test-package.json", prefix + "-report.md"}
	written := []string{}
	success := false
	defer func() {
		if !success {
			for _, path := range written {
				_ = os.Remove(path)
			}
		}
	}()
	for i, raw := range [][]byte{bundle, report} {
		path := filepath.Join(directory, names[i])
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return nil, err
		}
		written = append(written, path)
		_, err = file.Write(raw)
		if err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			return nil, err
		}
	}
	success = true
	return map[string]any{"filename": names[0], "path": written[0], "reportFilename": names[1], "reportPath": written[1]}, nil
}

func testPackageMarkdownText(s string) string {
	escape := strings.NewReplacer("\\", "\\\\", "<", "&lt;", ">", "&gt;", "&", "&amp;", "`", "\\`", "*", "\\*", "_", "\\_", "[", "\\[", "]", "\\]", "#", "\\#", "|", "\\|", "!", "\\!")
	lines := strings.Split(strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n"), "\n")
	for i, line := range lines {
		lines[i] = escape.Replace(line)
	}
	return strings.Join(lines, "  \n")
}
func testPackageReportValue(s string) string {
	if strings.TrimSpace(s) == "" {
		return "Unknown"
	}
	return testPackageMarkdownText(s)
}
func testPackageReportTime(t time.Time) string {
	if t.IsZero() {
		return "Not recorded"
	}
	return t.UTC().Format("Jan 2, 2006 · 15:04 UTC")
}
func testPackageMarkdown(pkg testPackage, sessionID string, includeNotes bool) (string, error) {
	if err := validateTestPackage(pkg); err != nil {
		return "", err
	}
	var out strings.Builder
	fmt.Fprintf(&out, "# %s\n\n**Runway Test Package** · %s\n\n", testPackageMarkdownText(pkg.Title), testPackageMarkdownText(pkg.Status))
	if pkg.IssueURL != "" {
		fmt.Fprintf(&out, "**Issue:** %s\n\n", testPackageMarkdownText(pkg.IssueURL))
	}
	if pkg.IssueTitle != "" {
		fmt.Fprintf(&out, "**Issue title:** %s\n\n", testPackageMarkdownText(pkg.IssueTitle))
	}
	if pkg.FixURL != "" {
		fmt.Fprintf(&out, "**Fix:** %s\n\n", testPackageMarkdownText(pkg.FixURL))
	}
	if pkg.FixTitle != "" {
		fmt.Fprintf(&out, "**Fix title:** %s\n\n", testPackageMarkdownText(pkg.FixTitle))
	}
	if pkg.Summary != "" {
		fmt.Fprintf(&out, "%s\n\n", testPackageMarkdownText(pkg.Summary))
	}
	fmt.Fprintf(&out, "| Package | Updated | Planned cases | Recorded sessions |\n| --- | --- | ---: | ---: |\n| `%s` | %s | %d | %d |\n\n", pkg.ID, testPackageReportTime(pkg.UpdatedAt), len(pkg.Cases), len(pkg.Sessions))
	out.WriteString("> Outcomes are recorded per case. Manual step timestamps are markers, not proof that logs were collected or that all behavior was covered.\n\n")
	sessions := pkg.Sessions
	if sessionID != "" {
		sessions = nil
		for _, session := range pkg.Sessions {
			if session.ID == sessionID {
				sessions = append(sessions, session)
			}
		}
		if len(sessions) == 0 {
			return "", errors.New("session not found")
		}
	}
	reportPackage := pkg
	reportPackage.Sessions = sessions
	testPackageWriteJourneyReport(&out, reportPackage)
	if len(sessions) > 0 {
		out.WriteString("## Session comparison\n\n| Session | Purpose | Rancher / Kubernetes | Finding | Passed / failed / blocked / skipped / not run |\n| --- | --- | --- | --- | --- |\n")
		for _, session := range sessions {
			counts := map[string]int{}
			for _, result := range session.Results {
				counts[result.Outcome]++
			}
			fmt.Fprintf(&out, "| %s | %s | %s / %s | %s | %d / %d / %d / %d / %d |\n", testPackageReportCell(session.Name), testPackageReportCell(session.Purpose), testPackageReportCell(session.Environment.RancherVersion), testPackageReportCell(session.Environment.KubernetesVersion), testPackageReportCell(session.Finding), counts["passed"], counts["failed"], counts["blocked"], counts["skipped"], counts["not-run"])
		}
		out.WriteString("\n")
	}
	if len(sessions) == 0 {
		out.WriteString("## Test plan\n\nNo execution sessions have been recorded.\n\n")
		for index, c := range pkg.Cases {
			testPackageWriteReportCase(&out, index, c, testPackageResult{Outcome: "not-run"}, false)
		}
	}
	for _, session := range sessions {
		fmt.Fprintf(&out, "## %s\n\n**%s** · %s · Finding: **%s**\n\nStarted %s · Finished %s\n\n", testPackageMarkdownText(session.Name), testPackageMarkdownText(session.Purpose), testPackageMarkdownText(session.Status), testPackageMarkdownText(session.Finding), testPackageReportTime(session.StartedAt), testPackageReportTime(session.FinishedAt))
		if session.FixURL != "" {
			fix := testPackageMarkdownText(session.FixURL)
			if title := strings.TrimSpace(session.FixTitle); title != "" {
				fix = testPackageMarkdownText(title) + " · " + fix
			}
			if session.FixCommit != "" {
				fix += " · commit " + session.FixCommit
			}
			fmt.Fprintf(&out, "**Fix under test:** %s (recorded when this session started)\n\n", fix)
		}
		testPackageWriteBaselineReport(&out, session)
		env := session.Environment
		fmt.Fprintf(&out, "### Environment snapshot\n\n| Cluster | Rancher | Kubernetes | Source | Recorded |\n| --- | --- | --- | --- | --- |\n| %s | %s | %s | %s | %s |\n\n", testPackageReportCell(env.ClusterName), testPackageReportCell(env.RancherVersion), testPackageReportCell(env.KubernetesVersion), testPackageReportCell(env.Source), testPackageReportTime(env.RecordedAt))
		if env.ClusterID != "" {
			fmt.Fprintf(&out, "Cluster identity: %s. Target URL is omitted.\n\n", testPackageMarkdownText(env.ClusterID))
		}
		if env.Configuration != "" {
			fmt.Fprintf(&out, "**Configuration notes:** %s\n\n", testPackageMarkdownText(env.Configuration))
		}
		if len(env.Details) > 0 {
			out.WriteString("| Detail | Value | Provenance | Observed |\n| --- | --- | --- | --- |\n")
			for _, detail := range env.Details {
				observed := "—"
				if detail.ObservedAt != nil {
					observed = testPackageReportTime(*detail.ObservedAt)
				}
				fmt.Fprintf(&out, "| %s | %s | %s | %s |\n", testPackageReportCell(detail.Label), testPackageReportCell(detail.Value), testPackageReportCell(detail.Source), observed)
			}
			out.WriteString("\n")
		}
		if command := sanitizeTestPackageHelmCommand(env.HelmCommand); command != "" {
			fmt.Fprintf(&out, "**Recorded Helm install command** (credentials, hostnames, custom values, and local paths redacted; review placeholders before reuse):\n\n```sh\n%s\n```\n\n", command)
		}
		for _, warning := range env.Warnings {
			fmt.Fprintf(&out, "> %s\n\n", testPackageMarkdownText(warning))
		}
		for _, image := range env.Images {
			fmt.Fprintf(&out, "- Image: %s\n", testPackageMarkdownText(image))
		}
		if len(env.Images) > 0 {
			out.WriteString("\n")
		}
		counts := map[string]int{}
		results := map[string]testPackageResult{}
		for _, result := range session.Results {
			counts[result.Outcome]++
			results[result.CaseID] = result
		}
		fmt.Fprintf(&out, "### Results\n\n| Passed | Failed | Blocked | Skipped | Not run |\n| ---: | ---: | ---: | ---: | ---: |\n| %d | %d | %d | %d | %d |\n\n", counts["passed"], counts["failed"], counts["blocked"], counts["skipped"], counts["not-run"])
		for index, c := range session.Cases {
			testPackageWriteReportCase(&out, index, c, results[c.ID], true)
		}
		if session.Conclusion != "" {
			fmt.Fprintf(&out, "### Conclusion\n\n%s\n\n", testPackageMarkdownText(session.Conclusion))
		}
		if len(session.Evidence) > 0 {
			out.WriteString("### Preserved evidence\n\n")
			for _, e := range session.Evidence {
				fmt.Fprintf(&out, "- **%s** · %s · Captured %s\n", testPackageMarkdownText(e.Name), testPackageMarkdownText(e.Kind), testPackageReportTime(e.CapturedAt))
				if e.Description != "" {
					fmt.Fprintf(&out, "  %s\n", testPackageMarkdownText(e.Description))
				}
				if e.CaseID != "" {
					label := e.CaseID
					for _, c := range session.Cases {
						if c.ID == e.CaseID {
							label = c.Title
							for index, step := range c.Steps {
								if step.ID == e.StepID {
									label += fmt.Sprintf(" · step %d", index+1)
								}
							}
						}
					}
					fmt.Fprintf(&out, "  Scope: %s.\n", testPackageMarkdownText(label))
				}
				switch e.Kind {
				case "pod-logs":
					var metadata testPackageLogMetadata
					if decodeTestPackage(e.Metadata, &metadata) == nil {
						fmt.Fprintf(&out, "  Pod: %s/%s · Container: %s · Component: %s.\n", testPackageMarkdownText(metadata.Namespace), testPackageMarkdownText(metadata.Pod), testPackageMarkdownText(metadata.Container), testPackageMarkdownText(metadata.Component))
						fmt.Fprintf(&out, "  Collected %s to %s · Last %d seconds · Up to %d lines · Previous container: %t · Truncated: %t.\n", testPackageReportTime(metadata.StartedAt), testPackageReportTime(metadata.CompletedAt), metadata.SinceSeconds, metadata.TailLines, metadata.Previous, metadata.Truncated)
						fmt.Fprintf(&out, "  Pod UID: %s · Current container image: %s · Current runtime image: %s · Restarts: %d.\n", testPackageMarkdownText(metadata.PodUID), testPackageReportValue(metadata.Image), testPackageReportValue(metadata.ImageID), metadata.Restarts)
						if metadata.Previous {
							out.WriteString("  Logs are from the previous container instance; that instance's image is not verified.\n")
						}
						out.WriteString("  A bounded snapshot, not a continuous recording. Known credential patterns are redacted; review log contents before sharing.\n")
					}
				case "test-run":
					var metadata testPackageTestRunMetadata
					if decodeTestPackage(e.Metadata, &metadata) == nil {
						counts := map[string]int{}
						for _, result := range metadata.Results {
							counts[result.Status]++
						}
						fmt.Fprintf(&out, "  Source status: %s. Recorded result events: %d passed, %d failed, %d skipped. These source events do not set manual case outcomes.\n", testPackageReportValue(metadata.Status), counts["pass"]+counts["passed"], counts["fail"]+counts["failed"], counts["skip"]+counts["skipped"])
						if metadata.SHA != "" {
							fmt.Fprintf(&out, "  Pinned source revision: %s.\n", testPackageMarkdownText(metadata.SHA))
						}
					}
				case "cache-snapshot":
					var metadata testPackageCacheMetadata
					if decodeTestPackage(e.Metadata, &metadata) == nil {
						fmt.Fprintf(&out, "  Snapshot: %d tables · %d bytes.\n", metadata.Tables, metadata.Bytes)
						if metadata.SHA256 != "" {
							fmt.Fprintf(&out, "  SHA-256: %s.\n", testPackageMarkdownText(metadata.SHA256))
						}
						if metadata.Image != "" {
							fmt.Fprintf(&out, "  Source image: %s.\n", testPackageMarkdownText(metadata.Image))
						}
					}
				}
				if e.Artifact != nil {
					if e.Artifact.Omitted {
						fmt.Fprintf(&out, "  Metadata only: %s artifact (%d bytes) was omitted from this package copy.\n", testPackageMarkdownText(e.Artifact.MediaType), e.Artifact.Bytes)
					} else {
						fmt.Fprintf(&out, "  Artifact record: %s, %d bytes. Artifact bytes are separate from this report.\n", testPackageMarkdownText(e.Artifact.MediaType), e.Artifact.Bytes)
					}
				}
			}
			out.WriteString("\n")
		}
	}
	if includeNotes && pkg.Notes != "" {
		fmt.Fprintf(&out, "## Included private notes\n\n%s\n\n", testPackageMarkdownText(pkg.Notes))
	}
	out.WriteString("---\n\nPrepared from saved Runway records. Review author-written notes and evidence before sharing. Credentials, local file paths, target URLs, and artifact bytes are not added by the report generator.\n")
	return out.String(), nil
}
func testPackageWriteReportCase(out *strings.Builder, index int, c testPackageCase, result testPackageResult, recorded bool) {
	outcome := result.Outcome
	if outcome == "" {
		outcome = "not-run"
	}
	fmt.Fprintf(out, "#### %d. %s — %s\n\n", index+1, testPackageMarkdownText(c.Title), testPackageMarkdownText(outcome))
	if c.Preconditions != "" {
		fmt.Fprintf(out, "**Preconditions:** %s\n\n", testPackageMarkdownText(c.Preconditions))
	}
	if c.Expected != "" {
		fmt.Fprintf(out, "**Expected:** %s\n\n", testPackageMarkdownText(c.Expected))
	}
	done := map[string]testPackageStepResult{}
	for _, step := range result.Steps {
		done[step.StepID] = step
	}
	for n, step := range c.Steps {
		mark := done[step.ID]
		status := "Not marked"
		if mark.Done {
			status = "Marked " + testPackageReportTime(mark.At)
		}
		fmt.Fprintf(out, "**Step %d:** %s\n\n", n+1, testPackageMarkdownText(step.Instruction))
		if step.Expected != "" {
			fmt.Fprintf(out, "**Expected:** %s\n\n", testPackageMarkdownText(step.Expected))
		}
		if recorded {
			fmt.Fprintf(out, "*%s*\n\n", status)
		}
	}
	if len(c.Steps) > 0 {
		out.WriteString("\n")
	}
	if result.Notes != "" {
		fmt.Fprintf(out, "**Observed:** %s\n\n", testPackageMarkdownText(result.Notes))
	}
	fmt.Fprintf(out, "Automation: %s.\n\n", testPackageMarkdownText(c.Automation))
	if a := c.AutomationSource; a != nil {
		fmt.Fprintf(out, "Pinned automation: %s · rancher/tests@%s · tags: %s · timeout: %d minutes per suite.\n\n", testPackageMarkdownText(a.Name), a.SHA, testPackageMarkdownText(a.Tags), a.Timeout)
	}
	if c.AutomationURL != "" {
		fmt.Fprintf(out, "Automation reference: %s\n\n", testPackageMarkdownText(c.AutomationURL))
	}
	if len(c.Selection) > 0 {
		out.WriteString("Referenced Test Lab selections:\n\n")
		for _, selection := range c.Selection {
			fmt.Fprintf(out, "- %s\n", testPackageMarkdownText(selection))
		}
		out.WriteString("\n")
	}
}

func testPackageReportCell(s string) string {
	return testPackageReportValue(strings.Join(strings.Fields(s), " "))
}
