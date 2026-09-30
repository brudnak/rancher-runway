# Test Lab

Test Lab is under **Local tools** and in the Home directory. This initial version supports source discovery, validation documentation, a reusable config library, static configuration preflight, local execution, saved plans/results, and GitHub account/repository onboarding. **Workflow installation and cloud dispatch are not enabled in this version.**

## Local workflow

1. Load `main`, a branch, a tag, or a commit from public `rancher/tests`. Runway resolves the ref to a full commit SHA and reads its source archive. Discovery does not compile or execute the repository.
2. Search the `validation` catalog. Select whole test entry points/suites or individual statically discoverable testify methods. Source links use the pinned SHA. Dynamically generated subtests appear only when executing; some suites also create names through helpers that cannot be discovered statically.
3. Select a detected Rancher or enter its hostname. Sign in with a local Rancher password on the default **URL + password** tab, or choose **URL + API token** to paste a Rancher **user** token. Supply the suite's required `cattle-config.yml`. The guided fields cover the common connection section; YAML editing preserves provider-specific fields and comments. Use **Cattle-config library** for reusable templates and **Browse READMEs** for the selected revision’s READMEs, kept beside the test catalog.
4. Set Go build tags and a per-suite timeout. Review exact commands, source revision, and target. Type `confirm` to execute locally.
5. Follow Activity & results, pause the log view, inspect failed test events, or copy a result summary. Stopping may interrupt suite cleanup; inspect the target afterward.

Local test code runs as the current user. Isolating working files and environment is **not** an OS security sandbox. Only run revisions and tests you trust, against a target you are authorized to modify. Integration tests can provision resources, change authentication, remove resources, and incur charges. The local runner does not take Runway's infrastructure lifecycle lock; do not run modifying tests against a Rancher still being provisioned or cleaned up.

### Go selectors

The selection format is the same mechanism used in Rancher's [Platform QA test runner](https://github.com/rancher/tests/blob/main/.github/workflows/platform-qa-test-runner.yml): `test_selector` is passed to Go's `-run` flag. See also [Go testing flags](https://pkg.go.dev/cmd/go#hdr-Testing_flags).

- Whole suite: `^TestConfigMapTestSuite$`
- One method: `^TestConfigMapTestSuite$/^(TestSteveGeneratedFields)$`
- Several methods in a suite: `^TestSuite$/^(TestFirst|TestSecond)$`

Each slash separates a test/subtest matching level. Runway anchors names, escapes regex metacharacters, groups methods from the same suite into one command, and runs separate suites sequentially. Selecting the whole suite supersedes individual methods. Suite setup/cleanup still execute. A successful Go command that never starts the selected test is recorded as a failure rather than a passing result. Counts include both suite and nested subtest events.

The runner uses Go JSON output, `-count=1`, `-buildvcs=false`, `-parallel=1`, and `-p=2`. Build constraints are checked against selected tags before starting. Compiler/package prerequisites can still fail. The timeout applies per suite, with ten additional minutes to bound compilation and module downloads. Go can download the toolchain required by `go.mod`. Modules use the public Go proxy; VCS fallback is disabled. Suites needing private modules or external tools may require further support.

On macOS, install Go and Xcode Command Line Tools (or Xcode). Before compiling tests, Runway locates the selected macOS SDK and checks that the C compiler can read the system headers needed by Go's native dependencies. It explicitly configures `SDKROOT` and Apple's compiler shims in the isolated environment, so launches from Finder do not depend on shell setup. An explicit `DEVELOPER_DIR` is honored; shell compiler overrides and flags are not inherited. A missing or broken SDK stops the run with installation guidance before module downloads or test execution.

### Connected cluster history

Use [Test Packages](test-packages.md) to keep a larger issue investigation: write manual cases, preserve reproduction and validation sessions, and attach finished Test Lab runs alongside Cache Lab evidence. Package-owned evidence copies survive ordinary lab cleanup.

Test runs and saved plans link to stable cluster IDs. **Clusters** shows the same results alongside that cluster's Cache Lab snapshots. Set a cluster nickname there or in either lab; every view uses the updated name while IDs, recorded targets, and versions remain visible. Activity and saved plans are grouped by cluster. Opening a result or snapshot from Clusters goes directly to that item in its lab.

Runway associates a known Rancher URL or pinned kubeconfig/context only when the match is unambiguous. Existing unlinked records are migrated using the same rule. If a hostname has been reused by multiple cluster histories, select the intended cluster explicitly. Renaming a cluster never moves files or changes credentials. Result and snapshot renames and deletions operate on the shared records, so changes appear everywhere.

Destroy keeps this local history by default. Its separate **Test Lab** and **Cache Lab** cleanup options remove linked test results/logs and cache workspaces/snapshots only after infrastructure cleanup succeeds for the selected run. Failed or canceled destroys preserve history; active lab work is retained with a warning. Saved test plans and reusable cattle-config templates are retained. Historical cluster workspaces stay available after infrastructure is gone.

Cluster names and stable IDs are saved in the private local `automation-output/control-panel/cluster-workspaces.json` manifest. Credentials remain in their existing lab stores. Saved work is stored on the computer running Runway, not uploaded to Kubernetes.

### Storage and credentials

Data lives in `automation-output/control-panel/test-lab`:

- `library.json`: catalog, plan metadata, run results, non-secret GitHub connection metadata.
- `<commit>.tar.gz`: cached source archives.
- `<run-id>.log`: the most recent 2 MiB of sanitized activity.
- `go-cache`, `go-modules`: reusable Go compilation/module caches.
- `work-<run-id>`: isolated source, temporary HOME, and a mode-0600 configuration file. Removed on completion/cancel; interrupted work folders are removed on restart.

Runway creates a small `go.mod` boundary around runtime data so downloaded dependencies are not mistaken for application source by Go or Wails. App builds and `make test` also prepare boundaries around existing `automation-output` directories; no downloaded modules, plans, or results are removed.

A saved plan stores its full YAML in macOS Keychain only when **Remember configuration** is selected. Unless explicitly saved in the separate Config library, other credentials stay in the current UI draft and temporary execution file. Tokens and YAML are not stored in localStorage or the library manifest. Known configuration string values and common token patterns are redacted from retained output; review logs before sharing because tests may create additional credentials unknown to Runway.

The app blocks normal close while a local test is active. After a crash or force quit, the next launch marks unfinished runs interrupted. Inspect local processes and target resources before running again; do not assume external test cleanup completed.

Delete saved plans/results through their confirmed local cleanup actions. Deleting a result never deletes Rancher resources.

### Inline READMEs and full YAML editor

The explorer indexes README files anywhere under `validation` in the cached, pinned source archive (85 at the initial tested revision). Search titles and paths; filter to guides in selected packages and their parent directories; switch between rendered Markdown and exact raw Markdown. Relative links to other indexed READMEs stay in the reader. Code blocks have a copy action. Raw HTML is escaped and images are links, so opening a guide does not run scripts or load external images. The reader supports common headings, lists, quotes, tables, inline formatting, and fenced examples; use Raw MD for unsupported Markdown extensions.

The YAML editor provides line numbers, syntax colors, two-space indentation, automatic indentation on Enter, Find, cursor position, and local syntax diagnostics. Esc then Tab leaves the editor. Guided connection edits use the YAML document model to retain comments and other sections. Drafts and README state survive background polling and workspace navigation within the app; unsaved drafts are not persisted across app closure.

### Reusable config library

The explorer shows each folder's files directly beneath it. Expand or collapse folders, search names, and use arrow keys to navigate; Enter opens a config and Shift F10 opens its actions. The item menu offers duplicate, export, rename (folders), and confirmed deletion. Create folders inline. **Rename or move** in the editor changes a config's name or destination when saved.

**Use for run** loads the current editor contents into an independent working copy. Changes to that run draft never update the template, and the previous run draft can be restored immediately. **Save changes** or ⌘ / Ctrl S explicitly updates the saved config; stale revisions are rejected. Switching files with unsaved edits offers Save & continue, Discard edits, or Keep editing. Deletion requires typed confirmation, and folders must be empty before deletion.

#### Import and export

- **One config:** Export saved YAML from its menu or editor footer. This preserves the exact saved document, including comments; unsaved editor changes are not exported.
- **One folder:** Export folder from its item menu. The `.runway-cattle-configs.json` bundle includes that folder and its configs, including an empty folder.
- **Entire library:** Export library writes `runway-cattle-configs.json` with every folder and config, including configs at the library root.

Exports go to Downloads with a unique prefix, mode 0600, and no overwrite of existing downloads. Acknowledgment is required because the unencrypted export includes all saved values, including tokens and provider credentials. No export is uploaded automatically.

Import accepts individual `.yml` / `.yaml` files (128 KiB maximum) and bundles (80 MiB maximum). A preview lists destinations and collision renames before writing anything. Choose a destination for configs without a folder. Bundled folders always become new folders; matching names get an “(imported 2)” suffix. Existing files are never overwritten. The preview is bound to the exact input and current library; if either changes, review again. Import validates every config, stages private files, then atomically publishes the library metadata; a failed commit removes the newly staged files.

The bundle is readable JSON with `format: "rancher-runway/cattle-configs"`, `version: 1`, `exportedAt`, `folders` (`id`, `name`), and `configs` (`name`, `folder`, `yaml`). Folder references are remapped to new local IDs on import. YAML is stored as an exact string so formatting and comments survive round trips.

Saved configs use the OS user configuration directory, **outside the source checkout**, so they are not version-control files:

- macOS: `~/Library/Application Support/Rancher Runway/Test Lab/configs`
- Other systems: `os.UserConfigDir()/Rancher Runway/Test Lab/configs`

The directory is mode 0700 and files are mode 0600. Config contents are unencrypted YAML: they can contain provider credentials and may be included in OS backups. Names/folders are stored separately in `index.json`; list responses contain no YAML or tokens. Folder organization is logical; filenames use opaque IDs. Writes replace files atomically; updates use revision checks. The app never uploads templates or writes them to browser storage. Limits: 128 KiB per YAML document, 500 configs, and 100 folders. Templates can contain incomplete Rancher connection fields, but must be valid single-document YAML mappings.

### Configuration preflight

**Check config** runs without compiling tests, loading Go dependencies, or contacting Rancher. It also runs when opening Review & run. The source must match the selected catalog revision. The scan checks:

- YAML mapping syntax, duplicate keys, document count, and size; basic Rancher hostname/token shape.
- Direct Shepherd `config.LoadConfig` and `operations.LoadObjectFromMap` calls in selected packages, including setup and other tests in those packages.
- Resolvable local Go constants and struct definitions, YAML/JSON field names, and supplied value types (including nested structs/lists).
- Direct testify `require.NotEmpty` / `assert.NotEmpty` expectations on loaded struct fields, with source file/line evidence. For example, hosted-tenant RBAC flags an empty `tenantRanchers.clients` list.

Source findings are **advisory**: all package files are inspected, so other tests, build conditions, defaults, and runtime branches may change what is actually required. No field is assumed required merely because it appears in a Go struct. External dependency types and helper call graphs are not fully resolved; unresolved references/types and the coverage description remain visible. An empty findings list is not a guarantee of completeness, credentials, provider access, or test success. Editing config or selections marks results stale. No config values are included in findings.

### Rancher token caveat

A Kubernetes `system:admin` kubeconfig does not automatically identify an active Rancher user. Rancher's [Token API](https://ranchermanager.docs.rancher.com/v2.13/api/workflows/tokens) requires a valid active Rancher user to create a user token. The shared **Use this Rancher** picker fills the hostname from Runway’s recorded management environments, deduplicates Rancher URLs, and excludes downstream kubeconfigs. Provisioning targets remain visible but may not accept connections yet. Refreshing the list never replaces your draft. Test Lab requires an HTTPS hostname without a path, as expected by `rancher/tests`.

The default **URL + password** tab signs in as the local username you supply (default `admin`), creates a named Test Lab API token, and inserts it into `rancher.adminToken`. Comments and other YAML fields are preserved. Choose 1 hour, 8 hours, 24 hours, 7 days, or 30 days; Rancher may enforce a shorter maximum. The token has that user’s permissions. SSO users should continue pasting a token from Rancher. A system kubeconfig is never used to impersonate a Rancher user.

Passwords are cleared after each attempt and never saved, logged, or passed through command arguments. Requests require HTTPS (loopback HTTP is supported by the shared Cache Lab connection endpoint), verify certificates unless you explicitly opt out, and never follow redirects. Runway logs out the temporary sign-in session after token creation or a failed creation attempt. A cleanup failure is shown with its session ID. If a request times out, check Rancher’s API & Keys before retrying because the server may have created a credential.

The new token stays in the working draft until you explicitly save a plan to Keychain or a config to the library. Generating another token does not revoke earlier ones. Revoke tokens in Rancher’s **API & Keys** when finished; deleting local plans or Cache Lab workspaces does not revoke remote credentials.

## One-time GitHub App registration

The maintainer registers one GitHub App, then distributes its **public client ID and app slug**. End users authorize that same App; each user controls its installation and private repositories. No hosted token broker, App private key, or client secret is embedded in Runway.

1. Open [GitHub App registration](https://github.com/settings/apps/new). Choose a product name/homepage and allow installation by other accounts as appropriate for distribution.
2. Enable **Device flow**. Keep expiring user access tokens enabled. Webhooks are not required for this onboarding implementation.
3. Request Metadata read access and the **Repository creation (write)** permission for the optional create-private-repository action. GitHub also supports Administration write for repository creation, but prefer the narrower creation permission where available. Do not request deletion permissions for a Runway deletion feature: that feature does not exist.
4. For the later managed Actions execution phase, review the additional Contents, Workflows, Actions, and Secrets write permissions before enabling that phase. Current onboarding does not install workflow files, upload Rancher secrets, or dispatch workflows.
5. In **Test Lab → GitHub connection → Integration configuration**, enter the App client ID and slug. These public settings persist in the local library. Distributions can supply `RUNWAY_GITHUB_CLIENT_ID` and `RUNWAY_GITHUB_APP_SLUG` before Runway starts.
6. Use **Connect GitHub**, enter the displayed code on GitHub, then authorize the App. Runway honors polling intervals, device-flow expiry, and GitHub's `slow_down` responses. It stores the user/refresh tokens in macOS Keychain and refreshes without a client secret, as supported for device-flow tokens.
7. Grant the App access to selected repositories. Use an existing active private repository or create a dedicated private repository in the authenticated personal account. Creating a repository may require a subsequent installation-access grant. Organization-owned existing repositories can be connected subject to organization policy; organization repository creation is not included in this first pass.

Authorization, App installation, and choosing a repository are separate states. A token has the intersection of the App's permissions and the user's permissions. GitHub sign-in alone does not grant access to every repository. Review the [GitHub device-flow documentation](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-a-user-access-token-for-a-github-app), [refresh-token documentation](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/refreshing-user-access-tokens), and [repository API permissions](https://docs.github.com/en/rest/repos/repos#create-a-repository-for-the-authenticated-user).

### Disconnect and manual deletion

**Disconnect repository** only forgets the local link. **Disconnect GitHub** removes the local connection and Keychain token. It does not revoke an authorization or uninstall the App on GitHub; the interface links to GitHub settings for those actions.

For a repository created through Runway, **Delete repository on GitHub…** displays instructions and opens repository settings. The user reviews GitHub's warnings and completes deletion there. Runway has no repository deletion API/action. Missing access is described as **Repository unavailable**, because removal of permissions, a rename, or deletion can all cause that result.

### Remaining cloud work

Workflow review/install/update, per-run encrypted secret delivery and cleanup, dispatch correlation, remote cancellation, result ingestion, and private-network reachability checks are the next execution milestone. They are deliberately shown as unavailable, not simulated by a successful connection. GitHub App registration and live end-to-end testing are also still needed. No user's real repository or Rancher was changed while implementing or verifying this initial version.

### Browse files and read alongside tests

The catalog's **Validation explorer** shows test files and READMEs inside their
actual folders. **Find folders or files** filters as you type, retaining parent
folders for context. Entering `vai` reveals the `steve/vai` folder and its files.
Use **Tests** or **READMEs** to narrow file types, or **All files** to see both.
Documentation-only folders and the root README are included.

Click a test file to see its runnable suites and individual cases in the center.
This does not select or run anything. A suite can span several files: the view
shows cases in the selected file and explains when selecting the whole suite
also includes other cases. Click a folder to browse tests in it and its children,
or **All runnable tests** to clear the file/folder filter.

Click a README in the explorer to open it alongside the tests. Rendered Markdown,
raw Markdown, copy actions, and internal README links stay in the reader. The
explorer highlights the open document while preserving the selected test file.
**README & guides** opens the package's guide or identifies the nearest parent
guide. Raw Go source is not shown. On narrow windows the reader stacks below
the catalog; documentation and file navigation never change run selections.
