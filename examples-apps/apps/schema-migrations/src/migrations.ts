import { Migration, createMigrationFromClass, type MigrationExecutionContext } from "@di-framework/repo";
@Migration({ version: 1, description: "create invoice ledger", binding: "verification" })
export class CreateInvoices {
  async up(ctx: MigrationExecutionContext) { await ctx.run("CREATE TABLE invoices (id TEXT PRIMARY KEY, amount INTEGER NOT NULL)"); }
}
@Migration({ version: 2, description: "add invoice status", binding: "verification" })
export class AddInvoiceStatus {
  async up(ctx: MigrationExecutionContext) { await ctx.run("ALTER TABLE invoices ADD COLUMN status TEXT NOT NULL DEFAULT 'pending'"); }
}
export const migrations = [AddInvoiceStatus, CreateInvoices].map(createMigrationFromClass);
