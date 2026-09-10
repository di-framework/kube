import { HttpRouter, type StaticAssetPackage } from "@di-framework/http";
import assets from "./assets.json";

const router = HttpRouter.builder().static("/assets", {
  directory: "public", package: assets as StaticAssetPackage, live: false,
  cacheControl: "public, max-age=60",
}).build();
router.get("/health", () => Response.json({ app: "static-site", status: "ok" }));
router.all("*", () => Response.json({ error: "Not found" }, { status: 404 }));
export default router;
