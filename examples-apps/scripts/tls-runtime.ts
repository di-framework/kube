import { createHash } from "node:crypto";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { binary, examplesRegistry, instance, run } from "./platform";

// The platform's default tenant host image at @di-framework/platform 6.0.28; keep it equal to
// DefaultTenantHostImage in internal/platform/pulumi.go.
const image =
  "ghcr.io/di-framework/wash:2.8.0-wasi-tls@sha256:8f0ee9c5a19ae0433e83a553431f1728ddf0a4787059c10e511209263d294537";
const imported = "ghcr.io/di-framework/wash:2.8.0-wasi-tls";
const checksums: Record<string, string> = {
  arm64: "5f2a7f451231ff35d8306f874c51606fc9da1e2db56048834a23260f68a78eef",
  amd64: "2d20037947cbb0def12b8ac0c572b212284c1832bf3c921df1e58975515d1d08",
};

async function succeeds(args: string[]) {
  try {
    return await Bun.spawn(args, { stdout: "ignore", stderr: "ignore" }).exited === 0;
  } catch {
    return false;
  }
}

async function containerEngine(): Promise<string> {
  const configured = process.env.DI_CONTAINER_CLI?.trim();
  if (configured) return configured;
  if (await succeeds(["docker", "version"])) return "docker";
  if (await succeeds(["podman", "version"])) return "podman";
  throw new Error("docker or podman is required to prepare the TLS runtime");
}

// This setup is for container-managed Kubesolo instances (Docker or Podman).
export async function prepareTlsRuntime() {
  const engine = await containerEngine();
  const container = `kubesolo-${instance}`;
  console.log(`Preparing TLS runtime for ${container} with ${engine}`);
  const directory = mkdtempSync(join(tmpdir(), "di-kube-tls-runtime-"));
  try {
    if (!await succeeds([engine, "image", "inspect", imported])) {
      await run([engine, "pull", image]);
      await run([engine, "tag", image, imported]);
    }
    if (!await succeeds([engine, "inspect", container])) {
      // Bootstrap once before importing the image. Existing clusters and data stay intact.
      await run([binary, "up", "--name", instance, "--http-port", process.env.DI_HTTP_PORT ?? "28080", "--allow-insecure-registries", "--insecure-registry", examplesRegistry]);
    }
    const machine = await run([engine, "exec", container, "uname", "-m"], { capture: true });
    const arch = machine === "aarch64" ? "arm64" : machine === "x86_64" ? "amd64" : "";
    if (!checksums[arch]) throw new Error(`Unsupported Kubesolo architecture: ${machine}`);
    const archive = join(directory, "containerd.tar.gz");
    console.log("Downloading checksum-verified containerd image import tool");
    const response = await fetch(`https://github.com/containerd/containerd/releases/download/v2.2.0/containerd-static-2.2.0-linux-${arch}.tar.gz`, { signal: AbortSignal.timeout(120_000) });
    if (!response.ok) throw new Error(`Downloading ctr failed: HTTP ${response.status}`);
    const bytes = new Uint8Array(await response.arrayBuffer());
    if (createHash("sha256").update(bytes).digest("hex") !== checksums[arch]) throw new Error("containerd archive checksum mismatch");
    await Bun.write(archive, bytes);
    await run(["tar", "-xzf", archive, "-C", directory, "bin/ctr"]);
    const remote = await run([engine, "exec", container, "mktemp", "-d", "/tmp/di-tls-image.XXXXXX"], { capture: true });
    try {
      await run([engine, "cp", join(directory, "bin/ctr"), `${container}:${remote}/ctr`]);
      const tar = join(directory, "wash.tar");
      await run([engine, "save", "-o", tar, imported]);
      await run([engine, "cp", tar, `${container}:${remote}/wash.tar`]);
      // Kubesolo omits containerd's transfer/streaming service; use direct import.
      await run([engine, "exec", container, `${remote}/ctr`, "--address", "/run/containerd/containerd.sock", "--namespace", "k8s.io", "images", "import", "--local", `${remote}/wash.tar`]);
    } finally {
      await run([engine, "exec", container, "rm", "-rf", remote]);
    }
    console.log(`TLS runtime ${imported} imported into ${container}`);
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
}

if (import.meta.main) await prepareTlsRuntime();
