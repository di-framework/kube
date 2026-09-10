import { TypedRouter } from "@di-framework/http";
import { Container } from "@di-framework/core";
import { MaintenanceAudit, ScheduledMaintenance } from "./service";
export { MaintenanceAudit, ScheduledMaintenance } from "./service";

export function createMaintenance() {
  const container = new Container();
  container.setCronMode("external");
  container.register(MaintenanceAudit);
  container.register(ScheduledMaintenance);
  container.resolve(ScheduledMaintenance);
  return container;
}

export const container = createMaintenance();
const router = TypedRouter();
router.get("/health", () => Response.json({ app: "scheduled-maintenance", status: "ok" }));
router.all("*", () => Response.json({ error: "Not found" }, { status: 404 }));
export default Object.assign(router, { container });
