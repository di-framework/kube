# DI Framework apps on Kubesolo

The [main verification apps](FEATURES.md) add six prerelease examples for the new
service bindings, static assets, actors, cron, queues, and migrations. Run
`bun run verify:features` to rebuild, deploy, and verify them against the linked framework.


Fifteen TypeScript HTTP apps use locally linked DI Framework packages and target
WASI 0.3 components with its wasmCloud extension. Each default export is a Fetch
router; the extension supplies the WASI adapter and generates the Kubernetes
resources.

The workspace links core, HTTP, CLI, CLI extension, and the wasmCloud plugin to
our sibling `../../di-framework` checkout, including transitive overrides. This
exercises the unpublished TLS/HTTPS implementation from framework PR #413.
Build that checkout first, then register its packages and install this workspace:

```sh
(cd ../../di-framework && bun install)
bun run link:framework
bun run check
bun test
bun run verify:tls
```

`link:framework` registers Bun links from that exact sibling checkout and refreshes
the install. These are live directory links, not registry packages or copied
snapshots. Rebuild the framework after changing its compiled packages. Bun's link
registry is user-wide; rerun this helper if another checkout replaces the links.
The per-app release ranges are overridden by the workspace's local links.

The operator chart stays **2.8.0**. The PostgreSQL example enables its native
PostgreSQL host plugin and provisions a database in the platform namespace. `bun run smoke` must
pass against the live components, including JSON POST `/quote`. Operator
readiness alone does not prove the guest can serve a request.

| App / HTTP Host | Requests | Demonstrates |
| --- | --- | --- |
| `greeter` | `GET /greet/Ada?lang=es` | Container-managed greeting service, route parameters, query parameters |
| `catalog` | `GET /products?q=mug`, `GET /products/mug` | Shared injectable catalog, filtering, JSON responses and 404s |
| `quotes` | `POST /quote` | Property injection with `@Component(ProductCatalog)`, body validation, totals in integer cents |
| `node-runtime` | `GET /verify` | Seeded JSON files, memory filesystem writes and ENOENT, process environment, Buffer, path, AsyncLocalStorage, createRequire failure |
| `node-crypto` | `GET /verify` | SHA-256, HMAC, HKDF and AES-GCM reference vectors, tamper rejection, ECDH, WASI randomness, UUIDs and randomInt bounds |
| `node-tls` | `GET /verify` | HTTPS Agent, raw TLS, existing TCP socket handoff, wrong server-name rejection |
| `node-http` | `GET /verify` | Node HTTP chunked POST/response over WASI TCP |
| `config` | `POST /verify` | ConfigMap overrides, merged `getAll`, missing keys |
| `secrets` | `POST /verify` | Kubernetes Secret lookup/reveal, digest comparison, missing-key errors |
| `keyvalue` | `POST /verify` | Redis-backed bucket set/get/update/delete |
| `blobstore` | `POST /verify` | NATS JetStream object write/read/delete using WIT streams |
| `messaging` | `POST /verify` | NATS publish and request/reply against a separate TLS-authenticated responder |
| `outgoing-http` | `POST /verify` | Native `OutgoingHttp.send` to an in-cluster HTTP fixture |
| `postgres` | `POST /verify`, `GET /health` | DI-managed native PostgreSQL binding, Secret-backed host configuration, real SQL insert/read/update/delete |
| `node-network` | `GET /verify`, `GET /verify/ip` | TCP and UDP echo over WASI sockets, service DNS resolution |

Every app also has `GET /`, `GET /health`, and a JSON 404 fallback. The catalog
contains three read-only fixtures shared at build time with the quote app. Quotes
are calculations only: they do not place orders, store data, or call the catalog
over HTTP. The separate PostgreSQL example exercises an external service binding.

## Published 5.2.12 baseline

The published release exposed failures in HTTP initialization, async context,
timers, and DNS permissions. The [baseline report](verification-5.2.12.md) records
those results. All 40 live checks passed against the local fixes; see
[local verification](verification-local.md) for those results. The published 5.2.13 packages were verified on 2026-09-08: all seven apps
deployed on Kubesolo 1.2.0 with wasmCloud operator 2.8.0, and all 40 live API checks
passed. That run also passed typechecking and all 37 local tests.

## Deploy all examples

Requirements: a running Docker Engine, Bun 1.3+, Node.js 22+, `kubectl`, `oras`,
`git`, `tar`, and either the built `../bin/di-framework-kube` or `di-framework-kube` on PATH.
From the repository root:

```sh
make build
cd examples-apps
bun run link:framework
bun run check
bun test
bun run deploy
bun run smoke
```

Deployment first builds/imports the TLS-capable runtime described below. It
creates or updates the binary's `local` cluster, enables local HTTP
registry pulls, and installs a registry with a 2Gi persistent-volume claim in the
platform namespace. A temporary loopback port-forward on `127.0.0.1:25001` lets
ORAS publish components. wasmCloud pulls the same artifacts through the
registry's internal Kubernetes service. The port-forward closes after deployment;
the registry and apps keep running.

The deployment helper invokes `di-framework wasmcloud deploy <name> --yes` for
each discovered `apps/*/di-framework.config.json`. The checked-in
`di-framework.deploy.toml` connects the CLI to this cluster. Generated components,
WIT definitions, and workload manifests remain under each app's ignored `dist/` and
`.di-framework/` directories. The helper also sets each generated Service's
target port to `9191`, matching the binary's default wasmCloud host profile.
It then checks each app's live `/health` endpoint before reporting success.
Deployment continues after an individual app fails, then exits nonzero with the
failed app names. Smoke checks likewise report every failure and exit nonzero.
Use `bun run smoke node-crypto` to select one app.

The registry is for local development: it uses plain HTTP inside this cluster.
It has no public ingress; publishing is exposed only while deploying, on loopback.
The registry's blobs survive pod restarts through Kubesolo's local-path storage.
This workflow needs internet access to install dependencies and pull base images.

## Try the live APIs

All apps share the default HTTP entrypoint; the Host header selects the app:

```sh
curl -H 'Host: greeter' 'http://127.0.0.1:28080/greet/Ada?lang=es'
# {"message":"Hola, Ada!","language":"es","platform":"di-framework-kube"}

curl -H 'Host: catalog' 'http://127.0.0.1:28080/products?q=mug'

curl -H 'Host: quotes' -H 'Content-Type: application/json' \
  -d '{"items":[{"sku":"mug","quantity":2},{"sku":"stickers","quantity":3}]}' \
  http://127.0.0.1:28080/quote
# currency: USD, totalCents: 4500

curl -H 'Host: node-runtime' http://127.0.0.1:28080/verify
curl -H 'Host: node-crypto' http://127.0.0.1:28080/verify
curl -H 'Host: node-network' http://127.0.0.1:28080/verify
# Expected: {"tcp":"hello wasm\n","udp":"hello wasm"}
```

`bun run smoke` runs the same API cases as the local tests against the deployed
Wasm components, checking both status codes and response bodies.

## What the 5.2.12 probes verify

Local tests exercise native Bun APIs; the live smoke checks exercise the local
wasmCloud plugin's replacements inside QuickJS. Both must pass. The live runtime
check additionally requires `process.arch === "wasm32"` and `process.cwd() === "/"`.
The runtime fixture is intentionally public and is embedded at build time. Its
scratch write is memory-only in the guest; it does not demonstrate persistence.

Crypto checks compare fixed outputs with native Node 22 reference vectors. Random
checks verify shape, bounds, and distinct samples, not statistical uniformity.
The deliberately wide `randomInt` range exercises rejection sampling, and exactly
`2**48` must be rejected. The all-zero AES key/IV are test vectors only.

Deploying `node-network` or `node-http` also installs `infra/node-compat-echo.yaml`: a small Python
HTTP/TCP/UDP echo service reachable only inside the cluster. HTTP requires chunked
requests and sends chunk extensions and trailers back. The app uses a fixed service
DNS name. For `/verify/ip`, the deployment helper writes the echo Service's
cluster IP to an ignored JSON fixture that the plugin seeds at build time. That
probe separates DNS failures from transport failures without accepting arbitrary
caller destinations. Deploy before using this route; rebuilding after the Service
IP changes refreshes the fixture. The smoke client enforces a 15-second request timeout; guest probes
remain independently bounded if a guest stalls. Local network tests start ephemeral
loopback echo servers; live tests use Kubernetes DNS and actual WASI sockets.
No database, KV provider, external API, or mocked WASI implementation is involved.
Those older probes do not cover TLS/HTTPS or child processes. The new `node-tls`
example covers the outbound TLS/HTTPS subset added in PR #413.

## Edit and redeploy

Redeploy just one app after editing its source:

```sh
bun run deploy greeter
```

Build a component without deploying:

```sh
cd apps/greeter
../../node_modules/.bin/di-framework wasmcloud build
```

The CLI uses stable content-derived tags, so unchanged builds reuse their
published artifact. To add an app, create another directory under `apps/` with a
package manifest, `di-framework.config.json`, and default-exported Fetch handler.
Add API cases to `tests/api-cases.ts` and the router to `tests/apps.test.ts`.

Override the selected instance, binary, or temporary registry port as needed:

```sh
DI_KUBE_NAME=examples DI_HTTP_PORT=28081 DI_REGISTRY_PORT=25002 bun run deploy
DI_KUBE_NAME=examples bun run smoke
# DI_KUBE_BIN=/absolute/path/to/di-framework-kube is also supported.
```

The helper uses the selected instance's private kubeconfig for every application
operation. For an existing instance, use the same `DI_HTTP_PORT` that was chosen
when creating it; Docker port mappings are fixed at container creation. These
examples use the binary's default namespace, host group, and NodePort profile.

## Inspect and remove

From this directory, explicitly select the instance rather than relying on the
current kubectl context:

```sh
export KUBECONFIG="$(../bin/di-framework-kube kubeconfig --name local)"
kubectl -n wasmcloud get workloaddeployments,workloadreplicasets
kubectl -n wasmcloud logs deployment/hostgroup-default --tail=100

# Remove the apps, retaining the platform, fixtures, and published components.
kubectl -n wasmcloud delete workloaddeployment greeter catalog quotes node-runtime node-crypto node-network node-http node-tls postgres config secrets keyvalue blobstore messaging outgoing-http
kubectl -n wasmcloud delete service greeter catalog quotes node-runtime node-crypto node-network node-http node-tls postgres config secrets keyvalue blobstore messaging outgoing-http

# Remove the network probe's supporting service too.
kubectl -n wasmcloud delete -f infra/node-compat-echo.yaml

# Alternatively, permanently remove the entire managed local cluster and its data.
../bin/di-framework-kube down --name local --purge-cluster
```

Platform background: [wasmCloud's Kubernetes operator documentation](https://wasmcloud.com/docs/kubernetes-operator/).

## PostgreSQL binding

```sh
bun run deploy postgres
bun run smoke postgres
curl -H 'Host: postgres' -H 'Content-Type: application/json' -d '{}' http://127.0.0.1:28080/verify
```

`src/bindings.ts` is the CLI binding-discovery entrypoint. Its `ExampleDatabase` extends `Postgres` and declares
`@WasmCloudBinding("example-database", ...)`. The application resolves it through
DI; the guest calls `wasmcloud:postgres/query@0.2.0`, with no Node PostgreSQL driver.
The deployment script creates `examples-postgres`, a 1Gi PVC, and a ClusterIP service.
A randomly generated password and connection URL live in `examples-postgres-binding`;
subsequent deployments reuse that Secret. Credentials are not embedded in the component.
`infra/postgres-host.yaml` supplies `WASH_POSTGRES_URL` to the native host plugin.
On first provisioning the helper restarts the host so it receives the new Secret.
This also briefly restarts the other example workloads on that host.

`GET /health` executes SQL. `POST /verify` runs a transaction with a temporary table,
checks inserted and updated values inside PostgreSQL, deletes the row, and checks
that it is gone. Any failed assertion raises a database error. The table is dropped
on commit, so repeated and concurrent probes do not share rows. This covers the
binding's `queryBatch` path; streamed `query` results and prepared statements are
not yet covered. The probe also verifies that a deliberate PostgreSQL error propagates. Database-dependent checks run in `bun run smoke`, not the local
router tests.

The fixture uses plaintext PostgreSQL only inside this local cluster, with no host
port exposure. Data survives database pod restarts; deleting the cluster removes it.
Do not delete or regenerate the credentials independently of the initialized data volume.

### Binding compatibility in 5.3.0

DI Framework 5.3.0 includes the startup and PostgreSQL linking fixes from
[framework PR #399](https://github.com/di-framework/di-framework/pull/399).
Guest bindings initialize before application evaluation, and PostgreSQL host
declarations match the unlabeled imports emitted by QuickJS. Dependencies now
resolve from the registry without a Bun compatibility patch.

This example uses one database binding. It does not verify multiple named,
independently credentialed databases.

The earlier patched 5.2.13 run on 2026-09-08 used Kubesolo 1.2.0 and
runtime-operator 2.8.0: the PostgreSQL example deployed and all **43 live API checks
passed**, including the seven existing apps. Typechecking and all **37 local
tests** also passed. Those results describe the patched release before this upgrade.

## Additional service bindings

```sh
bun run deploy config secrets keyvalue blobstore messaging outgoing-http
bun run smoke config secrets keyvalue blobstore messaging outgoing-http
```

Each app declares its DI binding in `src/bindings.ts` and resolves the service at
module startup. Both `/health` and `POST /verify` exercise the actual host capability;
the latter returns the verification results. These checks are live-only: local router tests do not
substitute fake providers for them. Requests use JSON, including an empty `{}` body.

The helper provisions these fixtures in the selected platform namespace:

- `binding-config` supplies configuration that overrides an inline value.
- `binding-secrets` holds a generated token, reused across deployments. The app
  reveals it internally and returns only its SHA-256 digest. Smoke compares that
  digest against `binding-secret-check`; no secret value is printed or embedded.
- `binding-redis` provides the key-value backend. Keys use a dedicated prefix and
  unique probe IDs and are removed after checks.
- `binding-nats` is a separate JetStream server for object storage. It uses
  ephemeral storage; these fixtures verify service bindings, not backup or durability.
- `binding-echo` serves HTTP and consumes `bindings.publish` / `bindings.echo` on
  the platform's NATS broker. It uses the existing `wasmcloud-data-tls` certificate
  for mutual TLS. A request succeeds only when the responder observed the preceding
  publication with the same unique token.

For `outgoing-http`, the deployment helper sets the component's
`localResources.allowedHosts` to `["http://binding-echo:8080"]`. This is an explicit,
endpoint-specific egress grant; an absent grant returns `HTTP-request-denied`.
The framework's current project config does not expose this field, so the helper
applies it to the generated workload before checking its live health.

Redis and the separate object-store NATS server are local fixtures with ClusterIP
services and no host port exposure. The platform broker keeps its existing mutual
TLS configuration. The fixtures assume the binary's default Helm release and service
names. They do not require an external account.

DI Framework 5.3.0 also includes the remaining binding fixes from
[framework PR #400](https://github.com/di-framework/di-framework/pull/400):
unnamed key-value, blobstore, messaging, and secrets host interfaces;
configuration overlays on unnamed bindings; a single HTTP ingress/client host
declaration; and omission of the internally linked HTTP `client` and key-value
`types` interfaces from host discovery while preserving their component imports.
Regression checks in `tests/binding-manifests.test.ts` exercise the installed plugin.

Each example uses one binding of its kind, so multiple labeled backends remain
outside this verification.
See the [runtime's host-interface configuration reference](https://wasmcloud.com/docs/kubernetes-operator/host-interface-configuration/)
for backend-selection and credential settings.

Verified with patched 5.2.13 on 2026-09-08: all six additional binding apps deployed on Kubesolo 1.2.0
with wasmCloud 2.8.0, and **61/61 live checks passed across all 14 apps**.
Typechecking and **40 local tests** passed. A clean frozen-lockfile install also
reapplied the compatibility patch and passed the three manifest regression tests.
These historical results use the local compatibility patch. Both upstream PRs are
merged for 5.3.0, and the workspace no longer applies that patch.

## TLS and HTTPS (local PR packages)

**Kubernetes verification is required.** The example is verified only when the
compiled component is deployed to the local Kubernetes cluster and the live smoke
checks pass through its HTTP ingress, including `GET /verify`. A successful build,
local Bun tests, or Wasmtime probes alone do not meet this requirement.

```sh
bun run link:framework
bun run check
bun test
bun run verify:tls
```

`verify:tls` runs deployment followed by the live Kubernetes smoke checks and
exits nonzero if either fails.

### Verified on Kubernetes — 2026-09-09

`bun run link:framework` and `bun run verify:tls` passed. All **3/3 live checks**
ran through `http://127.0.0.1:28080` with `Host: node-tls`: `/verify` returned 200,
`/health` returned 200, and `/missing` returned the expected JSON 404.

```sh
curl --fail-with-body --max-time 15 -sS -H 'Host: node-tls' \
  http://127.0.0.1:28080/verify
# {"https":true,"tls":true,"upgrade":true,"wrongNameRejected":true}
```

Verified versions:

| Part | Version / revision |
| --- | --- |
| DI Framework local PR #413 head | `53e241cb63c5f3bcc5d81a773148c1ee27a20fc4` |
| componentize-qjs | `0.4.4-di.2` |
| Bun | `1.3.14` |
| Kubesolo / Kubernetes | `1.2.0` / `1.35.7+kubesolo-v1.2.0` |
| wasmCloud operator chart | `2.8.0` |
| wash host | `2.8.0`, default features plus `wasi-tls` |
| Wasmtime in the host | `47.0.3` (upstream Cargo.lock) |
| Host platform | Linux arm64 |

The built runtime image's observed index digest is
`sha256:00f802dd795876cec409574df6d6a4df900c5b10c5b63ef898ef7b5f1be0fd00`.
The unchanged component artifact tag is
`sha256-0017aedc83c23256b3e3421020d519260bf9befdb12d3ee6ccaf071097d04463`;
replacing the host build resolved the linking failure. Rebuilds may have different
image digests because build metadata is not normalized.

A subsequent `bun run deploy greeter` retained the TLS host image and
`pull_policy: Never`. Then `bun run smoke` passed **64/64 live API checks across
all 15 apps**, including the TLS checks again. PostgreSQL and registry PVCs
retained their original bound volumes; existing credentials and cluster data
were preserved.

Local regression commands also passed:

```sh
bun install --frozen-lockfile
bun run check
bun test  # 42 passed
(cd ../../di-framework && bun test --timeout 30000 \
  packages/di-framework-cli-plugin-wasmcloud/tests/node-compat-tls.test.ts \
  packages/di-framework-cli-plugin-wasmcloud/tests/wash-dev.test.ts)
# 17 passed; the longer runner timeout accommodates component bundling.
```

The live probe deadlines and all TLS assertions were preserved. No Bun or
Wasmtime run was used as a substitute for the Kubernetes acceptance checks.

### Runtime integration

The stock `ghcr.io/wasmcloud/wash:2.8.0` image fails to link this component:

```text
component imports instance `wasi:tls/types@0.3.0-draft`, but a matching implementation was not found in the linker
instance export `error` has the wrong type: resource implementation is missing
```

The release image uses Cargo's default features, which omit `wasi-tls`.
The existing runtime registers the TLS resources only when that feature is
compiled in. This is a host build configuration issue; changing the guest WIT
or bypassing certificate verification is unnecessary.

[`infra/tls-runtime/Dockerfile`](infra/tls-runtime/Dockerfile) builds upstream
wasmCloud **2.8.0**, commit `5c4ec4a3d008b3f401d9e763515f434deebc9936`, with
`cargo build --locked --release --bin wash --features wasi-tls`. It retains the
default host features, uses the upstream lockfile (Wasmtime **47.0.3**), and pins
both base images by digest. Source:
[release build configuration](https://github.com/wasmCloud/wasmCloud/blob/v2.8.0/.github/workflows/wash.yml),
[TLS linker registration](https://github.com/wasmCloud/wasmCloud/blob/v2.8.0/crates/wash-runtime/src/engine/mod.rs).

Every `deploy` invocation runs `scripts/tls-runtime.ts` before upgrading Helm.
The helper builds `docker.io/di-framework/wash:2.8.0-tls` if absent, verifies the
source revision, and imports the image into the selected `kubesolo-$DI_KUBE_NAME`
container's `k8s.io` containerd namespace. It downloads a checksum-verified static
`ctr` from containerd **2.2.0**, uses `images import --local` because Kubesolo
omits the streaming service, and removes its temporary files after import.
A first-time instance is bootstrapped before import; existing cluster volumes
and credentials are reused. The first Rust build needs several GB of free Docker
storage and substantially longer than an application build (25 minutes for the
verified release compilation on a 16-CPU Docker VM). This helper supports
Docker-managed Kubesolo on arm64 and amd64; the live result below is arm64.

[`infra/tls-runtime/values.yaml`](infra/tls-runtime/values.yaml) selects the imported
image with `pull_policy: Never`. All app deployments pass these values alongside
the existing PostgreSQL configuration, so deploying another app cannot reset the
host to the stock image. The operator chart remains **2.8.0**. The host rollout
briefly restarts its workloads. To rebuild after editing the runtime recipe,
rebuild the local image explicitly using the Dockerfile and pinned source before
redeploying; a cached image is otherwise reused.

```sh
# Optional: build/import the runtime ahead of deployment.
bun run runtime:tls
# Required acceptance check: deployment followed by all three ingress checks.
bun run verify:tls
# Regression check across all deployed examples.
bun run smoke
```

The host must allow DNS/outbound TCP to the fixed `example.com:443` destination.
The app grants DNS lookup only for `example.com`; certificate verification and
server-name authentication remain enabled. Incoming TLS termination remains an
ingress concern.

### Live assertions

`GET /verify` checks these operations concurrently inside the deployed component:

- `node:https.get` with an HTTPS Agent returns the expected public example page.
- `node:tls.connect` authenticates the peer and exchanges an HTTP request over TLS.
- An existing `node:net` socket is handed to `tls.connect`, exercising the socket
  upgrade mechanism used by STARTTLS (without SMTP/IMAP protocol negotiation).
- A wrong TLS server name fails with an authentication/handshake error. A timeout
  or DNS failure does not count as a successful rejection.

Each probe has a 10-second deadline and closes its socket. Destinations are fixed
in source; callers cannot choose arbitrary hosts. `/health` checks router liveness;
`/verify` checks connectivity and TLS behavior. The wrong-name endpoint may reject
SNI before presenting its certificate, so this is not an isolated hostname verifier test.

### Diagnostic runner

`bun run smoke:tls` is an optional Wasmtime diagnostic. It is not used by
`verify:tls` and does not count as Kubernetes verification. It builds the same
router through the linked CLI, checks TLS WIT discovery, adds a callable test
export, and runs `/verify`, `/health`, and `/missing` with real network traffic.
It requires Wasmtime 48 with P3/TLS support and access to `example.com:443`.

Wasmtime 48 `serve` currently fails to link the TLS error resource even with TLS
enabled. `smoke:tls` uses its working `run` linker to exercise the compiled router.
This does not verify HTTP ingress in Wasmtime or a Kubernetes deployment.
The required Kubernetes checks are the deployment and smoke commands above.
The diagnostic runner does not change the cluster's host image.
