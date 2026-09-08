import { byteStream, check, streamText, unwrap } from "../../../shared/binding-probe";
import type { Objects } from "./bindings";
interface Container {
  name(): Promise<unknown>;
  writeData(name: string, body: AsyncIterable<Uint8Array>): Promise<unknown>;
  // QuickJS 0.4.4-di.2 lowers these u64 offsets from Numbers; the probe uses small offsets.
  getData(name: string, start: number, end: number): Promise<unknown>;
  hasObject(name: string): Promise<unknown>;
  deleteObject(name: string): Promise<unknown>;
}
export async function verify(objects: Objects) {
  // A fixed container with unique objects keeps probes repeatable and concurrent.
  let container: Container;
  try { container = unwrap<Container>(await objects.getContainer("binding-probes")); }
  catch {
    try { container = unwrap<Container>(await objects.createContainer("binding-probes")); }
    catch { container = unwrap<Container>(await objects.getContainer("binding-probes")); }
  }
  const key = `probe-${crypto.randomUUID()}`;
  const value = "from-nats-object-store";
  try {
    unwrap(await container.writeData(key, byteStream(value)));
    check(unwrap<boolean>(await container.hasObject(key)), "Blob should exist");
    const body = unwrap<AsyncIterable<Uint8Array>>(await container.getData(key, 0, value.length));
    check(await streamText(body) === value, "Blob read did not match write");
    unwrap(await container.deleteObject(key));
    check(!unwrap<boolean>(await container.hasObject(key)), "Blob delete failed");
    return { value, deleted: true };
  } finally { try { unwrap(await container.deleteObject(key)); } catch { /* Already removed by the probe. */ } }
}
