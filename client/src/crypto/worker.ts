import init, { WasmEngine } from "./wasm-pkg/crypto_core.js";
import type { CryptoRequest, CryptoResponse } from "./protocol";

let engine: WasmEngine | null = null;
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

async function ensureEngine(name: string): Promise<WasmEngine> {
  if (!ready) ready = initWasm();
  await ready;
  if (!engine) engine = new WasmEngine(name);
  return engine;
}

/** Pure dispatch — testable without a real Worker. */
export async function handleRequest(
  name: string,
  req: CryptoRequest,
): Promise<CryptoResponse> {
  try {
    const e = await ensureEngine(name);
    let result: unknown;
    switch (req.kind) {
      case "keyPackage":
        result = e.key_package_bytes();
        break;
      case "createGroup":
        e.create_group(req.groupId);
        result = null;
        break;
      case "createGroupWithCompliance":
        result = e.create_group_with_compliance(
          req.groupId,
          req.complianceKeyPackage,
        );
        break;
      case "addMember": {
        const r = e.add_member(req.groupId, req.keyPackage);
        result = { commit: r.commit, welcome: r.welcome };
        break;
      }
      case "joinFromWelcome":
        e.join_from_welcome(req.welcome);
        result = null;
        break;
      case "encrypt":
        result = e.encrypt(req.groupId, req.plaintext);
        break;
      case "decrypt":
        result = e.decrypt(req.groupId, req.message);
        break;
    }
    return { id: req.id, ok: true, result };
  } catch (err) {
    return { id: req.id, ok: false, error: String(err) };
  }
}

// Worker entry: the device name is passed once via the first message.
if (typeof self !== "undefined" && "onmessage" in self) {
  let deviceName = "device";
  self.onmessage = async (ev: MessageEvent) => {
    const data = ev.data as { name?: string } & CryptoRequest;
    if (data.name) deviceName = data.name;
    const res = await handleRequest(deviceName, data);
    (self as unknown as Worker).postMessage(res);
  };
}
