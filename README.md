# di-framework-kube

`di-framework-kube` is a single distributable CLI that creates an isolated
[Kubesolo](https://github.com/portainer/kubesolo) cluster and installs the
[wasmCloud runtime operator](https://wasmcloud.com/docs/kubernetes-operator/).
It uses the same shared TypeScript/Pulumi platform package as the DI Framework
wasmCloud CLI extension, with Kubesolo managing the cluster lifecycle.

Node.js, npm, and Pulumi are required for provisioning. The binary retains a Helm
client for status and legacy cleanup, so `helm` is not required. It downloads
the pinned official `kubesoloctl` executable on first use, verifies its SHA-256
digest, and caches it. Kubesolo and wasmCloud container images are still pulled
from their upstream registries.

Pinned platform versions:

- Kubesolo `v1.2.0` (Kubernetes `v1.35.7+kubesolo-v1.2.0`)
- wasmCloud runtime operator `2.8.0`

## Quick start

Container mode supports macOS and Linux on amd64 and arm64. It requires a
running Docker Engine. The first invocation downloads images and can take a few
minutes.

```sh
di-framework-kube up
di-framework-kube status
di-framework-kube outputs
```

The default instance is named `local`. Its Kubernetes API and credentials are
kept in a dedicated kubeconfig rather than read from the current `kubectl`
context. Workload HTTP is published only on `127.0.0.1:28080` and reaches the
default wasmCloud host group through NodePort `30080`.

`outputs` prints the connection contract for other tooling:

```json
{
  "kubeconfig": "/path/to/di-framework-kube/local/kubeconfig",
  "namespace": "wasmcloud",
  "endpoints": {
    "http": "http://127.0.0.1:28080",
    "kubernetes": "https://127.0.0.1:54321"
  }
}
```

Deploy a workload with the kubeconfig returned by `di-framework-kube
kubeconfig`. HTTP workloads are routed by their configured `Host` value:

```sh
export KUBECONFIG="$(di-framework-kube kubeconfig)"
kubectl -n wasmcloud apply -f workload.yaml
curl -H 'Host: my-application' http://127.0.0.1:28080/
```

Re-running `up` upgrades the existing Helm release in place. Remove wasmCloud
while preserving Kubesolo:

```sh
di-framework-kube down
```

Permanently remove the managed cluster and its data volume:

```sh
di-framework-kube down --purge-cluster
```

`--purge-cluster` is destructive. It is refused for clusters supplied with
`--kubeconfig`.

## Example applications

Six additional [main verification apps](examples-apps/FEATURES.md) exercise the
service bindings, static assets, actors, cron, queues, and migrations against
local Bun and the real Kubernetes API.


The [examples-apps workspace](examples-apps/README.md) contains a greeter, product
catalog, quote API, seven service-binding examples, and five Node compatibility probes for DI Framework 5.3.0 with the local TLS changes from PR #413.
Install dependencies and deploy all examples to this platform:

```sh
cd examples-apps
bun run link:framework
bun run deploy
bun run smoke
```

The workspace includes a local component registry, deployment configuration, and
API checks that run both locally and against the deployed Wasm components.

The examples link core, HTTP, the CLI, CLI extension, and wasmCloud plugin to
the sibling `../di-framework` checkout. Build that checkout first. Deployment
builds and imports a pinned wasmCloud 2.8.0 host with its opt-in `wasi-tls`
feature; see the example README for requirements and verification results.
The probes exercise runtime APIs, crypto reference vectors, and HTTP/TCP/UDP over
WASI sockets, using a cluster-local echo service and an explicit DNS allowlist.
See the [published 5.2.12 baseline](examples-apps/verification-5.2.12.md) and
[local verification report](examples-apps/verification-local.md).
The wasmCloud chart stays `2.8.0`; the PostgreSQL example provisions its database and configures the native host plugin.

## Existing clusters and Linux service mode

Install onto an explicitly selected existing cluster:

```sh
di-framework-kube up \
  --name edge \
  --kubeconfig /secure/path/admin.kubeconfig \
  --context edge-admin
```

Run Kubesolo as a native Linux service instead of a container:

```sh
sudo di-framework-kube up --name edge --run-mode service
```

Kubesolo host mode has privileged OS requirements and conflicts with an active
host container runtime. Review the upstream Kubesolo installation guide before
using service mode.

## Configuration

Useful `up` flags include:

| Flag | Default | Purpose |
| --- | --- | --- |
| `--name` | `local` | Isolated instance and state name |
| `--run-mode` | `container` | Kubesolo `container` or `service` mode |
| `--http-port` | `28080` | Loopback workload port in container mode |
| `--node-port` | `30080` | wasmCloud host NodePort; `0` uses ClusterIP |
| `--namespace` | `wasmcloud` | Platform namespace |
| `--chart-version` | `2.8.0` | Pinned runtime-operator chart |
| `--values`, `-f` | none | Additional Helm values file; repeatable |
| `--allow-insecure-registries` | false | Permit plain-HTTP component registries |

Values files are merged after the Kubesolo profile, so they can override host
resources, plugins, NATS settings, or any other runtime-operator chart value.
The built-in profile disables the deprecated runtime gateway, creates one
HTTP-enabled host, and applies the memory sizing used by DI Framework's
JavaScript components.

The insecure-registry switch changes all wasmCloud host registry connections;
use it only for a trusted local development registry.

## Build and release

Go 1.26 or newer is required to build from source.

```sh
make test
make build
./bin/di-framework-kube version
```

Tagged releases are built as static Linux and macOS binaries for amd64 and
arm64. `kubectl` was used by the acceptance test and examples, but neither
`kubectl` nor `helm` is required for `up`, `status`, `outputs`, or `down`.

Air-gapped operation is not implemented yet: the CLI binary is self-contained,
but its pinned Kubesolo manager, Kubesolo image, wasmCloud chart, and workload
images currently come from upstream registries.

## License

Licensed under either the MIT License or Apache License 2.0, at your option.

## Shared Pulumi platform

`up` now provisions the same `@di-framework/platform` TypeScript package used by
`di-framework wasmcloud platform init`. Kubesolo creation remains in this CLI;
wasmCloud, Tenant/User CRDs, the tenancy controller, admission policies, and HTTP
routing are defined only in the shared package. `allowSharedHosts` stays false,
including when an administrator supplies Helm overrides.

Install Node.js, npm, and the Pulumi CLI before running `up`. The CLI installs
`@di-framework/platform@5.3.3` directly from npm by default:

```sh
di-framework-kube up
```

To explicitly select a published package version:

```sh
di-framework-kube up --platform-package @di-framework/platform@5.3.3
```

No local package build or tarball is needed for this workflow. Use an exact
published version with `--platform-package` when upgrading the shared platform.

Use `--platform-config /absolute/path/platform.json` for tenant declarations:

```json
{
  "tenants": [{ "name": "alpha" }],
  "users": [{ "name": "alice", "memberships": [{ "tenant": "alpha", "role": "developer" }] }]
}
```

Updates without this flag preserve existing tenant/user declarations. Supplying
it replaces those declarations. The file also accepts `tenantHostImage` and
`tenantHostImagePullPolicy`, `storageRoot`, and `networkPolicyEngine` (`existing` or `kube-router`).
Managed Kubesolo defaults to the shared package's policy-only kube-router
controller; external clusters default to their existing policy engine. `--values` still accepts administrator Helm values;
shared-host and watched-namespace security settings cannot be overridden.

Each instance stores its Pulumi project under `<state-dir>/<name>/platform`, with
stack `dev`, a local file backend, and a mode-0600 `.passphrase` file. Back up this
whole directory. Do not create another stack for the same cluster. A cluster-level
ownership claim rejects competing installations and is released only after a
successful `down`. `down --purge-cluster` destroys the platform before deleting
Kubesolo and its data. Failed updates keep their project and connection state so
`up` can retry and `down` can clean up.

To inspect or operate the same project directly, change into that directory and
set `PULUMI_CONFIG_PASSPHRASE` from `.passphrase` without printing it, then use
`pulumi preview --stack dev` or `pulumi up --stack dev`. The project records its
backend URL. Do not run direct Pulumi commands and this CLI concurrently. Use the existing
cluster deployment target in the framework CLI, with this instance's kubeconfig
and your application registry. Cluster creation/deletion remains owned by this CLI.

### Existing installations

An existing Helm-only installation is not silently imported or replaced. `up`
refuses it and leaves it intact. Existing state still supports Helm `status` and
`down`. Back up workload/backend data and arrange application downtime before
explicitly removing the legacy release with `down`; a subsequent `up` creates
the shared platform. Automated resource/data import is not provided. Retained
tenant data is not a backup, and purging the cluster removes it.

The shared tenant storage profile currently uses single-node host paths under
`/var/lib/kubesolo`; it is intended for local Kubesolo clusters. External clusters
must provide suitable storage/network-policy enforcement before using tenancy.
The policy-only kube-router profile preserves Kubesolo's CNI and service proxy;
it requires node privileges and uses the pinned v2.10.0 image. See the upstream
[selective functionality documentation](https://www.kube-router.io/docs/user-guide/).

### Developing the shared package locally

Use a tarball only when testing unpublished changes to `@di-framework/platform`:

```sh
# In di-framework/packages/di-framework-platform:
bun run build
npm pack --pack-destination /tmp

# In di-framework-kube:
go run ./cmd/di-framework-kube up --name shared-test \
  --http-port 28089 \
  --platform-package file:/tmp/di-framework-platform-5.3.3.tgz
```

Use the filename produced by `npm pack` if the package version differs.
