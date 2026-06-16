# MLS Crypto Engine Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the client-side MLS crypto engine (Rust→WASM core + TypeScript Web Worker wrapper) that encrypts/decrypts group messages end-to-end, with auto-injected compliance member, verified against RFC 9420 test vectors.

**Architecture:** A Rust crate (`crypto-core`) wraps OpenMLS and exposes a small, flat API (identity, group, encrypt, decrypt, membership). It compiles to WebAssembly via `wasm-pack`. A TypeScript layer runs that WASM inside a dedicated Web Worker (the client's "3rd thread") and speaks a typed `postMessage` protocol to the UI thread. Raw private keys never leave the worker — only ciphertext or decrypted plaintext crosses the boundary.

**Tech Stack:** Rust, OpenMLS (`openmls`, `openmls_rust_crypto`, `openmls_basic_credential`), `tls_codec`, `wasm-bindgen` + `wasm-pack`, TypeScript, Vitest.

**Scope note:** This plan covers ONLY the MLS crypto engine from the subproject-1 spec (`docs/superpowers/specs/2026-06-16-crypto-protocol-design.md`). OPAQUE authentication (client) and Key Transparency (client verification) are independently testable units and get their own plans. The Go services (AS/DS/KT) are subproject 2.

---

## File Structure

**Rust crate — `crypto-core/`** (compiles to WASM):
- `Cargo.toml` — deps and `cdylib` crate type
- `src/lib.rs` — `wasm-bindgen` exports (thin layer over `engine`)
- `src/engine.rs` — `Engine` struct: holds provider, signer, owns `MlsGroup`s by id
- `src/identity.rs` — credential + signature keypair + KeyPackage generation/serialization
- `src/serde_mls.rs` — TLS (de)serialization helpers for `MlsMessageOut`/`KeyPackage`
- `src/errors.rs` — `EngineError` enum
- `tests/rfc_vectors.rs` — RFC 9420 message/welcome test-vector harness
- `tests/vectors/` — vendored RFC 9420 JSON vectors

**TypeScript worker — `client/src/crypto/`**:
- `protocol.ts` — request/response message types (UI ↔ worker contract)
- `worker.ts` — Web Worker entry: loads WASM, dispatches requests
- `client.ts` — main-thread `CryptoClient` proxy (promise-per-request)
- `protocol.test.ts`, `client.test.ts` — Vitest unit/integration tests

Each file has one responsibility. The Rust `engine.rs` is the only place that touches OpenMLS group state; `lib.rs` is a pure binding shim; the TS `client.ts` never imports WASM directly (only `worker.ts` does).

---

## Milestone 1 — Rust crypto-core

### Task 1: Scaffold the Rust crate and verify the WASM toolchain

**Files:**
- Create: `crypto-core/Cargo.toml`
- Create: `crypto-core/src/lib.rs`

- [ ] **Step 1: Write the failing test**

In `crypto-core/src/lib.rs`:

```rust
#[cfg(test)]
mod tests {
    #[test]
    fn ciphersuite_is_mti() {
        use openmls::prelude::Ciphersuite;
        let cs = crate::DEFAULT_CIPHERSUITE;
        assert_eq!(cs, Ciphersuite::MLS_128_DHKEMX25519_AES128GCM_SHA256_Ed25519);
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd crypto-core && cargo test ciphersuite_is_mti`
Expected: FAIL — `DEFAULT_CIPHERSUITE` not found (compile error).

- [ ] **Step 3: Write minimal implementation**

`crypto-core/Cargo.toml`:

```toml
[package]
name = "crypto-core"
version = "0.1.0"
edition = "2021"

[lib]
crate-type = ["cdylib", "rlib"]

[dependencies]
openmls = "0.6"
openmls_rust_crypto = "0.3"
openmls_basic_credential = "0.3"
openmls_traits = "0.3"
tls_codec = "0.4"
wasm-bindgen = "0.2"
serde = { version = "1", features = ["derive"] }
serde-wasm-bindgen = "0.6"
getrandom = { version = "0.2", features = ["js"] }

[dev-dependencies]
serde_json = "1"
```

Top of `crypto-core/src/lib.rs` (above the test module):

```rust
use openmls::prelude::Ciphersuite;

pub const DEFAULT_CIPHERSUITE: Ciphersuite =
    Ciphersuite::MLS_128_DHKEMX25519_AES128GCM_SHA256_Ed25519;
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd crypto-core && cargo test ciphersuite_is_mti`
Expected: PASS (1 passed).

- [ ] **Step 5: Verify the WASM target builds**

Run: `cd crypto-core && rustup target add wasm32-unknown-unknown && cargo build --target wasm32-unknown-unknown`
Expected: build succeeds (warnings about unused code are fine).

- [ ] **Step 6: Commit**

```bash
git add crypto-core/Cargo.toml crypto-core/src/lib.rs
git commit -m "feat(crypto-core): scaffold crate, pin MTI ciphersuite, verify wasm target"
```

---

### Task 2: Identity — credential, signer, and KeyPackage

**Files:**
- Create: `crypto-core/src/errors.rs`
- Create: `crypto-core/src/identity.rs`
- Modify: `crypto-core/src/lib.rs` (add `mod` declarations)

- [ ] **Step 1: Write the failing test**

In `crypto-core/src/identity.rs`:

```rust
#[cfg(test)]
mod tests {
    use super::*;
    use openmls_rust_crypto::OpenMlsRustCrypto;

    #[test]
    fn generates_identity_and_key_package() {
        let provider = OpenMlsRustCrypto::default();
        let id = Identity::generate(&provider, b"alice@corp").unwrap();

        // KeyPackage serializes to non-empty bytes and round-trips back.
        let bytes = id.key_package_bytes(&provider).unwrap();
        assert!(!bytes.is_empty());

        let parsed = deserialize_key_package(&bytes).unwrap();
        // Same HPKE init key after a round-trip.
        assert_eq!(
            parsed.hpke_init_key().as_slice(),
            id.key_package(&provider).unwrap().hpke_init_key().as_slice()
        );
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd crypto-core && cargo test generates_identity_and_key_package`
Expected: FAIL — `Identity` not found.

- [ ] **Step 3: Write minimal implementation**

`crypto-core/src/errors.rs`:

```rust
#[derive(Debug, thiserror::Error)]
pub enum EngineError {
    #[error("mls error: {0}")]
    Mls(String),
    #[error("serialization error: {0}")]
    Serde(String),
    #[error("unknown group: {0}")]
    UnknownGroup(String),
}

impl EngineError {
    pub fn mls(e: impl std::fmt::Display) -> Self {
        EngineError::Mls(e.to_string())
    }
    pub fn serde(e: impl std::fmt::Display) -> Self {
        EngineError::Serde(e.to_string())
    }
}
```

Add `thiserror = "1"` to `[dependencies]` in `Cargo.toml`.

`crypto-core/src/identity.rs`:

```rust
use crate::errors::EngineError;
use crate::DEFAULT_CIPHERSUITE;
use openmls::prelude::*;
use openmls_basic_credential::SignatureKeyPair;
use openmls_rust_crypto::OpenMlsRustCrypto;
use openmls_traits::types::SignatureScheme;
use tls_codec::{Deserialize as _, Serialize as _};

/// A device identity: its signature keypair and credential.
pub struct Identity {
    pub signer: SignatureKeyPair,
    pub credential_with_key: CredentialWithKey,
}

impl Identity {
    pub fn generate(
        provider: &OpenMlsRustCrypto,
        name: &[u8],
    ) -> Result<Self, EngineError> {
        let credential = BasicCredential::new(name.to_vec());
        let signer = SignatureKeyPair::new(SignatureScheme::ED25519)
            .map_err(EngineError::mls)?;
        signer.store(provider.storage()).map_err(EngineError::mls)?;
        let credential_with_key = CredentialWithKey {
            credential: credential.into(),
            signature_key: signer.to_public_vec().into(),
        };
        Ok(Self { signer, credential_with_key })
    }

    pub fn key_package(
        &self,
        provider: &OpenMlsRustCrypto,
    ) -> Result<KeyPackage, EngineError> {
        let bundle = KeyPackage::builder()
            .build(
                DEFAULT_CIPHERSUITE,
                provider,
                &self.signer,
                self.credential_with_key.clone(),
            )
            .map_err(EngineError::mls)?;
        Ok(bundle.key_package().clone())
    }

    pub fn key_package_bytes(
        &self,
        provider: &OpenMlsRustCrypto,
    ) -> Result<Vec<u8>, EngineError> {
        self.key_package(provider)?
            .tls_serialize_detached()
            .map_err(EngineError::serde)
    }
}

pub fn deserialize_key_package(bytes: &[u8]) -> Result<KeyPackage, EngineError> {
    let kp_in = KeyPackageIn::tls_deserialize_exact(bytes)
        .map_err(EngineError::serde)?;
    // Validate against the protocol version + ciphersuite before use.
    kp_in
        .validate(OpenMlsRustCrypto::default().crypto(), ProtocolVersion::Mls10)
        .map_err(EngineError::mls)
}
```

In `crypto-core/src/lib.rs`, add near the top:

```rust
pub mod errors;
pub mod identity;
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd crypto-core && cargo test generates_identity_and_key_package`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add crypto-core/src/errors.rs crypto-core/src/identity.rs crypto-core/src/lib.rs crypto-core/Cargo.toml
git commit -m "feat(crypto-core): identity + KeyPackage generation and round-trip"
```

---

### Task 3: Engine — create group and add a member

**Files:**
- Create: `crypto-core/src/engine.rs`
- Modify: `crypto-core/src/lib.rs` (add `pub mod engine;`)

- [ ] **Step 1: Write the failing test**

In `crypto-core/src/engine.rs`:

```rust
#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn alice_creates_group_and_adds_bob() {
        let mut alice = Engine::new(b"alice@corp");
        let mut bob = Engine::new(b"bob@corp");

        // Bob publishes a KeyPackage; Alice fetches its bytes.
        let bob_kp = bob.key_package_bytes().unwrap();

        let group_id = b"team-1".to_vec();
        alice.create_group(&group_id).unwrap();

        let add = alice.add_member(&group_id, &bob_kp).unwrap();
        // Commit and Welcome are produced.
        assert!(!add.commit.is_empty());
        assert!(!add.welcome.is_empty());

        // Bob joins from the Welcome and now tracks the same group.
        bob.join_from_welcome(&add.welcome).unwrap();
        assert!(bob.has_group(&group_id));
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd crypto-core && cargo test alice_creates_group_and_adds_bob`
Expected: FAIL — `Engine` not found.

- [ ] **Step 3: Write minimal implementation**

`crypto-core/src/engine.rs`:

```rust
use crate::errors::EngineError;
use crate::identity::{deserialize_key_package, Identity};
use crate::DEFAULT_CIPHERSUITE;
use openmls::prelude::*;
use openmls_rust_crypto::OpenMlsRustCrypto;
use std::collections::HashMap;
use tls_codec::Serialize as _;

/// Result of a membership change that must be delivered.
pub struct AddResult {
    /// Commit fanned out to existing members (serialized MlsMessageOut).
    pub commit: Vec<u8>,
    /// Welcome sent to the new member (serialized MlsMessageOut).
    pub welcome: Vec<u8>,
}

/// One device's crypto engine. Owns this device's identity and group states.
pub struct Engine {
    provider: OpenMlsRustCrypto,
    identity: Identity,
    groups: HashMap<Vec<u8>, MlsGroup>,
}

impl Engine {
    pub fn new(name: &[u8]) -> Self {
        let provider = OpenMlsRustCrypto::default();
        let identity = Identity::generate(&provider, name)
            .expect("identity generation must not fail with default provider");
        Self { provider, identity, groups: HashMap::new() }
    }

    pub fn key_package_bytes(&self) -> Result<Vec<u8>, EngineError> {
        self.identity.key_package_bytes(&self.provider)
    }

    pub fn has_group(&self, group_id: &[u8]) -> bool {
        self.groups.contains_key(group_id)
    }

    pub fn create_group(&mut self, group_id: &[u8]) -> Result<(), EngineError> {
        let config = MlsGroupCreateConfig::builder()
            .ciphersuite(DEFAULT_CIPHERSUITE)
            .use_ratchet_tree_extension(true)
            .build();
        let group = MlsGroup::new_with_group_id(
            &self.provider,
            &self.identity.signer,
            &config,
            GroupId::from_slice(group_id),
            self.identity.credential_with_key.clone(),
        )
        .map_err(EngineError::mls)?;
        self.groups.insert(group_id.to_vec(), group);
        Ok(())
    }

    pub fn add_member(
        &mut self,
        group_id: &[u8],
        key_package_bytes: &[u8],
    ) -> Result<AddResult, EngineError> {
        let kp = deserialize_key_package(key_package_bytes)?;
        let group = self
            .groups
            .get_mut(group_id)
            .ok_or_else(|| EngineError::UnknownGroup(hex(group_id)))?;

        let (commit, welcome, _group_info) = group
            .add_members(
                &self.provider,
                &self.identity.signer,
                std::slice::from_ref(&kp),
            )
            .map_err(EngineError::mls)?;

        group.merge_pending_commit(&self.provider).map_err(EngineError::mls)?;

        Ok(AddResult {
            commit: commit.tls_serialize_detached().map_err(EngineError::serde)?,
            welcome: welcome.tls_serialize_detached().map_err(EngineError::serde)?,
        })
    }

    pub fn join_from_welcome(&mut self, welcome_bytes: &[u8]) -> Result<(), EngineError> {
        let msg_in = MlsMessageIn::tls_deserialize_exact(welcome_bytes)
            .map_err(EngineError::serde)?;
        let welcome = msg_in.into_welcome().ok_or_else(|| {
            EngineError::Mls("expected a Welcome message".to_string())
        })?;
        let config = MlsGroupJoinConfig::builder()
            .use_ratchet_tree_extension(true)
            .build();
        let staged = StagedWelcome::new_from_welcome(&self.provider, &config, welcome, None)
            .map_err(EngineError::mls)?;
        let group = staged.into_group(&self.provider).map_err(EngineError::mls)?;
        let gid = group.group_id().as_slice().to_vec();
        self.groups.insert(gid, group);
        Ok(())
    }
}

fn hex(bytes: &[u8]) -> String {
    bytes.iter().map(|b| format!("{:02x}", b)).collect()
}
```

Add `use tls_codec::Deserialize as _;` to the imports if `tls_deserialize_exact` is unresolved.

In `crypto-core/src/lib.rs` add:

```rust
pub mod engine;
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd crypto-core && cargo test alice_creates_group_and_adds_bob`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add crypto-core/src/engine.rs crypto-core/src/lib.rs
git commit -m "feat(crypto-core): create group, add member, join from welcome"
```

---

### Task 4: Engine — encrypt an application message

**Files:**
- Modify: `crypto-core/src/engine.rs`

- [ ] **Step 1: Write the failing test**

Add to the `tests` module in `crypto-core/src/engine.rs`:

```rust
#[test]
fn alice_encrypts_application_message() {
    let mut alice = Engine::new(b"alice@corp");
    let group_id = b"team-1".to_vec();
    alice.create_group(&group_id).unwrap();

    let ct = alice.encrypt(&group_id, b"hello group").unwrap();
    assert!(!ct.is_empty());
    // Ciphertext must not contain the plaintext.
    assert!(ct.windows(11).all(|w| w != b"hello group"));
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd crypto-core && cargo test alice_encrypts_application_message`
Expected: FAIL — no method `encrypt`.

- [ ] **Step 3: Write minimal implementation**

Add to `impl Engine` in `crypto-core/src/engine.rs`:

```rust
pub fn encrypt(
    &mut self,
    group_id: &[u8],
    plaintext: &[u8],
) -> Result<Vec<u8>, EngineError> {
    let group = self
        .groups
        .get_mut(group_id)
        .ok_or_else(|| EngineError::UnknownGroup(hex(group_id)))?;
    let out = group
        .create_message(&self.provider, &self.identity.signer, plaintext)
        .map_err(EngineError::mls)?;
    out.tls_serialize_detached().map_err(EngineError::serde)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd crypto-core && cargo test alice_encrypts_application_message`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add crypto-core/src/engine.rs
git commit -m "feat(crypto-core): encrypt application messages"
```

---

### Task 5: Engine — process incoming messages (decrypt + merge commits)

**Files:**
- Modify: `crypto-core/src/engine.rs`

- [ ] **Step 1: Write the failing test**

Add to the `tests` module:

```rust
#[test]
fn bob_decrypts_message_from_alice() {
    let mut alice = Engine::new(b"alice@corp");
    let mut bob = Engine::new(b"bob@corp");
    let bob_kp = bob.key_package_bytes().unwrap();

    let group_id = b"team-1".to_vec();
    alice.create_group(&group_id).unwrap();
    let add = alice.add_member(&group_id, &bob_kp).unwrap();
    bob.join_from_welcome(&add.welcome).unwrap();

    let ct = alice.encrypt(&group_id, b"hello bob").unwrap();
    let decrypted = bob.process(&group_id, &ct).unwrap();

    match decrypted {
        Incoming::Application(pt) => assert_eq!(pt, b"hello bob"),
        other => panic!("expected application message, got {:?}", other),
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd crypto-core && cargo test bob_decrypts_message_from_alice`
Expected: FAIL — `Incoming` and `process` not found.

- [ ] **Step 3: Write minimal implementation**

Add to `crypto-core/src/engine.rs` (above `impl Engine` or near `AddResult`):

```rust
/// The result of processing an inbound message.
#[derive(Debug)]
pub enum Incoming {
    /// Decrypted application plaintext.
    Application(Vec<u8>),
    /// A membership/commit message was applied; group advanced one epoch.
    CommitApplied,
    /// A proposal was stored, pending a future commit.
    ProposalStored,
}
```

Add to `impl Engine`:

```rust
pub fn process(
    &mut self,
    group_id: &[u8],
    message_bytes: &[u8],
) -> Result<Incoming, EngineError> {
    let group = self
        .groups
        .get_mut(group_id)
        .ok_or_else(|| EngineError::UnknownGroup(hex(group_id)))?;

    let msg_in = MlsMessageIn::tls_deserialize_exact(message_bytes)
        .map_err(EngineError::serde)?;
    let protocol_message = msg_in
        .try_into_protocol_message()
        .map_err(EngineError::mls)?;
    let processed = group
        .process_message(&self.provider, protocol_message)
        .map_err(EngineError::mls)?;

    match processed.into_content() {
        ProcessedMessageContent::ApplicationMessage(app) => {
            Ok(Incoming::Application(app.into_bytes()))
        }
        ProcessedMessageContent::StagedCommitMessage(staged) => {
            group
                .merge_staged_commit(&self.provider, *staged)
                .map_err(EngineError::mls)?;
            Ok(Incoming::CommitApplied)
        }
        ProcessedMessageContent::ProposalMessage(proposal) => {
            group
                .store_pending_proposal(self.provider.storage(), *proposal)
                .map_err(EngineError::mls)?;
            Ok(Incoming::ProposalStored)
        }
        ProcessedMessageContent::ExternalJoinProposalMessage(proposal) => {
            group
                .store_pending_proposal(self.provider.storage(), *proposal)
                .map_err(EngineError::mls)?;
            Ok(Incoming::ProposalStored)
        }
    }
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd crypto-core && cargo test bob_decrypts_message_from_alice`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add crypto-core/src/engine.rs
git commit -m "feat(crypto-core): process inbound messages — decrypt and merge commits"
```

---

### Task 6: Engine — remove member and verify post-compromise security

**Files:**
- Modify: `crypto-core/src/engine.rs`

- [ ] **Step 1: Write the failing test**

This proves a removed member cannot read messages from the new epoch (PCS / forward secrecy invariant from the spec).

```rust
#[test]
fn removed_member_cannot_read_new_epoch() {
    let mut alice = Engine::new(b"alice@corp");
    let mut bob = Engine::new(b"bob@corp");
    let bob_kp = bob.key_package_bytes().unwrap();

    let group_id = b"team-1".to_vec();
    alice.create_group(&group_id).unwrap();
    let add = alice.add_member(&group_id, &bob_kp).unwrap();
    bob.join_from_welcome(&add.welcome).unwrap();

    // Alice removes Bob (Bob is leaf index 1; Alice is 0).
    let remove = alice.remove_member(&group_id, 1).unwrap();
    // Bob processes the removal commit and learns he is out.
    let _ = bob.process(&group_id, &remove);

    // Alice sends a new-epoch message.
    let ct = alice.encrypt(&group_id, b"secret after removal").unwrap();

    // Bob must NOT be able to decrypt it.
    let result = bob.process(&group_id, &ct);
    assert!(result.is_err(), "removed member must not decrypt new-epoch messages");
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd crypto-core && cargo test removed_member_cannot_read_new_epoch`
Expected: FAIL — no method `remove_member`.

- [ ] **Step 3: Write minimal implementation**

Add to `impl Engine`:

```rust
pub fn remove_member(
    &mut self,
    group_id: &[u8],
    leaf_index: u32,
) -> Result<Vec<u8>, EngineError> {
    let group = self
        .groups
        .get_mut(group_id)
        .ok_or_else(|| EngineError::UnknownGroup(hex(group_id)))?;
    let (commit, _welcome, _group_info) = group
        .remove_members(
            &self.provider,
            &self.identity.signer,
            &[LeafNodeIndex::new(leaf_index)],
        )
        .map_err(EngineError::mls)?;
    group.merge_pending_commit(&self.provider).map_err(EngineError::mls)?;
    commit.tls_serialize_detached().map_err(EngineError::serde)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd crypto-core && cargo test removed_member_cannot_read_new_epoch`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add crypto-core/src/engine.rs
git commit -m "feat(crypto-core): remove member; PCS test for removed members"
```

---

### Task 7: Compliance member — auto-injected on group creation

**Files:**
- Modify: `crypto-core/src/engine.rs`

The spec requires every group to include a visible compliance leaf. We model the compliance member as an externally supplied KeyPackage that `create_group` adds immediately.

- [ ] **Step 1: Write the failing test**

```rust
#[test]
fn group_creation_injects_compliance_member() {
    // The org's compliance device has its own engine/identity.
    let mut compliance = Engine::new(b"compliance@corp");
    let compliance_kp = compliance.key_package_bytes().unwrap();

    let mut alice = Engine::new(b"alice@corp");
    let group_id = b"team-1".to_vec();

    // Creating with a compliance KeyPackage returns the Welcome for that member.
    let welcome = alice
        .create_group_with_compliance(&group_id, &compliance_kp)
        .unwrap();

    // The compliance device can join and decrypt subsequent traffic.
    compliance.join_from_welcome(&welcome).unwrap();
    let ct = alice.encrypt(&group_id, b"audited message").unwrap();
    match compliance.process(&group_id, &ct).unwrap() {
        Incoming::Application(pt) => assert_eq!(pt, b"audited message"),
        other => panic!("expected application message, got {:?}", other),
    }

    // Membership is 2 (creator + compliance) — compliance is visible.
    assert_eq!(alice.member_count(&group_id).unwrap(), 2);
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd crypto-core && cargo test group_creation_injects_compliance_member`
Expected: FAIL — methods `create_group_with_compliance` and `member_count` not found.

- [ ] **Step 3: Write minimal implementation**

Add to `impl Engine`:

```rust
/// Create a group and immediately add the org compliance member.
/// Returns the Welcome to deliver to the compliance device.
pub fn create_group_with_compliance(
    &mut self,
    group_id: &[u8],
    compliance_key_package: &[u8],
) -> Result<Vec<u8>, EngineError> {
    self.create_group(group_id)?;
    let add = self.add_member(group_id, compliance_key_package)?;
    Ok(add.welcome)
}

pub fn member_count(&self, group_id: &[u8]) -> Result<usize, EngineError> {
    let group = self
        .groups
        .get(group_id)
        .ok_or_else(|| EngineError::UnknownGroup(hex(group_id)))?;
    Ok(group.members().count())
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd crypto-core && cargo test group_creation_injects_compliance_member`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add crypto-core/src/engine.rs
git commit -m "feat(crypto-core): auto-inject visible compliance member on group creation"
```

---

### Task 8: RFC 9420 test-vector harness

**Files:**
- Create: `crypto-core/tests/rfc_vectors.rs`
- Create: `crypto-core/tests/vectors/README.md`

This is a correctness gate from the spec. OpenMLS ships interop test vectors; we pin a subset and assert our ciphersuite passes the `message-protection` vectors.

- [ ] **Step 1: Vendor the test vectors**

Run:

```bash
mkdir -p crypto-core/tests/vectors
curl -L -o crypto-core/tests/vectors/message-protection.json \
  https://raw.githubusercontent.com/openmls/openmls/main/openmls/test_vectors/message-protection.json
```

Write `crypto-core/tests/vectors/README.md`:

```markdown
# RFC 9420 interop test vectors

Source: openmls/openmls `openmls/test_vectors/`. Pinned for the engine correctness gate.
Re-fetch from the matching openmls tag when bumping the `openmls` dependency.
```

- [ ] **Step 2: Write the failing test**

`crypto-core/tests/rfc_vectors.rs`:

```rust
use serde_json::Value;

#[test]
fn message_protection_vectors_present_and_use_mti_suite() {
    let raw = std::fs::read_to_string("tests/vectors/message-protection.json")
        .expect("message-protection.json must be vendored (see tests/vectors/README.md)");
    let vectors: Value = serde_json::from_str(&raw).expect("valid JSON");
    let arr = vectors.as_array().expect("vectors are a JSON array");
    assert!(!arr.is_empty(), "expected at least one vector");

    // Ciphersuite 0x0001 == MLS_128_DHKEMX25519_AES128GCM_SHA256_Ed25519.
    let has_mti = arr
        .iter()
        .any(|v| v.get("cipher_suite").and_then(|c| c.as_u64()) == Some(1));
    assert!(has_mti, "vectors must cover our MTI ciphersuite (0x0001)");
}
```

- [ ] **Step 3: Run test to verify it fails (before vendoring) / passes (after)**

Run: `cd crypto-core && cargo test --test rfc_vectors`
Expected: PASS once the vector file is vendored. (If Step 1 was skipped, FAIL with the "must be vendored" message — confirming the gate works.)

- [ ] **Step 4: Wire the OpenMLS vector runner**

OpenMLS exposes a vector deserializer/runner behind its `test-utils` feature. Add to `crypto-core/Cargo.toml`:

```toml
[dev-dependencies]
serde_json = "1"
openmls = { version = "0.6", features = ["test-utils"] }
```

Extend `crypto-core/tests/rfc_vectors.rs`:

```rust
use openmls::prelude::test_utils::read_test_vectors_mp; // message-protection runner
// If the exact path differs in the pinned version, locate it with:
//   cargo doc -p openmls --features test-utils --open
// and search for the message-protection vector struct + run function.

#[test]
fn message_protection_vectors_pass() {
    // Runs every vector for our ciphersuite through encrypt/decrypt round-trips.
    read_test_vectors_mp("tests/vectors/message-protection.json");
}
```

- [ ] **Step 5: Run the full vector test**

Run: `cd crypto-core && cargo test --test rfc_vectors --features test-utils`
Expected: PASS (all message-protection vectors round-trip).

- [ ] **Step 6: Commit**

```bash
git add crypto-core/tests/rfc_vectors.rs crypto-core/tests/vectors/ crypto-core/Cargo.toml
git commit -m "test(crypto-core): RFC 9420 message-protection vector gate"
```

---

## Milestone 2 — WASM bindings + TypeScript Worker

### Task 9: `wasm-bindgen` exports for the engine

**Files:**
- Modify: `crypto-core/src/lib.rs`
- Modify: `crypto-core/Cargo.toml`

- [ ] **Step 1: Write the failing test**

In `crypto-core/src/lib.rs`:

```rust
#[cfg(test)]
mod wasm_api_tests {
    use super::*;

    #[test]
    fn wasm_engine_round_trips_a_message() {
        let mut alice = WasmEngine::new("alice@corp");
        let mut bob = WasmEngine::new("bob@corp");

        let bob_kp = bob.key_package_bytes().unwrap();
        alice.create_group("team-1").unwrap();
        let welcome = alice.add_member("team-1", bob_kp).unwrap_welcome();
        bob.join_from_welcome(welcome).unwrap();

        let ct = alice.encrypt("team-1", b"hi".to_vec()).unwrap();
        let pt = bob.decrypt("team-1", ct).unwrap();
        assert_eq!(pt, b"hi");
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd crypto-core && cargo test wasm_engine_round_trips_a_message`
Expected: FAIL — `WasmEngine` not found.

- [ ] **Step 3: Write minimal implementation**

In `crypto-core/src/lib.rs`:

```rust
use wasm_bindgen::prelude::*;
use crate::engine::{Engine, Incoming};

/// Returned by add_member: serialized commit + welcome.
#[wasm_bindgen]
pub struct WasmAddResult {
    commit: Vec<u8>,
    welcome: Vec<u8>,
}

#[wasm_bindgen]
impl WasmAddResult {
    #[wasm_bindgen(getter)]
    pub fn commit(&self) -> Vec<u8> { self.commit.clone() }
    #[wasm_bindgen(getter)]
    pub fn welcome(&self) -> Vec<u8> { self.welcome.clone() }
}

// Native-test convenience (not compiled to wasm consumers).
#[cfg(test)]
impl WasmAddResult {
    pub fn unwrap_welcome(self) -> Vec<u8> { self.welcome }
}

#[wasm_bindgen]
pub struct WasmEngine {
    inner: Engine,
}

#[wasm_bindgen]
impl WasmEngine {
    #[wasm_bindgen(constructor)]
    pub fn new(name: &str) -> WasmEngine {
        WasmEngine { inner: Engine::new(name.as_bytes()) }
    }

    pub fn key_package_bytes(&self) -> Result<Vec<u8>, JsError> {
        self.inner.key_package_bytes().map_err(to_js)
    }

    pub fn create_group(&mut self, group_id: &str) -> Result<(), JsError> {
        self.inner.create_group(group_id.as_bytes()).map_err(to_js)
    }

    pub fn create_group_with_compliance(
        &mut self,
        group_id: &str,
        compliance_kp: Vec<u8>,
    ) -> Result<Vec<u8>, JsError> {
        self.inner
            .create_group_with_compliance(group_id.as_bytes(), &compliance_kp)
            .map_err(to_js)
    }

    pub fn add_member(
        &mut self,
        group_id: &str,
        key_package: Vec<u8>,
    ) -> Result<WasmAddResult, JsError> {
        let r = self
            .inner
            .add_member(group_id.as_bytes(), &key_package)
            .map_err(to_js)?;
        Ok(WasmAddResult { commit: r.commit, welcome: r.welcome })
    }

    pub fn join_from_welcome(&mut self, welcome: Vec<u8>) -> Result<(), JsError> {
        self.inner.join_from_welcome(&welcome).map_err(to_js)
    }

    pub fn encrypt(&mut self, group_id: &str, plaintext: Vec<u8>) -> Result<Vec<u8>, JsError> {
        self.inner.encrypt(group_id.as_bytes(), &plaintext).map_err(to_js)
    }

    /// Returns plaintext for application messages; empty Vec for commits/proposals.
    pub fn decrypt(&mut self, group_id: &str, message: Vec<u8>) -> Result<Vec<u8>, JsError> {
        match self.inner.process(group_id.as_bytes(), &message).map_err(to_js)? {
            Incoming::Application(pt) => Ok(pt),
            Incoming::CommitApplied | Incoming::ProposalStored => Ok(Vec::new()),
        }
    }
}

fn to_js(e: crate::errors::EngineError) -> JsError {
    JsError::new(&e.to_string())
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd crypto-core && cargo test wasm_engine_round_trips_a_message`
Expected: PASS.

- [ ] **Step 5: Build the WASM package**

Run:

```bash
cd crypto-core && cargo install wasm-pack --locked 2>/dev/null; wasm-pack build --target web --out-dir ../client/src/crypto/wasm-pkg
```

Expected: produces `client/src/crypto/wasm-pkg/crypto_core.js` + `crypto_core_bg.wasm`.

- [ ] **Step 6: Commit**

```bash
git add crypto-core/src/lib.rs crypto-core/Cargo.toml client/src/crypto/wasm-pkg
git commit -m "feat(crypto-core): wasm-bindgen WasmEngine + build wasm package"
```

---

### Task 10: TypeScript protocol types (UI ↔ worker contract)

**Files:**
- Create: `client/src/crypto/protocol.ts`
- Create: `client/src/crypto/protocol.test.ts`

- [ ] **Step 1: Write the failing test**

`client/src/crypto/protocol.test.ts`:

```ts
import { describe, it, expect } from "vitest";
import { isCryptoResponse, type CryptoRequest } from "./protocol";

describe("crypto protocol", () => {
  it("builds a typed encrypt request", () => {
    const req: CryptoRequest = {
      id: "r1",
      kind: "encrypt",
      groupId: "team-1",
      plaintext: new Uint8Array([1, 2, 3]),
    };
    expect(req.kind).toBe("encrypt");
  });

  it("recognizes a valid response envelope", () => {
    expect(isCryptoResponse({ id: "r1", ok: true, result: null })).toBe(true);
    expect(isCryptoResponse({ nope: 1 })).toBe(false);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd client && npx vitest run src/crypto/protocol.test.ts`
Expected: FAIL — cannot find `./protocol`.

- [ ] **Step 3: Write minimal implementation**

`client/src/crypto/protocol.ts`:

```ts
export type CryptoRequest =
  | { id: string; kind: "keyPackage" }
  | { id: string; kind: "createGroup"; groupId: string }
  | { id: string; kind: "createGroupWithCompliance"; groupId: string; complianceKeyPackage: Uint8Array }
  | { id: string; kind: "addMember"; groupId: string; keyPackage: Uint8Array }
  | { id: string; kind: "joinFromWelcome"; welcome: Uint8Array }
  | { id: string; kind: "encrypt"; groupId: string; plaintext: Uint8Array }
  | { id: string; kind: "decrypt"; groupId: string; message: Uint8Array };

export type CryptoResponse =
  | { id: string; ok: true; result: unknown }
  | { id: string; ok: false; error: string };

export function isCryptoResponse(v: unknown): v is CryptoResponse {
  if (typeof v !== "object" || v === null) return false;
  const o = v as Record<string, unknown>;
  return typeof o.id === "string" && typeof o.ok === "boolean";
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd client && npx vitest run src/crypto/protocol.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add client/src/crypto/protocol.ts client/src/crypto/protocol.test.ts
git commit -m "feat(client): crypto worker protocol types + guard"
```

---

### Task 11: Web Worker host (loads WASM, dispatches requests)

**Files:**
- Create: `client/src/crypto/worker.ts`

- [ ] **Step 1: Write the failing test (integration, drives the real worker logic)**

Add to `client/src/crypto/worker.test.ts`:

```ts
import { describe, it, expect } from "vitest";
import { handleRequest } from "./worker";
import type { CryptoRequest } from "./protocol";

// handleRequest is the pure dispatch fn the worker's onmessage delegates to.
describe("worker dispatch", () => {
  it("returns a key package for a fresh engine", async () => {
    const req: CryptoRequest = { id: "r1", kind: "keyPackage" };
    const res = await handleRequest("alice@corp", req);
    expect(res.ok).toBe(true);
    if (res.ok) expect((res.result as Uint8Array).length).toBeGreaterThan(0);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd client && npx vitest run src/crypto/worker.test.ts`
Expected: FAIL — cannot find `./worker`.

- [ ] **Step 3: Write minimal implementation**

`client/src/crypto/worker.ts`:

```ts
import init, { WasmEngine } from "./wasm-pkg/crypto_core.js";
import type { CryptoRequest, CryptoResponse } from "./protocol";

let engine: WasmEngine | null = null;
let ready: Promise<void> | null = null;

async function ensureEngine(name: string): Promise<WasmEngine> {
  if (!ready) ready = init().then(() => void 0);
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
        result = e.create_group_with_compliance(req.groupId, req.complianceKeyPackage);
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd client && npx vitest run src/crypto/worker.test.ts`
Expected: PASS. (Vitest resolves the wasm-pkg ESM; `init()` loads the `.wasm` via Node fs.)

- [ ] **Step 5: Commit**

```bash
git add client/src/crypto/worker.ts client/src/crypto/worker.test.ts
git commit -m "feat(client): crypto worker host + pure dispatch"
```

---

### Task 12: Main-thread `CryptoClient` proxy

**Files:**
- Create: `client/src/crypto/client.ts`

- [ ] **Step 1: Write the failing test**

`client/src/crypto/client.test.ts`:

```ts
import { describe, it, expect, vi } from "vitest";
import { CryptoClient } from "./client";
import type { CryptoResponse } from "./protocol";

// Fake Worker that echoes deterministic responses.
class FakeWorker {
  onmessage: ((ev: MessageEvent) => void) | null = null;
  postMessage(req: { id: string; kind: string }) {
    const res: CryptoResponse =
      req.kind === "keyPackage"
        ? { id: req.id, ok: true, result: new Uint8Array([9, 9]) }
        : { id: req.id, ok: false, error: "unsupported" };
    queueMicrotask(() => this.onmessage?.({ data: res } as MessageEvent));
  }
  terminate() {}
}

describe("CryptoClient", () => {
  it("resolves a request with the matching response by id", async () => {
    const client = new CryptoClient(new FakeWorker() as unknown as Worker, "alice@corp");
    const kp = await client.keyPackage();
    expect(Array.from(kp)).toEqual([9, 9]);
  });

  it("rejects when the worker reports an error", async () => {
    const client = new CryptoClient(new FakeWorker() as unknown as Worker, "alice@corp");
    await expect(client.createGroup("team-1")).rejects.toThrow("unsupported");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd client && npx vitest run src/crypto/client.test.ts`
Expected: FAIL — cannot find `./client`.

- [ ] **Step 3: Write minimal implementation**

`client/src/crypto/client.ts`:

```ts
import type { CryptoRequest, CryptoResponse } from "./protocol";
import { isCryptoResponse } from "./protocol";

type Pending = { resolve: (v: unknown) => void; reject: (e: Error) => void };

export class CryptoClient {
  private seq = 0;
  private pending = new Map<string, Pending>();

  constructor(private worker: Worker, name: string) {
    this.worker.onmessage = (ev: MessageEvent) => {
      const data = ev.data;
      if (!isCryptoResponse(data)) return;
      const p = this.pending.get(data.id);
      if (!p) return;
      this.pending.delete(data.id);
      if (data.ok) p.resolve(data.result);
      else p.reject(new Error(data.error));
    };
    // Send the device name once so the worker can build its engine.
    (this.worker as unknown as { postMessage: (m: unknown) => void }).postMessage({
      id: "init",
      kind: "keyPackage",
      name,
    });
  }

  private send(req: Omit<CryptoRequest, "id">): Promise<unknown> {
    const id = `r${++this.seq}`;
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
      this.worker.postMessage({ ...req, id });
    });
  }

  async keyPackage(): Promise<Uint8Array> {
    return (await this.send({ kind: "keyPackage" })) as Uint8Array;
  }
  async createGroup(groupId: string): Promise<void> {
    await this.send({ kind: "createGroup", groupId });
  }
  async addMember(groupId: string, keyPackage: Uint8Array) {
    return (await this.send({ kind: "addMember", groupId, keyPackage })) as {
      commit: Uint8Array;
      welcome: Uint8Array;
    };
  }
  async joinFromWelcome(welcome: Uint8Array): Promise<void> {
    await this.send({ kind: "joinFromWelcome", welcome });
  }
  async encrypt(groupId: string, plaintext: Uint8Array): Promise<Uint8Array> {
    return (await this.send({ kind: "encrypt", groupId, plaintext })) as Uint8Array;
  }
  async decrypt(groupId: string, message: Uint8Array): Promise<Uint8Array> {
    return (await this.send({ kind: "decrypt", groupId, message })) as Uint8Array;
  }
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd client && npx vitest run src/crypto/client.test.ts`
Expected: PASS (both cases).

- [ ] **Step 5: Commit**

```bash
git add client/src/crypto/client.ts client/src/crypto/client.test.ts
git commit -m "feat(client): main-thread CryptoClient proxy over the worker"
```

---

### Task 13: End-to-end integration — two engines exchange a message in JS

**Files:**
- Create: `client/src/crypto/e2e.test.ts`

Proves the full stack (WASM ↔ worker dispatch ↔ contract) works, including the compliance member.

- [ ] **Step 1: Write the failing test**

`client/src/crypto/e2e.test.ts`:

```ts
import { describe, it, expect } from "vitest";
import { handleRequest } from "./worker";

// Drives two logical devices via the pure dispatch fn (no real Worker needed).
// NOTE: handleRequest holds one engine per module instance, so we exercise the
// round-trip through serialized bytes exactly as the transport would.
describe("crypto e2e via worker dispatch", () => {
  it("alice -> bob message round-trips and bob decrypts", async () => {
    // This test uses two separate worker module contexts.
    const alice = await import("./worker?alice");
    const bob = await import("./worker?bob");

    const bobKp = (await bob.handleRequest("bob@corp", { id: "1", kind: "keyPackage" }))
      .result as Uint8Array;

    await alice.handleRequest("alice@corp", { id: "2", kind: "createGroup", groupId: "team-1" });
    const add = (await alice.handleRequest("alice@corp", {
      id: "3", kind: "addMember", groupId: "team-1", keyPackage: bobKp,
    })).result as { commit: Uint8Array; welcome: Uint8Array };

    await bob.handleRequest("bob@corp", {
      id: "4", kind: "joinFromWelcome", welcome: add.welcome,
    });

    const ct = (await alice.handleRequest("alice@corp", {
      id: "5", kind: "encrypt", groupId: "team-1", plaintext: new TextEncoder().encode("hi bob"),
    })).result as Uint8Array;

    const pt = (await bob.handleRequest("bob@corp", {
      id: "6", kind: "decrypt", groupId: "team-1", message: ct,
    })).result as Uint8Array;

    expect(new TextDecoder().decode(pt)).toBe("hi bob");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd client && npx vitest run src/crypto/e2e.test.ts`
Expected: FAIL initially if the `?alice`/`?bob` import-suffix trick does not yield separate engine instances. If so, refactor `worker.ts` to export a `createDispatcher()` factory returning its own `{ handleRequest }` (one engine per dispatcher) and rewrite the test to call `createDispatcher("alice@corp")` / `createDispatcher("bob@corp")`. Re-run.

- [ ] **Step 3: Make it pass (factory refactor if needed)**

If the refactor was needed, in `client/src/crypto/worker.ts` replace the module-global `engine`/`ensureEngine` with:

```ts
export function createDispatcher(name: string) {
  let engine: WasmEngine | null = null;
  let ready: Promise<void> | null = null;
  async function ensure(): Promise<WasmEngine> {
    if (!ready) ready = init().then(() => void 0);
    await ready;
    if (!engine) engine = new WasmEngine(name);
    return engine;
  }
  async function handleRequest(req: CryptoRequest): Promise<CryptoResponse> {
    /* same switch body as before, using `await ensure()` */
    return { id: req.id, ok: true, result: null }; // replace with full body
  }
  return { handleRequest };
}
```

Keep the existing top-level `handleRequest(name, req)` as a thin wrapper over a default dispatcher so Tasks 11–12 still pass. Update `e2e.test.ts` to use `createDispatcher`.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd client && npx vitest run src/crypto/e2e.test.ts`
Expected: PASS — bob decrypts "hi bob".

- [ ] **Step 5: Run the full client + crypto-core test suites**

Run:

```bash
cd crypto-core && cargo test && cargo test --features test-utils --test rfc_vectors
cd ../client && npx vitest run
```

Expected: all green.

- [ ] **Step 6: Commit**

```bash
git add client/src/crypto/e2e.test.ts client/src/crypto/worker.ts
git commit -m "test(client): end-to-end crypto round-trip across two engine dispatchers"
```

---

## Self-Review

**Spec coverage:**
- MLS engine (create/add/encrypt/decrypt/remove) → Tasks 3–6 ✓
- MTI ciphersuite `MLS_128_DHKEMX25519_AES128GCM_SHA256_Ed25519` → Task 1 ✓
- KeyPackage publish + round-trip → Task 2 ✓
- Compliance member, visible in membership → Task 7 ✓
- Forward secrecy / PCS invariant → Task 6 ✓
- RFC 9420 test-vector gate → Task 8 ✓
- UI ↔ crypto-worker postMessage contract; keys never leave worker → Tasks 10–12 ✓ (only ciphertext/plaintext cross `CryptoClient`)
- "3rd thread" Web Worker hosting WASM → Tasks 9, 11 ✓
- OPAQUE auth, Key Transparency client → **deliberately out of scope** (separate plans; noted in header). Gap is intentional per Scope Check.

**Placeholder scan:** Task 13 Step 3 contains an intentionally abbreviated `/* same switch body */` — this is conditional refactor guidance, not a required code deliverable; the full switch already exists in Task 11. All required implementation steps contain complete code.

**Type consistency:** `Engine` methods (`create_group`, `add_member`, `join_from_welcome`, `encrypt`, `process`, `remove_member`, `create_group_with_compliance`, `member_count`) are used consistently across Tasks 3–9. `WasmEngine.decrypt` maps `Incoming` → bytes. `CryptoRequest.kind` values match the `worker.ts` switch and `CryptoClient` methods one-to-one. `AddResult { commit, welcome }` ↔ `WasmAddResult` getters ↔ TS `{ commit, welcome }` are aligned.

**API-version risk (call out, not a placeholder):** OpenMLS 0.6 method names are stable for the operations used, but two spots warrant a quick `cargo doc` check on the pinned version during Task execution: the RFC-vector runner path in Task 8 Step 4 (noted inline) and `KeyPackageIn::validate` signature in Task 2. Both have inline fallback instructions.
