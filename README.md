# Rancher Runway

Rancher Runway is a macOS desktop app for launching disposable Rancher test
environments and cleaning them up afterward. The app is the intended way to use
this repo: it wraps setup, readiness checks, logs, kubeconfigs, cloud inventory,
cost hints, and destroy actions in a local control panel.

Lower-level CLI and guarded Go test entrypoints exist for debugging and
automation, but they are not the recommended day-to-day workflow. See
[Advanced Usage](docs/advanced-usage.md) when you need those paths.

For repository-owned GitHub Actions automation, see [docs/README.md](docs/README.md).

## What The App Builds

- AWS RKE2 Rancher management clusters: single-server, 3-server HA, or 5-server HA
- Optional hosted/tenant K3s runs: one host Rancher plus tenant Ranchers on imported K3s clusters
- Optional Linode Docker runs: one standalone Rancher Docker install per requested Rancher version
- AWS Kubernetes ingress with ALB, ACM certificates, Route53 records, and external TLS termination
- Linode Docker DNS with Route53 records
- Local k3d clusters for desktop-only Kubernetes API endpoints
- Local Steve endpoints for trying Steve tags, branches, or commits against k3d
- Local kubeconfigs, install artifacts, run records, lifecycle logs, cloud inventory, and cost history

RKE2 installer scripts and optional image bundles are checksum-verified before
use. The setup path does not use `curl | bash`.

## Install The Desktop App

The supported installer for Apple Silicon and Intel macOS is Homebrew:

```bash
brew install --cask brudnak/tap/rancher-runway
```

The Cask installs the signed, notarized universal app plus Terraform, Helm 3,
and `kubectl`. The app contains its own signed lifecycle worker, so ordinary
setup, readiness, and cleanup runs do not require a source checkout, Go,
Node.js, or Xcode.

Upgrade in place with:

```bash
brew update
brew upgrade --cask rancher-runway
```

App updates preserve configuration, Terraform state, kubeconfigs, logs, and run
records under `~/Library/Application Support/Rancher Runway`. Do not use
`brew uninstall --zap rancher-runway` until all live infrastructure has been
destroyed; `--zap` intentionally removes that cleanup state.

### Build From Source

Contributors can still build and install the current checkout:

```bash
make setup
```

That development path embeds a checkout hint and requires Xcode Command Line
Tools, the Go version from [go.mod](go.mod), and Node.js/npm. Re-run `make setup`
to replace the development app, or set `INSTALL_DIR` to install elsewhere.

```bash
make setup INSTALL_DIR="$HOME/Desktop"
```

## Requirements

- macOS 12 Monterey or newer with Homebrew
- AWS credentials and Route53 inputs for AWS or Linode DNS provisioning
- Linode API token for Linode Docker runs
- Go, Git, Docker, and k3d only for the optional Steve Lab workflow

## First Run

After Homebrew finishes, open the macOS Applications folder and look for
`Rancher Runway`. Launching the app opens the desktop control panel.

If `tool-config.yml` does not exist, the app creates a private starter config in
its Application Support workspace. Fill in the blocked values from the Setup
and preflight screens before starting a run.

Common environment variables can live in your shell profile:

```bash
cat <<'EOF' >> ~/.zprofile
export AWS_ACCESS_KEY_ID="your-aws-access-key"
export AWS_SECRET_ACCESS_KEY="your-aws-secret-key"
export LINODE_TOKEN="optional-linode-token-for-linode-docker"
export DOCKERHUB_USERNAME="optional-dockerhub-username"
export DOCKERHUB_PASSWORD="optional-dockerhub-password"
EOF
```

Provisioning validates the optional Docker Hub credential pair before writing
RKE2 registry configuration. Rejected credentials are ignored and the run falls
back to anonymous Docker Hub pulls.

Then open the profile with your preferred editor and replace the placeholder
values:

```bash
open -R ~/.zprofile
```

Restart the app after changing shell credentials so new launches inherit them.

## Desktop Workflow

Use the app tabs as the main lifecycle:

- **Setup** resolves a plan, checks local prerequisites, lets you choose AWS
  RKE2, hosted/tenant K3s, or Linode Docker mode, and starts provisioning after
  review.
- **Runs** shows recorded run slots, active operations, per-run folders, logs,
  Terraform paths, hostnames, and destroy shortcuts.
- **Clusters** shows Rancher URLs, kubeconfig paths or Linode IPs, reachability,
  pod visibility, recent logs, and active leader details.
- **AWS Inventory** shows resources associated with recorded slots and owner
  tags.
- **Image Lookup** searches Rancher server, agent, and webhook tags across
  Docker Hub and the Rancher/SUSE registries, or inspects a custom image
  repository.
- **PR Image Check** resolves a GitHub pull request commit and checks whether
  Rancher head images across all known registries declare that commit in their
  source ancestry.
- **Destroy** removes provisioned cloud resources for a selected run slot.
- **Costs** shows cleanup estimates and the local cost ledger.
- **Settings** holds local app preferences such as GPU reminders.
- **K3D Lab** starts and stops local k3d clusters without provisioning cloud
  infrastructure.
- **Steve Lab** starts a local Steve API endpoint against k3d for quick Steve
  version checks.

The app protects active work:

- Closing the app is blocked while setup, readiness, or cleanup is running.
- Homebrew upgrades replace only the app bundle; the managed workspace and
  version-matched lifecycle worker remain available to an in-flight release.
- Development `make setup` installs refuse to replace an open app.
- Setup, readiness, and cleanup operations are serialized where shared state
  would collide.

### Image Lookup

Image Lookup is a read-only registry browser for finding and comparing Rancher
builds without a Docker daemon or `skopeo` command line workflow.

- Search Docker Hub, `stgregistry.suse.com`, `registry.rancher.com`, and
  `registry.suse.com` together or one at a time.
- Browse `rancher/rancher`, `rancher/rancher-agent`, and
  `rancher/rancher-webhook`, or paste a custom public HTTPS repository.
- Prime-head lookup distinguishes a mutable patch selector such as
  `2.15.1-head` from an immutable tag such as
  `v2.15.1-<7-to-40-character-hex-SHA>-head`; the leading `v` is optional for
  either form. A patch selector is a lookup instruction, not a literal image
  tag.
- Resolving a patch selector searches only `stgregistry.suse.com` for matching
  immutable `rancher/rancher` tags and requires the same exact tag on
  `rancher/rancher-agent` in that registry. It does not combine components from
  different registries or fall back to another registry.
- An eligible Prime-head pair must declare role-correct server and agent
  repositories in `org.opensuse.reference`, with the same exact canonical tag,
  the canonical Rancher Prime source on the server, and a full
  `org.opencontainers.image.oss.revision` whose prefix matches the SHA in the
  tag. Both images must also declare OCI creation times.
  Complete pairs are ranked by the later of those two times, with the exact tag
  breaking a tie. A registry lookup error fails the resolution closed instead
  of silently selecting a possibly stale pair.
- Filter by release channel, architecture, Prime-head status, mutable or
  immutable head kind, version or patch line, commit fragment, and pair
  verification status. Sort by version and tag, registry upload time when
  available, or verified pair completion rank. An exact tagged or digest
  reference can also be inspected directly without first finding it in a tag
  result page.
- Ordinary browsing defaults to **Fast · first matching tags**, a bounded scan
  that stops after the 200-row per-source result limit. Choose an explicit
  version, tag, upload-time, or pair-completion sort when complete global
  ordering is more important than speed. Prime-pair verification and date
  filtering still scan the complete candidate set needed for a correct answer.
- Entering a bare patch such as `2.15.1` offers the corresponding
  `v2.15.1-head` Prime lookup. **Base tags only** hides architecture-suffixed
  variants when the unsuffixed selector is what matters.
- **Last 30 days** is an opt-in evidence-based filter. It uses verified pair
  completion time first, then registry upload time or inspected image creation
  time. Images with no reliable timestamp are excluded and counted rather than
  assigned a guessed date. OCI tag listings do not include timestamps or
  guarantee newest-first order, so this filter cannot avoid the initial
  staging tag scan and is not enabled by default.
- Select a tag to inspect its digest, image creation time, platforms,
  configuration, labels, environment, entrypoint, OCI build history, layers,
  and sizes. Upload time is separate registry metadata: Docker Hub may provide
  it, while the other registries commonly leave it unavailable. For Rancher
  images, the selected platform's webhook version is highlighted in the
  overview when it is declared in the image environment.
- Architecture-suffixed tags retain their base-tag association but remain
  distinct registry tags. Inspect the image manifest's platform list for the
  authoritative architectures; a suffix alone does not prove that the base tag
  is a multi-platform image. Opening an architecture-suffixed row automatically
  selects its matching inspect platform, so an ARM64-only index is not queried
  as `linux/amd64`.
- `rancher/rancher-webhook` remains a separately searchable image family and is
  never counted as one half of a Prime server/agent pair.
- When an image contains `build.yaml`, the detail drawer safely reads the
  bounded eligible image layers and renders both a structured view and the
  original YAML. Oversized layers are reported as skipped instead of being
  downloaded without a safety bound.
- If the embedded scan is incomplete but the image declares an exact GitHub
  source repository and commit, an explicit **Fetch from declared source**
  action can retrieve that revision's root `build.yaml` through the configured
  GitHub CLI login. Canonical GitHub clone URLs ending in `.git` are supported;
  Rancher Prime images can fall back to the public `rancher/rancher` repository
  only when the image declares an exact OSS commit in
  `org.opencontainers.image.oss.revision`. The UI shows the effective
  repository, revision, and provenance rather than claiming the file was
  embedded in the image.

Registry authentication uses credentials already available through the local
container registry credential helpers. Docker Hub can also use
`DOCKERHUB_USERNAME` and `DOCKERHUB_PASSWORD` from the app environment.
Private declared-source metadata additionally requires an authenticated `gh`
CLI session with access to the repository; Rancher Runway never asks the
browser for a GitHub token.

### PR Image Check

PR Image Check answers whether a pull request's Git commit is represented in a
specific Rancher head image such as `2.14-head`.

- Paste an exact `https://github.com/{owner}/{repository}/pull/{number}` URL
  and enter `head` or a minor-line head tag such as `2.14-head`.
- For a merged PR, the verifier uses GitHub's integration commit. For an open
  or otherwise unmerged PR, it checks the current PR head commit and labels the
  result accordingly.
- The tool inspects the exact Rancher server and agent tag in SUSE staging,
  Rancher Prime, SUSE Registry, and Docker Hub. Missing tags and registry
  access errors remain isolated so evidence from the other registries is still
  shown.
- Each result records the observed OCI digest, selected `linux/amd64` manifest
  digest, build label, source repository, and declared Git revision. GitHub's
  commit comparison establishes whether that revision is the selected PR
  commit or a descendant of it. Prime images can use their declared Rancher OSS
  revision for a `rancher/rancher` PR.

The result is provenance evidence, not a binary attestation: image labels are
producer-declared, a later commit can revert a change, and an equivalent
cherry-pick has a different SHA. Head tags are mutable, so re-check immediately
before testing. GitHub PR and ancestry lookups use the configured `gh` CLI
login; the browser never receives a GitHub token.

## Local Labs

The local lab tabs are for fast desktop-only testing. They use local Docker and
k3d, write their run records in the app workspace, and do not create AWS,
Linode, Terraform, DNS, or certificate resources.

### K3D Lab

K3D Lab is a lightweight local Kubernetes launcher. Use it when you want one or
more local k3d clusters with stable kubeconfig files and Kubernetes API
endpoints for manual testing.

- Pick a K3s image tag from the app's version list.
- Leave the API port on Auto unless you need a fixed endpoint.
- Start multiple k3d clusters side by side when you need separate local
  Kubernetes targets.
- Copy the API endpoint, copy the kubeconfig path, or save a kubeconfig file to
  Downloads from the cluster card.
- Stop, restart, or delete each cluster from the app.

K3D Lab is intentionally independent from cloud run slots. It shares the local
port reservation pool with Steve Lab so local endpoints do not collide.

### Steve Lab

Steve Lab is for quickly trying a Steve release, branch, tag, or exact commit
against a disposable local k3d cluster. It is meant for endpoint testing, not
for running Rancher tests from this app.

- Pick a Steve release tag or paste a branch, tag, or commit.
- The app inspects Steve's `go.mod` when it can and suggests a compatible K3s
  image tag.
- Steve Lab keeps one active Steve endpoint at a time. Launching again replaces
  the current Steve cluster and run files.
- The endpoint is HTTPS-only to avoid Steve's local HTTP redirect behavior.
  Tools such as Bruno, Postman, or curl may need TLS verification disabled for
  the local self-signed certificate.
- Use the copied endpoint for API paths such as `/v1/pods`.
- Opening the base endpoint may show Rancher Dashboard because standalone Steve
  includes a dashboard fallback UI. The useful API surface for testing is still
  under `/v1/...`.

Steve Lab saves the k3d kubeconfig for the run and can copy the endpoint or save
the kubeconfig to Downloads from the run card.

## Configuration Notes

Most users should edit configuration through the app. These are the local values
you are most likely to care about:

- `deployment.type` chooses `ha-rke2`, `hosted-tenant-k3s`, or
  `linode-docker-cattle`.
- `rancher.mode` is usually `auto`, where the app resolves chart, image,
  supported RKE2 version, and installer checksum details.
- `rancher.version` or `rancher.versions` selects the Rancher build or builds.
  Auto mode accepts releases, alpha/RC/RCS versions such as
  `2.15.1-rcs-c936` and `2.16.0-rcs-0844.1`, `head`, minor-line head builds
  such as `2.13-head`, community commit heads such as `2.15-<SHA>-head`,
  mutable patch-qualified Prime selectors such as `2.15.1-head`, immutable
  patch-qualified Prime heads such as `2.15.1-<SHA>-head`, and exact custom server
  images such as `bigkevmcd/rancher:v2.16-da0ab2f1dc-head`,
  `docker.io/example/rancher:my-fix`, or their matching `rancher-agent` images.
  Docker Hub namespace shorthand is accepted. Runway derives the sibling image
  with the same tag, verifies both, and uses a recognizable version in the image
  tag to select the Rancher release line for chart and Kubernetes compatibility
  lookup. Opaque tags fall back to the latest released community chart.
  Turn off agent-image derivation in the setup UI to provide both references.
  Custom images and RCS builds support this explicit override; per-HA values
  are stored in `rancher.agent_images`. Linode Docker keeps image tags in the
  version rows and selects an exact repository separately through its custom
  image source (or `linode.dockerhub`).
  For HA RKE2 and hosted-tenant auto plans, a patch selector such as
  `2.15.1-head` is not treated as a literal image tag: Runway finds the newest
  complete matching Rancher server/agent pair in `stgregistry.suse.com`, then
  pins the resulting `v2.15.1-<SHA>-head` tag and OCI digests in the resolved
  plan. Chart resolution prefers an exact eligible chart (typically Optimus)
  and retains the normal compatible-chart fallback when chart publication lags
  the images.
  Patch-qualified head selectors and their explicit SHA forms do not fall back
  to another image registry. Use `rancher.distro=auto` (which infers Prime) or
  `prime` for these builds; an explicit `community` distro is rejected.
- `rancher.webhook_image` optionally pins a complete webhook image such as
  `stgregistry.suse.com/rancher/rancher-webhook:v0.12.1-rcs-0844.1`. The app
  validates its anonymously readable manifest and passes the override only to
  Rancher's managed webhook chart. `RANCHER_WEBHOOK_IMAGE` remains an
  environment-level override for upgrade and lifecycle validation runs.
- `rancher.preferred_image_registries` is an optional strict allow-list for
  exact Rancher server and agent image tags. The setup UI exposes SUSE staging,
  Rancher Prime, SUSE registry, and Docker Hub as checkboxes. Runway tries only
  the checked registries in that fixed priority order, requires both images in
  the same registry, and fails before Terraform starts if no complete pair exists. An
  empty or omitted list preserves the current automatic behavior. The approval
  plan records the resolution-time OCI digests and shows the OCI build version
  and linked GitHub commit when the image declares canonical source labels.
  Docker Hub verification uses the credentials Runway installs on the nodes;
  the other selected registries must be anonymously pullable by those nodes.
- `user.first_name` and `user.last_name` tag cloud resources with an owner.
- `tf_vars.aws_prefix` is the base resource prefix. Run slots derive unique
  per-run prefixes from it.
- `tf_vars.aws_route53_fqdn` is the hosted zone/domain used for Rancher records.
  Linode Docker runs still use AWS credentials for Route53 DNS.
- `tf_vars.custom_hostname_prefix` optionally pins one HA RKE2 run to a custom
  DNS label.
- `rke2.server_count` chooses 1, 3, or 5 RKE2 server nodes for each AWS Rancher
  management cluster.
- `gpu_worker.enabled` can add a worker-only GPU EC2 node per Rancher cluster.
  This is off by default because GPU instances can become expensive.
- `linode.access_token` or `LINODE_TOKEN` supplies the Linode API token for
  Linode Docker runs.

Checked-in examples are available if you want to compare shapes manually:

- [tool-config.auto.example.yml](tool-config.auto.example.yml)
- [tool-config.manual.example.yml](tool-config.manual.example.yml)
- [tool-config.hosted-tenant.auto.example.yml](tool-config.hosted-tenant.auto.example.yml)

## Run Slots And Cleanup

Each setup creates a run slot with isolated Terraform state, Terraform data,
module files, deployment output, kubeconfigs, logs, AWS names, and a run record.
Homebrew installs keep those files under
`~/Library/Application Support/Rancher Runway/workspace/terratest/automation-output/`;
source builds keep them under this checkout's `terratest/automation-output/`.

Linode Docker slots use the same slot model, but they do not produce
kubeconfigs. Cluster details show the Rancher URL and Linode IP instead.

Destroy provisioned resources from the app's Destroy tab. The slot record is
removed only after Terraform destroy succeeds. After all recorded slots are
gone, the app can clean ignored local run residue. Local residue cleanup does
not destroy cloud resources.

## Build Targets

Useful source-development targets:

```bash
make help
make setup
make app
make panel-ui
make test
```

Development Wails builds store the checkout path in ignored local build hints.
Release builds instead stage signed, versioned runtime assets in Application
Support and do not depend on the checkout.

## Advanced Usage

CLI commands, guarded Go test runs, and lower-level Wails helpers are documented
in [Advanced Usage](docs/advanced-usage.md). They are useful for development and
debugging, but the desktop app is the recommended interface.

## Ignored Local State

Important ignored local and generated paths include:

- `node_modules/`
- `desktop/wails/frontend/node_modules/`
- `desktop/wails/frontend/dist/*`, except `desktop/wails/frontend/dist/placeholder.txt`
- `desktop/wails/frontend/wailsjs/`
- `desktop/wails/frontend/package.json.md5`
- `desktop/wails/build/appicon.png`
- `desktop/wails/repo_hint.txt`
- `terratest/automation-output/`
- `tool-config.yml`
- `dist/`

`package-lock.json` files are intentionally kept so installs are repeatable.

## Supply Chain Notes

RKE2 artifacts downloaded onto cluster nodes are validated before use:

- The installer script is downloaded and SHA256 checked before provisioning.
- The same installer hash is checked again on each remote node before execution.
- When `rke2.preload_images: true` is set, the image tarball is checked against
  the official release checksum file before it is moved into place.

In manual mode, you provide installer checksum pins. In auto mode, the app
resolves the matching installer checksum during plan generation.

# Test
