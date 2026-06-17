import { CryptoClient } from "../shared/lib/crypto/client";
import { ProtocolClient } from "../shared/lib/transport/protocolClient";
import { createConversationStore } from "../entities/conversation/store";
import { createConnectionStore } from "../entities/connection/store";
import { createOrchestrator, type CryptoPort } from "./orchestrator";
import { AsClient } from "../shared/api/as";
import { initOpaque, OpaqueOps } from "../shared/lib/auth/opaqueClient";
import { createAuthenticator } from "../features/authenticate/authenticate";
import { onboardDevice } from "../features/onboard-device/onboardDevice";
import { createAuthFlow } from "./authFlow";
import { createSessionStore } from "../entities/session/store";
import { DS_HTTP_URL } from "../shared/config/env";
import wasmUrl from "../shared/lib/auth/opaque-wasm/opaque.wasm?url";
import wasmExecUrl from "../shared/lib/auth/opaque-wasm/wasm_exec.js?url";

// Loads the Go `wasm_exec.js` runtime. It is a CLASSIC script that assigns
// `globalThis.Go`, so we inject it via a <script> tag (a dynamic `import()` of a
// classic script is not reliably executed as a side effect under Vite/ESM).
// Browser-only; guard for tests.
function loadWasmExec(): Promise<void> {
  if (typeof (globalThis as any).Go === "function") return Promise.resolve();
  if (typeof document === "undefined") {
    return Promise.reject(new Error("wasm_exec.js requires a browser document"));
  }
  return new Promise<void>((resolve, reject) => {
    const existing = document.querySelector<HTMLScriptElement>(`script[data-wasm-exec="1"]`);
    if (existing) {
      existing.addEventListener("load", () => resolve(), { once: true });
      existing.addEventListener("error", () => reject(new Error("failed to load wasm_exec.js")), { once: true });
      return;
    }
    const script = document.createElement("script");
    script.src = wasmExecUrl;
    script.dataset.wasmExec = "1";
    script.addEventListener("load", () => resolve(), { once: true });
    script.addEventListener("error", () => reject(new Error("failed to load wasm_exec.js")), { once: true });
    document.head.appendChild(script);
  });
}

async function loadOpaqueInBrowser() {
  await loadWasmExec(); // defines globalThis.Go
  const GoCtor = (globalThis as any).Go;
  const makeGo = () => new GoCtor();
  const loadWasm = async (importObject: WebAssembly.Imports) => {
    const res = await fetch(wasmUrl);
    const { instance } = await WebAssembly.instantiate(await res.arrayBuffer(), importObject);
    return instance;
  };
  return initOpaque(makeGo, loadWasm);
}

export function bootstrap(deviceName: string) {
  const cryptoWorker = new Worker(new URL("../shared/lib/crypto/worker.ts", import.meta.url), { type: "module" });
  const protocolWorker = new Worker(new URL("../shared/lib/transport/protocol.worker.ts", import.meta.url), { type: "module" });

  const cryptoClient = new CryptoClient(cryptoWorker, deviceName);
  const protocol = new ProtocolClient(protocolWorker);
  const conversation = createConversationStore();
  const connection = createConnectionStore();

  // CryptoClient → CryptoPort. CryptoClient's encrypt/decrypt already match the
  // CryptoPort signature; this adapter bridges them without editing CryptoClient.
  const crypto: CryptoPort = {
    encrypt: (g, pt) => cryptoClient.encrypt(g, pt),
    decrypt: (g, msg) => cryptoClient.decrypt(g, msg),
  };

  const orchestrator = createOrchestrator({ protocol, crypto, conversation, connection });

  const session = createSessionStore();
  const as = new AsClient(DS_HTTP_URL);
  const opaqueOps = async () => {
    const g = await loadOpaqueInBrowser();
    return {
      regInit: (pw: string) => OpaqueOps.regInit(g, pw),
      regFinalize: (f: string, r: string, s: string) => OpaqueOps.regFinalize(g, f, r, s),
      loginKE1: (pw: string) => OpaqueOps.loginKE1(g, pw),
      loginKE3: (f: string, k: string, s: string) => OpaqueOps.loginKE3(g, f, k, s),
    };
  };
  const authFlow = createAuthFlow({
    authenticator: {
      register: async (email, pw) => createAuthenticator(await opaqueOps(), as).register(email, pw),
      login: async (email, pw) => createAuthenticator(await opaqueOps(), as).login(email, pw),
    },
    onboard: (token) => onboardDevice({
      crypto: { signingPublicKey: () => cryptoClient.signingPublicKey(), keyPackage: () => cryptoClient.keyPackage() },
      enroll: { enrollDevice: (t, pub, label, kps) => as.enrollDevice(t, pub, label, kps) },
      token, label: "web", poolSize: 5,
    }),
    connect: (token) => protocol.connect(token),
    session,
  });

  return { orchestrator, conversation, connection, session, authFlow };
}
