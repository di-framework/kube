declare module "wasi:http/types@0.3.0" {
  export const Fields: { fromList(entries: Array<[string, Uint8Array]>): unknown };
  export const Request: { "new": (headers: unknown, body: unknown, trailers: unknown, options: unknown) => unknown };
  export const Response: { consumeBody(response: unknown, completion: unknown): unknown };
}
