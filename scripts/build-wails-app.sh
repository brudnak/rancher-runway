#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "This build target creates a macOS app and must be run on macOS." >&2
  exit 1
fi

if ! command -v go >/dev/null 2>&1; then
  echo "Go is required. Install the Go version requested by go.mod, then rerun this script." >&2
  exit 1
fi

if ! command -v npm >/dev/null 2>&1; then
  echo "Node.js/npm is required. Install Node.js, then rerun this script." >&2
  exit 1
fi

if [[ -z "${WAILS_BIN:-}" ]]; then
  if command -v wails >/dev/null 2>&1; then
    wails_bin="$(command -v wails)"
  else
    wails_bin="$(go env GOPATH)/bin/wails"
  fi
else
  wails_bin="${WAILS_BIN}"
fi

if [[ ! -x "${wails_bin}" ]]; then
  echo "Installing Wails CLI to ${wails_bin}"
  wails_version="$(cd "${repo_root}" && go list -m -f '{{.Version}}' github.com/wailsapp/wails/v2 2>/dev/null || true)"
  go install "github.com/wailsapp/wails/v2/cmd/wails@${wails_version:-latest}"
fi

if [[ ! -x "${wails_bin}" ]]; then
  echo "Wails CLI was not found at ${wails_bin} after installation." >&2
  exit 1
fi

if [[ ! -x "${repo_root}/node_modules/.bin/tailwindcss" ]]; then
  (cd "${repo_root}" && npm install)
fi
(cd "${repo_root}" && npm run build:panel-css)

printf '%s\n' "${repo_root}" > "${repo_root}/desktop/wails/repo_hint.txt"

icon_png="${repo_root}/desktop/wails/build/appicon.png"
if [[ "$(uname -s)" == "Darwin" && -x "${repo_root}/scripts/render-macos-icon.swift" ]]; then
  HA_RANCHER_ICON_PNG_OUT="${icon_png}" "${repo_root}/scripts/render-macos-icon.swift"
fi

(
  cd "${repo_root}/desktop/wails"
  HA_RANCHER_REPO="${repo_root}" "${wails_bin}" build -ldflags "-X 'main.bundledRepoRoot=${repo_root}'"
)

echo "Built ${repo_root}/desktop/wails/build/bin/Rancher HA RKE2.app"
