package test

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	version "github.com/hashicorp/go-version"
)

// Community minor-head builds omit a patch number. Never manufacture .0 from
// this identity: it would falsely classify a release-to-head upgrade as a downgrade.
var minorHeadVersion = regexp.MustCompile(`^v?(\d+)\.(\d+)-[a-fA-F0-9]{7,40}-head$`)
var exactUpgradeVersion = regexp.MustCompile(`^v?\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.+-]+)?$`)

func isExactUpgradeVersion(value string) bool {
	if minorHeadVersion.MatchString(value) {
		return true
	}
	if !exactUpgradeVersion.MatchString(value) {
		return false
	}
	_, err := version.NewVersion(value)
	return err == nil
}

func resolveUpgradeImageVersion(image imagelookup.Provenance, tag string) (string, string, error) {
	if image.ServerVersion != "" {
		if !isExactUpgradeVersion(image.ServerVersion) {
			return "", "", fmt.Errorf("image CATTLE_SERVER_VERSION %q is not an exact Rancher build version; choose another image", image.ServerVersion)
		}
		return image.ServerVersion, "CATTLE_SERVER_VERSION in image configuration", nil
	}
	if isExactUpgradeVersion(image.BuildVersion) {
		return image.BuildVersion, "OCI image version label", nil
	}
	// Canonical references provide exact build identities for images that don't
	// carry the runtime environment variable. Branch labels and moving aliases
	// are deliberately excluded from this fallback.
	if _, repository, canonicalTag, err := imagelookup.ParseRegistryImage(image.CanonicalReference); err == nil && strings.HasSuffix(repository, "/rancher") && isExactUpgradeVersion(canonicalTag) {
		return canonicalTag, "Canonical image reference", nil
	}
	if isExactUpgradeVersion(tag) {
		return tag, "Explicit image tag (runtime version checked after rollout)", nil
	}
	return "", "", fmt.Errorf("image metadata does not provide an exact Rancher build version (version label %q); choose a commit-qualified image or one with CATTLE_SERVER_VERSION", image.BuildVersion)
}

func validateMinorHeadUpgradeStep(from, to string, experimental bool) error {
	if !experimental {
		return fmt.Errorf("commit-based head transitions require experimental mode")
	}
	if !isExactUpgradeVersion(from) || !isExactUpgradeVersion(to) {
		return fmt.Errorf("both endpoints must identify exact Rancher releases or commit-based head builds")
	}
	// go-version supplies the release-line segments; its implied patch zero is
	// not used. Only the known major/minor relationship is asserted here.
	a, err := version.NewVersion(from)
	if err != nil {
		return fmt.Errorf("cannot classify running Rancher version %q", from)
	}
	b, err := version.NewVersion(to)
	if err != nil {
		return fmt.Errorf("cannot classify target Rancher version %q", to)
	}
	as, bs := a.Segments(), b.Segments()
	if as[0] != bs[0] || bs[1] < as[1] || bs[1] > as[1]+1 {
		return fmt.Errorf("choose the current or next minor release; upgrade one minor at a time")
	}
	if from == to {
		return fmt.Errorf("this exact head build is already installed")
	}
	return nil
}
