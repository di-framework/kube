import { useContainer } from "@di-framework/core/container";
import { bindingRouter } from "../../../shared/binding-probe";
import { HttpClient } from "./bindings";
import { verify } from "./verify";

const service = useContainer().resolve(HttpClient);
export default bindingRouter("outgoing-http", () => verify(service));
