import { createApp } from "vue";
import AppIdentity from "./AppIdentity.vue";
import ControlPanelChrome from "./ControlPanelChrome.vue";
import ControlPanelTabs from "./ControlPanelTabs.vue";
import ControlPanelPanels from "./ControlPanelPanels.vue";
import ControlPanelModals from "./ControlPanelModals.vue";

const chromeMount = document.getElementById("controlPanelChromeVue");
const tabsMount = document.getElementById("panelTabs");
const panelsMount = document.getElementById("controlPanelPanelsVue");
const modalsMount = document.getElementById("controlPanelModalsVue");

const identityMount = document.getElementById("appIdentityVue");
if (identityMount) {
  createApp(AppIdentity).mount(identityMount);
}

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
