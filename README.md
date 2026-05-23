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

From a fresh clone on Apple Silicon or Intel macOS:

```bash
make setup
```

`make setup` builds the Wails desktop app and installs `Rancher Runway.app` to
`/Applications` by default. It also installs missing local build dependencies,
regenerates embedded control-panel assets, and refuses to replace the app while
the app or an active lifecycle operation is running.

Re-run the same command whenever you want to update the installed app:

```bash
make setup
```

Install somewhere else:

```bash
make setup INSTALL_DIR="$HOME/Desktop"
```

Keep the transient Wails build-output app as well as the installed copy:

```bash
RANCHER_RUNWAY_KEEP_WAILS_BUILD_APP=1 make setup
```

## Requirements

- macOS with Xcode Command Line Tools
- Go matching the version in [go.mod](go.mod)
- Node.js with `npm`
- Terraform, Helm, and kubectl for real lifecycle runs
- AWS credentials and Route53 inputs for AWS or Linode DNS provisioning
- Linode API token for Linode Docker runs

## First Run

After `make setup` finishes, open the macOS Applications folder and look for
`Rancher Runway`. Launching the app opens the desktop control panel.

If `tool-config.yml` does not exist, the app creates an ignored starter config
for you. Fill in the blocked values from the Setup and preflight screens before
starting a run. The local `tool-config.yml` is ignored so account details,
hostnames, and local choices do not get committed.

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
- **Destroy** removes provisioned cloud resources for a selected run slot.
- **Costs** shows cleanup estimates and the local cost ledger.
- **Settings** holds local app preferences such as GPU reminders.
- **K3D Lab** starts and stops local k3d clusters without provisioning cloud
  infrastructure.
- **Steve Lab** starts a local Steve API endpoint against k3d for quick Steve
  version checks.

The app protects active work:

- Closing the app is blocked while setup, readiness, or cleanup is running.
- `make setup` refuses to replace the installed app while the app or lifecycle
  operations are active.
- Setup, readiness, and cleanup operations are serialized where shared state
  would collide.

## Local Labs

The local lab tabs are for fast desktop-only testing. They use local Docker and
k3d, write their run records under `terratest/automation-output/`, and do not
create AWS, Linode, Terraform, DNS, or certificate resources.

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
  Auto mode accepts releases, alpha/RC versions, `head`, minor-line head builds
  such as `2.13-head`, and commit-specific head image tags.
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
module files, deployment output, kubeconfigs, logs, AWS names, and a run record
under `terratest/automation-output/`.

Linode Docker slots use the same slot model, but they do not produce
kubeconfigs. Cluster details show the Rancher URL and Linode IP instead.

Destroy provisioned resources from the app's Destroy tab. The slot record is
removed only after Terraform destroy succeeds. After all recorded slots are
gone, the app can clean ignored local run residue. Local residue cleanup does
not destroy cloud resources.

## Build Targets

Useful app-oriented targets:

```bash
make help
make setup
make app
make panel-ui
make test
```

The Wails app stores the checkout path in ignored local build hints so a
double-clicked app can find this repository without committing user-specific
paths.

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
