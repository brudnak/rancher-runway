# Advanced Usage

These commands are for debugging, automation, and development. They are not the
recommended workflow for normal use. Prefer the Rancher Runway desktop app from
macOS Applications.

## CLI

Open the same local panel without the Wails app:

```bash
go run ./cmd/rancher-runway panel
```

Inspect status without opening the browser:

```bash
go run ./cmd/rancher-runway status
go run ./cmd/rancher-runway status -json
```

## Guarded Go Runs

Live infrastructure tests are intentionally guarded. They run only when the
`-run` pattern exactly selects the intended test, which helps prevent broad test
runs from accidentally creating or destroying cloud resources.

Use anchored patterns:

```bash
go test -v -run '^TestHaSetup$' -timeout 60m ./terratest
go test -v -run '^TestHAWaitReady$' -timeout 35m ./terratest
go test -v -run '^TestLinodeDockerWaitReady$' -timeout 35m ./terratest
go test -v -run '^TestHAControlPanel$' -timeout 0 -count=1 ./terratest
go test -v -run '^TestHACleanup$' -timeout 30m ./terratest
```

For GoLand, configure the package as
`github.com/brudnak/ha-rancher-rke2/terratest` and use an exact pattern such as
`^TestHaSetup$`, `^TestHAWaitReady$`, `^TestLinodeDockerWaitReady$`,
`^TestHAControlPanel$`, or `^TestHACleanup$`.

## Rancher Server Images

For HA and hosted Kubernetes installs, Runway checks `rancher/rancher-server`
first and falls back to `rancher/rancher` when the requested production tag is
absent. This supports the November 2026 server-image split while keeping older
builds usable. Registry access errors stop resolution instead of selecting an
older image. The requested tag and matching `rancher/rancher-agent` are preserved.
Moving patch-head selectors search both repositories and choose the newest
complete, verified pair; a stale legacy alias does not take priority over a
published production-server alias.

Exact image input accepts `/rancher-server`, `/rancher`, and `/rancher-agent`.
For example, `stgregistry.suse.com/rancher/rancher-server:v2.16.0-d492998-head`
uses that server image and the matching `rancher-agent` tag. Linode Docker
installs keep their separate image selection: the production `rancher-server`
image expects Kubernetes and is not a replacement for the standalone Docker
image's bundled K3s runtime.

## Rancher system chart preflight and readiness

Use **Resolve & review plan** in Setup to check a Kubernetes install before provisioning.
Runway renders the Helm chart and checks the server and assets images. By
default Rancher updates its `rancher-charts` repository online. Runway reads
the branch configured by the rendered server environment or the server
build's `chartDefaultBranch`, resolves that branch at the configured upstream,
and checks the index at that commit. With `useBundledSystemChart=true` and
the default chart repository, Runway checks the chart revision packaged in
the rendered assets image.

A version missing from the selected catalog stops the plan. The message names
the server, catalog, revision, and missing versions, including Fleet's separate
`fleet-crd` chart. Custom external repositories, settings supplied by cluster
resources, and unavailable metadata leave the check inconclusive. In those
cases Runway proceeds to runtime verification.
Older charts without a separate assets image use runtime verification.

On HA RKE2 and hosted K3s, readiness checks the Rancher HTTP endpoints and the
rollout of each required deployment. Runway checks Rancher, the webhook, the
Fleet controller, managed Turtles (2.13 and later), and the system upgrade
controller when Rancher manages local cluster upgrades. An online catalog can
start with the packaged index while Rancher fetches the configured branch.
If Rancher reports a missing version during startup, Runway logs the chart,
catalog mode, branch, reported commit, and download status when available.
Runway waits for the required rollouts until the readiness deadline. The
timeout includes the last observed chart error and any change in catalog status.
In bundled mode, a missing version from the active packaged repository fails
early once the Rancher rollout is ready.

## Configured Linode Downstreams

For `deployment.type: ha-rke2`, enable a downstream in `tool-config.yml` and
run the usual setup command. `TestHaSetup` checks the downstream configuration
and token before provisioning AWS infrastructure, then waits for management
Rancher readiness and provisions the enabled downstreams. Each enabled row
creates one single-node cluster. The desktop panel uses the same plans and
keeps readiness and downstream provisioning as separate tracked operations.

For one Rancher (`total_has: 1`):

```yaml
downstream:
  linode:
    plans:
      - enabled: true
        distribution: k3s # Or rke2; independent of the management cluster distro.
        kubernetes_version: "" # Optional: Rancher's supported default for this distro.
        region: us-ord
        instance_type: g6-standard-2
        image: linode/ubuntu22.04
```

Supply the token through `LINODE_TOKEN` or `LINODE_ACCESS_TOKEN`. Existing
`linode.access_token` config also works. Environment variables take precedence.
No Linode root password or Docker Hub setting is needed for these downstreams.

```bash
.github/scripts/run-with-cancel-cleanup.sh \
  go test -v -run '^TestHaSetup$' -timeout 120m -count=1 ./terratest
```

The timeout covers management setup, readiness, and downstream provisioning.
In GitHub Actions, keep the token in the setup step's environment alongside
the same Terraform state settings you already use. No second command is needed.

Default runs preserve Rancher's shipped machine provisioner, including failures.
To bypass a known provisioner packaging failure while testing another feature,
explicitly set `RANCHER_RUNWAY_MACHINE_PROVISIONER_WORKAROUND=true` in Runway's
environment. This opt-in works around two machine provisioner builds whose
driver-download script cannot find `sed` and `grep`:

| Affected `rancher/machine` tag | Compatibility tag |
| --- | --- |
| `v0.15.0-rancher145.4-rc.1` | `v0.15.0-rancher145.3` |
| `v0.15.0-rancher142.8-rc.1` | `v0.15.0-rancher142.7` |

When enabled, Runway verifies the replacement in the configured system registry, updates
Rancher's `machine-provision-image` setting, and confirms it before creating
downstream resources. The change appears in the setup log or desktop operation
history and applies to future machine jobs across that Rancher instance. It
persists for subsequent provisioning and cleanup jobs. The Rancher server
version and system registry stay unchanged. Other image tags, custom repositories,
and digest-pinned images are preserved. If verification or the setting update
fails, Runway stops before creating the downstream. This also applies to
downstreams created from the deployed-cluster panel.
After upgrading to a build with a corrected provisioner image, clear this
`machine-provision-image` override to resume Rancher's default for that build.

Provide one plan per `total_has` row, in the same order as the Rancher versions
or manual Helm commands. Use `enabled: false` for rows without a downstream;
this does not disable their management Rancher deployment. With `total_has: 1`,
include only one entry, even when the management cluster has three server nodes.
Omitting the section disables downstream creation. Missing distro, region,
instance type, and image fields use the built-in defaults shown above, independently
for each entry. A pinned Kubernetes
version must match the selected distro and be offered by that Rancher instance.
See the [auto](../tool-config.auto.example.yml) and
[manual](../tool-config.manual.example.yml) examples for two-row configurations.

To provision or retry configured downstreams on an existing management run:

```bash
go test -v -run '^TestHAProvisionConfiguredLinodeDownstreams$' -timeout 35m -count=1 ./terratest
```

Reuse the run's config, Terraform state, output directory, and
`HA_RANCHER_RUN_ID` when set. Recorded active downstreams are reused. The panel's
frozen `RANCHER_RUNWAY_DOWNSTREAM_LINODE_PLANS` environment takes precedence over
the file. `TestHACleanup` attempts downstream cleanup before destroying management
infrastructure; `TestHADeleteLinodeDownstream` deletes only the downstreams.

`TestHAProvisionLinodeDownstream` remains the legacy entry point: it enables K3s
on every Rancher using `LINODE_REGION`, `LINODE_INSTANCE_TYPE`, `LINODE_IMAGE`,
and `K3S_VERSION`, rather than reading these plans.

## AWS SSM Readiness

Runway executes EC2 installation commands through AWS Systems Manager (SSM).
Before each command, it waits up to five minutes for the instance's agent to
report `Online`. This deadline includes SSM API calls and retries; it is separate
from the remote command execution timeout.

Override the readiness deadline in `tool-config.yml`:

```yaml
aws:
  ssm_ready_timeout: 5m
```

For CI, `RUNWAY_SSM_READY_TIMEOUT=10m` takes precedence over the YAML setting.
Both accept positive Go durations such as `5m` or `300s`.

Credential and permission errors stop the wait immediately and retain the AWS
error. Other API errors are retried until the deadline. A timeout reports the
last observed registration/PingStatus and any outstanding API error. Check the
agent startup logs, EC2 instance profile, and outbound HTTPS connectivity to
the regional SSM endpoints when the API succeeds but the agent stays offline.

The caller needs `ssm:DescribeInstanceInformation`, `ssm:SendCommand`, and
`ssm:GetCommandInvocation` permissions. These are separate from the EC2 agent's
instance profile permissions. The command runner reads `AWS_ACCESS_KEY_ID`,
`AWS_SECRET_ACCESS_KEY`, and `AWS_SESSION_TOKEN`; aliases such as
`AWS_ACCESS_KEY` and `AWS_SECRET_KEY` do not override an OIDC role's exported
credentials. See [AWS SSM troubleshooting](https://docs.aws.amazon.com/systems-manager/latest/userguide/troubleshooting-ssm-agent.html)
for instance-side checks.

## Lower-Level Build Helpers

The top-level app flow should be enough most of the time:

```bash
make setup
```

Lower-level Wails helpers remain available when you need to debug the installer
or app packaging directly:

```bash
scripts/build-wails-app.sh
scripts/install-wails-app.sh
scripts/install.sh
```

The desktop build prepares Runway's embedded UI assets before invoking Wails
and skips Wails' optional embed-directory scan. This avoids the Wails 2.12
`package "context" without types` error with Go 1.27; bindings, frontend, and
application compilation still run. No Go downgrade or cache deletion is needed.
