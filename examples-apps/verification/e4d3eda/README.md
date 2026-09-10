# Current main verification

Verified on 2026-09-10 against clean framework main `e4d3eda8d282670bfbff79ef54447ca8c585176e`.
The framework's 21 packages were rebuilt before linking this example workspace.
The verifier and rebuilt kube binary use branch code at `d54091a`.

**44/44 verification gates passed**, including:

- TypeScript checks and all 51 local example tests.
- Component builds, strict Kubernetes server validation, deployment image checks,
  and live probes for all six feature apps.
- Actor increment/read/rollback, generated cron Job completion, queue enqueue
  and consumption, SQLite schema migration, private bindings, and static assets.
- Rebuilding and deploying all 21 example apps, then passing all 64 live API
  regression checks, including Node TLS.

`report.json` records the framework SHA, runtime images, feature artifact hashes,
deployed image references, timestamp, and every gate. Its log paths refer to the
adjacent text files. `verification-run.txt` includes the live probe results.
`framework-build.txt` records the successful 21-package build.
The verifier rebuilds feature apps again during the final smoke deployment.
`report.json` identifies the builds used by the six feature probes;
`smoke-artifacts.json` separately records all 21 final builds, with artifact
hashes checked against build metadata and image references checked against the
live WorkloadDeployments. Successive builds produced different component hashes.
Generated YAML files are the manifests submitted for strict server validation.
ANSI formatting, trailing whitespace, and workstation paths are normalized.

The repo's `go test ./...`, `go vet ./...`, and static binary build also passed.
Docker Desktop was initially stopped; this saved full run occurred after the
existing Kubesolo instance recovered. Historical failed runs remain separate.

These live probes establish the behaviors listed above. Native tests separately
cover retries, dead letters, deduplication, and restart persistence; the live
suite does not establish all of those lifecycle behaviors.
