# Current main verification

**Passing against framework main `e4d3eda` on 2026-09-10.**
The full verifier passed **44/44 gates** after rebuilding all 21 framework
packages from a clean checkout. TypeScript checks and all **51 local tests** pass.
All six feature apps pass component builds, strict server validation, deployment,
and live behavioral probes. The complete set of 21 example apps was then rebuilt
and deployed, and the **64/64 live API regression checks** passed.

See the [fresh report and logs](verification/e4d3eda/README.md) for exact
framework and runtime revisions, component hashes, and gate results.
The [verification guide](FEATURES.md) describes the commands and coverage.

| Feature app | Live evidence |
| --- | --- |
| Private checkout | Authorized binding resolves; ungranted caller is rejected |
| Static site | Content, binary assets, metadata, caching, HEAD, and rejection checks |
| Actor counter | Increment, read, and transactional rollback on the storage host |
| Scheduled maintenance | Generated CronJob's manually created Job completes |
| Durable receipts | HTTP enqueue, duplicate submission, and completed consumption |
| Schema migrations | SQLite schema and applied migration versions are verified |

## Historical evidence

The [partial candidate report for `3a64e91`](verification/3a64e91/README.md)
and the baseline below are historical results, superseded by the current run.

## Baseline `870726c` — 2026-09-09

**Not ready for release on the current wasmCloud target.** Native tests pass,
but component compilation and real Kubernetes schema validation expose blockers.
No new feature app passed both gates, so none was deployed or claimed as live-verified.

Verified on 2026-09-09 against framework commit
`870726c9a5f420e03f4564d14acc0679a4f0b758`, the merged main tree for PRs #414–#423.
The framework worktree had no source changes. Its 21 packages built successfully.
The example workspace passed TypeScript checks and **49/49 local tests**, including
seven new acceptance tests. The existing 15 deployed apps passed **64/64 live API
checks**; those components predate this main build.

Environment: Bun 1.3.14, Kubesolo 1.2.0 / Kubernetes 1.35.7+kubesolo-v1.2.0,
wasmCloud operator 2.8.0, the existing TLS-enabled 2.8.0 wash host, and
componentize-qjs 0.4.4-di.2. Package links point to the recorded main worktree,
including actors, queues, repo, and wasmCloud transitive dependencies.

### Results

| Example | Native acceptance | Component build | Kubernetes server validation | Live candidate |
| --- | --- | --- | --- | --- |
| Private checkout | Pass | Missing filesystem exports | Pass | Blocked |
| Static site | Pass | Missing filesystem exports | Pass | Blocked |
| Actor counter | Pass, including RPC and CLI | Bundler requires multiple output chunks | Unknown actor deployment fields | Blocked |
| Scheduled maintenance | Pass | Componentizer reports an undefined WASI import | Unknown component `env` | Blocked |
| Durable receipts | Pass, including retries and CLI | Pass | Unknown `queueConsumers` | Blocked |
| Schema migrations | Pass, including CLI | Bundler requires multiple output chunks | Pass | Blocked |

The aggregate command reports **13/21 gates passed** and exits nonzero. The count
includes preparation, typechecking, the local acceptance suite, and three gates
per app (build, manifest generation, server validation). A successful dry run
only validates the API schema; it does not prove runtime behavior.

### Reproduced blockers

1. **HTTP regression:** the newly exported static asset code imports filesystem
   functions that the wasmCloud compatibility module does not export. The
   unchanged `greeter` also fails to rebuild against main. Errors include
   `closeSync`, `createReadStream`, `fstatSync`, `openSync`, and `readdirSync` from
   `node-compat/fs.js`. This affects apps that merely import the HTTP package,
   not only apps mounting static files.
2. **Actor and migration bundling:** Rolldown rejects `output.file` because the
   generated graph contains multiple chunks. Its error asks for `output.dir` or
   disabled code splitting. The build stops before these SQLite-backed apps can
   be tested in QuickJS; native SQLite success is not evidence of WASI support.
3. **Scheduled component generation:** componentize-qjs stops with
   `unknown import: wasi:cli/stderr@0.2.12#get-stderr has not been defined`.
   This is the observed failure; the underlying cause still needs isolation.
4. **Actor workload schema:** the live API rejects
   `spec.template.spec.components[0].env`,
   `spec.template.spec.kubernetes.volumeMounts`,
   `spec.template.spec.kubernetes.volumes`, and `spec.template.spec.strategy`.
5. **Cron workload schema:** the live API rejects
   `spec.template.spec.components[0].env`. It accepts the CronJob object in a
   server dry run, but that does not establish successful scheduled execution.
6. **Queue workload schema:** the live API rejects
   `spec.template.spec.components[0].queueConsumers`. The worker component builds,
   but supported host wiring and a producer-to-consumer delivery test are still
   required before claiming queue integration works.

The errors come from the real CLI and API server. The generated manifests were
submitted with `kubectl apply --dry-run=server --validate=strict`; they were not
patched to hide unsupported fields. Existing cluster workloads and data were
preserved. No workflow approval or release publication was performed.

### Reproduce

See [FEATURES.md](FEATURES.md) for package linking and worktree setup, then:

```sh
bun run check
bun test
bun run verify:features
bun run smoke

# Existing app regression, independent of the new example sources:
(cd apps/greeter && ../../node_modules/.bin/di-framework wasmcloud build)
```

The verifier records `.local/verification-main/report.json`, logs for each gate,
and the exact generated manifests. [Saved evidence](verification/870726c/report.json)
contains the captured results for this revision with workstation paths normalized.
After fixing the framework, rebuild it, rerun `link:framework`, and rerun the
full verifier. The buildable apps must then deploy and pass their live probes;
the existing 64 live checks should continue to pass.
