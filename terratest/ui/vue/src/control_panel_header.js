import { createApp } from "vue";
import ControlPanelChrome from "./ControlPanelChrome.vue";
import ControlPanelCommandDeck from "./ControlPanelCommandDeck.vue";
import ControlPanelTabs from "./ControlPanelTabs.vue";
import ControlPanelPanels from "./ControlPanelPanels.vue";
import ControlPanelModals from "./ControlPanelModals.vue";

const chromeMount = document.getElementById("controlPanelChromeVue");
const commandDeckMount = document.getElementById("commandDeck");
const tabsMount = document.getElementById("panelTabs");
const panelsMount = document.getElementById("controlPanelPanelsVue");
const modalsMount = document.getElementById("controlPanelModalsVue");

if (chromeMount) {
  createApp(ControlPanelChrome).mount(chromeMount);
}

if (commandDeckMount) {
  createApp(ControlPanelCommandDeck).mount(commandDeckMount);
}

if (tabsMount) {
  createApp(ControlPanelTabs).mount(tabsMount);
}

if (panelsMount) {
  createApp(ControlPanelPanels).mount(panelsMount);
}

if (modalsMount) {
  createApp(ControlPanelModals).mount(modalsMount);
}
