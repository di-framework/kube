import assert from "node:assert/strict";

export interface ApiCase {
  app: "node-tls" | "config" | "secrets" | "keyvalue" | "blobstore" | "messaging" | "outgoing-http" | "postgres" | "greeter" | "catalog" | "quotes" | "node-runtime" | "node-crypto" | "node-network" | "node-http";
  liveOnly?: boolean;
  path: string;
  method?: string;
  body?: string;
  contentType?: string;
  status: number;
  expected: Record<string, unknown>;
}

export const cases: ApiCase[] = [
  { app: "node-tls", path: "/verify", liveOnly: true, status: 200, expected: { https: true, tls: true, upgrade: true, wrongNameRejected: true } },
  { app: "node-tls", path: "/health", status: 200, expected: { app: "node-tls", status: "ok" } },
  { app: "node-tls", path: "/missing", status: 404, expected: { error: "Not found" } },
  {"app": "config", "path": "/health", "method": "GET", "liveOnly": true, "status": 200, "expected": {"app": "config", "status": "ok"}},
  {"app": "config", "path": "/verify", "method": "POST", "liveOnly": true, "status": 200, "expected": {"message": "from-kubernetes-configmap", "merged": true, "missing": true}, "body": "{}"},
  {"app": "config", "path": "/missing", "method": "GET", "liveOnly": true, "status": 404, "expected": {"error": "Not found"}},
  {"app": "secrets", "path": "/health", "method": "GET", "liveOnly": true, "status": 200, "expected": {"app": "secrets", "status": "ok"}},
  {"app": "secrets", "path": "/verify", "method": "POST", "liveOnly": true, "status": 200, "expected": {"revealed": true, "missing": true}, "body": "{}"},
  {"app": "secrets", "path": "/missing", "method": "GET", "liveOnly": true, "status": 404, "expected": {"error": "Not found"}},
  {"app": "keyvalue", "path": "/health", "method": "GET", "liveOnly": true, "status": 200, "expected": {"app": "keyvalue", "status": "ok"}},
  {"app": "keyvalue", "path": "/verify", "method": "POST", "liveOnly": true, "status": 200, "expected": {"value": "from-redis", "updated": true, "deleted": true}, "body": "{}"},
  {"app": "keyvalue", "path": "/missing", "method": "GET", "liveOnly": true, "status": 404, "expected": {"error": "Not found"}},
  {"app": "blobstore", "path": "/health", "method": "GET", "liveOnly": true, "status": 200, "expected": {"app": "blobstore", "status": "ok"}},
  {"app": "blobstore", "path": "/verify", "method": "POST", "liveOnly": true, "status": 200, "expected": {"value": "from-nats-object-store", "deleted": true}, "body": "{}"},
  {"app": "blobstore", "path": "/missing", "method": "GET", "liveOnly": true, "status": 404, "expected": {"error": "Not found"}},
  {"app": "messaging", "path": "/health", "method": "GET", "liveOnly": true, "status": 200, "expected": {"app": "messaging", "status": "ok"}},
  {"app": "messaging", "path": "/verify", "method": "POST", "liveOnly": true, "status": 200, "expected": {"published": true, "replied": true}, "body": "{}"},
  {"app": "messaging", "path": "/missing", "method": "GET", "liveOnly": true, "status": 404, "expected": {"error": "Not found"}},
  {"app": "outgoing-http", "path": "/health", "method": "GET", "liveOnly": true, "status": 200, "expected": {"app": "outgoing-http", "status": "ok"}},
  {"app": "outgoing-http", "path": "/verify", "method": "POST", "liveOnly": true, "status": 200, "expected": {"status": 200, "body": "from-http-binding"}, "body": "{}"},
  {"app": "outgoing-http", "path": "/missing", "method": "GET", "liveOnly": true, "status": 404, "expected": {"error": "Not found"}},
  { app: "postgres", path: "/health", liveOnly: true, status: 200, expected: { app: "postgres", status: "ok" } },
  { app: "postgres", path: "/verify", method: "POST", body: "{}", liveOnly: true, status: 200, expected: { rejectsErrors: true, binding: "example-database", insert: true, read: true, update: true, delete: true } },
  { app: "postgres", path: "/missing", liveOnly: true, status: 404, expected: { error: "Not found" } },
  { app: "node-runtime", path: "/verify", status: 200, expected: {
    concurrentContexts: ["request-20", "request-2"], cancelled: true,
    timerArgument: "timer-argument", intervalTicks: 2, immediate: true,
  } },
  { app: "node-network", path: "/verify/ip", liveOnly: true, status: 200, expected: { tcp: "hello wasm\n", udp: "hello wasm" } },
  { app: "node-http", path: "/verify", liveOnly: true, status: 200, expected: { http: "hello wasm" } },
  { app: "node-runtime", path: "/verify", status: 200, expected: {
    settings: { message: "Hello from a seeded file", release: "5.2.12" },
    missingFile: "ENOENT", missingModule: "MODULE_NOT_FOUND", roundtrip: true, environment: true,
    buffer: "V2FzbSDinJM=", normalizedPath: "/fixtures/settings.json",
  } },
  { app: "node-runtime", path: "/verify", status: 200, expected: { context: "request-512", contextRestored: true } },
  { app: "node-runtime", path: "/verify", status: 200, expected: { timersAvailable: true } },
  { app: "node-runtime", path: "/verify", liveOnly: true, status: 200, expected: { runtime: { arch: "wasm32", cwd: "/" } } },
  { app: "node-crypto", path: "/verify", status: 200, expected: {
    // Reference vectors generated with native Node 22, not the guest implementation.
    sha256: "136f0dec77ef3c5570737642efa4c7e150d23a492a37fc5b2eff183ef7084f02",
    hmac: "19650dcf8ec51f4edeabb67aebf84549ab4e8ec863085ee67871c0be0ceb2139",
    webHmac: "19650dcf8ec51f4edeabb67aebf84549ab4e8ec863085ee67871c0be0ceb2139",
    hkdf: "d04ae61cf905392b403863170ae20f8a",
    ciphertext: "6bedb6a20f96d4f380450f2b3e874420b8931b8d02bdce1fee7c",
    decrypted: "hello wasm", tamperRejected: true, hmacVerified: true, ecdh: true,
    random: true, uuid: true, boundaryRejected: true, randomInt: true,
  } },
  { app: "node-network", path: "/verify", liveOnly: true, status: 200, expected: { tcp: "hello wasm\n", udp: "hello wasm" } },
  ...(["greeter", "catalog", "quotes", "node-runtime", "node-crypto", "node-network", "node-http"] as const).map((app) => ({
    app, path: "/health", status: 200, expected: { app, status: "ok" },
  })),
  { app: "greeter", path: "/greet/Ada", status: 200, expected: { message: "Hello, Ada!", language: "en" } },
  { app: "greeter", path: "/greet/Ada?lang=es", status: 200, expected: { message: "Hola, Ada!" } },
  { app: "greeter", path: "/greet/Ada?lang=de", status: 400, expected: { error: "Supported languages: en, es, fr" } },
  { app: "catalog", path: "/products", status: 200, expected: { currency: "USD", count: 3 } },
  { app: "catalog", path: "/products?q=MUG", status: 200, expected: { count: 1, products: [{ sku: "mug", name: "DI Framework Mug", priceCents: 1500 }] } },
  { app: "catalog", path: "/products/mug", status: 200, expected: { sku: "mug", priceCents: 1500 } },
  { app: "catalog", path: "/products/missing", status: 404, expected: { error: "Product not found" } },
  { app: "quotes", path: "/quote", method: "POST", body: JSON.stringify({ items: [{ sku: "mug", quantity: 2 }, { sku: "stickers", quantity: 3 }] }), status: 200, expected: { currency: "USD", totalCents: 4500 } },
  ...[0, -1, 1.5, 101, "2"].map((quantity): ApiCase => ({
    app: "quotes", path: "/quote", method: "POST", body: JSON.stringify({ items: [{ sku: "mug", quantity }] }),
    status: 400, expected: { error: "Each item needs a sku and an integer quantity between 1 and 100" },
  })),
  { app: "quotes", path: "/quote", method: "POST", body: '{"items":[]}', status: 400, expected: { error: "Provide between 1 and 50 line items" } },
  { app: "quotes", path: "/quote", method: "POST", body: '{"items":[{"sku":"missing","quantity":1}]}', status: 400, expected: { error: "Unknown product: missing" } },
  { app: "quotes", path: "/quote", method: "POST", body: "null", status: 400, expected: { error: "Expected an object with an items array" } },
  { app: "quotes", path: "/quote", method: "POST", body: "{", status: 400, expected: {} },
  ...(["greeter", "catalog", "quotes", "node-runtime", "node-crypto", "node-network", "node-http"] as const).map((app) => ({
    app, path: "/missing", status: 404, expected: { error: "Not found" },
  })),
];

export function requestFor(test: ApiCase, base = `http://${test.app}`): Request {
  return new Request(new URL(test.path, base), {
    method: test.method ?? "GET",
    headers: { Host: test.app, ...(test.body === undefined ? {} : { "content-type": test.contentType ?? "application/json" }) },
    body: test.body,
  });
}

export async function verify(test: ApiCase, response: Response) {
  const text = await response.text();
  assert.equal(response.status, test.status, `${test.app} ${test.path}: ${text}`);
  assert.match(response.headers.get("content-type") ?? "", /application\/json/);
  const body = JSON.parse(text);
  for (const [key, expected] of Object.entries(test.expected)) {
    assert.deepEqual(body[key], expected, `${test.app} ${test.path}: ${key}`);
  }
}
