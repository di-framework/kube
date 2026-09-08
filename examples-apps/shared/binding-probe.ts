import { TypedRouter } from "@di-framework/http";

export function unwrap<T>(value: unknown): T {
  if (value && typeof value === "object" && "tag" in value) {
    const result = value as { tag: string; val?: unknown };
    if (result.tag === "err") throw new Error(`WIT operation failed: ${JSON.stringify(result.val)}`);
    if (result.tag === "ok") return result.val as T;
  }
  return value as T;
}
export function check(condition: unknown, message: string): asserts condition {
  if (!condition) throw new Error(message);
}
export function bindingRouter(app: string, verify: () => Promise<Record<string, unknown>>) {
  const router = TypedRouter();
  router.get("/", () => Response.json({ app, routes: ["/health", "POST /verify"] }));
  router.get("/health", async () => {
    await verify();
    return Response.json({ app, status: "ok" });
  });
  router.post("/verify", async () => {
    try { return Response.json({ app, ...await verify() }); }
    catch (error) {
      console.error(`${app} binding probe failed: ${error instanceof Error ? error.message : String(error)}`);
      throw error;
    }
  });
  router.all("*", () => Response.json({ error: "Not found" }, { status: 404 }));
  return router;
}
export async function* byteStream(value: string): AsyncGenerator<Uint8Array> {
  yield new TextEncoder().encode(value);
}
export async function streamText(stream: AsyncIterable<Uint8Array | number>): Promise<string> {
  const chunks: number[] = [];
  for await (const chunk of stream) {
    if (typeof chunk === "number") chunks.push(chunk);
    else chunks.push(...chunk);
    if (chunks.length > 65536) throw new Error("Probe response exceeds 64KiB");
  }
  return new TextDecoder().decode(new Uint8Array(chunks));
}
