# Rancher Runway Homebrew Releases

Rancher Runway's release pipeline builds a universal macOS app for the
project's own Homebrew tap. By default, the app and lifecycle worker are
**ad-hoc signed and not notarized by Apple**. This requires no paid Apple
Developer membership or Apple secrets. Users may need to approve the app on
first launch. Developer ID signing and notarization are available as an
explicit opt-in later.

Homebrew replaces only the app bundle during an upgrade, so the workspace
under `~/Library/Application Support/Rancher Runway` remains in place. Builds
require macOS 12 Monterey or newer, matching the minimum supported by the Go
1.26 toolchain used by this repository.

## Install and upgrade

After the `brudnak/homebrew-tap` repository has been created and the first Cask
has been published:

```bash
brew tap hashicorp/tap
brew trust --formula hashicorp/tap/terraform
brew install --cask brudnak/tap/rancher-runway
```

The first two commands add HashiCorp's tap and trust its Terraform formula.
Homebrew requires explicit trust for dependencies from non-official taps;
installing Rancher Runway does not grant trust to Terraform automatically.
See [Homebrew's tap trust instructions](https://docs.brew.sh/Tap-Trust).

The Cask also installs Terraform, Helm 3, and `kubectl` for the core Rancher
lifecycle, plus GitHub CLI (`gh`) for Issue Radar and PR Image Check. Sign in once
with `gh auth login` to use GitHub features. Go is embedded in the lifecycle worker and
is only needed separately for the optional Steve Lab workflow.

### Switching from `make setup`

Both `make setup` and Homebrew default to `/Applications/Rancher Runway.app`.
If the development copy is present, wait for active operations to finish, quit
it, and rename it to `Rancher Runway Dev.app` or move it out of Applications
before installing the Cask. A copy installed with `INSTALL_DIR` on the Desktop
can remain there. Open the Homebrew copy in Applications to use the release.

Keep the development checkout and its configuration, Terraform state, and run
records. Development builds use that checkout; packaged releases use a managed
workspace under `~/Library/Application Support/Rancher Runway`. The first
release launch does not import development runs. Use the development copy to
manage those existing runs. Subsequent Homebrew upgrades preserve the managed
workspace and its state.

To bring over settings, open **Setup → Import config file** in the Homebrew app
and choose the checkout's `tool-config.yml`. Review the detected sections,
choose **Back up & import**, then **Continue with imported config**. The app
keeps a private `.tool-config-before-import-*.yml` backup beside its managed config.
The source file is unchanged. Backups also survive later app upgrades and can be
restored with the same importer.

The Setup checklist identifies missing values and opens the matching fields.
**Tools & credentials** runs the local readiness checks. Importing does not
resolve a plan or create infrastructure, and is blocked while an operation or
plan resolution is in progress. This copies settings only; it does not move
Terraform state, existing runs, or shell credentials.

Readiness uses current baselines and accepts newer versions within the supported
major rather than requiring an exact match. Helm remains on major 3. See the
[requirements table](../README.md#requirements) for versions and the kubectl
client/server compatibility constraint.

Homebrew's [`--adopt` option](https://docs.brew.sh/Manpage#install-options-formulacask-)
is for matching existing artifacts; a development build should be moved aside
before installing a release.

### Identifying a build

The app displays its version and build number in a persistent top-left bar,
warning and confirmation dialogs, error notifications, and native close
warnings. Metadata is included in the initial page, so it remains available
even if status loading fails. Hover over a web version badge for the commit
and build date when available. Unversioned source builds show `v0.0.0-dev`.

Version labels come from the release tag rather than a hardcoded UI version.
After publishing source changes, use `make release` for the next patch or
`make release RELEASE_BUMP=minor` for the next minor. An explicit version can
be selected with `RELEASE_VERSION` as shown below.

Status, preflight, and Setup readiness requests time out after 30 seconds.
The header reports failed status checks with a retry action and keeps the last
successful snapshot visible. **Refresh checks** refreshes all three checks;
Setup also has its own readiness refresh button. These deadlines apply to
read-only checks, not provisioning or cleanup operations. Background status
polls keep the page and Setup controls stable; loading feedback on the Refresh
button appears only for a requested refresh. Lifecycle changes still update
safety locks immediately after the next successful status response.

### First launch

Open Rancher Runway from Applications. If macOS blocks it because the
developer cannot be verified or Apple cannot check it for malicious software,
and you trust the downloaded release:

1. Try opening the app once, then dismiss the alert.
2. Open **System Settings → Privacy & Security**.
3. Click **Open Anyway**, confirm **Open**, and authenticate if prompted.

This creates an exception for this app using Apple's
[documented approval process](https://support.apple.com/en-us/102445). Updates
may require approval again. Managed Macs may restrict this option; users on
those machines may need their administrator or a notarized release.

The generated Cask and GitHub Release notes include these instructions for
ad-hoc builds. The installer leaves macOS quarantine and Gatekeeper settings
intact. Ad-hoc signing does not establish an Apple-verified publisher identity
or provide Apple's notarization check. These builds are intended for our own
tap and do not meet the official `homebrew/cask`
[Gatekeeper requirements](https://docs.brew.sh/Acceptable-Casks).

### Upgrade and uninstall

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
   `runtime/bin/rancher-runway-lifecycle`, and records the release version and
   numeric CI build in the app metadata.
3. Ad-hoc signs the worker, writes a SHA-256 manifest for every runtime file,
   then signs the outer app bundle without modifying the worker. The app
   verifies the manifest again after staging an upgrade. In `developer-id`
   mode, uses a Developer ID
   Application certificate and hardened runtime, notarizes the app with
   `notarytool`, and staples the ticket instead.
4. Creates a DMG and ZIP and calculates SHA-256 sums. In `developer-id` mode,
   also signs, notarizes, and staples the DMG before calculating its checksum.
5. Renders a Cask containing the exact DMG checksum and, for ad-hoc builds,
   first-launch instructions.
6. Attests the artifacts and creates a draft GitHub Release before publishing
   it, so all assets are present when the release becomes visible.
7. For stable releases, optionally updates the Cask in a separate Homebrew tap
   through the GitHub contents API. Prerelease artifacts remain available on
   their GitHub Release and never replace the stable Cask.

GitHub artifact attestations record build provenance; they are separate from
Apple notarization and do not remove macOS first-launch prompts.

The workflow refuses to overwrite an existing GitHub Release. If a published
artifact is wrong, create a new patch release rather than replacing bytes at an
existing URL and invalidating the Cask checksum.

## One-time repository setup

1. Create a public `brudnak/homebrew-tap` repository with an initialized default
   branch. The release assets must also be publicly downloadable.
2. In the release repository's **Settings → Environments**, create
   `macos-release`. Add required reviewers if release publishing should require
   human approval.
3. Leave `RANCHER_RUNWAY_SIGNING_MODE` unset or set it to `adhoc`. No Apple
   secrets are needed in this mode.
4. Optionally add an environment secret named `HOMEBREW_TAP_TOKEN`: a
   fine-grained GitHub token with Contents write access to the tap repository.
   This enables automatic stable Cask updates.

The release repository itself is published with its scoped `GITHUB_TOKEN`.
The optional tap token is needed because `brudnak/homebrew-tap` is a separate
repository. Without it, download `rancher-runway.rb` from the published release
and place it at `Casks/rancher-runway.rb` in the tap. A successful release does
not make `brew install` work until that Cask is available in the tap.

The defaults can be changed with GitHub environment or repository variables:

| Variable | Default |
| --- | --- |
| `RANCHER_RUNWAY_SIGNING_MODE` | `adhoc`; the other accepted value is `developer-id`. |
| `RANCHER_RUNWAY_BUNDLE_ID` | `com.brudnak.rancher-runway` |
| `HOMEBREW_TAP_REPOSITORY` | `brudnak/homebrew-tap` |
| `HOMEBREW_CASK_PATH` | `Casks/rancher-runway.rb` |

The tap repository needs only its normal Homebrew layout:

```text
homebrew-tap/
└── Casks/
    └── rancher-runway.rb
```

### Optional Developer ID signing and notarization

For notarized releases, use an Apple Developer Program membership (currently
[$99 USD per year](https://developer.apple.com/support/compare-memberships/))
and set the environment or repository variable
`RANCHER_RUNWAY_SIGNING_MODE=developer-id`. Add all six environment secrets:

| Secret | Purpose |
| --- | --- |
| `APPLE_DEVELOPER_ID_APPLICATION_CERT_BASE64` | Base64-encoded `.p12` containing the Developer ID Application certificate and private key. |
| `APPLE_DEVELOPER_ID_APPLICATION_CERT_PASSWORD` | Password used when exporting the `.p12`. |
| `APPLE_SIGNING_IDENTITY` | Full `Developer ID Application: ... (TEAMID)` identity shown by `security find-identity -v -p codesigning`. |
| `APPLE_ID` | Apple account used for notarization. |
| `APPLE_APP_SPECIFIC_PASSWORD` | App-specific password for that Apple account. |
| `APPLE_TEAM_ID` | Apple Developer team identifier. |

The workflow imports Apple credentials only in `developer-id` mode. Missing
credentials, signing failures, or notarization failures stop that release;
they never fall back to ad-hoc signing. Merely adding Apple secrets does not
switch the release mode.

## Publish a release

From the application checkout, with Python 3 and an authenticated GitHub CLI:

```bash
make release-plan                         # read-only preflight and version preview
make release                              # first release v1.0.0; then bump patch
make release RELEASE_BUMP=minor           # for example v1.0.3 -> v1.1.0
make release RELEASE_BUMP=major           # for example v1.1.0 -> v2.0.0
make release RELEASE_VERSION=v1.0.0       # explicit version, or retry that release
```

The command releases source already published on GitHub's `main` branch.
Use `RELEASE_REF=branch-or-commit` to choose a different published ref. Before
creating a tag, it compares locally available tracked source and release
tooling with that ref and stops on differences. Publish all intended source,
including new files, before releasing: this command does not commit or upload
application changes and never invokes the Git CLI.

Versions follow [SemVer](https://semver.org/): patch for compatible fixes,
minor for compatible features, and major for breaking changes to supported
configuration or user-facing behavior. The first stable version is `v1.0.0`.
Later automatic bumps use the highest existing stable version tag, including
tags reserved by unfinished releases. Prerelease tags do not affect this
calculation. The version tag supplies the app's embedded version, bundle
metadata, artifact filenames, GitHub Release, and Cask version; there is no
separate version file to keep in sync.

`make release` creates the version tag through the GitHub API and waits for
the macOS workflow to build, test, and publish the artifacts and generated
release notes. It then downloads the DMG and Cask, verifies their version,
URL, and SHA-256 match, and updates the tap through the contents API. **That
tap update creates a commit in `brudnak/homebrew-tap`.** It skips an identical
Cask and refuses to downgrade a newer tap version. No local checkout is
updated automatically.

The GitHub CLI login needs Contents write access to both repositories and
Actions write access to `rancher-runway` for retries. A fine-grained token can
be scoped to those two repositories. This local command can update the tap
using your existing CLI login, so `HOMEBREW_TAP_TOKEN` is optional; that secret
is useful when publishing directly from GitHub without the local command.

If a build fails or waiting is interrupted, rerun with the same explicit
`RELEASE_VERSION`. Existing tags are preserved. If the release is already
published, the command only verifies the artifacts and completes the tap
update; it never replaces published release assets. An existing draft or
prerelease with that version must be resolved manually before retrying.

The workflow can also be dispatched manually for an existing tag. For
prereleases such as `v1.1.0-rc.1`, use that workflow directly; the stable
`make release` command intentionally accepts only `MAJOR.MINOR.PATCH`.

## Local packaging

On a Mac with the repository's Go and Node.js build dependencies and Xcode
Command Line Tools, the default build needs no Apple account:

```bash
scripts/package-macos-release.sh v1.0.0
scripts/render-homebrew-cask.sh \
  v1.0.0 \
  dist/Rancher-Runway-1.0.0-macOS-universal.dmg
```

Both scripts default `RANCHER_RUNWAY_SIGNING_MODE` to `adhoc`. Use the same
mode for packaging and Cask rendering so the installation notice matches the
artifact. This setting replaces the older `RANCHER_RUNWAY_ALLOW_UNSIGNED` and
`RANCHER_RUNWAY_SKIP_NOTARIZATION` switches; `developer-id` releases always
require notarization.

For a notarized build, the packaging script can use credentials stored in a
local keychain:

```bash
xcrun notarytool store-credentials rancher-runway-release-notary \
  --apple-id "you@example.com" \
  --team-id "YOURTEAMID" \
  --password "YOUR_APP_SPECIFIC_PASSWORD"

RANCHER_RUNWAY_SIGNING_MODE=developer-id \
RANCHER_RUNWAY_SIGNING_IDENTITY="Developer ID Application: Example (YOURTEAMID)" \
RANCHER_RUNWAY_NOTARY_KEYCHAIN_PROFILE="rancher-runway-release-notary" \
scripts/package-macos-release.sh v1.0.0
```

Render its Cask with the matching mode:

```bash
RANCHER_RUNWAY_SIGNING_MODE=developer-id scripts/render-homebrew-cask.sh \
  v1.0.0 \
  dist/Rancher-Runway-1.0.0-macOS-universal.dmg
```

Set `RANCHER_RUNWAY_RELEASE_REPOSITORY=owner/repository` when the release assets
are hosted outside `brudnak/rancher-runway`.

## Verify the first release

Before announcing the tap, test on a Mac without the source checkout or a
previous installation:

1. Install through the tap and confirm the DMG checksum and Terraform, Helm 3,
   `kubectl`, and GitHub CLI dependencies succeed.
2. Launch from Applications, follow the per-app approval step if required,
   and confirm the control panel and lifecycle preflight work with the bundled
   worker.
3. Install a newer patch release with `brew upgrade --cask rancher-runway` and
   confirm configuration and run records remain in Application Support.
4. Check both Intel and Apple Silicon support before claiming both are tested.

See Apple's
[notarization guidance](https://developer.apple.com/documentation/security/notarizing-macos-software-before-distribution)
for optional Developer ID releases, and the
[Homebrew Cask Cookbook](https://docs.brew.sh/Cask-Cookbook) for Cask conventions.
