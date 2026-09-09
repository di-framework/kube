import { cpSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { resolve, join } from "node:path";
import { DEFAULT_DEPS } from "../node_modules/@di-framework/cli-plugin-wasmcloud/dist/deps";
import { renderWorldWit, runtimeRequirementsFromJavaScript } from "../node_modules/@di-framework/cli-plugin-wasmcloud/dist/wit";
import { cases, verify } from "../tests/api-cases";

const workspace = resolve(import.meta.dir, "..");
const app = resolve(workspace, "apps/node-tls");
async function command(args: string[], cwd = workspace) {
  const child = Bun.spawn(args, { cwd, stdout: "pipe", stderr: "pipe" });
  const [stdout, stderr, code] = await Promise.all([
    new Response(child.stdout).text(), new Response(child.stderr).text(), child.exited,
  ]);
  if (code !== 0) throw new Error(`${args[0]} failed (${code}): ${stderr}\n${stdout}`);
  return stdout;
}
console.log(await command([resolve(workspace, "node_modules/.bin/di-framework"), "wasmcloud", "build"], app));
const lock = await Bun.file(resolve(app, ".di-framework/wit.lock.json")).text();
if (!lock.includes("wasi:tls")) throw new Error("Build did not discover WASI TLS imports; check local links");

// Wasmtime 48's `serve` linker lacks the TLS resource implementation. Its `run`
// linker supports it. Export a function that calls the SAME router for each case.
const directory = mkdtempSync(join(tmpdir(), "di-kube-tls-"));
try {
  const adapterPath = join(directory, "adapter.ts");
  const outFile = join(directory, "bundle.js");
  writeFileSync(adapterPath, `import app from 'virtual:di-framework-application';
export async function run(path) {
  const response = await app.fetch(new Request('http://node-tls' + path));
  return JSON.stringify({ status: response.status, headers: [...response.headers], body: await response.text() });
}\n`);
  await DEFAULT_DEPS.bundler({ adapterPath, entryPath: resolve(app, "src/app.ts"), outFile });
  const wit = join(directory, "wit");
  cpSync(resolve(app, ".di-framework/wit"), wit, { recursive: true });
  const requirements = runtimeRequirementsFromJavaScript(await Bun.file(outFile).text());
  writeFileSync(join(wit, "world.wit"), renderWorldWit("tls-smoke", "1.0.0", requirements)
    .replace("\n}", "\n  export run: async func(path: string) -> string;\n}"));
  const compiler = DEFAULT_DEPS.componentizeQjsPath();
  if (!compiler) throw new Error("componentize-qjs is required");
  const wasm = join(directory, "component.wasm");
  await command([compiler, "--wit", wit, "--js", outFile, "-n", "application", "-o", wasm]);
  for (const example of cases.filter((item) => item.app === "node-tls")) {
    const output = await command([
      "wasmtime", "run", "-S", "p3=y,tls=y,inherit-network=y,allow-ip-name-lookup=y",
      "--invoke", `run(${JSON.stringify(example.path)})`, wasm,
    ]);
    const result = JSON.parse(JSON.parse(output.trim()));
    await verify(example, new Response(result.body, { status: result.status, headers: result.headers }));
    console.log(`PASS compiled node-tls ${example.path}`);
  }
} finally {
  rmSync(directory, { recursive: true, force: true });
}
