import { realpathSync } from "node:fs";
import { resolve } from "node:path";

const workspace = resolve(import.meta.dir, "..");
const framework = resolve(process.env.DI_FRAMEWORK_DIR ?? resolve(workspace, "../../di-framework"));
const manifest = await Bun.file(resolve(workspace, "package.json")).json();
for (const name of Object.keys(manifest.overrides)) {
  if (!name.startsWith("@di-framework/")) continue;
  const directory = resolve(framework, "packages", `di-framework-${name.split("/")[1]}`);
  const pkg = await Bun.file(resolve(directory, "package.json")).json();
  if (pkg.name !== name) throw new Error(`Expected ${name} at ${directory}`);
  const child = Bun.spawn(["bun", "link"], { cwd: directory, stdout: "inherit", stderr: "inherit" });
  if (await child.exited !== 0) throw new Error(`Could not link ${name}`);
}
const install = Bun.spawn(["bun", "install"], { cwd: workspace, stdout: "inherit", stderr: "inherit" });
if (await install.exited !== 0) throw new Error("Could not install locally linked framework packages");

for (const name of Object.keys(manifest.overrides)) {
  if (!name.startsWith("@di-framework/")) continue;
  const expected = realpathSync(resolve(framework, "packages", `di-framework-${name.split("/")[1]}`));
  const actual = realpathSync(resolve(workspace, "node_modules", name));
  if (actual !== expected) throw new Error(`${name} resolves to ${actual}, expected ${expected}`);
  console.log(`${name} → ${actual}`);
}

const revision = Bun.spawnSync(["git", "-C", framework, "rev-parse", "HEAD"]);
if (revision.exitCode !== 0) throw new Error("Cannot record framework revision");
await Bun.write(resolve(workspace, ".local/framework.json"), JSON.stringify({ directory: framework, revision: revision.stdout.toString().trim() }, null, 2) + "\n");
