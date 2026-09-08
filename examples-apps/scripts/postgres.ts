import { resolve } from "node:path";
import { kubectl, run, workspace, type Platform } from "./platform";

export async function provisionPostgres(platform: Platform) {
  const exists = await run(kubectl(platform, "get", "secret", "examples-postgres-binding", "--ignore-not-found", "-o", "name"), { capture: true });
  if (!exists) {
    const password = crypto.randomUUID().replaceAll("-", "");
    const secret = {
      apiVersion: "v1", kind: "Secret", metadata: { name: "examples-postgres-binding" },
      type: "Opaque", stringData: { password, url: `postgres://examples:${password}@examples-postgres.${platform.namespace}.svc.cluster.local:5432/examples?sslmode=disable` },
    };
    // Keep credentials out of argv, generated manifests, and logs. Reuse on later deploys.
    const child = Bun.spawn(kubectl(platform, "create", "-f", "-"), { stdin: "pipe", stdout: "inherit", stderr: "inherit" });
    child.stdin.write(JSON.stringify(secret));
    child.stdin.end();
    if (await child.exited !== 0) throw new Error("Could not create PostgreSQL credentials");
  }
  await run(kubectl(platform, "apply", "-f", resolve(workspace, "infra/postgres.yaml")));
  await run(kubectl(platform, "rollout", "status", "deployment/examples-postgres", "--timeout=180s"));
  if (!exists) {
    await run(kubectl(platform, "rollout", "restart", "deployment", "-l", "wasmcloud.com/name=hostgroup"));
    await run(kubectl(platform, "rollout", "status", "deployment", "-l", "wasmcloud.com/name=hostgroup", "--timeout=180s"));
  }
}
