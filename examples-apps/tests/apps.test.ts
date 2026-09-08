import { test } from "bun:test";
import { resolve } from "node:path";
import { rmSync } from "node:fs";
import http from "../apps/node-http/src/app";
import runtime from "../apps/node-runtime/src/app";
import crypto from "../apps/node-crypto/src/app";
import network from "../apps/node-network/src/app";
import greeter from "../apps/greeter/src/app";
import catalog from "../apps/catalog/src/app";
import quotes from "../apps/quotes/src/app";
import { cases, requestFor, verify } from "./api-cases";

const apps = { greeter, catalog, quotes, "node-runtime": runtime, "node-crypto": crypto, "node-network": network, "node-http": http };
for (const [index, example] of cases.entries()) {
  if (example.liveOnly || example.app === "postgres") continue;
  if (!(example.app in apps)) throw new Error(`Missing local test app: ${example.app}`);
  const app = apps[example.app as keyof typeof apps];
  test(`${index + 1}: ${example.app} ${example.method ?? "GET"} ${example.path} → ${example.status}`, async () => {
    const previous = process.cwd();
    const directory = resolve(import.meta.dir, "../apps", example.app);
    try {
      // Native fs resolves the same relative fixture paths as the seeded guest fs.
      process.chdir(directory);
      await verify(example, await app.fetch(requestFor(example)));
    } finally {
      process.chdir(previous);
      if (example.app === "node-runtime") rmSync(resolve(directory, "fixtures/roundtrip.txt"), { force: true });
    }
  });
}
