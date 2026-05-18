# RKE2 Rancher HA Bootstrapper

Deploy disposable Rancher High Availability test environments on AWS with RKE2,
Terraform, and a local control panel. The project can be driven from the
double-clickable macOS app, the `ha-rancher` CLI, or guarded Go test entrypoints.

For repository-owned GitHub Actions automation, see [docs/README.md](docs/README.md).

## What This Builds

- One or more RKE2 management clusters on AWS: single-server, 3-server HA, or 5-server HA
- AWS ALB, ACM, and Route53 records in front of Rancher
- Rancher installed with external TLS termination at the ALB
- Local kubeconfigs, install artifacts, run records, lifecycle logs, and cost history
- A local-only control panel for setup, readiness, status, logs, AWS inventory, and cleanup

RKE2 installer scripts and optional image bundles are checksum-verified before
use. The setup path does not use `curl | bash`.

## Quick Start: macOS App

From a fresh clone on Apple Silicon or Intel macOS:

```bash
make setup
```

`make setup` is the friendly installer. It checks that `Rancher HA RKE2.app` is
closed and that no setup, readiness, or cleanup operation is running, then
rebuilds the Wails app and installs it to `/Applications` by default. It also
installs missing build dependencies used by this repo, regenerates the embedded
control-panel CSS, and removes transient build output after installation.

Re-run the same command to update the installed app:

```bash
make setup
```

Requirements:

- macOS with Xcode Command Line Tools
- Go matching the version in [go.mod](go.mod)
- Node.js with `npm`
- Terraform, Helm, and kubectl for real lifecycle runs
- AWS credentials and Route53 inputs for provisioning

Install somewhere other than `/Applications`:

```bash
make setup INSTALL_DIR="$HOME/Desktop"
```

Keep the transient Wails build-output app as well as the installed copy:

```bash
HA_RANCHER_KEEP_WAILS_BUILD_APP=1 make setup
```

## Quick Start: CLI

Create your local config:

```bash
cp tool-config.auto.example.yml tool-config.yml
```

Set the required secrets in your shell:

```bash
export AWS_ACCESS_KEY_ID="your-aws-access-key"
export AWS_SECRET_ACCESS_KEY="your-aws-secret-key"
export DOCKERHUB_USERNAME="optional-dockerhub-username"
export DOCKERHUB_PASSWORD="optional-dockerhub-password"
```

Open the local panel:

```bash
go run ./cmd/ha-rancher panel
```

Inspect status without opening the browser:

```bash
go run ./cmd/ha-rancher status
go run ./cmd/ha-rancher status -json
```

The canonical lifecycle is also available through guarded test entrypoints:

```bash
go test -v -run '^TestHaSetup$' -timeout 60m ./terratest
go test -v -run '^TestHAWaitReady$' -timeout 35m ./terratest
go test -v -run '^TestHAControlPanel$' -timeout 0 -count=1 ./terratest
go test -v -run '^TestHACleanup$' -timeout 30m ./terratest
```

## Configuration

Use one of the checked-in examples as your starting point:

- [tool-config.auto.example.yml](tool-config.auto.example.yml)
- [tool-config.manual.example.yml](tool-config.manual.example.yml)

Copy the example you want to `tool-config.yml` and adjust local values. The
actual `tool-config.yml` is ignored so account details, hostnames, and local
choices do not get committed. The app can also create an ignored starter config
in auto mode.

Common local values:

- `user.first_name` and `user.last_name` tag AWS resources with an owner.
- `tf_vars.aws_prefix` is the base AWS resource prefix; run slots derive unique per-run prefixes from it.
- `tf_vars.aws_pem_key_name` is the EC2 key pair name attached to instances for manual break-glass SSH. The tool itself configures nodes through AWS Systems Manager Run Command, not SSH.
- `tf_vars.aws_route53_fqdn` is the hosted zone/domain used for Rancher records.
- `tf_vars.custom_hostname_prefix` optionally pins Rancher to a custom DNS label such as `brudnak.example.com`.

### Auto Mode

Auto mode lets you provide Rancher versions and lets the tool resolve chart,
image, supported RKE2 version, and installer checksum details:

```yaml
rancher:
  mode: auto
  versions:
    - "2.13-head"
    - "2.13.4"
  distro: auto
  bootstrap_password: "your-password"
  auto_approve: false

rke2:
  server_count: 3
  preload_images: true

total_has: 2

user:
  first_name: "Ada"
  last_name: "Lovelace"

tf_vars:
  aws_region: "us-east-2"
  aws_prefix: "xyz"
  aws_vpc: ""
  aws_subnet_a: ""
  aws_subnet_b: ""
  aws_subnet_c: ""
  aws_ami: ""
  aws_subnet_id: ""
  aws_security_group_id: ""
  aws_pem_key_name: ""
  aws_route53_fqdn: ""
  custom_hostname_prefix: ""
```

For one HA cluster, use `rancher.version` instead of `rancher.versions`.
Auto mode accepts release versions, alpha/RC versions, `head`, minor-line head
builds such as `2.13-head`, and commit-specific head image tags such as
`2.13-a2770149753c8e4a48aec2c1e2598bb30cbb2652-head`. Commit-specific head
inputs resolve a compatible chart from the same minor line and use the full
`v...-head` value as the Rancher image tag.

### RKE2 Server Layout

Set `rke2.server_count` to choose how many RKE2 server nodes each Rancher
cluster gets:

- `1`: single-server Rancher install. Valid for lightweight testing, but not HA.
- `3`: default HA layout and the recommended choice for normal testing.
- `5`: expanded HA layout for larger cluster testing, with higher cost and longer setup.

RKE2 server nodes are schedulable by default, so a single-server install can run
Rancher without separate worker nodes.

### Manual Mode

Manual mode lets you provide full Helm commands and RKE2 pinning yourself:

```yaml
rancher:
  mode: manual
  helm_commands:
    - |
      helm install rancher rancher-prime/rancher \
        --namespace cattle-system \
        --version 2.13.4 \
        --set hostname=placeholder \
        --set bootstrapPassword=your-password \
        --set tls=external \
        --set global.cattle.psp.enabled=false \
        --set rancherImage=registry.rancher.com/rancher/rancher \
        --set rancherImageTag=v2.13.4 \
        --set agentTLSMode=system-store

total_has: 1

k8s:
  version: "v1.33.7+rke2r1"

rke2:
  server_count: 3
  install_script_sha256: "bfbd978d603b7070f5748c934326db509bf1470c97d3f61a3aaa6e2eed6bd054"
  preload_images: true
```

For multiple HA clusters, provide one Rancher version or Helm command per HA.
The tool validates the shape before provisioning starts.

## Run Slots

The control panel treats each setup as a run slot. A slot has isolated Terraform
state, Terraform data, module files, HA output, kubeconfigs, logs, AWS names,
and a run record under `terratest/automation-output/`.

This means one checkout can keep recorded slots visible while starting another
slot. Setup, readiness, and cleanup are still serialized so Terraform state and
AWS actions stay unambiguous.

Custom Rancher hostnames are supported for one HA per slot. A custom hostname
does not block new slots as long as the full hostname is unique. Starting a new
slot with a duplicate custom hostname is blocked until the existing slot is
destroyed or the config is changed.

## Control Panel

The local control panel is bound to `127.0.0.1` only. It provides:

- Local preflight checks for tools, `tool-config.yml`, owner fields, and required environment variables
- Interactive setup for auto/manual Rancher plans and custom DNS
- Guarded lifecycle launchers for setup, readiness, and cleanup
- Run-slot overview with per-slot logs, Terraform paths, hostnames, and destroy actions
- Per-cluster Rancher URL, kubeconfig path, reachability, and `cattle-system` visibility
- Recent pod logs, live log streaming, and active Rancher leader detection
- AWS inventory for resources associated with recorded slots and owner tags
- Cleanup cost estimates and a local cost ledger

The macOS app also protects active work:

- Closing the app is blocked while setup, readiness, or cleanup is running.
- `make setup` refuses to replace the app while the app or lifecycle operations are active.

## Cleanup

Destroy provisioned AWS resources from the panel's Destroy tab, or with the
guarded test entrypoint:

```bash
go test -v -run '^TestHACleanup$' -timeout 30m ./terratest
```

Cleanup is per run slot. The slot record is removed only after Terraform destroy
succeeds. Cleanup also prints a best-effort AWS estimate for EC2 runtime and EBS
root volume cost. It is not a final AWS bill and does not include every charge
type.

After all recorded slots are gone, the panel can clean ignored local run
residue. This local cleanup does not destroy AWS resources.

## Build And Test

Useful top-level targets:

```bash
make help
make setup
make app
make panel-css
make test
```

Lower-level Wails helpers remain available:

```bash
scripts/build-wails-app.sh
scripts/install-wails-app.sh
scripts/install.sh
```

The Wails app stores the checkout path in ignored local build hints so a
double-clicked app can find this repository without committing user-specific
paths.

## Guarded Test Runs

Live infrastructure tests are intentionally guarded. They run only when the
`-run` pattern exactly selects the intended test, which helps prevent broad test
runs from accidentally creating or destroying cloud resources.

Use anchored patterns:

```bash
go test -v -run '^TestHaSetup$' -timeout 60m ./terratest
go test -v -run '^TestHAWaitReady$' -timeout 35m ./terratest
go test -v -run '^TestHAControlPanel$' -timeout 0 -count=1 ./terratest
go test -v -run '^TestHACleanup$' -timeout 30m ./terratest
```

For GoLand, configure the package as
`github.com/brudnak/ha-rancher-rke2/terratest` and use an exact pattern such as
`^TestHaSetup$`, `^TestHAControlPanel$`, or `^TestHACleanup$`.

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

In manual mode, you provide installer checksum pins. In auto mode, the tool
resolves the matching installer checksum during plan generation.

To update a manual checksum:

```bash
export RKE2_VERSION="v1.33.7+rke2r1"
curl -fsSL "https://raw.githubusercontent.com/rancher/rke2/${RKE2_VERSION}/install.sh" -o /tmp/rke2-install.sh
shasum -a 256 /tmp/rke2-install.sh
```

Copy only the hash into `tool-config.yml`.
