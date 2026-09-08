import { Fields, Request as WasiRequest, Response as WasiResponse } from "wasi:http/types@0.3.0";
import { check, streamText, unwrap } from "../../../shared/binding-probe";
import type { HttpClient } from "./bindings";
function future(type: string, value: unknown): unknown {
  const factory = (globalThis as unknown as { wit: { Future: ((type: unknown) => { readable: unknown; writable: { write(value: unknown): void } }) & Record<string, unknown> } }).wit.Future;
  const pair = factory(factory[type]);
  pair.writable.write(value);
  return pair.readable;
}
export async function verify(client: HttpClient) {
  const created = WasiRequest.new(Fields.fromList([]), undefined,
    future("RESULT_OPTION_OTHER_ERROR_CODE", { tag: "ok", val: undefined }), undefined) as [
      { setMethod(value: unknown): void; setScheme(value: unknown): void; setAuthority(value: string): void; setPathWithQuery(value: string): void }, unknown];
  const request = created[0];
  request.setMethod({ tag: "get" });
  request.setScheme({ tag: "HTTP" });
  request.setAuthority("binding-echo:8080");
  request.setPathWithQuery("/echo");
  const response = unwrap<{ getStatusCode(): number }>(await client.send(request));
  check(response.getStatusCode() === 200, "Outgoing HTTP status should be 200");
  const [body] = WasiResponse.consumeBody(response, future("RESULT_VOID_ERROR_CODE", { tag: "ok", val: undefined })) as [AsyncIterable<Uint8Array>];
  const text = await streamText(body);
  check(text === "from-http-binding", "Outgoing HTTP body mismatch");
  return { status: 200, body: text };
}
