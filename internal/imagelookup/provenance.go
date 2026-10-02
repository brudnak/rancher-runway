package imagelookup

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"
)

const (
	VersionLabel            = "org.opencontainers.image.version"
	CanonicalReferenceLabel = "org.opensuse.reference"
)

type Provenance struct {
	Reference          string
	Digest             string
	CreatedAt          time.Time
	ServerVersion      string
	BuildVersion       string
	SourceURL          string
	Revision           string
	OSSRevision        string
	CanonicalReference string
}

func InspectProvenance(ctx context.Context, service *Service, reference string) (Provenance, bool, error) {
	platform := "linux/amd64"
	if parsed, parseErr := service.parseReference(reference, true); parseErr == nil && parsed.tag != "" {
		if architecture, _ := TagArchitecture(parsed.tag); architecture != "" && architecture != "multi" && architecture != "unknown" {
			platform = "linux/" + architecture
		}
	}
	response, err := service.Inspect(ctx, InspectRequest{
		Reference:        reference,
		Platform:         platform,
		IncludeBuildYAML: false,
		SkipTagMetadata:  true,
	})
	if err != nil {
		if RegistryNotFound(err) {
			return Provenance{Reference: reference}, false, nil
		}
		return Provenance{Reference: reference}, false, fmt.Errorf("%s", SafeError(err))
	}

	serverVersion := ""
	for _, entry := range response.Config.Env {
		if value, ok := strings.CutPrefix(entry, "CATTLE_SERVER_VERSION="); ok {
			serverVersion = SafeOCIProvenanceLabel(value)
		}
	}
	labels := response.Config.Labels
	createdAt, _ := time.Parse(time.RFC3339Nano, response.CreatedAt)
	return Provenance{
		Reference:          response.Reference,
		Digest:             response.Digest,
		CreatedAt:          createdAt,
		ServerVersion:      serverVersion,
		BuildVersion:       SafeOCIProvenanceLabel(labels[VersionLabel]),
		SourceURL:          SafeOCIProvenanceLabel(labels[SourceLabel]),
		Revision:           SafeOCIProvenanceLabel(labels[RevisionLabel]),
		OSSRevision:        SafeOCIProvenanceLabel(labels[OSSRevisionLabel]),
		CanonicalReference: SafeOCIProvenanceLabel(labels[CanonicalReferenceLabel]),
	}, true, nil
}

func ValidatePatchHeadServerProvenance(version string, server Provenance) error {
	normalizedVersion := NormalizeVersionInput(version)
	if !IsPrimeCommitHeadRancherVersion(normalizedVersion) {
		return fmt.Errorf("%s is not an immutable patch-qualified Rancher head", version)
	}
	components := strings.Split(normalizedVersion, "-")
	expectedRevision := strings.ToLower(components[len(components)-2])
	source := strings.TrimSpace(server.SourceURL)
	if source != "https://github.com/rancher/rancher-prime" && source != "https://github.com/rancher/rancher-prime.git" {
		return fmt.Errorf("Rancher head image %s did not declare the canonical Rancher Prime source", NormalizeDockerRancherTag(normalizedVersion))
	}
	ossRevision := strings.ToLower(strings.TrimSpace(server.OSSRevision))
	if !GitRevisionPattern.MatchString(ossRevision) || !strings.HasPrefix(ossRevision, expectedRevision) {
		return fmt.Errorf("Rancher head image %s identifies commit %s, but its public OSS revision is %s", NormalizeDockerRancherTag(normalizedVersion), expectedRevision, ossRevision)
	}
	return nil
}

func SafeOCIProvenanceLabel(value string) string {
	value = strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return ' '
		}
		return character
	}, value)
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	runes := []rune(value)
	if len(runes) > 512 {
		value = string(runes[:512])
	}
	return value
}
