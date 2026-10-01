package ui

import (
	"bytes"
	"html/template"
	"strings"
	"testing"

	"github.com/brudnak/ha-rancher-rke2/internal/buildinfo"
	"golang.org/x/net/html"
)

// Setup is server-rendered, outside the Vue panel mount. It must remain present
// and respond directly to the shared workspace state, including in WebKit.
func TestEmbeddedSetupNavigationVisibility(t *testing.T) {
	page := template.Must(template.New("panel").Parse(ControlPanelHTML))
	var output bytes.Buffer
	err := page.Execute(&output, struct {
		Token           string
		Build           buildinfo.Info
		SetupEditorHTML template.HTML
	}{Token: "test-token", SetupEditorHTML: template.HTML(`<div id="interactiveSetupRoot"><input id="setup-version" value="v2.16.0"></div>`)})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := html.Parse(strings.NewReader(output.String()))
	if err != nil {
		t.Fatal(err)
	}
	var panel *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		for _, a := range n.Attr {
			if a.Key == "id" && a.Val == "setupTabPanel" {
				panel = n
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	if panel == nil {
		t.Fatal("embedded Setup panel is missing")
	}
	for _, a := range panel.Attr {
		if a.Key == "hidden" || a.Key == "style" {
			t.Fatal("Setup must not carry stale imperative visibility state")
		}
		if a.Key == "class" {
			for _, c := range strings.Fields(a.Val) {
				if c == "hidden" {
					t.Fatal("Setup cannot retain a hidden class after workspace selection")
				}
			}
		}
	}
	for _, rule := range []string{`[data-tab-panel="setup"] { display: none; }`, `body[data-workspace="setup"] [data-tab-panel="setup"] { display: block; }`} {
		if !strings.Contains(output.String(), rule) {
			t.Fatalf("missing Setup visibility rule: %s", rule)
		}
	}
	var content bytes.Buffer
	if err := html.Render(&content, panel); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content.String(), `id="setup-version" value="v2.16.0"`) {
		t.Fatal("embedded form was lost during rendering")
	}
}
