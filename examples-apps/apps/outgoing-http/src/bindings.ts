import { Container } from "@di-framework/core/decorators";
import { OutgoingHttp, WasmCloudBinding } from "@di-framework/bindings";

@WasmCloudBinding("http-client")
@Container()
export class HttpClient extends OutgoingHttp {}
