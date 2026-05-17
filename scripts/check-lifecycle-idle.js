#!/usr/bin/env node

const { readFileSync } = require("node:fs");
const { spawnSync } = require("node:child_process");

const statusPath = process.argv[2];

if (!statusPath) {
  console.error("usage: node scripts/check-lifecycle-idle.js /path/to/status.json");
  process.exit(2);
}

const status = JSON.parse(readFileSync(statusPath, "utf8"));
const operations = Object.entries(status.operations || {}).filter(([, operation]) => operation?.running);

if (operations.length === 0) {
  process.exit(0);
}

const lines = operations
  .map(([name, operation]) => {
    const details = [];
    if (operation.runId) {
      details.push(`run ${operation.runId}`);
    }
    if (operation.pid) {
      details.push(`pid ${operation.pid}`);
    }
    return details.length ? `- ${name} (${details.join(", ")})` : `- ${name}`;
  })
  .join("\n");

const message = `Lifecycle processes are still running:\n${lines}\n\nWait for them to finish before rebuilding and replacing the app.`;
console.error(message);

if (process.platform === "darwin" && process.env.HA_RANCHER_SKIP_INSTALL_ALERT !== "1") {
  spawnSync(
    "osascript",
    ["-e", `display alert "Rancher HA RKE2 is busy" message ${JSON.stringify(message)} as warning`],
    { stdio: "ignore", timeout: 15000 },
  );
}

process.exit(1);
