# Frontend Scaffold Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up the SolidJS client scaffold — Vite + Feature-Sliced Design layers, a ui-kit, a typed `window`-hub message bus, a protocol worker (WebSocket client against the DS frame contract), the relocated crypto worker, and a clean-architecture orchestration layer — proven by a transport demo where a message flows DS→protocol-worker→bus→crypto-worker(decrypt)→store→UI.

**Architecture:** Three threads. The main (window) thread runs SolidJS (FSD: app/pages/widgets/features/entities/shared) and is the bus hub — it relays typed `postMessage` envelopes between two workers (protocol = WebSocket↔DS; crypto = WASM MLS) and orchestrates inbound ciphertext→decrypt→store and outbound intent→encrypt→send. All business logic lives in `app`/`features`/`entities`; `shared/ui` (ui-kit) and widgets/pages are presentation only. Worker-facing logic is extracted into pure, testable units (a `WebSocketLike`-driven connection, request/response RPC) so it tests in vitest without real Workers.

**Tech Stack:** SolidJS, Vite + vite-plugin-solid, TypeScript, Vitest + jsdom + @solidjs/testing-library, the existing WASM crypto worker. Reuses `client/src/crypto/` (relocated to `shared/lib/crypto`).

**Spec:** `docs/superpowers/specs/2026-06-17-frontend-scaffold-design.md`. OPAQUE-client, KT-client, client-side MLS group flows, and real UI/UX + animations (subproject 4) are out of scope.

**Existing state:** `client/` has `src/crypto/` (CryptoClient + worker + protocol + wasm-pkg, vitest tests, node env), `package.json` (typescript, vitest), `tsconfig.json`, `vitest.config.ts` (environment: node). This plan adds the SolidJS app around it and relocates crypto under FSD.

**Config risks (flagged inline):** (1) vitest + solid needs vite-plugin-solid + a resolve condition; the existing crypto tests must keep passing after the config change. (2) Vite worker bundling (`new Worker(new URL(...), {type:'module'})`) works in `vite build`/dev but vitest doesn't run real module Workers — so all worker LOGIC is pure and tested directly; real-Worker instantiation is verified by `vite build` + the bootstrap, not by spinning Workers in vitest. No Docker needed for any task here.

---

## File Structure

```
client/
  vite.config.ts                       # Vite + solid plugin + worker config
  vitest.config.ts                     # jsdom + solid + resolve conditions
  index.html                           # Vite entry
  src/
    main.tsx                           # mounts <App/>
    app/
      App.tsx                          # root component (providers + router shell)
      orchestrator.ts                  # wires bus↔crypto↔protocol↔stores (business logic, no UI)
      bootstrap.ts                     # instantiates real workers + clients (browser only)
    pages/
      chat/ChatPage.tsx
    widgets/
      message-list/MessageList.tsx
      composer/Composer.tsx
    features/
      send-message/sendMessage.ts
      sync-conversation/syncConversation.ts
      connect-transport/connectTransport.ts
    entities/
      conversation/store.ts            # Solid store: messages by group, cursor, status
      connection/store.ts              # Solid store: online/connecting/offline
    shared/
      ui/                              # ui-kit: Button.tsx, Spinner.tsx, TextField.tsx
      lib/
        rpc/workerRpc.ts               # generic request/response over a worker-like
        bus/                           # (covered by clients; the window-hub wiring lives in orchestrator)
        transport/
          connection.ts                # pure ProtocolConnection (WebSocketLike-driven)
          protocolClient.ts            # main-thread proxy over the protocol worker
          protocol.worker.ts           # thin worker shell: real WS + postMessage → connection
        crypto/                        # relocated from src/crypto (CryptoClient, worker, protocol, wasm-pkg)
      api/
        ds.ts                          # DS WS frame types (send/ack/sync/sent/message/error)
      config/
        env.ts
```

---

## Milestone 1 — Tooling, FSD skeleton, ui-kit

### Task 1: Vite + Solid + vitest(jsdom) setup; app mounts; crypto tests still pass

**Files:**
- Create: `client/vite.config.ts`, `client/index.html`, `client/src/main.tsx`, `client/src/app/App.tsx`, `client/src/app/App.test.tsx`
- Modify: `client/package.json` (deps + scripts), `client/vitest.config.ts`, `client/tsconfig.json` (jsx solid)

- [ ] **Step 1: Add deps + scripts**

Run: `cd client && npm install -D vite vite-plugin-solid @solidjs/testing-library jsdom && npm install solid-js`
Then set `client/package.json` scripts:
```json
{
  "scripts": {
    "dev": "vite",
    "build": "vite build",
    "test": "vitest run",
    "test:watch": "vitest"
  }
}
```

- [ ] **Step 2: Write the failing test**

`client/src/app/App.test.tsx`:
```tsx
// @vitest-environment jsdom
import { render } from "@solidjs/testing-library";
import { describe, it, expect } from "vitest";
import { App } from "./App";

describe("App", () => {
  it("renders the app shell", () => {
    const { getByText } = render(() => <App />);
    expect(getByText("Messenger")).toBeTruthy();
  });
});
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd client && npx vitest run src/app/App.test.tsx`
Expected: FAIL — cannot find `./App` (and/or solid JSX not configured).

- [ ] **Step 4: Configure + implement**

`client/vite.config.ts`:
```ts
import { defineConfig } from "vite";
import solid from "vite-plugin-solid";

export default defineConfig({
  plugins: [solid()],
  worker: { format: "es" },
});
```

`client/vitest.config.ts` (replace the node-env config; jsdom works for both DOM and the existing crypto tests which run on Node APIs available under vitest):
```ts
import { defineConfig } from "vitest/config";
import solid from "vite-plugin-solid";

export default defineConfig({
  plugins: [solid()],
  test: {
    environment: "jsdom",
    include: ["src/**/*.test.{ts,tsx}"],
  },
  resolve: {
    conditions: ["development", "browser"],
  },
});
```
NOTE (config risk): the `resolve.conditions` line is what makes solid's reactivity work under test. If solid components render but reactivity/effects misbehave in tests, this condition is the usual fix. If the existing crypto worker test (`worker.test.ts`, which `import init`s the wasm via node fs) breaks under jsdom, give it a per-file `// @vitest-environment node` pragma at the top (jsdom and node both run under Node in vitest, so `node:fs` works either way — only add the pragma if a real incompatibility appears). Verify both the new App test AND all `src/crypto/*.test.ts` pass after this change.

`client/tsconfig.json` — ensure JSX is solid: add to compilerOptions:
```json
    "jsx": "preserve",
    "jsxImportSource": "solid-js",
```
(keep the existing strict/ESNext options.)

`client/index.html`:
```html
<!doctype html>
<html lang="en">
  <head><meta charset="utf-8" /><title>Messenger</title></head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
```

`client/src/app/App.tsx`:
```tsx
import type { Component } from "solid-js";

export const App: Component = () => {
  return <div data-testid="app-shell">Messenger</div>;
};
```

`client/src/main.tsx`:
```tsx
import { render } from "solid-js/web";
import { App } from "./app/App";

const root = document.getElementById("root");
if (root) {
  render(() => <App />, root);
}
```

- [ ] **Step 5: Verify + commit**

Run: `cd client && npx vitest run` (App test + all crypto tests green) and `cd client && npx vite build` (production build succeeds, bundling the wasm worker).
```bash
git -C /Users/denisurevic/Documents/slack add client/vite.config.ts client/vitest.config.ts client/tsconfig.json client/index.html client/src/main.tsx client/src/app client/package.json client/package-lock.json
git -C /Users/denisurevic/Documents/slack commit -m "feat(client): Vite + SolidJS + vitest(jsdom) scaffold; app shell"
```

---

### Task 2: ui-kit primitives (shared/ui) — pure presentational components

**Files:**
- Create: `client/src/shared/ui/Button.tsx`, `client/src/shared/ui/Spinner.tsx`, `client/src/shared/ui/index.ts`, `client/src/shared/ui/Button.test.tsx`

- [ ] **Step 1: Write the failing test**

`client/src/shared/ui/Button.test.tsx`:
```tsx
// @vitest-environment jsdom
import { render, fireEvent } from "@solidjs/testing-library";
import { describe, it, expect, vi } from "vitest";
import { Button } from "./Button";

describe("Button (ui-kit)", () => {
  it("renders label and fires onClick", () => {
    const onClick = vi.fn();
    const { getByRole } = render(() => <Button onClick={onClick}>Send</Button>);
    const btn = getByRole("button");
    expect(btn.textContent).toBe("Send");
    fireEvent.click(btn);
    expect(onClick).toHaveBeenCalledOnce();
  });

  it("is disabled when disabled prop set", () => {
    const { getByRole } = render(() => <Button disabled>X</Button>);
    expect((getByRole("button") as HTMLButtonElement).disabled).toBe(true);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd client && npx vitest run src/shared/ui/Button.test.tsx`
Expected: FAIL — cannot find `./Button`.

- [ ] **Step 3: Write minimal implementation**

`client/src/shared/ui/Button.tsx`:
```tsx
import type { Component, JSX } from "solid-js";

export type ButtonProps = {
  children: JSX.Element;
  onClick?: () => void;
  disabled?: boolean;
  type?: "button" | "submit";
};

// Pure presentational button. No business logic; ui-kit only.
export const Button: Component<ButtonProps> = (props) => {
  return (
    <button type={props.type ?? "button"} disabled={props.disabled} onClick={() => props.onClick?.()}>
      {props.children}
    </button>
  );
};
```

`client/src/shared/ui/Spinner.tsx`:
```tsx
import type { Component } from "solid-js";

export const Spinner: Component = () => <span role="status" aria-label="loading">…</span>;
```

`client/src/shared/ui/index.ts`:
```ts
export { Button } from "./Button";
export { Spinner } from "./Spinner";
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd client && npx vitest run src/shared/ui/Button.test.tsx`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add client/src/shared/ui
git -C /Users/denisurevic/Documents/slack commit -m "feat(client): ui-kit primitives (Button, Spinner)"
```

---

## Milestone 2 — Bus, crypto relocation, protocol worker

### Task 3: Generic worker RPC (shared/lib/rpc)

**Files:**
- Create: `client/src/shared/lib/rpc/workerRpc.ts`, `client/src/shared/lib/rpc/workerRpc.test.ts`

The window-hub bus is built from two pieces: request/response RPC (this task) and an event stream (Task 5's ProtocolClient). This RPC generalizes the pattern `CryptoClient` already uses.

- [ ] **Step 1: Write the failing test**

`client/src/shared/lib/rpc/workerRpc.test.ts`:
```ts
import { describe, it, expect } from "vitest";
import { createWorkerRpc, type Postable } from "./workerRpc";

// Fake worker that echoes a deterministic response per request id.
class FakeWorker implements Postable {
  onmessage: ((ev: MessageEvent) => void) | null = null;
  postMessage(msg: any) {
    const res = msg.kind === "ping"
      ? { id: msg.id, ok: true, result: "pong" }
      : { id: msg.id, ok: false, error: "unsupported" };
    queueMicrotask(() => this.onmessage?.({ data: res } as MessageEvent));
  }
}

describe("workerRpc", () => {
  it("resolves a call with the matching response", async () => {
    const rpc = createWorkerRpc(new FakeWorker());
    await expect(rpc.call("ping", {})).resolves.toBe("pong");
  });
  it("rejects on error response", async () => {
    const rpc = createWorkerRpc(new FakeWorker());
    await expect(rpc.call("nope", {})).rejects.toThrow("unsupported");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd client && npx vitest run src/shared/lib/rpc/workerRpc.test.ts`
Expected: FAIL — cannot find `./workerRpc`.

- [ ] **Step 3: Write minimal implementation**

`client/src/shared/lib/rpc/workerRpc.ts`:
```ts
// Postable is the minimal worker surface the RPC needs (real Worker satisfies it).
export interface Postable {
  postMessage(message: unknown): void;
  onmessage: ((ev: MessageEvent) => void) | null;
}

type Pending = { resolve: (v: unknown) => void; reject: (e: Error) => void };

type Response =
  | { id: string; ok: true; result: unknown }
  | { id: string; ok: false; error: string };

function isResponse(v: unknown): v is Response {
  if (typeof v !== "object" || v === null) return false;
  const o = v as Record<string, unknown>;
  return typeof o.id === "string" && typeof o.ok === "boolean";
}

export interface WorkerRpc {
  call(kind: string, payload: unknown): Promise<unknown>;
  /** Subscribe to fire-and-forget events the worker pushes (no id). */
  onEvent(handler: (event: string, payload: unknown) => void): void;
}

export function createWorkerRpc(worker: Postable): WorkerRpc {
  let seq = 0;
  const pending = new Map<string, Pending>();
  let eventHandler: ((event: string, payload: unknown) => void) | null = null;

  worker.onmessage = (ev: MessageEvent) => {
    const data = ev.data;
    if (isResponse(data)) {
      const p = pending.get(data.id);
      if (!p) return;
      pending.delete(data.id);
      if (data.ok) p.resolve(data.result);
      else p.reject(new Error(data.error));
      return;
    }
    // Eventful (no id): { event, payload }
    if (typeof data === "object" && data !== null && "event" in (data as object)) {
      const e = data as { event: string; payload: unknown };
      eventHandler?.(e.event, e.payload);
    }
  };

  return {
    call(kind, payload) {
      const id = `r${++seq}`;
      return new Promise((resolve, reject) => {
        pending.set(id, { resolve, reject });
        worker.postMessage({ id, kind, payload });
      });
    },
    onEvent(handler) {
      eventHandler = handler;
    },
  };
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd client && npx vitest run src/shared/lib/rpc/workerRpc.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add client/src/shared/lib/rpc
git -C /Users/denisurevic/Documents/slack commit -m "feat(client): generic worker RPC (request/response + events)"
```

---

### Task 4: Relocate crypto worker to shared/lib/crypto

**Files:**
- Move: `client/src/crypto/*` → `client/src/shared/lib/crypto/*` (protocol.ts, worker.ts, client.ts, e2e.test.ts, protocol.test.ts, worker.test.ts, client.test.ts, wasm-pkg/)

- [ ] **Step 1: Move the files (preserve git history)**

Run:
```bash
cd /Users/denisurevic/Documents/slack/client
mkdir -p src/shared/lib/crypto
git mv src/crypto/* src/shared/lib/crypto/
rmdir src/crypto 2>/dev/null || true
```

- [ ] **Step 2: Fix internal references**

The crypto files import each other relatively (`./protocol`, `./wasm-pkg/crypto_core.js`) — those relative paths are unchanged by the move, so no edits needed inside the files. Verify nothing else in the repo imported `src/crypto` (only the crypto tests reference it, and they moved together).
Run: `cd client && grep -rn "src/crypto\|from \"\.\./crypto\|from \"\.\./\.\./crypto" src || echo "no stale references"`
Expected: no stale references.

- [ ] **Step 3: Run the moved tests**

Run: `cd client && npx vitest run src/shared/lib/crypto/`
Expected: PASS — all crypto tests (protocol, worker, client, e2e) pass at the new path. (If the worker test's wasm load breaks under jsdom, add `// @vitest-environment node` at the top of `worker.test.ts` and `e2e.test.ts`.)

- [ ] **Step 4: Verify build + full suite**

Run: `cd client && npx vitest run && npx tsc --noEmit`
Expected: all green, tsc clean.

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add -A client/src
git -C /Users/denisurevic/Documents/slack commit -m "refactor(client): relocate crypto worker to shared/lib/crypto (FSD)"
```

---

### Task 5: Protocol connection (pure) + ProtocolClient + worker shell

**Files:**
- Create: `client/src/shared/api/ds.ts`, `client/src/shared/lib/transport/connection.ts`, `client/src/shared/lib/transport/connection.test.ts`, `client/src/shared/lib/transport/protocolClient.ts`, `client/src/shared/lib/transport/protocol.worker.ts`

- [ ] **Step 1: Write the failing test (pure connection against a mock WebSocket)**

`client/src/shared/lib/transport/connection.test.ts`:
```ts
import { describe, it, expect, vi } from "vitest";
import { ProtocolConnection, type WebSocketLike } from "./connection";

// Minimal controllable mock socket.
class MockSocket implements WebSocketLike {
  onopen: (() => void) | null = null;
  onmessage: ((data: string) => void) | null = null;
  onclose: (() => void) | null = null;
  sent: string[] = [];
  send(data: string) { this.sent.push(data); }
  close() { this.onclose?.(); }
  open() { this.onopen?.(); }
  receive(obj: unknown) { this.onmessage?.(JSON.stringify(obj)); }
}

function setup() {
  const sock = new MockSocket();
  const messages: any[] = [];
  const conn = new ProtocolConnection(() => sock, "token-abc");
  conn.onMessage((m) => messages.push(m));
  return { sock, conn, messages };
}

describe("ProtocolConnection", () => {
  it("queues sends until open, then flushes", () => {
    const { sock, conn } = setup();
    conn.connect();
    conn.send({ clientMsgId: "c1", groupId: "g1", contentType: "application", ciphertext: "AQID" });
    expect(sock.sent.length).toBe(0); // not open yet → queued
    sock.open();
    expect(sock.sent.length).toBe(1);
    expect(JSON.parse(sock.sent[0]).type).toBe("send");
  });

  it("emits inbound message frames", () => {
    const { sock, conn, messages } = setup();
    conn.connect();
    sock.open();
    sock.receive({ type: "message", group_id: "g1", seq: 5, sender_device: "d", content_type: "application", ciphertext: "AQID" });
    expect(messages.length).toBe(1);
    expect(messages[0].seq).toBe(5);
  });

  it("on reconnect, re-sends a sync for the known cursor", () => {
    const { sock, conn } = setup();
    conn.setCursor("g1", 7);
    conn.connect();
    sock.open();
    const syncs = sock.sent.map((s) => JSON.parse(s)).filter((f) => f.type === "sync");
    expect(syncs.some((f) => f.group_id === "g1" && f.since_seq === 7)).toBe(true);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd client && npx vitest run src/shared/lib/transport/connection.test.ts`
Expected: FAIL — cannot find `./connection`.

- [ ] **Step 3: Write minimal implementation**

`client/src/shared/api/ds.ts`:
```ts
// DS WebSocket frame contract (mirrors backend/internal/ws/frames.go). Ciphertext
// fields are base64 strings on the wire.
export type SendFrame = { type: "send"; client_msg_id: string; group_id: string; content_type: string; ciphertext: string };
export type AckFrame = { type: "ack"; group_id: string; up_to_seq: number };
export type SyncFrame = { type: "sync"; group_id: string; since_seq: number };
export type SentFrame = { type: "sent"; client_msg_id: string; group_id: string; seq: number; server_ts: number };
export type MessageFrame = { type: "message"; group_id: string; seq: number; sender_device: string; content_type: string; ciphertext: string; server_ts: number };
export type ErrorFrame = { type: "error"; code: string; message: string };

export type InboundFrame = SentFrame | MessageFrame | ErrorFrame;
```

`client/src/shared/lib/transport/connection.ts`:
```ts
import type { MessageFrame } from "../../api/ds";

// WebSocketLike is the minimal socket surface (real WebSocket adapts to it).
export interface WebSocketLike {
  send(data: string): void;
  close(): void;
  onopen: (() => void) | null;
  onmessage: ((data: string) => void) | null;
  onclose: (() => void) | null;
}

export type OutgoingSend = { clientMsgId: string; groupId: string; contentType: string; ciphertext: string };

// ProtocolConnection is the pure transport logic: queue-until-open, sync-on-connect,
// inbound frame demux. It is driven by an injected WebSocketLike so it tests without
// a real socket. The worker shell wires a real WebSocket to it.
export class ProtocolConnection {
  private sock: WebSocketLike | null = null;
  private open = false;
  private outQueue: string[] = [];
  private cursors = new Map<string, number>();
  private msgHandler: ((m: MessageFrame) => void) | null = null;
  private statusHandler: ((s: "online" | "offline") => void) | null = null;

  constructor(private mkSocket: () => WebSocketLike, private token: string) {}

  onMessage(h: (m: MessageFrame) => void) { this.msgHandler = h; }
  onStatus(h: (s: "online" | "offline") => void) { this.statusHandler = h; }
  setCursor(groupID: string, seq: number) { this.cursors.set(groupID, seq); }

  connect() {
    const sock = this.mkSocket();
    this.sock = sock;
    sock.onopen = () => {
      this.open = true;
      this.statusHandler?.("online");
      // Re-sync every known conversation from its cursor.
      for (const [groupID, since] of this.cursors) {
        this.transmit({ type: "sync", group_id: groupID, since_seq: since });
      }
      // Flush queued sends.
      const q = this.outQueue;
      this.outQueue = [];
      for (const raw of q) sock.send(raw);
    };
    sock.onmessage = (data) => this.handleInbound(data);
    sock.onclose = () => {
      this.open = false;
      this.statusHandler?.("offline");
    };
  }

  send(s: OutgoingSend) {
    const frame = JSON.stringify({
      type: "send", client_msg_id: s.clientMsgId, group_id: s.groupId,
      content_type: s.contentType, ciphertext: s.ciphertext,
    });
    if (this.open && this.sock) this.sock.send(frame);
    else this.outQueue.push(frame);
  }

  ack(groupID: string, upToSeq: number) {
    this.cursors.set(groupID, upToSeq);
    this.transmit({ type: "ack", group_id: groupID, up_to_seq: upToSeq });
  }

  private transmit(frame: object) {
    const raw = JSON.stringify(frame);
    if (this.open && this.sock) this.sock.send(raw);
    else this.outQueue.push(raw);
  }

  private handleInbound(data: string) {
    let frame: any;
    try { frame = JSON.parse(data); } catch { return; }
    if (frame.type === "message" && this.msgHandler) {
      this.msgHandler(frame as MessageFrame);
    }
  }
}
```

`client/src/shared/lib/transport/protocolClient.ts`:
```ts
import type { Postable } from "../rpc/workerRpc";
import type { MessageFrame } from "../../api/ds";

// ProtocolClient is the main-thread proxy over the protocol worker. It speaks the
// worker's postMessage protocol: commands {cmd, ...} and inbound events {event, payload}.
export class ProtocolClient {
  private msgHandler: ((m: MessageFrame) => void) | null = null;
  private statusHandler: ((s: string) => void) | null = null;

  constructor(private worker: Postable) {
    this.worker.onmessage = (ev: MessageEvent) => {
      const d = ev.data as { event?: string; payload?: unknown };
      if (d.event === "message") this.msgHandler?.(d.payload as MessageFrame);
      else if (d.event === "status") this.statusHandler?.(d.payload as string);
    };
  }

  connect(token: string) { this.worker.postMessage({ cmd: "connect", token }); }
  send(s: { clientMsgId: string; groupId: string; contentType: string; ciphertext: string }) {
    this.worker.postMessage({ cmd: "send", ...s });
  }
  ack(groupID: string, upToSeq: number) { this.worker.postMessage({ cmd: "ack", groupID, upToSeq }); }
  onMessage(h: (m: MessageFrame) => void) { this.msgHandler = h; }
  onStatus(h: (s: string) => void) { this.statusHandler = h; }
}
```

`client/src/shared/lib/transport/protocol.worker.ts` (thin shell; not unit-tested — logic lives in ProtocolConnection):
```ts
import { ProtocolConnection, type WebSocketLike } from "./connection";

// Adapts a browser WebSocket to WebSocketLike.
function realSocket(url: string, token: string): WebSocketLike {
  // The DS expects the bearer token; browsers can't set headers on WebSocket, so the
  // token is passed as a subprotocol or query param per deployment. Here: query param.
  const ws = new WebSocket(`${url}?access_token=${encodeURIComponent(token)}`);
  const like: WebSocketLike = {
    send: (d) => ws.send(d),
    close: () => ws.close(),
    onopen: null, onmessage: null, onclose: null,
  };
  ws.onopen = () => like.onopen?.();
  ws.onmessage = (e) => like.onmessage?.(typeof e.data === "string" ? e.data : "");
  ws.onclose = () => like.onclose?.();
  return like;
}

let conn: ProtocolConnection | null = null;

self.onmessage = (ev: MessageEvent) => {
  const d = ev.data as any;
  switch (d.cmd) {
    case "connect": {
      const url = (self as any).DS_WS_URL ?? "ws://localhost:8080/ws";
      conn = new ProtocolConnection(() => realSocket(url, d.token), d.token);
      conn.onMessage((m) => (self as unknown as Worker).postMessage({ event: "message", payload: m }));
      conn.onStatus((s) => (self as unknown as Worker).postMessage({ event: "status", payload: s }));
      conn.connect();
      break;
    }
    case "send": conn?.send({ clientMsgId: d.clientMsgId, groupId: d.groupId, contentType: d.contentType, ciphertext: d.ciphertext }); break;
    case "ack": conn?.ack(d.groupID, d.upToSeq); break;
  }
};
```
NOTE: the bearer-token-over-WebSocket detail (browsers can't set `Authorization` on the WS handshake) — this plan uses an `?access_token=` query param. The DS gateway currently reads `Authorization: Bearer`. Reconciling this (DS also accepting a query-param/subprotocol token, or a short-lived ticket) is a backend follow-up; flag it. For the transport DEMO and tests, the pure `ProtocolConnection` is driven directly with a mock socket, so this doesn't block this plan.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd client && npx vitest run src/shared/lib/transport/connection.test.ts`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add client/src/shared/api client/src/shared/lib/transport
git -C /Users/denisurevic/Documents/slack commit -m "feat(client): protocol connection (pure) + ProtocolClient + worker shell"
```

---

## Milestone 3 — Entities, features, orchestration, demo

### Task 6: Entity stores (conversation, connection)

**Files:**
- Create: `client/src/entities/conversation/store.ts`, `client/src/entities/connection/store.ts`, `client/src/entities/conversation/store.test.ts`

- [ ] **Step 1: Write the failing test**

`client/src/entities/conversation/store.test.ts`:
```ts
import { describe, it, expect } from "vitest";
import { createConversationStore } from "./store";

describe("conversation store", () => {
  it("appends decrypted messages ordered by seq and tracks cursor", () => {
    const s = createConversationStore();
    s.addMessage("g1", { seq: 2, sender: "bob", text: "hi" });
    s.addMessage("g1", { seq: 1, sender: "alice", text: "yo" });
    const msgs = s.messages("g1");
    expect(msgs.map((m) => m.seq)).toEqual([1, 2]); // sorted
    expect(s.cursor("g1")).toBe(2); // highest seq seen
  });

  it("dedupes by seq", () => {
    const s = createConversationStore();
    s.addMessage("g1", { seq: 1, sender: "a", text: "x" });
    s.addMessage("g1", { seq: 1, sender: "a", text: "x" });
    expect(s.messages("g1").length).toBe(1);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd client && npx vitest run src/entities/conversation/store.test.ts`
Expected: FAIL — cannot find `./store`.

- [ ] **Step 3: Write minimal implementation**

`client/src/entities/conversation/store.ts`:
```ts
import { createStore } from "solid-js/store";

export type ChatMessage = { seq: number; sender: string; text: string };

type State = { byGroup: Record<string, ChatMessage[]> };

// Conversation store: decrypted messages per group, ordered by seq, deduped.
export function createConversationStore() {
  const [state, setState] = createStore<State>({ byGroup: {} });

  return {
    messages(groupID: string): ChatMessage[] {
      return state.byGroup[groupID] ?? [];
    },
    cursor(groupID: string): number {
      const m = state.byGroup[groupID] ?? [];
      return m.length ? m[m.length - 1].seq : 0;
    },
    addMessage(groupID: string, msg: ChatMessage) {
      const cur = state.byGroup[groupID] ?? [];
      if (cur.some((m) => m.seq === msg.seq)) return; // dedupe
      const next = [...cur, msg].sort((a, b) => a.seq - b.seq);
      setState("byGroup", groupID, next);
    },
  };
}

export type ConversationStore = ReturnType<typeof createConversationStore>;
```

`client/src/entities/connection/store.ts`:
```ts
import { createSignal } from "solid-js";

export type ConnStatus = "offline" | "connecting" | "online";

export function createConnectionStore() {
  const [status, setStatus] = createSignal<ConnStatus>("offline");
  return { status, setStatus };
}

export type ConnectionStore = ReturnType<typeof createConnectionStore>;
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd client && npx vitest run src/entities/conversation/store.test.ts`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add client/src/entities
git -C /Users/denisurevic/Documents/slack commit -m "feat(client): conversation + connection entity stores"
```

---

### Task 7: Orchestrator (app layer) — inbound decrypt + outbound encrypt

**Files:**
- Create: `client/src/app/orchestrator.ts`, `client/src/app/orchestrator.test.ts`

The orchestrator is the clean-architecture core: it wires the protocol + crypto clients to the stores. It depends on small interfaces (not concrete workers), so it tests with fakes.

- [ ] **Step 1: Write the failing test**

`client/src/app/orchestrator.test.ts`:
```ts
import { describe, it, expect, vi } from "vitest";
import { createOrchestrator } from "./orchestrator";
import { createConversationStore } from "../entities/conversation/store";
import { createConnectionStore } from "../entities/connection/store";
import type { MessageFrame } from "../shared/api/ds";

// Fakes for the two clients (the orchestrator depends on interfaces, not Workers).
function fakes() {
  let onMsg: (m: MessageFrame) => void = () => {};
  let onStatus: (s: string) => void = () => {};
  const acked: Array<{ g: string; s: number }> = [];
  const sent: any[] = [];
  const protocol = {
    connect: vi.fn(),
    send: (s: any) => sent.push(s),
    ack: (g: string, s: number) => acked.push({ g, s }),
    onMessage: (h: (m: MessageFrame) => void) => (onMsg = h),
    onStatus: (h: (s: string) => void) => (onStatus = h),
  };
  const crypto = {
    // decrypt returns the plaintext bytes for the ciphertext; here we fake it.
    decrypt: vi.fn(async (_group: string, _ct: Uint8Array) => new TextEncoder().encode("hello")),
    encrypt: vi.fn(async (_group: string, _pt: Uint8Array) => new Uint8Array([1, 2, 3])),
  };
  return { protocol, crypto, sent, acked, fire: (m: MessageFrame) => onMsg(m), fireStatus: (s: string) => onStatus(s) };
}

describe("orchestrator", () => {
  it("inbound message → decrypt → store → ack", async () => {
    const f = fakes();
    const conv = createConversationStore();
    const conn = createConnectionStore();
    createOrchestrator({ protocol: f.protocol as any, crypto: f.crypto as any, conversation: conv, connection: conn });

    f.fire({ type: "message", group_id: "g1", seq: 4, sender_device: "bob", content_type: "application", ciphertext: btoa("ct"), server_ts: 0 });
    await Promise.resolve(); // let the async decrypt settle
    await Promise.resolve();

    expect(conv.messages("g1").map((m) => m.text)).toEqual(["hello"]);
    expect(f.acked).toContainEqual({ g: "g1", s: 4 });
  });

  it("status events update the connection store", () => {
    const f = fakes();
    const conn = createConnectionStore();
    createOrchestrator({ protocol: f.protocol as any, crypto: f.crypto as any, conversation: createConversationStore(), connection: conn });
    f.fireStatus("online");
    expect(conn.status()).toBe("online");
  });

  it("sendText → encrypt → protocol.send", async () => {
    const f = fakes();
    const orch = createOrchestrator({ protocol: f.protocol as any, crypto: f.crypto as any, conversation: createConversationStore(), connection: createConnectionStore() });
    await orch.sendText("g1", "hi there");
    expect(f.crypto.encrypt).toHaveBeenCalled();
    expect(f.sent.length).toBe(1);
    expect(f.sent[0].groupId).toBe("g1");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd client && npx vitest run src/app/orchestrator.test.ts`
Expected: FAIL — cannot find `./orchestrator`.

- [ ] **Step 3: Write minimal implementation**

`client/src/app/orchestrator.ts`:
```ts
import type { MessageFrame } from "../shared/api/ds";
import type { ConversationStore } from "../entities/conversation/store";
import type { ConnectionStore, ConnStatus } from "../entities/connection/store";

// Narrow interfaces so the orchestrator depends on behavior, not on Worker classes.
export interface ProtocolPort {
  connect(token: string): void;
  send(s: { clientMsgId: string; groupId: string; contentType: string; ciphertext: string }): void;
  ack(groupID: string, upToSeq: number): void;
  onMessage(h: (m: MessageFrame) => void): void;
  onStatus(h: (s: string) => void): void;
}
export interface CryptoPort {
  encrypt(groupID: string, plaintext: Uint8Array): Promise<Uint8Array>;
  decrypt(groupID: string, ciphertext: Uint8Array): Promise<Uint8Array>;
}

export interface OrchestratorDeps {
  protocol: ProtocolPort;
  crypto: CryptoPort;
  conversation: ConversationStore;
  connection: ConnectionStore;
}

function b64ToBytes(b64: string): Uint8Array {
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}
function bytesToB64(b: Uint8Array): string {
  let s = "";
  for (const x of b) s += String.fromCharCode(x);
  return btoa(s);
}

// The orchestration layer: all cross-thread business logic lives here, NOT in UI.
export function createOrchestrator(deps: OrchestratorDeps) {
  const { protocol, crypto, conversation, connection } = deps;

  protocol.onStatus((s) => connection.setStatus(s as ConnStatus));

  protocol.onMessage((m) => {
    // Only application messages render as text; handshake frames are applied by the
    // crypto engine without producing displayable text (empty plaintext).
    void (async () => {
      try {
        const plaintext = await crypto.decrypt(m.group_id, b64ToBytes(m.ciphertext));
        if (plaintext.length > 0) {
          conversation.addMessage(m.group_id, {
            seq: m.seq, sender: m.sender_device, text: new TextDecoder().decode(plaintext),
          });
        }
        protocol.ack(m.group_id, m.seq);
      } catch {
        // Undecryptable (missing epoch/keys): leave for a later sync; do not crash.
      }
    })();
  });

  return {
    connect(token: string) { protocol.connect(token); },
    async sendText(groupID: string, text: string) {
      const ct = await crypto.encrypt(groupID, new TextEncoder().encode(text));
      protocol.send({
        clientMsgId: cryptoRandomId(), groupId: groupID,
        contentType: "application", ciphertext: bytesToB64(ct),
      });
    },
  };
}

function cryptoRandomId(): string {
  const b = new Uint8Array(16);
  crypto.getRandomValues(b); // Web Crypto (browser + jsdom)
  return Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");
}
```
NOTE: `cryptoRandomId` uses the global Web Crypto `crypto.getRandomValues` (available in browsers and jsdom). Don't shadow it with the `CryptoPort` param name — the param is `deps.crypto`, the global is `crypto`; keep them distinct (the helper is module-scope, outside the deps closure, so it sees the global). Verify no name collision.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd client && npx vitest run src/app/orchestrator.test.ts`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add client/src/app/orchestrator.ts client/src/app/orchestrator.test.ts
git -C /Users/denisurevic/Documents/slack commit -m "feat(client): app orchestrator — inbound decrypt + outbound encrypt (clean arch)"
```

---

### Task 8: Chat widgets + page (presentational, wired via props)

**Files:**
- Create: `client/src/widgets/message-list/MessageList.tsx`, `client/src/widgets/composer/Composer.tsx`, `client/src/pages/chat/ChatPage.tsx`, `client/src/pages/chat/ChatPage.test.tsx`

- [ ] **Step 1: Write the failing test**

`client/src/pages/chat/ChatPage.test.tsx`:
```tsx
// @vitest-environment jsdom
import { render, fireEvent } from "@solidjs/testing-library";
import { describe, it, expect, vi } from "vitest";
import { ChatPage } from "./ChatPage";

describe("ChatPage", () => {
  it("renders messages and sends composed text via the onSend prop", async () => {
    const onSend = vi.fn();
    const messages = [{ seq: 1, sender: "alice", text: "hello" }];
    const { getByText, getByRole } = render(() => (
      <ChatPage messages={messages} status="online" onSend={onSend} />
    ));
    expect(getByText("hello")).toBeTruthy();

    const input = getByRole("textbox") as HTMLInputElement;
    fireEvent.input(input, { target: { value: "hi back" } });
    fireEvent.click(getByRole("button"));
    expect(onSend).toHaveBeenCalledWith("hi back");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd client && npx vitest run src/pages/chat/ChatPage.test.tsx`
Expected: FAIL — cannot find `./ChatPage`.

- [ ] **Step 3: Write minimal implementation**

`client/src/widgets/message-list/MessageList.tsx`:
```tsx
import { For, type Component } from "solid-js";
import type { ChatMessage } from "../../entities/conversation/store";

export const MessageList: Component<{ messages: ChatMessage[] }> = (props) => (
  <ul data-testid="message-list">
    <For each={props.messages}>
      {(m) => <li><b>{m.sender}:</b> {m.text}</li>}
    </For>
  </ul>
);
```

`client/src/widgets/composer/Composer.tsx`:
```tsx
import { createSignal, type Component } from "solid-js";
import { Button } from "../../shared/ui";

export const Composer: Component<{ onSend: (text: string) => void }> = (props) => {
  const [text, setText] = createSignal("");
  const submit = () => {
    const t = text().trim();
    if (!t) return;
    props.onSend(t);
    setText("");
  };
  return (
    <div>
      <input type="text" value={text()} onInput={(e) => setText(e.currentTarget.value)} />
      <Button onClick={submit}>Send</Button>
    </div>
  );
};
```

`client/src/pages/chat/ChatPage.tsx`:
```tsx
import type { Component } from "solid-js";
import { MessageList } from "../../widgets/message-list/MessageList";
import { Composer } from "../../widgets/composer/Composer";
import type { ChatMessage } from "../../entities/conversation/store";

// Presentational page: receives data + callbacks via props. No business logic / no
// direct worker or store access here (clean architecture).
export const ChatPage: Component<{
  messages: ChatMessage[];
  status: string;
  onSend: (text: string) => void;
}> = (props) => (
  <div>
    <header data-testid="status">{props.status}</header>
    <MessageList messages={props.messages} />
    <Composer onSend={props.onSend} />
  </div>
);
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd client && npx vitest run src/pages/chat/ChatPage.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add client/src/widgets client/src/pages
git -C /Users/denisurevic/Documents/slack commit -m "feat(client): chat widgets + page (presentational, prop-driven)"
```

---

### Task 9: Bootstrap wiring + App integration (real workers, browser-only)

**Files:**
- Create: `client/src/app/bootstrap.ts`, `client/src/shared/config/env.ts`
- Modify: `client/src/app/App.tsx`, `client/src/app/App.test.tsx`

- [ ] **Step 1: Write the failing test (App renders ChatPage from injected orchestrator + stores)**

Update `client/src/app/App.test.tsx`:
```tsx
// @vitest-environment jsdom
import { render } from "@solidjs/testing-library";
import { describe, it, expect, vi } from "vitest";
import { App } from "./App";
import { createConversationStore } from "../entities/conversation/store";
import { createConnectionStore } from "../entities/connection/store";

describe("App", () => {
  it("renders the chat page wired to injected stores + send handler", () => {
    const conv = createConversationStore();
    conv.addMessage("g1", { seq: 1, sender: "alice", text: "wired" });
    const conn = createConnectionStore();
    conn.setStatus("online");
    const onSend = vi.fn();

    const { getByText, getByTestId } = render(() => (
      <App groupId="g1" conversation={conv} connection={conn} onSend={onSend} />
    ));
    expect(getByText("wired")).toBeTruthy();
    expect(getByTestId("status").textContent).toBe("online");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd client && npx vitest run src/app/App.test.tsx`
Expected: FAIL — `App` doesn't accept these props yet.

- [ ] **Step 3: Implement App (props-injected) + bootstrap (real wiring)**

`client/src/app/App.tsx` (now a thin composition that takes injected dependencies — keeps App testable and free of worker instantiation):
```tsx
import type { Component } from "solid-js";
import { ChatPage } from "../pages/chat/ChatPage";
import type { ConversationStore } from "../entities/conversation/store";
import type { ConnectionStore } from "../entities/connection/store";

export const App: Component<{
  groupId: string;
  conversation: ConversationStore;
  connection: ConnectionStore;
  onSend: (text: string) => void;
}> = (props) => (
  <ChatPage
    messages={props.conversation.messages(props.groupId)}
    status={props.connection.status()}
    onSend={props.onSend}
  />
);
```

`client/src/shared/config/env.ts`:
```ts
export const DS_WS_URL = import.meta.env?.VITE_DS_WS_URL ?? "ws://localhost:8080/ws";
```

`client/src/app/bootstrap.ts` (browser-only: instantiates the real workers, builds clients + orchestrator, returns what `main.tsx` mounts). Not unit-tested — it's pure wiring exercised by `vite build`:
```ts
import { CryptoClient } from "../shared/lib/crypto/client";
import { ProtocolClient } from "../shared/lib/transport/protocolClient";
import { createConversationStore } from "../entities/conversation/store";
import { createConnectionStore } from "../entities/connection/store";
import { createOrchestrator } from "./orchestrator";

export function bootstrap(deviceName: string) {
  const cryptoWorker = new Worker(new URL("../shared/lib/crypto/worker.ts", import.meta.url), { type: "module" });
  const protocolWorker = new Worker(new URL("../shared/lib/transport/protocol.worker.ts", import.meta.url), { type: "module" });

  const crypto = new CryptoClient(cryptoWorker, deviceName);
  const protocol = new ProtocolClient(protocolWorker);
  const conversation = createConversationStore();
  const connection = createConnectionStore();

  // Adapt CryptoClient (encrypt/decrypt return Uint8Array) to the orchestrator's CryptoPort.
  const orchestrator = createOrchestrator({
    protocol, crypto, conversation, connection,
  });
  return { orchestrator, conversation, connection, protocol };
}
```
NOTE: `CryptoClient` (from subproject 1) exposes `encrypt(groupId, plaintext)` / `decrypt(groupId, message)` returning `Promise<Uint8Array>` — that matches `CryptoPort`. If the method names/signatures differ, add a tiny adapter object implementing `CryptoPort` rather than changing CryptoClient. `ProtocolClient` matches `ProtocolPort`. Verify both satisfy the ports (tsc will catch mismatches).

Update `client/src/main.tsx` to use bootstrap:
```tsx
import { render } from "solid-js/web";
import { App } from "./app/App";
import { bootstrap } from "./app/bootstrap";

const root = document.getElementById("root");
if (root) {
  const { orchestrator, conversation, connection } = bootstrap("device");
  render(() => (
    <App
      groupId="g1"
      conversation={conversation}
      connection={connection}
      onSend={(text) => orchestrator.sendText("g1", text)}
    />
  ), root);
}
```

- [ ] **Step 4: Run test + build**

Run: `cd client && npx vitest run && npx tsc --noEmit && npx vite build`
Expected: tests green, tsc clean, `vite build` succeeds (bundles both workers + wasm). If tsc flags a `CryptoPort` mismatch, add the adapter as noted. Report the exact CryptoClient signature you found.

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add client/src/app client/src/main.tsx client/src/shared/config
git -C /Users/denisurevic/Documents/slack commit -m "feat(client): bootstrap real workers + props-injected App"
```

---

### Task 10: Transport-demo vertical slice (real crypto worker + mock DS) + FSD boundary doc

**Files:**
- Create: `client/src/app/transport-demo.test.ts`, `client/docs/architecture.md`

This is the capstone: prove the full pipeline (protocol inbound → bus → real crypto decrypt → store) using the REAL crypto engine over an MLS group, and a mock protocol port feeding a genuine DS `message` frame.

- [ ] **Step 1: Write the test**

`client/src/app/transport-demo.test.ts`:
```ts
// @vitest-environment node
import { describe, it, expect } from "vitest";
import { createOrchestrator, type ProtocolPort } from "./orchestrator";
import { createConversationStore } from "../entities/conversation/store";
import { createConnectionStore } from "../entities/connection/store";
import { createDispatcher } from "../shared/lib/crypto/worker";
import type { MessageFrame } from "../shared/api/ds";

// A mock ProtocolPort whose inbound stream we drive, and that captures acks.
function mockProtocol() {
  let onMsg: (m: MessageFrame) => void = () => {};
  const acks: number[] = [];
  const port: ProtocolPort = {
    connect() {},
    send() {},
    ack: (_g, s) => acks.push(s),
    onMessage: (h) => (onMsg = h),
    onStatus() {},
  };
  return { port, acks, fire: (m: MessageFrame) => onMsg(m) };
}

// Adapt two crypto dispatchers (alice = sender, bob = receiver) to CryptoPort.
function cryptoPort(name: string) {
  const d = createDispatcher(name);
  return {
    dispatcher: d,
    port: {
      async encrypt(group: string, pt: Uint8Array): Promise<Uint8Array> {
        const r = await d.handleRequest({ id: "e", kind: "encrypt", groupId: group, plaintext: pt });
        return (r as any).result as Uint8Array;
      },
      async decrypt(group: string, ct: Uint8Array): Promise<Uint8Array> {
        const r = await d.handleRequest({ id: "d", kind: "decrypt", groupId: group, message: ct });
        return (r as any).result as Uint8Array;
      },
    },
  };
}

describe("transport demo (real MLS through the orchestrator)", () => {
  it("DS message frame → orchestrator → real decrypt → store, then ack", async () => {
    const alice = cryptoPort("alice@corp");
    const bob = cryptoPort("bob@corp");

    // Establish an MLS group alice→bob via the real crypto dispatchers.
    const bobKp = (await bob.dispatcher.handleRequest({ id: "1", kind: "keyPackage" })).result as Uint8Array;
    await alice.dispatcher.handleRequest({ id: "2", kind: "createGroup", groupId: "g1" });
    const add = (await alice.dispatcher.handleRequest({ id: "3", kind: "addMember", groupId: "g1", keyPackage: bobKp })).result as { commit: Uint8Array; welcome: Uint8Array };
    await bob.dispatcher.handleRequest({ id: "4", kind: "joinFromWelcome", welcome: add.welcome });

    // Alice encrypts a real ciphertext for the group.
    const ct = (await alice.dispatcher.handleRequest({ id: "5", kind: "encrypt", groupId: "g1", plaintext: new TextEncoder().encode("hi bob") })).result as Uint8Array;

    // Wire bob's orchestrator and feed the ciphertext as if it arrived from DS.
    const proto = mockProtocol();
    const conv = createConversationStore();
    createOrchestrator({ protocol: proto.port, crypto: bob.port, conversation: conv, connection: createConnectionStore() });

    const toB64 = (b: Uint8Array) => Buffer.from(b).toString("base64");
    proto.fire({ type: "message", group_id: "g1", seq: 1, sender_device: "alice", content_type: "application", ciphertext: toB64(ct), server_ts: 0 });

    // Let the async decrypt pipeline settle.
    await new Promise((r) => setTimeout(r, 50));

    expect(conv.messages("g1").map((m) => m.text)).toEqual(["hi bob"]);
    expect(proto.acks).toContain(1);
  });
});
```
NOTE: this reuses `createDispatcher` from the crypto worker (subproject 1 / Task 13 of that plan) which runs the real WASM MLS engine in-process (no real Worker). If the orchestrator's `b64ToBytes` (browser `atob`) isn't available under the node env, this test runs `@vitest-environment node` — `atob`/`btoa` are global in modern Node (18+), so they work. If not, the orchestrator already uses them at runtime in the browser; for the node test, confirm `atob` is defined (Node 16+ has it global since 16). Verify.

- [ ] **Step 2: Run the test**

Run: `cd client && npx vitest run src/app/transport-demo.test.ts`
Expected: PASS — bob's store contains "hi bob", decrypted by the real MLS engine, and seq 1 was acked. This proves: protocol inbound → window-bus orchestration → crypto worker decrypt → entity store, end to end.
If it fails on the decrypt, the most likely cause is a ciphertext base64 round-trip mismatch or the dispatcher's result shape — diagnose against the crypto worker's actual `handleRequest`/`createDispatcher` return type; do not weaken the assertion (the real plaintext must equal "hi bob").

- [ ] **Step 3: Document the FSD boundaries**

`client/docs/architecture.md`:
```markdown
# Frontend architecture

Three threads, FSD layers, window-hub bus. See
docs/superpowers/specs/2026-06-17-frontend-scaffold-design.md for the full design.

## Import rules (FSD — imports only point DOWN the layers)
app → pages → widgets → features → entities → shared

- `shared/ui` (ui-kit): pure presentational components. MUST NOT import features/entities/app.
- `widgets`, `pages`: presentation only — receive data + callbacks via props. NO worker/store access, NO business logic.
- `features`, `entities`, `app`: business logic lives here. The orchestrator (app) is the only place that wires the protocol + crypto clients to stores.
- `shared/lib/{crypto,transport,rpc}`: infrastructure (workers, bus). UI never imports these directly.

## Threads
- UI (window): SolidJS + orchestrator (bus hub).
- Protocol worker: WebSocket ↔ DS (frame contract in shared/api/ds.ts).
- Crypto worker: WASM MLS engine (shared/lib/crypto).
```

- [ ] **Step 4: Run the full client suite**

Run: `cd client && npx vitest run && npx tsc --noEmit && npx vite build`
Expected: all green.

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add client/src/app/transport-demo.test.ts client/docs/architecture.md
git -C /Users/denisurevic/Documents/slack commit -m "test(client): transport-demo vertical slice — real MLS decrypt through orchestrator; FSD boundary docs"
```

---

## Self-Review

**1. Spec coverage:**
- Vite + SolidJS + strict FSD skeleton + ui-kit → Tasks 1, 2, 8 ✓
- Typed window-hub bus (main thread relays; components never touch workers) → Tasks 3, 7 (orchestrator is the hub), 9 ✓
- Protocol worker: WS to DS (send/ack/sync/sent/message/error), bearer auth, send queue, reconnect+sync → Task 5 ✓
- Crypto worker relocated to shared/lib/crypto → Task 4 ✓
- Orchestration inbound ciphertext→decrypt→store→ack and outbound intent→encrypt→send → Task 7 ✓
- Entity stores (conversation w/ cursor, connection status) → Task 6 ✓
- Transport-demo vertical slice (real MLS through the pipeline) → Task 10 ✓
- Clean architecture (business logic out of UI; ui-kit/widgets/pages presentational) → enforced by structure + documented Task 10 ✓
- Tooling: Vite, vite-plugin-solid, vitest+jsdom, @solidjs/testing-library → Task 1 ✓
- Error handling (no token/offline, worker failure, decrypt failure non-fatal, reconnect+sync) → Tasks 5 (queue/reconnect), 7 (decrypt try/catch, status) ✓
- Out of scope (OPAQUE-client, KT-client, MLS group flows, real UI/UX) → correctly excluded.

**2. Placeholder scan:** No TBD/"handle errors" placeholders. Flagged verify-spots (vitest+solid `resolve.conditions`; per-file `@vitest-environment node` for wasm tests; bearer-token-over-WS query-param reconciliation with DS; CryptoClient↔CryptoPort signature check; `atob` under node) are conscious risk callouts with concrete fallbacks, not gaps. Every code step has complete code.

**3. Type consistency:** `MessageFrame`/DS frame types (shared/api/ds.ts) are used consistently in connection.ts, protocolClient.ts, orchestrator.ts, and tests. `ProtocolPort`/`CryptoPort` (orchestrator) are satisfied by `ProtocolClient`/`CryptoClient` (verified in Task 9 via tsc + adapter fallback). `ConversationStore`/`ConnectionStore` types flow from entities (Task 6) into orchestrator (7), App (9), and tests. `createWorkerRpc`/`Postable` (Task 3) — `Postable` is reused by `ProtocolClient` (Task 5). `createDispatcher`/`handleRequest` (crypto worker, subproject 1) used in Task 10 — its real return shape is flagged for verification.

**4. Worker-in-test risk (call out, not placeholder):** vitest does not run real module Workers, so ALL worker logic is pure and tested directly (`ProtocolConnection` with a mock socket — Task 5; `createDispatcher` in-process — Task 10). Real `new Worker(new URL(...))` wiring lives only in `bootstrap.ts`/`protocol.worker.ts` and is verified by `vite build`, not by spinning Workers in vitest. This mirrors how subproject 1 tested the crypto worker.
