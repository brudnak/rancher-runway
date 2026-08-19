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

func TestControlPanelTabsUseStableLayoutTrack(t *testing.T) {
	if !strings.Contains(ControlPanelHeaderVueJS, "panel-tabs-track") {
		t.Fatal("compiled control panel tabs must render the stable navigation track")
	}

	stableTrackCSS := `.panel-tabs-track {
      display: flex;
      width: max-content;
      min-width: 100%;
      gap: 0.25rem;
    }`
	if !strings.Contains(ControlPanelHTML, stableTrackCSS) {
		t.Fatal("control panel navigation track must provide a stable max-content flex layout")
	}
}

func TestControlPanelTabsReserveBadgeGeometryBeforeFirstRefresh(t *testing.T) {
	source := controlPanelTabsSource(t)

	if !strings.Contains(source, `v-if="tab.badgeSlot"`) {
		t.Fatal("control panel tabs must always render fixed badge slots so refreshes cannot move the navigation")
	}

	badgeSlots := []struct {
		tab  string
		slot string
	}{
		{tab: "setup", slot: "micro"},
		{tab: "runs", slot: "count"},
		{tab: "clusters", slot: "count"},
		{tab: "aws", slot: "count"},
		{tab: "destroy", slot: "count"},
		{tab: "k3d", slot: "micro"},
		{tab: "steve", slot: "micro"},
	}
	for _, expected := range badgeSlots {
		tab := expected.tab
		definition := `id: "` + tab + `"`
		start := strings.Index(source, definition)
		if start < 0 {
			t.Fatalf("control panel tabs are missing the %q tab definition", tab)
		}
		end := strings.Index(source[start:], "},")
		if end < 0 {
			t.Fatalf("control panel tabs have an incomplete %q tab definition", tab)
		}
		entry := source[start : start+end]
		if !strings.Contains(entry, `badgeSlot: "`+expected.slot+`"`) {
			t.Fatalf("%q must reserve its fixed %q badge slot before the first refresh", tab, expected.slot)
		}
	}

	if strings.Contains(source, `badgeSlot: "wide"`) || strings.Contains(source, `badgeSlot: "compact"`) {
		t.Fatal("operational tabs must use micro status slots instead of reserving wide text pills")
	}

	for _, marker := range []string{
		"badgeSlot:`count`",
		"badgeSlot:`micro`",
		`data-empty`,
		`aria-hidden`,
		`aria-label`,
		`tab-count-loading`,
	} {
		if !strings.Contains(ControlPanelHeaderVueJS, marker) {
			t.Fatalf("compiled control panel tabs must retain stable badge slots; missing %q", marker)
		}
	}

	for _, css := range []string{
		`.tab-count--count {`,
		`width: 2rem;`,
		`.tab-count--micro {`,
		`width: 1.125rem;`,
		`padding-inline: 0;`,
		`.tab-count[data-empty="true"] {`,
		`visibility: hidden;`,
	} {
		if !strings.Contains(ControlPanelHTML, css) {
			t.Fatalf("control panel navigation badges must keep fixed geometry while empty; missing %q", css)
		}
	}
}

func TestControlPanelTabsUseCompactHitTargets(t *testing.T) {
	source := controlPanelTabsSource(t)
	if !strings.Contains(source, "rounded-lg px-3 py-1.5") {
		t.Fatal("control panel tab buttons must retain compact horizontal padding")
	}

	for _, css := range []string{
		`min-height: 2.75rem;`,
		`min-height: 1.125rem;`,
		`gap: 0.25rem;`,
	} {
		if !strings.Contains(ControlPanelHTML, css) {
			t.Fatalf("control panel navigation must retain its compact layout; missing %q", css)
		}
	}

	if strings.Contains(ControlPanelHTML, `.panel-tab:hover`) {
		t.Fatal("control panel tab hover must not shift the navigation geometry")
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
