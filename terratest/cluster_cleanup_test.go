package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gruntwork-io/terratest/modules/logger"
	"github.com/gruntwork-io/terratest/modules/terraform"
)

func TestClusterCleanupScope(t *testing.T) {
	t.Setenv("RANCHER_RUNWAY_WORKSPACE", "")
	p := newCleanupBatchTestPanel(t)
	runID := "4ff008a3"
	record := panelRunRecord{RunID: runID, TotalHAs: 5, DeploymentType: deploymentTypeHARKE2, Status: "ready",
		TerraformModuleDir: p.terraformModuleDirForRun(runID), TerraformStatePath: p.terraformStatePathForRun(runID),
		TerraformDataDir: p.terraformDataDirForRun(runID), HAOutputRoot: p.haOutputRootForRun(runID)}
	p.writeRunRecord(record)
	p.writeCurrentRunRecord(panelRunRecord{RunID: "other"})
	id := localClusterIDForRun(runID, 4)
	if r, index, err := p.clusterCleanupRecord(id); err != nil || r.RunID != runID || index != 4 {
		t.Fatalf("target: %v, %d, %v", r.RunID, index, err)
	}
	for _, bad := range []string{"", "ha-4-local", localClusterIDForRun("other", 4), id + "-downstream", localClusterIDForRun(runID, 6), strings.Replace(id, "-4-", "-04-", 1)} {
		if _, _, err := p.clusterCleanupRecord(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	req := func(handler http.HandlerFunc, body string, token string) int {
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		r.Header.Set("X-Control-Panel-Token", token)
		w := httptest.NewRecorder()
		handler(w, r)
		return w.Code
	}
	body := fmt.Sprintf(`{"confirm":"confirm","clusterId":%q}`, id)
	if req(p.handleClusterCleanup, body, "") != http.StatusForbidden || req(p.handleCleanup, body, "token") != http.StatusBadRequest {
		t.Fatal("invalid token or mixed scope accepted")
	}
	for _, body := range []string{`{"confirm":"confirm","runId":"other"}`, `{"confirm":"confirm","all":true}`, `{"confirm":"confirm","clusterId":""}`, `{"confirm":"confirm"}`} {
		if code := req(p.handleClusterCleanup, body, "token"); code < 400 {
			t.Errorf("accepted %s", body)
		}
	}
	// Simulate the actual lifecycle runner in an isolated workspace.
	worker := filepath.Join(t.TempDir(), "worker")
	if err := os.MkdirAll(record.TerraformModuleDir, 0700); err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf(`#!/bin/sh
set -eu
[ "$%s" = '%s' ]
[ "$%s" = '%s' ]
[ "$2" = '-test.run=^TestDestroyRancherCluster$' ]
touch "$%s/.runway-destroyed-ha-4"
`, cleanupClusterIDEnv, id, runIDEnv, runID, terraformModuleDirEnv)
	if err := os.WriteFile(worker, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(packagedLifecycleBinaryEnv, worker)
	if code := req(p.handleClusterCleanup, body, "token"); code != http.StatusOK {
		t.Fatalf("cluster request: %d", code)
	}
	p.lifecycleWorkers.Wait()
	op := p.snapshotOperation(panelOperationCleanup)
	if op.Error != "" || op.ClusterID != id || op.RunID != runID {
		t.Fatalf("wrong worker or target: %+v", op)
	}
	if r, ok := p.readRunRecord(runID); !ok || r.Status != "ready" {
		t.Fatal("cluster cleanup removed or changed its run record")
	}
	if r, ok := p.readCurrentRunRecord(); !ok || r.RunID != "other" {
		t.Fatal("cluster cleanup changed the current slot")
	}

	if _, _, err := p.clusterCleanupRecord(id); err == nil {
		t.Fatal("accepted an already destroyed cluster")
	}
}

func TestClusterCleanupRemoteBackendIdentity(t *testing.T) {
	t.Setenv("TF_STATE_BUCKET", "fixture-bucket")
	t.Setenv("TF_STATE_KEY", "rancher/terraform.tfstate")
	t.Setenv("TF_STATE_REGION", "us-east-2")
	t.Setenv("TF_STATE_LOCK_TABLE", "fixture-lock")
	want := "s3://fixture-bucket/rancher/runs/run-a/terraform.tfstate (us-east-2)"
	if got := terraformBackendLabelForRun("run-a", "/fixture/state"); got != want {
		t.Errorf("backend label %q, want %q", got, want)
	}
}

func TestClusterRemovalRejectsBroaderPlans(t *testing.T) {
	target := `module.ha["4"]`
	for _, tc := range []struct {
		addr, action string
		valid        bool
	}{
		{target + ".aws_instance.node", "delete", true},
		{`module.ha["40"].aws_instance.node`, "delete", false},
		{`module.ha["5"].aws_instance.node`, "delete", false},
		{"aws_route53_record.shared", "delete", false},
		{target + ".aws_instance.node", "create", false},
		{target + ".aws_instance.node", "update", false},
	} {
		raw := fmt.Sprintf(`{"format_version":"1.2","resource_changes":[{"mode":"managed","address":%q,"change":{"actions":[%q],"before":{"id":"fixture"}}}]}`, tc.addr, tc.action)
		if _, err := clusterRemovalPlanResources(raw, target); (err == nil) != tc.valid {
			t.Errorf("%s %s: %v", tc.action, tc.addr, err)
		}
	}
}

func TestClusterRemovalDownstreamPreflight(t *testing.T) {
	for _, tc := range []struct {
		kind, item string
		ok         bool
	}{
		{"", "", true},
		{"clusters.management.cattle.io", `{"name":"local"}`, true},
		{"clusters.provisioning.cattle.io", `{"name":"local","namespace":"fleet-local"}`, true},
		{"clusters.provisioning.cattle.io", `{"name":"prod","namespace":"fleet-default"}`, false},
		{"clusters.management.cattle.io", `{"name":"c-m-imported"}`, false},
		{"machines.cluster.x-k8s.io", `{"name":"deleting-machine"}`, false},
		{"offline", "", false},
	} {
		command := func(_ context.Context, _ []byte, name string, args ...string) ([]byte, error) {
			if name != "kubectl" || args[1] != "only-ha-4" || args[3] != "get" {
				t.Fatalf("unexpected invocation %s %v", name, args)
			}
			if tc.kind == "offline" {
				return nil, fmt.Errorf("offline")
			}
			if args[4] == "crd" {
				if args[5] == tc.kind {
					return []byte(tc.kind), nil
				}
				return nil, nil
			}
			if args[4] != tc.kind {
				t.Fatalf("unexpected resource %s", args[4])
			}
			return []byte(`{"items":[{"metadata":` + tc.item + `}]}`), nil
		}
		err := verifyNoAttachedClusters(context.Background(), "only-ha-4", false, command)
		if (err == nil) != tc.ok {
			t.Errorf("%s %s: %v", tc.kind, tc.item, err)
		}
	}
}

// Built-in terraform_data has no external providers, network, or cloud account.
func TestClusterRemovalPreservesTerraformSiblings(t *testing.T) {
	if _, err := exec.LookPath("terraform"); err != nil {
		t.Skip("terraform is not installed")
	}
	dir := t.TempDir()
	for file, body := range map[string]string{
		"main.tf": `locals { ha_instances = { for i in range(1, 6) : i => "HA ${i}" } }
module "ha" {
  for_each = local.ha_instances
  source = "./child"
  name = each.value
}
output "clusters" { value = { for key, m in module.ha : key => m.id } }
`,
		"child/main.tf": `variable "name" { type = string }
resource "terraform_data" "node" { input = var.name }
output "id" { value = terraform_data.node.id }
`,
	} {
		if err := writePrivateConfigAtomically(filepath.Join(dir, file), []byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	options := &terraform.Options{TerraformDir: dir, Vars: map[string]interface{}{}, Lock: true, NoColor: true, Logger: logger.Discard, EnvVars: map[string]string{"TF_DATA_DIR": filepath.Join(dir, "data"), "TF_WORKSPACE": "default"}}
	if _, err := terraform.InitAndApplyE(t, options); err != nil {
		t.Fatal(err)
	}
	read := func() map[string]string {
		t.Helper()
		raw, err := terraform.RunTerraformCommandE(t, options, "output", "-json", "clusters")
		if err != nil {
			t.Fatal(err)
		}
		var ids map[string]string
		if err := json.Unmarshal([]byte(raw), &ids); err != nil {
			t.Fatal(err)
		}
		return ids
	}
	want := read()
	if len(want) != 5 {
		t.Fatalf("fixture returned %v", want)
	}
	if err := applyClusterRemoval(t, options, 4, func(bool) error { return fmt.Errorf("attached downstream") }); err == nil {
		t.Fatal("ignored downstream check")
	}
	if got := read(); !reflect.DeepEqual(got, want) {
		t.Fatalf("failed check affected state: %v", got)
	}
	for _, index := range []int{4, 2} {
		if err := applyClusterRemoval(t, options, index, func(resuming bool) error {
			if resuming {
				t.Fatal("unexpected pending receipt")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		// Simulate restarting after apply but before local bookkeeping.
		if err := applyClusterRemoval(t, options, index, func(resuming bool) error {
			if !resuming {
				t.Fatal("expected a retry")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		marker := destroyedHAPath(dir, index)
		if err := os.Rename(marker+".pending", marker); err != nil {
			t.Fatal(err)
		}
		delete(want, fmt.Sprint(index))
		if got := read(); !reflect.DeepEqual(got, want) {
			t.Fatalf("after destroying HA %d: got %v want %v", index, got, want)
		}
	}
	delete(options.Vars, "runway_retiring_ha")
	options.PlanFilePath = ""
	if code, err := terraform.PlanExitCodeE(t, options); err != nil || code != 0 {
		t.Fatalf("future apply would change resources: %d %v", code, err)
	}
}
