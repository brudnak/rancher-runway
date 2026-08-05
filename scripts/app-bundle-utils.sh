#!/usr/bin/env bash

# Remove only a concrete macOS app bundle (or this installer's temporary app
# bundle) after restoring owner write/execute access to its directories.
# Packaged Rancher Runway runtimes are intentionally read-only, and plain
# `rm -rf` cannot traverse those directories on macOS.
rancher_runway_remove_app_tree() {
  local app_path="${1:-}"
  local app_basename

  if [[ -z "${app_path}" || "${app_path}" != /* || "${app_path}" == "/" ]]; then
    echo "Refusing to remove unsafe app bundle path: ${app_path:-<empty>}" >&2
    return 1
  fi

  app_basename="${app_path##*/}"
  if [[ ! "${app_basename}" =~ \.app$ && ! "${app_basename}" =~ ^\..+\.app\.tmp\.[0-9]+$ ]]; then
    echo "Refusing to remove non-app bundle path: ${app_path}" >&2
    return 1
  fi

  if [[ -L "${app_path}" ]]; then
    echo "Refusing to remove app bundle symlink: ${app_path}" >&2
    return 1
  fi
  if [[ ! -e "${app_path}" ]]; then
    return 0
  fi
  if [[ ! -d "${app_path}" ]]; then
    echo "Refusing to remove non-directory app bundle path: ${app_path}" >&2
    return 1
  fi

  # Directory permissions control unlinking. `find` does not follow internal
  # symlinks, so an app bundle cannot make an external target writable.
  find "${app_path}" -type d -exec chmod u+rwx {} +
  rm -rf -- "${app_path}"
}
