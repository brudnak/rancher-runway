package test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestRunConfigSnapshotPreservesEffectiveConfiguration(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	root := t.TempDir()
	p := &localControlPanel{repoRoot: root, testDir: root}
	original := filepath.Join(root, "tool-config.yml")
	if err := os.WriteFile(original, []byte("rancher:\n  distro: auto\n  bootstrap_password: saved-password\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(runToolConfigPathEnv, "")
	if err := setupConfigE(root); err != nil {
		t.Fatal(err)
	}
	// Overrides in memory represent the setup editor's effective configuration.
	viper.Set("rancher.distro", "prime")
	viper.Set("rancher.preferred_image_registries", []string{"registry.rancher.com"})
	viper.Set("rancher.helm_commands", []string{"helm install prime"})
	if err := p.freezeRunConfig("prime"); err != nil {
		t.Fatal(err)
	}
	viper.Set("rancher.distro", "community")
	viper.Set("rancher.preferred_image_registries", []string{"docker.io"})
	viper.Set("rancher.helm_commands", []string{"helm install community"})
	if err := p.freezeRunConfig("community"); err != nil {
		t.Fatal(err)
	}
	if err := p.freezeRunConfig("prime"); err == nil {
		t.Fatal("existing run snapshot was overwritten")
	}
	for _, run := range []struct{ name, registry string }{
		{"prime", "registry.rancher.com"},
		{"community", "docker.io"},
	} {
		t.Setenv(runToolConfigPathEnv, p.runConfigPath(run.name))
		if err := setupConfigE(root); err != nil {
			t.Fatal(err)
		}
		if got := viper.GetString("rancher.distro"); got != run.name {
			t.Fatalf("snapshot distro %q, want %q", got, run.name)
		}
		if got := viper.GetStringSlice("rancher.preferred_image_registries"); len(got) != 1 || got[0] != run.registry {
			t.Fatalf("snapshot registry %v, want %s", got, run.registry)
		}
		if got := viper.GetStringSlice("rancher.helm_commands"); len(got) != 1 || got[0] != "helm install "+run.name {
			t.Fatalf("snapshot Helm command %v", got)
		}
		if viper.GetString("rancher.bootstrap_password") != "saved-password" {
			t.Fatal("snapshot omitted original config fields")
		}
		info, err := os.Stat(p.runConfigPath(run.name))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("snapshot permissions: %v, %v", info, err)
		}
	}
	t.Setenv(runToolConfigPathEnv, filepath.Join(root, "missing.yml"))
	if err := setupConfigE(root); err == nil {
		t.Fatal("missing explicit snapshot fell back to shared configuration")
	}
}

func TestRunWorkerToolsIsolateCachesAndPreserveCustomHelmConfiguration(t *testing.T) {
	root := t.TempDir()
	t.Setenv("RANCHER_RUNWAY_WORKSPACE", root)
	p := &localControlPanel{repoRoot: root, testDir: root}
	for _, key := range []string{"HELM_REPOSITORY_CONFIG", "HELM_REGISTRY_CONFIG"} {
		source := filepath.Join(root, key)
		if err := os.WriteFile(source, []byte("private "+key), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv(key, source)
	}
	envs := map[string]map[string]string{}
	for _, run := range []string{"prime", "community"} {
		if err := p.prepareRunWorkerTools(run); err != nil {
			t.Fatal(err)
		}
		env := p.panelRunWorkerEnv([]string{"TF_PLUGIN_CACHE_DIR=/shared/cache", "HELM_CONTENT_CACHE=/shared/helm"}, run)
		values := map[string]string{}
		for _, item := range env {
			key, value, _ := strings.Cut(item, "=")
			if _, exists := values[key]; exists {
				t.Fatalf("duplicate env value for %s", key)
			}
			values[key] = value
		}
		envs[run] = values
		for _, key := range []string{"HELM_REPOSITORY_CONFIG", "HELM_REGISTRY_CONFIG"} {
			data, err := os.ReadFile(values[key])
			if err != nil || string(data) != "private "+key {
				t.Fatalf("custom Helm configuration %s missing: %s, %v", key, data, err)
			}
			info, err := os.Stat(values[key])
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("private Helm configuration permissions: %v, %v", info, err)
			}
		}
	}
	for key, primePath := range envs["prime"] {
		if key != runToolConfigPathEnv && primePath == envs["community"][key] {
			t.Fatalf("workers share %s: %s", key, primePath)
		}
	}
	primeRepo := envs["prime"]["HELM_REPOSITORY_CONFIG"]
	if err := os.WriteFile(primeRepo, []byte("updated by prime"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := p.prepareRunWorkerTools("prime"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(primeRepo)
	if err != nil || string(data) != "updated by prime" {
		t.Fatalf("later phase replaced the run's Helm configuration: %s, %v", data, err)
	}
}

func TestRunWorkerConfigEnvHandlesLegacyAndMissingSnapshots(t *testing.T) {
	root := t.TempDir()
	t.Setenv("RANCHER_RUNWAY_WORKSPACE", root)
	p := &localControlPanel{repoRoot: root, testDir: root}
	p.writeRunRecord(panelRunRecord{RunID: "new", ConfigSnapshot: true})
	for _, run := range []struct{ id, path string }{
		{"legacy", ""},
		{"new", p.runConfigPath("new")},
	} {
		env := p.panelRunWorkerEnv([]string{runToolConfigPathEnv + "=/another/run/config.yml"}, run.id)
		found := false
		for _, item := range env {
			if strings.HasPrefix(item, runToolConfigPathEnv+"=") {
				found = true
				if item != runToolConfigPathEnv+"="+run.path {
					t.Fatalf("incorrect configuration for %s: %s", run.id, item)
				}
			}
		}
		if !found {
			t.Fatalf("configuration selection missing for %s", run.id)
		}
	}
}
