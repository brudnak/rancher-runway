package imagelookup

import (
	"github.com/google/go-containerregistry/pkg/name"
	"strings"
)

func imageLookupInspectPrimeHead(parsed imageLookupReference, labels map[string]string) PrimeHead {
	requested := imageLookupClassifyTag(parsed.registry, parsed.repository, parsed.tag)
	result := PrimeHead{
		IsPrimeHead:        requested.IsPrimeHead,
		HeadKind:           requested.HeadKind,
		Mutable:            requested.Mutable,
		Version:            requested.Version,
		VersionLine:        requested.VersionLine,
		Commit:             requested.Commit,
		Selector:           requested.Selector,
		ImageRole:          requested.ImageRole,
		CompanionReference: requested.CompanionReference,
		Source:             SafeOCIProvenanceLabel(labels[SourceLabel]),
		Revision:           strings.ToLower(SafeOCIProvenanceLabel(labels[RevisionLabel])),
		OSSRevision:        strings.ToLower(SafeOCIProvenanceLabel(labels[OSSRevisionLabel])),
		CanonicalReference: SafeOCIProvenanceLabel(labels[CanonicalReferenceLabel]),
		Issues:             []string{},
	}
	result.PrimeSource = imageLookupRancherPrimeSource(result.Source)
	canonicalRepository, canonicalTag, canonicalOK := imageLookupCanonicalTag(result.CanonicalReference)
	result.CanonicalRepository = canonicalRepository
	result.CanonicalTag = canonicalTag
	canonical := imageLookupClassifyTag(parsed.registry, canonicalRepository, canonicalTag)

	minorAlias := imageLookupMinorHeadPattern.MatchString(strings.TrimSpace(parsed.tag))
	if !result.IsPrimeHead && result.PrimeSource && canonicalOK && canonical.IsPrimeHead && canonical.HeadKind == "immutable" &&
		(minorAlias || parsed.tag == "") {
		if minorAlias {
			match := imageLookupMinorHeadPattern.FindStringSubmatch(strings.TrimSpace(parsed.tag))
			if len(match) == 3 && canonical.VersionLine == match[1]+"."+match[2] {
				result.IsPrimeHead = true
				result.HeadKind = "moving"
				result.Mutable = true
			}
		} else {
			result.IsPrimeHead = true
			result.HeadKind = "immutable"
		}
		if result.IsPrimeHead {
			result.Version = canonical.Version
			result.VersionLine = canonical.VersionLine
			result.Commit = canonical.Commit
			result.Selector = canonical.Selector
			companionRepository := ""
			_, companionRepository = imageLookupRepositoryRole(parsed.repository)
			if companionRepository != "" {
				result.CompanionReference = parsed.registry + "/" + companionRepository + ":" + canonicalTag
			}
		}
	}
	if !result.IsPrimeHead {
		return result
	}
	expectedRepository := ""
	switch result.ImageRole {
	case "server":
		expectedRepository = "rancher/rancher"
		if !result.PrimeSource {
			result.Issues = append(result.Issues, "org.opencontainers.image.source is not the canonical Rancher Prime repository")
		}
	case "agent":
		expectedRepository = "rancher/rancher-agent"
	default:
		result.Issues = append(result.Issues, "Prime head classification is supported only for Rancher server and agent images")
	}
	if !canonicalOK {
		result.Issues = append(result.Issues, "org.opensuse.reference does not contain a valid canonical tag")
	} else {
		if expectedRepository != "" && canonicalRepository != expectedRepository {
			result.Issues = append(result.Issues, "org.opensuse.reference names an unexpected canonical repository")
		}
		result.CanonicalMatchesRequest = canonical.IsPrimeHead && canonical.HeadKind == "immutable" && canonical.Version == result.Version
		if requested.HeadKind == "immutable" {
			result.CanonicalMatchesRequest = result.CanonicalMatchesRequest && strings.EqualFold(canonicalTag, requested.Name)
		}
		if !result.CanonicalMatchesRequest {
			result.Issues = append(result.Issues, "org.opensuse.reference does not identify the expected immutable Prime head tag")
		}
	}
	if result.ImageRole == "server" {
		if GitRevisionPattern.MatchString(result.OSSRevision) && result.Commit != "" {
			result.CommitMatchesOSS = strings.HasPrefix(result.OSSRevision, strings.ToLower(result.Commit))
		}
		if !result.CommitMatchesOSS {
			result.Issues = append(result.Issues, "org.opencontainers.image.oss.revision does not match the tag commit")
		}
	}
	result.Consistent = len(result.Issues) == 0
	return result
}

func imageLookupCanonicalTag(reference string) (string, string, bool) {
	reference = strings.TrimSpace(reference)
	if reference == "" || HasUnsafeCharacters(reference) {
		return "", "", false
	}
	parsed, err := name.ParseReference(imageLookupRegistryForLibraryReference(reference), name.WeakValidation)
	if err != nil {
		return "", "", false
	}
	tag, ok := parsed.(name.Tag)
	if !ok {
		return "", "", false
	}
	return tag.Context().RepositoryStr(), tag.TagStr(), true
}

func imageLookupRancherPrimeSource(source string) bool {
	source = strings.ToLower(strings.TrimSpace(source))
	source = strings.TrimSuffix(source, "/")
	source = strings.TrimSuffix(source, ".git")
	return source == "https://github.com/rancher/rancher-prime"
}

func imageLookupTagChannel(tag string) string {
	lower := strings.ToLower(tag)
	switch {
	case strings.Contains(lower, "head"):
		return "head"
	case strings.Contains(lower, "alpha"):
		return "alpha"
	case strings.Contains(lower, "devel") || strings.Contains(lower, "dev") || strings.Contains(lower, "beta") || strings.Contains(lower, "master") || strings.Contains(lower, "main"):
		return "devel"
	case strings.Contains(lower, "-rcs-") || strings.Contains(lower, ".rcs."):
		return "rcs"
	case strings.Contains(lower, "-rc") || strings.Contains(lower, ".rc"):
		return "rc"
	default:
		return "stable"
	}
}

func imageLookupTagMatches(tag, channel, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	if imageLookupQuickQuery(query) {
		if query == "devel" {
			return channel == "devel" || channel == "alpha"
		}
		return channel == query
	}
	return strings.Contains(strings.ToLower(tag), query)
}

func imageLookupQuickQuery(query string) bool {
	switch strings.ToLower(strings.TrimSpace(query)) {
	case "head", "devel", "alpha", "rcs", "rc", "stable", "prime-head", "primehead", "prime_head":
		return true
	default:
		return false
	}
}

func TagArchitecture(tag string) (string, string) {
	lower := strings.ToLower(tag)
	for _, architecture := range []string{"amd64", "arm64", "s390x", "ppc64le", "386", "arm"} {
		for _, suffix := range []string{"-linux-" + architecture, "-" + architecture, "_" + architecture, "." + architecture} {
			if strings.HasSuffix(lower, suffix) {
				return architecture, tag[:len(tag)-len(suffix)]
			}
		}
	}
	return "multi", tag
}

func imageLookupArtifactTag(tag string) bool {
	lower := strings.ToLower(tag)
	if strings.HasSuffix(lower, ".sig") || strings.HasSuffix(lower, ".att") || strings.HasSuffix(lower, ".sbom") || strings.HasSuffix(lower, ".attestation") {
		return true
	}
	if strings.HasPrefix(lower, "sha256-") && (strings.Contains(lower, ".sig") || strings.Contains(lower, ".att") || strings.Contains(lower, ".sbom")) {
		return true
	}
	return strings.Contains(lower, "cosign") && (strings.Contains(lower, "signature") || strings.Contains(lower, "attestation"))
}

func NaturalCompare(left, right string) int {
	left = strings.ToLower(left)
	right = strings.ToLower(right)
	for len(left) > 0 && len(right) > 0 {
		leftDigit := left[0] >= '0' && left[0] <= '9'
		rightDigit := right[0] >= '0' && right[0] <= '9'
		leftEnd := imageLookupTokenEnd(left, leftDigit)
		rightEnd := imageLookupTokenEnd(right, rightDigit)
		leftToken, rightToken := left[:leftEnd], right[:rightEnd]
		if leftDigit && rightDigit {
			leftNumber := strings.TrimLeft(leftToken, "0")
			rightNumber := strings.TrimLeft(rightToken, "0")
			if leftNumber == "" {
				leftNumber = "0"
			}
			if rightNumber == "" {
				rightNumber = "0"
			}
			if len(leftNumber) != len(rightNumber) {
				if len(leftNumber) > len(rightNumber) {
					return 1
				}
				return -1
			}
			if leftNumber != rightNumber {
				if leftNumber > rightNumber {
					return 1
				}
				return -1
			}
			if len(leftToken) != len(rightToken) {
				if len(leftToken) < len(rightToken) {
					return 1
				}
				return -1
			}
		} else if leftToken != rightToken {
			if leftToken > rightToken {
				return 1
			}
			return -1
		}
		left, right = left[leftEnd:], right[rightEnd:]
	}
	if len(left) == len(right) {
		return 0
	}
	if len(left) > 0 {
		return 1
	}
	return -1
}

func imageLookupTokenEnd(value string, digits bool) int {
	index := 0
	for index < len(value) {
		isDigit := value[index] >= '0' && value[index] <= '9'
		if isDigit != digits {
			break
		}
		index++
	}
	return index
}
