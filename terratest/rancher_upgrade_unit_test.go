package test

import (
	"strings"
	"testing"
	"time"
)

func TestRancherUpgradeFailureDiagnosticsAreBoundedAndSafe(t *testing.T) {
	if rancherUpgradeDiagnosticTimeout <= 0 || rancherUpgradeDiagnosticTimeout > 15*time.Second {
		t.Fatalf("diagnostic timeout = %s, want a positive timeout no longer than 15s", rancherUpgradeDiagnosticTimeout)
	}

	diagnostics := rancherUpgradeFailureDiagnostics()
	if len(diagnostics) == 0 {
		t.Fatal("expected Rancher upgrade diagnostics")
	}

	wantTitles := map[string]bool{
		"Helm client version":               false,
		"Rancher Helm release history":      false,
		"Rancher Helm release status":       false,
		"pre-upgrade hook job":              false,
		"pre-upgrade hook pods":             false,
		"pre-upgrade hook job description":  false,
		"pre-upgrade hook pod descriptions": false,
		"pre-upgrade hook logs":             false,
		"recent cattle-system events":       false,
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.maxLines <= 0 || diagnostic.maxLines > rancherUpgradeDiagnosticLineLimit {
			t.Errorf("diagnostic %q maxLines = %d, want 1..%d", diagnostic.title, diagnostic.maxLines, rancherUpgradeDiagnosticLineLimit)
		}
		if diagnostic.name != "helm" && diagnostic.name != "kubectl" {
			t.Errorf("diagnostic %q executable = %q, want helm or kubectl", diagnostic.title, diagnostic.name)
		}

		command := strings.ToLower(strings.Join(append([]string{diagnostic.name}, diagnostic.args...), " "))
		for _, forbidden := range []string{"get hooks", "secret", "bootstrap"} {
			if strings.Contains(command, forbidden) {
				t.Errorf("diagnostic %q contains forbidden command content %q: %s", diagnostic.title, forbidden, command)
			}
		}
		if _, expected := wantTitles[diagnostic.title]; expected {
			wantTitles[diagnostic.title] = true
		}
	}

	for title, found := range wantTitles {
		if !found {
			t.Errorf("missing diagnostic %q", title)
		}
	}
}

func TestFormatRancherUpgradeDiagnosticOutputRedactsAndBounds(t *testing.T) {
	t.Setenv("RANCHER_BOOTSTRAP_PASSWORD", "upgrade-super-secret")

	output := formatRancherUpgradeDiagnosticOutput(
		"discarded line\nvisible upgrade-super-secret value\nlast line\n",
		2,
	)
	if strings.Contains(output, "upgrade-super-secret") {
		t.Fatalf("expected sensitive value to be redacted, got %q", output)
	}
	if output != "visible *** value\nlast line" {
		t.Fatalf("formatRancherUpgradeDiagnosticOutput() = %q, want redacted trailing lines", output)
	}
}

func TestFormatRancherUpgradeDiagnosticOutputHandlesEmptyOutput(t *testing.T) {
	if got := formatRancherUpgradeDiagnosticOutput("\n", 20); got != "(no output)" {
		t.Fatalf("formatRancherUpgradeDiagnosticOutput() = %q, want no-output marker", got)
	}
}
