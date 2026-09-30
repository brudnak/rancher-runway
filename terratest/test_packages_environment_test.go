package test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTestPackageHelmCommandRedaction(t *testing.T) {
	command := `helm upgrade --install rancher rancher-prime/rancher --namespace cattle-system --version 2.15.2 --set 'bootstrapPassword=space secret,replicas=3,hostname=private.rancher.test' --set-string 'rancherImage=rancher/rancher,rancherImageTag=v2.15.2' --set-json 'webhook={"image":{"repository":"rancher/rancher-webhook","tag":"v0.12.1"},"credentials":{"password":"json-secret"}}' --set-literal 'customValue=unknown-secret' --values /Users/private/values.yml --kubeconfig=/Users/private/kubeconfig --password 'repo secret' --atomic --wait`
	got := sanitizeTestPackageHelmCommand(command)
	for _, secret := range []string{"space secret", "private.rancher.test", "json-secret", "unknown-secret", "/Users/private", "repo secret"} {
		if strings.Contains(got, secret) {
			t.Fatalf("command exposed %q: %s", secret, got)
		}
	}
	for _, kept := range []string{"helm upgrade rancher rancher-prime/rancher", "--install", "--version '2.15.2'", "replicas=3", "rancherImageTag=v2.15.2", "rancher/rancher-webhook", "v0.12.1", "--wait"} {
		if !strings.Contains(got, kept) {
			t.Fatalf("lost safe command metadata %q: %s", kept, got)
		}
	}
	if got != sanitizeTestPackageHelmCommand(got) {
		t.Fatal("redaction must be stable when report sanitizes saved command again")
	}
	for _, bad := range []string{`helm install rancher rancher/rancher ; cat secret`, `helm install rancher rancher/rancher --set 'broken`, `cat /private/secret`} {
		if sanitizeTestPackageHelmCommand(bad) != "" {
			t.Fatalf("accepted malformed command %q", bad)
		}
	}
	subs := sanitizeTestPackageHelmCommand("helm install rancher rancher/rancher --set rancherImage=$(secret) --set-json 'webhook={\"image\":{\"tag\":\"$(secret)\"}}'")
	if strings.Contains(subs, "$(secret)") {
		t.Fatal("shell substitution survived redaction")
	}
}

func TestTestPackageEnvironmentCopiesKnownMetadataAndRedactedCommand(t *testing.T) {
	p := clusterWorkspaceTestPanel(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "install.sh"), []byte("#!/bin/sh\nhelm install rancher rancher-prime/rancher --version 2.15.2 --set bootstrapPassword=private,replicas=3\n"), 0600); err != nil {
		t.Fatal(err)
	}
	digest := "sha256:" + strings.Repeat("a", 64)
	p.rememberClusterSnapshot([]clusterView{{ID: "known", Name: "Rancher", RunID: "run-1", Type: "local", Role: "local", DeploymentType: "rke2", Version: "head", RancherURL: "https://rancher.test", KubeconfigPath: filepath.Join(dir, "kubeconfig.yaml"), Pods: []podView{{Name: "rancher-webhook-a", Namespace: "cattle-system", Images: []containerImageView{{Name: "rancher-webhook", Image: "rancher/rancher-webhook:v0.12.1", ImageID: "docker-pullable://rancher/rancher-webhook@" + digest}}}}}})
	e, err := p.testPackageEnvironment(testPackageEnvironment{ClusterID: "known", HelmCommand: "forged", Details: []testPackageEnvironmentDetail{{Label: "Fake", Value: "untrusted", Source: "observed"}}})
	if err != nil {
		t.Fatal(err)
	}
	if e.RancherVersion != "head" || strings.Contains(e.HelmCommand, "private") || !strings.Contains(e.HelmCommand, "replicas=3") || len(e.Images) != 2 {
		t.Fatalf("missing or unsafe environment %+v", e)
	}
	details := map[string]testPackageEnvironmentDetail{}
	for _, d := range e.Details {
		details[d.Label] = d
		if d.Source != "recorded" {
			t.Fatal("cached pod incorrectly promoted to fresh observation")
		}
	}
	if details["Fake"].Value != "" || details["Rancher chart version (install)"].Value != "2.15.2" || details["Run"].Value != "run-1" {
		t.Fatalf("metadata incorrect: %+v", details)
	}
}

func TestTestPackageEnvironmentProbePreservesProvenanceAndPartialFailures(t *testing.T) {
	old := time.Now().UTC().Add(-time.Hour)
	e := testPackageEnvironment{ClusterID: "cluster", Source: "recorded", RecordedAt: time.Now().UTC(), RancherVersion: "head", Details: []testPackageEnvironmentDetail{{Label: "Webhook chart version", Value: "110.0.1+up0.11.0", Source: "observed", ObservedAt: &old}}}
	runner := func(ctx context.Context, path string, args ...string) (string, error) {
		if path != "selected-kubeconfig" || len(args) < 3 || args[0] != "--context" || args[1] != "selected-context" {
			t.Errorf("unpinned context: %s %v", path, args)
		}
		switch strings.Join(args[2:], " ") {
		case "version -o json":
			return `{"serverVersion":{"gitVersion":"v1.35.3+rke2r1"}}`, nil
		case "get settings.management.cattle.io server-version -o json":
			return `{"value":"v2.15.2-rc2"}`, nil
		case "get apps.catalog.cattle.io rancher-webhook -n cattle-system -o json":
			return "", errors.New("Authorization token-private:secret /Users/private/kubeconfig")
		case "get pods -n cattle-system -o json":
			return `{"items":[{"metadata":{"name":"rancher-webhook-abc","namespace":"cattle-system"},"spec":{"containers":[{"name":"rancher-webhook","image":"rancher/rancher-webhook:v0.12.1"}]},"status":{"phase":"Running","containerStatuses":[{"name":"rancher-webhook","ready":true,"imageID":"containerd://sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}}]}`, nil
		}
		return "", errors.New("unexpected call")
	}
	got := probeTestPackageEnvironment(context.Background(), e, clusterView{ID: "cluster", Type: "local", Role: "local", KubeconfigPath: "selected-kubeconfig"}, "selected-context", runner)
	if got.RancherVersion != "v2.15.2-rc2" || got.KubernetesVersion != "v1.35.3+rke2r1" || got.ObservedAt == nil || len(got.Images) != 2 || len(got.Warnings) != 1 {
		t.Fatalf("incomplete enriched metadata: %+v", got)
	}
	for _, d := range got.Details {
		if d.Label == "Webhook chart version" && !d.ObservedAt.Equal(old) {
			t.Fatal("failed refresh relabeled old observation with new time")
		}
		if strings.Contains(d.Label, " image · ") && (d.Source != "observed" || d.ObservedAt == nil) {
			t.Fatal("fresh runtime digest lost provenance")
		}
	}
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), "token-private") || strings.Contains(string(raw), "/Users/private") {
		t.Fatal("probe errors leaked credentials or paths")
	}
	if err := validateTestPackageEnvironment(got); err != nil {
		t.Fatal(err)
	}
}

func TestTestPackageEnvironmentReusesObservationsButNeverRewritesSessions(t *testing.T) {
	p := clusterWorkspaceTestPanel(t)
	p.rememberClusterSnapshot([]clusterView{{ID: "cluster", Name: "Known Rancher", Role: "local", Type: "local", Version: "head", RancherURL: "https://rancher.test"}})
	old := time.Now().UTC().Add(-time.Hour)
	p.rememberTestPackageDeploymentDetails(clusterDeploymentDetailsResponse{ClusterID: "cluster", CollectedAt: old, RancherVersion: "v2.15.1", KubernetesVersion: "v1.35.2+rke2r1", WebhookChartVersion: "110.0.1"})
	first, err := p.testPackageEnvironment(testPackageEnvironment{ClusterID: "cluster", Configuration: "First session"})
	if err != nil {
		t.Fatal(err)
	}
	s := packageTestService(t)
	pkg := packageTestPlan(t, s)
	pkg = packageTestMutation(t, s, testPackageRequest{Action: "start-session", ID: pkg.ID, Revision: pkg.Revision, Name: "Baseline", Purpose: "reproduction"}, &first, nil, nil)
	frozen := cloneTestPackage(pkg).Sessions[0].Environment
	p.rememberTestPackageDeploymentDetails(clusterDeploymentDetailsResponse{ClusterID: "cluster", CollectedAt: time.Now().UTC(), RancherVersion: "v2.15.2", KubernetesVersion: "v1.35.3+rke2r1"})
	second, err := p.testPackageEnvironment(testPackageEnvironment{ClusterID: "cluster", Configuration: "My independent notes", Details: []testPackageEnvironmentDetail{{Label: "Rancher server version", Value: "forged", Source: "observed"}}})
	if err != nil {
		t.Fatal(err)
	}
	if second.RancherVersion != "v2.15.2" || second.Configuration != "My independent notes" {
		t.Fatalf("did not reuse trusted observation independently: %+v", second)
	}
	saved, _ := s.get(pkg.ID)
	if !reflect.DeepEqual(saved.Sessions[0].Environment, frozen) {
		t.Fatal("refresh rewrote preserved session")
	}
	p.rememberClusterSnapshot([]clusterView{{ID: "cluster", Name: "Different endpoint", Role: "local", Version: "head", RancherURL: "https://different.test"}})
	changed, err := p.testPackageEnvironment(testPackageEnvironment{ClusterID: "cluster"})
	if err != nil || changed.ObservedAt != nil || changed.RancherVersion != "head" {
		t.Fatal("observations crossed endpoint identity")
	}
}

func TestTestPackageEnvironmentDownstreamProbesOnlyKubernetes(t *testing.T) {
	calls := 0
	e := probeTestPackageEnvironment(context.Background(), testPackageEnvironment{Source: "recorded", RecordedAt: time.Now()}, clusterView{Type: "downstream", Role: "downstream"}, "context", func(_ context.Context, _ string, args ...string) (string, error) {
		calls++
		if strings.Join(args[2:], " ") != "version -o json" {
			t.Fatal("management probe sent to downstream")
		}
		return `{"serverVersion":{"gitVersion":"v1.35.3+k3s1"}}`, nil
	})
	if calls != 1 || e.KubernetesVersion != "v1.35.3+k3s1" || e.RancherVersion != "" {
		t.Fatal("downstream version attribution failed")
	}
}

func TestTestPackageEnvironmentReportAndBundleRoundTrip(t *testing.T) {
	_, s, pkg := testPackageTransferFixture(t)
	now := time.Now().UTC()
	pkg.Sessions[0].Environment.Details = []testPackageEnvironmentDetail{{Label: "Webhook chart version", Value: "110.0.2+up0.12.1", Source: "observed", ObservedAt: &now}}
	pkg.Sessions[0].Environment.ObservedAt = &now
	pkg.Sessions[0].Environment.HelmCommand = sanitizeTestPackageHelmCommand("helm install rancher rancher/rancher --version 2.15.2 --set bootstrapPassword=private,replicas=3")
	s.mu.Lock()
	err := s.saveLocked(pkg)
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	report, err := testPackageMarkdown(pkg, "", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Webhook chart version", "110.0.2+up0.12.1", "Recorded Helm install command", "replicas=3"} {
		if !strings.Contains(report, expected) {
			t.Fatalf("report omitted %s", expected)
		}
	}
	if strings.Contains(report, "bootstrapPassword=private") {
		t.Fatal("report exposed secret")
	}
	bundle, err := s.buildBundle(pkg.ID, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(bundle)
	var restored testPackageBundle
	if err = json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	if _, _, err = validateTestPackageBundle(&restored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored.Package.Sessions[0].Environment, pkg.Sessions[0].Environment) {
		t.Fatal("portable archive lost environment provenance")
	}
}
