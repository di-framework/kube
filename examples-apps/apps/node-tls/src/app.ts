import { useContainer } from "@di-framework/core/container";
import { TypedRouter } from "@di-framework/http";
import { ProbeService } from "./service";

const probe = useContainer().resolve(ProbeService);
const router = TypedRouter();
router.get("/", () => Response.json({ app: "node-tls", routes: ["/health", "/verify"] }));
router.get("/health", () => Response.json({ app: "node-tls", status: "ok" }));
router.get("/verify", async () => {
  try {
    return Response.json(await probe.verify());
  } catch (error) {
    return Response.json({ error: error instanceof Error ? error.message : String(error) }, { status: 500 });
  }
});
router.all("*", () => Response.json({ error: "Not found" }, { status: 404 }));

export default router;
