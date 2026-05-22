import { createApp } from "vue";
import ControlPanelHeader from "./ControlPanelHeader.vue";
import ControlPanelCommandDeck from "./ControlPanelCommandDeck.vue";
import ControlPanelTabs from "./ControlPanelTabs.vue";

const headerMount = document.getElementById("controlPanelHeaderVue");
const commandDeckMount = document.getElementById("commandDeck");
const tabsMount = document.getElementById("panelTabs");

if (headerMount) {
  createApp(ControlPanelHeader).mount(headerMount);
}

if (commandDeckMount) {
  createApp(ControlPanelCommandDeck).mount(commandDeckMount);
}

if (tabsMount) {
  createApp(ControlPanelTabs).mount(tabsMount);
}
