import { check, unwrap } from "../../../shared/binding-probe";
import type { AppConfig } from "./bindings";
export async function verify(config: AppConfig) {
  const message = unwrap<string>(await config.get("message"));
  const all = Object.fromEntries(unwrap<Array<[string, string]>>(await config.getAll()));
  const missing = unwrap<string | undefined>(await config.get("missing-probe-key"));
  check(message === "from-kubernetes-configmap", "ConfigMap must override inline config");
  check(all.inlineOnly === "inline-value" && all.message === message, "getAll must return merged configuration");
  check(missing == null, "Missing config should be absent");
  return { message, merged: true, missing: true };
}
