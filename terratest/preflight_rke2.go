package test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/terratest/settings"
	goversion "github.com/hashicorp/go-version"
	"github.com/spf13/viper"
	"io"
	"log"
	"net/http"
	"strings"
)

func getRKE2InstallScriptURL(rke2Version, expectedInstallerSHA256 string) (string, string, error) {
	if rke2Version == "" {
		return "", "", fmt.Errorf("k8s.version must be set")
	}
	if expectedInstallerSHA256 == "" {
		return "", "", fmt.Errorf("rke2.install_script_sha256 must be set")
	}

	installScriptURL := fmt.Sprintf("https://raw.githubusercontent.com/rancher/rke2/%s/install.sh", rke2Version)
	return installScriptURL, expectedInstallerSHA256, nil
}

func validatePinnedRKE2InstallerChecksum(plans []*RancherResolvedPlan) error {
	log.Printf("[preflight] Validating pinned RKE2 installer checksum before provisioning...")

	if len(plans) == 0 {
		installScriptURL, expectedInstallerSHA256, err := getRKE2InstallScriptURL(
			viper.GetString("k8s.version"),
			viper.GetString("rke2.install_script_sha256"),
		)
		if err != nil {
			return err
		}
		if err := validateSinglePinnedRKE2InstallerChecksum(installScriptURL, expectedInstallerSHA256); err != nil {
			return err
		}
		log.Printf("[preflight] RKE2 installer checksum validated successfully")
		return nil
	}

	seen := map[string]bool{}
	for _, plan := range plans {
		if plan == nil {
			continue
		}

		installScriptURL, expectedInstallerSHA256, err := getRKE2InstallScriptURL(plan.RecommendedRKE2Version, plan.InstallerSHA256)
		if err != nil {
			return err
		}

		dedupKey := installScriptURL + "|" + strings.ToLower(expectedInstallerSHA256)
		if seen[dedupKey] {
			continue
		}
		seen[dedupKey] = true

		if err := validateSinglePinnedRKE2InstallerChecksum(installScriptURL, expectedInstallerSHA256); err != nil {
			return err
		}
	}

	log.Printf("[preflight] RKE2 installer checksum validated successfully")
	return nil
}

func validateSinglePinnedRKE2InstallerChecksum(installScriptURL, expectedInstallerSHA256 string) error {
	resp, err := http.Get(installScriptURL)
	if err != nil {
		return fmt.Errorf("failed to download installer from %s: %w", installScriptURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected HTTP status %d downloading %s", resp.StatusCode, installScriptURL)
	}

	hasher := sha256.New()
	if _, err := io.Copy(hasher, resp.Body); err != nil {
		return fmt.Errorf("failed to hash installer from %s: %w", installScriptURL, err)
	}

	actualInstallerSHA256 := hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(actualInstallerSHA256, expectedInstallerSHA256) {
		return fmt.Errorf("installer checksum mismatch for %s: expected %s, got %s", installScriptURL, expectedInstallerSHA256, actualInstallerSHA256)
	}

	return nil
}

func isRKE2InstallerChecksumFailure(stdout, stderr string) bool {
	combinedOutput := stdout + "\n" + stderr
	return strings.Contains(combinedOutput, "SECURITY ERROR: RKE2 installer checksum validation failed")
}

func parseRKE2CompatibilityVersion(raw string) (*goversion.Version, error) {
	normalized, err := normalizeRKE2VersionInput(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid RKE2 version for ingress compatibility: %w", err)
	}
	parsed, err := goversion.NewVersion(strings.TrimPrefix(normalized, "v"))
	if err != nil {
		return nil, fmt.Errorf("invalid RKE2 version for ingress compatibility %q: %w", raw, err)
	}
	return parsed, nil
}

func validateRKE2IngressControllerVersion(rke2Version, ingressController string) error {
	parsed, err := parseRKE2CompatibilityVersion(rke2Version)
	if err != nil {
		return err
	}

	switch strings.ToLower(strings.TrimSpace(ingressController)) {
	case settings.RKE2IngressControllerTraefik:
		if parsed.LessThan(rke2TraefikMinimumVersion) {
			return fmt.Errorf("rke2.ingress_controller=traefik requires RKE2 v1.30.3+rke2r1 or newer; use ingress-nginx for %s", rke2Version)
		}
		return nil
	case settings.RKE2IngressControllerNginx:
		if parsed.GreaterThanOrEqual(rke2NginxRemovalVersion) {
			return fmt.Errorf("rke2.ingress_controller=ingress-nginx is not available in community RKE2 %s; use traefik", rke2Version)
		}
		return nil
	default:
		return fmt.Errorf("unsupported RKE2 ingress controller %q", ingressController)
	}
}

func validateResolvedRKE2IngressControllerVersions(plans []*RancherResolvedPlan, ingressController string) error {
	if len(plans) == 0 {
		return validateRKE2IngressControllerVersion(viper.GetString("k8s.version"), ingressController)
	}
	for i, plan := range plans {
		if plan == nil {
			return fmt.Errorf("resolved Rancher plan %d is missing", i+1)
		}
		if err := validateRKE2IngressControllerVersion(plan.RecommendedRKE2Version, ingressController); err != nil {
			return fmt.Errorf("resolved Rancher plan %d: %w", i+1, err)
		}
	}
	return nil
}

func rke2ImageArchiveNames(rke2Version, ingressController string) ([]string, error) {
	if err := validateRKE2IngressControllerVersion(rke2Version, ingressController); err != nil {
		return nil, err
	}
	parsed, err := parseRKE2CompatibilityVersion(rke2Version)
	if err != nil {
		return nil, err
	}

	archives := []string{"rke2-images.linux-amd64.tar.zst"}
	switch strings.ToLower(strings.TrimSpace(ingressController)) {
	case settings.RKE2IngressControllerTraefik:
		if parsed.LessThan(rke2TraefikDefaultVersion) {
			archives = append(archives, "rke2-images-traefik.linux-amd64.tar.zst")
		}
	case settings.RKE2IngressControllerNginx:
		if parsed.GreaterThanOrEqual(rke2TraefikDefaultVersion) {
			archives = append(archives, "rke2-images-ingress-nginx.linux-amd64.tar.zst")
		}
	}
	return archives, nil
}

func buildRKE2ImagesDownloadCommand(rke2Version, ingressController string) (string, error) {
	archives, err := rke2ImageArchiveNames(rke2Version, ingressController)
	if err != nil {
		return "", err
	}
	checksumURL := fmt.Sprintf("https://github.com/rancher/rke2/releases/download/%s/sha256sum-amd64.txt", rke2Version)
	temporaryPaths := make([]string, 0, len(archives))
	selectedChecksumPath := "/tmp/rke2-selected-sha256sum.txt"
	var command strings.Builder

	for _, archive := range archives {
		temporaryPaths = append(temporaryPaths, shellSingleQuote("/tmp/"+archive))
	}
	fmt.Fprintf(&command, "rm -f %s /tmp/rke2-sha256sum-amd64.txt %s\n", strings.Join(temporaryPaths, " "), shellSingleQuote(selectedChecksumPath))
	for _, archive := range archives {
		archivePath := "/tmp/" + archive
		imagesURL := fmt.Sprintf("https://github.com/rancher/rke2/releases/download/%s/%s", rke2Version, archive)
		fmt.Fprintf(&command, "curl -fsSL --retry 5 --retry-all-errors --retry-delay 5 --connect-timeout 20 --max-time 600 -o %s %s\n",
			shellSingleQuote(archivePath), shellSingleQuote(imagesURL))
	}
	fmt.Fprintf(&command, "curl -fsSL --retry 5 --retry-all-errors --retry-delay 5 --connect-timeout 20 --max-time 120 -o /tmp/rke2-sha256sum-amd64.txt %s\n",
		shellSingleQuote(checksumURL))

	for _, archive := range archives {
		fmt.Fprintf(&command, `
if ! awk -v archive=%s '$2 == archive || $2 == "*" archive { print; found=1 } END { if (!found) exit 1 }' /tmp/rke2-sha256sum-amd64.txt > %s ||
   [ ! -s %s ] ||
   ! (cd /tmp && sha256sum -c %s); then
  echo "############################################################" >&2
  echo "# SECURITY ERROR: RKE2 images checksum validation failed   #" >&2
  echo "# Refusing to use the downloaded images tarball.           #" >&2
  echo "############################################################" >&2
  rm -f %s /tmp/rke2-sha256sum-amd64.txt %s
  exit 1
fi
`, shellSingleQuote(archive), shellSingleQuote(selectedChecksumPath), shellSingleQuote(selectedChecksumPath), shellSingleQuote(selectedChecksumPath), strings.Join(temporaryPaths, " "), shellSingleQuote(selectedChecksumPath))
	}
	fmt.Fprintf(&command, "\nrm -f /tmp/rke2-sha256sum-amd64.txt %s", shellSingleQuote(selectedChecksumPath))
	return command.String(), nil
}

func buildRKE2ImagesMoveCommand(rke2Version, ingressController string) (string, error) {
	archives, err := rke2ImageArchiveNames(rke2Version, ingressController)
	if err != nil {
		return "", err
	}
	temporaryPaths := make([]string, 0, len(archives))
	for _, archive := range archives {
		temporaryPaths = append(temporaryPaths, shellSingleQuote("/tmp/"+archive))
	}
	return fmt.Sprintf("sudo mv %s /var/lib/rancher/rke2/agent/images/", strings.Join(temporaryPaths, " ")), nil
}

func buildRKE2InstallCommand(nodeType string, rke2Version string, expectedInstallerSHA256 string) (string, error) {
	installScriptURL, expectedInstallerSHA256, err := getRKE2InstallScriptURL(rke2Version, expectedInstallerSHA256)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(`tmp_script="$(mktemp /tmp/rke2-install.XXXXXX.sh)"
trap 'rm -f "$tmp_script"' EXIT

# Download the exact installer script for the requested RKE2 version.
curl -fsSL -o "$tmp_script" %s

# Refuse to execute the script unless it matches the pinned checksum.
if ! echo %s"  $tmp_script" | sha256sum -c -; then
  echo "############################################################" >&2
  echo "# SECURITY ERROR: RKE2 installer checksum validation failed #" >&2
  echo "# Refusing to run the downloaded installer.                #" >&2
  echo "# Check the resolved RKE2 version and installer checksum.  #" >&2
  echo "############################################################" >&2
  exit 1
fi

sudo INSTALL_RKE2_VERSION=%s INSTALL_RKE2_TYPE=%s sh "$tmp_script"`,
		shellSingleQuote(installScriptURL),
		shellSingleQuote(expectedInstallerSHA256),
		shellSingleQuote(rke2Version),
		shellSingleQuote(nodeType),
	), nil
}
