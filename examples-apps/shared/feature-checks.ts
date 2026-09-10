import assert from "node:assert/strict";
export type ProbeFetch = (path: string, init?: RequestInit) => Promise<Response>;

export async function verifyStaticSite(fetch: ProbeFetch) {
  const binary = await fetch("/assets/data.bin");
  assert.equal(binary.status, 200);
  assert.equal(binary.headers.get("content-type"), "application/octet-stream");
  assert.deepEqual(new Uint8Array(await binary.arrayBuffer()), new Uint8Array([0, 255, 128, 10]));
  const first = await fetch("/assets/index.html");
  assert.equal(first.status, 200);
  assert.match(first.headers.get("content-type") ?? "", /text\/html/);
  assert.equal(first.headers.get("cache-control"), "public, max-age=60");
  assert.match(await first.text(), /Packaged on main/);
  const etag = first.headers.get("etag");
  assert.ok(etag);
  const cached = await fetch("/assets/index.html", { headers: { "if-none-match": etag } });
  assert.equal(cached.status, 304);
  assert.equal(await cached.text(), "");
  const head = await fetch("/assets/index.html", { method: "HEAD" });
  assert.equal(head.status, 200);
  assert.equal(head.headers.get("etag"), etag);
  assert.equal(await head.text(), "");
  assert.ok(Number(head.headers.get("content-length")) > 0);
  assert.deepEqual(await (await fetch("/assets/info.json")).json(), { release: "5.3.1-candidate", packaged: true });
  assert.equal((await fetch("/assets/missing.txt")).status, 404);
  assert.ok([400, 403, 404].includes((await fetch("/assets/%2e%2e%2fpackage.json")).status));
}
