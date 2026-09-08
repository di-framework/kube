import { check, unwrap } from "../../../shared/binding-probe";
import type { Cache } from "./bindings";
interface Bucket {
  set(key: string, value: Uint8Array, options?: unknown): Promise<unknown>;
  get(key: string): Promise<unknown>;
  exists(key: string): Promise<unknown>;
  delete(key: string): Promise<unknown>;
  listKeys(cursor?: string): Promise<unknown>;
}
export async function verify(cache: Cache) {
  const bucket = unwrap<Bucket>(await cache.open("examples"));
  const key = `probe-${crypto.randomUUID()}`;
  try {
    unwrap(await bucket.set(key, new TextEncoder().encode("from-redis"), undefined));
    const value = new TextDecoder().decode(unwrap<Uint8Array>(await bucket.get(key)));
    check(value === "from-redis", "Redis read did not match write");
    check(unwrap<boolean>(await bucket.exists(key)), "Redis key should exist");
    unwrap(await bucket.set(key, new TextEncoder().encode("updated"), undefined));
    check(new TextDecoder().decode(unwrap<Uint8Array>(await bucket.get(key))) === "updated", "Redis update failed");
    unwrap(await bucket.delete(key));
    check(!unwrap<boolean>(await bucket.exists(key)), "Redis delete failed");
    return { value, updated: true, deleted: true };
  } finally { unwrap(await bucket.delete(key)); }
}
