import { createApp } from "vue";
import ControlPanelHeader from "./ControlPanelHeader.vue";

const mount = document.getElementById("controlPanelHeaderVue");

if (mount) {
  createApp(ControlPanelHeader).mount(mount);
}
