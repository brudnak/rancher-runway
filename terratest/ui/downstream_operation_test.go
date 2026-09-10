package ui

import (
	"strings"
	"testing"
)

func TestControlPanelBundlesIndependentDownstreamLifecycle(t *testing.T) {
	runsSource := controlPanelVueSource(t, "WorkspaceRunsPanel.vue")
	modalSource := controlPanelVueSource(t, "ControlPanelModals.vue")
	destroySource := controlPanelVueSource(t, "DestroyPanel.vue")
	clustersSource := controlPanelVueSource(t, "ClustersPanel.vue")
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
		`Cleanup attempts any recorded Linode downstream clusters first, then proceeds to Terraform destroy for the AWS management infrastructure.`,
		`If downstream deletion fails, AWS destroy still continues and you will be warned that Linode resources may require manual cleanup.`,
		`then proceeds to AWS management Terraform destroy even if downstream deletion fails.`,
		`Any remaining Linode resources are reported for manual cleanup.`,
		`Slots whose management Terraform destroy succeeds are removed; Terraform failures stay recorded`,
		`recorded Linode downstream deletion or Terraform destroy`,
	} {
		if !strings.Contains(storeSource, marker) {
			t.Fatalf("control panel store is missing destructive-scope marker %q", marker)
		}
	}

	for _, marker := range []string{
		`proceeds to AWS management Terraform destroy even if downstream deletion fails`,
		`The panel warns when Linode resources may require manual cleanup.`,
		`A slot record is removed after management Terraform destroy succeeds`,
		`even when downstream cleanup needs manual follow-up`,
	} {
		if !strings.Contains(destroySource, marker) {
			t.Fatalf("destroy panel is missing destructive-scope marker %q", marker)
		}
	}

	for _, marker := range []string{
		`id="manualLinodeCleanupWarningModal"`,
		`role="alertdialog"`,
		`Manual Linode cleanup required`,
		`AWS management destroy continued`,
		`{{ manualLinodeCleanupWarning.warning }}`,
		`Review cleanup logs`,
		`I understand`,
	} {
		if !strings.Contains(modalSource, marker) {
			t.Fatalf("manual Linode cleanup popup is missing contract marker %q", marker)
		}
	}

	for _, marker := range []string{
		`const completedCleanupWarning = (source, operation) =>`,
		`operation?.warning`,
		`const maybeShowManualLinodeCleanupWarning = currentState =>`,
		`shownManualLinodeCleanupWarningKeys`,
		`manualLinodeCleanupWarning.show = true`,
		`maybeShowManualLinodeCleanupWarning(fetched)`,
		`syncCleanupLogModal`,
		`cleanup.warning && cleanup.finishedAt`,
		`cleanupBatch?.warning && cleanupBatch?.finishedAt`,
	} {
		if !strings.Contains(storeSource, marker) {
			t.Fatalf("control panel store is missing cleanup-warning behavior marker %q", marker)
		}
	}

	for _, marker := range []string{
		`id="cleanupWarning"`,
		`Manual Linode cleanup required`,
		`AWS management destroy continued after downstream deletion failed`,
		`AWS destroy finished; manual Linode cleanup required`,
		`cleanup.warning || "no-warning"`,
		`batchWarning`,
	} {
		if !strings.Contains(destroySource, marker) {
			t.Fatalf("destroy panel is missing cleanup-warning result marker %q", marker)
		}
	}

	for _, marker := range []string{
		`cleanupWarning ? 'text-amber-800 dark:text-amber-200'`,
		`AWS management destroy finished for the selected run, but downstream Linode cleanup did not.`,
		`state.value?.cleanup?.warning`,
		`cleanup.warning || ""`,
	} {
		if !strings.Contains(clustersSource, marker) {
			t.Fatalf("clusters panel is missing cleanup-warning marker %q", marker)
		}
	}

	for _, marker := range []string{
		`Management ready; downstream failed`,
		`TestHAProvisionConfiguredLinodeDownstreams`,
		`-timeout 35m`,
		`^TestHACleanup$ -timeout 60m`,
		`/api/downstream/retry`,
		`Downstream provisioning failed; management remains ready`,
		`Manual Linode cleanup required`,
		`manualLinodeCleanupWarningModal`,
		`AWS management destroy continued`,
		`proceeds to AWS management Terraform destroy even if downstream deletion fails`,
		`Any remaining Linode resources are reported for manual cleanup.`,
		`AWS destroy finished; manual Linode cleanup required`,
	} {
		if !strings.Contains(ControlPanelHeaderVueJS, marker) {
			t.Fatalf("compiled control panel bundle is missing downstream marker %q", marker)
		}
	}

	staleBlockingPhrases := []string{
		`AWS destroy will not start if downstream deletion fails`,
		`A downstream deletion failure prevents AWS destroy`,
		`AWS management Terraform destroy starts only after downstream deletion succeeds`,
	}
	for _, surface := range []struct {
		name   string
		source string
	}{
		{name: "control panel store", source: storeSource},
		{name: "destroy panel", source: destroySource},
		{name: "clusters panel", source: clustersSource},
		{name: "cleanup modal", source: modalSource},
		{name: "compiled control panel bundle", source: ControlPanelHeaderVueJS},
	} {
		for _, phrase := range staleBlockingPhrases {
			if strings.Contains(surface.source, phrase) {
				t.Fatalf("%s still claims downstream failure blocks AWS destroy: %q", surface.name, phrase)
			}
		}
	}
}
