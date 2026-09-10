import { TypedRouter } from "@di-framework/http";
import { Container } from "@di-framework/core";
import { ContainerQueueDispatcher } from "@di-framework/queues";
import { ReceiptAudit, ReceiptProcessor } from "./processor";
export { ReceiptAudit, ReceiptProcessor } from "./processor";

export const container = new Container();
container.register(ReceiptAudit);
container.register(ReceiptProcessor);
export const dispatcher = new ContainerQueueDispatcher(container);

const router = TypedRouter();
router.get("/health", () => Response.json({ app: "durable-receipts", status: "ok" }));
router.all("*", () => Response.json({ error: "Not found" }, { status: 404 }));
export default Object.assign(router, { container, default: dispatcher });
