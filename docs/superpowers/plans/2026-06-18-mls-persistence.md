# MLS Persistence + History Backfill Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Make chat messages actually send and survive a page reload. Today the OpenMLS group state lives only in the WASM engine's in-memory `HashMap`, so after F5 the engine throws `unknown group` on encrypt — the message never reaches the server — and the optimistic UI echo is lost. We persist the engine's full crypto state to IndexedDB so the device remembers its identity + groups across reloads, then backfill conversation history on load via the existing server-side `sync` path.

**Architecture:** Three phases.
- **A — crypto-core (Rust/WASM):** `Engine` gains `export_state()`/`restore()`. State = the device name, its `SignatureKeyPair` (derives serde), a snapshot of the provider's `MemoryStorage.values` map (public field), and the list of group ids. Restore rebuilds a default `OpenMlsRustCrypto`, repopulates its storage map, rebuilds the `Identity`, and `MlsGroup::load`s each group. Serialized with `serde_json`.
- **B — crypto worker (TS):** A small raw-IndexedDB module persists the exported state keyed by device name. The worker loads it on first use (`WasmEngine.restore`) and re-persists after every state-mutating op.
- **C — history backfill (TS):** The protocol layer gains `track(groupId, sinceSeq)` so the client seeds a sync cursor for every known conversation on connect; the existing `sync` server path returns stored ciphertext, which the (now-restored) engine decrypts and the orchestrator renders.

**Tech Stack:** Rust + openmls 0.6 / openmls_rust_crypto 0.3 (→ WASM via wasm-pack `--target web`), SolidJS + TS client, IndexedDB, vitest.

---

## Phase A — crypto-core persistence (Rust)

### Task 1: `Engine::export_state` / `Engine::restore`

**Files:**
- Modify: `crypto-core/src/engine.rs`
- Modify: `crypto-core/Cargo.toml` (promote `serde_json` to a runtime dependency)
- Test: inline `#[cfg(test)] mod tests` in `engine.rs`

Confirmed API facts (do not re-research):
- `openmls_rust_crypto::OpenMlsRustCrypto::storage()` returns `&MemoryStorage`.
- `openmls_memory_storage::MemoryStorage` has `pub values: RwLock<HashMap<Vec<u8>, Vec<u8>>>` (public field — read/write directly; no feature flag needed).
- `openmls_basic_credential::SignatureKeyPair` derives `serde::Serialize`/`Deserialize` and has `from_raw`, `private()`, `public()`, `to_public_vec()`.
- `MlsGroup::load(storage, &GroupId) -> Result<Option<MlsGroup>, Storage::Error>` (openmls 0.6).

- [ ] **Step 1: Cargo.toml** — move `serde_json = "1"` from `[dev-dependencies]` into `[dependencies]` (keep `serde` with `derive`). Run `cargo build` to confirm it resolves for the lib target.

- [ ] **Step 2: Add a `name` field to `Engine`** so the credential can be rebuilt on restore. In `engine.rs`, change the struct to:
```rust
pub struct Engine {
    provider: OpenMlsRustCrypto,
    identity: Identity,
    groups: HashMap<Vec<u8>, MlsGroup>,
    name: Vec<u8>,
}
```
In `Engine::new`, after generating the identity, set `name: name.to_vec()` in the constructed `Self { .. }`.

- [ ] **Step 3: Write the failing round-trip test** in the `tests` module:
```rust
#[test]
fn engine_state_survives_export_restore() {
    let mut alice = Engine::new(b"alice@corp");
    let group_id = b"team-1".to_vec();
    alice.create_group(&group_id).unwrap();
    let ct_before = alice.encrypt(&group_id, b"before reload").unwrap();
    assert!(!ct_before.is_empty());

    // Serialize the whole engine, drop it, and rebuild from bytes (simulating reload).
    let state = alice.export_state().unwrap();
    assert!(!state.is_empty());
    drop(alice);

    let mut restored = Engine::restore(&state).unwrap();
    assert!(restored.has_group(&group_id), "group must survive restore");
    // The killer bug: encrypt must NOT return UnknownGroup after restore.
    let ct_after = restored.encrypt(&group_id, b"after reload").unwrap();
    assert!(!ct_after.is_empty());
    // Signing identity is preserved.
    assert_eq!(restored.signing_public_key().len(), 32);
}

#[test]
fn restored_engine_decrypts_peer_message() {
    // Alice creates a group and adds Bob; after Alice round-trips her state,
    // Bob can still decrypt a message Alice sends, proving epoch secrets persisted.
    let mut alice = Engine::new(b"alice@corp");
    let mut bob = Engine::new(b"bob@corp");
    let bob_kp = bob.key_package_bytes().unwrap();
    let gid = b"team-1".to_vec();
    alice.create_group(&gid).unwrap();
    let add = alice.add_member(&gid, &bob_kp).unwrap();
    bob.join_from_welcome(&add.welcome).unwrap();

    let state = alice.export_state().unwrap();
    let mut alice2 = Engine::restore(&state).unwrap();

    let ct = alice2.encrypt(&gid, b"hi bob after reload").unwrap();
    assert_eq!(
        bob.process(&gid, &ct).unwrap().unwrap_application(),
        b"hi bob after reload"
    );
}
```

- [ ] **Step 4: Run the tests, verify they fail to compile** (`export_state`/`restore` undefined): `cd crypto-core && cargo test engine_state_survives_export_restore`. Expected: compile error.

- [ ] **Step 5: Implement `export_state` and `restore`** in `impl Engine`. Add the serde imports at the top (`use serde::{Serialize, Deserialize};`). Implementation:
```rust
#[derive(Serialize, Deserialize)]
struct PersistedState {
    name: Vec<u8>,
    // SignatureKeyPair derives serde; serialize it directly.
    signer: openmls_basic_credential::SignatureKeyPair,
    // Snapshot of the provider's MemoryStorage key/value map.
    storage: std::collections::HashMap<Vec<u8>, Vec<u8>>,
    group_ids: Vec<Vec<u8>>,
}

/// Serialize this device's full crypto state (identity + provider storage +
/// group ids) so it can be rebuilt verbatim after a reload.
pub fn export_state(&self) -> Result<Vec<u8>, EngineError> {
    let storage = self
        .provider
        .storage()
        .values
        .read()
        .expect("storage lock poisoned")
        .clone();
    let state = PersistedState {
        name: self.name.clone(),
        signer: self.identity.signer.clone(),
        storage,
        group_ids: self.groups.keys().cloned().collect(),
    };
    serde_json::to_vec(&state).map_err(|e| EngineError::serde(e))
}

/// Rebuild an Engine from `export_state` bytes.
pub fn restore(bytes: &[u8]) -> Result<Self, EngineError> {
    let state: PersistedState =
        serde_json::from_slice(bytes).map_err(|e| EngineError::serde(e))?;
    let provider = OpenMlsRustCrypto::default();
    // Repopulate the provider's storage with the persisted key/value map.
    {
        let mut values = provider
            .storage()
            .values
            .write()
            .expect("storage lock poisoned");
        *values = state.storage;
    }
    // Rebuild the identity (credential is derived from name + signer pubkey).
    let credential = BasicCredential::new(state.name.clone());
    let credential_with_key = CredentialWithKey {
        credential: credential.into(),
        signature_key: state.signer.to_public_vec().into(),
    };
    let identity = Identity {
        signer: state.signer,
        credential_with_key,
    };
    // Reload each group from the now-populated storage.
    let mut groups = HashMap::new();
    for gid in state.group_ids {
        let group = MlsGroup::load(provider.storage(), &GroupId::from_slice(&gid))
            .map_err(EngineError::mls)?
            .ok_or_else(|| EngineError::UnknownGroup(hex(&gid)))?;
        groups.insert(gid, group);
    }
    Ok(Self { provider, identity, groups, name: state.name })
}
```
Notes for the implementer:
- `BasicCredential`, `CredentialWithKey`, `GroupId`, `MlsGroup` are already imported via `openmls::prelude::*` in this file. `Identity` is `crate::identity::Identity` (already imported) — its fields `signer`/`credential_with_key` are `pub`.
- `EngineError::serde` and `EngineError::mls` already exist (used throughout this file). If `EngineError::mls` does not accept the `Storage::Error` type from `MlsGroup::load`, map it with `.map_err(|e| EngineError::Mls(format!("group load: {e:?}")))` instead — check `errors.rs` for the exact constructor and match the existing pattern.
- `SignatureKeyPair::clone` — `SignatureKeyPair` derives the needed traits; if `Clone` is not derived, reconstruct via `SignatureKeyPair::from_raw(scheme, self.identity.signer.private().to_vec(), self.identity.signer.public().to_vec())` using `self.identity.signer.signature_scheme()`.

- [ ] **Step 6: Run the tests, verify they pass**: `cd crypto-core && cargo test`. Expected: all tests pass (the two new ones plus the existing suite).

- [ ] **Step 7: Commit**
```bash
git add crypto-core/src/engine.rs crypto-core/Cargo.toml crypto-core/Cargo.lock
git commit -m "feat(crypto-core): Engine export_state/restore for reload persistence"
```

### Task 2: WASM bindings + rebuild wasm-pkg

**Files:**
- Modify: `crypto-core/src/lib.rs` (add `WasmEngine::export_state` + `WasmEngine::restore`)
- Regenerate: `client/src/shared/lib/crypto/wasm-pkg/*` (via wasm-pack)

- [ ] **Step 1: Add WASM bindings** in `lib.rs` to `impl WasmEngine`:
```rust
    /// Serialize the engine's full state (identity + groups) for persistence.
    pub fn export_state(&self) -> Result<Vec<u8>, JsError> {
        self.inner.export_state().map_err(to_js)
    }

    /// Rebuild an engine from `export_state` bytes (e.g. after a page reload).
    pub fn restore(state: Vec<u8>) -> Result<WasmEngine, JsError> {
        Ok(WasmEngine { inner: Engine::restore(&state).map_err(to_js)? })
    }
```
Note: `restore` is an associated function (no `&self`) — wasm-bindgen exposes it as a static method `WasmEngine.restore(state)`.

- [ ] **Step 2: Add a native wasm-api test** in the `wasm_api_tests` module of `lib.rs`:
```rust
    #[test]
    fn wasm_engine_round_trips_state() {
        let mut alice = WasmEngine::new("alice@corp");
        alice.create_group("team-1").unwrap();
        let state = alice.export_state().unwrap();
        let mut restored = WasmEngine::restore(state).unwrap();
        let ct = restored.encrypt("team-1", b"after reload".to_vec()).unwrap();
        assert!(!ct.is_empty());
    }
```

- [ ] **Step 3: Run native tests**: `cd crypto-core && cargo test`. Expected: pass.

- [ ] **Step 4: Rebuild the wasm package** into the client. Find the existing build command (check `crypto-core/` for a `build.sh`/`Makefile`/`package.json`, or the project README; the prior builds used `wasm-pack build --target web`). Run it so the output lands in `client/src/shared/lib/crypto/wasm-pkg/` (match the existing output path — confirm by `git status` showing only files under `wasm-pkg/` changed). Example (adjust `--out-dir` to the real path):
```bash
cd crypto-core && wasm-pack build --target web --out-dir ../client/src/shared/lib/crypto/wasm-pkg
```
After building, verify `client/src/shared/lib/crypto/wasm-pkg/crypto_core.d.ts` now declares `export_state(): Uint8Array;` and `static restore(state: Uint8Array): WasmEngine;`. Also confirm `export_group_info`/`join_by_external_commit` now appear in the `.d.ts` (this rebuild also catches up the bindings the worker currently casts via `PendingEngineOps`).

- [ ] **Step 5: Commit**
```bash
git add crypto-core/src/lib.rs client/src/shared/lib/crypto/wasm-pkg
git commit -m "feat(crypto-core): WASM export_state/restore bindings + rebuild wasm-pkg"
```

---

## Phase B — crypto worker persistence (TS / IndexedDB)

### Task 3: IndexedDB state store

**Files:**
- Create: `client/src/shared/lib/crypto/persistence.ts`
- Test: `client/src/shared/lib/crypto/persistence.test.ts`

This module persists one `Uint8Array` blob per device name. It uses raw IndexedDB (available in Web Workers). Tests run under vitest/jsdom; if `indexedDB` is undefined in the test env, the test installs `fake-indexeddb` — check whether `fake-indexeddb` is already a devDependency (`client/package.json`); if not, the test imports `"fake-indexeddb/auto"` and the implementer adds `fake-indexeddb` to devDependencies (`npm i -D fake-indexeddb`).

- [ ] **Step 1: Write the failing test** `persistence.test.ts`:
```ts
import "fake-indexeddb/auto";
import { describe, it, expect } from "vitest";
import { loadCryptoState, saveCryptoState } from "./persistence";

describe("crypto state persistence", () => {
  it("returns null when nothing is stored", async () => {
    expect(await loadCryptoState("nobody@corp")).toBeNull();
  });

  it("round-trips a saved blob by device name", async () => {
    const blob = new Uint8Array([1, 2, 3, 4]);
    await saveCryptoState("alice@corp", blob);
    const got = await loadCryptoState("alice@corp");
    expect(got).not.toBeNull();
    expect(Array.from(got!)).toEqual([1, 2, 3, 4]);
  });

  it("keeps device states separate", async () => {
    await saveCryptoState("a@corp", new Uint8Array([1]));
    await saveCryptoState("b@corp", new Uint8Array([2]));
    expect(Array.from((await loadCryptoState("a@corp"))!)).toEqual([1]);
    expect(Array.from((await loadCryptoState("b@corp"))!)).toEqual([2]);
  });
});
```

- [ ] **Step 2: Run it, verify it fails** (module missing): `cd client && npx vitest run src/shared/lib/crypto/persistence.test.ts`. Expected: FAIL (cannot resolve `./persistence`).

- [ ] **Step 3: Implement** `persistence.ts`:
```ts
// Per-device persistence of the OpenMLS engine state in IndexedDB. The crypto
// worker exports its state (identity + groups) as bytes; we store one blob per
// device name so a page reload can rebuild the engine instead of losing all
// group keys (which caused "unknown group" on send after refresh).
const DB_NAME = "messenger-crypto";
const STORE = "engine-state";

function openDb(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(DB_NAME, 1);
    req.onupgradeneeded = () => {
      const db = req.result;
      if (!db.objectStoreNames.contains(STORE)) db.createObjectStore(STORE);
    };
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
}

export async function saveCryptoState(name: string, state: Uint8Array): Promise<void> {
  const db = await openDb();
  try {
    await new Promise<void>((resolve, reject) => {
      const tx = db.transaction(STORE, "readwrite");
      // Store a fresh copy so the value is structured-cloned, not a view that
      // may be neutered by a later transfer.
      tx.objectStore(STORE).put(state.slice(), name);
      tx.oncomplete = () => resolve();
      tx.onerror = () => reject(tx.error);
    });
  } finally {
    db.close();
  }
}

export async function loadCryptoState(name: string): Promise<Uint8Array | null> {
  const db = await openDb();
  try {
    return await new Promise<Uint8Array | null>((resolve, reject) => {
      const tx = db.transaction(STORE, "readonly");
      const req = tx.objectStore(STORE).get(name);
      req.onsuccess = () => {
        const v = req.result as ArrayBuffer | Uint8Array | undefined;
        if (v == null) resolve(null);
        else resolve(v instanceof Uint8Array ? v : new Uint8Array(v));
      };
      req.onerror = () => reject(req.error);
    });
  } finally {
    db.close();
  }
}
```

- [ ] **Step 4: Run it, verify it passes**: `cd client && npx vitest run src/shared/lib/crypto/persistence.test.ts`. Expected: PASS.

- [ ] **Step 5: Commit**
```bash
git add client/src/shared/lib/crypto/persistence.ts client/src/shared/lib/crypto/persistence.test.ts client/package.json client/package-lock.json
git commit -m "feat(client): IndexedDB persistence store for crypto engine state"
```

### Task 4: Wire persistence into the crypto worker

**Files:**
- Modify: `client/src/shared/lib/crypto/worker.ts`
- Test: `client/src/shared/lib/crypto/worker.test.ts` (extend the existing test file; if none, create one)

Behavior: a dispatcher loads persisted state on first request (`WasmEngine.restore(blob)` if present, else `new WasmEngine(name)`), and after every **state-mutating** request it re-exports and saves. Read-only ops do not persist. `encrypt` and a `decrypt` that applies a commit/app message DO mutate the ratchet, so they persist.

- [ ] **Step 1: Write a failing test** that proves the dispatcher persists after a mutating op and rehydrates a fresh dispatcher. Mock the persistence module so the test is hermetic:
```ts
import { describe, it, expect, vi, beforeEach } from "vitest";

const store = new Map<string, Uint8Array>();
vi.mock("./persistence", () => ({
  loadCryptoState: vi.fn(async (name: string) => store.get(name) ?? null),
  saveCryptoState: vi.fn(async (name: string, b: Uint8Array) => { store.set(name, b.slice()); }),
}));

import { createDispatcher } from "./worker";

beforeEach(() => store.clear());

describe("crypto worker persistence", () => {
  it("persists engine state after creating a group and rehydrates it", async () => {
    const d1 = createDispatcher("alice@corp");
    const created = await d1.handleRequest({ id: 1, kind: "createGroup", groupId: "team-1" } as any);
    expect(created.ok).toBe(true);
    expect(store.has("alice@corp")).toBe(true);

    // A brand-new dispatcher (simulating reload) must load the saved state and
    // be able to encrypt to the existing group — NOT throw "unknown group".
    const d2 = createDispatcher("alice@corp");
    const enc = await d2.handleRequest({ id: 2, kind: "encrypt", groupId: "team-1", plaintext: new Uint8Array([1,2,3]) } as any);
    expect(enc.ok).toBe(true);
  });
});
```
(Match the exact `CryptoRequest` shape from `./protocol` — adjust field names/`id` type if needed.)

- [ ] **Step 2: Run it, verify it fails**: `cd client && npx vitest run src/shared/lib/crypto/worker.test.ts`. Expected: FAIL (`d2` throws `unknown group` because no persistence yet).

- [ ] **Step 3: Implement** the wiring in `worker.ts`. Add `import { loadCryptoState, saveCryptoState } from "./persistence";` at the top. Replace `createDispatcher`:
```ts
// Ops that never change engine state — skip the persist round-trip for these.
const READ_ONLY_KINDS = new Set([
  "keyPackage",
  "signingPublicKey",
  "exportGroupInfo",
]);

export function createDispatcher(name: string) {
  let engine: WasmEngine | null = null;
  let initEngine: Promise<WasmEngine> | null = null;

  async function getEngine(): Promise<WasmEngine> {
    if (engine) return engine;
    if (!initEngine) {
      initEngine = (async () => {
        if (!ready) ready = initWasm();
        await ready;
        const saved = await loadCryptoState(name);
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
```
Also: the worker entry at the bottom and the module-level `handleRequest` already delegate to `createDispatcher`, so they inherit persistence with no change. Remove the now-stale `PendingEngineOps` cast in `dispatchTo` only if Task 2's rebuild added `export_group_info`/`join_by_external_commit` to the `.d.ts` (otherwise leave it). Add `export_state(): Uint8Array;` and `static restore(state: Uint8Array): WasmEngine;` usage compiles against the regenerated `.d.ts`.

- [ ] **Step 4: Run the test, verify it passes**: `cd client && npx vitest run src/shared/lib/crypto/worker.test.ts`. Expected: PASS.

- [ ] **Step 5: Full client check**: `cd client && npx vitest run && npx tsc --noEmit`. Expected: all green.

- [ ] **Step 6: Commit**
```bash
git add client/src/shared/lib/crypto/worker.ts client/src/shared/lib/crypto/worker.test.ts
git commit -m "feat(client): persist+restore crypto engine state across reloads"
```

---

## Phase C — history backfill on load

### Task 5: `track(groupId, sinceSeq)` through the protocol layer

**Files:**
- Modify: `client/src/shared/lib/transport/connection.ts`
- Modify: `client/src/shared/lib/transport/protocolClient.ts`
- Modify: `client/src/shared/lib/transport/protocol.worker.ts`
- Modify: `client/src/app/orchestrator.ts` (`ProtocolPort` interface + expose `track`)
- Test: `client/src/shared/lib/transport/connection.test.ts` (extend existing)

- [ ] **Step 1: Write the failing test** for `ProtocolConnection.track` in `connection.test.ts`. It must prove: (a) tracking a group before connect causes a `sync` frame on open; (b) tracking after open sends a `sync` immediately. Use the existing fake `WebSocketLike` pattern in that test file. Sketch:
```ts
it("track() seeds a sync cursor sent on connect", () => {
  const sent: string[] = [];
  const fake = makeFakeSocket(sent); // reuse the file's existing helper
  const conn = new ProtocolConnection(() => fake, "tok");
  conn.track("g1", 0);
  conn.connect();
  fake.onopen!(); // simulate socket opening
  expect(sent.some((r) => JSON.parse(r).type === "sync" && JSON.parse(r).group_id === "g1")).toBe(true);
});

it("track() after open syncs immediately", () => {
  const sent: string[] = [];
  const fake = makeFakeSocket(sent);
  const conn = new ProtocolConnection(() => fake, "tok");
  conn.connect();
  fake.onopen!();
  sent.length = 0;
  conn.track("g2", 5);
  const f = sent.map((r) => JSON.parse(r)).find((x) => x.type === "sync");
  expect(f).toMatchObject({ type: "sync", group_id: "g2", since_seq: 5 });
});
```
(Adapt to the helpers/naming actually present in `connection.test.ts`.)

- [ ] **Step 2: Run it, verify it fails**: `cd client && npx vitest run src/shared/lib/transport/connection.test.ts`. Expected: FAIL (`track` undefined).

- [ ] **Step 3: Implement `track`** in `connection.ts`. Add a method and reuse `transmit` (which queues until open):
```ts
  track(groupID: string, sinceSeq: number) {
    this.cursors.set(groupID, sinceSeq);
    if (this.open) this.transmit({ type: "sync", group_id: groupID, since_seq: sinceSeq });
  }
```
The existing `onopen` already iterates `this.cursors` and emits `sync` for each, so groups tracked before open are covered; the `if (this.open)` branch covers post-open tracking.

- [ ] **Step 4: Run the test, verify it passes**. Expected: PASS.

- [ ] **Step 5: Thread `track` through the proxy + worker.** In `protocolClient.ts` add:
```ts
  track(groupID: string, sinceSeq: number) { this.worker.postMessage({ cmd: "track", groupID, sinceSeq }); }
```
In `protocol.worker.ts` add a case to the `switch`:
```ts
    case "track": conn?.track(d.groupID, d.sinceSeq); break;
```
In `orchestrator.ts`, add `track(groupID: string, sinceSeq: number): void;` to the `ProtocolPort` interface, and add a passthrough to the returned object:
```ts
    track(groupID: string, sinceSeq: number) { protocol.track(groupID, sinceSeq); },
```

- [ ] **Step 6: Typecheck + tests**: `cd client && npx tsc --noEmit && npx vitest run src/shared/lib/transport/`. Expected: green. (If `bootstrap.ts` constructs a `ProtocolPort` adapter object literal, add a `track` passthrough there too so it satisfies the interface — `tsc` will flag it if missing.)

- [ ] **Step 7: Commit**
```bash
git add client/src/shared/lib/transport client/src/app/orchestrator.ts
git commit -m "feat(client): protocol track() to seed sync cursors for history backfill"
```

### Task 6: Seed cursors for known conversations on load

**Files:**
- Modify: `client/src/app/conversations.ts` (track each group after `load`)
- Modify: `client/src/app/bootstrap.ts` (wire the orchestrator's `track` into the conversations controller deps)
- Test: `client/src/app/conversations.test.ts` (extend existing)

The `conversations` controller already knows every channel + dm group after `load()`. Give it a `track` dep and call it for each group so the protocol layer backfills history (the orchestrator's inbound handler decrypts + renders).

- [ ] **Step 1: Write the failing test** in `conversations.test.ts`. Extend the `deps()` helper to include `track: vi.fn()`, then assert `load()` tracks every group at seq 0:
```ts
it("load() tracks every conversation so history backfills", async () => {
  const d = deps([
    { group_id: "c1", type: "channel", visibility: "public", name: "general" },
    { group_id: "d1", type: "dm", visibility: "private", name: "bob" },
  ]);
  const c = createConversationsController(d as any);
  await c.load();
  expect(d.track).toHaveBeenCalledWith("c1", 0);
  expect(d.track).toHaveBeenCalledWith("d1", 0);
});
```
Add `track: vi.fn()` to the object returned by the `deps()` helper at the top of the file.

- [ ] **Step 2: Run it, verify it fails**: `cd client && npx vitest run src/app/conversations.test.ts`. Expected: FAIL.

- [ ] **Step 3: Implement.** In `conversations.ts`, add to `ConversationsControllerDeps`:
```ts
  // Seed a sync cursor so the server backfills this group's stored history.
  track(groupId: string, sinceSeq: number): void;
```
At the end of `load()` (after the channels/dms signals are set, before/after the auto-select block), track each group:
```ts
    for (const c of channels()) deps.track(c.id, 0);
    for (const m of dms()) deps.track(m.id, 0);
```

- [ ] **Step 4: Wire the dep in `bootstrap.ts`.** Where `createConversationsController({ ... })` is constructed, add `track: (groupId, sinceSeq) => orchestrator.track(groupId, sinceSeq),` to the deps object (the orchestrator is already in scope in bootstrap). Confirm by reading the existing controller construction site.

- [ ] **Step 5: Run tests + typecheck**: `cd client && npx vitest run src/app/conversations.test.ts && npx tsc --noEmit`. Expected: all green.

- [ ] **Step 6: Commit**
```bash
git add client/src/app/conversations.ts client/src/app/bootstrap.ts client/src/app/conversations.test.ts
git commit -m "feat(client): backfill conversation history by tracking groups on load"
```

---

## Phase D — full verify + live test

### Task 7: Build, test, and live-verify reload persistence

**Files:** none (verification only), plus a possible `backend/.env`-free run.

- [ ] **Step 1: Full automated verification.**
```bash
cd /Users/denisurevic/Documents/slack/crypto-core && cargo test
cd /Users/denisurevic/Documents/slack/client && npx vitest run && npx tsc --noEmit && npx vite build
cd /Users/denisurevic/Documents/slack/backend && go build ./... && go test ./...
```
Expected: all green. (If `backend` `TestExportVectors` rewrites `kt_vectors.json` and dirties the tree, `git checkout` that file afterward — known quirk.)

- [ ] **Step 2: Live test.** Ensure the stack is up (`docker compose` backend on :18080, `vite` dev on :5173). Log in as a seeded user, select #general, send a message, then **reload the page**. Verify via the browser:
  - the message is still visible after reload (history backfill),
  - the console shows NO `unknown group` error on send,
  - sending a new message after reload succeeds (network shows a `send` frame; no error).
  Drive the UI via Playwright `evaluate` + DOM (synthetic pointer clicks are unreliable on this layout — known issue).

- [ ] **Step 3: Report the live result** (what persisted, what didn't). Note any remaining limitation (e.g. cross-epoch history needs `max_past_epochs` — see self-review).

---

## Self-Review

**Coverage:** The "unknown group after reload" root cause is fixed by Phase A (engine state export/restore) + Phase B (IndexedDB persist/rehydrate in the worker). The "message disappears after reload" symptom is fixed by Phase C (seed sync cursors → server returns stored ciphertext → restored engine decrypts → orchestrator renders). Phase D verifies end to end. The backend already persists ciphertext and serves `sync` (confirmed) — no backend change needed.

**Placeholders:** none — exact APIs confirmed against the vendored crate sources (`MemoryStorage.values` public field; `SignatureKeyPair` serde derive + `from_raw`; `MlsGroup::load`). The one researched-at-implementation detail is the exact `wasm-pack` build invocation/out-dir (Task 2 Step 4 says to match the existing output path) and whether `fake-indexeddb` is already a devDependency (Task 3).

**Type consistency:** `export_state(): Uint8Array` / `static restore(state: Uint8Array): WasmEngine` (Rust → wasm-bindgen → `.d.ts`); `loadCryptoState`/`saveCryptoState(name, Uint8Array)`; `ProtocolConnection.track(groupID, sinceSeq)` ↔ `ProtocolClient.track` ↔ worker `cmd:"track"` ↔ `ProtocolPort.track` ↔ orchestrator `track` ↔ conversations dep `track(groupId, sinceSeq)`. Sync frame shape `{type:"sync", group_id, since_seq}` matches the existing `onopen` emitter and the Go gateway's `syncFrame`.

**Known limitation (document, don't fix here):** OpenMLS decrypts application messages from the current epoch by default; messages from *prior* epochs (after membership changes) need `max_past_epochs` raised in `MlsGroupCreateConfig`. For the immediate single-epoch case (create channel, send, reload) this is irrelevant. If cross-epoch history loss shows up in testing, a one-line `.max_past_epochs(N)` in `create_group`'s config is the follow-up (only affects groups created after the change).

**Risk:** persisting after every op adds an IndexedDB write per message — acceptable at chat volume; writes are fire-and-forget and wrapped so a persistence failure never breaks the live op. `serde_json` of the storage map is a few KB; fine. If `MlsGroup::load`'s storage error type doesn't fit `EngineError::mls`, Task 1 Step 5 gives the fallback constructor.
