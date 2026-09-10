import { Component, Container } from "@di-framework/core/decorators";
import { QueueHandler, type JobMetadata } from "@di-framework/queues";
export interface Receipt { id: string; amount: number; failUntilAttempt?: number; }
@Container()
export class ReceiptAudit {
  readonly receipts = new Map<string, number>();
  record(receipt: Receipt) { this.receipts.set(receipt.id, receipt.amount); }
}
@Container()
export class ReceiptProcessor {
  constructor(@Component(ReceiptAudit) readonly audit: ReceiptAudit) {}
  @QueueHandler("verification-receipts", { maxRetries: 2, backoffMs: 1, timeoutMs: 1000, concurrency: 1 })
  async process(receipt: Receipt, metadata: JobMetadata) {
    if (!receipt.id || receipt.amount < 0) throw new Error("Invalid receipt");
    if (metadata.attempts <= (receipt.failUntilAttempt ?? 0)) throw new Error("Expected retry probe");
    await Promise.resolve();
    this.audit.record(receipt);
    console.log(JSON.stringify({ receipt: receipt.id, amount: receipt.amount, attempt: metadata.attempts }));
  }
}
