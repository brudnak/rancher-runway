package test

import (
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/terratest/settings"
	"log"
	"net/netip"
	"sort"
	"strings"
)

func configureRKE2IngressForExternalTLS(ip, ingressController string, loadBalancerSourceCIDRs []string) error {
	log.Printf("[rke2-ingress] Configuring %s forwarded-header trust for external TLS termination on %s", ingressController, ip)

	manifestFileName, manifest, err := rke2IngressConfigManifest(ingressController, loadBalancerSourceCIDRs)
	if err != nil {
		return err
	}

	cmd := "sudo mkdir -p /var/lib/rancher/rke2/server/manifests"
	if err := runRemoteSetupStep("rke2-ingress/create-manifests-dir", ip, cmd); err != nil {
		log.Printf("[rke2-ingress] FAILED to create manifests directory on %s: %v", ip, err)
		return fmt.Errorf("failed to create manifests directory: %w", err)
	}

	cmd = fmt.Sprintf("sudo bash -c 'cat > /var/lib/rancher/rke2/server/manifests/%s << EOL\n%s\nEOL'", manifestFileName, manifest)
	if err := runRemoteSetupStep("rke2-ingress/write-config", ip, cmd); err != nil {
		log.Printf("[rke2-ingress] FAILED to write forwarded headers config on %s: %v", ip, err)
		return fmt.Errorf("failed to write rke2 ingress config: %w", err)
	}

	return nil
}

func rke2IngressConfigManifest(ingressController string, loadBalancerSourceCIDRs []string) (string, string, error) {
	trustedCIDRs, err := normalizeTrustedCIDRs(loadBalancerSourceCIDRs)
	if err != nil {
		return "", "", err
	}
	if len(trustedCIDRs) == 0 {
		return "", "", fmt.Errorf("no ALB subnet CIDRs were available for RKE2 ingress forwarded-header trust")
	}

	switch ingressController {
	case settings.RKE2IngressControllerTraefik:
		return "rke2-traefik-config.yaml", rke2TraefikConfigManifest(trustedCIDRs), nil
	case settings.RKE2IngressControllerNginx:
		return "rke2-ingress-nginx-config.yaml", rke2IngressNginxConfigManifest(trustedCIDRs), nil
	default:
		return "", "", fmt.Errorf("unsupported RKE2 ingress controller %q", ingressController)
	}
}

func rke2IngressDaemonSetName(ingressController string) (string, error) {
	switch ingressController {
	case settings.RKE2IngressControllerTraefik:
		return "rke2-traefik", nil
	case settings.RKE2IngressControllerNginx:
		return "rke2-ingress-nginx-controller", nil
	default:
		return "", fmt.Errorf("unsupported RKE2 ingress controller %q", ingressController)
	}
}

func normalizeTrustedCIDRs(values []string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return nil, fmt.Errorf("invalid ALB subnet CIDR %q: %w", value, err)
		}
		canonical := prefix.Masked().String()
		if _, ok := seen[canonical]; ok {
			continue
		}
		seen[canonical] = struct{}{}
		normalized = append(normalized, canonical)
	}
	sort.Strings(normalized)
	return normalized, nil
}

func rke2TraefikConfigManifest(trustedCIDRs []string) string {
	var manifest strings.Builder
	manifest.WriteString(`apiVersion: helm.cattle.io/v1
kind: HelmChartConfig
metadata:
  name: rke2-traefik
  namespace: kube-system
spec:
  valuesContent: |-
    ports:
      web:
        forwardedHeaders:
          insecure: false
          trustedIPs:`)
	for _, cidr := range trustedCIDRs {
		fmt.Fprintf(&manifest, "\n            - %q", cidr)
	}
	return manifest.String()
}

func rke2IngressNginxConfigManifest(trustedCIDRs []string) string {
	return fmt.Sprintf(`apiVersion: helm.cattle.io/v1
kind: HelmChartConfig
metadata:
  name: rke2-ingress-nginx
  namespace: kube-system
spec:
  valuesContent: |-
    controller:
      config:
        use-forwarded-headers: "true"
        proxy-real-ip-cidr: %q`, strings.Join(trustedCIDRs, ","))
}
