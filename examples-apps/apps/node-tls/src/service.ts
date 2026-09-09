import { Container } from "@di-framework/core/decorators";
import { Buffer } from "node:buffer";
import { get, Agent } from "node:https";
import { connect as tcpConnect } from "node:net";
import { connect } from "node:tls";

const host = "example.com";
const timeout = 10_000;

@Container()
export class ProbeService {
  async verify() {
    const [https, tls, upgrade, wrongNameRejected] = await Promise.all([
      this.https(), this.tls(false), this.tls(true), this.rejectWrongName(),
    ]);
    return { https, tls, upgrade, wrongNameRejected };
  }

  https(servername = host): Promise<true> {
    return new Promise((resolve, reject) => {
      const agent = new Agent({ servername });
      const req = get(`https://${host}/`, { agent, headers: { connection: "close" } }, (res) => {
        const chunks: Buffer[] = [];
        res.on("data", (chunk) => chunks.push(Buffer.from(chunk)));
        res.on("error", fail);
        res.on("end", () => {
          clearTimeout(deadline);
          req.destroy();
          if (res.statusCode === 200 && Buffer.concat(chunks).toString().includes("Example Domain")) resolve(true);
          else reject(new Error(`Unexpected HTTPS response: ${res.statusCode}`));
        });
      });
      const fail = (error: Error) => { clearTimeout(deadline); req.destroy(); reject(error); };
      const deadline = setTimeout(() => fail(new Error("HTTPS probe timed out")), timeout);
      req.on("error", fail);
    });
  }

  tls(upgrade: boolean): Promise<true> {
    return new Promise((resolve, reject) => {
      // Supplying an existing TCP socket exercises the handoff used by STARTTLS.
      // This endpoint speaks TLS immediately; there is no SMTP/IMAP negotiation.
      const tcp = upgrade ? tcpConnect({ host, port: 443 }) : undefined;
      const socket = connect(tcp ? { socket: tcp, servername: host } : { host, port: 443, servername: host });
      const chunks: Buffer[] = [];
      const fail = (error: Error) => { clearTimeout(deadline); socket.destroy(); tcp?.destroy(); reject(error); };
      const deadline = setTimeout(() => fail(new Error("TLS probe timed out")), timeout);
      tcp?.on("error", fail);
      socket.on("error", fail);
      socket.on("secureConnect", () => {
        if (!socket.authorized) return fail(new Error("TLS peer was not authenticated"));
        socket.write(`GET / HTTP/1.1\r\nHost: ${host}\r\nConnection: close\r\n\r\n`);
      });
      socket.on("data", (chunk) => chunks.push(Buffer.from(chunk)));
      socket.on("end", () => {
        clearTimeout(deadline);
        socket.destroy();
        const response = Buffer.concat(chunks).toString();
        if (/^HTTP\/1\.[01] 200 /.test(response) && response.includes("Example Domain")) resolve(true);
        else reject(new Error("Unexpected raw TLS response"));
      });
    });
  }

  async rejectWrongName(): Promise<true> {
    try {
      await this.https("mismatch.invalid");
    } catch (error) {
      // DNS failures and timeouts are failures, not evidence of authentication.
      const detail = error instanceof Error ? `${error.name}: ${error.message}` : String(error);
      if (/certificate|hostname|altname|handshake.?failure|unrecognized.?name/i.test(detail)) return true;
      throw error;
    }
    throw new Error("HTTPS accepted an incorrect TLS server name");
  }
}
