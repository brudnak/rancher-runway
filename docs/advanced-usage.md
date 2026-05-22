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
