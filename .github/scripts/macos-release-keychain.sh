#!/usr/bin/env bash
set -euo pipefail
umask 077

keychain_path="${RUNNER_TEMP:?}/rancher-runway-signing.keychain-db"
certificate_path="$RUNNER_TEMP/rancher-runway-developer-id.p12"
case "${1:-}" in
  cleanup)
    security delete-keychain "$keychain_path" 2>/dev/null || true
    rm -f "$certificate_path"
    exit 0
    ;;
  signing|notary-only) mode="$1" ;;
  *) echo "Expected signing, notary-only, or cleanup" >&2; exit 2 ;;
esac

required=(APPLE_ID APPLE_APP_SPECIFIC_PASSWORD APPLE_TEAM_ID)
if [[ "$mode" == signing ]]; then
  required+=(APPLE_DEVELOPER_ID_APPLICATION_CERT_BASE64 APPLE_DEVELOPER_ID_APPLICATION_CERT_PASSWORD APPLE_SIGNING_IDENTITY)
fi
for name in "${required[@]}"; do
  if [[ -z "${!name:-}" ]]; then
    echo "Required macos-release secret '$name' is not configured" >&2
    exit 1
  fi
done
xcrun --find notarytool >/dev/null
xcrun --find stapler >/dev/null

keychain_password="$(openssl rand -base64 32)"
security create-keychain -p "$keychain_password" "$keychain_path"
security set-keychain-settings -lut 21600 "$keychain_path"
security unlock-keychain -p "$keychain_password" "$keychain_path"
if [[ "$mode" == signing ]]; then
  printf '%s' "$APPLE_DEVELOPER_ID_APPLICATION_CERT_BASE64" | base64 --decode > "$certificate_path"
  security import "$certificate_path" -P "$APPLE_DEVELOPER_ID_APPLICATION_CERT_PASSWORD" \
    -T /usr/bin/codesign -t cert -f pkcs12 -k "$keychain_path"
  rm -f "$certificate_path"
  security set-key-partition-list -S apple-tool:,apple: -s -k "$keychain_password" "$keychain_path"
  # Keep the runner's existing keychains available for certificate chain lookup.
  search_list=("$keychain_path")
  current_list="$(security list-keychains -d user)"
  while read -r path; do
    path="${path#\"}"
    path="${path%\"}"
    if [[ -n "$path" && "$path" != "$keychain_path" ]]; then
      search_list+=("$path")
    fi
  done <<< "$current_list"
  security list-keychains -d user -s "${search_list[@]}"
  if ! security find-identity -v -p codesigning "$keychain_path" | grep -F -- "\"$APPLE_SIGNING_IDENTITY\"" >/dev/null; then
    echo "Imported certificate and private key do not match a valid APPLE_SIGNING_IDENTITY" >&2
    exit 1
  fi
  echo "RANCHER_RUNWAY_SIGNING_IDENTITY=$APPLE_SIGNING_IDENTITY" >> "$GITHUB_ENV"
fi

profile=rancher-runway-release-notary
xcrun notarytool store-credentials "$profile" --apple-id "$APPLE_ID" \
  --password "$APPLE_APP_SPECIFIC_PASSWORD" --team-id "$APPLE_TEAM_ID" --keychain "$keychain_path"
{
  echo "RANCHER_RUNWAY_NOTARY_KEYCHAIN_PROFILE=$profile"
  echo "RANCHER_RUNWAY_NOTARY_KEYCHAIN=$keychain_path"
} >> "$GITHUB_ENV"
