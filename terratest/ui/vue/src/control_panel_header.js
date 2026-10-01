import { createApp } from "vue";
import ControlPanelChrome from "./ControlPanelChrome.vue";
import ControlPanelTabs from "./ControlPanelTabs.vue";
import ControlPanelPanels from "./ControlPanelPanels.vue";
import ControlPanelModals from "./ControlPanelModals.vue";

const chromeMount = document.getElementById("controlPanelChromeVue");
const tabsMount = document.getElementById("panelTabs");
const panelsMount = document.getElementById("controlPanelPanelsVue");
const modalsMount = document.getElementById("controlPanelModalsVue");

if (chromeMount) {
  createApp(ControlPanelChrome).mount(chromeMount);
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
