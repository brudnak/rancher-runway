import { createApp } from "vue";
import ControlPanelHeader from "./ControlPanelHeader.vue";
import ControlPanelCommandDeck from "./ControlPanelCommandDeck.vue";
import ControlPanelTabs from "./ControlPanelTabs.vue";
import AwsInventoryPanel from "./AwsInventoryPanel.vue";
import CostHistoryPanel from "./CostHistoryPanel.vue";
import PreflightPanel from "./PreflightPanel.vue";
import SettingsPanel from "./SettingsPanel.vue";
import K3DLabPanel from "./K3DLabPanel.vue";
import SteveLabPanel from "./SteveLabPanel.vue";
import WorkspaceRunsPanel from "./WorkspaceRunsPanel.vue";

const headerMount = document.getElementById("controlPanelHeaderVue");
const commandDeckMount = document.getElementById("commandDeck");
const tabsMount = document.getElementById("panelTabs");
const awsInventoryMount = document.getElementById("awsInventoryVue");
const costHistoryMount = document.getElementById("costHistoryVue");
const preflightMount = document.getElementById("preflightVue");
const settingsMount = document.getElementById("settingsVue");
const k3dLabMount = document.getElementById("k3dLabVue");
const steveLabMount = document.getElementById("steveLabVue");
const workspaceMount = document.getElementById("workspaceVue");

if (headerMount) {
  createApp(ControlPanelHeader).mount(headerMount);
}

if (commandDeckMount) {
  createApp(ControlPanelCommandDeck).mount(commandDeckMount);
}

if (tabsMount) {
  createApp(ControlPanelTabs).mount(tabsMount);
}

if (awsInventoryMount) {
  createApp(AwsInventoryPanel).mount(awsInventoryMount);
}

if (costHistoryMount) {
  createApp(CostHistoryPanel).mount(costHistoryMount);
}

if (preflightMount) {
  createApp(PreflightPanel).mount(preflightMount);
}

if (settingsMount) {
  createApp(SettingsPanel).mount(settingsMount);
}

if (k3dLabMount) {
  createApp(K3DLabPanel).mount(k3dLabMount);
}

if (steveLabMount) {
  createApp(SteveLabPanel).mount(steveLabMount);
}

if (workspaceMount) {
  createApp(WorkspaceRunsPanel).mount(workspaceMount);
}
