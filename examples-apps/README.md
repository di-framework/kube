# DI Framework apps on Kubesolo

Three TypeScript HTTP apps use DI Framework 5.2.8 and compile into WASI 0.2
components with its wasmCloud extension. Each default export is a Fetch router;
the extension supplies the WASI adapter and generates the Kubernetes resources.

The toolchain is deliberately pinned: 5.2.9's WASI 0.3 adapter produced HTTP 500s
in the live test (`expected future handle` in the QuickJS backend). Version 5.2.8
provides separate registry push/pull addresses with the WASI 0.2 adapter, without
requiring a local dependency patch.

Keep DI Framework at **5.2.8** and JCO at **1.17.9**, using the checked-in
lockfile. Upgrade only after the replacement toolchain passes `bun run smoke`
against wasmCloud 2.8.0, including JSON POST bodies; operator readiness alone
did not catch this failure. The pinned versions passed all 23 local and 23 live
API checks.

| App / HTTP Host | Requests | Demonstrates |
| --- | --- | --- |
| `greeter` | `GET /greet/Ada?lang=es` | Container-managed greeting service, route parameters, query parameters |
| `catalog` | `GET /products?q=mug`, `GET /products/mug` | Shared injectable catalog, filtering, JSON responses and 404s |
| `quotes` | `POST /quote` | Property injection with `@Component(ProductCatalog)`, body validation, totals in integer cents |

Every app also has `GET /`, `GET /health`, and a JSON 404 fallback. The catalog
contains three read-only fixtures shared at build time with the quote app. Quotes
are calculations only: they do not place orders, store data, or call the catalog
over HTTP. This keeps the examples independent of databases and external APIs.

## Deploy all three

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

The registry is for local development: it uses plain HTTP inside this cluster.
It has no public ingress; publishing is exposed only while deploying, on loopback.
The registry's blobs survive pod restarts through Kubesolo's local-path storage.
This workflow needs internet access to install dependencies and pull base images.

## Try the live APIs

All three share the default HTTP entrypoint; the Host header selects the app:

```sh
curl -H 'Host: greeter' 'http://127.0.0.1:28080/greet/Ada?lang=es'
# {"message":"Hola, Ada!","language":"es","platform":"di-framework-kube"}

curl -H 'Host: catalog' 'http://127.0.0.1:28080/products?q=mug'

curl -H 'Host: quotes' -H 'Content-Type: application/json' \
  -d '{"items":[{"sku":"mug","quantity":2},{"sku":"stickers","quantity":3}]}' \
  http://127.0.0.1:28080/quote
# currency: USD, totalCents: 4500
```

`bun run smoke` runs the same API cases as the local tests against the deployed
Wasm components, checking both status codes and response bodies.

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

# Remove only these three apps, retaining the platform and published components.
kubectl -n wasmcloud delete workloaddeployment greeter catalog quotes
kubectl -n wasmcloud delete service greeter catalog quotes

# Alternatively, permanently remove the entire managed local cluster and its data.
../bin/di-framework-kube down --name local --purge-cluster
```

Platform background: [wasmCloud's Kubernetes operator documentation](https://wasmcloud.com/docs/kubernetes-operator/).
