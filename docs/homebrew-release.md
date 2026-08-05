# Rancher Runway Homebrew Releases

Rancher Runway is distributed as a signed, notarized universal macOS app in a
Homebrew Cask. Homebrew replaces only the app bundle during an upgrade, so the
workspace under `~/Library/Application Support/Rancher Runway` remains in
place. Releases currently require macOS 12 Monterey or newer, matching the
minimum supported by the Go 1.26 toolchain used by this repository.

## Install and upgrade

After the `brudnak/homebrew-tap` repository has been created and the first Cask
has been published:

```bash
brew install --cask brudnak/tap/rancher-runway
```

The Cask also installs Terraform, Helm 3, and `kubectl`, which are required for
the core Rancher lifecycle. Go is embedded in the signed lifecycle worker and
is only needed separately for the optional Steve Lab workflow.

Upgrade to the latest stable release with:

```bash
brew update
brew upgrade --cask rancher-runway
```

Normal uninstall also preserves application support data:

```bash
brew uninstall --cask rancher-runway
```

`brew uninstall --zap rancher-runway` additionally removes Rancher Runway's
application-support directory, caches, preferences, and saved window state.
That directory may contain Terraform state and cleanup metadata. Destroy or
otherwise account for live infrastructure before using `--zap`.

## Release flow

The [release workflow](../.github/workflows/release-macos.yml) runs for tags in
the form `vMAJOR.MINOR.PATCH` and also accepts SemVer prerelease suffixes such
as `v1.2.3-rc.1`. It performs these operations on a GitHub-hosted macOS runner:

1. Builds an Intel and Apple Silicon universal Wails application and lifecycle
   worker.
2. Bundles the immutable runtime under
   `Contents/Resources/runtime`, including the worker at
   `runtime/bin/rancher-runway-lifecycle`, writes a SHA-256 manifest for every
   runtime file, then records the release version and numeric CI build in the
   app metadata. The app verifies that manifest again after staging an upgrade.
3. Signs the app with a Developer ID Application certificate and hardened
   runtime, notarizes it with `notarytool`, and staples the ticket.
4. Creates and notarizes a DMG, creates a ZIP, and calculates SHA-256 sums.
5. Renders a Cask containing the exact DMG checksum.
6. Attests the artifacts and creates a draft GitHub Release before publishing
   it, so all assets are present when the release becomes visible.
7. For stable releases, optionally updates the Cask in a separate Homebrew tap
   through the GitHub contents API. Prerelease artifacts remain available on
   their GitHub Release and never replace the stable Cask.

The workflow refuses to overwrite an existing GitHub Release. If a published
artifact is wrong, create a new patch release rather than replacing bytes at an
existing URL and invalidating the Cask checksum.

## One-time repository setup

Create a protected GitHub environment named `macos-release`. Configure these
environment secrets; none of their values belong in this repository:

| Secret | Purpose |
| --- | --- |
| `APPLE_DEVELOPER_ID_APPLICATION_CERT_BASE64` | Base64-encoded `.p12` containing the Developer ID Application certificate and private key. |
| `APPLE_DEVELOPER_ID_APPLICATION_CERT_PASSWORD` | Password used when exporting the `.p12`. |
| `APPLE_SIGNING_IDENTITY` | Full `Developer ID Application: ... (TEAMID)` identity shown by `security find-identity -v -p codesigning`. |
| `APPLE_ID` | Apple account used for notarization. |
| `APPLE_APP_SPECIFIC_PASSWORD` | App-specific password for that Apple account. |
| `APPLE_TEAM_ID` | Apple Developer team identifier. |
| `HOMEBREW_TAP_TOKEN` | Optional fine-grained GitHub token with Contents write access to the tap repository. |

The release repository itself is published with its scoped `GITHUB_TOKEN`.
The optional tap token is needed only because `brudnak/homebrew-tap` is a
different repository. If it is omitted, the release still contains a rendered
`rancher-runway.rb` for manual placement in the tap.

The defaults can be changed with GitHub environment or repository variables:

| Variable | Default |
| --- | --- |
| `RANCHER_RUNWAY_BUNDLE_ID` | `com.brudnak.rancher-runway` |
| `HOMEBREW_TAP_REPOSITORY` | `brudnak/homebrew-tap` |
| `HOMEBREW_CASK_PATH` | `Casks/rancher-runway.rb` |

The tap repository needs only its normal Homebrew layout:

```text
homebrew-tap/
└── Casks/
    └── rancher-runway.rb
```

Add required reviewers to the `macos-release` environment if publishing a tag
should require a human approval.

## Publish a release

Create and push a tag that identifies the application release. The tag push
starts the workflow automatically:

```text
v0.1.0
v0.2.0-rc.1
```

The workflow can also be dispatched manually for an existing tag. It verifies
the tag format and refuses to synthesize or overwrite a tag or existing
release.

## Local packaging

The packaging script can use credentials already stored in a local keychain:

```bash
xcrun notarytool store-credentials rancher-runway-release-notary \
  --apple-id "you@example.com" \
  --team-id "YOURTEAMID" \
  --password "YOUR_APP_SPECIFIC_PASSWORD"

RANCHER_RUNWAY_SIGNING_IDENTITY="Developer ID Application: Example (YOURTEAMID)" \
RANCHER_RUNWAY_NOTARY_KEYCHAIN_PROFILE="rancher-runway-release-notary" \
scripts/package-macos-release.sh v0.1.0
```

Render the Cask from the resulting DMG with:

```bash
scripts/render-homebrew-cask.sh \
  v0.1.0 \
  dist/Rancher-Runway-0.1.0-macOS-universal.dmg
```

Set `RANCHER_RUNWAY_RELEASE_REPOSITORY=owner/repository` when the release assets
are hosted outside `brudnak/rancher-runway`.

Apple requires Developer ID signing, hardened runtime, and notarization for
software distributed outside the Mac App Store. Homebrew Casks use a versioned
URL and exact SHA-256 for downloaded artifacts. See Apple's
[notarization guidance](https://developer.apple.com/documentation/security/notarizing-macos-software-before-distribution),
the [Wails build reference](https://wails.io/docs/reference/cli/), and the
[Homebrew Cask Cookbook](https://docs.brew.sh/Cask-Cookbook) for the underlying
distribution conventions.
