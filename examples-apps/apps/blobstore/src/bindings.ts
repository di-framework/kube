import { Container } from "@di-framework/core/decorators";
import { Blobstore, WasmCloudBinding } from "@di-framework/wasmcloud";

@WasmCloudBinding("objects", { configFrom: "binding-blobstore" })
@Container()
export class Objects extends Blobstore {}
