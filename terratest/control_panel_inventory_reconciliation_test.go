package test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDownstreamInventoryReconcilesManualChanges(t *testing.T) {
	t.Setenv("RANCHER_RUNWAY_WORKSPACE", t.TempDir())
	bin := t.TempDir()
	fixture := filepath.Join(bin, "clusters.json")
	t.Setenv("RUNWAY_TEST_CLUSTERS", fixture)
	script := `#!/bin/sh
case "$*" in
  *clusters.provisioning.cattle.io*) cat "$RUNWAY_TEST_CLUSTERS" ;;
  *clusters.management.cattle.io*) printf '%s' '{"items":[]}' ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "kubectl"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	writeFixture := func(body string) {
		t.Helper()
		if err := os.WriteFile(fixture, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	panel := &localControlPanel{}
	local := clusterView{RunID: "run-a", HAIndex: 1, Available: true, KubeconfigPath: "fixture"}
	writeFixture(`{"items":[{"metadata":{"name":"manually-created","namespace":"fleet-default"}}]}`)
	clusters := panel.discoverDownstreamClusters(local, nil)
	if len(clusters) != 1 || clusters[0].Name != "manually-created" {
		t.Fatalf("manual cluster was not discovered: %#v", clusters)
	}
	if panel.anyOperationRunning() {
		t.Fatal("external provisioning must not lock cleanup or quit")
	}
	// A successful empty live response takes precedence over any old run record.
	records := []downstreamOutputRecord{{Namespace: "fleet-default", ClusterName: "manually-created"}}
	writeFixture(`{"items":[]}`)
	if clusters := panel.discoverDownstreamClusters(local, records); len(clusters) != 0 {
		t.Fatalf("deleted downstream remains in inventory: %#v", clusters)
	}
	// Discovery failure must not be mistaken for confirmed absence.
	writeFixture(`unavailable`)
	if clusters := panel.discoverDownstreamClusters(local, records); len(clusters) != 1 || clusters[0].ProvisioningMessage == "" {
		t.Fatalf("discovery failure discarded the saved cluster: %#v", clusters)
	}
}
