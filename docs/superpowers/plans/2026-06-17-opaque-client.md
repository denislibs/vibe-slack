# OPAQUE Client + Device Onboarding Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make login real end-to-end — a Go `bytemare/opaque` client compiled to WASM (guaranteed wire-compatible with the AS server), a TypeScript auth flow over the AS HTTP API, device onboarding (generate an MLS device, register it, get a device-bound session), and the handoff that finally lets the frontend call `connect()`.

**Architecture:** The OPAQUE client is the SAME library the server uses (`bytemare/opaque` v0.18, `DefaultConfiguration`) compiled `GOOS=js GOARCH=wasm` — eliminating cross-implementation interop risk. Its client logic is split into pure Go functions (`client.go`, tested natively against a real `bytemare` Server for byte-level compatibility) and a thin `syscall/js` binding (`main.go`, wasm-only). A TS `opaqueClient` loads the wasm and exposes stepwise calls; a `features/authenticate` use-case orchestrates the stepwise OPAQUE messages with AS HTTP round-trips; `features/onboard-device` generates the device's MLS keys via the existing crypto worker and registers it; the session entity flips to `onboarded` and the orchestrator dials the DS WebSocket.

**Tech Stack:** Go 1.26 (`bytemare/opaque` v0.18 → js/wasm + `wasm_exec.js`), the existing Rust→WASM crypto worker (+ a new `signing_public_key()` export), SolidJS + Vite + FSD, vitest + jsdom. Reuses AS HTTP endpoints (`/auth/*`, `/devices`, `/keypackages`).

**Spec:** `docs/superpowers/specs/2026-06-17-opaque-client-design.md`. KT-client, client-side MLS group flows, screen styling, and session persistence are out of scope.

**Config/build risks (flagged inline):** (1) `bytemare/opaque` compiling to `GOOS=js GOARCH=wasm` — its deps are pure Go and should compile; the KSF (Argon2id) is memory-hard and slow in single-threaded wasm (acceptable: login is infrequent). (2) `wasm_exec.js` loading — it's Go's runtime glue (a classic script that assigns `globalThis.Go`); the node smoke test loads it via `createRequire` (no eval); browser loading via Vite is verified by `vite build` + the smoke path. All genuine interop is proven by the NATIVE Go round-trip test (Task 2), independent of the wasm packaging. No Docker needed.

---

## File Structure

```
client/src/shared/lib/auth/opaque-wasm/
  go.mod                    # module: bytemare/opaque dep (separate from backend module)
  client.go                 # PURE Go: RegInit/RegFinalize/LoginKE1/LoginKE3 + flowID state map
  client_test.go            # NATIVE interop test vs a real bytemare Server (the real proof)
  main.go                   # //go:build js && wasm — syscall/js bindings over client.go
  wasm_exec.js              # copied from $(go env GOROOT)/lib/wasm/wasm_exec.js
  opaque.wasm               # built artifact (committed, like the crypto wasm-pkg)
client/src/shared/lib/auth/
  opaqueClient.ts           # loads opaque.wasm + wasm_exec.js; stepwise TS API
  opaqueClient.smoke.test.ts# node smoke: wasm loads, funcs callable, flows independent
client/src/shared/api/
  as.ts                     # typed AS HTTP client (register/login/devices/keypackages)
  as.test.ts
client/src/features/authenticate/
  authenticate.ts           # register(email,pw) / login(email,pw) → session token
  authenticate.test.ts
client/src/features/onboard-device/
  onboardDevice.ts          # crypto signing key + keypackages → POST /devices → device-bound
  onboardDevice.test.ts
client/src/entities/session/
  store.ts                  # token, deviceId, status
  store.test.ts
client/src/widgets/login-form/LoginForm.tsx
client/src/pages/auth/AuthPage.tsx + AuthPage.test.tsx
client/src/app/{authFlow.ts, authFlow.test.ts, bootstrap.ts, App.tsx, main.tsx}
crypto-core/src/{engine.rs,lib.rs}        # + signing_public_key()
client/src/shared/lib/crypto/{protocol.ts,worker.ts,client.ts}  # + signingPublicKey
```

---

## Milestone 1 — Crypto device key + Go OPAQUE client (wasm) + interop

### Task 1: Expose the device signing public key from the crypto worker

**Files:**
- Modify: `crypto-core/src/engine.rs`, `crypto-core/src/lib.rs`, `client/src/shared/lib/crypto/protocol.ts`, `client/src/shared/lib/crypto/worker.ts`, `client/src/shared/lib/crypto/client.ts`, rebuild `client/src/shared/lib/crypto/wasm-pkg/`
- Test: `crypto-core/src/engine.rs` (Rust), `client/src/shared/lib/crypto/client.test.ts`

The onboarding `POST /devices` needs the device's Ed25519 signing public key. The MLS `Identity` holds `signer` (a `SignatureKeyPair`) which exposes the public key via `to_public_vec()` (used in `identity.rs`). Surface it.

- [ ] **Step 1: Write the failing Rust test**

Add to `crypto-core/src/engine.rs` `#[cfg(test)] mod tests`:
```rust
#[test]
fn engine_exposes_signing_public_key() {
    let e = Engine::new(b"alice@corp");
    let pk = e.signing_public_key();
    assert_eq!(pk.len(), 32, "Ed25519 public key is 32 bytes");
    assert_eq!(pk, e.signing_public_key()); // stable across calls
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd crypto-core && cargo test engine_exposes_signing_public_key`
Expected: FAIL — no method `signing_public_key`.

- [ ] **Step 3: Implement (Rust)**

In `crypto-core/src/engine.rs`, add to `impl Engine`:
```rust
/// The device's Ed25519 signing public key (the MLS credential's signature key),
/// for registering the device with the Authentication Service.
pub fn signing_public_key(&self) -> Vec<u8> {
    self.identity.signer.to_public_vec()
}
```
(If `Identity.signer` is private, add `pub fn signing_public_key(&self) -> Vec<u8> { self.signer.to_public_vec() }` on `Identity` and call it.)

In `crypto-core/src/lib.rs`, add to `#[wasm_bindgen] impl WasmEngine`:
```rust
pub fn signing_public_key(&self) -> Vec<u8> {
    self.inner.signing_public_key()
}
```

- [ ] **Step 4: Rust test passes + rebuild wasm**

Run: `cd crypto-core && cargo test engine_exposes_signing_public_key && cargo build --target wasm32-unknown-unknown`
Then rebuild the client wasm package:
```bash
cd crypto-core && wasm-pack build --target web --out-dir ../client/src/shared/lib/crypto/wasm-pkg
rm -f ../client/src/shared/lib/crypto/wasm-pkg/.gitignore   # wasm-pack regenerates `*` .gitignore; remove so the pkg stays committed
```
Confirm `client/src/shared/lib/crypto/wasm-pkg/crypto_core.d.ts` now lists `signing_public_key(): Uint8Array`.

- [ ] **Step 5: Extend the TS crypto worker contract**

`client/src/shared/lib/crypto/protocol.ts` — add to the `CryptoRequest` union: `| { id: string; kind: "signingPublicKey" }`.
`client/src/shared/lib/crypto/worker.ts` — in the dispatch switch(es) (both the default `handleRequest` and the `createDispatcher` factory), add:
```ts
      case "signingPublicKey":
        result = e.signing_public_key();
        break;
```
`client/src/shared/lib/crypto/client.ts` — add to `CryptoClient`:
```ts
  async signingPublicKey(): Promise<Uint8Array> {
    return (await this.send({ kind: "signingPublicKey" })) as Uint8Array;
  }
```
Add a test in `client/src/shared/lib/crypto/client.test.ts` (FakeWorker style already there) asserting `signingPublicKey()` round-trips a Uint8Array; extend the FakeWorker to return a 32-byte array for that kind.

- [ ] **Step 6: Verify + commit**

Run: `cd client && npx vitest run src/shared/lib/crypto/ && npx tsc --noEmit`
```bash
git -C /Users/denisurevic/Documents/slack add crypto-core/src client/src/shared/lib/crypto
git -C /Users/denisurevic/Documents/slack commit -m "feat(crypto): expose device signing public key (engine + wasm + TS client)"
```

---

### Task 2: Pure Go OPAQUE client + native interop test

**Files:**
- Create: `client/src/shared/lib/auth/opaque-wasm/go.mod`, `client.go`, `client_test.go`

The heart: the pure client logic + the proof it interops byte-for-byte with the server's library.

- [ ] **Step 1: Init the module + write the failing test**

Run: `cd client/src/shared/lib/auth/opaque-wasm && go mod init github.com/messenger/opaque-wasm && go get github.com/bytemare/opaque@v0.18.0`

`client/src/shared/lib/auth/opaque-wasm/client_test.go`:
```go
package main

import (
	"bytes"
	"testing"

	xopaque "github.com/bytemare/opaque"
)

// A bytemare Server with DefaultConfiguration + fresh key material, mirroring the AS.
func testServer(t *testing.T) *xopaque.Server {
	t.Helper()
	cfg := xopaque.DefaultConfiguration()
	sk, pk := cfg.KeyGen()
	srv, err := cfg.Server()
	if err != nil {
		t.Fatalf("server: %v", err)
	}
	priv := cfg.AKE.Group().NewScalar()
	if err := priv.Decode(sk.Encode()); err != nil {
		t.Fatalf("decode sk: %v", err)
	}
	skm := &xopaque.ServerKeyMaterial{
		PrivateKey: priv, PublicKeyBytes: pk.Encode(),
		OPRFGlobalSeed: cfg.GenerateOPRFSeed(), Identity: []byte("messenger-as"),
	}
	if err := srv.SetKeyMaterial(skm); err != nil {
		t.Fatalf("set key material: %v", err)
	}
	return srv
}

func TestClientInteropRegisterThenLogin(t *testing.T) {
	srv := testServer(t)
	credID := []byte("cred:alice@corp")
	serverID := []byte("messenger-as")
	password := []byte("correct horse battery staple")

	regFlow, reqBytes, err := RegInit(password)
	if err != nil {
		t.Fatalf("RegInit: %v", err)
	}
	req, err := srv.Deserialize.RegistrationRequest(reqBytes)
	if err != nil {
		t.Fatalf("server deserialize req: %v", err)
	}
	resp, err := srv.RegistrationResponse(req, credID, nil)
	if err != nil {
		t.Fatalf("server RegistrationResponse: %v", err)
	}
	recordBytes, _, err := RegFinalize(regFlow, resp.Serialize(), serverID)
	if err != nil {
		t.Fatalf("RegFinalize: %v", err)
	}

	loginFlow, ke1Bytes, err := LoginKE1(password)
	if err != nil {
		t.Fatalf("LoginKE1: %v", err)
	}
	ke1, _ := srv.Deserialize.KE1(ke1Bytes)
	rec, _ := srv.Deserialize.RegistrationRecord(recordBytes)
	cr := &xopaque.ClientRecord{RegistrationRecord: rec, CredentialIdentifier: credID}
	ke2, serverOut, err := srv.GenerateKE2(ke1, cr)
	if err != nil {
		t.Fatalf("server GenerateKE2: %v", err)
	}
	ke3Bytes, clientSession, err := LoginKE3(loginFlow, ke2.Serialize(), serverID)
	if err != nil {
		t.Fatalf("LoginKE3: %v", err)
	}
	ke3, _ := srv.Deserialize.KE3(ke3Bytes)
	if err := srv.LoginFinish(ke3, serverOut.ClientMAC); err != nil {
		t.Fatalf("server LoginFinish: %v", err)
	}
	if !bytes.Equal(clientSession, serverOut.SessionSecret) {
		t.Fatal("client and server session keys must match — interop proven")
	}
}

func TestWrongPasswordFails(t *testing.T) {
	srv := testServer(t)
	credID := []byte("cred:bob@corp")
	serverID := []byte("messenger-as")

	regFlow, reqBytes, _ := RegInit([]byte("right"))
	req, _ := srv.Deserialize.RegistrationRequest(reqBytes)
	resp, _ := srv.RegistrationResponse(req, credID, nil)
	recordBytes, _, _ := RegFinalize(regFlow, resp.Serialize(), serverID)

	loginFlow, ke1Bytes, _ := LoginKE1([]byte("WRONG"))
	ke1, _ := srv.Deserialize.KE1(ke1Bytes)
	rec, _ := srv.Deserialize.RegistrationRecord(recordBytes)
	ke2, serverOut, _ := srv.GenerateKE2(ke1, &xopaque.ClientRecord{RegistrationRecord: rec, CredentialIdentifier: credID})
	ke3Bytes, _, err := LoginKE3(loginFlow, ke2.Serialize(), serverID)
	if err != nil {
		return // client-side failure acceptable
	}
	ke3, _ := srv.Deserialize.KE3(ke3Bytes)
	if srv.LoginFinish(ke3, serverOut.ClientMAC) == nil {
		t.Fatal("wrong password must fail at LoginFinish")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd client/src/shared/lib/auth/opaque-wasm && go test ./...`
Expected: FAIL — `RegInit` etc. undefined.

- [ ] **Step 3: Write the pure client (`client.go`)**

`client/src/shared/lib/auth/opaque-wasm/client.go`:
```go
package main

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"

	xopaque "github.com/bytemare/opaque"
)

// State for in-flight OPAQUE flows. The bytemare Client is stateful between
// Init→Finalize and KE1→KE3, so we keep the instance keyed by a flow id.
var (
	mu      sync.Mutex
	clients = map[string]*xopaque.Client{}
)

func newFlowID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func putClient(id string, c *xopaque.Client) { mu.Lock(); clients[id] = c; mu.Unlock() }
func takeClient(id string) (*xopaque.Client, bool) {
	mu.Lock()
	c, ok := clients[id]
	delete(clients, id)
	mu.Unlock()
	return c, ok
}

func newClient() (*xopaque.Client, error) { return xopaque.DefaultConfiguration().Client() }

// RegInit starts registration: returns a flow id and the serialized RegistrationRequest.
func RegInit(password []byte) (flowID string, request []byte, err error) {
	c, err := newClient()
	if err != nil {
		return "", nil, err
	}
	req, err := c.RegistrationInit(password)
	if err != nil {
		return "", nil, err
	}
	id, err := newFlowID()
	if err != nil {
		return "", nil, err
	}
	putClient(id, c)
	return id, req.Serialize(), nil
}

// RegFinalize completes registration from the server's RegistrationResponse.
func RegFinalize(flowID string, responseBytes, serverID []byte) (record []byte, exportKey []byte, err error) {
	c, ok := takeClient(flowID)
	if !ok {
		return nil, nil, errors.New("unknown flow id")
	}
	resp, err := c.Deserialize.RegistrationResponse(responseBytes)
	if err != nil {
		return nil, nil, err
	}
	rec, ek, err := c.RegistrationFinalize(resp, nil, serverID)
	if err != nil {
		return nil, nil, err
	}
	return rec.Serialize(), ek, nil
}

// LoginKE1 starts login: returns a flow id and the serialized KE1.
func LoginKE1(password []byte) (flowID string, ke1 []byte, err error) {
	c, err := newClient()
	if err != nil {
		return "", nil, err
	}
	m, err := c.GenerateKE1(password)
	if err != nil {
		return "", nil, err
	}
	id, err := newFlowID()
	if err != nil {
		return "", nil, err
	}
	putClient(id, c)
	return id, m.Serialize(), nil
}

// LoginKE3 completes login from the server's KE2, returning the serialized KE3 and
// the client session key (== the server's session secret on success).
func LoginKE3(flowID string, ke2Bytes, serverID []byte) (ke3 []byte, sessionKey []byte, err error) {
	c, ok := takeClient(flowID)
	if !ok {
		return nil, nil, errors.New("unknown flow id")
	}
	ke2, err := c.Deserialize.KE2(ke2Bytes)
	if err != nil {
		return nil, nil, err
	}
	m, sessionKey, _, err := c.GenerateKE3(ke2, nil, serverID)
	if err != nil {
		return nil, nil, err
	}
	return m.Serialize(), sessionKey, nil
}
```
**API-version risk:** the `bytemare/opaque` v0.18 client/server methods used here are taken from the AS implementation (`backend/internal/opaque/opaque.go`), which already uses them in production. If anything differs, mirror exactly what `backend/internal/opaque/opaque.go` does and report.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd client/src/shared/lib/auth/opaque-wasm && go test ./...`
Expected: PASS — register+login round-trip, client session key == server session secret, wrong password fails.

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add client/src/shared/lib/auth/opaque-wasm/go.mod client/src/shared/lib/auth/opaque-wasm/go.sum client/src/shared/lib/auth/opaque-wasm/client.go client/src/shared/lib/auth/opaque-wasm/client_test.go
git -C /Users/denisurevic/Documents/slack commit -m "feat(client): pure Go OPAQUE client + native interop test vs bytemare Server"
```

---

### Task 3: WASM bindings + build + node smoke test

**Files:**
- Create: `client/src/shared/lib/auth/opaque-wasm/main.go`, copy `wasm_exec.js`, build `opaque.wasm`, `client/src/shared/lib/auth/opaqueClient.ts`, `client/src/shared/lib/auth/opaqueClient.smoke.test.ts`

- [ ] **Step 1: Write the js bindings (`main.go`, wasm-only)**

`client/src/shared/lib/auth/opaque-wasm/main.go`:
```go
//go:build js && wasm

package main

import (
	"encoding/base64"
	"syscall/js"
)

func b64d(s string) []byte { b, _ := base64.StdEncoding.DecodeString(s); return b }
func b64e(b []byte) string { return base64.StdEncoding.EncodeToString(b) }
func errObj(err error) any { return map[string]any{"error": err.Error()} }

func jsRegInit(_ js.Value, args []js.Value) any {
	flow, req, err := RegInit(b64d(args[0].String()))
	if err != nil {
		return errObj(err)
	}
	return map[string]any{"flowId": flow, "request": b64e(req)}
}
func jsRegFinalize(_ js.Value, args []js.Value) any {
	rec, ek, err := RegFinalize(args[0].String(), b64d(args[1].String()), b64d(args[2].String()))
	if err != nil {
		return errObj(err)
	}
	return map[string]any{"record": b64e(rec), "exportKey": b64e(ek)}
}
func jsLoginKE1(_ js.Value, args []js.Value) any {
	flow, ke1, err := LoginKE1(b64d(args[0].String()))
	if err != nil {
		return errObj(err)
	}
	return map[string]any{"flowId": flow, "ke1": b64e(ke1)}
}
func jsLoginKE3(_ js.Value, args []js.Value) any {
	ke3, sk, err := LoginKE3(args[0].String(), b64d(args[1].String()), b64d(args[2].String()))
	if err != nil {
		return errObj(err)
	}
	return map[string]any{"ke3": b64e(ke3), "sessionKey": b64e(sk)}
}

func main() {
	js.Global().Set("opaqueRegInit", js.FuncOf(jsRegInit))
	js.Global().Set("opaqueRegFinalize", js.FuncOf(jsRegFinalize))
	js.Global().Set("opaqueLoginKE1", js.FuncOf(jsLoginKE1))
	js.Global().Set("opaqueLoginKE3", js.FuncOf(jsLoginKE3))
	select {} // keep the Go runtime alive so the exported funcs remain callable
}
```
All byte args/results are base64 strings (matches the AS HTTP contract). `serverID` is passed base64 too — the TS layer base64-encodes `"messenger-as"`.

- [ ] **Step 2: Build the wasm + copy wasm_exec.js**

Run:
```bash
cd client/src/shared/lib/auth/opaque-wasm
GOOS=js GOARCH=wasm go build -o opaque.wasm .
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" ./wasm_exec.js
```
NOTE (verify-spot): on Go 1.26 `wasm_exec.js` is at `$(go env GOROOT)/lib/wasm/wasm_exec.js`; if absent there try `$(go env GOROOT)/misc/wasm/wasm_exec.js`. Report the path used. If `go build` fails compiling `bytemare/opaque` for js/wasm, report the exact error — do not work around silently.

- [ ] **Step 3: Write the TS loader + smoke test**

`client/src/shared/lib/auth/opaqueClient.ts`:
```ts
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

// initOpaque instantiates the Go runtime + wasm. `newGo`/`loadWasm` are injected so
// node and browser each supply their own wasm-loading + wasm_exec.js wiring.
export async function initOpaque(
  newGo: () => { importObject: WebAssembly.Imports; run(i: WebAssembly.Instance): void },
  loadWasm: (importObject: WebAssembly.Imports) => Promise<WebAssembly.Instance>,
): Promise<OpaqueGlobals> {
  if (ready) return ready;
  ready = (async () => {
    const go = newGo();
    const instance = await loadWasm(go.importObject);
    go.run(instance); // non-blocking: Go's event loop yields; globals are set during run
    const g = globalThis as unknown as OpaqueGlobals;
    return {
      opaqueRegInit: g.opaqueRegInit.bind(globalThis),
      opaqueRegFinalize: g.opaqueRegFinalize.bind(globalThis),
      opaqueLoginKE1: g.opaqueLoginKE1.bind(globalThis),
      opaqueLoginKE3: g.opaqueLoginKE3.bind(globalThis),
    };
  })();
  return ready;
}

function unwrap<T>(r: T | { error: string }): T {
  if (r && typeof r === "object" && "error" in r) throw new Error((r as { error: string }).error);
  return r as T;
}

// Thin typed wrappers used by the authenticate feature.
export const OpaqueOps = {
  regInit: (g: OpaqueGlobals, pw: string) => unwrap(g.opaqueRegInit(pw)),
  regFinalize: (g: OpaqueGlobals, flow: string, resp: string, srvId: string) => unwrap(g.opaqueRegFinalize(flow, resp, srvId)),
  loginKE1: (g: OpaqueGlobals, pw: string) => unwrap(g.opaqueLoginKE1(pw)),
  loginKE3: (g: OpaqueGlobals, flow: string, ke2: string, srvId: string) => unwrap(g.opaqueLoginKE3(flow, ke2, srvId)),
};
```

`client/src/shared/lib/auth/opaqueClient.smoke.test.ts`:
```ts
// @vitest-environment node
import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { createRequire } from "node:module";
import { initOpaque, OpaqueOps } from "./opaqueClient";

// Load Go's wasm_exec.js (a classic script that assigns globalThis.Go) via require —
// no eval. require() executes the script's side effects, setting globalThis.Go.
function nodeLoaders() {
  const dir = fileURLToPath(new URL("./opaque-wasm/", import.meta.url));
  const require = createRequire(import.meta.url);
  require(dir + "wasm_exec.js");
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
```
NOTE (verify-spot): `createRequire(...)(wasm_exec.js)` executes Go's classic script and sets `globalThis.Go` without eval. If Go 1.26's `wasm_exec.js` is published as ESM or references browser-only globals that throw in node, wrap it in a tiny `.cjs` shim or run this test under a browser-ish env. The crypto contract is already proven by Task 2's native test — if wasm-in-node proves intractable, downgrade this to a build-only assertion (`opaque.wasm` exists and is non-empty) and report, but try the real load first. Do NOT use `eval`/`new Function`.

- [ ] **Step 4: Run smoke test + commit**

Run: `cd client && npx vitest run src/shared/lib/auth/opaqueClient.smoke.test.ts`
Expected: PASS (wasm loads, two flows have distinct ids, non-empty messages).
```bash
git -C /Users/denisurevic/Documents/slack add client/src/shared/lib/auth/opaque-wasm/main.go client/src/shared/lib/auth/opaque-wasm/wasm_exec.js client/src/shared/lib/auth/opaque-wasm/opaque.wasm client/src/shared/lib/auth/opaqueClient.ts client/src/shared/lib/auth/opaqueClient.smoke.test.ts
git -C /Users/denisurevic/Documents/slack commit -m "feat(client): OPAQUE wasm bindings + build + node smoke test"
```

---

## Milestone 2 — AS HTTP client + authenticate feature

### Task 4: Typed AS HTTP client

**Files:**
- Create: `client/src/shared/api/as.ts`, `client/src/shared/api/as.test.ts`

- [ ] **Step 1: Write the failing test (fetch mock)**

`client/src/shared/api/as.test.ts`:
```ts
import { describe, it, expect, vi } from "vitest";
import { AsClient } from "./as";

function mockFetch(routes: Record<string, { status: number; body: unknown }>) {
  return vi.fn(async (url: string, init?: RequestInit) => {
    const key = `${init?.method ?? "GET"} ${new URL(url).pathname}`;
    const r = routes[key];
    if (!r) throw new Error("no route " + key);
    return { status: r.status, ok: r.status < 400, json: async () => r.body } as Response;
  });
}

describe("AsClient", () => {
  it("register start returns the opaque response", async () => {
    const as = new AsClient("http://as.test", mockFetch({
      "POST /auth/register/start": { status: 200, body: { opaque_registration_response: "RESP" } },
      "POST /auth/register/finish": { status: 200, body: { ok: true } },
    }) as any);
    expect(await as.registerStart("a@corp", "REQ")).toBe("RESP");
    await as.registerFinish("a@corp", "RECORD");
  });

  it("login finish returns token + enroll flag", async () => {
    const as = new AsClient("http://as.test", mockFetch({
      "POST /auth/login/start": { status: 200, body: { login_id: "L1", ke2: "KE2" } },
      "POST /auth/login/finish": { status: 200, body: { session_token: "TOK", device_enroll_required: true } },
    }) as any);
    const { loginId, ke2 } = await as.loginStart("a@corp", "KE1");
    expect(loginId).toBe("L1");
    expect(ke2).toBe("KE2");
    const fin = await as.loginFinish(loginId, "KE3");
    expect(fin).toEqual({ sessionToken: "TOK", deviceEnrollRequired: true });
  });

  it("throws on non-2xx", async () => {
    const as = new AsClient("http://as.test", mockFetch({
      "POST /auth/login/start": { status: 401, body: { error: "auth_failed" } },
    }) as any);
    await expect(as.loginStart("a@corp", "KE1")).rejects.toThrow();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd client && npx vitest run src/shared/api/as.test.ts`
Expected: FAIL — cannot find `./as`.

- [ ] **Step 3: Write minimal implementation**

`client/src/shared/api/as.ts`:
```ts
type FetchFn = typeof fetch;

// Thin typed wrapper over the Authentication Service HTTP API. All OPAQUE byte fields
// are base64 strings (matches the server contract).
export class AsClient {
  constructor(private baseURL: string, private fetchFn: FetchFn = fetch) {}

  private async post<T>(path: string, body: unknown, token?: string): Promise<T> {
    const headers: Record<string, string> = { "Content-Type": "application/json" };
    if (token) headers.Authorization = `Bearer ${token}`;
    const res = await this.fetchFn(this.baseURL + path, { method: "POST", headers, body: JSON.stringify(body) });
    if (!res.ok) {
      let code = "error";
      try { code = ((await res.json()) as { error?: string }).error ?? code; } catch { /* ignore */ }
      throw new Error(`AS ${path} failed: ${res.status} ${code}`);
    }
    return (await res.json()) as T;
  }

  async registerStart(email: string, opaqueRegistrationRequest: string): Promise<string> {
    const r = await this.post<{ opaque_registration_response: string }>(
      "/auth/register/start", { email, opaque_registration_request: opaqueRegistrationRequest });
    return r.opaque_registration_response;
  }
  async registerFinish(email: string, record: string): Promise<void> {
    await this.post("/auth/register/finish", { email, opaque_registration_record: record });
  }
  async loginStart(email: string, ke1: string): Promise<{ loginId: string; ke2: string }> {
    const r = await this.post<{ login_id: string; ke2: string }>("/auth/login/start", { email, ke1 });
    return { loginId: r.login_id, ke2: r.ke2 };
  }
  async loginFinish(loginId: string, ke3: string): Promise<{ sessionToken: string; deviceEnrollRequired: boolean }> {
    const r = await this.post<{ session_token: string; device_enroll_required: boolean }>(
      "/auth/login/finish", { login_id: loginId, ke3 });
    return { sessionToken: r.session_token, deviceEnrollRequired: r.device_enroll_required };
  }
  async enrollDevice(token: string, signingPublicKey: string, label: string, initialKeyPackages: string[]): Promise<string> {
    const r = await this.post<{ device_id: string }>(
      "/devices", { signing_public_key: signingPublicKey, label, initial_key_packages: initialKeyPackages }, token);
    return r.device_id;
  }
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd client && npx vitest run src/shared/api/as.test.ts`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add client/src/shared/api/as.ts client/src/shared/api/as.test.ts
git -C /Users/denisurevic/Documents/slack commit -m "feat(client): typed AS HTTP client"
```

---

### Task 5: authenticate feature (register/login orchestration)

**Files:**
- Create: `client/src/features/authenticate/authenticate.ts`, `authenticate.test.ts`

- [ ] **Step 1: Write the failing test**

`client/src/features/authenticate/authenticate.test.ts`:
```ts
import { describe, it, expect, vi } from "vitest";
import { createAuthenticator, type OpaqueOpsLike, type AsLike } from "./authenticate";

const SERVER_ID_B64 = btoa("messenger-as");

function fakes() {
  const opaque: OpaqueOpsLike = {
    regInit: vi.fn(() => ({ flowId: "f1", request: "REQ" })),
    regFinalize: vi.fn(() => ({ record: "RECORD", exportKey: "EK" })),
    loginKE1: vi.fn(() => ({ flowId: "f2", ke1: "KE1" })),
    loginKE3: vi.fn(() => ({ ke3: "KE3", sessionKey: "SK" })),
  };
  const as: AsLike = {
    registerStart: vi.fn(async () => "RESP"),
    registerFinish: vi.fn(async () => {}),
    loginStart: vi.fn(async () => ({ loginId: "L1", ke2: "KE2" })),
    loginFinish: vi.fn(async () => ({ sessionToken: "TOK", deviceEnrollRequired: true })),
  };
  return { opaque, as };
}

describe("authenticate", () => {
  it("register runs init→start→finalize→finish in order", async () => {
    const { opaque, as } = fakes();
    await createAuthenticator(opaque, as).register("a@corp", "pw");
    expect(as.registerStart).toHaveBeenCalledWith("a@corp", "REQ");
    expect(opaque.regFinalize).toHaveBeenCalledWith("f1", "RESP", SERVER_ID_B64);
    expect(as.registerFinish).toHaveBeenCalledWith("a@corp", "RECORD");
  });

  it("login returns token + enroll flag", async () => {
    const { opaque, as } = fakes();
    const r = await createAuthenticator(opaque, as).login("a@corp", "pw");
    expect(as.loginStart).toHaveBeenCalledWith("a@corp", "KE1");
    expect(opaque.loginKE3).toHaveBeenCalledWith("f2", "KE2", SERVER_ID_B64);
    expect(r).toEqual({ sessionToken: "TOK", deviceEnrollRequired: true });
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd client && npx vitest run src/features/authenticate/authenticate.test.ts`
Expected: FAIL — cannot find `./authenticate`.

- [ ] **Step 3: Write minimal implementation**

`client/src/features/authenticate/authenticate.ts`:
```ts
// The OPAQUE server identity is fixed by the AS contract.
const SERVER_ID_B64 = btoa("messenger-as");
// Passwords are base64-encoded for the wasm boundary (it expects base64 bytes).
const pwB64 = (pw: string) => btoa(unescape(encodeURIComponent(pw)));

// Narrow ports so the feature tests with fakes (no real wasm/HTTP).
export interface OpaqueOpsLike {
  regInit(pwB64: string): { flowId: string; request: string };
  regFinalize(flowId: string, respB64: string, serverIdB64: string): { record: string; exportKey: string };
  loginKE1(pwB64: string): { flowId: string; ke1: string };
  loginKE3(flowId: string, ke2B64: string, serverIdB64: string): { ke3: string; sessionKey: string };
}
export interface AsLike {
  registerStart(email: string, request: string): Promise<string>;
  registerFinish(email: string, record: string): Promise<void>;
  loginStart(email: string, ke1: string): Promise<{ loginId: string; ke2: string }>;
  loginFinish(loginId: string, ke3: string): Promise<{ sessionToken: string; deviceEnrollRequired: boolean }>;
}

export function createAuthenticator(opaque: OpaqueOpsLike, as: AsLike) {
  return {
    async register(email: string, password: string): Promise<void> {
      const init = opaque.regInit(pwB64(password));
      const response = await as.registerStart(email, init.request);
      const fin = opaque.regFinalize(init.flowId, response, SERVER_ID_B64);
      await as.registerFinish(email, fin.record);
    },
    async login(email: string, password: string): Promise<{ sessionToken: string; deviceEnrollRequired: boolean }> {
      const ke1 = opaque.loginKE1(pwB64(password));
      const start = await as.loginStart(email, ke1.ke1);
      const ke3 = opaque.loginKE3(ke1.flowId, start.ke2, SERVER_ID_B64);
      return as.loginFinish(start.loginId, ke3.ke3);
    },
  };
}
```
Note: `regInit`/`loginKE1` take a base64 password (the wasm decodes it). The app layer partially-applies the loaded `OpaqueGlobals` into an `OpaqueOpsLike`, keeping this feature wasm-agnostic and testable.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd client && npx vitest run src/features/authenticate/authenticate.test.ts`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add client/src/features/authenticate
git -C /Users/denisurevic/Documents/slack commit -m "feat(client): authenticate feature (OPAQUE register/login orchestration)"
```

---

## Milestone 3 — Session, onboarding, UI, wiring

### Task 6: Session entity store

**Files:**
- Create: `client/src/entities/session/store.ts`, `store.test.ts`

- [ ] **Step 1: Write the failing test**

`client/src/entities/session/store.test.ts`:
```ts
import { describe, it, expect } from "vitest";
import { createSessionStore } from "./store";

describe("session store", () => {
  it("transitions anonymous → authenticated → onboarded", () => {
    const s = createSessionStore();
    expect(s.status()).toBe("anonymous");
    expect(s.token()).toBe("");
    s.authenticated("TOK");
    expect(s.status()).toBe("authenticated");
    expect(s.token()).toBe("TOK");
    s.onboarded("DEV1");
    expect(s.status()).toBe("onboarded");
    expect(s.deviceId()).toBe("DEV1");
  });

  it("clear() resets to anonymous", () => {
    const s = createSessionStore();
    s.authenticated("TOK");
    s.clear();
    expect(s.status()).toBe("anonymous");
    expect(s.token()).toBe("");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd client && npx vitest run src/entities/session/store.test.ts`
Expected: FAIL — cannot find `./store`.

- [ ] **Step 3: Write minimal implementation**

`client/src/entities/session/store.ts`:
```ts
import { createSignal } from "solid-js";

export type SessionStatus = "anonymous" | "authenticated" | "onboarded";

// Session token kept in memory (not localStorage) to limit XSS token theft.
export function createSessionStore() {
  const [status, setStatus] = createSignal<SessionStatus>("anonymous");
  const [token, setToken] = createSignal("");
  const [deviceId, setDeviceId] = createSignal("");
  return {
    status, token, deviceId,
    authenticated(t: string) { setToken(t); setStatus("authenticated"); },
    onboarded(dev: string) { setDeviceId(dev); setStatus("onboarded"); },
    clear() { setToken(""); setDeviceId(""); setStatus("anonymous"); },
  };
}

export type SessionStore = ReturnType<typeof createSessionStore>;
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd client && npx vitest run src/entities/session/store.test.ts`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add client/src/entities/session
git -C /Users/denisurevic/Documents/slack commit -m "feat(client): session entity store"
```

---

### Task 7: onboard-device feature

**Files:**
- Create: `client/src/features/onboard-device/onboardDevice.ts`, `onboardDevice.test.ts`

- [ ] **Step 1: Write the failing test**

`client/src/features/onboard-device/onboardDevice.test.ts`:
```ts
import { describe, it, expect, vi } from "vitest";
import { onboardDevice, type CryptoLike, type EnrollLike } from "./onboardDevice";

function b64(bytes: number[]) { return btoa(String.fromCharCode(...bytes)); }

describe("onboardDevice", () => {
  it("collects signing key + key packages and enrolls, returning device id", async () => {
    const crypto: CryptoLike = {
      signingPublicKey: vi.fn(async () => new Uint8Array([1, 2, 3])),
      keyPackage: vi.fn(async () => new Uint8Array([9])),
    };
    const enroll: EnrollLike = {
      enrollDevice: vi.fn(async (_t, pub, label, kps) => {
        expect(pub).toBe(b64([1, 2, 3]));
        expect(label.length).toBeGreaterThan(0);
        expect(kps.length).toBe(3);
        return "DEVICE-1";
      }),
    };
    const id = await onboardDevice({ crypto, enroll, token: "TOK", label: "web", poolSize: 3 });
    expect(id).toBe("DEVICE-1");
    expect(crypto.keyPackage).toHaveBeenCalledTimes(3);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd client && npx vitest run src/features/onboard-device/onboardDevice.test.ts`
Expected: FAIL — cannot find `./onboardDevice`.

- [ ] **Step 3: Write minimal implementation**

`client/src/features/onboard-device/onboardDevice.ts`:
```ts
function bytesToB64(b: Uint8Array): string {
  let s = "";
  for (const x of b) s += String.fromCharCode(x);
  return btoa(s);
}

export interface CryptoLike {
  signingPublicKey(): Promise<Uint8Array>;
  keyPackage(): Promise<Uint8Array>;
}
export interface EnrollLike {
  enrollDevice(token: string, signingPublicKey: string, label: string, initialKeyPackages: string[]): Promise<string>;
}
export interface OnboardArgs {
  crypto: CryptoLike;
  enroll: EnrollLike;
  token: string;
  label: string;
  poolSize: number;
}

// Generates the device's signing key + a pool of one-time KeyPackages and registers
// the device with the AS, binding it to the session. Returns the new device id.
export async function onboardDevice(args: OnboardArgs): Promise<string> {
  const pub = bytesToB64(await args.crypto.signingPublicKey());
  const packages: string[] = [];
  for (let i = 0; i < args.poolSize; i++) {
    packages.push(bytesToB64(await args.crypto.keyPackage()));
  }
  return args.enroll.enrollDevice(args.token, pub, args.label, packages);
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd client && npx vitest run src/features/onboard-device/onboardDevice.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add client/src/features/onboard-device
git -C /Users/denisurevic/Documents/slack commit -m "feat(client): onboard-device feature (device key + keypackages → enroll)"
```

---

### Task 8: Login form widget + auth page

**Files:**
- Create: `client/src/widgets/login-form/LoginForm.tsx`, `client/src/pages/auth/AuthPage.tsx`, `AuthPage.test.tsx`

- [ ] **Step 1: Write the failing test**

`client/src/pages/auth/AuthPage.test.tsx`:
```tsx
// @vitest-environment jsdom
import { render, fireEvent } from "@solidjs/testing-library";
import { describe, it, expect, vi } from "vitest";
import { AuthPage } from "./AuthPage";

describe("AuthPage", () => {
  it("submits email+password to onLogin", () => {
    const onLogin = vi.fn();
    const { getByLabelText, getByText } = render(() => (
      <AuthPage onLogin={onLogin} onRegister={vi.fn()} error="" busy={false} />
    ));
    fireEvent.input(getByLabelText("Email"), { target: { value: "a@corp" } });
    fireEvent.input(getByLabelText("Password"), { target: { value: "pw" } });
    fireEvent.click(getByText("Log in"));
    expect(onLogin).toHaveBeenCalledWith("a@corp", "pw");
  });

  it("shows an error message", () => {
    const { getByText } = render(() => (
      <AuthPage onLogin={vi.fn()} onRegister={vi.fn()} error="invalid email or password" busy={false} />
    ));
    expect(getByText("invalid email or password")).toBeTruthy();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd client && npx vitest run src/pages/auth/AuthPage.test.tsx`
Expected: FAIL — cannot find `./AuthPage`.

- [ ] **Step 3: Write minimal implementation**

`client/src/widgets/login-form/LoginForm.tsx`:
```tsx
import { createSignal, Show, type Component } from "solid-js";
import { Button } from "../../shared/ui";

// Presentational auth form: collects credentials, delegates via callbacks. No logic.
export const LoginForm: Component<{
  onLogin: (email: string, password: string) => void;
  onRegister: (email: string, password: string) => void;
  error: string;
  busy: boolean;
}> = (props) => {
  const [email, setEmail] = createSignal("");
  const [password, setPassword] = createSignal("");
  return (
    <form onSubmit={(e) => e.preventDefault()}>
      <label>Email<input aria-label="Email" type="email" value={email()} onInput={(e) => setEmail(e.currentTarget.value)} /></label>
      <label>Password<input aria-label="Password" type="password" value={password()} onInput={(e) => setPassword(e.currentTarget.value)} /></label>
      <Show when={props.error}><p role="alert">{props.error}</p></Show>
      <Button disabled={props.busy} onClick={() => props.onLogin(email(), password())}>Log in</Button>
      <Button disabled={props.busy} onClick={() => props.onRegister(email(), password())}>Register</Button>
    </form>
  );
};
```
`client/src/pages/auth/AuthPage.tsx`:
```tsx
import type { Component } from "solid-js";
import { LoginForm } from "../../widgets/login-form/LoginForm";

export const AuthPage: Component<{
  onLogin: (email: string, password: string) => void;
  onRegister: (email: string, password: string) => void;
  error: string;
  busy: boolean;
}> = (props) => (
  <div>
    <h1>Sign in</h1>
    <LoginForm onLogin={props.onLogin} onRegister={props.onRegister} error={props.error} busy={props.busy} />
  </div>
);
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd client && npx vitest run src/pages/auth/AuthPage.test.tsx`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add client/src/widgets/login-form client/src/pages/auth
git -C /Users/denisurevic/Documents/slack commit -m "feat(client): login form widget + auth page (presentational)"
```

---

### Task 9: Auth flow (authenticate → onboard → connect)

**Files:**
- Create: `client/src/app/authFlow.ts`, `authFlow.test.ts`

- [ ] **Step 1: Write the failing test**

`client/src/app/authFlow.test.ts`:
```ts
import { describe, it, expect, vi } from "vitest";
import { createAuthFlow } from "./authFlow";
import { createSessionStore } from "../entities/session/store";

function deps() {
  return {
    authenticator: {
      register: vi.fn(async () => {}),
      login: vi.fn(async () => ({ sessionToken: "TOK", deviceEnrollRequired: true })),
    },
    onboard: vi.fn(async () => "DEV1"),
    connect: vi.fn(),
    session: createSessionStore(),
  };
}

describe("authFlow", () => {
  it("login → authenticated → onboard → onboarded → connect(token)", async () => {
    const d = deps();
    await createAuthFlow(d as any).login("a@corp", "pw");
    expect(d.session.status()).toBe("onboarded");
    expect(d.session.token()).toBe("TOK");
    expect(d.session.deviceId()).toBe("DEV1");
    expect(d.onboard).toHaveBeenCalledWith("TOK");
    expect(d.connect).toHaveBeenCalledWith("TOK");
  });

  it("skips onboarding when device_enroll_required is false", async () => {
    const d = deps();
    d.authenticator.login = vi.fn(async () => ({ sessionToken: "TOK", deviceEnrollRequired: false }));
    await createAuthFlow(d as any).login("a@corp", "pw");
    expect(d.onboard).not.toHaveBeenCalled();
    expect(d.connect).toHaveBeenCalledWith("TOK");
    expect(d.session.status()).toBe("authenticated");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd client && npx vitest run src/app/authFlow.test.ts`
Expected: FAIL — cannot find `./authFlow`.

- [ ] **Step 3: Write minimal implementation**

`client/src/app/authFlow.ts`:
```ts
import type { SessionStore } from "../entities/session/store";

export interface AuthFlowDeps {
  authenticator: {
    register(email: string, password: string): Promise<void>;
    login(email: string, password: string): Promise<{ sessionToken: string; deviceEnrollRequired: boolean }>;
  };
  onboard(token: string): Promise<string>;   // registers this device, returns its id
  connect(token: string): void;              // dials the DS WebSocket with the device-bound token
  session: SessionStore;
}

// Orchestrates sign-in: authenticate, onboard a device if required, then connect.
// Business logic in the app layer; UI invokes it via callbacks.
export function createAuthFlow(deps: AuthFlowDeps) {
  return {
    register: (email: string, password: string) => deps.authenticator.register(email, password),
    async login(email: string, password: string): Promise<void> {
      const { sessionToken, deviceEnrollRequired } = await deps.authenticator.login(email, password);
      deps.session.authenticated(sessionToken);
      if (deviceEnrollRequired) {
        deps.session.onboarded(await deps.onboard(sessionToken));
      }
      deps.connect(sessionToken);
    },
  };
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd client && npx vitest run src/app/authFlow.test.ts`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add client/src/app/authFlow.ts client/src/app/authFlow.test.ts
git -C /Users/denisurevic/Documents/slack commit -m "feat(client): auth flow — authenticate → onboard → connect"
```

---

### Task 10: Auth gate in App + bootstrap wiring + full verification

**Files:**
- Modify: `client/src/app/App.tsx`, `client/src/app/App.test.tsx`, `client/src/app/bootstrap.ts`, `client/src/main.tsx`, `client/src/shared/config/env.ts`

- [ ] **Step 1: Write the failing test (App gates on session status)**

ADD to `client/src/app/App.test.tsx` (keep existing tests):
```tsx
// @vitest-environment jsdom
import { render } from "@solidjs/testing-library";
import { describe, it, expect, vi } from "vitest";
import { App } from "./App";
import { createConversationStore } from "../entities/conversation/store";
import { createConnectionStore } from "../entities/connection/store";
import { createSessionStore } from "../entities/session/store";

describe("App auth gate", () => {
  const base = (session: ReturnType<typeof createSessionStore>) => ({
    groupId: "g1", conversation: createConversationStore(), connection: createConnectionStore(),
    session, onLogin: vi.fn(), onRegister: vi.fn(), onSend: vi.fn(), authError: "", busy: false,
  });

  it("shows the auth page when anonymous", () => {
    const { getByText } = render(() => <App {...base(createSessionStore())} />);
    expect(getByText("Sign in")).toBeTruthy();
  });

  it("shows the chat when onboarded", () => {
    const session = createSessionStore();
    session.authenticated("TOK"); session.onboarded("DEV1");
    const props = base(session);
    props.conversation.addMessage("g1", { seq: 1, sender: "alice", text: "hello-chat" });
    props.connection.setStatus("online");
    const { getByText } = render(() => <App {...props} />);
    expect(getByText("hello-chat")).toBeTruthy();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd client && npx vitest run src/app/App.test.tsx`
Expected: FAIL — `App` doesn't accept `session`/`onLogin` or gate on status.

- [ ] **Step 3: Implement App gate + bootstrap + main + env**

`client/src/app/App.tsx`:
```tsx
import { Show, type Component } from "solid-js";
import { ChatPage } from "../pages/chat/ChatPage";
import { AuthPage } from "../pages/auth/AuthPage";
import type { ConversationStore } from "../entities/conversation/store";
import type { ConnectionStore } from "../entities/connection/store";
import type { SessionStore } from "../entities/session/store";

export const App: Component<{
  groupId: string;
  conversation: ConversationStore;
  connection: ConnectionStore;
  session: SessionStore;
  onLogin: (email: string, password: string) => void;
  onRegister: (email: string, password: string) => void;
  onSend: (text: string) => void;
  authError: string;
  busy: boolean;
}> = (props) => (
  <Show
    when={props.session.status() !== "anonymous"}
    fallback={<AuthPage onLogin={props.onLogin} onRegister={props.onRegister} error={props.authError} busy={props.busy} />}
  >
    <ChatPage
      messages={props.conversation.messages(props.groupId)}
      status={props.connection.status()}
      onSend={props.onSend}
    />
  </Show>
);
```
Add to `client/src/shared/config/env.ts`:
```ts
export const DS_HTTP_URL = (import.meta as any).env?.VITE_DS_HTTP_URL ?? "http://localhost:8080";
```
Update `client/src/app/bootstrap.ts` — add auth machinery to the existing `bootstrap` (which already builds cryptoClient, protocol, conversation, connection, orchestrator). Append:
```ts
import { AsClient } from "../shared/api/as";
import { initOpaque, OpaqueOps } from "../shared/lib/auth/opaqueClient";
import { createAuthenticator } from "../features/authenticate/authenticate";
import { onboardDevice } from "../features/onboard-device/onboardDevice";
import { createAuthFlow } from "./authFlow";
import { createSessionStore } from "../entities/session/store";
import { DS_HTTP_URL } from "../shared/config/env";
import wasmUrl from "../shared/lib/auth/opaque-wasm/opaque.wasm?url";
import wasmExecUrl from "../shared/lib/auth/opaque-wasm/wasm_exec.js?url";

async function loadOpaqueInBrowser() {
  // Load wasm_exec.js (classic script) to define globalThis.Go.
  await import(/* @vite-ignore */ wasmExecUrl);
  const newGo = () => new (globalThis as any).Go();
  const loadWasm = async (importObject: WebAssembly.Imports) => {
    const res = await fetch(wasmUrl);
    const { instance } = await WebAssembly.instantiate(await res.arrayBuffer(), importObject);
    return instance;
  };
  return initOpaque(newGo, loadWasm);
}

// ...inside bootstrap(), after orchestrator/stores are built:
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
```
(`cryptoClient`, `protocol`, `orchestrator`, `conversation`, `connection` are the existing locals — keep them. Update the return type accordingly.)
NOTE (verify-spot): `import(wasmExecUrl)` for a classic (non-module) script may not execute under Vite as a side-effect import; if `globalThis.Go` is undefined afterward, inject a `<script src={wasmExecUrl}>` and await its `onload`, or vendor a tiny ESM wrapper around `wasm_exec.js`. Browser-only; verified by `vite build` + reasoning. Report the approach that worked. Do NOT use `eval`/`new Function`.

Update `client/src/main.tsx`:
```tsx
import { render } from "solid-js/web";
import { createSignal } from "solid-js";
import { App } from "./app/App";
import { bootstrap } from "./app/bootstrap";

const root = document.getElementById("root");
if (root) {
  const { orchestrator, conversation, connection, session, authFlow } = bootstrap("device");
  const [authError, setAuthError] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const run = (fn: () => Promise<void>) => async () => {
    setBusy(true); setAuthError("");
    try { await fn(); } catch { setAuthError("invalid email or password"); }
    finally { setBusy(false); }
  };
  render(() => (
    <App
      groupId="g1"
      conversation={conversation}
      connection={connection}
      session={session}
      onLogin={(email, pw) => void run(() => authFlow.login(email, pw))()}
      onRegister={(email, pw) => void run(() => authFlow.register(email, pw))()}
      onSend={(text) => orchestrator.sendText("g1", text)}
      authError={authError()}
      busy={busy()}
    />
  ), root);
}
```

- [ ] **Step 4: Full verification**

Run:
```bash
cd client && npx vitest run && npx tsc --noEmit && npx vite build
cd client/src/shared/lib/auth/opaque-wasm && go test ./...
```
Expected: all client tests green (App gate, authenticate, onboard, session, as, opaque smoke, + existing); tsc clean; `vite build` bundles the OPAQUE wasm; Go interop test passes. Report the per-area summary.

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add client/src/app client/src/main.tsx client/src/shared/config/env.ts
git -C /Users/denisurevic/Documents/slack commit -m "feat(client): auth gate (AuthPage↔ChatPage) + lazy OPAQUE bootstrap"
```

---

## Self-Review

**1. Spec coverage:**
- Go bytemare/opaque client → WASM (guaranteed interop) → Tasks 2, 3 ✓
- TS auth client (stepwise OPAQUE ⇄ HTTP) → Tasks 3, 4, 5 ✓
- Typed AS HTTP client (register/login/devices) → Task 4 ✓
- Device onboarding (signing key + keypackages → POST /devices → device-bound) → Tasks 1, 7, 9 ✓
- Session entity (token/deviceId/status) → Task 6 ✓
- Frontend calls connect(token) after onboarded → Tasks 9, 10 ✓
- Minimal functional login form → Task 8 ✓
- `signing_public_key()` in crypto-core → Task 1 ✓
- flowId state in WASM → Task 2 ✓; serverIdentity "messenger-as" → Task 5 ✓; in-memory token → Task 6 ✓
- Errors (wrong password unified, 409, wasm-load failure, onboard non-fatal) → Tasks 4, 5/9, 10 ✓
- Interop test (client session key == server) → Task 2 ✓; wasm smoke → Task 3; mocked orchestration → 5,7,9; live-AS e2e → noted follow-up ✓
- Out of scope (KT-client, MLS group flows, styling, persistence) → excluded.

**2. Placeholder scan:** No "TBD"/"handle errors" placeholders. Verify-spots (wasm_exec.js path; wasm-in-node via `createRequire`; browser `import(wasmExecUrl)`; bytemare js/wasm compile; Argon2 perf) are conscious callouts with concrete fallbacks. **No `eval`/`new Function`** anywhere (the node loader uses `createRequire`). All code steps contain complete code.

**3. Type consistency:** `RegInit/RegFinalize/LoginKE1/LoginKE3` (Go) ↔ `main.go` bindings ↔ `OpaqueGlobals`/`OpaqueOps` (opaqueClient) ↔ `OpaqueOpsLike` (authenticate). `AsClient` methods ↔ `AsLike` (authenticate) + `EnrollLike` (onboard). `CryptoClient.signingPublicKey()`/`keyPackage()` (Task 1) ↔ `CryptoLike` (onboard). `SessionStore` (authenticated/onboarded/clear; status/token/deviceId) consistent across 6, 9, 10. `createAuthFlow` deps ↔ Task 10 bootstrap wiring. `App` props consistent 8–10.

**4. Interop-risk callout (not placeholder):** the ONLY guaranteed-correct interop proof is Task 2's native Go round-trip vs a real `bytemare` Server with `DefaultConfiguration` — asserting matching session keys. Wasm packaging (Task 3) + browser loading (Task 10) are delivery, verified by smoke + `vite build`; if wasm-in-node/browser loading is intractable, crypto correctness still stands on Task 2 and only the loader needs adjustment. The bearer-token-over-WebSocket gap (frontend `?access_token=` vs DS bearer header) remains a documented backend follow-up — `connect()` now fires with a real token, making that reconciliation the next thing for a live socket.
