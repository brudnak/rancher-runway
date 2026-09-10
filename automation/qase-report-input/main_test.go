package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const (
	chartsPackage = "github.com/rancher/tests/validation/charts"
	testTime      = "2026-09-02T21:00:00.123456789Z"
)

func TestNormalizeVersion(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "v2.16.2-abcdef0-head", want: "2.16.2.abcdef0"},
		{input: "2.16-ABCDEF0-head", want: "2.16.abcdef0"},
		{input: "v2.16.2-head", want: "2.16.2"},
		{input: "v2.16-head", want: "2.16"},
		{input: "v2.16.2-rcs-0844.1", want: "2.16.2-rcs-0844.1"},
		{input: "  v2.16.2-alpha1  ", want: "2.16.2-alpha1"},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			got, err := normalizeVersion(test.input)
			if err != nil {
				t.Fatalf("normalizeVersion: %v", err)
			}
			if got != test.want {
				t.Fatalf("normalizeVersion(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
	for _, input := range []string{"", "v", "head", "release-head", "2.16.2\nattack"} {
		t.Run("reject_"+input, func(t *testing.T) {
			if _, err := normalizeVersion(input); err == nil {
				t.Fatalf("expected %q to be rejected", input)
			}
		})
	}
}

func TestPrepareEnabledSanitizesAndVerifyGeneratesSchemas(t *testing.T) {
	workspace := t.TempDir()
	goJSON := filepath.Join(workspace, "test-results", "charts.json")
	mustMkdirAll(t, filepath.Dir(goJSON))
	mustWrite(t, goJSON, strings.Join([]string{
		`{"Time":"` + testTime + `","Action":"start","Package":"` + chartsPackage + `"}`,
		`{"Time":"` + testTime + `","Action":"run","Package":"` + chartsPackage + `","Test":"TestWebhookTestSuite"}`,
		`{"Time":"` + testTime + `","Action":"output","Package":"` + chartsPackage + `","Test":"TestWebhookTestSuite/TestChart","Output":"QASE_AUTOMATION_TOKEN=must-never-survive\\n"}`,
		`{"Time":"` + testTime + `","Action":"run","Package":"` + chartsPackage + `","Test":"TestWebhookTestSuite/TestChart"}`,
		`{"Time":"` + testTime + `","Action":"pass","Package":"` + chartsPackage + `","Test":"TestWebhookTestSuite/TestChart","Elapsed":0.125}`,
		`{"Time":"` + testTime + `","Action":"pass","Package":"` + chartsPackage + `","Test":"TestWebhookTestSuite","Elapsed":0.2}`,
		`{"Time":"` + testTime + `","Action":"pass","Package":"` + chartsPackage + `","Elapsed":0.3}`,
	}, "\n")+"\n")
	manifestPath := writeManifest(t, workspace, []manifestResult{{
		Suite:      "TestWebhookTestSuite",
		Package:    "./validation/charts",
		TestRun:    "TestWebhookTestSuite",
		GoJSON:     "test-results/charts.json",
		Conclusion: "success",
	}})
	artifactDir := filepath.Join(t.TempDir(), "artifact")

	err := prepare(options{
		lane:            "framework-regression",
		rancherVersion:  "v2.16.2-ABCDEF0-head",
		sourceRunURL:    "https://github.com/rancher/runway/actions/runs/123",
		rancherTestsRef: "main",
		resultsManifest: manifestPath,
		workspace:       workspace,
		outputDir:       artifactDir,
	}, true)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}

	metadataData := mustRead(t, filepath.Join(artifactDir, metadataName))
	var gotMetadata metadata
	if err := json.Unmarshal(metadataData, &gotMetadata); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	wantMetadata := metadata{
		Enabled:         true,
		Title:           "[frameworks][2.16.2.abcdef0][Frameworks Regression]",
		Lane:            "framework-regression",
		Version:         "v2.16.2-ABCDEF0-head",
		SourceURL:       "https://github.com/rancher/runway/actions/runs/123",
		RancherTestsRef: "main",
		Project:         "RM",
		ResultCount:     1,
	}
	if gotMetadata != wantMetadata {
		t.Fatalf("metadata = %#v, want %#v", gotMetadata, wantMetadata)
	}

	resultsData := mustRead(t, filepath.Join(artifactDir, resultsName))
	if bytes.Contains(resultsData, []byte("Output")) || bytes.Contains(resultsData, []byte("must-never-survive")) {
		t.Fatalf("results leaked dropped output: %s", resultsData)
	}
	lines := nonemptyLines(resultsData)
	if len(lines) != 2 {
		t.Fatalf("got %d sanitized events, want 2: %s", len(lines), resultsData)
	}
	wantKeys := []map[string]bool{
		{"Time": true, "Action": true, "Package": true, "Test": true},
		{"Time": true, "Action": true, "Package": true, "Test": true, "Elapsed": true},
	}
	for i, line := range lines {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(line, &fields); err != nil {
			t.Fatalf("decode result line %d: %v", i, err)
		}
		gotKeys := make(map[string]bool, len(fields))
		for key := range fields {
			gotKeys[key] = true
		}
		if !reflect.DeepEqual(gotKeys, wantKeys[i]) {
			t.Fatalf("line %d keys = %v, want %v", i, gotKeys, wantKeys[i])
		}
	}

	reporterRoot := filepath.Join(t.TempDir(), "tests")
	mustMkdirAll(t, filepath.Join(reporterRoot, "validation", "charts"))
	githubOutput := filepath.Join(t.TempDir(), "github-output")
	if err := verify(options{inputDir: artifactDir, reporterRoot: reporterRoot, githubOutput: githubOutput}); err != nil {
		t.Fatalf("verify: %v", err)
	}
	outputs := string(mustRead(t, githubOutput))
	for _, expected := range []string{
		"enabled=true\n",
		"test_run_name=[frameworks][2.16.2.abcdef0][Frameworks Regression]\n",
		"source_url=https://github.com/rancher/runway/actions/runs/123\n",
		"result_count=1\n",
	} {
		if !strings.Contains(outputs, expected) {
			t.Errorf("GitHub outputs do not contain %q:\n%s", expected, outputs)
		}
	}

	schemaPath := filepath.Join(reporterRoot, "validation", "charts", "schemas", "runway_schemas.yaml")
	var schema []schemaSuite
	if err := json.Unmarshal(mustRead(t, schemaPath), &schema); err != nil {
		t.Fatalf("schema is not JSON-compatible YAML: %v", err)
	}
	if len(schema) != 1 || len(schema[0].Cases) != 1 {
		t.Fatalf("unexpected schema: %#v", schema)
	}
	if schema[0].Cases[0].Title != "TestChart" || schema[0].Cases[0].CustomField["15"] != "TestChart" {
		t.Fatalf("schema case is not keyed by reporter leaf/custom field 15: %#v", schema[0].Cases[0])
	}
}

func TestRunAcceptsWorkflowEnabledFlagForm(t *testing.T) {
	workspace := t.TempDir()
	mustWrite(t, filepath.Join(workspace, "result.json"), validEventStream(chartsPackage, "TestSuite/TestCase"))
	manifestPath := writeManifest(t, workspace, []manifestResult{{GoJSON: "result.json", Conclusion: "success"}})
	outputDir := filepath.Join(t.TempDir(), "output")
	err := run([]string{
		"-mode", "prepare",
		"-enabled", "true",
		"-lane", "framework-regression",
		"-rancher-version", "v2.16.2-head",
		"-source-run-url", "https://github.com/rancher/runway/actions/runs/1",
		"-rancher-tests-ref", "main",
		"-results-manifest", manifestPath,
		"-workspace", workspace,
		"-output-dir", outputDir,
	})
	if err != nil {
		t.Fatalf("workflow flag contract failed: %v", err)
	}
}

func TestPrepareDisabledNeedsNoManifestAndVerifyNeedsNoReporter(t *testing.T) {
	artifactDir := filepath.Join(t.TempDir(), "artifact")
	if err := prepare(options{outputDir: artifactDir}, false); err != nil {
		t.Fatalf("prepare disabled: %v", err)
	}
	if data := mustRead(t, filepath.Join(artifactDir, resultsName)); len(data) != 0 {
		t.Fatalf("disabled results should be empty, got %q", data)
	}
	githubOutput := filepath.Join(t.TempDir(), "output")
	if err := verify(options{inputDir: artifactDir, reporterRoot: filepath.Join(t.TempDir(), "does-not-exist"), githubOutput: githubOutput}); err != nil {
		t.Fatalf("verify disabled: %v", err)
	}
	outputs := string(mustRead(t, githubOutput))
	if !strings.Contains(outputs, "enabled=false\n") || !strings.Contains(outputs, "result_count=0\n") {
		t.Fatalf("unexpected disabled outputs: %s", outputs)
	}
}

func TestPrepareRequiresEveryManifestConclusionSuccess(t *testing.T) {
	workspace := t.TempDir()
	mustWrite(t, filepath.Join(workspace, "result.json"), validEventStream(chartsPackage, "TestSuite/TestCase"))
	manifestPath := writeManifest(t, workspace, []manifestResult{{GoJSON: "result.json", Conclusion: "failure"}})
	err := prepare(validPrepareOptions(t, workspace, manifestPath), true)
	assertErrorContains(t, err, "all results must be success")
}

func TestPrepareRejectsLexicalAndSymlinkPathEscapes(t *testing.T) {
	outsideDir := t.TempDir()
	mustWrite(t, filepath.Join(outsideDir, "outside.json"), validEventStream(chartsPackage, "TestSuite/TestCase"))

	t.Run("lexical", func(t *testing.T) {
		workspace := t.TempDir()
		manifestPath := writeManifest(t, workspace, []manifestResult{{GoJSON: filepath.Join(outsideDir, "outside.json"), Conclusion: "success"}})
		err := prepare(validPrepareOptions(t, workspace, manifestPath), true)
		assertErrorContains(t, err, "escapes workspace")
	})

	t.Run("symlink", func(t *testing.T) {
		workspace := t.TempDir()
		if err := os.Symlink(filepath.Join(outsideDir, "outside.json"), filepath.Join(workspace, "linked.json")); err != nil {
			t.Fatalf("create symlink: %v", err)
		}
		manifestPath := writeManifest(t, workspace, []manifestResult{{GoJSON: "linked.json", Conclusion: "success"}})
		err := prepare(validPrepareOptions(t, workspace, manifestPath), true)
		assertErrorContains(t, err, "resolved path escapes workspace")
	})
}

func TestPrepareRejectsUnsafeResultStreams(t *testing.T) {
	tests := []struct {
		name   string
		stream string
		want   string
	}{
		{
			name: "failure action",
			stream: eventLine("run", chartsPackage, "TestSuite/TestCase", "") +
				eventLine("fail", chartsPackage, "TestSuite/TestCase", "0.1"),
			want: "failed test event",
		},
		{
			name:   "unknown action",
			stream: eventLine("mystery", chartsPackage, "TestSuite/TestCase", ""),
			want:   "unknown test action",
		},
		{
			name: "unapproved package",
			stream: eventLine("run", "github.com/example/evil", "TestSuite/TestCase", "") +
				eventLine("pass", "github.com/example/evil", "TestSuite/TestCase", "0.1"),
			want: "not an allowed Runway test package",
		},
		{
			name:   "terminal without run",
			stream: eventLine("pass", chartsPackage, "TestSuite/TestCase", "0.1"),
			want:   "no preceding run",
		},
		{
			name:   "run without terminal",
			stream: eventLine("run", chartsPackage, "TestSuite/TestCase", ""),
			want:   "no terminal event",
		},
		{
			name: "run event elapsed",
			stream: eventLine("run", chartsPackage, "TestSuite/TestCase", "0") +
				eventLine("pass", chartsPackage, "TestSuite/TestCase", "0.1"),
			want: "run event contains Elapsed",
		},
		{
			name: "leaf collision",
			stream: validEventStream(chartsPackage, "TestSuiteOne/SameLeaf") +
				validEventStream(chartsPackage, "TestSuiteTwo/SameLeaf"),
			want: "leaf name collision",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			mustWrite(t, filepath.Join(workspace, "result.json"), test.stream)
			manifestPath := writeManifest(t, workspace, []manifestResult{{GoJSON: "result.json", Conclusion: "success"}})
			err := prepare(validPrepareOptions(t, workspace, manifestPath), true)
			assertErrorContains(t, err, test.want)
		})
	}
}

func TestVerifyRejectsTampering(t *testing.T) {
	tests := []struct {
		name           string
		mutateMetadata func(string) string
		mutateResults  func(string) string
		want           string
	}{
		{
			name: "output field",
			mutateResults: func(value string) string {
				return strings.Replace(value, `"Action":"run"`, `"Action":"run","Output":"secret"`, 1)
			},
			want: "unknown field",
		},
		{
			name: "fail action",
			mutateResults: func(value string) string {
				return strings.Replace(value, `"Action":"pass"`, `"Action":"fail"`, 1)
			},
			want: "forbidden action",
		},
		{
			name: "unknown metadata",
			mutateMetadata: func(value string) string {
				return strings.Replace(value, `"enabled": true`, `"enabled": true, "token": "secret"`, 1)
			},
			want: "unknown field",
		},
		{
			name: "duplicate metadata key",
			mutateMetadata: func(value string) string {
				return strings.Replace(value, `"enabled": true`, `"enabled": false, "enabled": true`, 1)
			},
			want: "duplicate JSON key",
		},
		{
			name: "title",
			mutateMetadata: func(value string) string {
				return strings.Replace(value, "Frameworks Regression", "Injected", 1)
			},
			want: "does not match",
		},
		{
			name: "count",
			mutateMetadata: func(value string) string {
				return strings.Replace(value, `"result_count": 1`, `"result_count": 2`, 1)
			},
			want: "validated results contain 1",
		},
		{
			name: "elapsed string",
			mutateResults: func(value string) string {
				return strings.Replace(value, `"Elapsed":0.1`, `"Elapsed":"0.1"`, 1)
			},
			want: "Elapsed must be a JSON number",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			artifactDir := makeValidArtifact(t)
			metadataPath := filepath.Join(artifactDir, metadataName)
			resultsPath := filepath.Join(artifactDir, resultsName)
			if test.mutateMetadata != nil {
				mustWrite(t, metadataPath, test.mutateMetadata(string(mustRead(t, metadataPath))))
			}
			if test.mutateResults != nil {
				mustWrite(t, resultsPath, test.mutateResults(string(mustRead(t, resultsPath))))
			}
			reporterRoot := filepath.Join(t.TempDir(), "tests")
			mustMkdirAll(t, filepath.Join(reporterRoot, "validation", "charts"))
			err := verify(options{inputDir: artifactDir, reporterRoot: reporterRoot})
			assertErrorContains(t, err, test.want)
		})
	}
}

func TestGenerateSchemasForEveryAllowedPackage(t *testing.T) {
	reporterRoot := filepath.Join(t.TempDir(), "tests")
	leaves := make(map[string][]string)
	for pkg, relative := range allowedPackages {
		mustMkdirAll(t, filepath.Join(reporterRoot, filepath.FromSlash(relative)))
		leaves[pkg] = []string{"CaseFor" + filepath.Base(relative)}
	}
	if err := generateSchemas(reporterRoot, leaves); err != nil {
		t.Fatalf("generateSchemas: %v", err)
	}
	for pkg, relative := range allowedPackages {
		path := filepath.Join(reporterRoot, filepath.FromSlash(relative), "schemas", "runway_schemas.yaml")
		data := mustRead(t, path)
		if !json.Valid(data) {
			t.Errorf("schema for %s is not JSON-compatible YAML: %s", pkg, data)
		}
	}
}

func TestVerifyRejectsWrongReporterBasenameAndSchemaSymlink(t *testing.T) {
	t.Run("basename", func(t *testing.T) {
		artifactDir := makeValidArtifact(t)
		reporterRoot := filepath.Join(t.TempDir(), "rancher-tests")
		mustMkdirAll(t, filepath.Join(reporterRoot, "validation", "charts"))
		err := verify(options{inputDir: artifactDir, reporterRoot: reporterRoot})
		assertErrorContains(t, err, `basename must be "tests"`)
	})

	t.Run("schema symlink", func(t *testing.T) {
		artifactDir := makeValidArtifact(t)
		reporterRoot := filepath.Join(t.TempDir(), "tests")
		packageDir := filepath.Join(reporterRoot, "validation", "charts")
		mustMkdirAll(t, packageDir)
		if err := os.Symlink(t.TempDir(), filepath.Join(packageDir, "schemas")); err != nil {
			t.Fatalf("create schema symlink: %v", err)
		}
		err := verify(options{inputDir: artifactDir, reporterRoot: reporterRoot})
		assertErrorContains(t, err, "not a plain directory")
	})
}

func TestVerifyRejectsSymlinkedArtifactFile(t *testing.T) {
	artifactDir := makeValidArtifact(t)
	metadataPath := filepath.Join(artifactDir, metadataName)
	metadataCopy := filepath.Join(t.TempDir(), metadataName)
	mustWrite(t, metadataCopy, string(mustRead(t, metadataPath)))
	if err := os.Remove(metadataPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(metadataCopy, metadataPath); err != nil {
		t.Fatal(err)
	}
	err := verify(options{inputDir: artifactDir})
	assertErrorContains(t, err, "not a plain regular file")
}

func validPrepareOptions(t *testing.T, workspace, manifestPath string) options {
	t.Helper()
	return options{
		lane:            "framework-regression",
		rancherVersion:  "v2.16.2-abcdef0-head",
		sourceRunURL:    "https://github.com/rancher/runway/actions/runs/1",
		rancherTestsRef: "main",
		resultsManifest: manifestPath,
		workspace:       workspace,
		outputDir:       filepath.Join(t.TempDir(), "artifact"),
	}
}

func makeValidArtifact(t *testing.T) string {
	t.Helper()
	workspace := t.TempDir()
	mustWrite(t, filepath.Join(workspace, "result.json"), validEventStream(chartsPackage, "TestSuite/TestCase"))
	manifestPath := writeManifest(t, workspace, []manifestResult{{GoJSON: "result.json", Conclusion: "success"}})
	opts := validPrepareOptions(t, workspace, manifestPath)
	if err := prepare(opts, true); err != nil {
		t.Fatalf("prepare fixture: %v", err)
	}
	return opts.outputDir
}

func writeManifest(t *testing.T, workspace string, results []manifestResult) string {
	t.Helper()
	value := manifest{Ref: "main", Lane: "framework-regression", Results: results}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(workspace, "manifest.json")
	mustWrite(t, path, string(data))
	return path
}

func validEventStream(pkg, test string) string {
	return eventLine("run", pkg, test, "") + eventLine("pass", pkg, test, "0.1")
}

func eventLine(action, pkg, test, elapsed string) string {
	fields := map[string]any{
		"Time":    testTime,
		"Action":  action,
		"Package": pkg,
		"Test":    test,
	}
	if elapsed != "" {
		value, _ := strconvParseJSONNumber(elapsed)
		fields["Elapsed"] = value
	}
	data, _ := json.Marshal(fields)
	return string(data) + "\n"
}

// strconvParseJSONNumber keeps event fixtures numeric after json.Marshal.
func strconvParseJSONNumber(value string) (json.Number, error) {
	return json.Number(value), nil
}

func nonemptyLines(data []byte) [][]byte {
	var lines [][]byte
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		if len(bytes.TrimSpace(scanner.Bytes())) != 0 {
			lines = append(lines, bytes.Clone(scanner.Bytes()))
		}
	}
	return lines
}

func mustWrite(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func assertErrorContains(t *testing.T, err error, expected string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %q", expected)
	}
	if !strings.Contains(err.Error(), expected) {
		t.Fatalf("error %q does not contain %q", err, expected)
	}
}
