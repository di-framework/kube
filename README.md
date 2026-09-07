# di-framework-kube

`di-framework-kube` is a single distributable CLI that creates an isolated
[Kubesolo](https://github.com/portainer/kubesolo) cluster and installs the
[wasmCloud runtime operator](https://wasmcloud.com/docs/kubernetes-operator/).
It is intended to replace the Docker + k0s + Pulumi platform bootstrap used by
the DI Framework wasmCloud prototype.

The binary contains the Helm client, so `helm` is not required. It downloads
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

The [examples-apps workspace](examples-apps/README.md) contains a greeter, product
catalog, and quote API built with DI Framework. Deploy all three to this platform:

```sh
cd examples-apps
bun install --frozen-lockfile
bun run deploy
bun run smoke
```

The workspace includes a local component registry, deployment configuration, and
API checks that run both locally and against the deployed Wasm components.

The examples require the pinned **DI Framework 5.2.8** toolchain and JCO
`1.17.9` (WASI 0.2). In live testing against wasmCloud `2.8.0`, DI Framework
`5.2.9`'s WASI 0.3 adapter returned HTTP 500 even though the operator reported
the workloads ready. The QuickJS backend logged `expected future handle` when
the adapter passed a JavaScript Promise instead of a WASI future handle.
Keep the checked-in versions and lockfile until an updated toolchain passes
`bun run smoke`, including the JSON POST checks. Version 5.2.8 passed all 23
local and 23 live API checks without dependency patches.

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
