package ui

import _ "embed"

//go:embed templates/interactive_setup.html
var InteractiveSetupHTML string

//go:embed static/interactive_setup.js
var InteractiveSetupJS string

//go:embed templates/control_panel.html
var ControlPanelHTML string

//go:embed static/control_panel.js
var ControlPanelJS string

//go:embed static/control_panel_header_vue.js
var ControlPanelHeaderVueJS string

//go:embed static/control_panel_utils.js
var ControlPanelUtilsJS string

//go:embed static/control_panel_modals.js
var ControlPanelModalsJS string

//go:embed static/control_panel_runs.js
var ControlPanelRunsJS string

//go:embed static/control_panel_clusters.js
var ControlPanelClustersJS string

//go:embed static/control_panel.css
var ControlPanelCSS string
