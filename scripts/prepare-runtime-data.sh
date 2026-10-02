#!/usr/bin/env bash
set -euo pipefail

repo_root="${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"

# Go ignores nested modules during ./... discovery and mod tidy. Runtime
# workspaces may contain downloaded Go source (including legacy modules with
# @version directories and no go.mod), but none of it is part of Runway.
# Repair older workspaces before Wails' binding generation, preserving caches.
for output_dir in "${repo_root}/automation-output" "${repo_root}/terratest/automation-output" "${repo_root}/runway-data" "${repo_root}/terratest/runway-data"; do
  mkdir -p -m 0700 "${output_dir}"
  if [[ -f "${output_dir}/go.mod" ]]; then
    continue
  fi
  (
    umask 077
    set -o noclobber
    printf '%s\n' '// Generated runtime data; excluded from Runway source discovery.' 'module rancher-runway.local/runtime-data' > "${output_dir}/go.mod"
  )
done
