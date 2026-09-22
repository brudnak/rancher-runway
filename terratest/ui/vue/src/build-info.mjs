export function buildLabel(build = {}) {
  const version = String(build.version || "").trim().replace(/^v/i, "");
  if (!version) return "Version unavailable";
  const number = String(build.buildNumber ?? "").trim();
  return `v${version}${build.modified ? "*" : ""}${number ? ` · build ${number}` : ""}`;
}

export function buildTitle(build = {}) {
  return [
    `Rancher Runway ${buildLabel(build)}`,
    build.commit ? `Commit: ${build.commit}` : "",
    build.buildDate ? `Built: ${build.buildDate}` : "",
    build.modified ? "Built with local changes." : "",
  ].filter(Boolean).join("\n");
}
