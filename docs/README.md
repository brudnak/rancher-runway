# Documentation

This directory is for the automation and operational docs that would make the
root README too noisy. The root README stays focused on local Rancher HA usage:
copy a `tool-config.yml`, run setup, open the local control panel, and clean up.

## Start Here

- [GitHub Actions setup](github-actions-setup.md)
- Sign-off planner CLI: [automation/signoff-plan](../automation/signoff-plan)
- Terraform state bootstrap: [bootstrap/terraform-state](../bootstrap/terraform-state)

## Intended Split

### Local Rancher Environments

Local users and forks should continue to use the root README flow:

1. Create a local `tool-config.yml`.
2. Run `go test -v -run '^TestHaSetup$' -timeout 60m ./terratest`.
3. Use `go test -v -run '^TestHAControlPanel$' -timeout 0 -count=1 ./terratest` when a browser-based local view is useful.
4. Run `go test -v -run '^TestHACleanup$' -timeout 30m ./terratest`.

This path should not require GitHub Actions, S3 state, Linode automation, or
automation-only secrets.

### Repository-Owned Automation

The original repository can layer scheduled GitHub Actions on top:

1. Watch for new Rancher alpha releases.
2. Resolve the webhook candidate from `build.yaml`.
3. Plan the sign-off bundle based on whether the alpha changed webhook versions.
4. Use a persistent S3/DynamoDB Terraform backend for isolated per-lane state.
5. Render report artifacts.
6. Clean up all AWS and Linode resources.

That automation should live behind Actions templates and environment secrets, so
forks can ignore it unless they intentionally configure their own cloud accounts.

Current workflow layers:

- `signoff-plan.yml`: manual planner from `signoff-targets.json` or one input
  Rancher version. Dispatch skips lanes that are already active or already
  successful on the current branch unless `rerun_successful_lanes=true`.
- `bootstrap-terraform-state.yml`: manual S3/DynamoDB backend bootstrap, plan-only unless `apply=true`.
- `run-rancher-signoff-lane.yml`: manual sign-off lane runner for
  `framework-regression`, `webhook-fresh-install`, `webhook-upgrade`, or
  `webhook-candidate-on-previous`, with automatic Helm repo setup, Rancher
  readiness gates, optional Linode downstream provisioning, webhook overrides,
  optional direct `rancher/tests` suites, compact JSON receipts, and automatic
  cleanup.
## Actions Visibility And State Bootstrap

Run `bootstrap-terraform-state.yml` from GitHub Actions when you want the repo-owned automation to create the S3 state bucket and DynamoDB lock table. Keep it behind the protected `automation-bootstrap` environment with an OIDC role in `AWS_BOOTSTRAP_ROLE_ARN`.

The bootstrap output contains bucket/table names and region only. Those values are not credentials, but Actions logs, summaries, and artifacts are visible to people who can read workflow runs for the repository. Put the resulting `TF_STATE_BUCKET`, `TF_STATE_LOCK_TABLE`, and `TF_STATE_REGION` values into the protected `rancher-signoff` environment variables; do not print or upload AWS credentials.

## Design Principle

This can be one repository if local and automated concerns stay separate:

- Local defaults stay simple and interactive.
- Actions defaults are headless, tagged, isolated, and disposable.
- Lane receipts are compact JSON artifacts so results can be read without
  scraping raw logs or uploading generated credentials.
- Safety infrastructure, especially Terraform state storage, is bootstrapped separately and reused.
