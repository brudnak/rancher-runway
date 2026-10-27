package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLaneWorkflowValidatesLinodeTokenBeforeProvisioning(t *testing.T) {
	workflow := readScopedActionsWorkflow(t, "run-rancher-signoff-lane.yml")
	job := workflow.Jobs["run-lane"]
	protectedAt, _ := scopedStepByName(t, job, "Validate protected configuration")
	validateAt, validation := scopedStepByName(t, job, "Validate Linode token")
	setupAt, _ := scopedStepByName(t, job, "Run lane setup")
	_, downstream := scopedStepByName(t, job, "Provision downstream Linode K3s")
	if !(protectedAt < validateAt && validateAt < setupAt) {
		t.Fatalf("Linode preflight must follow protected configuration and precede setup: protected=%d validation=%d setup=%d", protectedAt, validateAt, setupAt)
	}
	wantCondition := "${{ inputs.lane != 'framework-regression' }}"
	if validation.If != wantCondition || downstream.If != wantCondition {
		t.Fatalf("Linode preflight and provisioning must both skip framework-regression: validation=%q downstream=%q", validation.If, downstream.If)
	}
	if validation.Env["LINODE_TOKEN"] != "${{ secrets.LINODE_TOKEN }}" {
		t.Fatal("Linode preflight must use the protected LINODE_TOKEN secret")
	}
}

func TestLaneWorkflowLinodeTokenPreflight(t *testing.T) {
	workflow := readActionsWorkflow(t, "run-rancher-signoff-lane.yml")
	script := workflowStepScript(t, workflow, "run-lane", "Validate Linode token")
	const token = "sensitive-test-token"
	tests := []struct {
		name      string
		token     string
		status    string
		curlExit  string
		wantError string
		wantCall  bool
	}{
		{name: "empty", wantError: "LINODE_TOKEN is empty"},
		{name: "whitespace", token: " \t\n\r ", wantError: "LINODE_TOKEN is empty"},
		{name: "valid", token: token, status: "200", wantCall: true},
		{name: "trimmed", token: " \t\n" + token + "\r\n ", status: "200", wantCall: true},
		{name: "invalid", token: token, status: "401", wantError: "invalid or expired", wantCall: true},
		{name: "forbidden", token: token, status: "403", wantError: "Grant Linodes read/write access", wantCall: true},
		{name: "rate_limited", token: token, status: "429", wantError: "HTTP 429", wantCall: true},
		{name: "unavailable", token: token, status: "503", wantError: "HTTP 503", wantCall: true},
		{name: "unexpected_status", token: token, status: "204", wantError: "HTTP 204", wantCall: true},
		{name: "transport", token: token, status: "000", curlExit: "7", wantError: "runner connectivity", wantCall: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			curlPath := filepath.Join(root, "curl")
			callsPath := filepath.Join(root, "calls")
			// The fake rejects mutations, credential leaks, unbounded retries, and
			// an endpoint that does not exercise the downstream instance-list scope.
			fakeCurl := `#!/bin/bash
set -euo pipefail
silent=0
method=GET
header=
output=
format=
connect=0
maximum=0
retries=0
retry_maximum=0
url=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --silent) silent=1; shift ;;
    --request) method="$2"; shift 2 ;;
    --header) header="$2"; shift 2 ;;
    --output) output="$2"; shift 2 ;;
    --write-out) format="$2"; shift 2 ;;
    --connect-timeout) connect="$2"; shift 2 ;;
    --max-time) maximum="$2"; shift 2 ;;
    --retry) retries="$2"; shift 2 ;;
    --retry-delay) shift 2 ;;
    --retry-max-time) retry_maximum="$2"; shift 2 ;;
    https://*) url="$1"; shift ;;
    *) echo "Unsafe or unexpected curl argument" >&2; exit 88 ;;
  esac
done
[ "$silent" -eq 1 ]
[ "$method" = GET ]
[ "$header" = 'Authorization: Bearer sensitive-test-token' ]
[ "$output" = /dev/null ]
[ "$format" = '%{http_code}' ]
[ "$connect" -gt 0 ] && [ "$connect" -le 10 ]
[ "$maximum" -gt 0 ] && [ "$maximum" -le 15 ]
[ "$retries" -gt 0 ] && [ "$retries" -le 2 ]
[ "$retry_maximum" -gt 0 ] && [ "$retry_maximum" -le 45 ]
[ "$url" = 'https://api.linode.com/v4/linode/instances?page_size=25' ]
printf 'called\n' >> "$FAKE_CURL_CALLS"
printf 'private-instance-response' > "$output"
printf '%s' "$FAKE_CURL_STATUS"
exit "${FAKE_CURL_EXIT:-0}"
`
			if err := os.WriteFile(curlPath, []byte(fakeCurl), 0o700); err != nil {
				t.Fatal(err)
			}
			command := exec.Command("bash", "-c", script)
			command.Env = []string{
				"PATH=" + root + string(os.PathListSeparator) + os.Getenv("PATH"),
				"LINODE_TOKEN=" + test.token,
				"FAKE_CURL_CALLS=" + callsPath,
				"FAKE_CURL_STATUS=" + test.status,
				"FAKE_CURL_EXIT=" + test.curlExit,
			}
			output, err := command.CombinedOutput()
			if (err != nil) != (test.wantError != "") {
				t.Fatalf("preflight error = %v, want message %q; output: %s", err, test.wantError, output)
			}
			if test.wantError != "" && !strings.Contains(string(output), test.wantError) {
				t.Fatalf("expected actionable error %q, got %s", test.wantError, output)
			}
			if test.wantError == "" && !strings.Contains(string(output), "instance-read access validated") {
				t.Fatalf("expected successful authentication message, got %s", output)
			}
			if strings.Contains(string(output), token) || strings.Contains(string(output), "private-instance-response") {
				t.Fatalf("preflight exposed credentials or an API response: %s", output)
			}
			calls, readErr := os.ReadFile(callsPath)
			if test.wantCall {
				if readErr != nil || string(calls) != "called\n" {
					t.Fatalf("expected one read-only curl invocation, got %q (%v)", calls, readErr)
				}
			} else if !os.IsNotExist(readErr) {
				t.Fatalf("empty token reached the API: %q (%v)", calls, readErr)
			}
		})
	}
}
