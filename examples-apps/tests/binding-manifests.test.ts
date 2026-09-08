import { expect, test } from "bun:test";
import { hostInterfacesFromRequirements } from "../node_modules/@di-framework/cli-plugin-wasmcloud/dist/host-interface";
import type { WitRequirement } from "../node_modules/@di-framework/cli-plugin-wasmcloud/dist/wit";

const http: WitRequirement = {
  package: "wasi:http", version: "0.3.0", interfaces: ["handler"],
  direction: "export", source: "http-adapter",
};

test("ConfigMap overlays reach an unnamed config binding", () => {
  const [entry] = hostInterfacesFromRequirements([
    { package: "wasi:config", version: "0.2.0-rc.1", interfaces: ["store"], direction: "import", source: "AppConfig" },
  ], {}, [{ name: "app-config", className: "AppConfig", config: { message: "inline" }, configFrom: "binding-config" }]);
  expect(entry?.name).toBeUndefined();
  expect(entry?.config).toEqual({ message: "inline" });
  expect(entry?.configFrom).toEqual([{ name: "binding-config" }]);
});

test("HTTP ingress and outgoing client share one valid host declaration", () => {
  const entries = hostInterfacesFromRequirements([
    http,
    { ...http, interfaces: ["client"], direction: "import", source: "HttpClient" },
  ], { httpHost: "outgoing-http" }, [{ name: "http-client", className: "HttpClient", configFrom: "outgoing-policy" }]);
  expect(entries).toHaveLength(1);
  expect(entries[0]?.interfaces).toEqual(["handler"]);
  // wasi:http/client is linked by the host core; the ingress advertises handler.
  expect(entries[0]?.config).toEqual({ host: "outgoing-http" });
  expect(entries[0]?.configFrom).toEqual([{ name: "outgoing-policy" }]);
});

test("key-value host requirements match the runtime's advertised interfaces", () => {
  const requirement: WitRequirement = {
    package: "wasmcloud:keyvalue", version: "0.2.0", interfaces: ["store", "atomics", "cas", "batch", "types"],
    direction: "import", instanceName: "cache", source: "Cache",
  };
  const [entry] = hostInterfacesFromRequirements([requirement], {}, [{ name: "cache", className: "Cache", configFrom: "binding-keyvalue" }]);
  expect(entry?.name).toBeUndefined();
  expect(entry?.interfaces).toEqual(["store", "atomics", "cas", "batch"]);
  expect(entry?.configFrom).toEqual([{ name: "binding-keyvalue" }]);
  // The component still imports resource types; only the host advertisement differs.
  expect(requirement.interfaces).toContain("types");
});
