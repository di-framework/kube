import { Actor, ActorContext, ActorMethod, type ActorContext as Context } from "@di-framework/actors";

@Actor({ name: "VerificationCounter", namespace: "verification", migrations: [
  { version: 1, description: "create probe schema", up(db) { db.run("CREATE TABLE probe_schema (id INTEGER PRIMARY KEY, version INTEGER NOT NULL)"); db.run("INSERT INTO probe_schema VALUES (1, 1)"); } },
  { version: 2, description: "upgrade probe schema", up(db) { db.run("UPDATE probe_schema SET version = 2 WHERE id = 1"); } },
] })
export class VerificationCounter {
  @ActorContext() private context!: Context;
  @ActorMethod() async read(): Promise<number> { return (await this.context.storage.get<number>("count")) ?? 0; }
  @ActorMethod() async increment(amount = 1): Promise<number> {
    if (!Number.isInteger(amount) || amount < 1 || amount > 100) throw new Error("Invalid increment");
    const next = await this.read() + amount;
    await this.context.storage.set("count", next);
    return next;
  }
  @ActorMethod() async rollback(): Promise<void> { await this.context.storage.set("count", -100); throw new Error("Expected rollback probe"); }
}
