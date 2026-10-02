package imagelookup

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type imageLookupPrimePairResult struct {
	tag         string
	status      string
	detail      string
	completedAt time.Time
	server      Provenance
	agent       Provenance
	lookupErr   error
}

func (s *Service) verifyPrimeHeadPairs(ctx context.Context, response *SearchResponse, options searchOptions) error {
	authorityGroups := make([]int, 0, 2)
	truncatedReference := ""
	for groupIndex := range response.Groups {
		group := &response.Groups[groupIndex]
		if group.Registry == "stgregistry.suse.com" && group.Error == "" && group.Truncated &&
			(group.ImageRole == "server" || group.ImageRole == "agent") && truncatedReference == "" {
			truncatedReference = group.Reference
		}
		if group.Registry != "stgregistry.suse.com" || group.Error != "" || group.Truncated ||
			(group.ImageRole != "server" && group.ImageRole != "agent") {
			continue
		}
		authorityGroups = append(authorityGroups, groupIndex)
	}
	if len(authorityGroups) == 0 {
		if truncatedReference != "" {
			return fmt.Errorf("Prime head pair verification is incomplete: %s matched more candidates than the bounded search could verify; narrow the patch, commit, or result set", truncatedReference)
		}
		return errors.New("Prime head pair verification is incomplete: neither the SUSE staging server nor agent search produced a successful complete candidate list")
	}

	candidateNames := map[string]struct{}{}
	for _, groupIndex := range authorityGroups {
		authority := &response.Groups[groupIndex]
		for tagIndex := range authority.Tags {
			tag := &authority.Tags[tagIndex]
			if tag.IsPrimeHead && tag.HeadKind == "immutable" && (options.primeVersion == "" || tag.Version == options.primeVersion) {
				candidateNames[tag.Name] = struct{}{}
			}
		}
	}
	for groupIndex := range response.Groups {
		group := &response.Groups[groupIndex]
		if group.Registry != "stgregistry.suse.com" || (group.ImageRole != "server" && group.ImageRole != "agent") {
			continue
		}
		for tagIndex := range group.Tags {
			tag := &group.Tags[tagIndex]
			if tag.IsPrimeHead && tag.HeadKind == "moving" {
				tag.PairStatus = "unverified"
				tag.PairError = "mutable Prime head selector; pair verification ranks immutable patch-qualified commit tags"
			}
		}
	}

	names := make([]string, 0, len(candidateNames))
	for tag := range candidateNames {
		names = append(names, tag)
	}
	sort.Slice(names, func(i, j int) bool { return NaturalCompare(names[i], names[j]) > 0 })

	results := make(chan imageLookupPrimePairResult, len(names))
	workerCount := 4
	if len(names) < workerCount {
		workerCount = len(names)
	}
	if workerCount > 0 {
		jobs := make(chan string)
		var workers sync.WaitGroup
		for worker := 0; worker < workerCount; worker++ {
			workers.Add(1)
			go func() {
				defer workers.Done()
				for tag := range jobs {
					results <- s.inspectPrimeHeadPair(ctx, tag)
				}
			}()
		}
		go func() {
			defer close(results)
			for _, tag := range names {
				select {
				case jobs <- tag:
				case <-ctx.Done():
					close(jobs)
					workers.Wait()
					return
				}
			}
			close(jobs)
			workers.Wait()
		}()
	} else {
		close(results)
	}

	byTag := make(map[string]imageLookupPrimePairResult, len(names))
	var lookupErrors []string
	for result := range results {
		byTag[result.tag] = result
		if result.lookupErr != nil {
			lookupErrors = append(lookupErrors, result.tag+": "+SafeError(result.lookupErr))
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(lookupErrors) > 0 {
		sort.Strings(lookupErrors)
		return fmt.Errorf("could not safely verify Prime head image pairs: %s", strings.Join(lookupErrors, "; "))
	}

	verified := make([]imageLookupPrimePairResult, 0, len(byTag))
	for _, result := range byTag {
		if result.status == "verified" {
			verified = append(verified, result)
		}
	}
	sort.SliceStable(verified, func(i, j int) bool {
		if verified[i].completedAt.Equal(verified[j].completedAt) {
			return NaturalCompare(verified[i].tag, verified[j].tag) > 0
		}
		return verified[i].completedAt.After(verified[j].completedAt)
	})
	ranks := make(map[string]int, len(verified))
	for index, result := range verified {
		ranks[result.tag] = index + 1
	}

	for groupIndex := range response.Groups {
		group := &response.Groups[groupIndex]
		for tagIndex := range group.Tags {
			tag := &group.Tags[tagIndex]
			result, ok := byTag[tag.Name]
			if !ok {
				continue
			}
			tag.PairStatus = result.status
			tag.PairComplete = result.status == "verified"
			tag.CompanionVerified = tag.PairComplete
			tag.ProvenanceValid = tag.PairComplete
			tag.PairError = result.detail
			tag.ResolvedRank = ranks[tag.Name]
			if !result.completedAt.IsZero() {
				tag.PairCompletedAt = imageLookupFormatTime(result.completedAt)
			}
			provenance := result.server
			if group.ImageRole == "agent" {
				provenance = result.agent
			}
			tag.PrimeSource = imageLookupRancherPrimeSource(result.server.SourceURL)
			tag.Source = result.server.SourceURL
			tag.CanonicalReference = provenance.CanonicalReference
			tag.OSSRevision = strings.ToLower(strings.TrimSpace(provenance.OSSRevision))
			if !provenance.CreatedAt.IsZero() {
				tag.CreatedAt = imageLookupFormatTime(provenance.CreatedAt)
			}
		}
		if options.pairStatus != "all" {
			filtered := group.Tags[:0]
			for _, tag := range group.Tags {
				if tag.PairStatus == options.pairStatus {
					filtered = append(filtered, tag)
				}
			}
			group.Tags = filtered
			group.Matched = len(filtered)
		}
		imageLookupApplyRecentFilter(group, options)
		if options.sortBy == "pair-completed" {
			imageLookupSortTags(group.Tags, options.sortBy, options.sortOrder)
		}
		imageLookupCountPrimeHeads(group, group.Tags)
		imageLookupCountPrimePairStatuses(group, group.Tags)
	}
	return nil
}

func (s *Service) inspectPrimeHeadPair(ctx context.Context, tag string) imageLookupPrimePairResult {
	result := imageLookupPrimePairResult{tag: tag}
	classification := imageLookupClassifyTag("stgregistry.suse.com", "rancher/rancher", tag)
	if !classification.IsPrimeHead || classification.HeadKind != "immutable" {
		result.status = "invalid"
		result.detail = "candidate is not an immutable patch-qualified Prime head tag"
		return result
	}
	serverReference := "stgregistry.suse.com/rancher/rancher:" + tag
	agentReference := "stgregistry.suse.com/rancher/rancher-agent:" + tag

	server, serverFound, err := InspectProvenance(ctx, s, serverReference)
	result.server = server
	if err != nil {
		result.lookupErr = fmt.Errorf("inspect server image: %w", err)
		return result
	}
	if !serverFound {
		result.status = "missing"
		result.detail = "server image was not found"
		return result
	}
	agent, agentFound, err := InspectProvenance(ctx, s, agentReference)
	result.agent = agent
	if err != nil {
		result.lookupErr = fmt.Errorf("inspect agent image: %w", err)
		return result
	}
	if !agentFound {
		result.status = "missing"
		result.detail = "matching rancher-agent image was not found"
		return result
	}
	if err := imageLookupValidateExactPrimeHeadPair(tag, server, agent); err != nil {
		result.status = "invalid"
		result.detail = err.Error()
		return result
	}
	if err := ValidatePatchHeadServerProvenance(classification.BaseTag, server); err != nil {
		result.status = "invalid"
		result.detail = err.Error()
		return result
	}
	if server.CreatedAt.IsZero() {
		result.status = "invalid"
		result.detail = "server image did not declare a creation timestamp"
		return result
	}
	if agent.CreatedAt.IsZero() {
		result.status = "invalid"
		result.detail = "agent image did not declare a creation timestamp"
		return result
	}
	result.status = "verified"
	result.completedAt = server.CreatedAt
	if agent.CreatedAt.After(result.completedAt) {
		result.completedAt = agent.CreatedAt
	}
	return result
}

func imageLookupValidateExactPrimeHeadPair(tag string, server, agent Provenance) error {
	expectedTag := NormalizeDockerRancherTag(NormalizeVersionInput(tag))
	_, serverCanonicalRepository, serverCanonicalTag, serverErr := ParseRegistryImage(server.CanonicalReference)
	_, agentCanonicalRepository, agentCanonicalTag, agentErr := ParseRegistryImage(agent.CanonicalReference)
	if serverErr != nil || agentErr != nil || serverCanonicalTag == "" || agentCanonicalTag == "" {
		return fmt.Errorf("exact Rancher head image pair %s did not declare canonical server and agent org.opensuse.reference labels", expectedTag)
	}
	if serverCanonicalRepository != "rancher/rancher" || agentCanonicalRepository != "rancher/rancher-agent" {
		return fmt.Errorf("exact Rancher head image pair %s has unexpected canonical repositories: server %s, agent %s", expectedTag, serverCanonicalRepository, agentCanonicalRepository)
	}
	if serverCanonicalTag != expectedTag || agentCanonicalTag != expectedTag {
		return fmt.Errorf("exact Rancher head image pair %s has mismatched canonical tags: server %s, agent %s", expectedTag, serverCanonicalTag, agentCanonicalTag)
	}
	return nil
}
