import { TypedRouter } from "@di-framework/http";
export { VerificationCounter } from "./counter";
const router = TypedRouter();
router.get("/health", () => Response.json({ app: "actor-counter", status: "ok" }));
router.all("*", () => Response.json({ error: "Not found" }, { status: 404 }));
export default router;
