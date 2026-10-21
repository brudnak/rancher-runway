package test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWebhookOverrideWaitsForDiscovery(t *testing.T) {
	for _, tc := range []struct {
		name         string
		mode         string
		wantAttempts int
	}{
		{name: "deployment appears after cluster connects", mode: "delayed", wantAttempts: 3},
		{name: "temporary API failure recovers", mode: "transient", wantAttempts: 2},
		{name: "stderr warning does not corrupt JSON", mode: "warning", wantAttempts: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logPath := installWebhookDiscoveryKubectlFixture(t, tc.mode)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			target, err := overrideWebhookDeploymentWithDiscovery(ctx, 1, "downstream", "test-cluster", "/tmp/downstream.yaml", "", "registry.example/rancher-webhook:candidate", time.Millisecond)
			if err != nil {
				t.Fatalf("override failed after discovery: %v", err)
			}
			if target.Namespace != "cattle-system" || target.DeploymentName != "rancher-webhook" || target.ContainerName != "rancher-webhook" {
				t.Fatalf("unexpected discovered target: %#v", target)
			}
			calls := readWebhookDiscoveryCalls(t, logPath)
			if len(calls) != tc.wantAttempts+2 {
				t.Fatalf("expected %d discoveries followed by image update and rollout, got %q", tc.wantAttempts, calls)
			}
			for _, call := range calls[:tc.wantAttempts] {
				if call != "get deployments -A -o json" {
					t.Fatalf("mutation or unexpected command before discovery completed: %q", calls)
				}
			}
			if got := calls[tc.wantAttempts]; got != "set image deployment/rancher-webhook rancher-webhook=registry.example/rancher-webhook:candidate -n cattle-system" {
				t.Fatalf("image override did not use discovered deployment/container: %q", got)
			}
			if got := calls[tc.wantAttempts+1]; got != "rollout status deployment/rancher-webhook -n cattle-system --timeout=10m" {
				t.Fatalf("image override was not followed by rollout verification: %q", got)
			}
		})
	}
}

func TestWebhookOverrideDiscoveryTimeoutCollectsDiagnosticsWithoutMutation(t *testing.T) {
	logPath := installWebhookDiscoveryKubectlFixture(t, "missing")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := overrideWebhookDeploymentWithDiscovery(ctx, 3, "downstream", "slow-cluster", "/tmp/downstream.yaml", "", "registry.example/rancher-webhook:candidate", 2*time.Second)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected discovery deadline error, got %v", err)
	}
	for _, detail := range []string{"HA 3 downstream cluster slow-cluster", "last discovery error", "could not find a deployment/container/image matching rancher-webhook"} {
		if !strings.Contains(err.Error(), detail) {
			t.Fatalf("timeout omitted %q: %v", detail, err)
		}
	}
	calls := readWebhookDiscoveryCalls(t, logPath)
	if len(calls) < 4 || calls[0] != "get deployments -A -o json" {
		t.Fatalf("expected discovery before diagnostics, got %q", calls)
	}
	for _, call := range calls {
		if strings.HasPrefix(call, "set ") || strings.HasPrefix(call, "rollout ") {
			t.Fatalf("missing deployment must not be mutated: %q", calls)
		}
	}
	wantDiagnostics := []string{
		"get deployments,pods,jobs -A -o wide",
		"get apps.catalog.cattle.io -A",
		"get events -A --sort-by=.lastTimestamp",
	}
	for i, want := range wantDiagnostics {
		if got := calls[len(calls)-len(wantDiagnostics)+i]; got != want {
			t.Fatalf("timeout diagnostics = %q, want final commands %q", calls, wantDiagnostics)
		}
	}
}

func TestWebhookDiscoveryKubectlHonorsContextDeadline(t *testing.T) {
	installWebhookDiscoveryKubectlFixture(t, "hang")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := runWebhookDiscoveryKubectlOutput(ctx, "/tmp/downstream.yaml", "get", "deployments", "-A", "-o", "json")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected blocked kubectl to return the context deadline, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("blocked kubectl outlived its discovery deadline by %s", elapsed)
	}
}

func installWebhookDiscoveryKubectlFixture(t *testing.T, mode string) string {
	t.Helper()
	root := t.TempDir()
	logPath := filepath.Join(root, "calls.log")
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WEBHOOK_FIXTURE_MODE", mode)
	t.Setenv("WEBHOOK_FIXTURE_LOG", logPath)
	t.Setenv("WEBHOOK_FIXTURE_COUNT", filepath.Join(root, "count"))
	script := `#!/bin/sh
set -eu
[ "$1" = --kubeconfig ] && [ "$2" = /tmp/downstream.yaml ]
shift 2
request_timeout=
if [ "${1:-}" = --request-timeout=20s ]; then
  request_timeout=bounded
  shift
fi
printf '%s\n' "$*" >> "$WEBHOOK_FIXTURE_LOG"
case "$*" in
  "get deployments -A -o json")
    [ "$request_timeout" = bounded ]
    count=0
    if [ -f "$WEBHOOK_FIXTURE_COUNT" ]; then count=$(cat "$WEBHOOK_FIXTURE_COUNT"); fi
    count=$((count + 1))
    printf '%s\n' "$count" > "$WEBHOOK_FIXTURE_COUNT"
    case "$WEBHOOK_FIXTURE_MODE" in
      hang) exec sleep 30 ;;
      missing) printf '%s\n' '{"items":[]}'; exit 0 ;;
      delayed)
        if [ "$count" -lt 3 ]; then printf '%s\n' '{"items":[]}'; exit 0; fi
        ;;
      transient)
        if [ "$count" -eq 1 ]; then echo 'temporary API service unavailable' >&2; exit 1; fi
        ;;
      warning) echo 'Warning: use TokenRequest API tokens' >&2 ;;
    esac
    printf '%s\n' '{"items":[{"metadata":{"name":"rancher-webhook","namespace":"cattle-system"},"spec":{"template":{"spec":{"containers":[{"name":"rancher-webhook","image":"registry.example/rancher-webhook:previous"}]}}}}]}'
    ;;
  "set image "*|"rollout status "*)
    [ "$WEBHOOK_FIXTURE_MODE" != missing ]
    ;;
  "get deployments,pods,jobs -A -o wide"|"get apps.catalog.cattle.io -A"|"get events -A --sort-by=.lastTimestamp")
    [ "$request_timeout" = bounded ]
    echo 'fixture diagnostics'
    ;;
  *) echo "unexpected kubectl arguments: $*" >&2; exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(root, "kubectl"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return logPath
}

func readWebhookDiscoveryCalls(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}
