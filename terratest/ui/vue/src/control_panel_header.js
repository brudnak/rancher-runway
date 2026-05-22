import { createApp } from "vue";
import ControlPanelHeader from "./ControlPanelHeader.vue";
import ControlPanelCommandDeck from "./ControlPanelCommandDeck.vue";

const headerMount = document.getElementById("controlPanelHeaderVue");
const commandDeckMount = document.getElementById("commandDeck");

if (headerMount) {
  createApp(ControlPanelHeader).mount(headerMount);
}

if (commandDeckMount) {
  createApp(ControlPanelCommandDeck).mount(commandDeckMount);
}
