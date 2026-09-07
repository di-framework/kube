import { useContainer } from "@di-framework/core/container";
import { TypedRouter } from "@di-framework/http";
import { ProductCatalog } from "../../../shared/catalog";

const catalog = useContainer().resolve(ProductCatalog);
const router = TypedRouter();
router.get("/", () => Response.json({ app: "catalog", routes: ["/health", "/products?q=mug", "/products/:sku"] }));
router.get("/health", () => Response.json({ app: "catalog", status: "ok" }));
router.get("/products", (request) => {
  const products = catalog.list(new URL(request.url).searchParams.get("q") ?? "");
  return Response.json({ currency: "USD", products, count: products.length });
});
router.get("/products/:sku", (request) => {
  const product = catalog.find(request.params?.sku ?? "");
  return product
    ? Response.json({ ...product, currency: "USD" })
    : Response.json({ error: "Product not found" }, { status: 404 });
});
router.all("*", () => Response.json({ error: "Not found" }, { status: 404 }));

export default router;
