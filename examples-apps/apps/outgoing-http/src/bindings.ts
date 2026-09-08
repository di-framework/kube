import { Container } from "@di-framework/core/decorators";
import { OutgoingHttp, WasmCloudBinding } from "@di-framework/wasmcloud";

@WasmCloudBinding("http-client")
@Container()
export class HttpClient extends OutgoingHttp {}
