package test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteRancherResolutionArtifact(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("GITHUB_WORKSPACE", workspace)
	t.Setenv("RANCHER_RUNWAY_WORKSPACE", "")
	t.Setenv(runToolConfigPathEnv, "")

	plan := &RancherResolvedPlan{
		RequestedVersion:         "2.14.1-alpha7",
		RequestedDistro:          "auto",
		PreferredImageRegistries: []string{"stgregistry.suse.com", "docker.io"},
		BuildType:                "alpha",
		ResolvedDistro:           "community-staging",
		ResolvedImageRegistry:    "stgregistry.suse.com",
		ChartRepoAlias:           "optimus-rancher-alpha",
		ChartVersion:             "2.14.1-alpha7",
		RancherImage:             "stgregistry.suse.com/rancher/rancher",
		RancherImageTag:          "v2.14.1-alpha7",
		AgentImage:               "stgregistry.suse.com/rancher/rancher-agent:v2.14.1-alpha7",
		RancherImageDigest:       "sha256:" + strings.Repeat("a", 64),
		AgentImageDigest:         "sha256:" + strings.Repeat("b", 64),
		ImageBuildVersion:        "v2.14.1-alpha7-build42",
		ImageSourceURL:           "https://github.com/rancher/rancher",
		ImageSourceRevision:      strings.Repeat("c", 40),
		ImageSourceCommitURL:     "https://github.com/rancher/rancher/commit/" + strings.Repeat("c", 40),
		CompatibilityBaseline:    "2.14.0",
		RecommendedRKE2Version:   "v1.34.6+rke2r3",
		Explanation:              []string{"Using exact chart match optimus-rancher-alpha/rancher@2.14.1-alpha7"},
	}

	if err := writeRancherResolutionArtifact("install", 1, plan); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(workspace, "automation-output", "rancher-resolution-install-ha-1.json"))
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		`"phase": "install"`,
		`"chart_repo_alias": "optimus-rancher-alpha"`,
		`"chart_version": "2.14.1-alpha7"`,
		`"chart_source": "optimus-rancher-alpha/rancher@2.14.1-alpha7"`,
		`"resolved_distro": "community-staging"`,
		`"resolved_image_registry": "stgregistry.suse.com"`,
		`"preferred_image_registries": [`,
		`"rancher_image_digest": "sha256:`,
		`"image_build_version": "v2.14.1-alpha7-build42"`,
		`"image_source_commit_url": "https://github.com/rancher/rancher/commit/`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected artifact to contain %s:\n%s", want, got)
		}
	}
}

func TestRancherResolutionArtifactsStayWithFrozenRun(t *testing.T) {
	root := t.TempDir()
	t.Setenv("RANCHER_RUNWAY_WORKSPACE", root)
	for _, distro := range []string{"prime", "community"} {
		config := filepath.Join(root, "automation-output", "runs", distro, "tool-config.yml")
		t.Setenv(runToolConfigPathEnv, config)
		plan := &RancherResolvedPlan{RequestedDistro: distro}
		if err := writeRancherResolutionArtifact("install", 1, plan); err != nil {
			t.Fatal(err)
		}
	}
	for _, distro := range []string{"prime", "community"} {
		path := filepath.Join(root, "automation-output", "runs", distro, "rancher-resolution-install-ha-1.json")
		data, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(data), `"requested_distro": "`+distro+`"`) {
			t.Fatalf("run %s resolution overwritten: %s, %v", distro, data, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "automation-output", "rancher-resolution-install-ha-1.json")); !os.IsNotExist(err) {
		t.Fatalf("frozen run wrote shared resolution artifact: %v", err)
	}
}
