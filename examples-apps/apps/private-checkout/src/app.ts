import { useContainer } from "@di-framework/core/container";
import { Container } from "@di-framework/core/decorators";
import { ExportService, LocalServiceDevManager, ServiceBinding, UnboundCallerError } from "@di-framework/core/service-bindings";
import { TypedRouter } from "@di-framework/http";

interface Inventory { price(sku: string, quantity: number): Promise<number>; }
@Container()
@ExportService({ name: "probe-inventory", operations: ["price"] })
export class InventoryService implements Inventory {
  async price(sku: string, quantity: number) {
    if (sku !== "mug" || !Number.isInteger(quantity) || quantity < 1) throw new Error("Invalid line item");
    return quantity * 1500;
  }
}
@Container()
class Checkout {
  constructor(@ServiceBinding("inventory", { caller: "probe-checkout", target: "probe-inventory", expectedOperations: ["price"] }) readonly inventory: Inventory) {}
  async total() { return this.inventory.price("mug", 2); }
}
@Container()
class UnboundCheckout {
  constructor(@ServiceBinding("inventory", { caller: "probe-unbound", target: "probe-inventory" }) readonly inventory: Inventory) {}
  async total() { return this.inventory.price("mug", 1); }
}
const services = new LocalServiceDevManager();
services.registerService("probe-inventory", new InventoryService(), { operations: ["price"] });
services.bind("probe-checkout", "inventory", "probe-inventory", { allowedOperations: ["price"] });
services.bind("probe-unbound", "inventory", "probe-inventory", { grantAccess: false });
const router = TypedRouter();
router.get("/health", () => Response.json({ app: "private-checkout", status: "ok" }));
router.get("/verify", async () => {
  const totalCents = await useContainer().resolve(Checkout).total();
  let denied = false;
  try { await useContainer().resolve(UnboundCheckout).total(); }
  catch (error) { if (!(error instanceof UnboundCallerError)) throw error; denied = true; }
  if (totalCents !== 3000 || !denied) throw new Error("Private binding assertions failed");
  return Response.json({ totalCents, denied, transport: "in-process" });
});
router.all("*", () => Response.json({ error: "Not found" }, { status: 404 }));
export default router;
