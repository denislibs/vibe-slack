import init, { WasmEngine } from "./wasm-pkg/crypto_core.js";
import type { CryptoRequest, CryptoResponse } from "./protocol";
import { loadCryptoState, saveCryptoState } from "./persistence";

let ready: Promise<void> | null = null;

// Minimal, browser-safe view of the node `process` global so we can detect the
// node/vitest runtime without pulling in `@types/node` (this is a browser
// tsconfig: DOM + WebWorker libs only).
declare const process:
  | { versions?: { node?: string } }
  | undefined;

function isNode(): boolean {
  return (
    typeof process !== "undefined" &&
    process?.versions?.node != null
  );
}

async function initWasm(): Promise<void> {
  if (isNode()) {
    // Node/vitest: the generated `--target web` init defaults to
    // `fetch(new URL('crypto_core_bg.wasm', import.meta.url))`, which fails in
    // node. Read the wasm bytes from disk and hand them to init instead. The
    // generated `__wbg_init` accepts `{ module_or_path: InitInput }` where
    // InitInput includes BufferSource, routing to `WebAssembly.instantiate`.
    //
    // The `node:` modules are resolved through indirection so that the browser
    // tsconfig (which lacks `@types/node`) does not try to type-check them.
    const dynImport = (s: string): Promise<any> =>
      import(/* @vite-ignore */ s);
    const { readFile } = await dynImport("node:fs/promises");
    const { fileURLToPath } = await dynImport("node:url");
    const wasmPath = fileURLToPath(
      new URL("./wasm-pkg/crypto_core_bg.wasm", import.meta.url),
    );
    const bytes = await readFile(wasmPath);
    await init({ module_or_path: bytes });
    return;
  }
  // Browser: default fetch-based init resolves the wasm relative to this module.
  await init();
}

/** Pure dispatch over a specific engine — shared by every dispatcher. */
export async function dispatchTo(
  engine: WasmEngine,
  req: CryptoRequest,
): Promise<CryptoResponse> {
  try {
    let result: unknown;
    switch (req.kind) {
      case "keyPackage":
        result = engine.key_package_bytes();
        break;
      case "signingPublicKey":
        result = engine.signing_public_key();
        break;
      case "createGroup":
        engine.create_group(req.groupId);
        result = null;
        break;
      case "createGroupWithCompliance":
        result = engine.create_group_with_compliance(
          req.groupId,
          req.complianceKeyPackage,
        );
        break;
      case "addMember": {
        const r = engine.add_member(req.groupId, req.keyPackage);
        result = { commit: r.commit, welcome: r.welcome };
        break;
      }
      case "removeMember":
        result = engine.remove_member(req.groupId, req.leafIndex);
        break;
      case "joinFromWelcome":
        engine.join_from_welcome(req.welcome);
        result = null;
        break;
      case "exportGroupInfo":
        result = engine.export_group_info(req.groupId);
        break;
      case "joinByExternalCommit":
        result = engine.join_by_external_commit(req.groupInfo);
        break;
      case "encrypt":
        result = engine.encrypt(req.groupId, req.plaintext);
        break;
      case "decrypt":
        result = engine.decrypt(req.groupId, req.message);
        break;
    }
    return { id: req.id, ok: true, result };
  } catch (err) {
    return { id: req.id, ok: false, error: String(err) };
  }
}

/**
 * Create a dispatcher that owns its own independent `WasmEngine`. The WASM
 * module is initialised once per process (shared, memoized), but each
 * dispatcher's engine instance has fully independent OpenMLS state.
 */
// Ops that never change engine state — skip the persist round-trip for these.
const READ_ONLY_KINDS = new Set(["keyPackage", "signingPublicKey", "exportGroupInfo"]);

export function createDispatcher(name: string) {
  let engine: WasmEngine | null = null;
  let initEngine: Promise<WasmEngine> | null = null;

  async function getEngine(): Promise<WasmEngine> {
    if (engine) return engine;
    if (!initEngine) {
      initEngine = (async () => {
        if (!ready) ready = initWasm();
        await ready;
        // Loading may reject if no storage backend is available (e.g. a node
        // runtime without IndexedDB); treat that like "no saved state".
        let saved: Uint8Array | null = null;
        try {
          saved = await loadCryptoState(name);
        } catch {
          saved = null;
        }
        if (saved) {
          try {
            return WasmEngine.restore(saved);
          } catch {
            // Corrupt/incompatible blob: fall back to a fresh engine.
            return new WasmEngine(name);
          }
        }
        return new WasmEngine(name);
      })();
    }
    engine = await initEngine;
    return engine;
  }

  async function handleRequest(req: CryptoRequest): Promise<CryptoResponse> {
    const e = await getEngine();
    const res = await dispatchTo(e, req);
    if (res.ok && !READ_ONLY_KINDS.has(req.kind)) {
      try {
        await saveCryptoState(name, e.export_state());
      } catch {
        // Persistence failure must not break the live operation.
      }
    }
    return res;
  }
  return { handleRequest };
}

// A single lazily-created default dispatcher backs the module-level
// `handleRequest(name, req)`, preserving the historical module-global engine
// behavior (the engine is keyed by the first name seen).
let defaultDispatcher: ReturnType<typeof createDispatcher> | null = null;

/** Pure dispatch — testable without a real Worker. */
export async function handleRequest(
  name: string,
  req: CryptoRequest,
): Promise<CryptoResponse> {
  if (!defaultDispatcher) defaultDispatcher = createDispatcher(name);
  return defaultDispatcher.handleRequest(req);
}

// Worker entry: the device name is passed once via the first message.
if (typeof self !== "undefined" && "onmessage" in self) {
  let dispatcher: ReturnType<typeof createDispatcher> | null = null;
  self.onmessage = async (ev: MessageEvent) => {
    const data = ev.data as { name?: string } & CryptoRequest;
    if (!dispatcher) dispatcher = createDispatcher(data.name ?? "device");
    const res = await dispatcher.handleRequest(data);
    (self as unknown as Worker).postMessage(res);
  };
}
