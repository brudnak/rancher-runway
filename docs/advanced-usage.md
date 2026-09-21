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
