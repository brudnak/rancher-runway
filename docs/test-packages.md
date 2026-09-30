# Test Packages

Test Packages keeps an issue's manual test plan, reproduction attempts, fix validation, and lab evidence together. Open it under **Investigate & prepare**, choose **Create test package** on an Issue Radar card, or start from a cluster workspace. Packages are personal and local first; sharing and private backup are explicit actions.

## Milestone library

The library groups packages into named milestone buckets such as **v2.16.0 · Frameworks**, with an **Unfiled** area for new and imported investigations. Create a bucket, rename it when release plans change, and use its menu to reorder it. Empty buckets can be removed. Buckets are local organization; renaming or moving one does not update a GitHub milestone.

Drag a package to another bucket or position, or use its **Move to bucket**, **Move package up**, and **Move package down** actions. Package identity, saved plans, recovery drafts, and preserved sessions stay intact. Search and status filters apply inside the buckets. Placement and order are stored in the private `.library.json` index separately from package revisions. Concurrent organization changes are rejected; refresh before retrying.

Choose **Export this bucket** or **Export library** to review a bulk export. The ZIP follows bucket and package order and contains individual portable `.runway-test-package.json` bundles, Markdown reports, `library.json`, and a readable `README.md` index. Private notes and copied logs/databases are excluded by default; include them explicitly after reviewing their contents. Only saved plans and observations are exported. Exports use private file permissions and unique filenames in Downloads, with a 256 MiB total uncompressed limit. Export a smaller bucket or omit evidence when it exceeds that bound.

Unzip the archive and import individual package bundles through the existing preview/import flow. This first bulk-export version does not import the ZIP or restore bucket placement automatically. Each package imports as an independent local copy in Unfiled; the included index records the original organization and order.

## Link a case to Test Lab

In the case editor, expand **Automation reference**, then **Choose Test Lab plan**. Save a plan in Test Lab first if none are available. Linking copies its selections, exact `rancher/tests` source commit, reference, build tags, and timeout into the case; it does not copy credentials. Save the package plan before starting a session. Existing sessions retain their original automation source, even if the plan or saved Test Lab plan changes later.

In an active session, select the linked case and choose **Review & run in Test Lab**. Test Lab loads that frozen source and selection, sets the session's recorded Rancher target, and keeps its normal configuration/preflight/command review and typed execution confirmation. Supply credentials in Test Lab. Runs must match the recorded target and cluster, source, selection, tags, and timeout to remain linked. Use **Detach run from package** to perform a separate experiment with different options.

When a linked run finishes, Runway automatically copies its result metadata and sanitized retained log into the originating session as case-scoped evidence. The metadata includes the exact source commit, selected tests, options, status, and test results. Completed package sessions, reports, baseline comparisons, and portable exports retain that evidence after Test Lab cleanup. The case outcome and session finding remain explicit tester decisions; a passing command does not automatically validate every manual assertion.

A session cannot be finished while its linked run is awaiting preservation. If copying evidence fails, Test Lab displays the reason and keeps the original result. Opening or refreshing Test Packages retries unsaved finished results, including interrupted runs recovered after restart. Repeated preservation does not duplicate evidence. A removed package/session or an already completed session cannot be rewritten; use the original Test Lab result to document another active attempt.

## Plan, record, preserve

1. Give the package a title, issue link, summary, and workflow status. Link the fix pull request when one exists, with an optional label; the link appears on the package heading and in reports. **Look up on GitHub** reads the pull request through the Test Lab GitHub connection and offers its title as the label and any issues it says it closes as the issue link. The lookup is explicit and read-only; nothing is saved until you save the plan, and it does not verify that those issues exist. Keep discussion notes and follow-up questions in Notes. Issue Radar may provide a shortened description; verify it against the original issue.
2. Write cases with preconditions, steps, expected behavior, and optional automation follow-up. Use the case preview to read the plan as a tester; insert, duplicate, and reorder steps as the plan develops. Runway automatically saves a private recovery draft as you write, including incomplete cases and working notes. Wait for **Draft secured locally** before closing. **Save plan** explicitly establishes the checkpoint used by new sessions, reports, and exports. Editing this plan does not rewrite existing sessions.
3. Start a reproduction, validation, or exploration session. Choose a recorded cluster or enter environment details manually. The environment preview includes available recorded versions, deployment details, images, immutable image IDs, and a redacted copy of the saved Helm install command. Previously observed cluster details are reused with their observation times. Choose **Refresh from cluster** to read the exact Rancher server, Kubernetes server, webhook chart version, and current container images before starting. The session freezes those details and the current cases; starting itself does not contact Rancher. A validation session also records the fix under test, pre-filled from the package's fix link, and keeps that reference even if the package link changes later. **Look up the current head commit** fills in the commit under test from GitHub; for a merged pull request you can choose the merge commit instead, or enter a commit by hand. The recorded commit states what you meant to test. Runway does not verify that the candidate build contains it. Missing details remain unknown, and failed probes retain earlier observations with their original timestamps.
4. Mark steps as you perform them, then record each case's outcome and notes. A checked step records a timestamp, **not a passing result**. These are manual markers, not an audit of actions performed in Rancher.
5. Attach existing Test Lab results, Cache Lab snapshots, written observations, or a live Rancher/webhook log snapshot. Complete the session with an explicit finding and conclusion. Completed sessions are preserved; start another session for another attempt.

**Where this stands** on the Overview shows four derived stages: test plan, reproduction, fix, and validation, with a suggested next step and a button that opens it. Stages are computed from saved cases, preserved sessions, and the fix link every time they are shown, so they cannot drift from the records. The latest session of each purpose determines its stage; exploration sessions do not move it. The summary never changes the package status, chooses a finding, or marks a case passed. The same table appears as **Progress** in reports.

Reproduction and validation have different meanings. A failing behavioral case can demonstrate successful reproduction of the original bug. The session finding stays separate from individual case outcomes, and cases you did not run remain **Not run**. Marking a package Verified is an organizational choice, not an automatic claim that every case passed.

Sessions survive app closure. Leaving the workspace does not finish an active session. Plan editing and background status refreshes do not reset session history. Unfinished case observations are also recovered after reopening. Stale saves are rejected so another view cannot silently overwrite newer work. If writing changes in another window, review both copies and choose what to keep. Observations whose sessions were changed, finished, or removed are retained separately for recovery; they never rewrite preserved results.

The progress summary treats a validation of a different fix (or a session with no recorded fix) as needing attention when a current fix is linked. Historical findings stay preserved. Reports restricted to one session derive their progress from that session only. Changing the fix URL clears the Overview lookup so its label and linked issues cannot be applied to another pull request.

## Validate against a baseline

Finish a reproduction session, then choose **Validate against this baseline**. Select the candidate environment or describe it manually, review its versions, and start the validation. The new session uses the baseline’s exact frozen cases with fresh outcomes and step markers—even if the current package plan has since changed. Starting does not run tests or change the selected cluster.

During execution, the baseline’s observation stays beside the current case (above it on smaller screens). **Next unrun case** moves to the next unfinished case; blocked cases remain available to revisit. Choosing an outcome does not automatically advance or mark untouched steps performed.

The comparison shows baseline and candidate outcomes, case-scoped evidence counts, coverage gaps, and recorded environment differences. Cases are matched by stable identity and checked for content changes, so renamed or materially edited cases are never silently treated as equivalent. Unknown versions remain unknown. Outcome changes alone do not establish that a fix caused the difference; the session finding is still your explicit conclusion.

Each candidate owns a frozen baseline comparison record, including cases, results, environment, and evidence summaries. It survives restart, source-session deletion, and portable export/import. Baseline attachment files remain owned by their original session; deleting that session removes those files while retaining the candidate’s comparison summaries. Keep or export the source session when those original files are needed.

## Cluster history and evidence

Sessions link to stable cluster IDs and appear under that cluster's **Test Packages** section. Current cluster nicknames are used for navigation; the session retains the environment name and details recorded at the time.

Attached evidence belongs to the package:

- A completed Test Lab run contributes result metadata and a copy of its retained activity log when available.
- A Cache Lab snapshot contributes snapshot metadata. Copying the SQLite database is optional and explicit, with a 32 MiB limit per database.
- An observation records written evidence without claiming that logs were collected automatically.
- **Get log snapshot** reads a selected Rancher or rancher-webhook container in `cattle-system`, then preserves a package-owned log file and capture metadata. Select a time window, a line limit, and optionally the previous container instance. Attach it to the session, a case, or a specific step using **Get logs for this step**.

Cleaning up a source test result, cache workspace, or cluster does not erase copies already attached to a package. Conversely, deleting a package or its session only removes that package's local data; it does not clean up a cluster or alter the original lab record. Completed sessions cannot have their evidence rewritten.

Live log snapshots require an active session started with a currently registered, kubeconfig-backed Rancher management cluster. The source is checked before and after capture; changed pod identities, restarts, changed connections, stale package revisions, and completed sessions cannot silently produce a mislabeled attachment. Each capture is bounded to 10,000 lines within the last 24 hours and 4 MiB of retained logs, with a timeout and no streaming. Requested windows are subsets of retained Kubernetes logs, not a complete audit. Recognizable credentials are redacted, but arbitrary application log content may still be sensitive and should be reviewed before sharing.

Checking a step does not automatically capture logs. Test Packages does not run a case or recreate infrastructure from imported environment metadata. Imported plans and preserved version information guide reproduction; they do not guarantee that every environmental dependency has been captured.

## Reports and portable bundles

Preview and copy a Markdown report for an issue comment. Reports describe cases, frozen sessions, outcomes, findings, and evidence, including the linked fix, the progress table, the fix and commit each validation session recorded, and a case comparison and environment differences for each baseline-linked validation. Sharing does not post anything to GitHub. Private package notes are excluded unless explicitly selected. User-authored case text, conclusions, and evidence descriptions still require review before sharing; Runway does not promise complete secret detection.

Export writes a readable `.runway-test-package.json` bundle and a Markdown report to Downloads. Choose which artifact bytes to include. Unselected artifacts remain described as omitted rather than being represented as portable files. Database contents and logs can contain sensitive data, so they are excluded by default.

The versioned archive envelope uses:

```json
{
  "format": "rancher-runway/test-package",
  "version": 1,
  "exportedAt": "...",
  "package": { "id": "...", "revision": "...", "cases": [], "sessions": [] },
  "artifacts": []
}
```

Each included artifact has an opaque name, media type, SHA-256 checksum, and base64 data. Imports validate the schema, bounds, references, and checksums before offering a preview that names the package's issue and fix links. Confirming creates a new local package with original lineage; it never overwrites another package or executes tests. Sessions imported while active become preserved, incomplete attempts. Imported history is identified as imported rather than work performed on this computer.

## Optional private GitHub archive

Connect GitHub through Test Lab's existing connection, then select a private repository in the package's Archive view. Inspect the destination and inclusion options before saving. Runway uses the same portable bundle format at a fixed path under `packages/`; local export and remote restore share the same validation rules.

Backup is explicit, not continuous synchronization. The save checks the inspected local revision and remote file revision so a changed remote package is not silently overwritten. A failed remote operation leaves local work in place. Restore loads and previews a bundle, then imports an independent local copy after confirmation. A private repository can be created from the archive flow; repository deletion remains a manual action on GitHub.

The GitHub integration must be configured and authorized for repository contents access. An existing Test Lab connection does not imply access to every repository. Archive operations do not install workflows or run Actions. Binary-heavy investigations can exceed the bounded bundle/API limits; export selected evidence separately or split the package.

## Local storage and limits

Packages live in the ignored runtime directory `automation-output/control-panel/test-packages`, with a private `package.json` per package and package-owned files beneath `artifacts/`. Writes are atomic and revisioned. Unfinished writing lives separately in a private `draft.json`, with its own revision and an 8 MiB bound; it is excluded from reports, portable bundles, and GitHub backup until saved as a plan or observation. Files are unencrypted on disk with private filesystem permissions; include them in your own backup policy. Invalid or unrecognized existing data is preserved and reported rather than replaced.

The initial limits are 500 packages, 250 cases per package, 100 steps per case, 500 sessions per package, 250 evidence items per session, an 8 MiB manifest, 32 MiB per artifact, and 64 MiB of evidence per package. Portable bundles have their own bounded encoded size. Large database investigations should use selective evidence or separate packages.

