package buildinfo

import "testing"

func TestCurrentUsesExplicitBuildValues(t *testing.T) {
	oldCommit := Commit
	oldBuildDate := BuildDate
	t.Cleanup(func() {
		Commit = oldCommit
		BuildDate = oldBuildDate
	})

	Commit = "1234567890abcdef1234567890abcdef12345678"
	BuildDate = "2026-05-16T21:00:00Z"

	info := Current()
	if info.Commit != Commit {
		t.Fatalf("expected explicit commit %q, got %q", Commit, info.Commit)
	}
	if info.CommitShort != "1234567890ab" {
		t.Fatalf("expected short commit, got %q", info.CommitShort)
	}
	if info.BuildDate != BuildDate {
		t.Fatalf("expected explicit build date %q, got %q", BuildDate, info.BuildDate)
	}
	if info.Source != "ldflags" {
		t.Fatalf("expected ldflags source, got %q", info.Source)
	}
}

func TestCurrentDoesNotDisplayAutoSentinelAsCommit(t *testing.T) {
	oldCommit := Commit
	oldBuildDate := BuildDate
	t.Cleanup(func() {
		Commit = oldCommit
		BuildDate = oldBuildDate
	})

	Commit = "auto"
	BuildDate = "2026-05-16T21:00:00Z"

	info := Current()
	if info.Commit == "auto" || info.CommitShort == "auto" {
		t.Fatalf("auto sentinel leaked into build info: %#v", info)
	}
}
