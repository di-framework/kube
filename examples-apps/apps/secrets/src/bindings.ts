import { Container } from "@di-framework/core/decorators";
import { Secrets, WasmCloudBinding } from "@di-framework/bindings";

@WasmCloudBinding("app-secrets", { secretFrom: "binding-secrets" })
@Container()
export class AppSecrets extends Secrets {}
