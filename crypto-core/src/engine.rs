use crate::errors::EngineError;
use crate::identity::{deserialize_key_package, Identity};
use crate::DEFAULT_CIPHERSUITE;
use openmls::prelude::*;
use openmls_rust_crypto::OpenMlsRustCrypto;
use std::collections::HashMap;
use tls_codec::{Deserialize as _, Serialize as _};

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
        Self {
            provider,
            identity,
            groups: HashMap::new(),
        }
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

        group
            .merge_pending_commit(&self.provider)
            .map_err(EngineError::mls)?;

        Ok(AddResult {
            commit: commit.tls_serialize_detached().map_err(EngineError::serde)?,
            welcome: welcome.tls_serialize_detached().map_err(EngineError::serde)?,
        })
    }

    pub fn join_from_welcome(&mut self, welcome_bytes: &[u8]) -> Result<(), EngineError> {
        let msg_in =
            MlsMessageIn::tls_deserialize_exact(welcome_bytes).map_err(EngineError::serde)?;
        // `into_welcome()` is gated behind the `test-utils`/`test` cfg in openmls
        // 0.6, so we use the public `extract()` + body match instead.
        let welcome = match msg_in.extract() {
            MlsMessageBodyIn::Welcome(welcome) => welcome,
            _ => return Err(EngineError::Mls("expected a Welcome message".to_string())),
        };
        let config = MlsGroupJoinConfig::builder()
            .use_ratchet_tree_extension(true)
            .build();
        let staged = StagedWelcome::new_from_welcome(&self.provider, &config, welcome, None)
            .map_err(EngineError::mls)?;
        let group = staged
            .into_group(&self.provider)
            .map_err(EngineError::mls)?;
        let gid = group.group_id().as_slice().to_vec();
        self.groups.insert(gid, group);
        Ok(())
    }
}

fn hex(bytes: &[u8]) -> String {
    bytes.iter().map(|b| format!("{:02x}", b)).collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn alice_creates_group_and_adds_bob() {
        let mut alice = Engine::new(b"alice@corp");
        let mut bob = Engine::new(b"bob@corp");

        let bob_kp = bob.key_package_bytes().unwrap();

        let group_id = b"team-1".to_vec();
        alice.create_group(&group_id).unwrap();

        let add = alice.add_member(&group_id, &bob_kp).unwrap();
        assert!(!add.commit.is_empty());
        assert!(!add.welcome.is_empty());

        bob.join_from_welcome(&add.welcome).unwrap();
        assert!(bob.has_group(&group_id));
    }
}
