package prbuild

import (
	"context"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/brudnak/ha-rancher-rke2/internal/registrycatalog"
)

type readinessScopeKey struct{}
type readinessSearchScope struct {
	Line, Version string
	Expanded      bool
}

var readinessPatchPattern = regexp.MustCompile(`(?i)^v?([0-9]+\.[0-9]+\.[0-9]+)(?:$|[-+])`)

func readinessPatchVersion(title string) string {
	if match := readinessPatchPattern.FindStringSubmatch(strings.TrimSpace(title)); len(match) > 1 {
		return match[1]
	}
	return ""
}
func readinessSelectors(scope readinessSearchScope) []string {
	selectors := []string{"head"}
	if scope.Line != "" {
		selectors = append(selectors, "v"+scope.Line+"-head")
		version := scope.Version
		if version == "" {
			version = scope.Line + ".0"
		}
		selectors = append(selectors, "v"+version+"-head")
	}
	return selectors
}
func (s *Service) readinessTargetBuilds(ctx context.Context, scope readinessSearchScope) ([]issueReadinessBuild, []string) {
	Notify(ctx, "Checking target release "+scope.Line+" and default head; older release lines are skipped.")
	builds := []issueReadinessBuild{}
	for _, registry := range registrycatalog.Preferred {
		for _, tag := range readinessSelectors(scope) {
			builds = append(builds, issueReadinessBuild{Registry: registry, Label: imagelookup.PreferredRegistryLabel(registry), Tag: tag, Line: readinessLine(tag), Matches: []issueReadinessMatch{}})
		}
	}
	type job struct {
		index int
		agent bool
	}
	jobs := make(chan job)
	var workers sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for task := range jobs {
				build := &builds[task.index]
				repo := "rancher/rancher"
				if task.agent {
					repo = "rancher/rancher-agent"
				}
				reference := build.Registry + "/" + repo + ":" + build.Tag
				Notify(ctx, "Inspecting "+reference)
				imageCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
				image := s.inspectImage(imageCtx, reference)
				cancel()
				if task.agent {
					build.Agent = image
				} else {
					build.Server = image
				}
			}
		}()
	}
dispatch:
	for i := range builds {
		for _, agent := range []bool{false, true} {
			select {
			case jobs <- job{i, agent}:
			case <-ctx.Done():
				break dispatch
			}
		}
	}
	close(jobs)
	workers.Wait()
	warnings := []string{}
	out := []issueReadinessBuild{}
	for _, build := range builds {
		if !build.Server.Found && !build.Agent.Found && build.Server.Error == "" && build.Agent.Error == "" {
			continue
		}
		s.readinessAssignHeadLine(ctx, &build)
		out = append(out, build)
	}
	if ctx.Err() != nil {
		warnings = append(warnings, "Target build inspection stopped before every candidate was checked.")
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.Line == scope.Line) != (b.Line == scope.Line) {
			return a.Line == scope.Line
		}
		if a.Registry == b.Registry {
			return a.Tag < b.Tag
		}
		return a.Registry < b.Registry
	})
	if len(out) == 0 {
		warnings = append(warnings, "No target server/agent selectors were available. Use Search other release heads for broader discovery.")
	}
	return out, warnings
}

var readinessDevVersionPattern = regexp.MustCompile(`RancherVersionDev\s*=\s*"([0-9]+\.[0-9]+)\.[0-9]+"`)

func (s *Service) readinessAssignHeadLine(ctx context.Context, build *issueReadinessBuild) {
	if build.Tag != "head" {
		return
	}
	line := readinessLine(build.Server.BuildVersion)
	if line == "" && build.Server.Found {
		repo, revision, err := readinessRancherSource(build.Server)
		if err == nil {
			raw, err := s.readinessFile(ctx, repo, revision, "pkg/settings/setting.go")
			if err == nil {
				if match := readinessDevVersionPattern.FindStringSubmatch(raw); len(match) > 1 {
					line = match[1]
				}
			}
		}
	}
	build.Line = line
	if line != "" {
		Notify(ctx, fmt.Sprintf("%s head identifies Rancher %s", build.Registry, line))
	}
}
