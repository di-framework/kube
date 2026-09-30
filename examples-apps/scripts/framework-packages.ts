import { resolve } from "node:path";

const workspace = resolve(import.meta.dir, "..");

export type SourceRoot = { name: string; directory: string };

/** Sibling checkouts that publish the packages this workspace links. */
export function sourceRoots(): SourceRoot[] {
  return [
    {
      name: "di-framework",
      directory: resolve(process.env.DI_FRAMEWORK_DIR ?? resolve(workspace, "../../di-framework")),
    },
    {
      name: "cli-extensions",
      directory: resolve(process.env.DI_CLI_EXTENSIONS_DIR ?? resolve(workspace, "../../cli-extensions")),
    },
    {
      name: "platform",
      directory: resolve(process.env.DI_PLATFORM_DIR ?? resolve(workspace, "../../platform")),
    },
  ];
}

const relocated: Record<string, (roots: SourceRoot[]) => string> = {
  "@di-framework/cli-plugin-platform": (roots) => resolve(roots[1]!.directory, "packages/cli-plugin-platform"),
  "@di-framework/bindings": (roots) => resolve(roots[2]!.directory, "platform/bindings"),
  "@di-framework/platform": (roots) => resolve(roots[2]!.directory, "platform/platform"),
};

/** Directory of a linked `@di-framework/*` package in the sibling checkouts. */
export function packageDirectory(name: string): string {
  const roots = sourceRoots();
  const special = relocated[name];
  if (special) return special(roots);
  const slug = name.split("/")[1];
  if (!slug) throw new Error(`Invalid package name ${name}`);
  return resolve(roots[0]!.directory, "packages", `di-framework-${slug}`);
}
