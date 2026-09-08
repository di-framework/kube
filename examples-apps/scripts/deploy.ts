import { readdirSync } from "node:fs";
import { resolve } from "node:path";
import { binary, instance, kubectl, outputs, run, workspace } from "./platform";

// The app config is the single source of project identity; new apps are discovered.
const names: string[] = [];
for (const directory of readdirSync(resolve(workspace, "apps"))) {
  const file = Bun.file(resolve(workspace, "apps", directory, "di-framework.config.json"));
  if (await file.exists()) names.push((await file.json()).name);
}
const requested = process.argv.slice(2);
for (const name of requested) {
  if (!names.includes(name)) throw new Error(`Unknown app ${name}; choose ${names.join(", ")}`);
}
const selected = requested.length ? requested : names.sort();
const port = Number(process.env.DI_REGISTRY_PORT ?? "25001");
if (!Number.isInteger(port) || port < 1024 || port > 65535) throw new Error("DI_REGISTRY_PORT must be 1024–65535");
const httpPort = Number(process.env.DI_HTTP_PORT ?? "28080");
if (!Number.isInteger(httpPort) || httpPort < 1024 || httpPort > 65535) throw new Error("DI_HTTP_PORT must be 1024–65535");
if (httpPort === port) throw new Error("DI_HTTP_PORT and DI_REGISTRY_PORT must be different");

// Explicitly scoped to the selected di-framework-kube instance, never kubectl's current context.
await run([binary, "up", "--name", instance, "--http-port", String(httpPort), "--allow-insecure-registries", "--values", resolve(workspace, "infra/postgres-host.yaml")]);
const platform = await outputs();
await run(kubectl(platform, "apply", "-f", resolve(workspace, "infra/registry.yaml")));
await run(kubectl(platform, "rollout", "status", "deployment/examples-registry", "--timeout=180s"));

if (selected.includes("node-network") || selected.includes("node-http")) {
  await run(kubectl(platform, "apply", "-f", resolve(workspace, "infra/node-compat-echo.yaml")));
  await run(kubectl(platform, "rollout", "status", "deployment/node-compat-echo", "--timeout=180s"));
  if (selected.includes("node-network")) {
    const address = await run(kubectl(platform, "get", "service", "node-compat-echo", "-o", "jsonpath={.spec.clusterIP}"), { capture: true });
    await Bun.write(resolve(workspace, "apps/node-network/fixtures/network.json"), JSON.stringify({ address }) + "\n");
  }
}

if (selected.includes("postgres")) {
  const { provisionPostgres } = await import("./postgres");
  await provisionPostgres(platform);
}

const { provisionBindings } = await import("./bindings");
await provisionBindings(platform, selected);

const forward = Bun.spawn(kubectl(platform, "port-forward", "--address=127.0.0.1", "service/examples-registry", `${port}:5000`), {
  stdout: "pipe", stderr: "inherit",
});
let stopping = false;
const stop = () => { stopping = true; forward.kill(); };
process.once("SIGINT", () => { stop(); process.exit(130); });
process.once("SIGTERM", () => { stop(); process.exit(143); });
try {
  const reader = forward.stdout.getReader();
  const ready = async () => {
    let log = "";
    while (true) {
      const { done, value } = await reader.read();
      if (done) throw new Error("Registry port-forward exited before becoming ready; check DI_REGISTRY_PORT");
      log += new TextDecoder().decode(value);
      if (log.includes("Forwarding from 127.0.0.1:")) return;
    }
  };
  let timeout: ReturnType<typeof setTimeout>;
  await Promise.race([
    ready(),
    new Promise<never>((_, reject) => { timeout = setTimeout(() => reject(new Error("Registry port-forward timed out")), 30_000); }),
  ]).finally(() => clearTimeout(timeout!));
  // Drain kubectl's connection messages so long deploys cannot fill the pipe.
  void (async () => { while (!(await reader.read()).done) { /* drain */ } })();
  void forward.exited.then((code) => {
    if (!stopping) console.error(`Registry port-forward exited unexpectedly (${code})`);
  });
  const env = {
    ...process.env,
    DI_KUBE_CONFIG: platform.kubeconfig,
    DI_KUBE_NAMESPACE: platform.namespace,
    DI_REGISTRY_PORT: String(port),
  };
  const deployed: string[] = [];
  const failed: string[] = [];
  for (const name of selected) {
    try {
      await run([resolve(workspace, "node_modules/.bin/di-framework"), "wasmcloud", "deploy", name, "--yes"], { env });
      // The extension generates port 80; the Kubesolo profile listens on 9191.
      await run(kubectl(platform, "patch", "service", name, "--type=merge", "-p",
        JSON.stringify({ spec: { ports: [{ name: "http", port: 80, targetPort: 9191, protocol: "TCP" }] } })));
      if (name === "outgoing-http") {
        // Grant only the fixture endpoint; the runtime denies HTTP egress by default.
        await run(kubectl(platform, "patch", "workloaddeployment", name, "--type=json", "-p",
          JSON.stringify([{ op: "add", path: "/spec/template/spec/components/0/localResources",
            value: { allowedHosts: ["http://binding-echo:8080"] } }])));
      }
      // Operator readiness alone does not prove the guest can serve a request.
      let healthy = false;
      let detail = "no response";
      const deadline = Date.now() + 120_000;
      while (Date.now() < deadline) {
        try {
          const response = await fetch(new URL("/health", platform.endpoints.http), {
            headers: { Host: name }, signal: AbortSignal.timeout(5_000),
          });
          const body = await response.text();
          detail = `HTTP ${response.status}: ${body}`;
          if (response.ok && JSON.parse(body).status === "ok") { healthy = true; break; }
        } catch (error) { detail = String(error); }
        await Bun.sleep(1_000);
      }
      if (!healthy) throw new Error(`${name} failed its live /health check: ${detail}`);
      console.log(`${name}: live /health passed`);
      deployed.push(name);
    } catch (error) {
      failed.push(name);
      console.error(`${name}: deployment failed: ${error}`);
    }
  }
  console.log(`\nDeployed ${deployed.join(", ") || "no apps"} to ${instance}. HTTP: ${platform.endpoints.http}`);
  console.log("Run bun run smoke to verify the live APIs.");
  if (failed.length) throw new Error(`Failed to deploy: ${failed.join(", ")}`);
} finally {
  stop();
  await forward.exited;
}
