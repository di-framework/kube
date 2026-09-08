import { Container } from "@di-framework/core/decorators";
import { Postgres, WasmCloudBinding } from "@di-framework/wasmcloud";

@WasmCloudBinding("example-database", { config: { database: "examples" }, secretFrom: "examples-postgres-binding" })
@Container()
export class ExampleDatabase extends Postgres {
  async execute(sql: string): Promise<void> {
    const result = await this.queryBatch(sql);
    // Raw WIT guests may return a result variant instead of throwing an error.
    if (result && typeof result === "object" && "tag" in result && result.tag === "err") {
      throw new Error("PostgreSQL rejected the batch");
    }
  }
}
