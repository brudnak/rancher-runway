package prbuild

import (
	"context"
)

func (verifier *Service) NewIssueScan() func(context.Context, Request) (Report, error) {
	cache := &readinessEvidenceCache{entries: map[string]readinessCachedEvidence{}}
	type observed struct {
		builds   []issueReadinessBuild
		warnings []string
	}
	observations := map[readinessSearchScope]observed{}
	return func(ctx context.Context, req Request) (Report, error) {
		ctx = context.WithValue(ctx, readinessCacheKey{}, cache)
		return verifier.checkIssue(ctx, req, func(ctx context.Context) ([]issueReadinessBuild, []string) {
			scope, _ := ctx.Value(readinessScopeKey{}).(readinessSearchScope)
			if saved, ok := observations[scope]; ok {
				Notify(ctx, "Reusing this scan’s observed head images for the same release.")
				return saved.builds, saved.warnings
			}
			builds, warnings := verifier.readinessBuilds(ctx)
			if ctx.Err() == nil {
				observations[scope] = observed{builds, warnings}
			}
			return builds, warnings
		})
	}
}
