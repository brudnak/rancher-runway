package imagelookup

import (
	"fmt"
	"github.com/google/go-containerregistry/pkg/name"
	"sort"
	"strings"
	"time"
	"unicode"
)

func imageLookupNormalizeRegistry(input string) (string, error) {
	input = strings.TrimSpace(strings.ToLower(input))
	if input == "" || len(input) > 253 || strings.ContainsAny(input, "/@?#\\") || HasUnsafeCharacters(input) {
		return "", &InputError{Message: "registry host is invalid"}
	}
	if input == "index.docker.io" || input == "registry-1.docker.io" {
		input = "docker.io"
	}
	if strings.HasSuffix(input, ":") {
		return "", &InputError{Message: "registry host has an empty port"}
	}
	if _, err := name.NewRegistry(imageLookupRegistryForLibrary(input), name.StrictValidation); err != nil {
		return "", &InputError{Message: "registry host is invalid: " + err.Error()}
	}
	return input, nil
}

func imageLookupRegistryForLibrary(registry string) string {
	if registry == "docker.io" {
		return name.DefaultRegistry
	}
	return registry
}

func imageLookupRegistryForLibraryReference(reference string) string {
	if strings.HasPrefix(reference, "docker.io/") {
		return name.DefaultRegistry + strings.TrimPrefix(reference, "docker.io")
	}
	return reference
}

func RegistryForDisplay(registry string) string {
	registry = strings.ToLower(registry)
	if registry == name.DefaultRegistry || registry == "registry-1.docker.io" {
		return "docker.io"
	}
	return registry
}

func RegistryLabel(registry string) string {
	switch registry {
	case "docker.io":
		return "Docker Hub"
	case "stgregistry.suse.com":
		return "SUSE Staging"
	case "registry.rancher.com":
		return "Rancher Registry"
	case "registry.suse.com":
		return "SUSE Registry"
	default:
		return registry
	}
}

func imageLookupStripScheme(input string, allowHTTP bool) (string, error) {
	input = strings.TrimSpace(input)
	lower := strings.ToLower(input)
	for _, prefix := range []string{"docker://", "oci://", "https://"} {
		if strings.HasPrefix(lower, prefix) {
			return input[len(prefix):], nil
		}
	}
	if strings.HasPrefix(lower, "http://") {
		if !allowHTTP {
			return "", &InputError{Message: "plain HTTP registry references are not allowed"}
		}
		return input[len("http://"):], nil
	}
	if strings.Contains(lower, "://") {
		return "", &InputError{Message: "image reference scheme is not supported"}
	}
	return input, nil
}

func imageLookupFirstComponentIsRegistry(component string) bool {
	return strings.Contains(component, ".") || strings.Contains(component, ":") || strings.EqualFold(component, "localhost")
}

func HasUnsafeCharacters(value string) bool {
	for _, character := range value {
		if unicode.IsSpace(character) || unicode.IsControl(character) {
			return true
		}
	}
	return false
}

func LooksLikeReference(value string) bool {
	if !strings.Contains(value, "/") {
		return false
	}
	lastSlash := strings.LastIndex(value, "/")
	return strings.LastIndex(value, "@") > lastSlash || strings.LastIndex(value, ":") > lastSlash
}

func imageLookupSimpleToken(value string) bool {
	for _, character := range value {
		if !(character >= 'a' && character <= 'z') && !(character >= '0' && character <= '9') && character != '_' && character != '-' && character != '.' {
			return false
		}
	}
	return value != ""
}

func imageLookupChoice(input, fallback string, allowed ...string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(input))
	if value == "" {
		value = fallback
	}
	for _, candidate := range allowed {
		if value == candidate {
			return value, nil
		}
	}
	return "", &InputError{Message: fmt.Sprintf("unsupported filter value %q; expected one of %s", input, strings.Join(allowed, ", "))}
}

func imageLookupNormalizeVersionLine(input string) (string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return "", nil
	}
	match := imageLookupVersionLinePattern.FindStringSubmatch(value)
	if len(match) != 4 {
		return "", &InputError{Message: "versionLine must use X.Y or X.Y.Z format"}
	}
	result := match[1] + "." + match[2]
	if match[3] != "" {
		result += "." + match[3]
	}
	return result, nil
}

func imageLookupCommitPrefix(value string) bool {
	if len(value) < 7 || len(value) > 40 {
		return false
	}
	for _, character := range value {
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f') || (character >= 'A' && character <= 'F')) {
			return false
		}
	}
	return true
}

func imageLookupEnrichedSearchRequested(request SearchRequest) bool {
	nonDefault := func(value, defaultValue string) bool {
		value = strings.ToLower(strings.TrimSpace(value))
		return value != "" && value != defaultValue
	}
	return nonDefault(request.Channel, "all") ||
		nonDefault(request.Architecture, "all") ||
		nonDefault(request.PrimeHead, "all") ||
		nonDefault(request.HeadKind, "all") ||
		strings.TrimSpace(request.VersionLine) != "" ||
		strings.TrimSpace(request.Commit) != "" ||
		nonDefault(request.PairStatus, "all") ||
		nonDefault(request.SortBy, "natural") ||
		nonDefault(request.SortOrder, "desc") ||
		request.RecentDays > 0
}

func imageLookupRepositoryRole(repository string) (string, string) {
	repository = strings.Trim(strings.ToLower(strings.TrimSpace(repository)), "/")
	switch {
	case strings.HasSuffix(repository, "/rancher-agent"):
		return "agent", strings.TrimSuffix(repository, "/rancher-agent") + "/rancher"
	case strings.HasSuffix(repository, "/rancher-webhook"):
		return "webhook", ""
	case strings.HasSuffix(repository, "/rancher"):
		return "server", strings.TrimSuffix(repository, "/rancher") + "/rancher-agent"
	default:
		return "other", ""
	}
}

func imageLookupClassifyDigest(registry, repository, digest string) Tag {
	imageRole, _ := imageLookupRepositoryRole(repository)
	digest = strings.ToLower(strings.TrimSpace(digest))
	return Tag{
		Name:         digest,
		Reference:    registry + "/" + repository + "@" + digest,
		Channel:      "digest",
		Architecture: "unknown",
		ImageRole:    imageRole,
		Digest:       digest,
	}
}

func imageLookupClassifyTag(registry, repository, tagName string) Tag {
	architecture, baseTag := TagArchitecture(tagName)
	imageRole, companionRepository := imageLookupRepositoryRole(repository)
	result := Tag{
		Name:         tagName,
		Reference:    registry + "/" + repository + ":" + tagName,
		Channel:      imageLookupTagChannel(tagName),
		Architecture: architecture,
		BaseTag:      baseTag,
		ImageRole:    imageRole,
		Artifact:     imageLookupArtifactTag(tagName),
	}
	if companionRepository != "" {
		result.CompanionReference = registry + "/" + companionRepository + ":" + tagName
	}
	if imageRole != "server" && imageRole != "agent" {
		return result
	}

	normalized := strings.ToLower(NormalizeVersionInput(baseTag))
	if match := imageLookupTagVersionPattern.FindStringSubmatch(normalized); len(match) == 2 {
		result.Version = match[1]
		parts := strings.Split(result.Version, ".")
		if len(parts) >= 2 {
			result.VersionLine = parts[0] + "." + parts[1]
		}
	}
	switch {
	case IsPatchHeadAliasRancherVersion(normalized):
		result.IsPrimeHead = true
		result.HeadKind = "moving"
		result.Mutable = true
		result.Version = strings.TrimSuffix(normalized, "-head")
	case IsPrimeCommitHeadRancherVersion(normalized):
		result.IsPrimeHead = true
		result.HeadKind = "immutable"
		withoutHead := strings.TrimSuffix(normalized, "-head")
		separator := strings.LastIndex(withoutHead, "-")
		if separator > 0 {
			result.Version = withoutHead[:separator]
			result.Commit = withoutHead[separator+1:]
		}
	}
	if !result.IsPrimeHead {
		return result
	}
	result.Selector = "v" + result.Version + "-head"
	result.PairStatus = "unverified"
	return result
}

func imageLookupPrimeHeadAliasQuery(query string) bool {
	switch strings.ToLower(strings.TrimSpace(query)) {
	case "prime-head", "primehead", "prime_head":
		return true
	default:
		return false
	}
}

func imageLookupPrimeHeadQuery(query string) (string, string) {
	_, baseTag := TagArchitecture(strings.TrimSpace(query))
	normalized := strings.ToLower(NormalizeVersionInput(baseTag))
	switch {
	case IsPatchHeadAliasRancherVersion(normalized):
		return strings.TrimSuffix(normalized, "-head"), "moving"
	case IsPrimeCommitHeadRancherVersion(normalized):
		withoutHead := strings.TrimSuffix(normalized, "-head")
		separator := strings.LastIndex(withoutHead, "-")
		if separator > 0 {
			return withoutHead[:separator], "immutable"
		}
	}
	return "", ""
}

func imageLookupPatchHeadSelector(query string) bool {
	_, kind := imageLookupPrimeHeadQuery(query)
	return kind == "moving"
}

func imageLookupBarePatchVersion(query string) bool {
	match := imageLookupVersionLinePattern.FindStringSubmatch(strings.TrimSpace(query))
	return len(match) == 4 && match[3] != ""
}

func imageLookupPatchVersion(version string) bool {
	match := imageLookupVersionLinePattern.FindStringSubmatch(strings.TrimSpace(version))
	return len(match) == 4 && match[3] != ""
}

func imageLookupTagMatchesOptions(tag Tag, options searchOptions) bool {
	if options.movingHeadsOnly && !ReadinessHeadPattern.MatchString(tag.Name) {
		return false
	}
	query := strings.ToLower(strings.TrimSpace(options.query))
	if imageLookupPrimeHeadAliasQuery(query) {
		if !tag.IsPrimeHead {
			return false
		}
	} else if version, kind := imageLookupPrimeHeadQuery(query); kind == "moving" {
		if !tag.IsPrimeHead || tag.Version != version {
			return false
		}
	} else if !imageLookupTagMatches(tag.Name, tag.Channel, query) {
		return false
	}
	if options.channel != "" && options.channel != "all" && !imageLookupTagMatches(tag.Name, tag.Channel, options.channel) {
		return false
	}
	if options.architecture != "" && options.architecture != "all" && tag.Architecture != options.architecture {
		return false
	}
	switch options.primeHead {
	case "only":
		if !tag.IsPrimeHead {
			return false
		}
	case "exclude":
		if tag.IsPrimeHead {
			return false
		}
	}
	if options.headKind != "" && options.headKind != "all" && tag.HeadKind != options.headKind {
		return false
	}
	if options.versionLine != "" {
		if imageLookupPatchVersion(options.versionLine) {
			if tag.Version != options.versionLine {
				return false
			}
		} else if tag.VersionLine != options.versionLine {
			return false
		}
	}
	if options.commit != "" && !strings.HasPrefix(strings.ToLower(tag.Commit), options.commit) {
		return false
	}
	return true
}

// imageLookupFilterRecentTags applies an evidence-based cutoff. Unknown
// timestamps are excluded rather than guessed: OCI tag-list responses do not
// contain upload dates and do not guarantee chronological ordering.
func imageLookupFilterRecentTags(tags []Tag, cutoff time.Time) ([]Tag, int, int) {
	if cutoff.IsZero() {
		return tags, 0, 0
	}
	filtered := tags[:0]
	excluded, unknown := 0, 0
	for _, tag := range tags {
		observedAt, ok := imageLookupTagObservedAt(tag)
		switch {
		case !ok:
			unknown++
		case observedAt.Before(cutoff):
			excluded++
		default:
			filtered = append(filtered, tag)
		}
	}
	return filtered, excluded, unknown
}

func imageLookupTagObservedAt(tag Tag) (time.Time, bool) {
	for _, value := range []string{tag.PairCompletedAt, tag.UploadedAt, tag.CreatedAt} {
		if observedAt, err := time.Parse(time.RFC3339Nano, value); err == nil && !observedAt.IsZero() {
			return observedAt.UTC(), true
		}
	}
	return time.Time{}, false
}

func imageLookupApplyRecentFilter(group *SearchGroup, options searchOptions) {
	if options.recentCutoff.IsZero() {
		return
	}
	group.Tags, group.RecentExcludedCount, group.UnknownTimestampCount = imageLookupFilterRecentTags(group.Tags, options.recentCutoff)
	group.Matched = len(group.Tags)
}

func imageLookupSortTags(tags []Tag, sortBy, sortOrder string) {
	descending := sortOrder != "asc"
	sort.SliceStable(tags, func(i, j int) bool {
		comparison := imageLookupCompareTags(tags[i], tags[j], sortBy, descending)
		return comparison < 0
	})
}

func imageLookupCompareTags(left, right Tag, sortBy string, descending bool) int {
	compareNatural := func(leftValue, rightValue string) int {
		comparison := NaturalCompare(leftValue, rightValue)
		if descending {
			comparison = -comparison
		}
		return comparison
	}
	compareOptionalTime := func(leftValue, rightValue string) int {
		leftTime, leftErr := time.Parse(time.RFC3339Nano, leftValue)
		rightTime, rightErr := time.Parse(time.RFC3339Nano, rightValue)
		leftKnown, rightKnown := leftErr == nil, rightErr == nil
		if leftKnown != rightKnown {
			if leftKnown {
				return -1
			}
			return 1
		}
		if leftKnown && !leftTime.Equal(rightTime) {
			if leftTime.Before(rightTime) != descending {
				return -1
			}
			return 1
		}
		return 0
	}

	var comparison int
	switch sortBy {
	case "tag":
		leftName, rightName := strings.ToLower(left.Name), strings.ToLower(right.Name)
		if leftName < rightName {
			comparison = -1
		} else if leftName > rightName {
			comparison = 1
		}
		if descending {
			comparison = -comparison
		}
	case "version":
		if left.Version == "" || right.Version == "" {
			if left.Version != right.Version {
				if left.Version != "" {
					return -1
				}
				return 1
			}
		} else {
			comparison = compareNatural(left.Version, right.Version)
		}
	case "uploaded":
		comparison = compareOptionalTime(left.UploadedAt, right.UploadedAt)
	case "pair-completed":
		comparison = compareOptionalTime(left.PairCompletedAt, right.PairCompletedAt)
	default:
		comparison = compareNatural(left.Name, right.Name)
	}
	if comparison != 0 {
		return comparison
	}
	return compareNatural(left.Name, right.Name)
}

func imageLookupCountPrimeHeads(group *SearchGroup, tags []Tag) {
	group.PrimeHeadCount = 0
	group.MovingPrimeHeadCount = 0
	group.ImmutablePrimeHeadCount = 0
	for _, tag := range tags {
		if !tag.IsPrimeHead {
			continue
		}
		group.PrimeHeadCount++
		switch tag.HeadKind {
		case "moving":
			group.MovingPrimeHeadCount++
		case "immutable":
			group.ImmutablePrimeHeadCount++
		}
	}
}

func imageLookupCountPrimePairStatuses(group *SearchGroup, tags []Tag) {
	group.VerifiedPrimeHeadCount = 0
	group.InvalidPrimeHeadCount = 0
	group.MissingCompanionCount = 0
	for _, tag := range tags {
		switch tag.PairStatus {
		case "verified":
			group.VerifiedPrimeHeadCount++
		case "invalid":
			group.InvalidPrimeHeadCount++
		case "missing":
			group.MissingCompanionCount++
		}
	}
}
