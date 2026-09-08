import { useContainer } from "@di-framework/core/container";
import { bindingRouter } from "../../../shared/binding-probe";
import { Objects } from "./bindings";
import { verify } from "./verify";

const service = useContainer().resolve(Objects);
export default bindingRouter("blobstore", () => verify(service));
