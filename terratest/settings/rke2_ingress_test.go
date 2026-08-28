package settings

import (
	"testing"

	"github.com/spf13/viper"
)

func TestCurrentRKE2IngressControllerDefaultsToTraefik(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	if got := CurrentRKE2IngressController(); got != RKE2IngressControllerTraefik {
		t.Fatalf("CurrentRKE2IngressController() = %q, want %q", got, RKE2IngressControllerTraefik)
	}
}

func TestCurrentRKE2IngressControllerNormalizesConfiguredValue(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("rke2.ingress_controller", " Ingress-NGINX ")

	if got := CurrentRKE2IngressController(); got != RKE2IngressControllerNginx {
		t.Fatalf("CurrentRKE2IngressController() = %q, want %q", got, RKE2IngressControllerNginx)
	}
}

func TestValidateRKE2IngressControllerConfigRejectsUnsupportedValue(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("rke2.ingress_controller", "none")

	err := ValidateRKE2IngressControllerConfig()
	if err == nil {
		t.Fatal("expected unsupported ingress controller to fail validation")
	}
}

func TestValidateRKE2IngressControllerConfigRejectsNonStringValue(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("rke2.ingress_controller", []string{"traefik"})

	if err := ValidateRKE2IngressControllerConfig(); err == nil {
		t.Fatal("expected a sequence-valued ingress controller to fail validation")
	}
	if got := CurrentRKE2IngressController(); got != "" {
		t.Fatalf("expected invalid non-string configuration not to silently default, got %q", got)
	}
}
