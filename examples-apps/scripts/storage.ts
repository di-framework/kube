import { existsSync, readdirSync, readFileSync } from "node:fs";
import { resolve } from "node:path";
import type { Platform } from "./platform";
import { kubectl, run, workspace } from "./platform";

const STORAGE_ROOT = "/var/lib/di-framework/storage";
const STORAGE_APPS = new Set(["actor-counter", "durable-receipts", "schema-migrations"]);

function asWitIdentifier(name: string): string {
  return name
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

/** Apps that schedule onto the storage hostgroup with a per-app hostPath. */
function listStorageApps(): string[] {
  const names: string[] = [];
  for (const directory of readdirSync(resolve(workspace, "apps"))) {
    const configPath = resolve(workspace, "apps", directory, "di-framework.config.json");
    if (!existsSync(configPath)) continue;
    const config = JSON.parse(readFileSync(configPath, "utf8")) as {
      name?: string;
      persistentStorage?: boolean;
    };
    if (!config.name) continue;
    if (config.persistentStorage === true || STORAGE_APPS.has(config.name)) {
      names.push(asWitIdentifier(config.name));
    }
  }
  return names;
}

/** Ensure the shared application-storage PVC and storage hostgroup exist. */
export async function provisionStorageHost(platform: Platform): Promise<void> {
  await run(kubectl(platform, "apply", "-f", resolve(workspace, "infra/storage-host.yaml")));
  const patch = {
    spec: {
      strategy: { type: "Recreate", rollingUpdate: null },
      replicas: 1,
    },
  };
  for (let attempt = 0; attempt < 30; attempt++) {
    const result = Bun.spawnSync(
      kubectl(platform, "get", "deployment", "hostgroup-storage", "-o", "name"),
      { stdout: "pipe", stderr: "pipe" },
    );
    if (result.exitCode === 0) break;
    await Bun.sleep(2_000);
  }
  await run(
    kubectl(
      platform,
      "patch",
      "deployment",
      "hostgroup-storage",
      "--type=merge",
      "-p",
      JSON.stringify(patch),
    ),
  ).catch(() => undefined);
  await run(
    kubectl(platform, "rollout", "status", "deployment/hostgroup-storage", "--timeout=180s"),
  ).catch(() => undefined);

  // hostPath volumes require the directories to exist on the PVC mount.
  const apps = listStorageApps();
  if (apps.length === 0) return;
  const mkdir = apps
    .map((name) => `mkdir -p '${STORAGE_ROOT}/${name}' && chmod 777 '${STORAGE_ROOT}/${name}'`)
    .join(" && ");
  // Ensure the shared root is writable by the non-root wash host process.
  const script = `mkdir -p '${STORAGE_ROOT}' && chmod 777 '${STORAGE_ROOT}' && ${mkdir}`;
  await run([
    ...kubectl(platform, "delete", "pod", "di-framework-storage-init", "--ignore-not-found"),
  ]).catch(() => undefined);
  await run(
    kubectl(
      platform,
      "run",
      "di-framework-storage-init",
      "--rm",
      "--restart=Never",
      "--attach",
      "--overrides",
      JSON.stringify({
        apiVersion: "v1",
        kind: "Pod",
        metadata: { name: "di-framework-storage-init" },
        spec: {
          containers: [
            {
              name: "init",
              image: "busybox:1.36",
              command: ["sh", "-ec", script],
              volumeMounts: [{ name: "storage", mountPath: STORAGE_ROOT }],
            },
          ],
          volumes: [
            {
              name: "storage",
              persistentVolumeClaim: { claimName: "di-framework-storage" },
            },
          ],
          restartPolicy: "Never",
        },
      }),
      "--image=busybox:1.36",
      "--command",
      "--",
      "true",
    ),
  ).catch(() => undefined);
}
