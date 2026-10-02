package imagelookup

import (
	"fmt"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"net/url"
	"strings"
)

func (s *Service) searchTargets(request SearchRequest) ([]searchTarget, string, int, error) {
	targets, options, err := s.searchParameters(request)
	if err != nil {
		return nil, "", 0, err
	}
	return targets, options.query, options.limit, nil
}

func (s *Service) searchParameters(request SearchRequest) ([]searchTarget, searchOptions, error) {
	options := searchOptions{
		includeArtifacts: request.IncludeArtifacts,
		channel:          "all",
		architecture:     "all",
		primeHead:        "all",
		headKind:         "all",
		pairStatus:       "all",
		sortBy:           "natural",
		sortOrder:        "desc",
		scanMode:         "auto",
	}
	registryValue := strings.TrimSpace(request.Registry)
	if registryValue == "" {
		registryValue = "all"
	}
	query := strings.TrimSpace(request.Query)
	if len(query) > 256 {
		return nil, searchOptions{}, &InputError{Message: "query must be 256 characters or fewer"}
	}
	if HasUnsafeCharacters(query) {
		return nil, searchOptions{}, &InputError{Message: "query contains whitespace or control characters"}
	}
	limit := request.Limit
	if limit == 0 {
		limit = imageLookupDefaultResultLimit
	}
	if limit < 1 || limit > imageLookupMaxResultLimit {
		return nil, searchOptions{}, &InputError{Message: fmt.Sprintf("limit must be between 1 and %d", imageLookupMaxResultLimit)}
	}
	options.limit = limit
	if request.RecentDays < 0 || request.RecentDays > imageLookupMaxRecentDays {
		return nil, searchOptions{}, &InputError{Message: fmt.Sprintf("recentDays must be 0 (all dates) or between 1 and %d", imageLookupMaxRecentDays)}
	}
	options.recentDays = request.RecentDays

	repositoryValue := strings.TrimSpace(request.Repository)
	var explicitRegistry string
	bareDigestQuery := imageLookupDigestPattern.MatchString(strings.ToLower(query))
	if !bareDigestQuery && LooksLikeReference(query) {
		parsed, err := s.parseReference(query, true)
		if err != nil {
			return nil, searchOptions{}, err
		}
		options.exactLookup = true
		explicitRegistry = parsed.registry
		repositoryValue = parsed.repository
		if parsed.tag != "" {
			query = parsed.tag
		} else {
			query = parsed.digest
		}
	}

	if repositoryValue == "" || strings.EqualFold(repositoryValue, "all") {
		repositoryValue = "all"
	} else {
		parsedRegistry, repository, selector, err := s.parseSearchRepository(repositoryValue)
		if err != nil {
			return nil, searchOptions{}, err
		}
		if parsedRegistry != "" {
			explicitRegistry = parsedRegistry
		}
		repositoryValue = repository
		if selector != "" && query == "" {
			query = selector
			options.exactLookup = true
		}
	}

	var err error
	if options.channel, err = imageLookupChoice(request.Channel, "all", "all", "head", "devel", "alpha", "rcs", "rc", "stable"); err != nil {
		return nil, searchOptions{}, err
	}
	if options.architecture, err = imageLookupChoice(request.Architecture, "all", "all", "multi", "amd64", "arm64", "s390x", "ppc64le", "386", "arm"); err != nil {
		return nil, searchOptions{}, err
	}
	if options.primeHead, err = imageLookupChoice(request.PrimeHead, "all", "all", "only", "exclude"); err != nil {
		return nil, searchOptions{}, err
	}
	if options.headKind, err = imageLookupChoice(request.HeadKind, "all", "all", "moving", "immutable"); err != nil {
		return nil, searchOptions{}, err
	}
	if options.pairStatus, err = imageLookupChoice(request.PairStatus, "all", "all", "verified", "unverified", "missing", "invalid"); err != nil {
		return nil, searchOptions{}, err
	}
	if options.sortBy, err = imageLookupChoice(request.SortBy, "natural", "natural", "tag", "version", "uploaded", "pair-completed"); err != nil {
		return nil, searchOptions{}, err
	}
	if options.sortOrder, err = imageLookupChoice(request.SortOrder, "desc", "asc", "desc"); err != nil {
		return nil, searchOptions{}, err
	}
	if options.scanMode, err = imageLookupChoice(request.ScanMode, "auto", "auto", "bounded", "complete"); err != nil {
		return nil, searchOptions{}, err
	}
	if options.versionLine, err = imageLookupNormalizeVersionLine(request.VersionLine); err != nil {
		return nil, searchOptions{}, err
	}
	options.commit = strings.ToLower(strings.TrimSpace(request.Commit))
	if options.commit != "" && !imageLookupCommitPrefix(options.commit) {
		return nil, searchOptions{}, &InputError{Message: "commit must be a 7 to 40 character hexadecimal Git revision prefix"}
	}
	if options.primeHead == "exclude" && options.headKind != "all" {
		return nil, searchOptions{}, &InputError{Message: "headKind cannot be combined with primeHead=exclude"}
	}

	query = strings.TrimSpace(query)
	primeQueryVersion, primeQueryKind := imageLookupPrimeHeadQuery(query)
	primeAliasQuery := imageLookupPrimeHeadAliasQuery(query)
	if primeAliasQuery {
		options.primeHead = "only"
		query = "prime-head"
	}
	if primeQueryKind != "" {
		options.primeHead = "only"
		options.primeVersion = primeQueryVersion
		options.verifyPrimePairs = true
		if primeQueryKind == "moving" && strings.TrimSpace(request.SortBy) == "" {
			options.sortBy = "pair-completed"
		}
	}
	if options.headKind != "all" {
		options.primeHead = "only"
	}
	if options.primeHead == "only" && imageLookupPatchVersion(options.versionLine) {
		options.primeVersion = options.versionLine
		options.verifyPrimePairs = true
	}
	if options.primeHead == "only" && imageLookupBarePatchVersion(query) {
		options.primeVersion, err = imageLookupNormalizeVersionLine(query)
		if err != nil {
			return nil, searchOptions{}, err
		}
		options.verifyPrimePairs = true
		if strings.TrimSpace(request.SortBy) == "" {
			options.sortBy = "pair-completed"
		}
	}
	if options.pairStatus != "all" || options.sortBy == "pair-completed" {
		if !options.verifyPrimePairs {
			return nil, searchOptions{}, &InputError{Message: "pairStatus and pair-completed sorting require an exact Prime head tag or patch-qualified Prime selector"}
		}
	}
	options.query = query
	autoFullScan := imageLookupEnrichedSearchRequested(request) || primeAliasQuery || primeQueryKind == "moving" || imageLookupBarePatchVersion(query)
	switch options.scanMode {
	case "bounded":
		// Date filtering and pair verification require a complete candidate set.
		// Other filters and sorts may operate on the bounded sample; Truncated
		// tells clients that the result is not a global ordering.
		options.fullScan = options.recentDays > 0 || options.verifyPrimePairs
	case "complete":
		options.fullScan = true
	default:
		options.fullScan = autoFullScan
	}

	registries := []string{}
	if explicitRegistry != "" {
		registries = []string{explicitRegistry}
	} else if strings.EqualFold(registryValue, "all") {
		if options.primeHead == "only" || primeQueryKind != "" {
			registries = []string{"stgregistry.suse.com"}
		} else {
			registries = append(registries, imageLookupKnownRegistries...)
		}
	} else {
		registry, err := imageLookupNormalizeRegistry(registryValue)
		if err != nil {
			return nil, searchOptions{}, err
		}
		registries = []string{registry}
	}

	repositories := []string{repositoryValue}
	if repositoryValue == "all" {
		if options.primeHead == "only" || primeQueryKind != "" {
			repositories = []string{"rancher/rancher", "rancher/rancher-agent"}
		} else {
			repositories = append([]string(nil), imageLookupKnownRepositories...)
		}
	}
	targets := make([]searchTarget, 0, len(registries)*len(repositories))
	for _, registry := range registries {
		for _, repository := range repositories {
			if _, err := s.parseRepository(registry, repository); err != nil {
				return nil, searchOptions{}, err
			}
			targets = append(targets, searchTarget{registry: registry, repository: repository})
		}
	}
	if options.primeHead == "only" || primeQueryKind != "" {
		for _, target := range targets {
			if target.registry != "stgregistry.suse.com" {
				return nil, searchOptions{}, &InputError{Message: "Prime-head image search is supported only in stgregistry.suse.com"}
			}
			if target.repository != "rancher/rancher" && target.repository != "rancher/rancher-agent" {
				return nil, searchOptions{}, &InputError{Message: "Prime-head image search requires the canonical rancher/rancher or rancher/rancher-agent repository"}
			}
		}
	}
	if (options.pairStatus != "all" || options.sortBy == "pair-completed") && !options.verifyPrimePairs {
		return nil, searchOptions{}, &InputError{Message: "pair filtering and pair-completed sorting are supported only for SUSE staging Prime head lookups"}
	}
	return targets, options, nil
}

func (s *Service) parseSearchRepository(input string) (string, string, string, error) {
	if len(input) > 512 {
		return "", "", "", &InputError{Message: "repository must be 512 characters or fewer"}
	}
	cleaned, err := imageLookupStripScheme(input, s.allowHTTP)
	if err != nil {
		return "", "", "", err
	}
	if HasUnsafeCharacters(cleaned) || strings.ContainsAny(cleaned, "?#\\") {
		return "", "", "", &InputError{Message: "repository contains invalid characters"}
	}

	selector := ""
	repositoryPart := cleaned
	if at := strings.LastIndex(repositoryPart, "@"); at >= 0 {
		selector = repositoryPart[at+1:]
		repositoryPart = repositoryPart[:at]
	} else if colon := strings.LastIndex(repositoryPart, ":"); colon > strings.LastIndex(repositoryPart, "/") {
		selector = repositoryPart[colon+1:]
		repositoryPart = repositoryPart[:colon]
	}
	parts := strings.Split(repositoryPart, "/")
	if len(parts) == 0 {
		return "", "", "", &InputError{Message: "repository is required"}
	}
	explicitRegistry := ""
	if imageLookupFirstComponentIsRegistry(parts[0]) {
		explicitRegistry, err = imageLookupNormalizeRegistry(parts[0])
		if err != nil {
			return "", "", "", err
		}
		parts = parts[1:]
	}
	repository := strings.Join(parts, "/")
	if repository == "" {
		return "", "", "", &InputError{Message: "repository path is required"}
	}
	if len(parts) == 1 && (explicitRegistry == "" || explicitRegistry == "docker.io") {
		repository = "library/" + repository
	}
	validationRegistry := explicitRegistry
	if validationRegistry == "" {
		validationRegistry = "docker.io"
	}
	if _, parseErr := s.parseRepository(validationRegistry, repository); parseErr != nil {
		return "", "", "", parseErr
	}
	if selector != "" {
		if _, parseErr := s.parseReference(validationRegistry+"/"+repository+":"+selector, true); parseErr != nil {
			if _, digestErr := s.parseReference(validationRegistry+"/"+repository+"@"+selector, true); digestErr != nil {
				return "", "", "", &InputError{Message: "repository tag or digest is invalid"}
			}
		}
	}
	return explicitRegistry, repository, selector, nil
}

func (s *Service) parseRepository(registry, repository string) (name.Repository, error) {
	registry, err := imageLookupNormalizeRegistry(registry)
	if err != nil {
		return name.Repository{}, err
	}
	repository = strings.Trim(strings.TrimSpace(repository), "/")
	if repository == "" || len(repository) > 255 || strings.Contains(repository, "..") || HasUnsafeCharacters(repository) || strings.ContainsAny(repository, "@:#?\\") {
		return name.Repository{}, &InputError{Message: "repository path is invalid"}
	}
	options := []name.Option{name.StrictValidation}
	if s.allowHTTP {
		options = append(options, name.Insecure)
	}
	parsed, parseErr := name.NewRepository(imageLookupRegistryForLibrary(registry)+"/"+repository, options...)
	if parseErr != nil {
		return name.Repository{}, &InputError{Message: "repository path is invalid: " + parseErr.Error()}
	}
	return parsed, nil
}

func (s *Service) parseReference(input string, requireSelector bool) (imageLookupReference, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return imageLookupReference{}, &InputError{Message: "image reference is required"}
	}
	if len(input) > 1024 {
		return imageLookupReference{}, &InputError{Message: "image reference must be 1024 characters or fewer"}
	}
	cleaned, err := imageLookupStripScheme(input, s.allowHTTP)
	if err != nil {
		return imageLookupReference{}, err
	}
	if HasUnsafeCharacters(cleaned) || strings.ContainsAny(cleaned, "?#\\") {
		return imageLookupReference{}, &InputError{Message: "image reference contains invalid characters"}
	}
	for _, component := range strings.Split(cleaned, "/") {
		if component == "." || component == ".." {
			return imageLookupReference{}, &InputError{Message: "image reference may not contain path traversal"}
		}
	}

	if !strings.Contains(cleaned, "/") {
		cleaned = "docker.io/library/" + cleaned
	} else {
		first := strings.SplitN(cleaned, "/", 2)[0]
		if !imageLookupFirstComponentIsRegistry(first) {
			cleaned = "docker.io/" + cleaned
		}
	}
	parts := strings.SplitN(cleaned, "/", 2)
	normalizedRegistry, registryErr := imageLookupNormalizeRegistry(parts[0])
	if registryErr != nil {
		return imageLookupReference{}, registryErr
	}
	cleaned = normalizedRegistry + "/" + parts[1]
	lastSlash := strings.LastIndex(cleaned, "/")
	hasDigest := strings.LastIndex(cleaned, "@") > lastSlash
	hasTag := strings.LastIndex(cleaned, ":") > lastSlash
	if requireSelector && !hasDigest && !hasTag {
		return imageLookupReference{}, &InputError{Message: "image reference must include a tag or digest"}
	}
	options := []name.Option{name.StrictValidation}
	if s.allowHTTP {
		options = append(options, name.Insecure)
	}
	parsed, parseErr := name.ParseReference(imageLookupRegistryForLibraryReference(cleaned), options...)
	if parseErr != nil {
		return imageLookupReference{}, &InputError{Message: "image reference is invalid: " + parseErr.Error()}
	}
	registry := RegistryForDisplay(parsed.Context().RegistryStr())
	repository := parsed.Context().RepositoryStr()
	result := imageLookupReference{parsed: parsed, registry: registry, repository: repository}
	switch typed := parsed.(type) {
	case name.Tag:
		result.tag = typed.TagStr()
		result.canonical = registry + "/" + repository + ":" + typed.TagStr()
	case name.Digest:
		result.digest = typed.DigestStr()
		result.canonical = registry + "/" + repository + "@" + typed.DigestStr()
	default:
		return imageLookupReference{}, &InputError{Message: "image reference must include a tag or digest"}
	}
	return result, nil
}

func imageLookupParsePlatform(input string) (*v1.Platform, error) {
	platformText := strings.TrimSpace(input)
	if platformText == "" {
		platformText = "linux/amd64"
	}
	if len(platformText) > 64 {
		return nil, &InputError{Message: "platform is too long"}
	}
	platform, err := v1.ParsePlatform(platformText)
	if err != nil || platform.OS == "" || platform.Architecture == "" {
		return nil, &InputError{Message: "platform must use os/architecture or os/architecture/variant format"}
	}
	if !imageLookupSimpleToken(platform.OS) || !imageLookupSimpleToken(platform.Architecture) || (platform.Variant != "" && !imageLookupSimpleToken(platform.Variant)) {
		return nil, &InputError{Message: "platform contains invalid characters"}
	}
	return platform, nil
}

func ParseGitHubSource(source, revision string) (string, string, error) {
	if source == "" {
		return "", "", &imageLookupSourceMetadataError{message: "image does not declare org.opencontainers.image.source"}
	}
	if revision == "" {
		return "", "", &imageLookupSourceMetadataError{message: "image does not declare org.opencontainers.image.revision"}
	}
	if !GitRevisionPattern.MatchString(revision) {
		return "", "", &imageLookupSourceMetadataError{message: "image revision label is not a full 40-character Git commit SHA"}
	}
	metadataError := func() error {
		return &imageLookupSourceMetadataError{message: "image source label must be an exact https://github.com/{owner}/{repository} URL, optionally ending in .git"}
	}
	parsed, err := url.Parse(source)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" {
		return "", "", metadataError()
	}
	components := strings.Split(strings.TrimPrefix(parsed.Path, "/"), "/")
	if len(components) != 2 || components[0] == "" || components[1] == "" {
		return "", "", metadataError()
	}

	// OCI source labels commonly use the clone URL form ending in lowercase
	// ".git". Accept exactly one such suffix, but keep the repository name
	// canonical for GitHub API requests and provenance.
	repository := components[1]
	if strings.HasSuffix(repository, ".git") {
		repository = strings.TrimSuffix(repository, ".git")
		if repository == "" || strings.HasSuffix(repository, ".git") {
			return "", "", metadataError()
		}
	} else if len(repository) >= len(".git") && strings.EqualFold(repository[len(repository)-len(".git"):], ".git") {
		// The optional clone suffix is intentionally case-sensitive.
		return "", "", metadataError()
	}
	if !GitHubPathComponent(components[0]) || !GitHubPathComponent(repository) {
		return "", "", metadataError()
	}
	canonical := "https://github.com/" + components[0] + "/" + repository
	if source != canonical && source != canonical+".git" {
		return "", "", metadataError()
	}
	return components[0], repository, nil
}

func GitHubPathComponent(value string) bool {
	if len(value) == 0 || len(value) > 100 || value == "." || value == ".." {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '-' || character == '_' || character == '.' {
			continue
		}
		return false
	}
	return true
}
