import { test } from "bun:test";
import greeter from "../apps/greeter/src/app";
import catalog from "../apps/catalog/src/app";
import quotes from "../apps/quotes/src/app";
import { cases, requestFor, verify } from "./api-cases";

const apps = { greeter, catalog, quotes };
for (const [index, example] of cases.entries()) {
  test(`${index + 1}: ${example.app} ${example.method ?? "GET"} ${example.path} → ${example.status}`, async () => {
    await verify(example, await apps[example.app].fetch(requestFor(example)));
  });
}
