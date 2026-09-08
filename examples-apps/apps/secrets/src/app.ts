import { useContainer } from "@di-framework/core/container";
import { bindingRouter } from "../../../shared/binding-probe";
import { AppSecrets } from "./bindings";
import { verify } from "./verify";

const service = useContainer().resolve(AppSecrets);
export default bindingRouter("secrets", () => verify(service));
