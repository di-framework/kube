import { Container } from "@di-framework/core/decorators";
import { KeyValue, WasmCloudBinding } from "@di-framework/wasmcloud";

@WasmCloudBinding("cache", { configFrom: "binding-keyvalue" })
@Container()
export class Cache extends KeyValue {}
