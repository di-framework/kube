import { Component, Container, Cron } from "@di-framework/core/decorators";
@Container()
export class MaintenanceAudit {
  readonly runs: string[] = [];
  record(value: string) { this.runs.push(value); }
}
@Container()
export class ScheduledMaintenance {
  constructor(@Component(MaintenanceAudit) readonly audit: MaintenanceAudit) {}
  // Acceptance invokes this job explicitly; annual scheduling avoids noisy recurring probes.
  @Cron("0 0 1 1 *", { name: "verify-maintenance", allowConcurrent: false, timeoutMs: 5000 })
  async execute() {
    await Promise.resolve();
    this.audit.record("completed");
    const result = { job: "verify-maintenance", completed: true, runs: this.audit.runs.length };
    console.log(JSON.stringify(result));
    return result;
  }
}
