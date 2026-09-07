import { useContainer } from "@di-framework/core/container";
import { TypedRouter, type Json, type RequestSpec } from "@di-framework/http";
import { InvalidQuote, QuoteService } from "./service";

const quotes = useContainer().resolve(QuoteService);
const router = TypedRouter();
router.get("/", () => Response.json({ app: "quotes", routes: ["/health", "POST /quote"] }));
router.get("/health", () => Response.json({ app: "quotes", status: "ok" }));
router.post<RequestSpec<Json<unknown>>>("/quote", (request) => {
  try {
    return Response.json(quotes.calculate(request.content));
  } catch (error) {
    if (error instanceof InvalidQuote) return Response.json({ error: error.message }, { status: 400 });
    throw error;
  }
});
router.all("*", () => Response.json({ error: "Not found" }, { status: 404 }));

export default router;
