import { expect, test } from "bun:test";
import { createServer as createHttpServer } from "node:http";
import { createServer as createTcpServer, type AddressInfo } from "node:net";
import { createSocket } from "node:dgram";
import { once } from "node:events";
import { ProbeService as HttpProbe } from "../apps/node-http/src/service";
import { ProbeService } from "../apps/node-network/src/service";

test("Node network probe exchanges real HTTP, TCP and UDP bytes", async () => {
  let chunked = false;
  const http = createHttpServer((request, response) => {
    chunked = request.headers["transfer-encoding"] === "chunked";
    request.on("data", (chunk) => response.write(chunk));
    request.on("end", () => response.end());
  });
  const tcp = createTcpServer((socket) => socket.on("data", (data) => socket.end(data)));
  const udp = createSocket("udp4");
  udp.on("message", (message, remote) => udp.send(message, remote.port, remote.address));
  try {
    http.listen(0, "127.0.0.1");
    await once(http, "listening");
    tcp.listen(0, "127.0.0.1");
    await once(tcp, "listening");
    udp.bind(0, "127.0.0.1");
    await once(udp, "listening");
    const service = new ProbeService("127.0.0.1", {
      tcp: (tcp.address() as AddressInfo).port,
      udp: udp.address().port,
    });
    expect(await new HttpProbe("127.0.0.1", { http: (http.address() as AddressInfo).port }).verify()).toEqual({ http: "hello wasm" });
    expect(await service.verify()).toEqual({ tcp: "hello wasm\n", udp: "hello wasm" });
    expect(chunked).toBe(true);
  } finally {
    http.closeAllConnections();
    http.close();
    tcp.close();
    udp.close();
  }
}, 20_000);
