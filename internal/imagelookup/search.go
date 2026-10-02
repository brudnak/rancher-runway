package imagelookup

import (
	"context"
	"fmt"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"strings"
	"sync"
	"time"
)

func (s *Service) Search(ctx context.Context, request SearchRequest) (SearchResponse, error) {
	s.defaults()
	targets, options, err := s.searchParameters(request)
	if err != nil {
		return SearchResponse{}, err
	}
	searchedAt := s.now().UTC()
	if options.recentDays > 0 {
		options.recentCutoff = searchedAt.Add(-time.Duration(options.recentDays) * 24 * time.Hour)
	}

	response := SearchResponse{
		Query:        options.query,
		RecentDays:   options.recentDays,
		ScanMode:     options.scanMode,
		Channel:      options.channel,
		Architecture: options.architecture,
		PrimeHead:    options.primeHead,
		HeadKind:     options.headKind,
		VersionLine:  options.versionLine,
		Commit:       options.commit,
		PairStatus:   options.pairStatus,
		SortBy:       options.sortBy,
		SortOrder:    options.sortOrder,
		SearchedAt:   searchedAt,
		Groups:       make([]SearchGroup, len(targets)),
	}
	if !options.recentCutoff.IsZero() {
		response.RecentCutoff = imageLookupFormatTime(options.recentCutoff)
	}

	workerLimit := 4
	if len(targets) < workerLimit {
		workerLimit = len(targets)
	}
	jobs := make(chan int)
	var workers sync.WaitGroup
	for worker := 0; worker < workerLimit; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				response.Groups[index] = s.searchTargetWithOptions(ctx, targets[index], options)
			}
		}()
	}
	for index := range targets {
		select {
		case jobs <- index:
		case <-ctx.Done():
			close(jobs)
			workers.Wait()
			return SearchResponse{}, ctx.Err()
		}
	}
	close(jobs)
	workers.Wait()

	succeeded := 0
	for _, group := range response.Groups {
		if group.Error == "" {
			succeeded++
		}
	}
	if succeeded == 0 {
		if err := ctx.Err(); err != nil {
			return SearchResponse{}, err
		}
		return SearchResponse{}, fmt.Errorf("all registry searches failed")
	}
	if options.verifyPrimePairs {
		if err := s.verifyPrimeHeadPairs(ctx, &response, options); err != nil {
			return SearchResponse{}, err
		}
	}
	return response, nil
}

func (s *Service) searchTarget(ctx context.Context, target searchTarget, query string, limit int, includeArtifacts bool) SearchGroup {
	return s.searchTargetWithOptions(ctx, target, searchOptions{
		query:            query,
		limit:            limit,
		includeArtifacts: includeArtifacts,
		channel:          "all",
		architecture:     "all",
		primeHead:        "all",
		headKind:         "all",
		sortBy:           "natural",
		sortOrder:        "desc",
		exactLookup:      true,
	})
}

func (s *Service) searchTargetWithOptions(ctx context.Context, target searchTarget, options searchOptions) SearchGroup {
	canonical := target.registry + "/" + target.repository
	imageRole, companionRepository := imageLookupRepositoryRole(target.repository)
	group := SearchGroup{
		Key:                 canonical,
		Label:               RegistryLabel(target.registry) + " / " + target.repository,
		Registry:            target.registry,
		Repository:          target.repository,
		Reference:           canonical,
		ImageRole:           imageRole,
		CompanionRepository: companionRepository,
		Tags:                []Tag{},
	}

	repository, err := s.parseRepository(target.registry, target.repository)
	if err != nil {
		group.Error = SafeError(err)
		return group
	}
	if exactDigest, ok := imageLookupExactDigestReference(repository, options.query, s.allowHTTP); ok {
		descriptor, getErr := remote.Get(exactDigest, s.remoteOptions(ctx, nil, 0)...)
		group.Scanned = 1
		if getErr == nil {
			digest := strings.ToLower(options.query)
			result := imageLookupClassifyDigest(target.registry, target.repository, digest)
			result.Digest = descriptor.Digest.String()
			if imageLookupTagMatchesOptions(result, options) {
				group.Matched = 1
				group.Tags = []Tag{result}
			}
			if !options.verifyPrimePairs {
				imageLookupApplyRecentFilter(&group, options)
			}
			return group
		}
		if !RegistryNotFound(getErr) {
			group.Error = SafeError(getErr)
		}
		return group
	}
	if exactTag, ok := imageLookupExactTagReference(repository, options.query, s.allowHTTP); ok &&
		!imageLookupPatchHeadSelector(options.query) &&
		(options.exactLookup || !imageLookupBarePatchVersion(options.query)) &&
		(options.includeArtifacts || !imageLookupArtifactTag(options.query)) {
		descriptor, getErr := remote.Get(exactTag, s.remoteOptions(ctx, nil, 0)...)
		if getErr == nil {
			group.Scanned = 1
			tag := imageLookupClassifyTag(target.registry, target.repository, options.query)
			tag.Digest = descriptor.Digest.String()
			if imageLookupTagMatchesOptions(tag, options) {
				group.Matched = 1
				group.Tags = []Tag{tag}
			}
			if target.registry == "docker.io" {
				requireComplete := options.sortBy == "uploaded" || options.recentDays > 0
				complete, metadataErr := s.enrichDockerHubTags(ctx, target.repository, options.query, group.Tags, requireComplete)
				if requireComplete {
					switch {
					case metadataErr != nil:
						group.Error = SafeError(metadataErr)
					case !complete && options.sortBy == "uploaded":
						group.Error = "Docker Hub did not expose upload metadata for every matched tag"
					}
				}
			}
			if !options.verifyPrimePairs {
				imageLookupApplyRecentFilter(&group, options)
			}
			imageLookupCountPrimeHeads(&group, group.Tags)
			return group
		}
		if !RegistryNotFound(getErr) {
			group.Error = SafeError(getErr)
			return group
		}
		if imageLookupFullVersionTag(options.query) {
			group.Scanned = 1
			return group
		}
	}
	puller, err := remote.NewPuller(s.remoteOptions(ctx, nil, imageLookupTagPageSize)...)
	if err != nil {
		group.Error = SafeError(err)
		return group
	}
	lister, err := puller.Lister(ctx, repository)
	if err != nil {
		group.Error = SafeError(err)
		return group
	}

	matches := make([]Tag, 0, options.limit)
	limitReached := false
	for lister.HasNext() && group.Scanned < s.maxTagScan {
		page, pageErr := lister.Next(ctx)
		if pageErr != nil {
			group.Error = SafeError(pageErr)
			break
		}
		for pageIndex, tagName := range page.Tags {
			if group.Scanned >= s.maxTagScan {
				group.Truncated = true
				break
			}
			group.Scanned++
			if !options.includeArtifacts && imageLookupArtifactTag(tagName) {
				continue
			}
			tag := imageLookupClassifyTag(target.registry, target.repository, tagName)
			if !imageLookupTagMatchesOptions(tag, options) {
				continue
			}
			matches = append(matches, tag)
			if !options.fullScan && len(matches) == options.limit {
				limitReached = true
				if pageIndex+1 < len(page.Tags) || lister.HasNext() {
					group.Truncated = true
				}
				break
			}
		}
		if limitReached {
			break
		}
	}
	if !limitReached && lister.HasNext() {
		group.Truncated = true
	}

	group.Matched = len(matches)
	if target.registry == "docker.io" && len(matches) > 0 {
		requireComplete := options.sortBy == "uploaded" || options.recentDays > 0
		complete, metadataErr := s.enrichDockerHubTags(ctx, target.repository, options.query, matches, requireComplete)
		if requireComplete {
			switch {
			case metadataErr != nil:
				group.Error = SafeError(metadataErr)
			case !complete && options.sortBy == "uploaded":
				group.Error = "Docker Hub did not expose upload metadata for every matched tag"
			}
		}
	}
	if !options.verifyPrimePairs && !options.recentCutoff.IsZero() {
		matches, group.RecentExcludedCount, group.UnknownTimestampCount = imageLookupFilterRecentTags(matches, options.recentCutoff)
		group.Matched = len(matches)
	}
	imageLookupSortTags(matches, options.sortBy, options.sortOrder)
	imageLookupCountPrimeHeads(&group, matches)
	if options.fullScan && len(matches) > options.limit {
		group.Truncated = true
		matches = matches[:options.limit]
	}
	group.Tags = matches
	return group
}
