// Lazily loads the Go OPAQUE wasm and exposes the stepwise client API. The wasm is
// the SAME bytemare/opaque library the server uses, so messages are wire-compatible.
type RegInit = { flowId: string; request: string };
type RegFinal = { record: string; exportKey: string };
type LoginInit = { flowId: string; ke1: string };
type LoginFinal = { ke3: string; sessionKey: string };

export type OpaqueGlobals = {
  opaqueRegInit(passwordB64: string): RegInit | { error: string };
  opaqueRegFinalize(flowId: string, responseB64: string, serverIdB64: string): RegFinal | { error: string };
  opaqueLoginKE1(passwordB64: string): LoginInit | { error: string };
  opaqueLoginKE3(flowId: string, ke2B64: string, serverIdB64: string): LoginFinal | { error: string };
};

let ready: Promise<OpaqueGlobals> | null = null;

export async function initOpaque(
  newGo: () => { importObject: WebAssembly.Imports; run(i: WebAssembly.Instance): void },
  loadWasm: (importObject: WebAssembly.Imports) => Promise<WebAssembly.Instance>,
): Promise<OpaqueGlobals> {
  if (ready) return ready;
  ready = (async () => {
    const go = newGo();
    const instance = await loadWasm(go.importObject);
    go.run(instance); // non-blocking; Go sets the globals during run
    const g = globalThis as unknown as OpaqueGlobals;
    return {
      opaqueRegInit: g.opaqueRegInit.bind(globalThis),
      opaqueRegFinalize: g.opaqueRegFinalize.bind(globalThis),
      opaqueLoginKE1: g.opaqueLoginKE1.bind(globalThis),
      opaqueLoginKE3: g.opaqueLoginKE3.bind(globalThis),
    };
  })().catch((e) => {
    // Don't cache a rejected load — a transient wasm fetch/run failure would
    // otherwise wedge auth permanently. Reset so the next call retries.
    ready = null;
    throw e;
  });
  return ready;
}

function unwrap<T>(r: T | { error: string }): T {
  if (r && typeof r === "object" && "error" in r) throw new Error((r as { error: string }).error);
  return r as T;
}

export const OpaqueOps = {
  regInit: (g: OpaqueGlobals, pw: string) => unwrap(g.opaqueRegInit(pw)),
  regFinalize: (g: OpaqueGlobals, flow: string, resp: string, srvId: string) => unwrap(g.opaqueRegFinalize(flow, resp, srvId)),
  loginKE1: (g: OpaqueGlobals, pw: string) => unwrap(g.opaqueLoginKE1(pw)),
  loginKE3: (g: OpaqueGlobals, flow: string, ke2: string, srvId: string) => unwrap(g.opaqueLoginKE3(flow, ke2, srvId)),
};
