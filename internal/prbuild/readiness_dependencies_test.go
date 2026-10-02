package prbuild

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	"reflect"
	"strings"
	"testing"
)

const readinessDependencySHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
const readinessRancherSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const readinessFixSHA = "cccccccccccccccccccccccccccccccccccccccc"

func readinessDependencyService(t *testing.T, files map[string]string, status string) *Service {
	t.Helper()
	return &Service{
		runCommand: func(_ context.Context, _ string, args, _ []string, _ int64) ([]byte, error) {
			request := strings.Join(args, " ")
			if strings.Contains(request, "/commits/") {
				return json.Marshal(map[string]string{"sha": readinessDependencySHA})
			}
			for path, content := range files {
				if strings.Contains(request, "/contents/"+path+"?") {
					return json.Marshal(map[string]string{"text": content})
				}
			}
			return nil, fmt.Errorf("fixture source is unavailable")
		},
		compare: func(_ context.Context, _ Target, base, head string) (GitHubCompare, error) {
			if base != readinessFixSHA || head != readinessDependencySHA {
				t.Errorf("wrong ancestry comparison: %s...%s", base, head)
			}
			return GitHubCompare{Status: status, MergeBaseSHA: base}, nil
		},
		inspect: func(_ context.Context, req imagelookup.InspectRequest) (imagelookup.InspectResponse, error) {
			if req.Reference != "docker.io/rancher/rancher-webhook:v0.12.1-rc.6" {
				t.Errorf("unexpected image %s", req.Reference)
			}
			return imagelookup.InspectResponse{Reference: req.Reference, Digest: "sha256:observed-webhook", Config: imagelookup.ImageConfig{Labels: map[string]string{imagelookup.SourceLabel: "https://github.com/rancher/webhook", imagelookup.RevisionLabel: readinessDependencySHA}}}, nil
		},
	}
}
func TestReadinessStevePinnedDependency(t *testing.T) {
	target := Target{owner: "rancher", repository: "steve", number: 1256}
	pull := PullRequest{InclusionCommitSHA: readinessFixSHA}
	image := ImageResult{Found: true, SourceURL: "https://github.com/rancher/rancher", Revision: readinessRancherSHA}
	for _, tc := range []struct{ name, mod, status, want, pin string }{
		{"included", "require github.com/rancher/steve v0.10.4", "ahead", "included", "v0.10.4"},
		{"not bumped", "require github.com/rancher/steve v0.10.3", "behind", "not_included", "v0.10.3"},
		{"pseudo replacement", "require github.com/rancher/steve v0.10.3\nreplace github.com/rancher/steve => github.com/rancher/steve v0.10.4-0.20260930120000-bbbbbbbbbbbb", "ahead", "included", "v0.10.4-0.20260930120000-bbbbbbbbbbbb"},
		{"fork", "require github.com/rancher/steve v0.10.4\nreplace github.com/rancher/steve => github.com/example/steve v0.10.4", "ahead", "unknown", ""},
		{"local replacement", "require github.com/rancher/steve v0.10.4\nreplace github.com/rancher/steve => ../steve", "ahead", "unknown", ""},
		{"missing", "", "ahead", "unknown", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := readinessDependencyService(t, map[string]string{"go.mod": "module github.com/rancher/rancher\n" + tc.mod + "\n"}, tc.status)
			got := s.readinessSteveMatch(readinessWithEvidenceCache(context.Background()), target, pull, image)
			if got.Verdict != tc.want || got.Pin != tc.pin {
				t.Fatalf("match=%+v", got)
			}
			if got.Verdict == "included" && (!strings.Contains(got.EvidenceURL, readinessRancherSHA) || got.CandidateRevision != readinessDependencySHA) {
				t.Fatalf("unbound evidence %+v", got)
			}
		})
	}
}
func TestReadinessWebhookPinnedChart(t *testing.T) {
	target := Target{owner: "rancher", repository: "webhook", number: 42}
	pull := PullRequest{InclusionCommitSHA: readinessFixSHA}
	path := "charts/rancher-webhook/111.0.0%2Bup0.12.1-rc.6/"
	files := map[string]string{
		"build.yaml":         "webhookVersion: 111.0.0+up0.12.1-rc.6\nchartDefaultBranch: dev-v2.16\n",
		path + "Chart.yaml":  "name: rancher-webhook\nversion: 111.0.0+up0.12.1-rc.6\nappVersion: 0.12.1-rc.6\n",
		path + "values.yaml": "image:\n  repository: rancher/rancher-webhook\n  tag: v0.12.1-rc.6\n",
	}
	// PathEscape preserves '+' in path segments; query encoding must not alter it.
	for key, value := range files {
		if strings.Contains(key, "%2B") {
			delete(files, key)
			files[strings.ReplaceAll(key, "%2B", "+")] = value
		}
	}
	image := ImageResult{Found: true, SourceURL: "https://github.com/rancher/rancher", Revision: readinessRancherSHA}
	s := readinessDependencyService(t, files, "ahead")
	registries := []RegistryResult{{Server: image, Agent: image}}
	if err := s.compareReadinessRevisions(readinessWithEvidenceCache(context.Background()), target, pull, registries); err != nil {
		t.Fatal(err)
	}
	got := registries[0].Server.Match
	if got.Verdict != "included" || got.ComponentDigest != "sha256:observed-webhook" || !strings.Contains(got.ChartURL, readinessDependencySHA) || registries[0].Agent.Match.Verdict != "not_applicable" {
		t.Fatalf("server=%+v agent=%+v", got, registries[0].Agent.Match)
	}
	files[strings.ReplaceAll(path, "%2B", "+")+"values.yaml"] = "image:\n  repository: rancher/rancher-webhook\n  tag: v0.11.0\n"
	if got = s.readinessWebhookMatch(context.Background(), target, pull, image); got.Verdict != "unknown" {
		t.Fatalf("mismatched chart accepted: %+v", got)
	}
	delete(files, "build.yaml")
	if got = s.readinessWebhookMatch(context.Background(), target, pull, image); got.Verdict != "unknown" {
		t.Fatalf("missing pin accepted: %+v", got)
	}
}
func TestReadinessTargetHeadsAndDefaultLine(t *testing.T) {
	if got := readinessSelectors(readinessSearchScope{Line: "2.16", Version: "2.16.0"}); !reflect.DeepEqual(got, []string{"head", "v2.16-head", "v2.16.0-head"}) {
		t.Fatal(got)
	}
	s := readinessDependencyService(t, map[string]string{"pkg/settings/setting.go": `const RancherVersionDev = "2.17.99"`}, "ahead")
	build := issueReadinessBuild{Tag: "head", Server: ImageResult{Found: true, SourceURL: "https://github.com/rancher/rancher", Revision: readinessRancherSHA}}
	s.readinessAssignHeadLine(context.Background(), &build)
	if build.Line != "2.17" {
		t.Fatalf("default head not resolved dynamically: %+v", build)
	}
}
func TestReadinessExplicitDependencyIssueDeclaration(t *testing.T) {
	issue := "https://github.com/rancher/rancher/issues/56397"
	target := Target{owner: "rancher", repository: "steve"}
	for _, tc := range []struct {
		body string
		want bool
	}{
		{"# Issue " + issue, true}, {"# Issue\n\n" + issue, true}, {"# Issue\nUnrelated prose\n" + issue, false}, {"Fixes " + issue, true}, {"Related discussion: " + issue, false}, {"> Fixes " + issue, false}, {"```md\nFixes " + issue + "\n```", false}, {"<!-- Fixes " + issue + " -->", false}, {"Issue #56397", false}, {"This does not fix " + issue, false},
	} {
		if got := readinessDeclaredIssue(tc.body, issue, target); got != tc.want {
			t.Errorf("%q = %v", tc.body, got)
		}
	}
}
