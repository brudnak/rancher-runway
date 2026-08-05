package buildinfo

import (
	"strings"
	"testing"
)

func TestCurrentUsesExplicitBuildValues(t *testing.T) {
	oldVersion := Version
	oldBuildNumber := BuildNumber
	oldCommit := Commit
	oldBuildDate := BuildDate
	t.Cleanup(func() {
		Version = oldVersion
		BuildNumber = oldBuildNumber
		Commit = oldCommit
		BuildDate = oldBuildDate
	})

	Version = "v1.4.0-rc.2+release"
	BuildNumber = "42"
	Commit = "1234567890abcdef1234567890abcdef12345678"
	BuildDate = "2026-05-16T21:00:00Z"

	info := Current()
	if info.Version != "1.4.0-rc.2+release" {
		t.Fatalf("expected normalized version, got %q", info.Version)
	}
	if info.BuildNumber != BuildNumber {
		t.Fatalf("expected build number %q, got %q", BuildNumber, info.BuildNumber)
	}
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

func TestCurrentUsesSafeDevelopmentDefaults(t *testing.T) {
	oldVersion := Version
	oldBuildNumber := BuildNumber
	t.Cleanup(func() {
		Version = oldVersion
		BuildNumber = oldBuildNumber
	})

	Version = "not a semantic version"
	BuildNumber = "build-12"

	info := Current()
	if info.Version != DevelopmentVersion {
		t.Fatalf("expected development version %q, got %q", DevelopmentVersion, info.Version)
	}
	if info.BuildNumber != DevelopmentBuildNumber {
		t.Fatalf("expected development build number %q, got %q", DevelopmentBuildNumber, info.BuildNumber)
	}
}

func TestNormalizeVersion(t *testing.T) {
	tests := map[string]string{
		"release":             "1.2.3",
		"leading v":           "v1.2.3",
		"prerelease metadata": "2.0.0-beta.1+sha.abc123",
	}

	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			want := strings.TrimPrefix(input, "v")
			if got := normalizeVersion(input); got != want {
				t.Fatalf("normalizeVersion(%q) = %q, want %q", input, got, want)
			}
		})
	}
}

func TestDisplayLineIncludesVersionAndNumericBuild(t *testing.T) {
	oldVersion := Version
	oldBuildNumber := BuildNumber
	oldCommit := Commit
	oldBuildDate := BuildDate
	t.Cleanup(func() {
		Version = oldVersion
		BuildNumber = oldBuildNumber
		Commit = oldCommit
		BuildDate = oldBuildDate
	})

	Version = "1.4.0"
	BuildNumber = "42"
	Commit = ""
	BuildDate = ""

	if got, want := DisplayLine(), "ha-rancher v1.4.0 build 42"; got != want {
		t.Fatalf("DisplayLine() = %q, want %q", got, want)
	}
}
