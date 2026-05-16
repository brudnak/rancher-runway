package buildinfo

import (
	"fmt"
	"runtime/debug"
	"strings"
)

// Commit and BuildDate can be set by release/build scripts with -ldflags.
var Commit string
var BuildDate string

type Info struct {
	Commit      string `json:"commit,omitempty"`
	CommitShort string `json:"commitShort,omitempty"`
	BuildDate   string `json:"buildDate,omitempty"`
	Modified    bool   `json:"modified,omitempty"`
	Source      string `json:"source,omitempty"`
}

func Current() Info {
	info := Info{
		Commit:    cleanValue(Commit),
		BuildDate: cleanValue(BuildDate),
		Source:    "ldflags",
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
	if info.CommitShort == "" {
		return "ha-rancher build unknown"
	}
	suffix := ""
	if info.Modified {
		suffix = " modified"
	}
	if info.BuildDate != "" {
		return fmt.Sprintf("ha-rancher build %s%s (%s)", info.CommitShort, suffix, info.BuildDate)
	}
	return fmt.Sprintf("ha-rancher build %s%s", info.CommitShort, suffix)
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
