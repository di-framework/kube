import { outputs } from "./platform";
import { cases, requestFor, verify } from "../tests/api-cases";

const platform = await outputs();
for (const example of cases) {
  const response = await fetch(requestFor(example, platform.endpoints.http), { signal: AbortSignal.timeout(15_000) });
  await verify(example, response);
  console.log(`PASS ${example.app}: ${example.method ?? "GET"} ${example.path} → ${response.status}`);
}
console.log(`\n${cases.length} live API checks passed at ${platform.endpoints.http}`);
