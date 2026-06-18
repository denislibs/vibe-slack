use openmls::prelude::Ciphersuite;
use wasm_bindgen::prelude::*;

use crate::engine::{Engine, Incoming};

pub mod engine;
pub mod errors;
pub mod identity;

pub const DEFAULT_CIPHERSUITE: Ciphersuite =
    Ciphersuite::MLS_128_DHKEMX25519_AES128GCM_SHA256_Ed25519;

/// Returned by add_member: serialized commit + welcome.
#[wasm_bindgen]
pub struct WasmAddResult {
    commit: Vec<u8>,
    welcome: Vec<u8>,
}

#[wasm_bindgen]
impl WasmAddResult {
    #[wasm_bindgen(getter)]
    pub fn commit(&self) -> Vec<u8> {
        self.commit.clone()
    }
    #[wasm_bindgen(getter)]
    pub fn welcome(&self) -> Vec<u8> {
        self.welcome.clone()
    }
}

// Native-test convenience (not compiled to wasm consumers).
#[cfg(test)]
impl WasmAddResult {
    pub fn unwrap_welcome(self) -> Vec<u8> {
        self.welcome
    }
}

#[wasm_bindgen]
pub struct WasmEngine {
    inner: Engine,
}

#[wasm_bindgen]
impl WasmEngine {
    #[wasm_bindgen(constructor)]
    pub fn new(name: &str) -> WasmEngine {
        WasmEngine {
            inner: Engine::new(name.as_bytes()),
        }
    }

    pub fn key_package_bytes(&self) -> Result<Vec<u8>, JsError> {
        self.inner.key_package_bytes().map_err(to_js)
    }

    pub fn signing_public_key(&self) -> Vec<u8> {
        self.inner.signing_public_key()
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
        Ok(WasmAddResult {
            commit: r.commit,
            welcome: r.welcome,
        })
    }

    pub fn remove_member(&mut self, group_id: &str, leaf_index: u32) -> Result<Vec<u8>, JsError> {
        self.inner
            .remove_member(group_id.as_bytes(), leaf_index)
            .map_err(to_js)
    }

    pub fn join_from_welcome(&mut self, welcome: Vec<u8>) -> Result<(), JsError> {
        self.inner.join_from_welcome(&welcome).map_err(to_js)
    }

    /// Export the group's GroupInfo (with public ratchet tree) for joining a
    /// PUBLIC channel via external commit.
    pub fn export_group_info(&self, group_id: &str) -> Result<Vec<u8>, JsError> {
        self.inner
            .export_group_info(group_id.as_bytes())
            .map_err(to_js)
    }

    /// Join a PUBLIC channel via external commit; returns the commit to fan out.
    pub fn join_by_external_commit(&mut self, group_info: Vec<u8>) -> Result<Vec<u8>, JsError> {
        self.inner
            .join_by_external_commit(&group_info)
            .map_err(to_js)
    }

    pub fn encrypt(&mut self, group_id: &str, plaintext: Vec<u8>) -> Result<Vec<u8>, JsError> {
        self.inner
            .encrypt(group_id.as_bytes(), &plaintext)
            .map_err(to_js)
    }

    /// Returns plaintext for application messages; empty Vec for commits/proposals.
    pub fn decrypt(&mut self, group_id: &str, message: Vec<u8>) -> Result<Vec<u8>, JsError> {
        match self
            .inner
            .process(group_id.as_bytes(), &message)
            .map_err(to_js)?
        {
            Incoming::Application(pt) => Ok(pt),
            Incoming::CommitApplied | Incoming::ProposalStored => Ok(Vec::new()),
        }
    }

    /// Serialize the engine's full state (identity + groups) for persistence.
    pub fn export_state(&self) -> Result<Vec<u8>, JsError> {
        self.inner.export_state().map_err(to_js)
    }

    /// Rebuild an engine from `export_state` bytes (e.g. after a page reload).
    pub fn restore(state: Vec<u8>) -> Result<WasmEngine, JsError> {
        Ok(WasmEngine { inner: Engine::restore(&state).map_err(to_js)? })
    }
}

fn to_js(e: crate::errors::EngineError) -> JsError {
    JsError::new(&e.to_string())
}

#[cfg(test)]
mod tests {
    #[test]
    fn ciphersuite_is_mti() {
        use openmls::prelude::Ciphersuite;
        let cs = crate::DEFAULT_CIPHERSUITE;
        assert_eq!(cs, Ciphersuite::MLS_128_DHKEMX25519_AES128GCM_SHA256_Ed25519);
    }
}

#[cfg(test)]
mod wasm_api_tests {
    use super::*;

    #[test]
    fn wasm_engine_round_trips_a_message() {
        let mut alice = WasmEngine::new("alice@corp");
        let mut bob = WasmEngine::new("bob@corp");

        let bob_kp = bob.key_package_bytes().unwrap();
        alice.create_group("team-1").unwrap();
        let welcome = alice.add_member("team-1", bob_kp).unwrap().unwrap_welcome();
        bob.join_from_welcome(welcome).unwrap();

        let ct = alice.encrypt("team-1", b"hi".to_vec()).unwrap();
        let pt = bob.decrypt("team-1", ct).unwrap();
        assert_eq!(pt, b"hi");
    }

    #[test]
    fn wasm_engine_joins_public_group_by_external_commit() {
        let mut alice = WasmEngine::new("alice@corp");
        let mut bob = WasmEngine::new("bob@corp");

        alice.create_group("public-1").unwrap();
        let gi = alice.export_group_info("public-1").unwrap();
        assert!(!gi.is_empty());

        let commit = bob.join_by_external_commit(gi).unwrap();
        assert!(!commit.is_empty());

        // Alice applies the external-commit; decrypt returns empty for commits.
        let applied = alice.decrypt("public-1", commit).unwrap();
        assert!(applied.is_empty());

        let ct = alice.encrypt("public-1", b"hello bob".to_vec()).unwrap();
        let pt = bob.decrypt("public-1", ct).unwrap();
        assert_eq!(pt, b"hello bob");
    }

    #[test]
    fn wasm_engine_round_trips_state() {
        let mut alice = WasmEngine::new("alice@corp");
        alice.create_group("team-1").unwrap();
        let state = alice.export_state().unwrap();
        let mut restored = WasmEngine::restore(state).unwrap();
        let ct = restored.encrypt("team-1", b"after reload".to_vec()).unwrap();
        assert!(!ct.is_empty());
    }
}
