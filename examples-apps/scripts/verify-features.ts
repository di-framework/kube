import { mkdirSync, openSync, closeSync, realpathSync } from "node:fs";
import { resolve } from "node:path";
import assert from "node:assert/strict";
import {
  loadProject, discoverActors, discoverScheduledJobs, discoverQueueHandlers,
  requirementsForProject, renderWorkloadManifest,
} from "@di-framework/cli-plugin-wasmcloud";
import { outputs, kubectl, run, workspace } from "./platform";
import { verifyStaticSite } from "../shared/feature-checks";

const names = ["private-checkout", "static-site", "actor-counter", "scheduled-maintenance", "durable-receipts", "schema-migrations"];
const requested = process.argv.slice(2);
for (const name of requested) if (!names.includes(name)) throw new Error(`Unknown feature app: ${name}`);
const selected = requested.length ? requested : names;
const reportDirectory = resolve(workspace, ".local/verification-main");
mkdirSync(reportDirectory, { recursive: true });
const framework = await Bun.file(resolve(workspace, ".local/framework.json")).json();
const revision = Bun.spawnSync(["git", "-C", framework.directory, "rev-parse", "HEAD"]);
if (revision.exitCode !== 0 || revision.stdout.toString().trim() !== framework.revision) throw new Error("Framework revision changed; rebuild and rerun link:framework");
const manifest = await Bun.file(resolve(workspace, "package.json")).json();
for (const name of Object.keys(manifest.overrides)) {
  if (!name.startsWith("@di-framework/")) continue;
  const expected = realpathSync(resolve(framework.directory, "packages", `di-framework-${name.split("/")[1]}`));
  if (realpathSync(resolve(workspace, "node_modules", name)) !== expected) throw new Error(`Link changed for ${name}; rerun link:framework`);
}

const steps: Array<{ name: string; passed: boolean; detail?: string; log?: string }> = [];
async function command(name: string, args: string[], cwd = workspace, timeoutMs = 300_000) {
  const file = resolve(reportDirectory, `${name}.log`);
  const descriptor = openSync(file, "w");
  const child = Bun.spawn(args, { cwd, env: process.env, stdout: descriptor, stderr: descriptor });
  closeSync(descriptor);
  let timedOut = false;
  const timeout = setTimeout(() => { timedOut = true; child.kill(); }, timeoutMs);
  const code = await child.exited.finally(() => clearTimeout(timeout));
  const passed = code === 0 && !timedOut;
  steps.push({ name, passed, detail: timedOut ? `Timed out after ${timeoutMs}ms` : `exit ${code}`, log: file });
  console.log(`${passed ? "PASS" : "FAIL"} ${name} (${file})`);
  return passed;
}
async function check(name: string, action: () => Promise<void>) {
  try { await action(); steps.push({ name, passed: true }); console.log(`PASS ${name}`); return true; }
  catch (error) { steps.push({ name, passed: false, detail: String(error) }); console.error(`FAIL ${name}: ${error}`); return false; }
}
const platform = await outputs();
const connection = { target: "kubesolo", kubeconfig: platform.kubeconfig, namespace: platform.namespace, registry: { push: "127.0.0.1:25001/examples", pull: `examples-registry.${platform.namespace}.svc.cluster.local:5000/examples`, insecure: true } };
try {
  await command("prepare", ["bun", "run", "prepare:features"]);
  await command("typecheck", ["bun", "run", "check"]);
  await command("local-tests", ["bun", "test", "tests/features.test.ts"]);
  const deployable: string[] = [];
  for (const name of selected) {
    const directory = resolve(workspace, "apps", name);
    const built = await command(`${name}-build`, [resolve(workspace, "node_modules/.bin/di-framework"), "wasmcloud", "build"], directory);
    const manifest = resolve(reportDirectory, `${name}.yaml`);
    const rendered = await check(`${name}-manifest`, async () => {
      const project = loadProject(directory);
      const yaml = renderWorkloadManifest(project, connection, "registry.invalid/verification:probe", requirementsForProject(project), [], { hasActors: discoverActors(project).length > 0 }, discoverScheduledJobs(directory), discoverQueueHandlers(project));
      await Bun.write(manifest, yaml);
    });
    const valid = rendered && await command(`${name}-server-validation`, kubectl(platform, "apply", "--dry-run=server", "--validate=strict", "-f", manifest));
    if (built && valid) deployable.push(name);
  }
  if (deployable.length) {
    await command("deploy", ["bun", "run", "deploy", ...deployable], workspace, 900_000);
    const outcomes: Array<{ app: string; passed: boolean }> = await Bun.file(resolve(workspace, ".local/deployment.json")).json();
    for (const name of deployable) {
      if (!outcomes.find((entry) => entry.app === name)?.passed) continue;
      const request = (path: string, init?: RequestInit) => fetch(new URL(path, platform.endpoints.http), { ...init, headers: { ...Object.fromEntries(new Headers(init?.headers)), Host: name }, signal: AbortSignal.timeout(15_000) });
      if (name === "static-site") await check(`${name}-live`, () => verifyStaticSite(request));
      else if (name === "private-checkout") await check(`${name}-live`, async () => {
        const response = await request("/verify"); assert.equal(response.status, 200);
        assert.deepEqual(await response.json(), { totalCents: 3000, denied: true, transport: "in-process" });
      });
      else if (name === "schema-migrations") await check(`${name}-live`, async () => {
        const response = await request("/verify"); assert.equal(response.status, 200);
        assert.deepEqual(await response.json(), { versions: ["1", "2"], upToDate: true });
      });
      else if (name === "actor-counter") await check(`${name}-live`, async () => {
        const key = `probe-${Date.now()}`;
        const invoke = async (method: string, args: unknown[] = [], expectedStatus = 200) => {
          const response = await request(`/_actors/VerificationCounter/${key}/${method}`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ args }) });
          const body = await response.json(); assert.equal(response.status, expectedStatus, JSON.stringify(body)); return body.result;
        };
        assert.equal(await invoke("increment", [2]), 2);
        await invoke("rollback", [], 500);
        assert.equal(await invoke("read"), 2);
      });
      else if (name === "scheduled-maintenance") {
        await check(`${name}-live`, async () => {
          const job = `verify-maintenance-${Date.now()}`;
          try {
            await run(kubectl(platform, "create", "job", job, "--from=cronjob/scheduled-maintenance-verify-maintenance"));
            await run(kubectl(platform, "wait", "--for=condition=complete", `job/${job}`, "--timeout=60s"));
            const logs = await run(kubectl(platform, "logs", `job/${job}`), { capture: true });
            assert.match(logs, /"completed":true/);
          } finally { await run(kubectl(platform, "delete", "job", job, "--ignore-not-found", "--wait=false")); }
        });
      } else {
        await check(`${name}-live`, async () => { throw new Error("No host queue producer/consumer wiring is exposed by the framework; workload readiness does not verify delivery"); });
      }
    }
  }
} catch (error) {
  steps.push({ name: "verification-runner", passed: false, detail: String(error) });
  console.error(error);
} finally {
  const report = { framework, platform: { namespace: platform.namespace, http: platform.endpoints.http }, recordedAt: new Date().toISOString(), steps, passed: steps.every((step) => step.passed) };
  await Bun.write(resolve(reportDirectory, "report.json"), JSON.stringify(report, null, 2) + "\n");
  console.log(`\n${steps.filter((step) => step.passed).length}/${steps.length} checks passed; report: ${reportDirectory}/report.json`);
  if (!report.passed) process.exitCode = 1;
}
