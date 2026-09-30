import { realpathSync } from "node:fs";
import { resolve } from "node:path";
import { packageDirectory, sourceRoots } from "./framework-packages";

const workspace = resolve(import.meta.dir, "..");
const manifest = await Bun.file(resolve(workspace, "package.json")).json();
const names = Object.keys(manifest.overrides).filter((name) => name.startsWith("@di-framework/"));
const packages: Record<string, string> = {};
for (const name of names) {
  const directory = packageDirectory(name);
  const pkg = await Bun.file(resolve(directory, "package.json")).json();
  if (pkg.name !== name) throw new Error(`Expected ${name} at ${directory}`);
  const child = Bun.spawn(["bun", "link"], { cwd: directory, stdout: "inherit", stderr: "inherit" });
  if (await child.exited !== 0) throw new Error(`Could not link ${name}`);
  packages[name] = directory;
}
const install = Bun.spawn(["bun", "install"], { cwd: workspace, stdout: "inherit", stderr: "inherit" });
if (await install.exited !== 0) throw new Error("Could not install locally linked framework packages");

for (const name of names) {
  const expected = realpathSync(packages[name]!);
  const actual = realpathSync(resolve(workspace, "node_modules", name));
  if (actual !== expected) throw new Error(`${name} resolves to ${actual}, expected ${expected}`);
  packages[name] = actual;
  console.log(`${name} → ${actual}`);
}

const sources = [];
for (const source of sourceRoots()) {
  const revision = Bun.spawnSync(["git", "-C", source.directory, "rev-parse", "HEAD"]);
  if (revision.exitCode !== 0) throw new Error(`Cannot record ${source.name} revision`);
  sources.push({ name: source.name, directory: source.directory, revision: revision.stdout.toString().trim() });
}
const framework = sources.find((source) => source.name === "di-framework");
if (!framework) throw new Error("Missing di-framework source");
await Bun.write(
  resolve(workspace, ".local/framework.json"),
  JSON.stringify({ directory: framework.directory, revision: framework.revision, sources, packages }, null, 2) + "\n",
);
