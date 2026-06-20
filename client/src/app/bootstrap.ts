import { createSignal } from "solid-js";
import { CryptoClient } from "../shared/lib/crypto/client";
import { ProtocolClient } from "../shared/lib/transport/protocolClient";
import { createConversationStore } from "../entities/conversation/store";
import { loadMessages, saveMessages } from "../entities/conversation/messagePersistence";
import { createConnectionStore } from "../entities/connection/store";
import { createOrchestrator, type CryptoPort } from "./orchestrator";
import { AsClient } from "../shared/api/as";
import { initOpaque, OpaqueOps } from "../shared/lib/auth/opaqueClient";
import { createAuthenticator } from "../features/authenticate/authenticate";
import { onboardDevice } from "../features/onboard-device/onboardDevice";
import { createAuthFlow } from "./authFlow";
import { createSessionStore } from "../entities/session/store";
import { createWorkspaceStore } from "../entities/workspace/store";
import { createWorkspaces } from "../features/workspaces/workspaces";
import { WorkspaceClient } from "../shared/api/workspace";
import { ConversationsClient } from "../shared/api/conversations";
import { createConversationsController } from "./conversations";
import { createConversation } from "../features/create-conversation/createConversation";
import { addMember } from "../features/add-member/addMember";
import { joinPublic } from "../features/join-public/joinPublic";
import { KTVerifier } from "../shared/lib/kt/client";
import { KTClient } from "../shared/api/kt";
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
  // Conversation messages persist to IndexedDB keyed by device name: the MLS
  // engine cannot decrypt the user's OWN sent messages from server history, so
  // the only durable copy of what the user sent is local. Hydrated on restore.
  const conversation = createConversationStore({
    load: () => loadMessages(deviceName),
    save: (byGroup) => { void saveMessages(deviceName, byGroup); },
  });
  const connection = createConnectionStore();

  // CryptoClient → CryptoPort. CryptoClient's encrypt/decrypt already match the
  // CryptoPort signature; this adapter bridges them without editing CryptoClient.
  const crypto: CryptoPort = {
    encrypt: (g, pt) => cryptoClient.encrypt(g, pt),
    decrypt: (g, msg) => cryptoClient.decrypt(g, msg),
    joinFromWelcome: (w) => cryptoClient.joinFromWelcome(w),
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
      register: async (email, username, pw) => createAuthenticator(await opaqueOps(), as).register(email, username, pw),
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

  const workspace = createWorkspaceStore();
  const wsClient = new WorkspaceClient(DS_HTTP_URL);
  const workspaces = createWorkspaces({
    client: wsClient,
    store: workspace,
    token: () => session.token() ?? "",
  });

  // Conversations controller. The logged-in user's label (email) is not known at
  // bootstrap time; main sets it after login via `setUserLabel`. The controller
  // reads it lazily through the `userLabel` getter so the optimistic echo is
  // attributed correctly.
  const [userLabel, setUserLabel] = createSignal("you");
  const convClient = new ConversationsClient(DS_HTTP_URL);
  const ktVerifier = new KTVerifier(new KTClient(DS_HTTP_URL));
  const conversations = createConversationsController({
    client: convClient,
    create: (args) =>
      createConversation(
        {
          // Adapter: ConversationsClient.putGroupInfo resolves `{ok:true}` but the
          // feature dep wants `Promise<void>`; drop the body to satisfy the shape.
          conversations: {
            create: (t, ws, body) => convClient.create(t, ws, body),
            complianceKeyPackage: (t) => convClient.complianceKeyPackage(t),
            putGroupInfo: async (t, g, bytes) => { await convClient.putGroupInfo(t, g, bytes); },
          },
          crypto: cryptoClient,
          token: () => session.token() ?? "",
        },
        { wsId: workspace.current() ?? "", ...args },
      ),
    sendText: (g, t) => orchestrator.sendText(g, t),
    track: (groupId, sinceSeq) => orchestrator.track(groupId, sinceSeq),
    // KT-verified MLS add. ConversationsClient's method signatures are token-first
    // positional, matching ConversationsPort's arg order — so no token-injecting
    // adapter is needed. The only mismatch is the return shape: addDeviceToRoster
    // and putGroupInfo resolve `{ ok: true }` but the port wants `Promise<void>`,
    // so those two are wrapped to drop the body (same pattern as createConversation
    // above). CryptoClient.addMember already resolves a plain { commit, welcome }.
    addMember: (args) =>
      addMember(
        {
          conversations: {
            addUser: (t, g, id) => convClient.addUser(t, g, id),
            keyMaterial: (t, ws, id) => convClient.keyMaterial(t, ws, id),
            addDeviceToRoster: async (t, g, d, seq) => { await convClient.addDeviceToRoster(t, g, d, seq); },
            putGroupInfo: async (t, g, bytes) => { await convClient.putGroupInfo(t, g, bytes); },
          },
          crypto: {
            addMember: (g, kp) => cryptoClient.addMember(g, kp),
            exportGroupInfo: (g) => cryptoClient.exportGroupInfo(g),
          },
          kt: { verifyIdentity: (t, id) => ktVerifier.verifyIdentity(t, id) },
          protocol: { sendCommit: orchestrator.sendCommit, sendWelcome: orchestrator.sendWelcome },
          token: () => session.token() ?? "",
        },
        args,
      ),
    // External-commit join of a public channel: fetch the published GroupInfo,
    // derive the join commit, register membership + device in the roster, and fan
    // the commit to the group. Same adapter pattern as addMember above.
    joinPublic: (args) =>
      joinPublic(
        {
          conversations: {
            getGroupInfo: (t, g) => convClient.getGroupInfo(t, g),
            join: (t, g) => convClient.join(t, g),
            addDeviceToRoster: async (t, g, d, seq) => { await convClient.addDeviceToRoster(t, g, d, seq); },
          },
          crypto: { joinByExternalCommit: (gi) => cryptoClient.joinByExternalCommit(gi) },
          protocol: { sendCommit: orchestrator.sendCommit },
          token: () => session.token() ?? "",
          deviceId: () => session.deviceId(),
        },
        args,
      ),
    conversation,
    token: () => session.token() ?? "",
    wsId: () => workspace.current() ?? "",
    userLabel: () => userLabel(),
  });

  // Restore-on-boot: probe the HttpOnly `session` cookie. If it authenticates,
  // skip the auth screen and rehydrate workspaces/conversations + WS. Empty token
  // is fine — the cookie carries auth (credentials:"include"). Any failure (e.g. a
  // 401 with no/expired cookie) means we stay on the auth screen.
  async function restoreSession(): Promise<boolean> {
    try {
      const sess = await as.session(""); // cookie-authed; throws 401 if no session
      session.restore(sess.device_id);   // skip the auth screen; keep device id for roster ops
      await conversation.hydrate();  // show locally-persisted messages immediately
      await workspaces.load();
      // Conversations load reactively once a workspace is active (see the effect
      // in main.tsx). Loading here would fire with an empty workspace id whenever
      // the user has ≠1 workspace (no auto-select), 404, and throw — which also
      // skipped the connect() below and left the app offline.
      orchestrator.connect("");      // WS via the cookie (empty token ok)
      return true;
    } catch {
      return false;                  // no/expired session → stay on auth screen
    }
  }

  return { orchestrator, conversation, connection, session, workspace, workspaces, wsClient, authFlow, conversations, setUserLabel, restoreSession };
}
