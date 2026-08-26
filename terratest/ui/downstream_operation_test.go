package ui

import (
	"strings"
	"testing"
)

func TestControlPanelBundlesIndependentDownstreamLifecycle(t *testing.T) {
	runsSource := controlPanelVueSource(t, "WorkspaceRunsPanel.vue")
	modalSource := controlPanelVueSource(t, "ControlPanelModals.vue")
	destroySource := controlPanelVueSource(t, "DestroyPanel.vue")
	storeSource := controlPanelVueSource(t, "store.js")

	for _, marker := range []string{
		`{ mode: "downstream", label: "Downstream", operation: state.value?.downstream }`,
		`Management ready; downstream failed`,
		`Management and downstream ready`,
		`Retry downstream`,
		`open-downstream-logs`,
		`stop-downstream`,
		`downstreamStatus === "downstream_failed"`,
	} {
		if !strings.Contains(runsSource, marker) {
			t.Fatalf("workspace runs panel is missing downstream lifecycle marker %q", marker)
		}
	}

	for _, marker := range []string{
		`TestHAProvisionConfiguredLinodeDownstreams`,
		`-timeout 35m`,
		`^TestHACleanup$ -timeout 60m`,
		`Downstream provisioning failed; management remains ready`,
		`downstreamRunning`,
		`downstreamDone`,
		`downstreamError`,
	} {
		if !strings.Contains(modalSource, marker) {
			t.Fatalf("control panel log modal is missing downstream marker %q", marker)
		}
	}

	for _, marker := range []string{
		`/api/downstream/retry`,
		`retry downstream`,
		`openDownstreamLogs`,
		`state.value?.downstream?.running`,
		`syncDownstreamLogModal`,
	} {
		if !strings.Contains(storeSource, marker) {
			t.Fatalf("control panel store is missing downstream marker %q", marker)
		}
	}

	for _, marker := range []string{
		`Any recorded Linode downstream clusters are deleted first; if none exist, cleanup proceeds directly to Terraform destroy for the AWS management infrastructure.`,
		`AWS destroy will not start if downstream deletion fails.`,
		`For each HA management run, any recorded Linode downstream clusters are deleted first; when none exist, cleanup proceeds directly to AWS management Terraform destroy.`,
		`A downstream deletion failure prevents AWS destroy for that run.`,
		`recorded Linode downstream deletion or Terraform destroy`,
	} {
		if !strings.Contains(storeSource, marker) {
			t.Fatalf("control panel store is missing destructive-scope marker %q", marker)
		}
	}

	for _, marker := range []string{
		`if none exist, cleanup proceeds directly to AWS management Terraform destroy`,
		`AWS management Terraform destroy starts only after downstream deletion succeeds`,
		`deletes any recorded Linode downstreams first`,
	} {
		if !strings.Contains(destroySource, marker) {
			t.Fatalf("destroy panel is missing destructive-scope marker %q", marker)
		}
	}

	for _, marker := range []string{
		`Management ready; downstream failed`,
		`TestHAProvisionConfiguredLinodeDownstreams`,
		`-timeout 35m`,
		`^TestHACleanup$ -timeout 60m`,
		`/api/downstream/retry`,
		`Downstream provisioning failed; management remains ready`,
		`Any recorded Linode downstream clusters are deleted first; if none exist, cleanup proceeds directly to Terraform destroy for the AWS management infrastructure.`,
		`AWS destroy will not start if downstream deletion fails.`,
		`For each HA management run, any recorded Linode downstream clusters are deleted first; when none exist, cleanup proceeds directly to AWS management Terraform destroy.`,
		`A downstream deletion failure prevents AWS destroy for that run.`,
		`AWS management Terraform destroy starts only after downstream deletion succeeds`,
	} {
		if !strings.Contains(ControlPanelHeaderVueJS, marker) {
			t.Fatalf("compiled control panel bundle is missing downstream marker %q", marker)
		}
	}
}
