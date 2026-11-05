import { existsSync } from "node:fs";
import { resolve } from "node:path";

export const workspace = resolve(import.meta.dir, "..");
const localBinary = resolve(workspace, "../bin/di-framework-kube");
export const binary = process.env.DI_KUBE_BIN ?? (existsSync(localBinary) ? localBinary : "di-framework-kube");
export const instance = process.env.DI_KUBE_NAME ?? "local";
// `di-framework-kube up` installs into this namespace by default; the helpers never override it.
export const namespace = "wasmcloud";
// The in-cluster examples registry as workload references spell it (di-framework.deploy.toml,
// `pull`). Tenant hosts may pull over plain HTTP only from exact `insecureRegistries` entries,
// so every `up` here passes it with --insecure-registry (kube#8).
export const examplesRegistry = `examples-registry.${namespace}.svc.cluster.local:5000`;

export async function run(args: string[], options: { cwd?: string; env?: NodeJS.ProcessEnv; capture?: boolean } = {}) {
  const child = Bun.spawn(args, {
    cwd: options.cwd ?? workspace,
    env: options.env ?? process.env,
    stdout: options.capture ? "pipe" : "inherit",
    stderr: "inherit",
  });
  const output = options.capture ? await new Response(child.stdout as ReadableStream).text() : "";
  const code = await child.exited;
  if (code !== 0) throw new Error(`${args[0]} ${args[1] ?? ""} failed (exit ${code})`);
  return output.trim();
}

export interface Platform {
  kubeconfig: string;
  namespace: string;
  endpoints: { http: string };
}

export async function outputs(): Promise<Platform> {
  return JSON.parse(await run([binary, "outputs", "--name", instance], { capture: true }));
}

export function kubectl(platform: Platform, ...args: string[]) {
  return ["kubectl", "--kubeconfig", platform.kubeconfig, "--namespace", platform.namespace, ...args];
}
