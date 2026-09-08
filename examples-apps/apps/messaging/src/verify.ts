import { byteStream, check, streamText, unwrap } from "../../../shared/binding-probe";
import type { Broker } from "./bindings";
export async function verify(broker: Broker) {
  const token = crypto.randomUUID();
  unwrap(await broker.publish({ subject: "bindings.publish", body: byteStream(token), replyTo: undefined }));
  const response = unwrap<{ body: AsyncIterable<Uint8Array> }>(await broker.request("bindings.echo", byteStream(token), 5000));
  const reply = JSON.parse(await streamText(response.body));
  check(reply.token === token && reply.published === token, "NATS responder did not observe publish and request");
  return { published: true, replied: true };
}
