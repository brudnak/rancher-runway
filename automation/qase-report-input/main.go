package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	projectID       = "RM"
	metadataName    = "metadata.json"
	resultsName     = "results.json"
	maxJSONLineSize = 16 << 20
	maxResultEvents = 100000
)

var (
	commitHeadVersionRE = regexp.MustCompile(`^([0-9]+\.[0-9]+(?:\.[0-9]+)?)-([0-9A-Fa-f]{7,40})-head$`)
	safeVersionRE       = regexp.MustCompile(`^[0-9]+\.[0-9]+[0-9A-Za-z._+-]*$`)

	allowedPackages = map[string]string{
		"github.com/rancher/tests/validation/charts":          "validation/charts",
		"github.com/rancher/tests/validation/configmaps":      "validation/configmaps",
		"github.com/rancher/tests/validation/nodeannotations": "validation/nodeannotations",
		"github.com/rancher/tests/validation/schemas":         "validation/schemas",
		"github.com/rancher/tests/validation/steve/vai":       "validation/steve/vai",
	}

	friendlyLanes = map[string]string{
		"framework-regression":          "Frameworks Regression",
		"webhook-candidate-on-previous": "Webhook on Previous",
		"webhook-fresh-install":         "Webhook Fresh Install",
		"webhook-upgrade":               "Webhook Upgrade",
	}
)

type options struct {
	mode            string
	enabled         string
	lane            string
	rancherVersion  string
	sourceRunURL    string
	rancherTestsRef string
	resultsManifest string
	workspace       string
	outputDir       string
	inputDir        string
	artifact        string
	reporterRoot    string
	githubOutput    string
}

type manifest struct {
	Repo           string           `json:"repo"`
	Ref            string           `json:"ref"`
	Lane           string           `json:"lane"`
	RancherVersion string           `json:"rancher_version"`
	Results        []manifestResult `json:"results"`
}

type manifestResult struct {
	Suite      string `json:"suite"`
	Package    string `json:"package"`
	TestRun    string `json:"test_run"`
	JUnit      string `json:"junit"`
	GoJSON     string `json:"go_json"`
	Conclusion string `json:"conclusion"`
}

type metadata struct {
	Enabled         bool   `json:"enabled"`
	Title           string `json:"title"`
	Lane            string `json:"lane"`
	Version         string `json:"version"`
	SourceURL       string `json:"source_url"`
	RancherTestsRef string `json:"rancher_tests_ref"`
	Project         string `json:"project"`
	ResultCount     int    `json:"result_count"`
}

type rawEvent struct {
	Time    string          `json:"Time"`
	Action  string          `json:"Action"`
	Package string          `json:"Package"`
	Test    string          `json:"Test"`
	Elapsed json.RawMessage `json:"Elapsed"`
}

type resultEvent struct {
	Time    string      `json:"Time"`
	Action  string      `json:"Action"`
	Package string      `json:"Package"`
	Test    string      `json:"Test"`
	Elapsed json.Number `json:"Elapsed,omitempty"`
}

type resultState struct {
	run      bool
	terminal string
}

type schemaSuite struct {
	Projects []string     `json:"projects"`
	Suite    string       `json:"suite"`
	Cases    []schemaCase `json:"cases"`
}

type schemaCase struct {
	Title       string            `json:"title"`
	CustomField map[string]string `json:"custom_field"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "qase-report-input:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	var opts options
	flags := flag.NewFlagSet("qase-report-input", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&opts.mode, "mode", "", "prepare or verify")
	// This is deliberately a string flag. GitHub workflows pass `-enabled true`,
	// which the standard bool flag parser otherwise interprets as a positional arg.
	flags.StringVar(&opts.enabled, "enabled", "false", "whether Qase input should be prepared")
	flags.StringVar(&opts.lane, "lane", "", "sign-off lane")
	flags.StringVar(&opts.rancherVersion, "rancher-version", "", "resolved Rancher version")
	flags.StringVar(&opts.sourceRunURL, "source-run-url", "", "source GitHub Actions run URL")
	flags.StringVar(&opts.rancherTestsRef, "rancher-tests-ref", "", "rancher/tests ref")
	flags.StringVar(&opts.resultsManifest, "results-manifest", "", "Rancher test result manifest")
	flags.StringVar(&opts.workspace, "workspace", "", "trusted workspace root")
	flags.StringVar(&opts.outputDir, "output-dir", "", "prepared artifact directory")
	flags.StringVar(&opts.inputDir, "input-dir", "", "prepared artifact directory")
	flags.StringVar(&opts.artifact, "artifact", "", "deprecated alias for -input-dir")
	flags.StringVar(&opts.reporterRoot, "reporter-root", "", "extracted rancher/tests root")
	flags.StringVar(&opts.githubOutput, "github-output", "", "optional GitHub output file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %q", flags.Args())
	}

	switch opts.mode {
	case "prepare":
		enabled, err := strconv.ParseBool(opts.enabled)
		if err != nil {
			return fmt.Errorf("parse -enabled: %w", err)
		}
		return prepare(opts, enabled)
	case "verify":
		if opts.inputDir != "" && opts.artifact != "" && filepath.Clean(opts.inputDir) != filepath.Clean(opts.artifact) {
			return errors.New("-input-dir and -artifact identify different directories")
		}
		if opts.inputDir == "" {
			opts.inputDir = opts.artifact
		}
		return verify(opts)
	default:
		return fmt.Errorf("invalid -mode %q: expected prepare or verify", opts.mode)
	}
}

func prepare(opts options, enabled bool) error {
	if strings.TrimSpace(opts.outputDir) == "" {
		return errors.New("-output-dir is required")
	}

	meta := metadata{Enabled: false, Project: projectID}
	var events []resultEvent
	if enabled {
		if strings.TrimSpace(opts.resultsManifest) == "" || strings.TrimSpace(opts.workspace) == "" {
			return errors.New("-results-manifest and -workspace are required when enabled")
		}
		friendlyLane, ok := friendlyLanes[opts.lane]
		if !ok {
			return fmt.Errorf("unsupported lane %q", opts.lane)
		}
		normalizedVersion, err := normalizeVersion(opts.rancherVersion)
		if err != nil {
			return err
		}
		if err := validateSourceURL(opts.sourceRunURL); err != nil {
			return err
		}
		if err := validateSingleLine("rancher/tests ref", opts.rancherTestsRef, 256); err != nil {
			return err
		}

		resultManifest, err := readManifest(opts.resultsManifest)
		if err != nil {
			return err
		}
		if len(resultManifest.Results) == 0 {
			return errors.New("results manifest contains no results")
		}
		if resultManifest.Lane != "" && resultManifest.Lane != opts.lane {
			return fmt.Errorf("results manifest lane %q does not match %q", resultManifest.Lane, opts.lane)
		}
		if resultManifest.Ref != "" && resultManifest.Ref != opts.rancherTestsRef {
			return fmt.Errorf("results manifest ref %q does not match %q", resultManifest.Ref, opts.rancherTestsRef)
		}

		seenPaths := make(map[string]struct{})
		for i, result := range resultManifest.Results {
			if result.Conclusion != "success" {
				return fmt.Errorf("manifest result %d has conclusion %q; all results must be success", i, result.Conclusion)
			}
			path, err := containedRegularFile(opts.workspace, result.GoJSON)
			if err != nil {
				return fmt.Errorf("manifest result %d go_json: %w", i, err)
			}
			if _, exists := seenPaths[path]; exists {
				return fmt.Errorf("manifest result %d repeats go_json file %q", i, result.GoJSON)
			}
			seenPaths[path] = struct{}{}
			parsed, err := readAndSanitizeEvents(path)
			if err != nil {
				return fmt.Errorf("manifest result %d: %w", i, err)
			}
			events = append(events, parsed...)
			if len(events) > maxResultEvents {
				return fmt.Errorf("more than %d reportable result events", maxResultEvents)
			}
		}

		// reporter-v2 indexes results globally by the final slash-delimited test
		// name. Nested tests can legitimately reuse a dynamic leaf (for example,
		// the same Pod_<name> beneath several VAI checks). Validate the complete
		// stream first, then omit every identity behind an ambiguous leaf. Keeping
		// any one of them would let reporter-v2 silently attach an arbitrary result
		// to that name.
		if _, err := validateEventLifecycles(events, true); err != nil {
			return err
		}
		var omittedLeaves []string
		events, omittedLeaves = omitAmbiguousReporterLeaves(events)
		if len(omittedLeaves) != 0 {
			fmt.Fprintf(os.Stderr, "qase-report-input: omitted %d ambiguous reporter-v2 leaf name(s)\n",
				len(omittedLeaves))
		}

		count, _, err := validateEventSet(events, true)
		if err != nil {
			return err
		}
		meta = metadata{
			Enabled:         true,
			Title:           fmt.Sprintf("[frameworks][%s][%s]", normalizedVersion, friendlyLane),
			Lane:            opts.lane,
			Version:         strings.TrimSpace(opts.rancherVersion),
			SourceURL:       opts.sourceRunURL,
			RancherTestsRef: opts.rancherTestsRef,
			Project:         projectID,
			ResultCount:     count,
		}
	}

	resultsData, err := marshalEvents(events)
	if err != nil {
		return err
	}
	metadataData, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	metadataData = append(metadataData, '\n')
	return writeArtifact(opts.outputDir, metadataData, resultsData)
}

func verify(opts options) error {
	if strings.TrimSpace(opts.inputDir) == "" {
		return errors.New("-input-dir is required")
	}
	metadataPath := filepath.Join(opts.inputDir, metadataName)
	resultsPath := filepath.Join(opts.inputDir, resultsName)
	metadataData, err := readRegularNoSymlink(metadataPath, 64<<10)
	if err != nil {
		return fmt.Errorf("read metadata: %w", err)
	}
	resultsData, err := readRegularNoSymlink(resultsPath, 128<<20)
	if err != nil {
		return fmt.Errorf("read results: %w", err)
	}

	var meta metadata
	if err := strictUnmarshalObject(metadataData, &meta); err != nil {
		return fmt.Errorf("decode metadata: %w", err)
	}
	if meta.Project != projectID {
		return fmt.Errorf("metadata project must be %q", projectID)
	}

	if !meta.Enabled {
		if meta != (metadata{Project: projectID}) {
			return errors.New("disabled metadata contains non-canonical report fields")
		}
		if len(resultsData) != 0 {
			return errors.New("disabled report must contain an empty results.json")
		}
		return writeGitHubOutputs(opts.githubOutput, meta)
	}

	friendlyLane, ok := friendlyLanes[meta.Lane]
	if !ok {
		return fmt.Errorf("metadata has unsupported lane %q", meta.Lane)
	}
	normalizedVersion, err := normalizeVersion(meta.Version)
	if err != nil {
		return fmt.Errorf("metadata: %w", err)
	}
	expectedTitle := fmt.Sprintf("[frameworks][%s][%s]", normalizedVersion, friendlyLane)
	if meta.Title != expectedTitle {
		return fmt.Errorf("metadata title %q does not match %q", meta.Title, expectedTitle)
	}
	if err := validateSourceURL(meta.SourceURL); err != nil {
		return fmt.Errorf("metadata: %w", err)
	}
	if err := validateSingleLine("rancher/tests ref", meta.RancherTestsRef, 256); err != nil {
		return fmt.Errorf("metadata: %w", err)
	}

	events, err := decodeSanitizedEvents(resultsData)
	if err != nil {
		return err
	}
	count, leavesByPackage, err := validateEventSet(events, true)
	if err != nil {
		return err
	}
	if meta.ResultCount != count {
		return fmt.Errorf("metadata result_count is %d, validated results contain %d", meta.ResultCount, count)
	}
	if err := generateSchemas(opts.reporterRoot, leavesByPackage); err != nil {
		return err
	}
	return writeGitHubOutputs(opts.githubOutput, meta)
}

func readManifest(path string) (manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return manifest{}, fmt.Errorf("read results manifest: %w", err)
	}
	if len(data) > 1<<20 {
		return manifest{}, errors.New("results manifest exceeds 1 MiB")
	}
	var result manifest
	if err := strictUnmarshalObject(data, &result); err != nil {
		return manifest{}, fmt.Errorf("decode results manifest: %w", err)
	}
	return result, nil
}

func containedRegularFile(workspace, name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", errors.New("path is empty")
	}
	workspaceAbs, err := filepath.Abs(workspace)
	if err != nil {
		return "", fmt.Errorf("resolve workspace: %w", err)
	}
	workspaceResolved, err := filepath.EvalSymlinks(workspaceAbs)
	if err != nil {
		return "", fmt.Errorf("resolve workspace symlinks: %w", err)
	}
	info, err := os.Stat(workspaceResolved)
	if err != nil || !info.IsDir() {
		return "", errors.New("workspace is not a directory")
	}

	candidate := name
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(workspaceAbs, candidate)
	}
	candidate, err = filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	if !isWithin(workspaceAbs, candidate) {
		return "", errors.New("path escapes workspace")
	}
	candidateResolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve path symlinks: %w", err)
	}
	if !isWithin(workspaceResolved, candidateResolved) {
		return "", errors.New("resolved path escapes workspace")
	}
	info, err = os.Stat(candidateResolved)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("path is not a regular file")
	}
	return candidateResolved, nil
}

func isWithin(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func readAndSanitizeEvents(path string) ([]resultEvent, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var events []resultEvent
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), maxJSONLineSize)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			return nil, fmt.Errorf("line %d is empty", lineNumber)
		}
		if err := rejectDuplicateJSONKeys(line); err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNumber, err)
		}
		var raw rawEvent
		if err := json.Unmarshal(line, &raw); err != nil {
			return nil, fmt.Errorf("line %d: decode event: %w", lineNumber, err)
		}
		switch raw.Action {
		case "fail":
			return nil, fmt.Errorf("line %d contains a failed test event", lineNumber)
		case "run", "pass", "skip":
			if !strings.Contains(raw.Test, "/") {
				continue
			}
			event, err := sanitizeRawEvent(raw)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", lineNumber, err)
			}
			events = append(events, event)
		case "start", "output", "pause", "cont", "bench":
			// Non-result and package-level test2json events are intentionally dropped.
		default:
			return nil, fmt.Errorf("line %d has unknown test action %q", lineNumber, raw.Action)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan events: %w", err)
	}
	return events, nil
}

func sanitizeRawEvent(raw rawEvent) (resultEvent, error) {
	event := resultEvent{
		Time:    raw.Time,
		Action:  raw.Action,
		Package: raw.Package,
		Test:    raw.Test,
	}
	if err := validateResultIdentity(event); err != nil {
		return resultEvent{}, err
	}
	if event.Time == "" {
		return resultEvent{}, errors.New("event Time is empty")
	}
	if _, err := time.Parse(time.RFC3339Nano, event.Time); err != nil {
		return resultEvent{}, fmt.Errorf("invalid event Time: %w", err)
	}
	if len(raw.Elapsed) != 0 && !bytes.Equal(bytes.TrimSpace(raw.Elapsed), []byte("null")) {
		if event.Action == "run" {
			return resultEvent{}, errors.New("run event contains Elapsed")
		}
		elapsed, err := parseElapsed(raw.Elapsed)
		if err != nil {
			return resultEvent{}, err
		}
		event.Elapsed = elapsed
	} else if event.Action != "run" {
		return resultEvent{}, errors.New("terminal event has no Elapsed value")
	}
	return event, nil
}

func parseElapsed(raw json.RawMessage) (json.Number, error) {
	text := string(bytes.TrimSpace(raw))
	if text == "" || strings.HasPrefix(text, `"`) {
		return "", errors.New("Elapsed must be a JSON number")
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return "", fmt.Errorf("invalid Elapsed value %q", text)
	}
	return json.Number(text), nil
}

func marshalEvents(events []resultEvent) ([]byte, error) {
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	for _, event := range events {
		if err := encoder.Encode(event); err != nil {
			return nil, fmt.Errorf("marshal result event: %w", err)
		}
	}
	return output.Bytes(), nil
}

func decodeSanitizedEvents(data []byte) ([]resultEvent, error) {
	var events []resultEvent
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64<<10), maxJSONLineSize)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			return nil, fmt.Errorf("results line %d is empty", lineNumber)
		}
		var event resultEvent
		if err := strictUnmarshalObject(line, &event); err != nil {
			return nil, fmt.Errorf("results line %d: %w", lineNumber, err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(line, &fields); err != nil {
			return nil, fmt.Errorf("results line %d: decode fields: %w", lineNumber, err)
		}
		if event.Action != "run" && event.Action != "pass" && event.Action != "skip" {
			return nil, fmt.Errorf("results line %d has forbidden action %q", lineNumber, event.Action)
		}
		if err := validateResultIdentity(event); err != nil {
			return nil, fmt.Errorf("results line %d: %w", lineNumber, err)
		}
		if event.Time == "" {
			return nil, fmt.Errorf("results line %d has empty Time", lineNumber)
		}
		if _, err := time.Parse(time.RFC3339Nano, event.Time); err != nil {
			return nil, fmt.Errorf("results line %d has invalid Time: %w", lineNumber, err)
		}
		if event.Action == "run" {
			if _, present := fields["Elapsed"]; present {
				return nil, fmt.Errorf("results line %d run event contains Elapsed", lineNumber)
			}
		} else {
			rawElapsed, present := fields["Elapsed"]
			if !present {
				return nil, fmt.Errorf("results line %d terminal event has no Elapsed", lineNumber)
			}
			if _, err := parseElapsed(rawElapsed); err != nil {
				return nil, fmt.Errorf("results line %d: %w", lineNumber, err)
			}
		}
		events = append(events, event)
		if len(events) > maxResultEvents {
			return nil, fmt.Errorf("more than %d result events", maxResultEvents)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan results: %w", err)
	}
	return events, nil
}

func validateResultIdentity(event resultEvent) error {
	if _, ok := allowedPackages[event.Package]; !ok {
		return fmt.Errorf("package %q is not an allowed Runway test package", event.Package)
	}
	if !strings.Contains(event.Test, "/") {
		return fmt.Errorf("test %q is not a subtest", event.Test)
	}
	if err := validateSingleLine("test name", event.Test, 2048); err != nil {
		return err
	}
	if leafName(event.Test) == "" {
		return fmt.Errorf("test %q has an empty reporter-v2 leaf name", event.Test)
	}
	return nil
}

func validateEventSet(events []resultEvent, requireTerminal bool) (int, map[string][]string, error) {
	terminalCount, err := validateEventLifecycles(events, requireTerminal)
	if err != nil {
		return 0, nil, err
	}

	leaves := make(map[string]string)
	leavesByPackageSet := make(map[string]map[string]struct{})
	for _, event := range events {
		identity := event.Package + "\x00" + event.Test
		leaf := leafName(event.Test)
		if prior, exists := leaves[leaf]; exists && prior != identity {
			return 0, nil, fmt.Errorf("reporter-v2 leaf name collision for %q between %q and %q", leaf, prior, identity)
		}
		leaves[leaf] = identity

		if event.Action == "pass" || event.Action == "skip" {
			if leavesByPackageSet[event.Package] == nil {
				leavesByPackageSet[event.Package] = make(map[string]struct{})
			}
			leavesByPackageSet[event.Package][leaf] = struct{}{}
		}
	}

	leavesByPackage := make(map[string][]string, len(leavesByPackageSet))
	for pkg, values := range leavesByPackageSet {
		for leaf := range values {
			leavesByPackage[pkg] = append(leavesByPackage[pkg], leaf)
		}
		sort.Strings(leavesByPackage[pkg])
	}
	return terminalCount, leavesByPackage, nil
}

func validateEventLifecycles(events []resultEvent, requireTerminal bool) (int, error) {
	states := make(map[string]resultState)
	terminalCount := 0

	for _, event := range events {
		identity := event.Package + "\x00" + event.Test
		state := states[identity]
		switch event.Action {
		case "run":
			if state.run {
				return 0, fmt.Errorf("duplicate run event for %q", event.Test)
			}
			if state.terminal != "" {
				return 0, fmt.Errorf("run event follows terminal event for %q", event.Test)
			}
			state.run = true
		case "pass", "skip":
			if !state.run {
				return 0, fmt.Errorf("terminal event has no preceding run for %q", event.Test)
			}
			if state.terminal != "" {
				return 0, fmt.Errorf("duplicate terminal event for %q", event.Test)
			}
			state.terminal = event.Action
			terminalCount++
		default:
			return 0, fmt.Errorf("forbidden result action %q", event.Action)
		}
		states[identity] = state
	}

	for identity, state := range states {
		if state.run && state.terminal == "" {
			return 0, fmt.Errorf("run event has no terminal event for %q", identity)
		}
	}
	if requireTerminal && terminalCount == 0 {
		return 0, errors.New("report contains no terminal pass or skip results")
	}
	return terminalCount, nil
}

func omitAmbiguousReporterLeaves(events []resultEvent) ([]resultEvent, []string) {
	identitiesByLeaf := make(map[string]map[string]struct{})
	for _, event := range events {
		leaf := leafName(event.Test)
		if identitiesByLeaf[leaf] == nil {
			identitiesByLeaf[leaf] = make(map[string]struct{})
		}
		identity := event.Package + "\x00" + event.Test
		identitiesByLeaf[leaf][identity] = struct{}{}
	}

	ambiguous := make(map[string]struct{})
	var omittedLeaves []string
	for leaf, identities := range identitiesByLeaf {
		if len(identities) > 1 {
			ambiguous[leaf] = struct{}{}
			omittedLeaves = append(omittedLeaves, leaf)
		}
	}
	sort.Strings(omittedLeaves)
	if len(omittedLeaves) == 0 {
		return events, nil
	}

	filtered := make([]resultEvent, 0, len(events))
	for _, event := range events {
		if _, omit := ambiguous[leafName(event.Test)]; !omit {
			filtered = append(filtered, event)
		}
	}
	return filtered, omittedLeaves
}

func leafName(test string) string {
	parts := strings.Split(test, "/")
	return parts[len(parts)-1]
}

func normalizeVersion(value string) (string, error) {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "v")
	if matches := commitHeadVersionRE.FindStringSubmatch(value); matches != nil {
		value = matches[1] + "." + strings.ToLower(matches[2])
	} else {
		value = strings.TrimSuffix(value, "-head")
	}
	if len(value) == 0 || len(value) > 128 || !safeVersionRE.MatchString(value) {
		return "", fmt.Errorf("invalid Rancher version %q", value)
	}
	return value, nil
}

func validateSourceURL(value string) error {
	if err := validateSingleLine("source run URL", value, 2048); err != nil {
		return err
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return fmt.Errorf("source run URL must be an absolute HTTPS URL without user information")
	}
	return nil
}

func validateSingleLine(name, value string, maxLength int) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is empty", name)
	}
	if len(value) > maxLength {
		return fmt.Errorf("%s exceeds %d bytes", name, maxLength)
	}
	for _, character := range value {
		if character == '\r' || character == '\n' || character == 0 || character < 0x20 || character == 0x7f {
			return fmt.Errorf("%s contains control characters", name)
		}
	}
	return nil
}

func generateSchemas(reporterRoot string, leavesByPackage map[string][]string) error {
	if strings.TrimSpace(reporterRoot) == "" {
		return errors.New("-reporter-root is required for an enabled report")
	}
	rootAbs, err := filepath.Abs(reporterRoot)
	if err != nil {
		return fmt.Errorf("resolve reporter root: %w", err)
	}
	rootResolved, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return fmt.Errorf("resolve reporter root symlinks: %w", err)
	}
	info, err := os.Stat(rootResolved)
	if err != nil || !info.IsDir() {
		return errors.New("reporter root is not a directory")
	}
	// reporter-v2 derives package paths by splitting on the compile-time source
	// root basename. Its current implementation therefore requires this name.
	if filepath.Base(rootResolved) != "tests" {
		return fmt.Errorf("reporter root basename must be %q for reporter-v2 compatibility", "tests")
	}

	packages := make([]string, 0, len(leavesByPackage))
	for pkg := range leavesByPackage {
		packages = append(packages, pkg)
	}
	sort.Strings(packages)
	for _, pkg := range packages {
		relativePackage, ok := allowedPackages[pkg]
		if !ok {
			return fmt.Errorf("cannot generate schema for package %q", pkg)
		}
		packageDir := filepath.Join(rootResolved, filepath.FromSlash(relativePackage))
		packageResolved, err := filepath.EvalSymlinks(packageDir)
		if err != nil {
			return fmt.Errorf("resolve reporter package %q: %w", pkg, err)
		}
		if !isWithin(rootResolved, packageResolved) {
			return fmt.Errorf("reporter package %q escapes reporter root", pkg)
		}
		info, err := os.Stat(packageResolved)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("reporter package %q is not a directory", pkg)
		}

		schemaDir := filepath.Join(packageResolved, "schemas")
		if err := ensurePlainDirectory(schemaDir); err != nil {
			return fmt.Errorf("prepare schema directory for %q: %w", pkg, err)
		}
		cases := make([]schemaCase, 0, len(leavesByPackage[pkg]))
		for _, leaf := range leavesByPackage[pkg] {
			cases = append(cases, schemaCase{
				Title:       leaf,
				CustomField: map[string]string{"15": leaf},
			})
		}
		schema := []schemaSuite{{Projects: []string{projectID}, Suite: "Rancher Runway", Cases: cases}}
		data, err := json.MarshalIndent(schema, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal schema for %q: %w", pkg, err)
		}
		data = append(data, '\n')
		if err := atomicWrite(filepath.Join(schemaDir, "runway_schemas.yaml"), data, 0o644); err != nil {
			return fmt.Errorf("write schema for %q: %w", pkg, err)
		}
	}
	return nil
}

func ensurePlainDirectory(path string) error {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("existing schema path is not a plain directory")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Mkdir(path, 0o755)
}

func writeArtifact(outputDir string, metadataData, resultsData []byte) error {
	if info, err := os.Lstat(outputDir); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("output directory is not a plain directory")
		}
	} else if os.IsNotExist(err) {
		if err := os.MkdirAll(outputDir, 0o755); err != nil {
			return fmt.Errorf("create output directory: %w", err)
		}
	} else {
		return fmt.Errorf("inspect output directory: %w", err)
	}
	if err := atomicWrite(filepath.Join(outputDir, resultsName), resultsData, 0o600); err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(outputDir, metadataName), metadataData, 0o600); err != nil {
		return err
	}
	return nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("refusing to replace non-regular file %q", path)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".qase-report-input-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return nil
}

func readRegularNoSymlink(path string, maximum int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("input is not a plain regular file")
	}
	if info.Size() > maximum {
		return nil, fmt.Errorf("input exceeds %d bytes", maximum)
	}
	return os.ReadFile(path)
}

func writeGitHubOutputs(path string, meta metadata) error {
	if path == "" {
		return nil
	}
	values := map[string]string{
		"enabled":       strconv.FormatBool(meta.Enabled),
		"result_count":  strconv.Itoa(meta.ResultCount),
		"source_url":    meta.SourceURL,
		"test_run_name": meta.Title,
		"title":         meta.Title,
	}
	keys := []string{"enabled", "test_run_name", "title", "source_url", "result_count"}
	var output strings.Builder
	for _, key := range keys {
		value := values[key]
		if strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("unsafe newline in GitHub output %q", key)
		}
		fmt.Fprintf(&output, "%s=%s\n", key, value)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open GitHub output: %w", err)
	}
	if _, err := io.WriteString(file, output.String()); err != nil {
		file.Close()
		return fmt.Errorf("write GitHub output: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close GitHub output: %w", err)
	}
	return nil
}

func strictUnmarshalObject(data []byte, destination any) error {
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := requireJSONEOF(decoder); err != nil {
		return err
	}
	return nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func rejectDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := scanJSONValue(decoder); err != nil {
		return err
	}
	return requireJSONEOF(decoder)
}

func scanJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("JSON object key is not a string")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate JSON key %q", key)
			}
			seen[key] = struct{}{}
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim('}') {
			return errors.New("unterminated JSON object")
		}
	case '[':
		for decoder.More() {
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim(']') {
			return errors.New("unterminated JSON array")
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
	return nil
}
