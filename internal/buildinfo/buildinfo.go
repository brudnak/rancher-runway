package buildinfo

import (
	"fmt"
	"regexp"
	"runtime/debug"
	"strings"
)

const (
	DevelopmentVersion     = "0.0.0-dev"
	DevelopmentBuildNumber = "0"
)

var (
	semanticVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
	buildNumberPattern     = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)
)

// Version, BuildNumber, Commit, and BuildDate can be set by release/build
// scripts with -ldflags. The defaults identify an unversioned local build and
// remain valid when no release metadata is supplied.
var Version = DevelopmentVersion
var BuildNumber = DevelopmentBuildNumber
var Commit string
var BuildDate string

type Info struct {
	Version     string `json:"version"`
	BuildNumber string `json:"buildNumber"`
	Commit      string `json:"commit,omitempty"`
	CommitShort string `json:"commitShort,omitempty"`
	BuildDate   string `json:"buildDate,omitempty"`
	Modified    bool   `json:"modified,omitempty"`
	Source      string `json:"source,omitempty"`
}

func Current() Info {
	info := Info{
		Version:     normalizeVersion(Version),
		BuildNumber: normalizeBuildNumber(BuildNumber),
		Commit:      cleanValue(Commit),
		BuildDate:   cleanValue(BuildDate),
		Source:      "ldflags",
	}

	needsVCSFallback := info.Commit == "" || strings.EqualFold(info.Commit, "auto") || strings.EqualFold(info.Commit, "dev")
	if needsVCSFallback {
		info.Commit = ""
		vcs := vcsBuildInfo()
		if vcs.Commit != "" {
			info.Commit = vcs.Commit
			info.Source = "go-vcs"
		}
		if info.BuildDate == "" {
			info.BuildDate = vcs.BuildDate
		}
		info.Modified = vcs.Modified
	}

	info.CommitShort = shortCommit(info.Commit)
	if info.Commit == "" {
		info.Source = ""
	}
	return info
}

func DisplayLine() string {
	info := Current()
	build := fmt.Sprintf("ha-rancher v%s build %s", info.Version, info.BuildNumber)
	if info.CommitShort == "" {
		return build
	}
	suffix := ""
	if info.Modified {
		suffix = " modified"
	}
	if info.BuildDate != "" {
		return fmt.Sprintf("%s, commit %s%s (%s)", build, info.CommitShort, suffix, info.BuildDate)
	}
	return fmt.Sprintf("%s, commit %s%s", build, info.CommitShort, suffix)
}

func normalizeVersion(value string) string {
	value = cleanValue(value)
	if strings.HasPrefix(value, "v") || strings.HasPrefix(value, "V") {
		value = value[1:]
	}
	if !semanticVersionPattern.MatchString(value) {
		return DevelopmentVersion
	}
	return value
}

func normalizeBuildNumber(value string) string {
	value = cleanValue(value)
	if !buildNumberPattern.MatchString(value) {
		return DevelopmentBuildNumber
	}
	return value
}

func shortCommit(commit string) string {
	commit = cleanValue(commit)
	if len(commit) <= 12 {
		return commit
	}
	return commit[:12]
}

func cleanValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "<no value>" {
		return ""
	}
	return value
}

func vcsBuildInfo() Info {
	build, ok := debug.ReadBuildInfo()
	if !ok {
		return Info{}
	}

	var info Info
	for _, setting := range build.Settings {
		switch setting.Key {
		case "vcs.revision":
			info.Commit = cleanValue(setting.Value)
		case "vcs.time":
			info.BuildDate = cleanValue(setting.Value)
		case "vcs.modified":
			info.Modified = strings.EqualFold(setting.Value, "true")
		}
	}
	return info
}
