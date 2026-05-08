import "./style.css";

const frame = document.querySelector("#panelFrame");
const loadingShell = document.querySelector("#loadingShell");
const status = document.querySelector("#status");

const setStatus = (message, error = false) => {
  status.textContent = message;
  status.dataset.error = error ? "true" : "false";
};

const waitForPanelStatus = async () => {
  for (let attempt = 0; attempt < 120; attempt += 1) {
    const panelStatus = window.go?.main?.App?.PanelStatus;
    if (panelStatus) {
      return panelStatus;
    }
    await new Promise(resolve => window.setTimeout(resolve, 100));
  }
  throw new Error("Wails did not expose the Rancher HA panel bridge.");
};

const attachPanel = async () => {
  try {
    const panelStatus = await waitForPanelStatus();
    setStatus("Starting the local Go panel and attaching this native window.");
    const result = await panelStatus();

    if (result?.error) {
      throw new Error(result.error);
    }
    if (!result?.url) {
      throw new Error("The local control panel did not return a URL.");
    }

    frame.addEventListener("load", () => {
      loadingShell.hidden = true;
      frame.hidden = false;
    }, { once: true });
    frame.src = result.url;
    setStatus("Opening the control panel.");
  } catch (error) {
    setStatus(error instanceof Error ? error.message : String(error), true);
  }
};

void attachPanel();
