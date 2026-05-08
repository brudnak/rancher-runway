#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
app_name="${HA_RANCHER_APP_NAME:-Rancher HA RKE2}"
install_dir="${HA_RANCHER_INSTALL_DIR:-/Applications}"
source_app="${repo_root}/desktop/wails/build/bin/${app_name}.app"
target_app="${install_dir}/${app_name}.app"
temp_app="${install_dir}/.${app_name}.app.tmp.$$"

"${repo_root}/scripts/build-wails-app.sh"

if [[ ! -d "${source_app}" ]]; then
  echo "Built Wails app was not found at ${source_app}" >&2
  exit 1
fi

mkdir -p "${install_dir}"
if [[ -e "${target_app}" && ! -d "${target_app}" ]]; then
  echo "Refusing to replace non-app file at ${target_app}" >&2
  exit 1
fi

if [[ -d "${target_app}" ]]; then
  echo "Replacing existing ${target_app}"
else
  echo "Installing new ${target_app}"
fi

rm -rf "${temp_app}"
if command -v ditto >/dev/null 2>&1; then
  ditto "${source_app}" "${temp_app}"
else
  cp -R "${source_app}" "${temp_app}"
fi

rm -rf "${target_app}"
mv "${temp_app}" "${target_app}"
touch "${target_app}"

if command -v xattr >/dev/null 2>&1; then
  xattr -dr com.apple.quarantine "${target_app}" 2>/dev/null || true
fi

echo "Installed ${target_app}"
echo "Double-click it to open the native Rancher HA RKE2 control panel."

if [[ "${HA_RANCHER_KEEP_WAILS_BUILD_APP:-}" != "1" ]]; then
  rm -rf "${source_app}"
  echo "Removed build copy ${source_app}"
fi
