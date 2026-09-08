import { useContainer } from "@di-framework/core/container";
import { bindingRouter } from "../../../shared/binding-probe";
import { Broker } from "./bindings";
import { verify } from "./verify";

const service = useContainer().resolve(Broker);
export default bindingRouter("messaging", () => verify(service));
