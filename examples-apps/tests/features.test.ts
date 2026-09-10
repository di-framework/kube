import { expect, test } from "bun:test";
import { Database } from "bun:sqlite";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { ActorRuntime, SqliteActorStorage } from "@di-framework/actors";
import { SqliteQueueBackend, QueueWorker } from "@di-framework/queues";
import checkout from "../apps/private-checkout/src/app";
import staticSite from "../apps/static-site/src/app";
import { VerificationCounter } from "../apps/actor-counter/src/counter";
import { createMaintenance } from "../apps/scheduled-maintenance/src/app";
import { MaintenanceAudit } from "../apps/scheduled-maintenance/src/service";
import { dispatcher, container as receipts } from "../apps/durable-receipts/src/app";
import { ReceiptAudit } from "../apps/durable-receipts/src/processor";
import { createSchemaApp } from "../apps/schema-migrations/src/app";
import { verifyStaticSite } from "../shared/feature-checks";

const temp = () => mkdtempSync(join(tmpdir(), "di-main-examples-"));
async function cli(...args: string[]) {
  const child = Bun.spawn([process.execPath, resolve(import.meta.dir, "../node_modules/.bin/di-framework"), ...args], {
    cwd: resolve(import.meta.dir, ".."), stdout: "pipe", stderr: "pipe",
  });
  const timeout = setTimeout(() => child.kill(), 10_000);
  try {
    const [output, error, code] = await Promise.all([new Response(child.stdout).text(), new Response(child.stderr).text(), child.exited]);
    if (code !== 0) throw new Error(`CLI ${args.join(" ")} failed (${code}): ${error}${output}`);
    const result = JSON.parse(output);
    return result.data ?? result;
  } finally { clearTimeout(timeout); }
}


test("private binding resolves the exported service and rejects an ungranted caller", async () => {
  const response = await checkout.fetch(new Request("http://private-checkout/verify"));
  expect(response.status).toBe(200);
  expect(await response.json()).toEqual({ totalCents: 3000, denied: true, transport: "in-process" });
});

test("packaged static assets preserve content, metadata, conditional GET, HEAD, and rejection behavior", async () => {
  await verifyStaticSite((path, init) => staticSite.fetch(new Request(`http://static-site${path}`, init)));
});

test("actors serialize calls, roll back failed writes, migrate once, deduplicate and persist across restart", async () => {
  const directory = temp();
  let storage = new SqliteActorStorage({ baseDir: directory, fileLocking: true });
  let runtime = new ActorRuntime({ storage });
  try {
    runtime.register(VerificationCounter);
    const key = "customer/with spaces";
    const counter = runtime.get(VerificationCounter, key);
    const results = await Promise.all(Array.from({ length: 8 }, () => counter.increment()));
    expect(results).toEqual([1, 2, 3, 4, 5, 6, 7, 8]);
    await expect(counter.rollback()).rejects.toThrow("Expected rollback probe");
    expect(await counter.read()).toBe(8);
    expect(await runtime.invoke(VerificationCounter, key, "increment", [2], { requestId: "same-call" })).toBe(10);
    expect(await runtime.invoke(VerificationCounter, key, "increment", [2], { requestId: "same-call" })).toBe(10);
    const records = await runtime.listActors();
    expect(records).toHaveLength(1);
    expect(records[0]?.actorKey).toBe(key);
    await runtime.clear();
    expect((await cli("actor", "list", "--dir", directory, "--json")).total).toBe(1);
    storage = new SqliteActorStorage({ baseDir: directory, fileLocking: true });
    runtime = new ActorRuntime({ storage });
    runtime.register(VerificationCounter);
    expect(await runtime.get(VerificationCounter, key).read()).toBe(10);
    const file = (await storage.listPersistedActors())[0]!.filePath;
    const database = new Database(file, { readonly: true });
    try { expect(database.query("SELECT version FROM probe_schema WHERE id = 1").get()).toEqual({ version: 2 }); }
    finally { database.close(); }
    expect(await runtime.inspect(VerificationCounter, key)).toBeDefined();
    expect((await runtime.reload({ namespace: "verification" })).success).toBe(true);
    expect(await runtime.get(VerificationCounter, key).read()).toBe(10);
    expect((await runtime.reset({ namespace: "verification", actorName: "VerificationCounter", actorKey: key })).success).toBe(true);
    expect(await runtime.get(VerificationCounter, key).read()).toBe(0);
  } finally {
    await runtime.clear();
    rmSync(directory, { recursive: true, force: true });
  }
});

test("external cron resolves injected services and waits for completion without automatic timers", async () => {
  const container = createMaintenance();
  try {
    expect(container.getCronMode()).toBe("external");
    expect(container.resolve(MaintenanceAudit).runs).toEqual([]);
    const result = await container.invokeCronJob("verify-maintenance");
    expect(result.status).toBe("success");
    expect(result.result).toEqual({ job: "verify-maintenance", completed: true, runs: 1 });
    expect(container.resolve(MaintenanceAudit).runs).toEqual(["completed"]);
  } finally { container.clear(); }
});

test("SQLite jobs survive restart, retry, deduplicate, retain dead letters and support manual retry", async () => {
  const directory = temp();
  const path = join(directory, "receipts.sqlite");
  let backend = new SqliteQueueBackend(path);
  let worker: QueueWorker | undefined;
  const waitFor = async (id: string, status: string) => {
    const deadline = Date.now() + 5000;
    while (Date.now() < deadline) {
      const job = await backend.getJob(id);
      if (job?.status === status) return job;
      await Bun.sleep(10);
    }
    throw new Error(`Job ${id} did not reach ${status}: ${JSON.stringify(await backend.getJob(id))}`);
  };
  try {
    const job = await backend.enqueue("verification-receipts", { id: "receipt-retry", amount: 3000, failUntilAttempt: 1 }, { idempotencyKey: "receipt-retry", backoffMs: 1 });
    const duplicate = await backend.enqueue("verification-receipts", { id: "receipt-retry", amount: 3000 }, { idempotencyKey: "receipt-retry" });
    expect(duplicate.id).toBe(job.id);
    await backend.close();
    backend = new SqliteQueueBackend(path);
    expect((await backend.getJob(job.id))?.status).toBe("pending");
    worker = new QueueWorker(backend, dispatcher, { queues: ["verification-receipts"], pollIntervalMs: 5 });
    worker.start();
    expect((await waitFor(job.id, "completed")).attempts).toBe(2);
    expect(receipts.resolve(ReceiptAudit).receipts.get("receipt-retry")).toBe(3000);
    const bad = await backend.enqueue("verification-receipts", { id: "receipt-invalid", amount: -1 }, { maxRetries: 0 });
    expect((await waitFor(bad.id, "dead-letter")).errorMessage).toContain("Invalid receipt");
    await worker.stop(); worker = undefined;
    const retried = await backend.retryJob("verification-receipts", bad.id);
    expect(retried).toHaveLength(1);
    expect((await backend.getJob(bad.id))?.status).toBe("pending");
    const queues = await cli("queue", "list", "--db", path, "--json");
    expect(queues.queues.find((queue: { name: string }) => queue.name === "verification-receipts").completed).toBe(1);
  } finally {
    if (worker) await worker.stop();
    await backend.close();
    rmSync(directory, { recursive: true, force: true });
  }
}, 15000);

test("schema migrations apply in order, track history, survive restart and preview without writes", async () => {
  const directory = temp();
  const path = join(directory, "invoices.sqlite");
  let app = createSchemaApp(path);
  try {
    let { database, runner } = await app.getRunner();
    expect((await runner.status()).pending.map((migration) => migration.version)).toEqual(["1", "2"]);
    expect((await runner.execute({ dryRun: true })).dryRun).toBe(true);
    expect((await runner.status()).applied).toHaveLength(0);
    const response = await app.router.fetch(new Request("http://schema-migrations/verify"));
    expect(response.status).toBe(200);
    expect(await response.json()).toEqual({ versions: ["1", "2"], upToDate: true });
    await database.close?.();
    app = createSchemaApp(path);
    ({ database, runner } = await app.getRunner());
    expect((await runner.execute()).applied).toHaveLength(0);
    expect((await runner.status()).applied).toHaveLength(2);
    const status = await cli("migrations", "status", "--db", path, "--binding", "verification", "--module", resolve(import.meta.dir, "../apps/schema-migrations/src/migrations.ts"), "--json");
    expect(status.isUpToDate).toBe(true);
    expect(status.applied).toHaveLength(2);
  } finally {
    const { database } = await app.getRunner();
    await database.close?.();
    rmSync(directory, { recursive: true, force: true });
  }
});

test("distributed actor RPC crosses HTTP, enforces caller grants and rejects expired requests", async () => {
  const { RemoteActorClient } = await import("@di-framework/actors");
  const { createActorServer } = await import("../apps/actor-counter/src/server");
  const directory = temp();
  const server = createActorServer(directory);
  const transport = { async send(request: import("@di-framework/actors").ActorRpcRequest) {
    const response = await fetch(server.url, { method: "POST", body: JSON.stringify(request), headers: { "content-type": "application/json" }, signal: AbortSignal.timeout(2000) });
    return response.json();
  } };
  try {
    const client = new RemoteActorClient({ transport, callerId: "verification-client", namespace: "verification", timeoutMs: 2000, maxRetries: 0 });
    expect(await client.invokeRemote("VerificationCounter", "remote", "increment", [3])).toBe(3);
    const request = { requestId: "repeated-http-request", callerId: "verification-client", namespace: "verification", actorType: "VerificationCounter", actorKey: "remote", method: "increment", args: [2], deadline: Date.now() + 2000 };
    const first = await transport.send(request);
    const repeated = await transport.send(request);
    expect(first.success).toBe(true);
    expect(repeated.result).toBe(first.result);
    expect(await client.invokeRemote("VerificationCounter", "remote", "read", [])).toBe(5);
    const denied = await transport.send({ ...request, requestId: "denied", callerId: "unbound" });
    expect(denied.success).toBe(false);
    expect(denied.error.name).toBe("ActorAuthorizationError");
    const expired = await transport.send({ ...request, requestId: "expired", deadline: Date.now() - 1000 });
    expect(expired.error.name).toBe("ActorDeadlineExceededError");
    expect((await server.runtime.getActorOwnership(VerificationCounter, "remote"))?.ownerId).toBe("verification-owner");
  } finally { await server.close(); rmSync(directory, { recursive: true, force: true }); }
});
