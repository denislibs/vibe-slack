// @vitest-environment node
import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { runInThisContext } from "node:vm";
import { initOpaque, OpaqueOps } from "./opaqueClient";

// Load Go's wasm_exec.js (a classic script that assigns globalThis.Go). The client
// package is "type": "module", so require() of a .js file is rejected by node; instead
// we run the source through vm.runInThisContext so it sets globalThis.Go.
function nodeLoaders() {
  const dir = fileURLToPath(new URL("./opaque-wasm/", import.meta.url));
  const wasmExecPath = dir + "wasm_exec.js";
  const src = readFileSync(wasmExecPath, "utf8");
  runInThisContext(src, { filename: wasmExecPath });
  const wasmBytes = readFileSync(dir + "opaque.wasm");
  const newGo = () => new (globalThis as any).Go();
  const loadWasm = async (importObject: WebAssembly.Imports) => {
    const { instance } = await WebAssembly.instantiate(wasmBytes, importObject);
    return instance;
  };
  return { newGo, loadWasm };
}

describe("opaque wasm smoke", () => {
  it("loads and produces independent register/login flows", async () => {
    const { newGo, loadWasm } = nodeLoaders();
    const g = await initOpaque(newGo, loadWasm);
    const b64pw = Buffer.from("pw").toString("base64");
    const r1 = OpaqueOps.regInit(g, b64pw);
    const r2 = OpaqueOps.loginKE1(g, b64pw);
    expect(r1.flowId).not.toBe(r2.flowId);
    expect(r1.request.length).toBeGreaterThan(0);
    expect(r2.ke1.length).toBeGreaterThan(0);
  });
});
