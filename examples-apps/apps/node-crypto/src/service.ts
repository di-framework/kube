import { Container } from "@di-framework/core/decorators";
import { Buffer } from "node:buffer";
import { createHash, createHmac, randomBytes, randomInt, randomUUID, timingSafeEqual, webcrypto } from "node:crypto";

@Container()
export class ProbeService {
  async verify() {
    const bytes = new TextEncoder().encode("hello wasm");
    const subtle = webcrypto.subtle;
    const key = await subtle.importKey("raw", new Uint8Array(16), "AES-GCM", false, ["encrypt", "decrypt"]);
    const algorithm = { name: "AES-GCM", iv: new Uint8Array(12) };
    const encrypted = await subtle.encrypt(algorithm, key, bytes);
    const decrypted = await subtle.decrypt(algorithm, key, encrypted);
    const tampered = new Uint8Array(encrypted.slice(0));
    tampered[0] ^= 1;
    let tamperRejected = false;
    try { await subtle.decrypt(algorithm, key, tampered); }
    catch { tamperRejected = true; }
    const hmacKey = await subtle.importKey("raw", new TextEncoder().encode("key"), { name: "HMAC", hash: "SHA-256" }, false, ["sign", "verify"]);
    const signature = await subtle.sign("HMAC", hmacKey, bytes);
    const hkdfKey = await subtle.importKey("raw", bytes, "HKDF", false, ["deriveBits"]);
    const derived = await subtle.deriveBits({ name: "HKDF", hash: "SHA-256", salt: new Uint8Array(16), info: new TextEncoder().encode("probe") }, hkdfKey, 128);
    const alice = await subtle.generateKey({ name: "ECDH", namedCurve: "P-256" }, true, ["deriveBits"]);
    const bob = await subtle.generateKey({ name: "ECDH", namedCurve: "P-256" }, true, ["deriveBits"]);
    const a = await subtle.deriveBits({ name: "ECDH", public: bob.publicKey }, alice.privateKey, 256);
    const b = await subtle.deriveBits({ name: "ECDH", public: alice.publicKey }, bob.privateKey, 256);
    let boundaryRejected = false;
    try { randomInt(0, 2 ** 48); }
    catch (error) { boundaryRejected = error instanceof RangeError; }
    const samples = Array.from({ length: 32 }, () => randomInt(0, 2 ** 47 + 1));
    const callbackRandom = await new Promise<number>((resolve, reject) => randomInt(1, 10, (error, value) => error ? reject(error) : resolve(value)));
    const uuid = randomUUID();
    return {
      sha256: createHash("sha256").update("hello ").update("wasm").digest("hex"),
      hmac: createHmac("sha256", "key").update(bytes).digest("hex"),
      webHmac: Buffer.from(signature).toString("hex"),
      hmacVerified: await subtle.verify("HMAC", hmacKey, signature, bytes),
      hkdf: Buffer.from(derived).toString("hex"),
      ciphertext: Buffer.from(encrypted).toString("hex"),
      decrypted: new TextDecoder().decode(decrypted), tamperRejected,
      ecdh: timingSafeEqual(new Uint8Array(a), new Uint8Array(b)),
      random: randomBytes(32).length === 32 && !timingSafeEqual(randomBytes(32), randomBytes(32)),
      uuid: /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(uuid),
      boundaryRejected,
      randomInt: samples.every((n) => Number.isSafeInteger(n) && n >= 0 && n < 2 ** 47 + 1) && new Set(samples).size > 1 && callbackRandom >= 1 && callbackRandom < 10,
    };
  }
}
