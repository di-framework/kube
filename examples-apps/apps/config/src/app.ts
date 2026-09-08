import { useContainer } from "@di-framework/core/container";
import { bindingRouter } from "../../../shared/binding-probe";
import { AppConfig } from "./bindings";
import { verify } from "./verify";

const service = useContainer().resolve(AppConfig);
export default bindingRouter("config", () => verify(service));
