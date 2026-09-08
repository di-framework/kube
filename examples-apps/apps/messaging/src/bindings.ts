import { Container } from "@di-framework/core/decorators";
import { Messaging, WasmCloudBinding } from "@di-framework/wasmcloud";

@WasmCloudBinding("broker")
@Container()
export class Broker extends Messaging {}
