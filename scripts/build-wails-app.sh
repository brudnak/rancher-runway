#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

resolve_git_dir() {
  local dot_git="${repo_root}/.git"
  if [[ -d "${dot_git}" ]]; then
    printf '%s\n' "${dot_git}"
    return 0
  fi
  if [[ -f "${dot_git}" ]]; then
    local gitdir
    gitdir="$(sed -n 's/^gitdir: //p' "${dot_git}" | head -n 1)"
    if [[ -n "${gitdir}" && "${gitdir}" != /* ]]; then
      gitdir="${repo_root}/${gitdir}"
    fi
    if [[ -d "${gitdir}" ]]; then
      printf '%s\n' "${gitdir}"
      return 0
    fi
  fi
  return 1
}

resolve_build_commit() {
  local git_dir
  git_dir="$(resolve_git_dir 2>/dev/null || true)"
  if [[ -z "${git_dir}" || ! -f "${git_dir}/HEAD" ]]; then
    return 0
  fi

  local head
  head="$(tr -d '[:space:]' < "${git_dir}/HEAD")"
  if [[ "${head}" =~ ^[0-9a-fA-F]{40}$ ]]; then
    printf '%s\n' "${head}"
    return 0
  fi
  if [[ "${head}" != ref:* ]]; then
    return 0
  fi

  local ref="${head#ref:}"
  if [[ -f "${git_dir}/${ref}" ]]; then
    tr -d '[:space:]' < "${git_dir}/${ref}"
    printf '\n'
    return 0
  fi
  if [[ -f "${git_dir}/packed-refs" ]]; then
    awk -v ref="${ref}" '$2 == ref { print $1; exit }' "${git_dir}/packed-refs"
  fi
}

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
  RANCHER_RUNWAY_ICON_PNG_OUT="${icon_png}" "${repo_root}/scripts/render-macos-icon.swift"
fi

(
  cd "${repo_root}/desktop/wails"
  build_ldflags="-X 'main.bundledRepoRoot=${repo_root}'"
  build_commit="${RANCHER_RUNWAY_BUILD_COMMIT:-${HA_RANCHER_BUILD_COMMIT:-$(resolve_build_commit)}}"
  build_date="${RANCHER_RUNWAY_BUILD_DATE:-${HA_RANCHER_BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}}"
  if [[ -z "${build_commit}" ]]; then
    build_commit="auto"
  fi
  build_ldflags="${build_ldflags} -X 'github.com/brudnak/ha-rancher-rke2/internal/buildinfo.Commit=${build_commit}'"
  build_ldflags="${build_ldflags} -X 'github.com/brudnak/ha-rancher-rke2/internal/buildinfo.BuildDate=${build_date}'"
  RANCHER_RUNWAY_REPO="${repo_root}" HA_RANCHER_REPO="${repo_root}" "${wails_bin}" build -ldflags "${build_ldflags}"
)

echo "Built ${repo_root}/desktop/wails/build/bin/Rancher Runway.app"
