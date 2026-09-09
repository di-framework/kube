import { ActorRuntime, ActorRpcDispatcher, SqliteActorStorage, type ActorRpcRequest } from "@di-framework/actors";
import { VerificationCounter } from "./counter";

/** Native Bun RPC companion; the wasmCloud app continues to use its generated adapter. */
export function createActorServer(directory: string, port = 0) {
  const runtime = new ActorRuntime({
    storage: new SqliteActorStorage({ baseDir: directory, fileLocking: true }),
    ownerId: "verification-owner",
    authorizationPolicy: { authorize: async (request) => request.callerId === "verification-client" },
  });
  runtime.register(VerificationCounter);
  const dispatcher = new ActorRpcDispatcher(runtime);
  const server = Bun.serve({
    hostname: "127.0.0.1", port,
    async fetch(request) {
      if (request.method !== "POST" || new URL(request.url).pathname !== "/rpc") return new Response("Not found", { status: 404 });
      return Response.json(await dispatcher.dispatch(await request.json() as ActorRpcRequest));
    },
  });
  return { runtime, url: new URL("/rpc", server.url), async close() { await server.stop(true); await runtime.clear(); } };
}
