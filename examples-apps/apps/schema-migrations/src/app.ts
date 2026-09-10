import { createMigrationDatabase, MigrationRunner } from "@di-framework/repo";
import { TypedRouter } from "@di-framework/http";
import { migrations } from "./migrations";

export function createSchemaApp(path?: string) {
  let databasePromise: ReturnType<typeof createMigrationDatabase> | undefined;
  let runnerPromise: Promise<MigrationRunner> | undefined;

  const resolvePath = () =>
    path ?? process.env.MIGRATION_DB_PATH ?? (process.env.DI_STORAGE_DIR
      ? `${process.env.DI_STORAGE_DIR.replace(/\/$/, "")}/migrations.db`
      : ":memory:");

  const getRunner = async () => {
    if (!databasePromise) {
      databasePromise = createMigrationDatabase(resolvePath());
    }
    const database = await databasePromise;
    if (!runnerPromise) {
      runnerPromise = Promise.resolve(
        new MigrationRunner({ db: database, binding: "verification", migrations }),
      );
    }
    return { database, runner: await runnerPromise };
  };

  const router = TypedRouter();
  router.get("/health", () => Response.json({ app: "schema-migrations", status: "ok" }));
  router.get("/verify", async () => {
    const { database, runner } = await getRunner();
    await runner.execute();
    const status = await runner.status();
    const columns = await database.query<{ name: string }>("PRAGMA table_info(invoices)");
    const versions = status.applied.map((entry) => entry.version);
    if (versions.join(",") !== "1,2" || !columns.some((column) => column.name === "status")) {
      throw new Error("Migration assertions failed");
    }
    return Response.json({ versions, upToDate: status.isUpToDate });
  });
  return { router, getRunner };
}

const app = createSchemaApp();
export default app.router;
