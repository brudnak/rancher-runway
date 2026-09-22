package ui

import (
	"bytes"
	"encoding/json"
	"html/template"
	"strings"
	"testing"

	"github.com/brudnak/ha-rancher-rke2/internal/buildinfo"
)

func TestControlPanelBootIncludesBuildBeforeStatusLoads(t *testing.T) {
	// The initial page must carry build metadata even if /api/status never succeeds.
	build := buildinfo.Info{Version: "1.1.0", BuildNumber: "42", Commit: "abc</script>def"}
	page := template.Must(template.New("panel").Parse(ControlPanelHTML))
	var output bytes.Buffer
	if err := page.Execute(&output, struct {
		Token           string
		Build           buildinfo.Info
		SetupEditorHTML template.HTML
	}{Token: "test-token", Build: build}); err != nil {
		t.Fatal(err)
	}
	_, jsonStart, ok := strings.Cut(output.String(), `<script type="application/json" id="control-panel-data">`)
	if !ok {
		t.Fatal("missing panel bootstrap data")
	}
	payload, _, ok := strings.Cut(jsonStart, "</script>")
	if !ok {
		t.Fatal("missing bootstrap script terminator")
	}
	var data struct {
		Token string         `json:"token"`
		Build buildinfo.Info `json:"build"`
	}
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		t.Fatalf("invalid or unsafely escaped bootstrap JSON: %v", err)
	}
	if data.Token != "test-token" || data.Build != build {
		t.Fatalf("bootstrap metadata changed: %#v", data)
	}
}
