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
		`confirmText: "Retry downstream"`,
		`openDownstreamLogs`,
		`state.value?.downstream?.running`,
		`syncDownstreamLogModal`,
	} {
		if !strings.Contains(storeSource, marker) {
			t.Fatalf("control panel store is missing downstream marker %q", marker)
		}
	}

	for _, marker := range []string{
		`Cleanup removes tracked downstream clusters and waits for their machines first.`,
		`A failure retains management access and run records for inspection and retry.`,
		`Management Terraform destroy starts only after downstream deletion succeeds.`,
		`If downstream cleanup fails, that run stops and keeps management access and run records.`,
		`Slots whose management Terraform destroy succeeds are removed; Terraform failures stay recorded`,
		`recorded Linode downstream deletion or Terraform destroy`,
	} {
		if !strings.Contains(storeSource, marker) {
			t.Fatalf("control panel store is missing destructive-scope marker %q", marker)
		}
	}

	for _, marker := range []string{
		`Management Terraform destroy starts only after downstream deletion succeeds.`,
		`On failure, management access and run records are retained so you can fix the issue and retry.`,
		`Cleanup removes tracked downstream clusters through Rancher first and waits for their machines to disappear.`,
	} {
		if !strings.Contains(destroySource, marker) {
			t.Fatalf("destroy panel is missing destructive-scope marker %q", marker)
		}
	}

	for _, marker := range []string{
		`id="manualLinodeCleanupWarningModal"`,
		`role="alertdialog"`,
		`Manual Linode cleanup required`,
		`New Destroy operations stop on downstream cleanup failure`,
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
		`Downstream deletion needs attention`,
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
		`New Destroy operations stop on downstream cleanup failure`,
		`Management Terraform destroy starts only after downstream deletion succeeds`,
		`If downstream cleanup fails, that run stops and keeps management access and run records.`,
		`AWS destroy finished; manual Linode cleanup required`,
	} {
		if !strings.Contains(ControlPanelHeaderVueJS, marker) {
			t.Fatalf("compiled control panel bundle is missing downstream marker %q", marker)
		}
	}

	staleContinuingPhrases := []string{
		`AWS management destroy continued`,
		`proceeds to AWS management Terraform destroy even if downstream deletion fails`,
		`AWS destroy continues`,
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
		for _, phrase := range staleContinuingPhrases {
			if strings.Contains(surface.source, phrase) {
				t.Fatalf("%s still claims downstream failure permits AWS destroy: %q", surface.name, phrase)
			}
		}
	}
}
