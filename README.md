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

Homebrew installation for Apple Silicon and Intel macOS uses the project's
own tap. Once the first release and Cask have been published:

```bash
brew tap hashicorp/tap
brew trust --formula hashicorp/tap/terraform
brew install --cask brudnak/tap/rancher-runway
```

Homebrew requires the Terraform dependency's tap to be added and its formula
trusted explicitly. These steps follow Homebrew's
[tap trust instructions](https://docs.brew.sh/Tap-Trust).

The Cask installs the universal app plus Terraform, Helm 3, `kubectl`, and
GitHub CLI (`gh`).
Default release builds use ad-hoc signing without Apple notarization, so no
paid Apple Developer membership is needed to publish them through this tap.
macOS may require **Open Anyway** on first launch; see [First Run](#first-run).
The app contains its own lifecycle worker, so ordinary setup, readiness, and
cleanup runs do not require a source checkout, Go, Node.js, or Xcode.

If `make setup` already installed `/Applications/Rancher Runway.app`, quit
that app and rename or move its bundle before the first Homebrew install.
Keep the checkout: development builds store their configuration and run state
there, while releases use `~/Library/Application Support/Rancher Runway`.
Existing development runs are not automatically imported into the release app.

Release setup and optional Developer ID signing are documented in the
[Homebrew release guide](docs/homebrew-release.md).

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

Tool readiness baselines (September 2026):

| Tool | Baseline | Requirement |
| --- | --- | --- |
| kubectl | 1.37.0 | Major 1; choose a client within one minor of the target cluster |
| Helm | 3.22.0 | Major 3 (`helm@3`); Helm 4 is not supported |
| Terraform | 1.16.3 | Major 1 from HashiCorp's tap |
| Go | 1.27.1 | Source builds / Steve Lab only; core desktop operations use the bundled worker |

Baselines are recorded in [`terratest/system_readiness.json`](terratest/system_readiness.json).
Newer versions within the required major pass the baseline check. Older versions
show an advisory; missing tools and unsupported majors block setup. A tool
version check does not establish compatibility with every target cluster; see
[Kubernetes client version skew](https://kubernetes.io/releases/version-skew-policy/#kubectl).
The package versions follow [Homebrew Helm 3](https://formulae.brew.sh/formula/helm@3),
[Homebrew kubectl](https://formulae.brew.sh/formula/kubernetes-cli), and
[HashiCorp's Terraform formula](https://github.com/hashicorp/homebrew-tap/blob/main/Formula/terraform.rb).

## First Run

The top-left version bar stays visible while scrolling. Warning dialogs,
notifications, and error messages include the running version and build number
so screenshots identify the affected release. Source builds without release
metadata show `v0.0.0-dev`; release builds use their version tag automatically.
Use **Refresh checks** to rerun status, preflight, and Setup tool readiness.
A check that does not respond within 30 seconds shows an error and can be
retried; setup remains locked until the startup safety check succeeds.

After Homebrew finishes, open the macOS Applications folder and look for
`Rancher Runway`. If macOS blocks it because the developer cannot be verified
or Apple cannot check it for malicious software, and you trust the release:

1. Try opening the app once, then dismiss the alert.
2. Open **System Settings → Privacy & Security → Open Anyway**.
3. Confirm **Open**, then authenticate if prompted.

Apple documents this [per-app approval process](https://support.apple.com/en-us/102445).
Updates may require approval again, and managed Macs may restrict this option.
Launching the app opens the desktop control panel.

If `tool-config.yml` does not exist, the app creates a private starter config in
its Application Support workspace. Open **Setup** and choose either path:

- **Import configuration**: choose your existing `tool-config.yml`, review the
  detected sections, then select **Back up & import** and **Continue with imported
  config**. The preview hides passwords. The import replaces the workspace's
  saved config, preserves a private backup beside it, and refreshes Setup.
- **Fill in the checklist**: select each missing value to jump to its field,
  including fields inside **Network & infrastructure**. The checklist follows the
  selected deployment type. Check **Tools & credentials**, then **Resolve & review plan**
  to save the entered values and review the plan before starting infrastructure.

Setup groups the form into environment, Rancher configuration, and destination.
The live run brief distinguishes Rancher instances from underlying server counts.
Registry overrides and GPU options expand when needed; validation opens and
focuses the relevant field. The progress rail follows configuration, resolution,
and final approval. Setup and the labs share the same light/dark design tokens.

Import accepts a single `.yml` or `.yaml` file up to 1 MB, including partial
Runway configs. Invalid YAML and unrelated files are rejected without changing
settings. It is unavailable during plan resolution or a running operation.
Backups are named `.tool-config-before-import-*.yml`, are readable only by your
user, and survive app upgrades. You can restore one through the same import button.

The import copies configuration only. It does not migrate environment variables,
kubeconfigs, Terraform state, or run history from a development checkout. Keep
using the development app to manage runs created there.

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

Runway opens on **Home**, an overview of the workspace with live activity,
guided workflows, and a searchable guide to every tool. Use the Home icon in
the top bar to return at any time, or resume your last workspace from Home.
Press `/` on Home to find a tool by name or task.

The navigation groups workspaces into **Deploy & operate**, **Investigate**, and
**Local tools**, using the same catalog as Home. Open a group to browse its tools,
or use **Jump to…** (`⌘J` on macOS, `Ctrl+J` elsewhere) to search every workspace
by name or task. Arrow keys move through results; Enter opens a workspace and
Escape returns to where you were. Running-operation markers remain visible on
their group while you work elsewhere. Narrow windows use a compact workspace
picker instead of a horizontally scrolling tab bar.

Steve Lab and K3D Lab use their own local Docker environments and can run
while cloud provisioning continues. Each lab checks its own prerequisites and
ports; available machine resources still matter. Switching tabs does not stop
provisioning. Keep Runway open and return to **Runs** to follow its progress.

Use the app tabs as the main lifecycle:

- **Setup** resolves a plan, checks local prerequisites, lets you choose AWS
  RKE2, hosted/tenant K3s, or Linode Docker mode, and starts provisioning after
  review.
- **Runs** shows recorded run slots, active operations, per-run folders, logs,
  Terraform paths, hostnames, and destroy shortcuts.
- **Clusters** shows Rancher URLs, kubeconfig paths or Linode IPs, reachability,
  pod visibility, recent logs, and active leader details.
- **AWS Inventory** shows resources associated with recorded slots and owner
  tags. Search by name, ID, run, or tag; filter by resource type and cleanup
  eligibility. Review individual leftovers, a selection, or the visible
  candidates, then confirm the exact deletion list. Each protected resource
  explains why it cannot be deleted through inventory.
- **Image Lookup** searches Rancher server, agent, and webhook tags across
  Docker Hub and the Rancher/SUSE registries, or inspects a custom image
  repository.
- **Helm Lab** builds Rancher Helm commands from the live Community/Prime
  chart catalog. Searchable versions, grouped settings, a before/after changes
  review, inline validation, YAML import/export, presets, and undo help you
  prepare a release. The light and dark themes include syntax-highlighted output.
  The command preview supports explicit kube contexts, wait/timeout, dry runs,
  and a matching companion values file. A self-contained setup script adds the
  repository and handles temporary values files for you. Exports save directly
  into Downloads. Chart metadata requires internet access;
  editing and command generation happen locally and do not execute Helm.
- **PR Image Check** resolves a GitHub pull request commit and checks whether
  Rancher head images across all known registries declare that commit in their
  source ancestry.
- **Issue Radar** shows owner lanes, unassigned issues, milestone gaps, QA-size
  and issue-kind summaries, and downloadable assignment reports.
- **Destroy** retires recorded runs individually or in a sequential batch.
  Its **Costs & local data** view shows locally recorded AWS cleanup estimates
  with service and cumulative charts, date/region filters, CSV export, and
  JSON backup/import. Local file cleanup and cost-history reset are separate
  actions. Home links directly to both leftover cleanup and the cost journal.
  See [AWS cleanup and cost history](docs/aws-cost-history.md) for coverage
  and limitations.
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

### Helm Lab

Helm Lab is Runway’s native three-step release workbench: choose a chart and
destination, configure values, then review and export a pinned plan. It reads the
index, defaults, and schema directly from official Rancher chart repositories.
Each archive’s SHA-256 is verified against its index entry. It does not require a
local Helm installation or contact a cluster. Unavailable versions and failed
lookups block export instead of substituting another chart’s defaults.

Settings are grouped by purpose, with essentials shown first and all chart values
available through search or “more settings.” Certificate choices reveal related
inputs. Editable starting points, memory-only undo/redo, YAML import, inline type
checks, and actionable release checks help prepare a plan. Use **⌘/Ctrl K** to
find a setting and **⌘/Ctrl Enter** to review. Explicit values remain in exports
even when they match chart defaults; an imported `extraEnv: []` clears installed
environment variables. Revert an override to inherit instead.

Search understands both readable setting names and exact Helm keys. The overrides
filter spans all categories. Review lists blocking details separately from cluster
prerequisites, and each issue takes you to its setting. **⌘/Ctrl Z** and
**⌘/Ctrl Shift Z** undo and redo plan edits outside text fields; text fields retain
their native undo behavior. Both themes support keyboard focus, reduced motion,
and compact window layouts.

Copy the runbook, Helm command, or complete values YAML, or export **setup.sh** or
**values.yaml** into Downloads. Existing files are preserved with numbered names.
Exports have private file permissions, and the runbook uses a private temporary
values file that is removed on exit. Secret-bearing previews are hidden until
revealed; copied and exported content includes the actual values. Edits and undo
history stay in memory for the current session and are not stored in a URL or
browser storage.

For a command using `values.yaml`, place its companion file in your terminal’s
working directory with that exact name. Review exported scripts before running
`sh setup.sh`. The workbench does not execute them. Merge upgrades use Helm 3.14+
`--reset-then-reuse-values`; dry runs also require Helm 3.14+ for `--hide-secret`.
Checks guide preparation but do not inspect cluster readiness or installed values.

### Issue Radar

Issue Radar is a read-only GitHub workspace. Run `gh auth login` once in your
terminal using an account that can read the target repository. The Homebrew Cask
installs `gh`; source installations can use `brew install gh`. Credentials stay
with the CLI and are never sent to the panel or stored in its preferences.

Enter a repository, one or more team labels such as `team/frameworks` or
`area/frameworks`, and up to eight GitHub usernames. Multiple labels are combined
with AND. Choose a specific milestone by exact title (or load its title from
GitHub), all milestones, or issues without a milestone. The board fetches all
matching **open issues**, excludes pull requests, and keeps unassigned issues
in the result regardless of the selected usernames.

- **Board** groups issues into single selected-owner lanes, multiple selected
  owners, missing selected owners, and QA/None. It distinguishes truly unassigned
  issues from ones assigned only to people outside your chosen team. Search by
  title, number, label, milestone, or owner; filter assignment, milestone, and
  QA-size gaps. Click an owner card to see that person's issues. Compare relative
  QA effort (XS/S/M/L/XL = 1/2/3/5/8 points), with shared effort split between
  selected owners and missing or conflicting sizes marked provisional. Use `/`
  to search, switch to compact cards, or expand an issue's context in place.
- **Summary** shows assignment totals, QA sizes, and bugs/enhancements/other
  kinds. Shared issues count once. QA/None is excluded from assignment checks.
  These tables always represent the full fetched snapshot, independent of board
  search and filters.
- **Create prompt** prepares an AI assignment-planning prompt from the full
  snapshot and automatically gathers up to **50 recently updated closed issues
  per selected owner**, using the same labels across all milestones. Choose to
  preserve ownership or rebalance the team, adjust relative capacity, and add
  planning context. The prompt asks the AI to balance estimated effort, explain
  changes, produce work packages per owner, and reconcile every issue. History
  is tentative topic evidence, not a measure of performance. Shared historical
  issues are deduplicated; missing history stays distinct from zero matches.
  Failed requests can be retried, cancelled, or explicitly omitted. Preview and
  copy the prompt, or save `issue-radar-assignment-prompt.md` to Downloads.

Repository, labels, usernames, and milestone preferences stay on this device;
issue snapshots stay in memory until the app closes. Editing the scope does not
relabel an existing report: refresh to apply it. Failed refreshes retain the last
snapshot with its timestamp. Incomplete fetches never become successful reports;
scopes reaching the 100-page fetch limit must be narrowed. Saved reports use owner-only file
permissions and numbered copies to preserve existing files. No assignment,
milestone, label, or issue is changed on GitHub.

### Image Lookup

Image Lookup is a read-only registry browser for finding and comparing Rancher
builds without a Docker daemon or `skopeo` command line workflow.

The search workspace keeps advanced filters in **Refine the search**, presents
build identity in expandable rows, and opens metadata in a split or expanded
inspector. Copy a digest-pinned reference to preserve the inspected image. The
last five successful inspections remain available for the current session;
press `/` while this tab is active to focus search. Searches and inspections
can be cancelled and have bounded request times.

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

A registry comparison matrix separates server ancestry, agent ancestry, and
pair availability. Filter confirmed results or those needing review, expand
the underlying evidence, or **Copy evidence brief** for a complete Markdown
snapshot (including registries hidden by the filter). **Inspect server** opens
Image Lookup with the observed digest and platform, preserving the image
identity even if its mutable tag moves. Changed inputs are clearly marked
until a fresh check completes.

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

### Cleaning up leftover AWS resources

In **AWS Inventory**, use **Delete…** on a row, select multiple rows and choose
**Review selected**, or choose **Review all candidates**. The review fetches
current AWS metadata, lists the exact IDs and deletion effects, and requires a
typed confirmation. Reviews expire after five minutes and apply only once;
new resources discovered afterward are never added to an approved cleanup.
A review accepts up to 200 resources; use smaller selections for larger inventories.

A cleanup candidate must be in the configured region, have your matching
`Owner` tag, a recognized Runway `ManagedBy` tag and `HA_Rancher_RKE2_Run_ID`,
and have no matching recorded run in this workspace. **A candidate is not proof
that a resource is unused**: another installation or CI run may still depend on it.
Recorded runs stay protected; destroy them from the **Destroy** tab so Terraform
can clean up their complete infrastructure.

Inventory cleanup supports EC2 instances, EBS volumes, load balancers and their
listeners, target groups, and ACM certificates. Review includes listeners and
rules removed with their load balancer, and volumes configured to be deleted
when an instance terminates. No snapshots or backups are created. An attached
EBS volume requires its instance in the same selection; Runway never force-detaches
volumes or disables AWS deletion/termination protection. Associated target groups
and certificates require the load balancer in the selection or its prior removal.
IAM roles, profiles, policy attachments, DNS records, and resources with missing
ownership tags remain protected because this regional inventory cannot establish
all their dependencies.

If a scan is incomplete, **Review all candidates** covers only the currently
listed candidates. The review highlights unavailable or blocked selections.
Cleanup checks the whole reviewed plan again before deleting anything, checks
each resource before its deletion, and processes dependencies in order. Keep
Runway open until it finishes. Per-resource results report failures and skipped
dependents; refresh inventory before retrying. Other lifecycle operations and
configuration imports are locked while cleanup runs. A restart never resumes
unfinished deletion automatically; operation logs remain in the workspace.

The existing AWS credentials need read access for resource tags and dependencies
(including ELB `DescribeRules` and ACM `DescribeCertificate`), plus the relevant
`ec2:TerminateInstances`, `ec2:DeleteVolume`,
`elasticloadbalancing:DeleteLoadBalancer`, `elasticloadbalancing:DeleteListener`,
`elasticloadbalancing:DeleteTargetGroup`, or `acm:DeleteCertificate` permissions.
Runway reports AWS permission errors without changing account permissions.

## Local Labs

The local lab tabs are for fast desktop-only testing. They use local Docker and
k3d, write their run records in the app workspace, and do not create AWS,
Linode, Terraform, DNS, or certificate resources.

### K3D Lab

K3D Lab is a lightweight local Kubernetes launcher. Use it when you want one or
more local k3d clusters with stable kubeconfig files and Kubernetes API
endpoints for manual testing.

- Pick a suggested K3s image tag or enter an exact tag.
- Leave the API port on Automatic unless you need a fixed endpoint. The form
  flags ports reserved by your existing K3D sessions before launch.
- Start multiple k3d clusters side by side when you need separate local
  Kubernetes targets.
- Search and filter sessions by status, version, ID, or endpoint.
- Copy a ready-to-use `kubectl --kubeconfig ... get nodes` command, copy the API
  endpoint or file paths, or save the kubeconfig to Downloads.
- Use a session's settings as the next launch draft, with an automatic port
  and an Undo option.
- Stop, restart, or remove each cluster from its card. Removal requires the
  session ID and lets you preserve local files for inspection.

K3D Lab is intentionally independent from cloud run slots. It shares the local
port reservation pool with Steve Lab so local endpoints do not collide.

### Steve Lab

Steve Lab is for quickly trying a Steve release, branch, tag, or exact commit
against a disposable local k3d cluster. It is meant for endpoint testing, not
for running Rancher tests from this app.

- Pick a Steve release tag or paste a branch, tag, or commit.
- The app inspects Steve's `go.mod` when it can and suggests a compatible K3s
  image tag. Manual selections stay pinned until you choose **Use suggested**.
- Choose the Standard profile for API exploration or Observe to enable
  metrics. **Runtime & networking** contains the port, metrics interval,
  environment variables, and extra arguments. Enter one `NAME=value` or one
  argument per line; spaces inside an argument are preserved without shell
  expansion.
- Steve Lab keeps one active Steve endpoint at a time. Launching again opens
  a replacement review identifying the active sessions. Confirming removes
  those clusters and run folders before starting the new build.
- The endpoint is HTTPS-only to avoid Steve's local HTTP redirect behavior.
  Tools such as Bruno, Postman, or curl may need TLS verification disabled for
  the local self-signed certificate.
- Use the copied endpoint for API paths such as `/v1/pods`.
- Opening the base endpoint may show Rancher Dashboard because standalone Steve
  includes a dashboard fallback UI. The useful API surface for testing is still
  under `/v1/...`.

Steve Lab saves the k3d kubeconfig for the run. Session cards collect the
endpoint, kubeconfig, runtime logs, local file locations, and SQLite snapshot
export. **Use these settings** restores the ref, version, metrics, and runtime
overrides into an editable draft while assigning a new port automatically.

Both labs share a searchable Activity viewer with issue filtering, line
wrapping, pause/resume, and copying of matching output. Status checks pause
when a tab is inactive. If a refresh fails, the last successful workspace stays
visible and lifecycle actions wait for a fresh check.

## Test Packages

**My Work** pulls a GitHub milestone into a personal issue queue before or after assignment, prepares a local milestone bucket, and creates starter packages where an open issue has no plan yet. Its coverage and progress views distinguish saved plans, reproduction, validation, and GitHub closure. See [My Work](docs/my-work.md). Create an individual issue package from **Issue Radar**, **Clusters**, or **Test Packages** as well. Write manual cases, preserve reproduction and fix-validation sessions with recorded environment details, and attach existing Test Lab results or Cache Lab evidence. Link the fix pull request, look it up on GitHub for its title and linked issue, and let validation sessions record the fix and head commit they tested. Each session keeps the plan it started with; later edits do not rewrite that history. The Overview derives plan → reproduction → fix → validation progress with a suggested next step; it never sets the package status or a case outcome.

**Is this issue ready to test?** checks linked PRs against observed head-build pairs and detects **QA template found on GitHub** in issue descriptions and comments. Enable **Settings → Daily issue readiness** for a saved My Work briefing on the first app open or return each day, or use **Scan now**. Build evidence, GitHub QA guidance, and locally saved test cases are tracked separately. See [Issue readiness](docs/issue-readiness.md) for limits and evidence rules.

Preview a Markdown report for GitHub, export a portable package, or explicitly back up that same format to a private GitHub repository. Imports create independent local copies and never execute tests. Attached evidence survives cleanup of the original lab record. Organize packages into movable, ordered milestone buckets and export a bucket or the whole library. Link saved Test Lab plans to cases; reviewed local runs preserve their results and logs in the originating session. See [Test Packages](docs/test-packages.md) for the workflow, archive format, storage, and limits.

## Cache Lab

Cache Lab snapshots and Test Lab results share a cluster home in **Clusters**. Stable cluster IDs keep links intact, while a shared nickname makes the same environment easy to recognize everywhere. Cluster history remains available after infrastructure cleanup unless you select its optional Test Lab or Cache Lab cleanup in **Destroy**. Saved plans and config templates are retained.

Cache Lab keeps a persistent local workspace for each Rancher source. Connect
with a Rancher URL and password by default, choose the **URL + API token**
tab for an existing token, point to a trusted kubeconfig and context, or
start a collection of imported SQLite files. Names default to the source host;
use a nickname to distinguish environments. URL/token access uses Rancher's
Kubernetes proxy (management cluster `local` by default), so the account needs
permission to discover and exec into the Rancher pods. Kubeconfig connections
must target the cluster where Rancher runs. Runway pins the context and rejects
capture if that context's server URL later changes.

**Use this Rancher** lists management environments already detected by Runway’s
Runs & Clusters view. It fills the URL; **Use kubeconfig** selects the recorded
management kubeconfig without needing a Rancher token. Downstream kubeconfigs
are excluded, and Docker-only Ranchers are labeled as unsupported for pod
capture. You can still enter any other Rancher manually.

**URL + password → Sign in & generate token** creates a named, expiring API
token for a local Rancher account (default username `admin`) and fills the token
field. The password is used once and cleared, never saved. TLS verification is
on by default; Connection settings offers a custom CA or an explicit opt-out for
self-signed servers. Runway logs out its temporary sign-in session afterward.
The generated token stays in the draft unless you explicitly remember it in
Keychain. Choose its expiry; the server’s policy still applies. For SSO, paste
an API token from Rancher. Revoke generated tokens in Rancher’s **API & Keys**;
creating a replacement or deleting a local workspace does not revoke old keys.
The [same connection flow is available in Test Lab](docs/test-lab.md#rancher-token-caveat).

**Capture always uses `VACUUM INTO`.** Live capture downloads the pinned
[`vai-vacuum` v1.0.0-beta](https://github.com/brudnak/vai-vacuum) helper on your
computer, verifies its SHA-256 checksum, and streams it into the chosen pod.
The pod needs a shell, `cat`, `chmod`, `uname`, and standard file/process tools;
it needs neither SQLite installation nor outbound GitHub access. The published
helper is Linux amd64. For ARM pods or offline operation, supply a matching
Linux `vai-vacuum` binary under Connection → Advanced. The helper expects
`/var/lib/rancher/informer_object_cache.db`; SQL caching must already be active.
Capture reads the source and writes a temporary snapshot, so it still uses pod
CPU and disk space. Competing Runway captures use a pod lock and a remote
five-minute watchdog. An existing `/tmp/vai-snapshot.db` is never removed to
make room for a new capture; inspect a leftover file before removing it.

- **Library:** source tabs, nicknames, folders, favorites, notes, and saved SQL
  queries survive app restarts. Each snapshot records its source, pod UID,
  restart count, image, capture time, checksum, and size. Tokens are session-only
  unless you opt into macOS Keychain. Kubeconfig files stay at their original
  paths; Runway does not change your current Kubernetes context.
- **Explore:** searchable tables, schema and index definitions, paged/filterable
  rows, sorting, and expandable JSON records. The inspector opens snapshots
  read-only. SQL accepts a single SELECT/CTE, supports JSON functions, and exports
  the displayed result as CSV or JSON. Cmd/Ctrl+Enter runs a query.
- **Compare:** choose a baseline and comparison, then a table. The disk-backed
  engine compares schema and records, matching explicit primary keys or
  verified unique identity columns. Without a reliable key, it compares row
  multisets, preserving duplicate counts. JSON object key order is normalized;
  optional filtering ignores only `metadata.resourceVersion` and
  `metadata.managedFields`. Source/replica differences are flagged. Counts cover
  every scanned row; exported table reports include any preview-limit warning.
- **Capture again & compare:** captures the selected pod and opens its result
  against the currently selected baseline. Steve Lab's **Capture in Cache Lab**
  sends a fresh local VACUUM snapshot directly into the same library.
- **Cleanup:** delete a snapshot, clear a workspace's snapshots, or delete the
  entire workspace after typed confirmation. Removing a folder moves its files
  to Unfiled. These controls never delete live Rancher resources or the original
  file used for an import. Completed copies can be exported to Downloads.

The library lives in `automation-output/control-panel/cache-lab` under Runway's
workspace. Snapshot files and the manifest have private permissions. Keep the
whole directory together when backing it up; tokens stored in Keychain are
separate. Imports must be standalone SQLite files, not a bare copy of a live
WAL database. Interrupted or invalid captures are not published in the library.

Current bounds are 2 GiB per snapshot; SQL results show up to 500 rows / 16 MiB
and run for at most 20 seconds. A table comparison scans up to one million rows
per side / 512 MiB of decoded data with a 60-second deadline. It returns up to
300 record details / 8 MiB, with full scan counts. Exceeding the scan budget
fails explicitly rather than reporting an incomplete comparison as complete.

Lab and investigation tabs share the [workbench theme](docs/workbench-theme.md),
including Helm Lab's dark blue surfaces and accessible mint focus/accent colors.

## Configuration Notes

Most users should edit configuration through the app. These are the local values
you are most likely to care about:

- `deployment.type` chooses `ha-rke2`, `hosted-tenant-k3s`, or
  `linode-docker-cattle`.
- `downstream.linode.plans` enables a single-node K3s or RKE2 downstream per
  HA RKE2 Rancher. Both the app and direct `TestHaSetup` provision enabled plans
  after management readiness. See [downstream config and CI examples](docs/advanced-usage.md#configured-linode-downstreams).
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
  The plain `head` selector also accepts `Head` or `HEAD`; exact custom image
  tags keep their original capitalization.
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
- `rke2.ingress_controller` defaults to `traefik` and is written explicitly to
  every RKE2 server. Traefik requires RKE2 `v1.30.3+rke2r1` or newer.
  `ingress-nginx` remains available only as a legacy override through RKE2 1.36;
  Runway rejects it on community RKE2 1.37 and newer.
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
make release-plan
```

Maintainers can run `make release` to publish the first stable release as
`v1.0.0`, then increment the patch version on subsequent runs. Use
`RELEASE_BUMP=minor` or `RELEASE_BUMP=major` for larger version changes.
The command builds published GitHub source, publishes the release, and updates
the Homebrew Cask. See the [release guide](docs/homebrew-release.md#publish-a-release)
for prerequisites and retry commands.

Development Wails builds store the checkout path in ignored local build hints.
Release builds instead stage checksum-verified, versioned runtime assets in Application
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

## Test Lab

Open **Local tools → Test Lab** to search `rancher/tests/validation`, select suites or individual tests, configure a Rancher target, and review exact pinned Go commands before running locally. Saved plans, Keychain configuration, activity logs, and test results make repeat runs easier. GitHub onboarding supports device sign-in, private repository connection/creation, and manual-only repository deletion guidance. Managed workflow installation and cloud execution are the next milestone and are not enabled in this initial version. See [Test Lab setup and usage](docs/test-lab.md).
