import { readFileSync } from "node:fs";
import { useContainer } from "@di-framework/core/container";
import { TypedRouter } from "@di-framework/http";
import { ProbeService } from "./service";

const probe = useContainer().resolve(ProbeService);
const router = TypedRouter();
router.get("/", () => Response.json({ app: "node-network", routes: ["/health", "/verify", "/verify/ip"] }));
router.get("/health", () => Response.json({ app: "node-network", status: "ok" }));
router.get("/verify", async () => {
  try {
    return Response.json(await probe.verify());
  } catch (error) {
    return Response.json({ error: error instanceof Error ? error.message : String(error) }, { status: 500 });
  }
});
router.get("/verify/ip", async () => {
  try {
    // Seeded by deploy.ts from the echo Service; never supplied by the HTTP caller.
    const { address } = JSON.parse(readFileSync("fixtures/network.json", "utf8"));
    return Response.json(await new ProbeService(address).verify());
  } catch (error) {
    return Response.json({ error: error instanceof Error ? error.message : String(error) }, { status: 500 });
  }
});
router.all("*", () => Response.json({ error: "Not found" }, { status: 404 }));

export default router;
