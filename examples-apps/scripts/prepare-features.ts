import { packageStaticAssets } from "@di-framework/http";
import { resolve } from "node:path";
const directory = resolve(import.meta.dir, "../apps/static-site");
const assets = packageStaticAssets({ directory: resolve(directory, "public") });
// Do not bake workstation paths or a changing timestamp into the fixture.
const packaged = { ...assets, directory: "public", generatedAt: "2026-09-09T00:00:00.000Z" };
await Bun.write(resolve(directory, "src/assets.json"), JSON.stringify(packaged, null, 2) + "\n");
