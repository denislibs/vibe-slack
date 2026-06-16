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

    /// Add a member, returning the commit (for existing members) and welcome
    /// (for the new member).
    ///
    /// DELIVERY CONTRACT: this merges the pending commit locally *before*
    /// returning, so the group advances one epoch as soon as this call
    /// succeeds (optimistic merge). The caller MUST reliably deliver the
    /// returned `commit` to all existing members; if delivery fails, this
    /// device will be one epoch ahead of peers with no rollback. A future
    /// task may split the merge into a separate post-delivery confirmation
    /// step; until then, treat successful delivery of `commit` as required.
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

    #[test]
    fn alice_encrypts_application_message() {
        let mut alice = Engine::new(b"alice@corp");
        let group_id = b"team-1".to_vec();
        alice.create_group(&group_id).unwrap();

        let ct = alice.encrypt(&group_id, b"hello group").unwrap();
        assert!(!ct.is_empty());
        assert!(ct.windows(11).all(|w| w != b"hello group"));
    }

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
}
