package settings

import (
	"slices"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestNormalizePreferredImageRegistriesUsesFixedPriorityAndDeduplicates(t *testing.T) {
	got, err := NormalizePreferredImageRegistries([]string{
		"docker.io",
		" stgregistry.suse.com ",
		"STGREGISTRY.SUSE.COM",
		"auto",
		"",
	})
	if err != nil {
		t.Fatalf("NormalizePreferredImageRegistries returned error: %v", err)
	}
	want := []string{"stgregistry.suse.com", "docker.io"}
	if !slices.Equal(got, want) {
		t.Fatalf("NormalizePreferredImageRegistries = %#v, want %#v", got, want)
	}
}

func TestNormalizePreferredImageRegistriesRejectsUnknownRegistry(t *testing.T) {
	_, err := NormalizePreferredImageRegistries([]string{"registry.example.test"})
	if err == nil || !strings.Contains(err.Error(), `unsupported registry "registry.example.test"`) {
		t.Fatalf("expected fixed allow-list error, got %v", err)
	}
}

func TestCurrentPreferredImageRegistriesReadsCanonicalConfig(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("rancher.preferred_image_registries", []string{"docker.io", "registry.suse.com"})

	got := CurrentPreferredImageRegistries()
	want := []string{"registry.suse.com", "docker.io"}
	if !slices.Equal(got, want) {
		t.Fatalf("CurrentPreferredImageRegistries = %#v, want %#v", got, want)
	}
}
