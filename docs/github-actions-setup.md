# GitHub Actions Setup

This repo can run disposable Rancher head-build and prerelease sign-off lanes in
GitHub Actions while still keeping local `tool-config.yml` usage unchanged.

The GitHub Actions path is intentionally environment-gated:

- `automation-bootstrap` creates or updates the Terraform state backend.
- `rancher-signoff` provisions Rancher, optional Linode downstreams, optional
  direct `rancher/tests` suite runs, writes compact receipts, and cleans up.

Do not put cloud credentials, Rancher tokens, kubeconfigs, or generated `.env`
files in GitHub variables, logs, reports, or artifacts.

## Repository Environments

Create these GitHub environments under repository settings.

| Environment | Purpose | Recommended protection |
| --- | --- | --- |
| `automation-bootstrap` | One-time S3/DynamoDB state backend bootstrap. | Required reviewers. |
| `rancher-signoff` | Live AWS/Linode Rancher sign-off lanes. | Required reviewers. |

## Bootstrap Environment

`automation-bootstrap` needs these environment secrets:

| Secret | Required | Purpose |
| --- | --- | --- |
| `AWS_BOOTSTRAP_ROLE_ARN` | yes | AWS OIDC role that can create/update the S3 state bucket and DynamoDB lock table. |
| `AWS_REGION` | yes | AWS region for the state bucket and lock table. |
| `TF_STATE_BUCKET` | yes | Globally unique S3 bucket name for Terraform state. |
| `TF_STATE_LOCK_TABLE` | yes | DynamoDB table name for Terraform state locks. |

Store the same `AWS_REGION`, `TF_STATE_BUCKET`, and `TF_STATE_LOCK_TABLE`
values in the `rancher-signoff` environment before running a lane, and also set
`TF_STATE_REGION` there to the backend region. Save their original values in an
approved secret manager because GitHub will not display environment secret
values after they are configured.

Run `.github/workflows/bootstrap-terraform-state.yml` first with `apply=false`.
After reviewing the redacted plan, run it again with `apply=true`. The workflow
does not print or upload the backend names.

## Sign-Off Environment Secrets

`rancher-signoff` secrets:

| Secret | Required | Purpose |
| --- | --- | --- |
| `AWS_AUTOMATION_ROLE_ARN` | yes | AWS OIDC role used by live sign-off lanes. |
| `TF_STATE_BUCKET` | yes | S3 bucket from the bootstrap configuration. |
| `TF_STATE_LOCK_TABLE` | yes | DynamoDB lock table from the bootstrap configuration. |
| `TF_STATE_REGION` | yes | AWS region for the Terraform backend. |
| `AWS_REGION` | yes | AWS region for Rancher infrastructure. |
| `AWS_VPC` | yes | Existing VPC ID. |
| `AWS_SUBNET_A` | yes | Existing subnet for infrastructure node/security wiring. |
| `AWS_SUBNET_B` | yes | Existing subnet for infrastructure node/security wiring. |
| `AWS_SUBNET_C` | yes | Existing subnet for infrastructure node/security wiring. |
| `AWS_AMI` | yes | AMI used by Rancher infrastructure nodes. |
| `AWS_SUBNET_ID` | yes | Subnet ID used by EC2 instances. |
| `AWS_SECURITY_GROUP_ID` | yes | Security group ID used by EC2 instances. |
| `AWS_PEM_KEY_NAME` | yes | Existing EC2 key pair name expected by the Terraform module. |
| `AWS_ROUTE53_FQDN` | yes | Route53 zone/domain suffix used for Rancher DNS records. |
| `AWS_PREFIX` | optional | Owner/base prefix included in generated sign-off resource names. |
| `OWNER_FIRST_NAME` | yes | First name used in AWS `Owner` tags. |
| `OWNER_LAST_NAME` | yes | Last name used in AWS `Owner` tags. |
| `RANCHER_BOOTSTRAP_PASSWORD` | yes | Initial Rancher admin password rendered into generated `tool-config.yml`. |
| `LINODE_TOKEN` | yes for downstream lanes | Linode token used by Rancher to create the disposable downstream K3s node. |
| `DOCKERHUB_USERNAME` | optional | Docker Hub auth for RKE2 pulls when needed. Rejected credentials fall back to anonymous pulls rather than being installed on the nodes. |
| `DOCKERHUB_PASSWORD` | optional | Docker Hub auth for RKE2 pulls when needed. Rejected credentials fall back to anonymous pulls rather than being installed on the nodes. |

Infrastructure identifiers and owner fields are stored as individual secrets
for log redaction even though they are not authentication credentials. Do not
combine them into one JSON secret: GitHub cannot reliably redact substrings of
a structured secret. The workflows deliberately have no `vars.*` fallback for
these fields, so a missing protected value fails closed instead of appearing in
the runner-generated environment header. The values are scoped to the trusted
steps that need them rather than being inherited by the whole job.

When migrating an existing environment, copy the protected configuration
variables to secrets before deploying these workflows. After the updated
workflows have completed one validation run, delete the legacy variables; they
are intentionally ignored by the workflow and retaining them can cause future
configuration drift.

The workflow also masks generated state keys, AWS prefixes, Rancher admin
tokens, endpoints, and the generated Linode root password before noisy steps.
Before running the external `rancher/tests` checkout, it drops cloud
credentials and removes trusted planning/configuration files from the test
workspace. Cleanup reacquires short-lived AWS credentials afterward.

## Sign-Off Environment Variables

Only non-sensitive runner tuning remains in `rancher-signoff` variables:

| Variable | Required | Purpose |
| --- | --- | --- |
| `RANCHER_TESTS_REF` | optional | Ref to clone from `https://github.com/rancher/tests.git`; defaults to `main`. |
| `RANCHER_TEST_SUITE_SETTLE_SECONDS` | optional | Pause between direct `rancher/tests` suites; defaults to `30`. |

## Workflows

| Workflow | Creates cloud resources | Notes |
| --- | --- | --- |
| `signoff-plan.yml` | no, but it can dispatch the runner | Manual plan generation from `signoff-targets.json` or a single head/prerelease input. Dispatch suppresses an identical active lane. It also skips a previously successful immutable target unless `rerun_successful_lanes=true`; mutable `head`, `vX.Y-head`, and `vX.Y.Z-head` aliases are always reconsidered after the active run finishes. |
| `bootstrap-terraform-state.yml` | yes, only when `apply=true` | Creates or updates the persistent S3/DynamoDB backend. |
| `run-rancher-signoff-lane.yml` | yes | Runs one Rancher sign-off lane, optionally with Linode downstreams and direct `rancher/tests` suite runs, then cleans up. |

## First Live Run

After environments, secrets, and variables are configured:

1. Run `Plan Rancher Sign-Off` manually for a known immutable target, such as
   `v2.15.1-rcs-c936`, with `dispatch_runs=false`.
2. Run `Run Rancher Sign-Off Lane` with:
   - `rancher_version`: `v2.15.0-rc2`
   - `lane`: `framework-regression`
   - `keep_infra_on_failure`: `false`
   - `run_rancher_tests`: `false`
3. Confirm the run provisions Rancher, waits for readiness, renders a report,
   uploads a compact JSON receipt, and destroys AWS infrastructure.
4. Next, run `webhook-fresh-install` with `run_rancher_tests=false` to prove the
   single-node Linode downstream and downstream cleanup.
5. After those are clean, enable `run_rancher_tests=true` to clone
   `https://github.com/rancher/tests.git` and run the lane's suites in the same
   workflow job. The `framework-regression` lane runs framework regression plus VAI
   disabled for Rancher 2.11 and older and VAI enabled for Rancher 2.12 and
   newer. Downstream webhook lanes run webhook security settings for Rancher
   2.14 and newer when the actual Rancher chart should contain those settings.
6. For normal use, edit `signoff-targets.json` with the head builds or
   prereleases you care about and run `Plan Rancher Sign-Off` manually with
   `dispatch_runs=true`.

## Target Selection

Use `signoff-targets.json` as the source of truth for manually selected targets:

```json
{
  "targets": [
    {
      "rancher_version": "v2.14.1-alpha7"
    }
  ]
}
```

Supported target forms are:

| Form | Example | Mutability |
| --- | --- | --- |
| Current community head | `head` | Mutable alias. |
| Minor-line head | `v2.15-head` | Mutable alias. |
| Patch-line head | `v2.15.1-head` | Mutable alias for the newest matching immutable staging build. |
| Community commit head | `v2.15-0123456789abcdef-head` | Immutable commit build. |
| Prime patch commit head | `v2.14.5-0123456789abcdef-head` | Immutable commit build; the numeric patch replaces the `Z` in the general `vX.Y.Z-SHA-head` form. |
| Alpha | `v2.14.1-alpha7` | Immutable prerelease. |
| RC | `v2.15.0-rc2` | Immutable prerelease. |
| RCS | `v2.15.1-rcs-c936` or `v2.16.0-rcs-0844.1` | Immutable prerelease. |

Prefer a commit-specific head tag for sign-off. It identifies one build, makes
receipts reproducible, and lets the planner safely deduplicate a lane that has
already succeeded. The mutable `head`, `vX.Y-head`, and `vX.Y.Z-head` aliases
are useful for continuous smoke testing, but their contents can change without
their workflow run title changing. The planner therefore does not suppress
them because an older run with that title succeeded. It still suppresses an
identical active run, so concurrent planners do not launch duplicate lanes.

Head image publication changes as a release line moves through its lifecycle.
Current community lines can publish on Docker Hub, while Prime-only lines
publish on `stgregistry.suse.com`. Resolution is publication-aware: the planner
locates an exact tag for which both `rancher/rancher` and
`rancher/rancher-agent` exist in the same supported registry and declare the
same canonical head tag, then records those exact image references for the
lane. Do not infer the registry from the version number or assume a line remains
on Docker Hub. For a mutable alias, the exact resolved references in the
generated plan and receipt identify what that run actually tested.
Parent-planned lanes receive that immutable resolved tag, so the alias cannot
move between planning and execution.

Patch-qualified heads are staging-only, including both `v2.15.1-head` and an
exact `v2.15.1-SHA-head` tag. Because staging does not publish a literal patch
alias image, the planner lists immutable `v2.15.1-SHA-head` tags, verifies their
canonical OCI metadata and matching server/agent pair, and pins the newest pair
by the later of the server and agent OCI creation times. Both timestamps are
required. Tags from a different patch and Docker Hub images are not candidates.

Prime head images and Prime chart availability do not always begin on the same
day. Prime-head chart resolution uses an exact eligible head chart (normally
Optimus) when available; if chart publication lags, it may use a compatible
Prime or same-line community baseline while retaining the exact staging Prime
server/agent images. Released, non-head Prime targets remain strict and do not
fall back to community charts.

To keep a target in the file without planning it, set `enabled` to `false`.

Use `keep_infra_on_failure=true` only for manual debugging. It can leave AWS and
Linode resources running.

## Safe Artifacts

The sign-off workflow uploads one compact, redacted JSON receipt per lane. The
receipt omits Terraform state keys and AWS prefixes and keeps a field-allowlisted
install/upgrade image resolution. That resolution records
the requested alias, exact Rancher server and agent references, registry,
digests, source revisions, phase-specific distro, and chart source needed to
audit a mutable-head run. Upgrade lanes resolve the previous stable install in
`auto` mode, then apply the target build's Prime/community chart policy only to
the upgrade phase.
The receipt omits live Rancher URLs, kubeconfigs, generated environment files,
raw Terraform outputs, copied logs, and the unsanitized resolution files.

It does not upload:

- generated suite `.env` files
- Rancher admin tokens
- kubeconfigs
- Terraform state files
- AWS credentials
- Linode tokens

## Cleanup

Normal sign-off runs clean up automatically when `keep_infra_on_failure=false`.
