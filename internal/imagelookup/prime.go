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

type imageLookupPrimePairCandidate struct {
	repository string
	tag        string
}

type imageLookupPrimePairResult struct {
	candidate   imageLookupPrimePairCandidate
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

	candidateNames := map[imageLookupPrimePairCandidate]struct{}{}
	for _, groupIndex := range authorityGroups {
		authority := &response.Groups[groupIndex]
		for tagIndex := range authority.Tags {
			tag := &authority.Tags[tagIndex]
			if tag.IsPrimeHead && tag.HeadKind == "immutable" && (options.primeVersion == "" || tag.Version == options.primeVersion) {
				candidateNames[imageLookupPrimePairCandidate{repository: authority.Repository, tag: tag.Name}] = struct{}{}
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

	names := make([]imageLookupPrimePairCandidate, 0, len(candidateNames))
	for tag := range candidateNames {
		names = append(names, tag)
	}
	sort.Slice(names, func(i, j int) bool {
		return NaturalCompare(names[i].repository+":"+names[i].tag, names[j].repository+":"+names[j].tag) > 0
	})

	results := make(chan imageLookupPrimePairResult, len(names))
	workerCount := 4
	if len(names) < workerCount {
		workerCount = len(names)
	}
	if workerCount > 0 {
		jobs := make(chan imageLookupPrimePairCandidate)
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

	byTag := make(map[imageLookupPrimePairCandidate]imageLookupPrimePairResult, len(names))
	var lookupErrors []string
	for result := range results {
		byTag[result.candidate] = result
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
	seenPairs := make(map[string]bool)
	for _, result := range byTag {
		if result.status == "verified" && !seenPairs[result.server.Reference] {
			seenPairs[result.server.Reference] = true
			verified = append(verified, result)
		}
	}
	sort.SliceStable(verified, func(i, j int) bool {
		if verified[i].completedAt.Equal(verified[j].completedAt) {
			if verified[i].tag != verified[j].tag {
				return NaturalCompare(verified[i].tag, verified[j].tag) > 0
			}
			return verified[i].server.Reference < verified[j].server.Reference
		}
		return verified[i].completedAt.After(verified[j].completedAt)
	})
	ranks := make(map[string]int, len(verified))
	for index, result := range verified {
		ranks[result.server.Reference] = index + 1
	}

	for groupIndex := range response.Groups {
		group := &response.Groups[groupIndex]
		for tagIndex := range group.Tags {
			tag := &group.Tags[tagIndex]
			result, ok := byTag[imageLookupPrimePairCandidate{repository: group.Repository, tag: tag.Name}]
			if !ok || group.Registry != "stgregistry.suse.com" || group.Error != "" || group.Truncated {
				continue
			}
			tag.PairStatus = result.status
			tag.PairComplete = result.status == "verified"
			tag.CompanionVerified = tag.PairComplete
			tag.ProvenanceValid = tag.PairComplete
			tag.PairError = result.detail
			tag.ResolvedRank = ranks[result.server.Reference]
			if !result.completedAt.IsZero() {
				tag.PairCompletedAt = imageLookupFormatTime(result.completedAt)
			}
			provenance := result.server
			if group.ImageRole == "agent" {
				provenance = result.agent
				if result.server.Reference != "" {
					tag.CompanionReference = result.server.Reference
				}
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

func (s *Service) inspectPrimeHeadPair(ctx context.Context, candidate imageLookupPrimePairCandidate) imageLookupPrimePairResult {
	tag := candidate.tag
	result := imageLookupPrimePairResult{tag: tag, candidate: candidate}
	classification := imageLookupClassifyTag("stgregistry.suse.com", "rancher/rancher", tag)
	if !classification.IsPrimeHead || classification.HeadKind != "immutable" {
		result.status = "invalid"
		result.detail = "candidate is not an immutable patch-qualified Prime head tag"
		return result
	}
	serverRepositories := []string{candidate.repository}
	if candidate.repository == "rancher/rancher-agent" {
		serverRepositories = RancherServerRepositories()
	}
	var server Provenance
	serverFound := false
	for _, repository := range serverRepositories {
		var err error
		server, serverFound, err = InspectProvenance(ctx, s, "stgregistry.suse.com/"+repository+":"+tag)
		if err != nil {
			result.lookupErr = fmt.Errorf("inspect server image: %w", err)
			return result
		}
		if serverFound {
			break
		}
	}
	result.server = server
	if !serverFound {
		result.status = "missing"
		result.detail = "server image was not found"
		return result
	}
	agent, agentFound, err := InspectProvenance(ctx, s, "stgregistry.suse.com/rancher/rancher-agent:"+tag)
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
	if !IsRancherServerRepository(serverCanonicalRepository) || agentCanonicalRepository != "rancher/rancher-agent" {
		return fmt.Errorf("exact Rancher head image pair %s has unexpected canonical repositories: server %s, agent %s", expectedTag, serverCanonicalRepository, agentCanonicalRepository)
	}
	// Canonical labels must describe the image actually inspected, not another
	// server repository that happens to publish the same tag.
	if server.Reference != "" {
		_, requestedRepository, _, err := ParseRegistryImage(server.Reference)
		if err != nil || requestedRepository != serverCanonicalRepository {
			return fmt.Errorf("exact Rancher head image pair %s has a server canonical repository that differs from the requested image", expectedTag)
		}
	}
	if serverCanonicalTag != expectedTag || agentCanonicalTag != expectedTag {
		return fmt.Errorf("exact Rancher head image pair %s has mismatched canonical tags: server %s, agent %s", expectedTag, serverCanonicalTag, agentCanonicalTag)
	}
	return nil
}
