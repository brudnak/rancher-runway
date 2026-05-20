#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "This installer builds the macOS Rancher Runway app and must be run on macOS." >&2
  exit 1
fi

if ! xcode-select -p >/dev/null 2>&1; then
  echo "Xcode Command Line Tools are required. Install them with: xcode-select --install" >&2
  exit 1
fi

echo "Building and installing Rancher Runway.app"
"${repo_root}/scripts/install-wails-app.sh"
