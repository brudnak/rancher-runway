package test

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestRunCleanupDestroyPhasesStopsAfterDownstreamFailure(t *testing.T) {
	downstreamErr := errors.New("HA 1 cluster cluster-one: management API unavailable\nHA 2 cluster cluster-two: delete timed out")
	calls := []string{}

	result, err := runCleanupDestroyPhases(
		func() error {
			calls = append(calls, "downstream")
			return downstreamErr
		},
		func() error {
			calls = append(calls, "management")
			return nil
		},
		func() {
			calls = append(calls, "local")
		},
	)

	if !errors.Is(err, downstreamErr) {
		t.Fatalf("downstream failure must stop destroy: %v", err)
	}
	if want := []string{"downstream"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("cleanup phase calls = %#v, want %#v", calls, want)
	}
	if !strings.Contains(result.Warning, "cluster-one") || !strings.Contains(result.Warning, "cluster-two") {
		t.Fatalf("warning lost downstream failure context: %q", result.Warning)
	}
	if strings.Contains(result.Warning, "\n") {
		t.Fatalf("warning must remain a single structured log line: %q", result.Warning)
	}
	line := cleanupWarningLogLine(result.Warning)
	if !strings.Contains(line, cleanupManualWarningMarker) {
		t.Fatalf("warning log line is missing stable marker %q: %q", cleanupManualWarningMarker, line)
	}
	if got := cleanupWarningFromOutputLine("2026/09/10 15:00:00 " + line); got != result.Warning {
		t.Fatalf("parsed warning = %q, want %q", got, result.Warning)
	}
}

func TestRunCleanupDestroyPhasesNeverCallsManagementAfterDownstreamFailure(t *testing.T) {
	downstreamErr := errors.New("HA 1 cluster cluster-one: management API unavailable")
	managementErr := errors.New("terraform destroy failed")
	calls := []string{}

	result, err := runCleanupDestroyPhases(
		func() error {
			calls = append(calls, "downstream")
			return downstreamErr
		},
		func() error {
			calls = append(calls, "management")
			return managementErr
		},
		func() {
			calls = append(calls, "local")
		},
	)

	if !errors.Is(err, downstreamErr) {
		t.Fatalf("cleanup error = %v, want downstream failure", err)
	}
	if want := []string{"downstream"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("cleanup phase calls = %#v, want %#v", calls, want)
	}
	if !strings.Contains(result.Warning, "cluster-one") {
		t.Fatalf("management failure discarded downstream warning: %q", result.Warning)
	}
}

func TestRunCleanupDestroyPhasesCleanSuccessHasNoWarning(t *testing.T) {
	result, err := runCleanupDestroyPhases(func() error { return nil }, func() error { return nil }, func() {})
	if err != nil {
		t.Fatal(err)
	}
	if result.Warning != "" {
		t.Fatalf("clean cleanup warning = %q, want empty", result.Warning)
	}
}

func TestRunCleanupDestroyPhasesRetainsArtifactsAfterManagementFailure(t *testing.T) {
	want := errors.New("terraform destroy failed")
	cleaned := false
	_, err := runCleanupDestroyPhases(func() error { return nil }, func() error { return want }, func() { cleaned = true })
	if !errors.Is(err, want) || cleaned {
		t.Fatalf("management failure must retain local records: err=%v cleaned=%v", err, cleaned)
	}
}
