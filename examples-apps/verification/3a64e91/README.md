# Partial 5.3.1 candidate verification

Framework revision: `3a64e917e960745b32ddd317449d9550bf28e606`.

**Not ready for release.** The aggregate verifier reports 27/43 passing gates
and exits nonzero. All 51 local example tests pass. Private checkout and static
site pass component builds, strict server validation, deployment, and live
probes. The static probes include binary/text assets, MIME, cache headers,
ETags, HEAD, missing files, and traversal. The unchanged greeter rebuilds and
passed its five live smoke probes.

Cron and queue components compile, but their deployment manifests fail strict
server validation. Actors and migrations still import native SQLite. There is
no live acceptance evidence for those four applications. The required rebuilt
64-check smoke gate is missing and explicitly fails. The TLS-enabled wasmCloud
2.8.0 host image is preserved.

`report.json` records runtime images, rebuilt component SHA-256 digests,
deployed image references and each gate. A successful API dry run is not
runtime acceptance. Workstation paths are normalized in this saved copy.

The framework still needs the SQLite/WAC component and portable database
adapters, long-lived stateful services, storage ownership and supported
manifests, authenticated control APIs and the scheduled invoker, and the
remaining lifecycle acceptance scenarios. No merge or release approval follows
from this evidence.
