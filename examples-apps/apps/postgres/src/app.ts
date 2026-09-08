import { useContainer } from "@di-framework/core/container";
import { TypedRouter } from "@di-framework/http";
import { ExampleDatabase } from "./bindings";

const database = useContainer().resolve(ExampleDatabase);
const router = TypedRouter();
router.get("/", () => Response.json({ app: "postgres", routes: ["/health", "/verify"] }));
router.get("/health", async () => {
  await database.execute("SELECT 1");
  return Response.json({ app: "postgres", status: "ok" });
});
router.post("/verify", async () => {
  // One batch holds one connection. A temporary table isolates concurrent probes.
  // PostgreSQL raises an error if any assertion fails; success requires real SQL execution.
  await database.execute(`
    CREATE TEMP TABLE binding_probe (id integer PRIMARY KEY, message text) ON COMMIT DROP;
    INSERT INTO binding_probe VALUES (1, 'hello from wasm');
    DO $$ BEGIN
      IF (SELECT message FROM binding_probe WHERE id = 1) IS DISTINCT FROM 'hello from wasm' THEN
        RAISE EXCEPTION 'insert/read verification failed';
      END IF;
    END $$;
    UPDATE binding_probe SET message = 'updated through binding' WHERE id = 1;
    DO $$ BEGIN
      IF (SELECT message FROM binding_probe WHERE id = 1) IS DISTINCT FROM 'updated through binding' THEN
        RAISE EXCEPTION 'update/read verification failed';
      END IF;
    END $$;
    DELETE FROM binding_probe WHERE id = 1;
    DO $$ BEGIN
      IF EXISTS (SELECT 1 FROM binding_probe) THEN RAISE EXCEPTION 'delete verification failed'; END IF;
    END $$;
  `);
  let rejectsErrors = false;
  try { await database.execute("DO $$ BEGIN RAISE EXCEPTION 'expected binding probe failure'; END $$;"); }
  catch { rejectsErrors = true; }
  if (!rejectsErrors) throw new Error("PostgreSQL errors were not propagated");
  return Response.json({ app: "postgres", rejectsErrors, binding: database.bindingName, insert: true, read: true, update: true, delete: true });
});
router.all("*", () => Response.json({ error: "Not found" }, { status: 404 }));
export default router;
