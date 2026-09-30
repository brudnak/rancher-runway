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
