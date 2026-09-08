# DI Framework apps on Kubesolo

Seven TypeScript HTTP apps use DI Framework 5.2.13 and target
WASI 0.3 components with its wasmCloud extension. Each default export is a Fetch
router; the extension supplies the WASI adapter and generates the Kubernetes
resources.

The workspace pins DI Framework core, HTTP, CLI, CLI extension, and wasmCloud
plugin to `5.2.13`, including transitive dependency overrides. The lockfile resolves
these packages from the registry.

The operator chart stays **2.8.0**. Keep these examples HTTP-only: do not add
postgres or KV bindings without matching host providers. `bun run smoke` must
pass against the live components, including JSON POST `/quote`. Operator
readiness alone does not prove the guest can serve a request.

| App / HTTP Host | Requests | Demonstrates |
| --- | --- | --- |
| `greeter` | `GET /greet/Ada?lang=es` | Container-managed greeting service, route parameters, query parameters |
| `catalog` | `GET /products?q=mug`, `GET /products/mug` | Shared injectable catalog, filtering, JSON responses and 404s |
| `quotes` | `POST /quote` | Property injection with `@Component(ProductCatalog)`, body validation, totals in integer cents |
| `node-runtime` | `GET /verify` | Seeded JSON files, memory filesystem writes and ENOENT, process environment, Buffer, path, AsyncLocalStorage, createRequire failure |
| `node-crypto` | `GET /verify` | SHA-256, HMAC, HKDF and AES-GCM reference vectors, tamper rejection, ECDH, WASI randomness, UUIDs and randomInt bounds |
| `node-http` | `GET /verify` | Node HTTP chunked POST/response over WASI TCP |
| `node-network` | `GET /verify`, `GET /verify/ip` | TCP and UDP echo over WASI sockets, service DNS resolution |

Every app also has `GET /`, `GET /health`, and a JSON 404 fallback. The catalog
contains three read-only fixtures shared at build time with the quote app. Quotes
are calculations only: they do not place orders, store data, or call the catalog
over HTTP. This keeps the examples independent of databases and external APIs.

## Published 5.2.12 baseline

The published release exposed failures in HTTP initialization, async context,
timers, and DNS permissions. The [baseline report](verification-5.2.12.md) records
those results. All 40 live checks passed against the local fixes; see
[local verification](verification-local.md) for those results. The published 5.2.13 packages were verified on 2026-09-08: all seven apps
deployed on Kubesolo 1.2.0 with wasmCloud operator 2.8.0, and all 40 live API checks
passed. The workspace also passes typechecking and all 37 local tests.

## Deploy all seven

Requirements: a running Docker Engine, Bun 1.3+, Node.js 22+, `kubectl`, `oras`,
and either the built `../bin/di-framework-kube` or `di-framework-kube` on PATH.
From the repository root:

```sh
make build
cd examples-apps
bun install --frozen-lockfile
bun run check
bun test
bun run deploy
bun run smoke
```

Deployment creates or updates the binary's `local` cluster, enables local HTTP
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

All seven share the default HTTP entrypoint; the Host header selects the app:

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
These probes do not cover TLS/HTTPS or child processes, which remain mocks in
5.2.12, or claim to cover every Node API.

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

# Remove only these seven apps, retaining the platform and published components.
kubectl -n wasmcloud delete workloaddeployment greeter catalog quotes node-runtime node-crypto node-network node-http
kubectl -n wasmcloud delete service greeter catalog quotes node-runtime node-crypto node-network node-http

# Remove the network probe's supporting service too.
kubectl -n wasmcloud delete -f infra/node-compat-echo.yaml

# Alternatively, permanently remove the entire managed local cluster and its data.
../bin/di-framework-kube down --name local --purge-cluster
```

Platform background: [wasmCloud's Kubernetes operator documentation](https://wasmcloud.com/docs/kubernetes-operator/).
