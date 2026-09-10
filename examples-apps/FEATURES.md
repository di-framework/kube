# Verify main before releasing 5.3.1

These six apps consume the real framework packages from a chosen main worktree.
They cover the additions from PRs #414–#423. The verification command exits
nonzero when a build, Kubernetes validation, deployment, or live probe fails.
See the [recorded verification](verification-main.md) for the latest results and historical blockers.

| App | Local verification | Kubernetes acceptance |
| --- | --- | --- |
| `private-checkout` | Named service injection, authorized checkout, rejected unbound caller | `/verify` through the compiled component; these two services share one component |
| `static-site` | Packaged HTML/JSON, MIME and cache headers, ETag/304, HEAD, missing and traversal paths | The same assertions through HTTP ingress |
| `actor-counter` | Serialized calls, transactional rollback, migrations, deduplication, SQLite restart, inspection/reload/reset, actor CLI; separate RPC test over loopback HTTP with grants and deadlines | Generated actor adapter on the PVC-backed storage host; remote increment/read/rollback |
| `scheduled-maintenance` | External mode suppresses timers, resolves injected audit service, awaits completion | Generated CronJob invoked manually and completion confirmed from logs |
| `durable-receipts` | Producer idempotency, persisted queue restart, retry/backoff, dead-letter retention, manual retry, queue CLI | HTTP enqueue and duplicate submission, followed by polling for completed consumption on the storage host |
| `schema-migrations` | SQLite schema upgrade ordering, dry run, history, restart, migration CLI | Build the SQLite-backed component, deploy on the storage host, and invoke `/verify` to check schema and migration history |

The actors and schema examples deliberately use the new SQLite APIs. Local Bun
success is not evidence that those APIs work inside QuickJS. The build and
Kubernetes steps expose that boundary. The private binding example verifies
in-process service bindings, not communication between separate Kubernetes workloads.
The actor RPC companion uses real HTTP on a temporary loopback port, in the Bun
process; it is separate from the generated wasmCloud adapter.

## Run

Build an isolated checkout of the framework main revision first. The workspace
can select it without moving either repository's current branch:

```sh
# From examples-apps; replace with your built main worktree.
DI_FRAMEWORK_DIR=/absolute/path/to/di-framework-main bun run link:framework
bun run prepare:features
bun run check
bun test

# Uses the existing local Kubesolo instance and its dedicated kubeconfig.
# Set DI_KUBE_BIN if the CLI binary lives outside this worktree.
DI_KUBE_BIN=/absolute/path/to/di-framework-kube bun run verify:features

# Existing deployed components are a separate regression baseline.
bun run smoke
```

`link:framework` defaults to `../../di-framework`, supports `DI_FRAMEWORK_DIR`,
and records the selected commit in `.local/framework.json`. The verifier checks
that the revision and all package links still match. Build that worktree again
after changing the framework. Bun links are user-wide; another worktree can
replace them. Queue handlers import the public `@di-framework/queues` decorator
so the linked dispatcher and handler use the same registry. Mixing source and
compiled imports through the framework's own tsconfig aliases can create two registries.

The verifier regenerates deterministic static fixtures, runs type checks and the
seven local acceptance tests, builds every selected component, renders manifests
using the installed plugin, and submits them to the actual API server with
`--dry-run=server --validate=strict`. It deploys only components that pass both
build and validation. HTTP checks assert behavior, not just operator readiness.

```sh
bun run verify:features static-site private-checkout
bun run test:features
```

Artifacts and individual command logs are in `.local/verification-main/`.
`report.json` records the framework SHA and every result. Generated component
files remain in each app's `.di-framework/` and `dist/` directories.
The annual cron schedule avoids continuous probe jobs; acceptance creates a
uniquely named Job, waits for completion, then removes only that Job.
The SQLite tests use temporary directories and remove them on completion.

The queue probe submits a uniquely identified receipt through the generated HTTP
control API, submits the same idempotency key again, and polls until that receipt
is completed. Native tests separately cover retries, dead letters, and persistence
across restart. The live probe does not establish those lifecycle behaviors.

A full successful feature run also rebuilds and deploys all example apps before
running the 64-check smoke suite. Missing deployment, live, or smoke evidence
fails the aggregate result. Docker Desktop and the existing Kubesolo instance
must be running before invoking the verifier.
