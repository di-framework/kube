import { Container } from "@di-framework/core";
import { ContainerQueueDispatcher } from "@di-framework/queues";
import { ReceiptAudit, ReceiptProcessor } from "./processor";
export { ReceiptAudit, ReceiptProcessor } from "./processor";
export const container = new Container();
container.register(ReceiptAudit);
container.register(ReceiptProcessor);
export default new ContainerQueueDispatcher(container);
