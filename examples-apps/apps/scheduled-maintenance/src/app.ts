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
export default { container };
