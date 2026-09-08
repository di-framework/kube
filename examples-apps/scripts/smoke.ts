import { outputs, kubectl, run } from "./platform";
import { cases, requestFor, verify } from "../tests/api-cases";

const platform = await outputs();
const requested = process.argv.slice(2);
for (const name of requested) {
  if (!cases.some((example) => example.app === name)) throw new Error(`Unknown app: ${name}`);
}
const selected = requested.length ? cases.filter((example) => requested.includes(example.app)) : cases;
let failures = 0;
for (const example of selected) {
  try {
    const response = await fetch(requestFor(example, platform.endpoints.http), { signal: AbortSignal.timeout(15_000) });
    const expected = example.app === "secrets" && example.path === "/verify"
      ? { ...example.expected, digest: await run(kubectl(platform, "get", "configmap", "binding-secret-check", "-o", "jsonpath={.data.digest}"), { capture: true }) }
      : example.expected;
    await verify({ ...example, expected }, response);
    console.log(`PASS ${example.app}: ${example.method ?? "GET"} ${example.path} → ${response.status}`);
  } catch (error) {
    failures++;
    console.error(`FAIL ${example.app}: ${example.method ?? "GET"} ${example.path}: ${error}`);
  }
}
console.log(`\n${selected.length - failures}/${selected.length} live API checks passed at ${platform.endpoints.http}`);
if (failures) process.exitCode = 1;
