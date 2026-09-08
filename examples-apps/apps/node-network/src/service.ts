import { Container } from "@di-framework/core/decorators";
import { Buffer } from "node:buffer";
import { createSocket } from "node:dgram";
import { createConnection } from "node:net";

// Fixed destination: the probe cannot be used to request arbitrary network resources.
const destination = "node-compat-echo.wasmcloud.svc.cluster.local";

@Container()
export class ProbeService {
  constructor(private host = destination, private ports = { tcp: 8081, udp: 8082 }) {}
  async verify() {
    // Keep both results so a failed transport does not hide the other one.
    const capture = async (run: () => Promise<string>) => {
      try { return await run(); }
      catch (error) { return { error: error instanceof Error ? error.message : String(error) }; }
    };
    return { tcp: await capture(() => this.tcp()), udp: await capture(() => this.udp()) };
  }

  private tcp(): Promise<string> {
    return new Promise((resolve, reject) => {
      const socket = createConnection({ host: this.host, port: this.ports.tcp });
      const chunks: Buffer[] = [];
      socket.on("connect", () => socket.write("hello wasm\n"));
      socket.on("data", (chunk) => chunks.push(Buffer.from(chunk)));
      socket.on("error", (error) => { socket.destroy(); reject(new Error(`TCP: ${error.message}; payload=${JSON.stringify((error as Error & { payload?: unknown }).payload)}`)); });
      socket.on("end", () => { socket.destroy(); resolve(Buffer.concat(chunks).toString()); });
    });
  }

  private udp(): Promise<string> {
    return new Promise((resolve, reject) => {
      const socket = createSocket("udp4");
      socket.on("error", (error) => { socket.close(); reject(new Error(`UDP: ${error.message}; payload=${JSON.stringify((error as Error & { payload?: unknown }).payload)}`)); });
      socket.on("message", (message) => { socket.close(); resolve(message.toString()); });
      socket.send(Buffer.from("hello wasm"), this.ports.udp, this.host);
    });
  }
}
