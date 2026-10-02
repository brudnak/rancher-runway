package prbuild

import (
	"context"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"gopkg.in/yaml.v3"
)

type readinessCacheKey struct{}
type readinessCachedEvidence struct {
	value any
	err   error
}
type readinessEvidenceCache struct {
	mu      sync.Mutex
	entries map[string]readinessCachedEvidence
}

func readinessWithEvidenceCache(ctx context.Context) context.Context {
	if ctx.Value(readinessCacheKey{}) != nil {
		return ctx
	}
	return context.WithValue(ctx, readinessCacheKey{}, &readinessEvidenceCache{entries: map[string]readinessCachedEvidence{}})
}
func readinessCached(ctx context.Context, key string, fetch func() (any, error)) (any, error) {
	cache, _ := ctx.Value(readinessCacheKey{}).(*readinessEvidenceCache)
	if cache == nil {
		return fetch()
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if item, ok := cache.entries[key]; ok {
		return item.value, item.err
	}
	value, err := fetch()
	if ctx.Err() == nil {
		cache.entries[key] = readinessCachedEvidence{value, err}
	}
	return value, err
}
func (s *Service) readinessFile(ctx context.Context, repo, ref, path string) (string, error) {
	value, err := readinessCached(ctx, "file:"+repo+":"+ref+":"+path, func() (any, error) {
		var result struct {
			Text string `json:"text"`
		}
		parts := strings.Split(path, "/")
		for i := range parts {
			parts[i] = url.PathEscape(parts[i])
		}
		err := s.runGitHubJSON(ctx, "repos/"+repo+"/contents/"+strings.Join(parts, "/")+"?ref="+url.QueryEscape(ref), `{text:(.content|@base64d)}`, &result, "pinned source file")
		return result.Text, err
	})
	if err != nil {
		return "", err
	}
	return value.(string), nil
}
func (s *Service) readinessRevision(ctx context.Context, repo, ref string) (string, error) {
	value, err := readinessCached(ctx, "revision:"+repo+":"+ref, func() (any, error) {
		var result struct {
			SHA string `json:"sha"`
		}
		err := s.runGitHubJSON(ctx, "repos/"+repo+"/commits/"+url.PathEscape(ref), "{sha}", &result, "dependency revision")
		if err == nil && !imagelookup.GitRevisionPattern.MatchString(result.SHA) {
			err = fmt.Errorf("GitHub did not return a full dependency commit")
		}
		return result.SHA, err
	})
	if err != nil {
		return "", err
	}
	return value.(string), nil
}
func readinessRancherSource(image ImageResult) (string, string, error) {
	owner, repo, err := imagelookup.ParseGitHubSource(image.SourceURL, image.Revision)
	if err != nil || !imagelookup.GitRevisionPattern.MatchString(image.Revision) {
		return "", "", fmt.Errorf("The image does not declare a complete Rancher source revision")
	}
	repository := strings.ToLower(owner + "/" + repo)
	if repository != "rancher/rancher" && repository != "rancher/rancher-prime" {
		return "", "", fmt.Errorf("The image source is not a supported Rancher repository")
	}
	return repository, image.Revision, nil
}
func readinessModulePin(raw, modulePath string) (string, string, error) {
	file, err := modfile.Parse("go.mod", []byte(raw), nil)
	if err != nil {
		return "", "", fmt.Errorf("The pinned go.mod could not be parsed")
	}
	version := ""
	for _, require := range file.Require {
		if require.Mod.Path == modulePath {
			version = require.Mod.Version
			break
		}
	}
	if version == "" {
		return "", "", fmt.Errorf("The Rancher revision does not declare this Go module")
	}
	for _, replace := range file.Replace {
		if replace.Old.Path == modulePath && (replace.Old.Version == "" || replace.Old.Version == version) {
			modulePath = replace.New.Path
			version = replace.New.Version
			break
		}
	}
	if !strings.HasPrefix(modulePath, "github.com/") || version == "" {
		return "", "", fmt.Errorf("The module uses a local or unsupported replacement; inclusion is unproven")
	}
	return strings.TrimPrefix(modulePath, "github.com/"), version, nil
}
func (s *Service) readinessPinnedMatch(ctx context.Context, target Target, pull PullRequest, revision string) CommitMatch {
	image := ImageResult{Match: CommitMatch{Verdict: "unknown", CandidateRevision: revision, RequiredRevision: pull.InclusionCommitSHA, Basis: pull.InclusionBasis}}
	value, err := readinessCached(ctx, "compare:"+target.owner+"/"+target.repository+":"+pull.InclusionCommitSHA+":"+revision, func() (any, error) { return s.compare(ctx, target, pull.InclusionCommitSHA, revision) })
	result := prBuildComparisonResult{err: err}
	if err == nil {
		result.comparison = value.(GitHubCompare)
	}
	applyPRBuildComparison(&image, target, pull, result)
	return image.Match
}
func (s *Service) readinessSteveMatch(ctx context.Context, target Target, pull PullRequest, image ImageResult) CommitMatch {
	unknown := func(err error) CommitMatch {
		return CommitMatch{Verdict: "unknown", Reason: err.Error(), Basis: "rancher_module_pin"}
	}
	repo, revision, err := readinessRancherSource(image)
	if err != nil {
		return unknown(err)
	}
	raw, err := s.readinessFile(ctx, repo, revision, "go.mod")
	if err != nil {
		return unknown(err)
	}
	moduleRepo, version, err := readinessModulePin(raw, "github.com/rancher/steve")
	if err != nil {
		return unknown(err)
	}
	if !strings.EqualFold(moduleRepo, target.owner+"/"+target.repository) {
		return unknown(fmt.Errorf("Rancher replaces Steve with %s; upstream ancestry alone cannot prove this fork", moduleRepo))
	}
	ref := version
	if module.IsPseudoVersion(version) {
		ref, err = module.PseudoVersionRev(version)
		if err != nil {
			return unknown(err)
		}
	}
	pinned, err := s.readinessRevision(ctx, moduleRepo, ref)
	if err != nil {
		return unknown(err)
	}
	result := s.readinessPinnedMatch(ctx, target, pull, pinned)
	result.Basis = "rancher_module_pin"
	result.Pin = version
	result.EvidenceURL = "https://github.com/" + repo + "/blob/" + revision + "/go.mod"
	result.Reason = "Rancher pins Steve " + version + " at " + pinned[:12] + ". " + readinessDependencyConclusion(result.Verdict)
	return result
}
func readinessDependencyConclusion(verdict string) string {
	switch verdict {
	case "included":
		return "That dependency includes the merged PR."
	case "not_included":
		return "The PR is merged upstream, but this pin does not contain its merge commit. Equivalent cherry-picks with a different SHA are not detected."
	default:
		return "Dependency inclusion could not be established."
	}
}

var readinessChartVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+\+up[0-9A-Za-z.+-]+$`)

func (s *Service) readinessWebhookMatch(ctx context.Context, target Target, pull PullRequest, image ImageResult) CommitMatch {
	unknown := func(err error) CommitMatch {
		return CommitMatch{Verdict: "unknown", Reason: err.Error(), Basis: "rancher_webhook_chart"}
	}
	repo, revision, err := readinessRancherSource(image)
	if err != nil {
		return unknown(err)
	}
	raw, err := s.readinessFile(ctx, repo, revision, "build.yaml")
	if err != nil {
		return unknown(err)
	}
	var config struct {
		Version string `yaml:"webhookVersion"`
		Branch  string `yaml:"chartDefaultBranch"`
	}
	if err = yaml.Unmarshal([]byte(raw), &config); err != nil {
		return unknown(fmt.Errorf("Cannot parse Rancher build.yaml"))
	}
	if image.WebhookVersion != "" {
		config.Version = image.WebhookVersion
	}
	if image.ChartBranch != "" {
		config.Branch = image.ChartBranch
	}
	if !readinessChartVersion.MatchString(config.Version) || config.Branch == "" {
		return unknown(fmt.Errorf("Rancher does not declare a supported pinned webhook chart"))
	}
	chartsCommit, err := s.readinessRevision(ctx, "rancher/charts", config.Branch)
	if err != nil {
		return unknown(err)
	}
	path := "charts/rancher-webhook/" + config.Version
	chart, err := s.readinessFile(ctx, "rancher/charts", chartsCommit, path+"/Chart.yaml")
	if err != nil {
		return unknown(err)
	}
	var metadata struct {
		Name       string `yaml:"name"`
		Version    string `yaml:"version"`
		AppVersion string `yaml:"appVersion"`
	}
	if err = yaml.Unmarshal([]byte(chart), &metadata); err != nil || metadata.Name != "rancher-webhook" || metadata.Version != config.Version {
		return unknown(fmt.Errorf("Webhook chart metadata did not match Rancher's pinned version"))
	}
	values, err := s.readinessFile(ctx, "rancher/charts", chartsCommit, path+"/values.yaml")
	if err != nil {
		return unknown(err)
	}
	var defaults struct {
		Image struct{ Repository, Tag string }
	}
	if err = yaml.Unmarshal([]byte(values), &defaults); err != nil || defaults.Image.Repository != "rancher/rancher-webhook" || strings.TrimPrefix(defaults.Image.Tag, "v") != strings.TrimPrefix(metadata.AppVersion, "v") {
		return unknown(fmt.Errorf("Webhook image defaults did not match the chart appVersion"))
	}
	reference := "docker.io/" + defaults.Image.Repository + ":" + defaults.Image.Tag
	value, err := readinessCached(ctx, "image:"+reference, func() (any, error) { return s.inspectImage(ctx, reference), nil })
	if err != nil {
		return unknown(err)
	}
	component := value.(ImageResult)
	if !component.Found || component.Error != "" {
		return unknown(fmt.Errorf("The pinned webhook image could not be inspected: %s", reference))
	}
	componentRevision, _, reason := prBuildComparableRevision(target, component)
	if componentRevision == "" {
		return unknown(fmt.Errorf("Webhook image: %s", reason))
	}
	result := s.readinessPinnedMatch(ctx, target, pull, componentRevision)
	result.Basis = "rancher_webhook_chart"
	result.Pin = config.Version
	result.ComponentImage = component.Reference
	result.ComponentDigest = component.PlatformDigest
	result.EvidenceURL = "https://github.com/" + repo + "/blob/" + revision + "/build.yaml"
	result.ChartURL = "https://github.com/rancher/charts/blob/" + chartsCommit + "/" + path + "/values.yaml"
	result.Reason = "Rancher defaults to webhook chart " + config.Version + " → " + reference + ". " + readinessDependencyConclusion(result.Verdict) + " Cluster overrides can select a different webhook."
	return result
}
func (s *Service) compareReadinessRevisions(ctx context.Context, target Target, pull PullRequest, registries []RegistryResult) error {
	repo := strings.ToLower(target.owner + "/" + target.repository)
	if repo != "rancher/steve" && repo != "rancher/webhook" {
		return s.compareImageRevisions(ctx, target, pull, registries)
	}
	Notify(ctx, "Tracing "+repo+" through the dependency pinned by each Rancher build…")
	for i := range registries {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		for _, image := range []*ImageResult{&registries[i].Server, &registries[i].Agent} {
			if repo == "rancher/webhook" && image == &registries[i].Agent {
				image.Match = CommitMatch{Verdict: "not_applicable", Reason: "Webhook is a separate component selected by the Rancher server, not embedded in the agent."}
				continue
			}
			if !image.Found || image.Error != "" {
				continue
			}
			if repo == "rancher/steve" {
				image.Match = s.readinessSteveMatch(ctx, target, pull, *image)
			} else {
				image.Match = s.readinessWebhookMatch(ctx, target, pull, *image)
			}
		}
	}
	return nil
}
