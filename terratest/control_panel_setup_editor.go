package test

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"

	"github.com/brudnak/ha-rancher-rke2/terratest/ui"
	"github.com/spf13/viper"
)

const controlPanelSetupEditorBasePath = "/setup-editor"

func (p *localControlPanel) newSetupEditor() *interactiveServer {
	return &interactiveServer{
		token:      p.token,
		configPath: p.configPath,
		phase:      phaseEditor,
		responseHandler: func(action string, plans []*RancherResolvedPlan) error {
			if action != "continue" {
				return nil
			}
			if len(plans) == 0 {
				return fmt.Errorf("setup plan must resolve before starting AWS")
			}
			p.totalHAs = viper.GetInt("total_has")
			if p.totalHAs < 1 {
				p.totalHAs = len(plans)
			}
			return p.startIsolatedRun()
		},
	}
}

func (p *localControlPanel) registerSetupEditorHandlers(mux *http.ServeMux) {
	if p.setupEditor == nil {
		p.setupEditor = p.newSetupEditor()
	}

	versions := currentPreflightVersions()
	for len(versions) < 1 {
		versions = append(versions, "")
	}
	p.setupEditor.registerHandlersAt(mux, versions, controlPanelSetupEditorBasePath)
}

func (p *localControlPanel) renderSetupEditorHTML() (template.HTML, error) {
	versions := currentPreflightVersions()
	for len(versions) < 1 {
		versions = append(versions, "")
	}

	data := interactiveSetupTemplateDataFor(p.token, p.configPath, versions, controlPanelSetupEditorBasePath, true)
	pageTemplate, err := template.New("interactive-setup").Parse(ui.InteractiveSetupHTML)
	if err != nil {
		return "", err
	}

	var output bytes.Buffer
	if err := pageTemplate.ExecuteTemplate(&output, "interactive_setup_content", data); err != nil {
		return "", err
	}
	return template.HTML(output.String()), nil
}
