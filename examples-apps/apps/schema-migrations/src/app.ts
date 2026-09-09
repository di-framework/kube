import { Database } from "bun:sqlite";
import { MigrationRunner } from "@di-framework/repo";
import { TypedRouter } from "@di-framework/http";
import { migrations } from "./migrations";
export function createSchemaApp(path = ":memory:") {
  const database = new Database(path, { create: true });
  const runner = new MigrationRunner({ db: database, binding: "verification", migrations });
  const router = TypedRouter();
  router.get("/health", () => Response.json({ app: "schema-migrations", status: "ok" }));
  router.get("/verify", async () => {
    await runner.execute();
    const status = await runner.status();
    const columns = database.query("PRAGMA table_info(invoices)").all() as { name: string }[];
    const versions = status.applied.map((entry) => entry.version);
    if (versions.join(",") !== "1,2" || !columns.some((column) => column.name === "status")) throw new Error("Migration assertions failed");
    return Response.json({ versions, upToDate: status.isUpToDate });
  });
  return { router, database, runner };
}
const app = createSchemaApp();
export default app.router;
