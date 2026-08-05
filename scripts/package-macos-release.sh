#!/usr/bin/env bash
set -euo pipefail

umask 022

usage() {
  cat <<'EOF'
Usage: scripts/package-macos-release.sh <vMAJOR.MINOR.PATCH[-PRERELEASE]>

Builds a universal Rancher Runway app, signs and notarizes it, and writes a
DMG, ZIP, and SHA-256 checksum file to RANCHER_RUNWAY_OUTPUT_DIR (default:
dist).

Required for a distributable build:
  RANCHER_RUNWAY_SIGNING_IDENTITY       Developer ID Application identity

Use one of these notarization methods:
  RANCHER_RUNWAY_NOTARY_KEYCHAIN_PROFILE
  RANCHER_RUNWAY_NOTARY_KEYCHAIN        Optional keychain containing profile

or:
  APPLE_ID
  APPLE_TEAM_ID
  APPLE_APP_SPECIFIC_PASSWORD

Optional:
  RANCHER_RUNWAY_BUILD_NUMBER           Positive integer (default: CI run or 1)
  RANCHER_RUNWAY_BUILD_COMMIT           Commit identifier (default: GITHUB_SHA)
  RANCHER_RUNWAY_BUILD_DATE             UTC build timestamp
  RANCHER_RUNWAY_BUNDLE_ID              Default: com.brudnak.rancher-runway
  RANCHER_RUNWAY_MINIMUM_MACOS_VERSION  Default: 12.0
  RANCHER_RUNWAY_APP_NAME               Default: Rancher Runway
  RANCHER_RUNWAY_ARTIFACT_BASENAME       Default: Rancher-Runway
  RANCHER_RUNWAY_OUTPUT_DIR              Default: dist

For local packaging tests only, set RANCHER_RUNWAY_ALLOW_UNSIGNED=1. A signed
build may skip notarization only when RANCHER_RUNWAY_SKIP_NOTARIZATION=1.
Unsigned or unnotarized artifacts must not be published to Homebrew.
EOF
}

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "$1 is required"
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi
if [[ $# -gt 1 ]]; then
  usage >&2
  exit 2
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
version_input="${1:-${RANCHER_RUNWAY_VERSION:-}}"
[[ -n "${version_input}" ]] || die "a release version is required"

release_version="${version_input#v}"
if [[ ! "${release_version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z][0-9A-Za-z.-]*)?$ ]]; then
  die "version must look like v1.2.3 or v1.2.3-rc.1"
fi
marketing_version="${release_version%%-*}"

build_number="${RANCHER_RUNWAY_BUILD_NUMBER:-${GITHUB_RUN_NUMBER:-1}}"
[[ "${build_number}" =~ ^[1-9][0-9]*$ ]] || die "RANCHER_RUNWAY_BUILD_NUMBER must be a positive integer"

build_commit="${RANCHER_RUNWAY_BUILD_COMMIT:-${GITHUB_SHA:-unknown}}"
[[ "${build_commit}" =~ ^[0-9A-Za-z._-]+$ ]] || die "RANCHER_RUNWAY_BUILD_COMMIT contains unsupported characters"
build_date="${RANCHER_RUNWAY_BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
[[ "${build_date}" =~ ^[0-9TZ:+.-]+$ ]] || die "RANCHER_RUNWAY_BUILD_DATE contains unsupported characters"

app_name="${RANCHER_RUNWAY_APP_NAME:-Rancher Runway}"
bundle_id="${RANCHER_RUNWAY_BUNDLE_ID:-com.brudnak.rancher-runway}"
minimum_macos_version="${RANCHER_RUNWAY_MINIMUM_MACOS_VERSION:-12.0}"
artifact_basename="${RANCHER_RUNWAY_ARTIFACT_BASENAME:-Rancher-Runway}"
[[ "${app_name}" =~ ^[0-9A-Za-z][0-9A-Za-z._\ -]*$ ]] || die "RANCHER_RUNWAY_APP_NAME contains unsupported characters"
[[ "${bundle_id}" =~ ^[A-Za-z0-9][A-Za-z0-9.-]+$ ]] || die "RANCHER_RUNWAY_BUNDLE_ID is invalid"
[[ "${minimum_macos_version}" =~ ^[0-9]+\.[0-9]+(\.[0-9]+)?$ ]] || die "RANCHER_RUNWAY_MINIMUM_MACOS_VERSION is invalid"
[[ "${artifact_basename}" =~ ^[0-9A-Za-z][0-9A-Za-z._-]*$ ]] || die "RANCHER_RUNWAY_ARTIFACT_BASENAME is invalid"

output_dir="${RANCHER_RUNWAY_OUTPUT_DIR:-${repo_root}/dist}"
if [[ "${output_dir}" != /* ]]; then
  output_dir="${repo_root}/${output_dir}"
fi
mkdir -p "${output_dir}"

[[ "$(uname -s)" == "Darwin" ]] || die "macOS release packaging must run on macOS"
for command_name in go npm xcrun codesign ditto hdiutil lipo plutil rsync shasum; do
  require_command "${command_name}"
done
[[ -x /usr/libexec/PlistBuddy ]] || die "/usr/libexec/PlistBuddy is required"

temporary_root="$(mktemp -d "${TMPDIR:-/tmp}/rancher-runway-release.XXXXXX")"
cleanup() {
	chmod -R u+w "${temporary_root}" 2>/dev/null || true
  rm -rf "${temporary_root}"
}
trap cleanup EXIT

printf 'Building Rancher Runway %s (build %s)\n' "${release_version}" "${build_number}"
(cd "${repo_root}" && npm ci)
(cd "${repo_root}" && npm run build:panel-ui)
(cd "${repo_root}/desktop/wails/frontend" && npm ci)
(cd "${repo_root}/desktop/wails/frontend" && npm run build)

release_icon="${repo_root}/packaging/macos/AppIcon.icns"
release_plist="${repo_root}/packaging/macos/Info.release.plist"
[[ -f "${release_icon}" ]] || die "macOS app icon was not found at ${release_icon}"
[[ -f "${release_plist}" ]] || die "release Info.plist was not found at ${release_plist}"

ldflags="-X github.com/brudnak/ha-rancher-rke2/internal/buildinfo.Version=${release_version}"
ldflags+=" -X github.com/brudnak/ha-rancher-rke2/internal/buildinfo.BuildNumber=${build_number}"
ldflags+=" -X github.com/brudnak/ha-rancher-rke2/internal/buildinfo.Commit=${build_commit}"
ldflags+=" -X github.com/brudnak/ha-rancher-rke2/internal/buildinfo.BuildDate=${build_date}"
release_cgo_ldflags="${CGO_LDFLAGS:-}"
if [[ -n "${release_cgo_ldflags}" ]]; then
  release_cgo_ldflags+=" "
fi
release_cgo_ldflags+="-framework UniformTypeIdentifiers -mmacosx-version-min=${minimum_macos_version}"
release_cgo_cflags="${CGO_CFLAGS:-}"
if [[ -n "${release_cgo_cflags}" ]]; then
  release_cgo_cflags+=" "
fi
release_cgo_cflags+="-mmacosx-version-min=${minimum_macos_version}"
release_cgo_cxxflags="${CGO_CXXFLAGS:-}"
if [[ -n "${release_cgo_cxxflags}" ]]; then
  release_cgo_cxxflags+=" "
fi
release_cgo_cxxflags+="-mmacosx-version-min=${minimum_macos_version}"

app_path="${temporary_root}/${app_name}.app"
mkdir -p "${app_path}/Contents/MacOS" "${app_path}/Contents/Resources"
ditto "${release_plist}" "${app_path}/Contents/Info.plist"
ditto "${release_icon}" "${app_path}/Contents/Resources/iconfile.icns"

plist_path="${app_path}/Contents/Info.plist"
for app_arch in amd64 arm64; do
  (
    cd "${repo_root}/desktop/wails"
    GOOS=darwin GOARCH="${app_arch}" CGO_ENABLED=1 \
    CGO_CFLAGS="${release_cgo_cflags}" \
    CGO_CXXFLAGS="${release_cgo_cxxflags}" \
    CGO_LDFLAGS="${release_cgo_ldflags}" \
    MACOSX_DEPLOYMENT_TARGET="${minimum_macos_version}" \
    RANCHER_RUNWAY_PORTABLE_BUILD=1 \
    RANCHER_RUNWAY_VERSION="${release_version}" \
    RANCHER_RUNWAY_BUILD_NUMBER="${build_number}" \
      go build \
        -buildvcs=false \
        -trimpath \
        -tags 'desktop,wv2runtime.download,production' \
        -ldflags "${ldflags} -w -s" \
        -o "${temporary_root}/rancher-runway-app-${app_arch}" \
        .
  )
done
lipo -create \
  -output "${temporary_root}/rancher-runway-app-universal" \
  "${temporary_root}/rancher-runway-app-amd64" \
  "${temporary_root}/rancher-runway-app-arm64"
chmod 0755 "${temporary_root}/rancher-runway-app-universal"
executable_path="${app_path}/Contents/MacOS/${app_name}"
mv "${temporary_root}/rancher-runway-app-universal" "${executable_path}"

/usr/libexec/PlistBuddy -c "Set :CFBundleName ${app_name}" "${plist_path}"
/usr/libexec/PlistBuddy -c "Set :CFBundleDisplayName ${app_name}" "${plist_path}"
/usr/libexec/PlistBuddy -c "Set :CFBundleExecutable ${app_name}" "${plist_path}"
/usr/libexec/PlistBuddy -c "Set :CFBundleIdentifier ${bundle_id}" "${plist_path}"
/usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString ${marketing_version}" "${plist_path}"
/usr/libexec/PlistBuddy -c "Set :CFBundleVersion ${build_number}" "${plist_path}"
/usr/libexec/PlistBuddy -c "Set :LSMinimumSystemVersion ${minimum_macos_version}" "${plist_path}"
plutil -lint "${plist_path}"

architectures="$(lipo -archs "${executable_path}")"
[[ " ${architectures} " == *" arm64 "* ]] || die "the app executable is missing arm64"
[[ " ${architectures} " == *" x86_64 "* ]] || die "the app executable is missing x86_64"

runtime_stage="${temporary_root}/runtime"
mkdir -p "${runtime_stage}/bin"
printf '%s\n' "${release_version}" > "${runtime_stage}/.rancher-runway-runtime-version"
ditto "${repo_root}/go.mod" "${runtime_stage}/go.mod"

runtime_excludes=(
  --exclude .DS_Store
  --exclude .terraform
  --exclude .terraform.lock.hcl
  --exclude automation-output
  --exclude 'high-availability-*'
  --exclude '*.log'
  --exclude '*.tfstate'
  --exclude '*.tfstate.*'
  --exclude backend.env
  --exclude backend.tf
  --exclude terraform.tfvars
  --exclude 'tfplan*'
)
for runtime_directory in terratest modules bootstrap; do
  rsync -a "${runtime_excludes[@]}" \
    "${repo_root}/${runtime_directory}/" \
    "${runtime_stage}/${runtime_directory}/"
done

for helper_arch in amd64 arm64; do
  (
    cd "${repo_root}"
    GOOS=darwin GOARCH="${helper_arch}" CGO_ENABLED=1 \
    MACOSX_DEPLOYMENT_TARGET="${minimum_macos_version}" \
      go test -c -trimpath -o "${temporary_root}/rancher-runway-lifecycle-${helper_arch}" ./terratest
  )
done
lipo -create \
  -output "${runtime_stage}/bin/rancher-runway-lifecycle" \
  "${temporary_root}/rancher-runway-lifecycle-amd64" \
  "${temporary_root}/rancher-runway-lifecycle-arm64"
chmod 0755 "${runtime_stage}/bin/rancher-runway-lifecycle"
helper_architectures="$(lipo -archs "${runtime_stage}/bin/rancher-runway-lifecycle")"
[[ " ${helper_architectures} " == *" arm64 "* ]] || die "the lifecycle worker is missing arm64"
[[ " ${helper_architectures} " == *" x86_64 "* ]] || die "the lifecycle worker is missing x86_64"

unexpected_runtime_file="$(find "${runtime_stage}" -type f \( \
  -name 'tool-config.yml' -o \
  -name '*.tfstate' -o \
  -name '*.tfstate.*' -o \
  -name 'terraform.tfvars' -o \
  -name 'backend.env' \
\) -print -quit)"
[[ -z "${unexpected_runtime_file}" ]] || die "refusing to package runtime state or configuration: ${unexpected_runtime_file}"

packaged_runtime="${app_path}/Contents/Resources/runtime"
rm -rf "${packaged_runtime}"
ditto "${runtime_stage}" "${packaged_runtime}"
[[ -x "${packaged_runtime}/bin/rancher-runway-lifecycle" ]] || die "packaged lifecycle worker is missing"
[[ -f "${packaged_runtime}/.rancher-runway-runtime-version" ]] || die "packaged runtime version marker is missing"

signing_identity="${RANCHER_RUNWAY_SIGNING_IDENTITY:-}"
allow_unsigned="${RANCHER_RUNWAY_ALLOW_UNSIGNED:-0}"
skip_notarization="${RANCHER_RUNWAY_SKIP_NOTARIZATION:-0}"
if [[ -z "${signing_identity}" && "${allow_unsigned}" != "1" ]]; then
  die "RANCHER_RUNWAY_SIGNING_IDENTITY is required (or explicitly set RANCHER_RUNWAY_ALLOW_UNSIGNED=1 for a local-only artifact)"
fi

notary_auth=()
if [[ -n "${RANCHER_RUNWAY_NOTARY_KEYCHAIN_PROFILE:-}" ]]; then
  notary_auth+=(--keychain-profile "${RANCHER_RUNWAY_NOTARY_KEYCHAIN_PROFILE}")
  if [[ -n "${RANCHER_RUNWAY_NOTARY_KEYCHAIN:-}" ]]; then
    notary_auth+=(--keychain "${RANCHER_RUNWAY_NOTARY_KEYCHAIN}")
  fi
elif [[ -n "${APPLE_ID:-}" && -n "${APPLE_TEAM_ID:-}" && -n "${APPLE_APP_SPECIFIC_PASSWORD:-}" ]]; then
  notary_auth+=(--apple-id "${APPLE_ID}" --team-id "${APPLE_TEAM_ID}" --password "${APPLE_APP_SPECIFIC_PASSWORD}")
elif [[ -n "${signing_identity}" && "${skip_notarization}" != "1" ]]; then
  die "notarization credentials are required; configure a keychain profile or the APPLE_* environment variables"
fi

submit_for_notarization() {
  local artifact_path="$1"
  xcrun notarytool submit "${notary_auth[@]}" --wait --timeout 45m "${artifact_path}"
}

if [[ -n "${signing_identity}" ]]; then
  codesign --force --options runtime --timestamp --sign "${signing_identity}" \
    "${packaged_runtime}/bin/rancher-runway-lifecycle"
  codesign --verify --strict --verbose=2 \
    "${packaged_runtime}/bin/rancher-runway-lifecycle"
else
  codesign --force --options runtime --sign - \
    "${packaged_runtime}/bin/rancher-runway-lifecycle"
fi

manifest_temp="${temporary_root}/runtime.sha256"
(
  cd "${packaged_runtime}"
  find . -type f ! -name runtime.sha256 -print | LC_ALL=C sort | while IFS= read -r runtime_file; do
    runtime_hash="$(shasum -a 256 "${runtime_file}" | awk '{print $1}')"
    printf '%s  %s\n' "${runtime_hash}" "${runtime_file#./}"
  done
) > "${manifest_temp}"
mv "${manifest_temp}" "${packaged_runtime}/runtime.sha256"
[[ -s "${packaged_runtime}/runtime.sha256" ]] || die "packaged runtime SHA-256 manifest is empty"

# The app copies this tree to Application Support before using it. Keeping the
# signed source tree read-only prevents Go or another background process from
# adding files after the manifest and code signature have been created.
find "${packaged_runtime}" -type f -exec chmod a-w {} +
find "${packaged_runtime}" -type d -exec chmod a-w {} +

if [[ -n "${signing_identity}" ]]; then
  codesign --force --options runtime --timestamp --sign "${signing_identity}" "${app_path}"
  codesign --verify --deep --strict --verbose=2 "${app_path}"

  if [[ "${skip_notarization}" != "1" ]]; then
    notary_zip="${temporary_root}/notary-upload.zip"
    ditto -c -k --sequesterRsrc --keepParent "${app_path}" "${notary_zip}"
    submit_for_notarization "${notary_zip}"
    xcrun stapler staple "${app_path}"
    xcrun stapler validate "${app_path}"
  fi
else
  codesign --force --deep --options runtime --sign - "${app_path}"
  codesign --verify --deep --strict --verbose=2 "${app_path}"
  printf 'WARNING: producing unsigned local-only artifacts\n' >&2
fi

dmg_name="${artifact_basename}-${release_version}-macOS-universal.dmg"
zip_name="${artifact_basename}-${release_version}-macOS-universal.zip"
checksums_name="${artifact_basename}-${release_version}-checksums.txt"
dmg_path="${output_dir}/${dmg_name}"
zip_path="${output_dir}/${zip_name}"
checksums_path="${output_dir}/${checksums_name}"
rm -f "${dmg_path}" "${zip_path}" "${checksums_path}"

codesign --verify --deep --strict --verbose=2 "${app_path}"
ditto -c -k --sequesterRsrc --keepParent "${app_path}" "${zip_path}"

dmg_root="${temporary_root}/dmg-root"
mkdir -p "${dmg_root}"
ditto "${app_path}" "${dmg_root}/${app_name}.app"
ln -s /Applications "${dmg_root}/Applications"
hdiutil create \
  -quiet \
  -volname "${app_name}" \
  -srcfolder "${dmg_root}" \
  -format UDZO \
  -ov \
  "${dmg_path}"

# Catch any mutation between signing and archive construction before checksums
# or release metadata can be produced.
codesign --verify --deep --strict --verbose=2 "${app_path}"

if [[ -n "${signing_identity}" ]]; then
  codesign --force --timestamp --sign "${signing_identity}" "${dmg_path}"
  codesign --verify --verbose=2 "${dmg_path}"
  if [[ "${skip_notarization}" != "1" ]]; then
    submit_for_notarization "${dmg_path}"
    xcrun stapler staple "${dmg_path}"
    xcrun stapler validate "${dmg_path}"
  fi
fi

(
  cd "${output_dir}"
  shasum -a 256 "${dmg_name}" "${zip_name}" > "${checksums_name}"
)

printf 'Created %s\n' "${dmg_path}"
printf 'Created %s\n' "${zip_path}"
printf 'Created %s\n' "${checksums_path}"
