package ui

import (
	"os"
	"strings"
	"testing"
)

func controlPanelTabsSource(t *testing.T) string {
	t.Helper()

	source, err := os.ReadFile("vue/src/ControlPanelTabs.vue")
	if err != nil {
		t.Fatalf("read ControlPanelTabs.vue: %v", err)
	}
	return string(source)
}

func controlPanelVueSource(t *testing.T, name string) string {
	t.Helper()

	source, err := os.ReadFile("vue/src/" + name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(source)
}

func TestDeployedClusterImageDetailsAreBundled(t *testing.T) {
	clustersSource := controlPanelVueSource(t, "ClustersPanel.vue")
	cardSource := controlPanelVueSource(t, "ClusterCard.vue")
	detailSource := controlPanelVueSource(t, "DeployedImageDetails.vue")

	for _, marker := range []string{
		"Requested Rancher",
		"Kubernetes version",
		"DeployedImageDetails",
		`:key="cluster.id"`,
	} {
		if !strings.Contains(cardSource, marker) {
			t.Fatalf("cluster card source is missing %q", marker)
		}
	}

	for _, marker := range []string{
		"Deployment versions",
		"Rancher agent",
		"Full Rancher build version",
		"Expected webhook version",
		"Deployed webhook tag",
		"Exact runtime digest",
		"/api/clusters/details",
		"/api/images/inspect",
		"includeBuildYaml: true",
		"Inspect declared tag instead",
		"Build history",
		"build.yaml",
		"currentPodImageCount === 0",
		"handleDialogKeydown",
		"runtimeRepository || declared.repository",
	} {
		if !strings.Contains(detailSource, marker) {
			t.Fatalf("deployed image detail source is missing %q", marker)
		}
	}

	for _, marker := range []string{
		"Deployment versions",
		"Rancher agent",
		"Full Rancher build version",
		"Expected webhook version",
		"Deployed webhook tag",
		"Exact runtime digest",
		"/api/clusters/details",
		"/api/images/inspect",
		"includeBuildYaml",
		"Inspect declared tag instead",
		"Build history",
		"build.yaml",
	} {
		if !strings.Contains(ControlPanelHeaderVueJS, marker) {
			t.Fatalf("compiled control panel bundle is missing deployed image detail marker %q", marker)
		}
	}

	for _, marker := range []string{
		`class="grid min-w-0 gap-4"`,
		`class="min-w-0 max-w-full break-words rounded-md border`,
	} {
		if !strings.Contains(clustersSource, marker) {
			t.Fatalf("clusters panel source is missing responsive-width marker %q", marker)
		}
	}
}

func TestControlPanelNavigationUsesSearchableGroupedWorkspaces(t *testing.T) {
	source := controlPanelTabsSource(t)
	for _, marker := range []string{
		`matchingNavigationGroups`, `Search workspaces`, `aria-expanded`, `aria-controls="workspace-picker"`,
		`@keydown="searchKeys"`, `@keydown="resultKeys"`, `data-active-workspace`,
		`document.removeEventListener('keydown', shortcut)`,
	} {
		if !strings.Contains(source, marker) {
			t.Fatalf("workspace navigation is missing %q", marker)
		}
	}
	for _, marker := range []string{`panel-workspace-picker`, `Find a workspace`, `No workspaces found`} {
		if !strings.Contains(ControlPanelHeaderVueJS, marker) {
			t.Fatalf("compiled navigation is missing %q", marker)
		}
	}
	if strings.Contains(ControlPanelHTML, `class="panel-nav mb-5 overflow-x-auto`) {
		t.Fatal("navigation must not require horizontal scrolling")
	}
}

func TestControlPanelTabsUseMeaningfulGeometryNeutralStatuses(t *testing.T) {
	source := controlPanelTabsSource(t)

	for _, marker := range []string{
		`v-if="tabStatus(tab.id).visible"`,
		`:data-status-kind="tabStatus(tab.id).kind"`,
		`aria-label`,
		`aria-busy`,
		`aria-live="polite"`,
		`AWS setup running; lifecycle actions are locked`,
		`Linode setup running; lifecycle actions are locked`,
		`K3D operation running; K3D controls are locked`,
	} {
		if !strings.Contains(source, marker) {
			t.Fatalf("control panel tab source must expose a meaningful accessible status; missing %q", marker)
		}
	}

	for _, forbidden := range []string{`badgeSlot`, `data-empty`, `tab-count`, `badge("A"`, `badge("L"`} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("control panel tabs must not retain ambiguous or placeholder badge UI %q", forbidden)
		}
	}

	for _, marker := range []string{
		`tab-status--`,
		`data-operation-status`,
		`aria-busy`,
		`lifecycle actions are locked`,
	} {
		if !strings.Contains(ControlPanelHeaderVueJS, marker) {
			t.Fatalf("compiled control panel tabs must include geometry-neutral statuses; missing %q", marker)
		}
	}

	for _, css := range []string{
		`.tab-status {`,
		`position: absolute;`,
		`.tab-status--content {`,
		`.tab-status--busy {`,
		`animation: tab-status-pulse 1.35s ease-in-out infinite;`,
		`@media (prefers-reduced-motion: reduce) {`,
	} {
		if !strings.Contains(ControlPanelHTML, css) {
			t.Fatalf("control panel navigation status treatment is missing %q", css)
		}
	}

	if strings.Contains(ControlPanelHTML, `.tab-count`) {
		t.Fatal("control panel template must not reserve hidden count-badge geometry")
	}
}

func TestControlPanelNavigationHasResponsivePickerAndVisibleFocus(t *testing.T) {
	for _, css := range []string{
		`min-height: 2.75rem;`, `max-height: min(72vh, 44rem);`,
		`@media (max-width: 760px) {`, `overflow-y: auto;`,
		`.panel-nav button:focus-visible`, `.panel-picker-search:focus-within`,
		`.panel-nav-mobile`,
	} {
		if !strings.Contains(ControlPanelHTML, css) {
			t.Fatalf("responsive workspace navigation is missing %q", css)
		}
	}
}

func TestControlPanelClipboardUsesNativeWailsRuntime(t *testing.T) {
	for _, marker := range []string{
		"ClipboardSetText",
		"navigator.clipboard",
	} {
		if !strings.Contains(ControlPanelHeaderVueJS, marker) {
			t.Fatalf("compiled control panel clipboard helper is missing %q", marker)
		}
	}
}

func TestInteractiveSetupClipboardUsesNativeRuntimeWithBrowserFallback(t *testing.T) {
	for _, marker := range []string{
		"window.runtime?.ClipboardSetText",
		"window.runtime.ClipboardSetText(text)",
		"navigator.clipboard?.writeText",
		"navigator.clipboard.writeText(text)",
	} {
		if !strings.Contains(InteractiveSetupJS, marker) {
			t.Fatalf("setup-plan clipboard helper is missing %q", marker)
		}
	}

	if strings.Contains(InteractiveSetupJS, "ClipboardSetText(text.trim())") || strings.Contains(InteractiveSetupJS, "clipboard.writeText(text.trim())") {
		t.Fatal("setup-plan clipboard helper must preserve command text without trimming")
	}
}

func TestInteractiveSetupSupportsAutomaticDownstreamLinodePlans(t *testing.T) {
	for _, marker := range []string{
		`id="downstreamLinodePlansInput"`,
		`name="downstreamLinodePlans"`,
		`value="[]"`,
	} {
		if !strings.Contains(InteractiveSetupHTML, marker) {
			t.Fatalf("interactive setup template is missing downstream Linode form marker %q", marker)
		}
	}

	for _, marker := range []string{
		`config.downstreamLinodePlans`,
		`deploymentDownstreamLinodePlanSets`,
		`alignDownstreamLinodePlans`,
		`downstreamLinodePlans.push(normalizeDownstreamLinodePlan())`,
		`downstreamLinodePlans.splice(index, 1)`,
		`JSON.stringify(serializedPlans)`,
		`isDownstreamLinodeEligible = () => deploymentType === 'ha-rke2' && setupMode === 'auto'`,
		`enabled: Boolean(plan.enabled)`,
		`kubernetesVersion: ''`,
		`kubernetesVersion: String(plan.kubernetesVersion || '').trim()`,
		`distribution: 'k3s'`,
		`region: 'us-ord'`,
		`instanceType: 'g6-standard-2'`,
		`image: 'linode/ubuntu22.04'`,
		`Create a downstream Linode cluster after this Rancher is ready`,
		`A downstream failure does not delete Rancher.`,
		`Rancher default (resolved after readiness)`,
		`Pinned: ${normalized.kubernetesVersion}`,
		`downstreamLinodeKubernetesVersionMismatch`,
		`Needs attention: the pinned version does not match ${distribution}.`,
		`data-downstream-linode-kubernetes-version-index`,
		`data-downstream-linode-use-default-index`,
		`Setup preserved it instead of silently changing it; switch Distribution back to RKE2 or choose Use Rancher default before saving.`,
		`Switch Distribution or choose Use Rancher default before saving.`,
		`Customize downstream cluster`,
		`data-downstream-linode-plan-field="distribution"`,
		`data-downstream-linode-plan-field="region"`,
		`data-downstream-linode-plan-field="instanceType"`,
		`data-downstream-linode-plan-field="image"`,
		`aria-controls`,
		`aria-describedby`,
		`aria-expanded`,
		`aria-live="polite"`,
		`downstreamLinodeCatalogPromise`,
		`if (plan.enabled)`,
		`downstreamLinodePlans.some(plan => plan.enabled)`,
		`/api/linode-catalog?token=`,
		`payload?.regions`,
		`payload?.types`,
		`payload?.images`,
		`Known defaults remain available.`,
		`validateEnabledDownstreamLinodePlans`,
	} {
		if !strings.Contains(InteractiveSetupJS, marker) {
			t.Fatalf("interactive setup script is missing downstream Linode marker %q", marker)
		}
	}
}
