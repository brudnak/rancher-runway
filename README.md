# RKE2 Rancher HA Bootstrapper

Deploy local Rancher High Availability test environments on AWS with RKE2,
Terraform, and a local control panel. The project can be used from the command
line, from guarded Go test entrypoints, or from a double-clickable macOS app.

For repository-owned GitHub Actions automation, see [docs/README.md](docs/README.md).

## What This Builds

- One or more 3-node RKE2 HA clusters on AWS
- AWS ALB and Route53 records in front of Rancher
- TLS terminated by AWS ACM, with Rancher configured for `tls=external`
- Local kubeconfig and install artifacts for each HA cluster
- A local-only control panel for preflight checks, setup, readiness, status, logs, and cleanup

RKE2 installer scripts and optional image bundles are checksum-verified before
use. The setup path does not use `curl | bash`.

## Quick Start: macOS App

From a fresh clone on Apple Silicon or Intel macOS:

```bash
scripts/install.sh
```

The installer builds the native Wails app and installs `Rancher HA RKE2.app` to
`/Applications` by default. It installs the Wails CLI with Go if it is missing, installs
Node dependencies, regenerates the embedded control-panel CSS, builds the app,
and removes transient build output after installation. Re-running the installer
replaces the existing app bundle in place, so updating is:

```bash
git pull
scripts/install.sh
```

Requirements:

- macOS with Xcode Command Line Tools
- Go matching the version in [go.mod](go.mod)
- Node.js with `npm`
- Terraform, Helm, and kubectl for real lifecycle runs
- AWS credentials and any required Route53 inputs for provisioning

Install somewhere other than `/Applications`:

```bash
HA_RANCHER_INSTALL_DIR="$HOME/Desktop" scripts/install.sh
```

Keep the transient Wails build-output app as well as the installed copy:

```bash
HA_RANCHER_KEEP_WAILS_BUILD_APP=1 scripts/install.sh
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

Run the lifecycle:

```bash
# Create infrastructure
go test -v -run '^TestHaSetup$' -timeout 60m ./terratest

# Wait for Rancher and rancher-webhook health
go test -v -run '^TestHAWaitReady$' -timeout 35m ./terratest

# Open the local control panel
go test -v -run '^TestHAControlPanel$' -timeout 0 -count=1 ./terratest

# Destroy infrastructure
go test -v -run '^TestHACleanup$' -timeout 30m ./terratest
```

The same local panel can be started through the CLI entrypoint:

```bash
go run ./cmd/ha-rancher panel
```

Inspect local status without opening the browser:

```bash
go run ./cmd/ha-rancher status
go run ./cmd/ha-rancher status -json
```

## Configuration

Use one of the checked-in examples as your starting point:

- [tool-config.auto.example.yml](tool-config.auto.example.yml)
- [tool-config.manual.example.yml](tool-config.manual.example.yml)

Copy the example you want to `tool-config.yml` and adjust local values. The
actual `tool-config.yml` is ignored so cloud account details, hostnames, and
local choices do not get committed.

The local app can also create an ignored starter `tool-config.yml` in auto mode
with blank local values.

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
  preload_images: true

total_has: 2

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
  install_script_sha256: "bfbd978d603b7070f5748c934326db509bf1470c97d3f61a3aaa6e2eed6bd054"
  preload_images: true
```

For multiple HA clusters, provide one Rancher version or Helm command per HA.
The tool validates the shape before provisioning starts.

## Control Panel

The local control panel is bound to `127.0.0.1` only. It provides:

- Local preflight checks for tools, `tool-config.yml`, and required environment variables
- Setup, readiness, and cleanup launchers using the canonical lifecycle flows
- Searchable lifecycle logs
- Per-cluster Rancher URL, kubeconfig path, and reachability
- `cattle-system` visibility for Rancher and rancher-webhook pods
- Recent pod logs and live log streaming
- Active Rancher leader detection
- Guarded cleanup that requires typing `cleanup`

Panel state is disposable local cache under `terratest/automation-output/` and
is ignored by Git. This checkout is treated as a single-run workspace: run
cleanup before starting a new setup, or use a separate checkout with distinct
state and hostname values.

Regenerate the embedded panel CSS after changing panel templates or Tailwind
classes:

```bash
npm install
npm run build:panel-css
```

## Build Scripts

Public installer:

```bash
scripts/install.sh
```

Lower-level Wails helpers:

```bash
scripts/build-wails-app.sh
scripts/install-wails-app.sh
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

## Git Hygiene

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

## Cleanup

Destroy all provisioned AWS resources:

```bash
go test -v -run '^TestHACleanup$' -timeout 30m ./terratest
```

Cleanup also prints a best-effort AWS estimate for EC2 runtime and EBS root
volume cost. It is not a final AWS bill and does not include every charge type.

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
