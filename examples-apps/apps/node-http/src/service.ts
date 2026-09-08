import { Container } from "@di-framework/core/decorators";
import { Buffer } from "node:buffer";
import { request } from "node:http";

@Container()
export class ProbeService {
  constructor(private host = "node-compat-echo.wasmcloud.svc.cluster.local", private ports = { http: 8080 }) {}
  async verify() { return { http: await this.http() }; }

  http(): Promise<string> {
    return new Promise((resolve, reject) => {
      const req = request({ hostname: this.host, port: this.ports.http, method: "POST", path: "/echo", headers: { "transfer-encoding": "chunked", connection: "close" } }, (response) => {
        const chunks: Buffer[] = [];
        response.on("data", (chunk) => chunks.push(Buffer.from(chunk)));
        response.on("error", fail);
        response.on("end", () => {
          req.destroy();
          if (response.statusCode !== 200 || response.headers["transfer-encoding"] !== "chunked") reject(new Error("HTTP: expected a chunked 200 response"));
          else resolve(Buffer.concat(chunks).toString());
        });
      });
      const fail = (error: Error) => { req.destroy(); reject(new Error(`HTTP: ${error.message}`)); };
      req.on("error", fail);
      req.write("hello ");
      req.end("wasm");
    });
  }

}
