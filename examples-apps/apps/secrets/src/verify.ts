import { createHash } from "node:crypto";
import { check, unwrap } from "../../../shared/binding-probe";
import type { AppSecrets } from "./bindings";
export async function verify(secrets: AppSecrets) {
  const secret = unwrap(await secrets.get("probe-token"));
  const revealed = unwrap<{ tag: string; val: string | Uint8Array }>(await secrets.reveal(secret));
  const text = revealed.tag === "string" ? String(revealed.val) : new TextDecoder().decode(revealed.val as Uint8Array);
  check(text.length === 32, "Secret value length does not match the provisioned fixture");
  let missing = false;
  try { unwrap(await secrets.get("missing-probe-key")); } catch { missing = true; }
  check(missing, "Missing secrets must fail");
  // The smoke script compares this digest with the provisioned Secret; never return its value.
  return { revealed: true, missing, digest: createHash("sha256").update(text).digest("hex") };
}
