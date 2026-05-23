module.exports = {
  content: [
    './terratest/ui/templates/control_panel.html',
    './terratest/ui/templates/interactive_setup.html',
    './terratest/ui/static/control_panel.js',
    './terratest/ui/static/control_panel_clusters.js',
    './terratest/ui/static/control_panel_runs.js',
    './terratest/ui/static/control_panel_utils.js',
    './terratest/ui/static/interactive_setup.js',
    './terratest/ui/vue/src/**/*.{js,vue}'
  ],
  darkMode: 'class',
  theme: {
    extend: {}
  },
  plugins: []
}
