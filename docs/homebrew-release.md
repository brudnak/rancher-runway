# Rancher Runway Homebrew Releases

Homebrew releases require an Apple Developer ID signature and Apple notarization.
The release workflow builds a universal app for Intel and Apple Silicon Macs
and publishes it through the project's Homebrew tap.

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

The Cask also installs Terraform, Helm 4, and `kubectl` for the core Rancher
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
major rather than requiring an exact match. Helm requires major 4, supplied by
the `helm` formula. Existing `helm@3` installations can remain alongside it;
Runway prefers the unversioned Homebrew Helm installation. See the
[requirements table](desktop-guide.md#requirements) for versions and the kubectl
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

Open **Rancher Runway** from Applications. For a release marked as notarized,
macOS verifies the developer and may ask you to confirm opening a downloaded
app. If macOS reports that it cannot verify the developer of that release,
check which version Homebrew installed and report the warning with its version.

Older releases may be marked **ad-hoc signed** in their release notes. If
you're using one of those versions, check for a newer signed release with
`brew update` and `brew upgrade --cask rancher-runway`. The release notes for
each older build describe its first-launch procedure.

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
as `v1.2.3-rc.1`. It uses GitHub-hosted macOS runners for these steps:

1. Builds an Intel and Apple Silicon universal Wails application and lifecycle
   worker.
2. Bundles the immutable runtime under
   `Contents/Resources/runtime`, including the worker at
   `runtime/bin/rancher-runway-lifecycle`, and records the release version and
   numeric CI build in the app metadata.
3. Signs the worker with Developer ID and the hardened runtime, writes the
   runtime SHA-256 manifest, then signs the outer app. The app verifies the
   manifest again after staging an upgrade.
4. Submits the signed app to Apple and saves its ZIP and submission ID as a
   workflow artifact. The next job waits for Apple to accept the app, attaches
   the ticket, checks Gatekeeper, and creates the distribution ZIP and signed
   DMG. It submits the DMG and saves both files with the DMG submission ID.
   The final job waits for acceptance, attaches the DMG ticket, and checks
   Gatekeeper before publishing.
5. Calculates SHA-256 sums for the finished artifacts and renders a Cask pinned
   to the DMG checksum.
6. Attests the artifacts and creates a draft GitHub Release before publishing
   it, so all assets are present when the release becomes visible.
7. For stable releases, optionally updates the Cask in a separate Homebrew tap
   through the GitHub contents API. Prerelease artifacts remain available on
   their GitHub Release and never replace the stable Cask.

GitHub artifact attestations identify the workflow that produced the files.

### When Apple takes longer

Each Apple review gets up to three hours per attempt. GitHub runs the whole
release, so your Mac can be offline once the release starts. To include the
Homebrew tap update while your Mac is offline, configure `HOMEBREW_TAP_TOKEN`.

If a wait times out, Apple continues processing the original submission. Open
the **same run** in Actions and select **Re-run failed jobs**, or retry
`make release RELEASE_VERSION=vX.Y.Z` with the exact version. The next attempt
uses the saved files and submission ID. GitHub retains these artifacts for up
to 30 days. Starting a new run or re-running all jobs builds and submits again.
For an Apple rejection, inspect the notarization log in the job output and
correct the cause before building a new release.

The workflow refuses to overwrite an existing GitHub Release. If a published
artifact is wrong, create a new patch release rather than replacing bytes at an
existing URL and invalidating the Cask checksum.

## One-time repository setup

1. Create a public `brudnak/homebrew-tap` repository with an initialized default
   branch. The release assets must also be publicly downloadable.
2. In the release repository's **Settings → Environments**, create
   `macos-release`. Add required reviewers if release publishing should require
   human approval.
3. Add the six Apple secrets listed under [Signing credentials](#signing-credentials)
   to the `macos-release` environment.
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
| `RANCHER_RUNWAY_BUNDLE_ID` | `com.brudnak.rancher-runway` |
| `HOMEBREW_TAP_REPOSITORY` | `brudnak/homebrew-tap` |
| `HOMEBREW_CASK_PATH` | `Casks/rancher-runway.rb` |

The tap repository needs only its normal Homebrew layout:

```text
homebrew-tap/
└── Casks/
    └── rancher-runway.rb
```

### Signing credentials

1. On the signing Mac, open Keychain Access and create a certificate signing
   request using **Certificate Assistant → Request a Certificate From a
   Certificate Authority**. Save the request to disk.
2. As the Apple Developer team's Account Holder, open **Certificates,
   Identifiers & Profiles → Certificates → +**, choose **Developer ID
   Application**, and upload the request. Download the issued certificate and
   double-click it on the same Mac. [Apple's Developer ID certificate instructions](https://developer.apple.com/help/account/certificates/create-developer-id-certificates)
3. Run `security find-identity -v -p codesigning` and copy the full
   `Developer ID Application: … (TEAMID)` name. In Keychain Access, open
   **My Certificates**, export that identity with its private key as a
   password-protected `.p12`, and store it outside the checkout.
4. Generate an app-specific password for notarization in your Apple Account
   under **Sign-In and Security → App-Specific Passwords**.
   [Apple's app-specific password instructions](https://support.apple.com/en-us/102654)
5. Save the six values below as GitHub Actions secrets in the release
   repository's **macos-release** environment.

| Secret | Purpose |
| --- | --- |
| `APPLE_DEVELOPER_ID_APPLICATION_CERT_BASE64` | Base64-encoded `.p12` containing the Developer ID Application certificate and private key. |
| `APPLE_DEVELOPER_ID_APPLICATION_CERT_PASSWORD` | Password used when exporting the `.p12`. |
| `APPLE_SIGNING_IDENTITY` | Full `Developer ID Application: ... (TEAMID)` identity shown by `security find-identity -v -p codesigning`. |
| `APPLE_ID` | Apple account used for notarization. |
| `APPLE_APP_SPECIFIC_PASSWORD` | App-specific password for that Apple account. |
| `APPLE_TEAM_ID` | Apple Developer team identifier. |

To send the certificate directly from a local export to its GitHub secret,
run the following with the path to your export. Use the environment's secret
fields for the other values.

```bash
base64 -i "/path/to/Developer-ID-Application.p12" | \
  gh secret set APPLE_DEVELOPER_ID_APPLICATION_CERT_BASE64 \
    --repo brudnak/rancher-runway --env macos-release
```

Missing credentials or failed verification stop the workflow before publication.

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

The workflow can also be dispatched manually for an existing tag. Select that
tag as the workflow ref and enter the same tag as its input so the build and
attestation identify the same source. For
prereleases such as `v1.1.0-rc.1`, use that workflow directly; the stable
`make release` command intentionally accepts only `MAJOR.MINOR.PATCH`.

## Local packaging

On a Mac with the repository's Go and Node.js build dependencies, Xcode Command
Line Tools, and the Developer ID Application identity in Keychain Access, store
your notarization credentials once. Replace the account and team ID below and
enter the app-specific password at the secure prompt.

```bash
xcrun notarytool store-credentials rancher-runway-release-notary \
  --apple-id "you@example.com" --team-id "YOURTEAMID"
```

Set the identity to the name from `security find-identity -v -p codesigning`.
Both release scripts default to `developer-id`. Use the intended release
version in place of the example.

```bash
RANCHER_RUNWAY_SIGNING_IDENTITY="Developer ID Application: Example (YOURTEAMID)" \
RANCHER_RUNWAY_NOTARY_KEYCHAIN_PROFILE="rancher-runway-release-notary" \
scripts/package-macos-release.sh v1.0.0

scripts/render-homebrew-cask.sh \
  v1.0.0 \
  dist/Rancher-Runway-1.0.0-macOS-universal.dmg
```

For local packaging tests without Apple credentials, explicitly set
`RANCHER_RUNWAY_SIGNING_MODE=adhoc` for both scripts. The rendered test Cask
includes the first-launch notice for that build.

Set `RANCHER_RUNWAY_RELEASE_REPOSITORY=owner/repository` when the release assets
are hosted outside `brudnak/rancher-runway`.

## Verify the first release

Before announcing the tap, test on a Mac without the source checkout or a
previous installation:

1. Install through the tap and confirm the DMG checksum and Terraform, Helm 4,
   `kubectl`, and GitHub CLI dependencies succeed.
2. Launch from Applications and confirm Gatekeeper accepts the app, the control
   panel opens, and lifecycle preflight works with the bundled worker.
3. Install a newer patch release with `brew upgrade --cask rancher-runway` and
   confirm configuration and run records remain in Application Support.
4. Check both Intel and Apple Silicon support before claiming both are tested.

See Apple's
[notarization guidance](https://developer.apple.com/documentation/security/notarizing-macos-software-before-distribution)
for signed releases, and the
[Homebrew Cask Cookbook](https://docs.brew.sh/Cask-Cookbook) for Cask conventions.
