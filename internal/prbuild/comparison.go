package prbuild

import (
	"context"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	"strings"
	"sync"
)

type prBuildComparisonResult struct {
	comparison GitHubCompare
	err        error
}

func (s *Service) compareImageRevisions(ctx context.Context, target Target, pull PullRequest, registries []RegistryResult) error {
	type imagePointer struct {
		image *ImageResult
	}
	byRevision := map[string][]imagePointer{}
	for index := range registries {
		for _, image := range []*ImageResult{&registries[index].Server, &registries[index].Agent} {
			if !image.Found || image.Error != "" {
				continue
			}
			revision, label, reason := prBuildComparableRevision(target, *image)
			if revision == "" {
				image.Match = CommitMatch{
					Verdict:          "unknown",
					Reason:           reason,
					RequiredRevision: pull.InclusionCommitSHA,
					Basis:            pull.InclusionBasis,
				}
				continue
			}
			image.Match = CommitMatch{
				Verdict:           "unknown",
				Reason:            "GitHub ancestry has not been checked yet.",
				CandidateRevision: revision,
				RequiredRevision:  pull.InclusionCommitSHA,
				RevisionLabel:     label,
				Basis:             pull.InclusionBasis,
				CommitURL:         fmt.Sprintf("https://github.com/%s/%s/commit/%s", target.owner, target.repository, revision),
			}
			if revision == pull.InclusionCommitSHA {
				image.Match.Verdict = "included"
				image.Match.Relation = "exact"
				image.Match.Reason = "The image declares the exact commit selected from the pull request."
				continue
			}
			byRevision[revision] = append(byRevision[revision], imagePointer{image: image})
		}
	}

	comparisonResults := make(map[string]prBuildComparisonResult, len(byRevision))
	if len(byRevision) > 0 {
		jobs := make(chan string)
		results := make(chan struct {
			revision string
			result   prBuildComparisonResult
		})
		workerCount := prBuildWorkerLimit
		if len(byRevision) < workerCount {
			workerCount = len(byRevision)
		}
		var workers sync.WaitGroup
		for worker := 0; worker < workerCount; worker++ {
			workers.Add(1)
			go func() {
				defer workers.Done()
				for revision := range jobs {
					comparison, err := s.compare(ctx, target, pull.InclusionCommitSHA, revision)
					select {
					case results <- struct {
						revision string
						result   prBuildComparisonResult
					}{revision: revision, result: prBuildComparisonResult{comparison: comparison, err: err}}:
					case <-ctx.Done():
						return
					}
				}
			}()
		}
		go func() {
			for revision := range byRevision {
				select {
				case jobs <- revision:
				case <-ctx.Done():
					close(jobs)
					return
				}
			}
			close(jobs)
		}()
		go func() {
			workers.Wait()
			close(results)
		}()
		for result := range results {
			comparisonResults[result.revision] = result.result
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}

	for revision, pointers := range byRevision {
		result := comparisonResults[revision]
		for _, pointer := range pointers {
			applyPRBuildComparison(pointer.image, target, pull, result)
		}
	}
	return nil
}

func prBuildComparableRevision(target Target, image ImageResult) (string, string, string) {
	requestedRepository := target.owner + "/" + target.repository
	standardOwner, standardRepository, sourceErr := imagelookup.ParseGitHubSource(strings.TrimSpace(image.SourceURL), strings.Repeat("0", 40))
	standardRevision := strings.ToLower(strings.TrimSpace(image.Revision))
	if sourceErr == nil && strings.EqualFold(standardOwner+"/"+standardRepository, requestedRepository) && imagelookup.GitRevisionPattern.MatchString(standardRevision) {
		return standardRevision, imagelookup.RevisionLabel, ""
	}
	if strings.EqualFold(requestedRepository, "rancher/rancher") && sourceErr == nil && strings.EqualFold(standardOwner+"/"+standardRepository, "rancher/rancher-prime") {
		ossRevision := strings.ToLower(strings.TrimSpace(image.OSSRevision))
		if imagelookup.GitRevisionPattern.MatchString(ossRevision) {
			return ossRevision, imagelookup.OSSRevisionLabel, ""
		}
		return "", "", "The Prime image does not declare a valid full Rancher OSS revision."
	}
	if sourceErr != nil {
		return "", "", sourceErr.Error()
	}
	if strings.EqualFold(standardOwner+"/"+standardRepository, requestedRepository) {
		return "", "", "The image does not declare a valid full 40-character Git revision."
	}
	return "", "", fmt.Sprintf("The image source %s does not match pull request repository %s.", standardOwner+"/"+standardRepository, requestedRepository)
}

func applyPRBuildComparison(image *ImageResult, target Target, pull PullRequest, result prBuildComparisonResult) {
	image.Match.CompareURL = fmt.Sprintf("https://github.com/%s/%s/compare/%s...%s", target.owner, target.repository, pull.InclusionCommitSHA, image.Match.CandidateRevision)
	if result.err != nil {
		image.Match.Verdict = "unknown"
		image.Match.Reason = safePRBuildGitHubError(result.err, "commit ancestry")
		image.Match.ComparisonError = true
		return
	}
	comparison := result.comparison
	image.Match.Relation = comparison.Status
	switch comparison.Status {
	case "identical":
		image.Match.Verdict = "included"
		image.Match.Reason = "GitHub reports that the image revision is identical to the selected PR commit."
	case "ahead":
		if comparison.MergeBaseSHA != pull.InclusionCommitSHA {
			image.Match.Verdict = "unknown"
			image.Match.Reason = "GitHub reported the image revision as ahead, but did not confirm the selected PR commit as the merge base."
			image.Match.ComparisonError = true
			return
		}
		image.Match.Verdict = "included"
		image.Match.Reason = "GitHub confirms that the selected PR commit is an ancestor of the image's declared revision."
	case "behind", "diverged":
		image.Match.Verdict = "not_included"
		image.Match.Reason = "GitHub did not find the selected PR commit in this image revision's ancestry. This does not detect an equivalent cherry-pick with a different SHA."
	default:
		image.Match.Verdict = "unknown"
		image.Match.Reason = "GitHub returned an unrecognized commit relationship."
		image.Match.ComparisonError = true
	}
}
