import assert from "node:assert/strict";

export interface ApiCase {
  app: "greeter" | "catalog" | "quotes";
  path: string;
  method?: string;
  body?: string;
  contentType?: string;
  status: number;
  expected: Record<string, unknown>;
}

export const cases: ApiCase[] = [
  ...(["greeter", "catalog", "quotes"] as const).map((app) => ({
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
  ...(["greeter", "catalog", "quotes"] as const).map((app) => ({
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
