import { createHash } from "node:crypto";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { binary, instance, run, workspace } from "./platform";

const revision = "5c4ec4a3d008b3f401d9e763515f434deebc9936";
const image = "docker.io/di-framework/wash:2.8.0-tls";
const checksums: Record<string, string> = {
  arm64: "5f2a7f451231ff35d8306f874c51606fc9da1e2db56048834a23260f68a78eef",
  amd64: "2d20037947cbb0def12b8ac0c572b212284c1832bf3c921df1e58975515d1d08",
};

async function succeeds(args: string[]) {
  return await Bun.spawn(args, { stdout: "ignore", stderr: "ignore" }).exited === 0;
}

// This setup is for the CLI's Docker-managed Kubesolo instances only.
export async function prepareTlsRuntime() {
  const container = `kubesolo-${instance}`;
  console.log(`Preparing TLS runtime for ${container}`);
  const directory = mkdtempSync(join(tmpdir(), "di-kube-tls-runtime-"));
  try {
    if (!await succeeds(["docker", "image", "inspect", image])) {
      const source = join(directory, "source");
      await run(["git", "clone", "--depth", "1", "--branch", "v2.8.0", "https://github.com/wasmCloud/wasmCloud.git", source]);
      const actual = await run(["git", "rev-parse", "HEAD"], { cwd: source, capture: true });
      if (actual !== revision) throw new Error(`Unexpected wasmCloud v2.8.0 revision: ${actual}`);
      await run(["docker", "build", "--progress=plain", "-f", resolve(workspace, "infra/tls-runtime/Dockerfile"), "-t", image, source]);
    }
    if (!await succeeds(["docker", "inspect", container])) {
      // Bootstrap once before importing the image. Existing clusters and data stay intact.
      await run([binary, "up", "--name", instance, "--http-port", process.env.DI_HTTP_PORT ?? "28080", "--allow-insecure-registries"]);
    }
    const machine = await run(["docker", "exec", container, "uname", "-m"], { capture: true });
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
    const remote = await run(["docker", "exec", container, "mktemp", "-d", "/tmp/di-tls-image.XXXXXX"], { capture: true });
    try {
      await run(["docker", "cp", join(directory, "bin/ctr"), `${container}:${remote}/ctr`]);
      const tar = join(directory, "wash.tar");
      await run(["docker", "save", "-o", tar, image]);
      await run(["docker", "cp", tar, `${container}:${remote}/wash.tar`]);
      // Kubesolo omits containerd's transfer/streaming service; use direct import.
      await run(["docker", "exec", container, `${remote}/ctr`, "--address", "/run/containerd/containerd.sock", "--namespace", "k8s.io", "images", "import", "--local", `${remote}/wash.tar`]);
    } finally {
      await run(["docker", "exec", container, "rm", "-rf", remote]);
    }
    console.log(`TLS runtime ${image} imported into ${container}`);
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
}

if (import.meta.main) await prepareTlsRuntime();
