import { Container } from "@di-framework/core/decorators";
import { AsyncLocalStorage } from "node:async_hooks";
import { Buffer } from "node:buffer";
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import { join } from "node:path";
import process from "node:process";

@Container()
export class ProbeService {
  async verify() {
    const settings = JSON.parse(readFileSync(join(process.cwd(), "fixtures/settings.json"), "utf8"));
    let missingFile = "";
    try { readFileSync("missing-node-compat-fixture.json"); }
    catch (error) { missingFile = (error as NodeJS.ErrnoException).code ?? ""; }
    let missingModule = "";
    try { createRequire("/node-compat-probe.js")("missing-node-compat-fixture"); }
    catch (error) { missingModule = (error as NodeJS.ErrnoException).code ?? ""; }
    // This fixture is disposable. Guest writes are memory-only, never durable storage.
    const scratch = join(process.cwd(), "fixtures/roundtrip.txt");
    writeFileSync(scratch, "guest roundtrip", "utf8");
    const roundtrip = existsSync(scratch) && readFileSync(scratch, "utf8") === "guest roundtrip";
    const key = "DI_NODE_COMPAT_PROBE";
    const previous = process.env[key];
    let environment = false;
    try {
      process.env[key] = "first";
      const first = process.env[key];
      process.env[key] = "second";
      environment = first === "first" && process.env[key] === "second" && Object.keys(process.env).includes(key);
      delete process.env[key];
      environment = environment && !(key in process.env);
    } finally {
      if (previous !== undefined) process.env[key] = previous;
    }
    const context = new AsyncLocalStorage<string>();
    const scoped = await context.run("request-512", async () => {
      await Promise.resolve();
      return context.getStore();
    });
    const concurrentContexts = await Promise.all([20, 2].map((delay) => context.run(`request-${delay}`, async () => {
      await new Promise<void>((resolve) => setTimeout(resolve, delay));
      return context.getStore();
    })));
    let cancelled = true;
    const cancelledTimer = setTimeout(() => { cancelled = false; }, 1);
    clearTimeout(cancelledTimer);
    const timerArgument = await new Promise<string>((resolve) => setTimeout(resolve, 2, "timer-argument"));
    const intervalTicks = await new Promise<number>((resolve) => {
      let ticks = 0;
      const interval = setInterval(() => {
        if (++ticks === 2) { clearInterval(interval); resolve(ticks); }
      }, 2);
    });
    const immediate = await new Promise<boolean>((resolve) => setImmediate(() => resolve(true)));
    return {
      concurrentContexts, cancelled, timerArgument, intervalTicks, immediate,
      settings, missingFile, missingModule, roundtrip, environment,
      context: scoped, contextRestored: context.getStore() === undefined,
      buffer: Buffer.from("Wasm ✓").toString("base64"),
      normalizedPath: join("/fixtures", "nested", "..", "settings.json"),
      timersAvailable: typeof globalThis.setTimeout === "function",
      runtime: { arch: process.arch, cwd: process.cwd() },
    };
  }
}
