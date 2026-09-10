package test

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestRunCleanupDestroyPhasesContinuesAfterDownstreamFailure(t *testing.T) {
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

	if err != nil {
		t.Fatalf("cleanup phases returned an error after management destroy succeeded: %v", err)
	}
	if want := []string{"downstream", "management", "local"}; !reflect.DeepEqual(calls, want) {
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

func TestRunCleanupDestroyPhasesReturnsManagementFailureWithWarning(t *testing.T) {
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

	if !errors.Is(err, managementErr) {
		t.Fatalf("cleanup error = %v, want management failure", err)
	}
	if want := []string{"downstream", "management"}; !reflect.DeepEqual(calls, want) {
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
