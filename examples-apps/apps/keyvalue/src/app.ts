import { useContainer } from "@di-framework/core/container";
import { bindingRouter } from "../../../shared/binding-probe";
import { Cache } from "./bindings";
import { verify } from "./verify";

const service = useContainer().resolve(Cache);
export default bindingRouter("keyvalue", () => verify(service));
