import { Container } from "@di-framework/core/decorators";
import { Messaging, WasmCloudBinding } from "@di-framework/bindings";

@WasmCloudBinding("broker")
@Container()
export class Broker extends Messaging {}
