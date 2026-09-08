import { createHash } from "node:crypto";
import { resolve } from "node:path";
import { kubectl, run, workspace, type Platform } from "./platform";

async function apply(platform: Platform, resource: unknown, create = false) {
  const child = Bun.spawn(kubectl(platform, create ? "create" : "apply", "-f", "-"), { stdin: "pipe", stdout: "inherit", stderr: "inherit" });
  child.stdin.write(JSON.stringify(resource));
  child.stdin.end();
  if (await child.exited !== 0) throw new Error("Could not provision binding fixture");
}
export async function provisionBindings(platform: Platform, selected: string[]) {
  const config = (name: string, data: Record<string, string>) => apply(platform, { apiVersion: "v1", kind: "ConfigMap", metadata: { name }, data });
  if (selected.includes("config")) await config("binding-config", { message: "from-kubernetes-configmap" });
  if (selected.includes("secrets")) {
    const existing = await run(kubectl(platform, "get", "secret", "binding-secrets", "--ignore-not-found", "-o", "json"), { capture: true });
    const token = existing ? Buffer.from(JSON.parse(existing).data["probe-token"], "base64").toString() : crypto.randomUUID().replaceAll("-", "");
    if (!existing) await apply(platform, { apiVersion: "v1", kind: "Secret", metadata: { name: "binding-secrets" }, stringData: { "probe-token": token } }, true);
    await config("binding-secret-check", { digest: createHash("sha256").update(token).digest("hex") });
  }
  if (selected.includes("keyvalue")) {
    await run(kubectl(platform, "apply", "-f", resolve(workspace, "infra/binding-redis.yaml")));
    await run(kubectl(platform, "rollout", "status", "deployment/binding-redis", "--timeout=180s"));
    await config("binding-keyvalue", { backend: "redis", url: `redis://binding-redis.${platform.namespace}.svc.cluster.local:6379`, prefix: "binding-probes:" });
  }
  if (selected.includes("blobstore")) {
    await run(kubectl(platform, "apply", "-f", resolve(workspace, "infra/binding-nats.yaml")));
    await run(kubectl(platform, "rollout", "status", "deployment/binding-nats", "--timeout=180s"));
    await config("binding-blobstore", { backend: "nats", url: `nats://binding-nats.${platform.namespace}.svc.cluster.local:4222` });
  }
  if (selected.includes("messaging") || selected.includes("outgoing-http")) {
    const applied = await run(kubectl(platform, "apply", "-f", resolve(workspace, "infra/binding-echo.yaml")), { capture: true });
    console.log(applied);
    if (applied.includes("configmap/binding-echo configured")) {
      await run(kubectl(platform, "rollout", "restart", "deployment/binding-echo"));
    }
    await run(kubectl(platform, "rollout", "status", "deployment/binding-echo", "--timeout=180s"));
  }
}
