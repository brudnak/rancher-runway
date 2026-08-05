#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/render-homebrew-cask.sh <version> <release.dmg> [output.rb]

Renders the checksum-pinned Rancher Runway Cask for a GitHub Release asset.

Optional:
  RANCHER_RUNWAY_RELEASE_REPOSITORY  Default: GITHUB_REPOSITORY or brudnak/rancher-runway
EOF
}

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi
if [[ $# -lt 2 || $# -gt 3 ]]; then
  usage >&2
  exit 2
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
template_path="${repo_root}/packaging/homebrew/Casks/rancher-runway.rb.tmpl"
version_input="$1"
dmg_path="$2"
output_path="${3:-${repo_root}/dist/rancher-runway.rb}"

release_version="${version_input#v}"
if [[ ! "${release_version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z][0-9A-Za-z.-]*)?$ ]]; then
  die "version must look like v1.2.3 or v1.2.3-rc.1"
fi
release_tag="v${release_version}"
release_repository="${RANCHER_RUNWAY_RELEASE_REPOSITORY:-${GITHUB_REPOSITORY:-brudnak/rancher-runway}}"
[[ "${release_repository}" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || die "RANCHER_RUNWAY_RELEASE_REPOSITORY must be owner/repository"
[[ -f "${dmg_path}" ]] || die "DMG was not found at ${dmg_path}"
[[ -f "${template_path}" ]] || die "Cask template was not found at ${template_path}"

asset_name="$(basename "${dmg_path}")"
[[ "${asset_name}" =~ ^[0-9A-Za-z][0-9A-Za-z._+-]*$ ]] || die "DMG filename contains unsupported characters"
sha256="$(shasum -a 256 "${dmg_path}" | awk '{print $1}')"
[[ "${sha256}" =~ ^[0-9a-f]{64}$ ]] || die "could not calculate the DMG SHA-256"

mkdir -p "$(dirname "${output_path}")"
sed \
  -e "s|@VERSION@|${release_version}|g" \
  -e "s|@SHA256@|${sha256}|g" \
  -e "s|@RELEASE_REPOSITORY@|${release_repository}|g" \
  -e "s|@RELEASE_TAG@|${release_tag}|g" \
  -e "s|@ASSET_NAME@|${asset_name}|g" \
  "${template_path}" > "${output_path}"

printf 'Rendered %s\n' "${output_path}"
