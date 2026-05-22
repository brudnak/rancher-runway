import { createApp } from "vue";
import ControlPanelHeader from "./ControlPanelHeader.vue";
import ControlPanelCommandDeck from "./ControlPanelCommandDeck.vue";
import ControlPanelTabs from "./ControlPanelTabs.vue";
import AwsInventoryPanel from "./AwsInventoryPanel.vue";
import CostHistoryPanel from "./CostHistoryPanel.vue";

const headerMount = document.getElementById("controlPanelHeaderVue");
const commandDeckMount = document.getElementById("commandDeck");
const tabsMount = document.getElementById("panelTabs");
const awsInventoryMount = document.getElementById("awsInventoryVue");
const costHistoryMount = document.getElementById("costHistoryVue");

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
