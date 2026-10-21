# Rancher Runway

A macOS app for creating, testing, and cleaning up disposable Rancher environments.

- Deploy Rancher on AWS RKE2, hosted/tenant K3s, or Linode Docker.
- Follow runs, inspect clusters, collect kubeconfigs, and clean up resources.
- Explore images and Helm charts, run validation tests, and save issue evidence.
- Use local k3d and Steve labs without provisioning cloud infrastructure.

## Install

Requires macOS 12+ and Homebrew. Supports Apple Silicon and Intel.

```bash
brew tap hashicorp/tap
brew trust --formula hashicorp/tap/terraform
brew install --cask brudnak/tap/rancher-runway
```

The install includes Terraform, Helm 3, kubectl, and GitHub CLI. Core setup and
cleanup do not require a source checkout, Go, or Node.js.

Open **Rancher Runway** from Applications. If macOS blocks the first launch,
follow the [first-launch instructions](docs/homebrew-release.md#first-launch).

## Get started

1. Open **Setup** and import a `tool-config.yml` or fill in the configuration.
2. Choose your environment and Rancher version, check credentials, and select
   **Resolve & review plan** before starting.
3. Follow progress in **Runs** and inspect the environment in **Clusters**.
4. Use **Destroy** when finished.

Cloud runs need credentials for the selected provider; Linode Docker also uses
AWS Route53 for DNS. See the [desktop guide](docs/desktop-guide.md) for details.

Destroy live infrastructure before deleting workspace data or using
`brew uninstall --zap rancher-runway`; that data is needed for cleanup.

## Develop

Requires Xcode Command Line Tools, Go as specified in [go.mod](go.mod), and Node.js/npm.

```bash
make setup            # Build and install the desktop app
make test-regression  # Run local checks without provisioning infrastructure
```

See the [development guide](docs/development.md) for build targets and architecture.

## Documentation

- [Desktop guide and configuration](docs/desktop-guide.md)
- [Configuration examples](tool-config.auto.example.yml): [manual](tool-config.manual.example.yml) · [hosted/tenant](tool-config.hosted-tenant.auto.example.yml)
- [Test Lab](docs/test-lab.md) · [Issue Packages](docs/issue-packages.md) · [AWS cleanup and costs](docs/aws-cost-history.md)
- [CLI and advanced usage](docs/advanced-usage.md)
- [GitHub Actions setup](docs/github-actions-setup.md)
- [Homebrew upgrades and releases](docs/homebrew-release.md)
- [All documentation](docs/README.md)
