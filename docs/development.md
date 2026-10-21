# Development

Source builds, local checks, and backend reference for contributors.
For ordinary app use, start with the [quick start](../README.md).
For CLI and infrastructure test commands, see [Advanced Usage](advanced-usage.md).

- [Build from source](#build-from-source)
- [Build targets](#build-targets)
- [Backend organization](#backend-organization)
- [Ignored local state](#ignored-local-state)

## Build From Source

Contributors can still build and install the current checkout:

```bash
make setup
```

That development path embeds a checkout hint and requires Xcode Command Line
Tools, the Go version from [go.mod](../go.mod), and Node.js/npm. Re-run `make setup`
to replace the development app, or set `INSTALL_DIR` to install elsewhere.

```bash
make setup INSTALL_DIR="$HOME/Desktop"
```

## Build Targets

Useful source-development targets:

```bash
make help
make setup
make app
make panel-ui
make test
make release-plan
```

Maintainers can run `make release` to publish the first stable release as
`v1.0.0`, then increment the patch version on subsequent runs. Use
`RELEASE_BUMP=minor` or `RELEASE_BUMP=major` for larger version changes.
The command builds published GitHub source, publishes the release, and updates
the Homebrew Cask. See the [release guide](homebrew-release.md#publish-a-release)
for prerequisites and retry commands.

Development Wails builds store the checkout path in ignored local build hints.
Release builds instead stage checksum-verified, versioned runtime assets in Application
Support and do not depend on the checkout.

## Backend Organization

Backend extraction is incremental. The application currently composes services in
`terratest/control_panel.go`; infrastructure test entry points remain in `terratest`.
The extracted packages do not import `terratest`. The panel supplies application
state and identity resolution through constructors and callbacks:

| Package | Responsibility |
| --- | --- |
| `internal/imagelookup` | Registry search, image inspection, tags, provenance, and bounded command execution |
| `internal/prbuild` | PR-image verification and related issue-readiness evidence; depends on image lookup |
| `internal/cachelab` | Snapshot storage, SQLite queries and comparisons, capture jobs, and library synchronization |
| `internal/server` | HTTP handlers for those services, strict request decoding, and request-origin checks |
| `internal/workspace` | Retained-data paths, migration, and runtime-data module boundaries |
| `internal/history` | Observation storage interface, compatible JSON file backend, and chronological reads |
| `internal/panelsession` | Local app-session records, reuse checks, and ownership-aware cleanup |
| `internal/operations` | Worker admission, cancellation, and shutdown draining |
| `internal/awspricing` | AWS price-catalog queries using the existing environment credentials |
| `internal/localtools` | Local executable discovery and subprocess environment construction |
| `internal/registrycatalog` | Shared preferred-registry ordering |

Service tests live beside their implementation. The remaining panel adapters bind
these services to authentication, cluster identity, workspace paths, and worker
ownership. Application integration tests stay in `terratest`.

Cache Lab exposes detached library snapshots for ordinary reads and a locked
callback for evidence preservation. Callers must not mutate callback data or
re-enter the service from that callback. Its constructor accepts the worker manager,
cluster resolver, and command runner; consumers do not access its mutex or storage
fields. PR verification owns each readiness scan's evidence cache and image reuse.
Daily scheduling and saved reports remain application responsibilities.

Dependencies flow from the panel to HTTP handlers and services. Services never
import the HTTP layer or the application package. An architecture test enforces
these import boundaries. Remaining backend domains should move as complete
components, including tests, instead of adding more sibling files in `terratest`.

Keep new files focused on one responsibility. Before extending a large file,
consider whether the new behavior belongs in an existing domain module or an
independent package. Keep pure package tests beside their implementation and
application integration tests in `terratest`.

### Storage boundary and SQLite evaluation

Cluster observations now use `history.Store` (`Write`, `List`, `Delete`) with opaque
cluster IDs. `history.FileStore` remains the default and preserves the existing
hashed directories, event filenames, JSON payloads, permissions, and read order.
No user data is migrated. The application retains ownership of locking,
deduplication, redaction, purge authorization, and suppression of late writes.
The store override is set before workers start and must remain fixed during use.

Storage failures can now be injected independently of the filesystem. Regression
coverage verifies write retries do not lose observations through deduplication,
read errors propagate, failed deletions remain retryable, and late observations
cannot recreate purged history. File-backend tests cover legacy reads, isolation,
corruption, missing streams, replacement semantics, and detached payloads.

An initial SQLite read experiment lives only in
`internal/history/sqlite_benchmark_test.go`, using the project's existing
`modernc.org/sqlite` dependency. It is not a production backend. Reproduce with:

```sh
go test -buildvcs=false ./internal/history -run '^$' -bench 'Benchmark(FileStoreList|SQLiteHistoryList)' -benchtime=10x -count=1
```

On October 2, 2026, a local Apple M5 run produced:

| Observations in one cluster | JSON files, full read | SQLite prototype, full read |
| --- | --- | --- |
| 100 | 6.11 ms | 0.126 ms |
| 1,000 | 32.35 ms | 1.12 ms |

These are ten-iteration, repeated reads of small synthetic observations, with
setup excluded. Both paths decode the same event payloads and return complete
histories. This does not measure cold storage, realistic large payloads, writes,
concurrent workloads, crash recovery, or migration performance. It supports a
larger-history prototype, not a general performance guarantee.

**Recommendation:** retain JSON as the production default for this first step.
SQLite is a plausible next backend for retained cluster metadata, observations,
and operation records together. SQLite's documentation describes its suitability
for [local application storage](https://www.sqlite.org/whentouse.html) and
[atomic transactions](https://www.sqlite.org/atomiccommit.html). Our current purge
updates workspace metadata, deletes observation files, then removes operation
records; an observation-only database cannot make that whole operation atomic.
The current interface deliberately does not promise a transaction across stores.

Before switching, define a repository transaction covering the purge marker and
all three record types, test rollback and restart after interruption, and add a
versioned, resumable JSON import with record-count/payload verification and a
preserved source backup. Evaluate indexed/paginated reads, writer contention,
backup/restore, and representative workloads. Keep UI responses and identity
semantics stable. Test Lab, issue packages, and Cache Lab retain their existing
storage until each has its own domain boundary and migration coverage.

Run `make test-regression` for the local regression suite: frontend tests, internal
package tests with the race detector, selected backend lifecycle/history tests,
runtime-data preparation, and desktop runtime migration. This target uses temporary
fixtures and fake services; it does not provision infrastructure. The same target
runs in the local regression GitHub Actions workflow. Use `make test-web` for just
the frontend tests. The broader `make test` target also includes the repository's
other Go tests and their existing environment requirements.

## Ignored Local State

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
