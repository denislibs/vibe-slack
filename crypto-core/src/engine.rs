use crate::errors::EngineError;
use crate::identity::{deserialize_key_package, Identity};
use crate::DEFAULT_CIPHERSUITE;
use openmls::prelude::*;
use openmls_rust_crypto::OpenMlsRustCrypto;
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use tls_codec::{Deserialize as _, Serialize as _};

#[derive(Serialize, Deserialize)]
struct PersistedState {
    name: Vec<u8>,
    signer: openmls_basic_credential::SignatureKeyPair,
    // Stored as a list of (key, value) pairs because serde_json cannot use
    // non-string map keys (the storage keys are raw `Vec<u8>`).
    storage: Vec<(Vec<u8>, Vec<u8>)>,
    group_ids: Vec<Vec<u8>>,
}

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
    name: Vec<u8>,
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
            name: name.to_vec(),
        }
    }

    /// Serialize the full device state (identity signer, MLS key-value storage,
    /// and tracked group ids) so it can survive a process/page reload.
    pub fn export_state(&self) -> Result<Vec<u8>, EngineError> {
        let storage: Vec<(Vec<u8>, Vec<u8>)> = self
            .provider
            .storage()
            .values
            .read()
            .unwrap()
            .iter()
            .map(|(k, v)| (k.clone(), v.clone()))
            .collect();
        // `SignatureKeyPair`'s private accessor is gated behind `test-utils`, but
        // it derives serde, so clone it by round-tripping through serde rather
        // than via `from_raw`/`private()`.
        let signer_bytes =
            serde_json::to_vec(&self.identity.signer).map_err(EngineError::serde)?;
        let signer = serde_json::from_slice(&signer_bytes).map_err(EngineError::serde)?;
        let state = PersistedState {
            name: self.name.clone(),
            signer,
            storage,
            group_ids: self.groups.keys().cloned().collect(),
        };
        serde_json::to_vec(&state).map_err(EngineError::serde)
    }

    /// Reconstruct an engine from bytes produced by `export_state`.
    pub fn restore(bytes: &[u8]) -> Result<Self, EngineError> {
        let state: PersistedState = serde_json::from_slice(bytes).map_err(EngineError::serde)?;
        let provider = OpenMlsRustCrypto::default();
        *provider.storage().values.write().unwrap() = state.storage.into_iter().collect();

        let signer = state.signer;
        let credential = BasicCredential::new(state.name.clone());
        let credential_with_key = CredentialWithKey {
            credential: credential.into(),
            signature_key: signer.to_public_vec().into(),
        };
        let identity = Identity {
            signer,
            credential_with_key,
        };

        let mut groups = HashMap::new();
        for gid in state.group_ids {
            let group = MlsGroup::load(provider.storage(), &GroupId::from_slice(&gid))
                .map_err(EngineError::mls)?
                .ok_or_else(|| EngineError::UnknownGroup(hex(&gid)))?;
            groups.insert(gid, group);
        }

        Ok(Self {
            provider,
            identity,
            groups,
            name: state.name,
        })
    }

    pub fn key_package_bytes(&self) -> Result<Vec<u8>, EngineError> {
        self.identity.key_package_bytes(&self.provider)
    }

    /// The device's Ed25519 signing public key (the MLS credential's signature key),
    /// for registering the device with the Authentication Service.
    pub fn signing_public_key(&self) -> Vec<u8> {
        self.identity.signer.to_public_vec()
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

    /// Remove a member by leaf index, returning the commit to fan out.
    ///
    /// DELIVERY CONTRACT: like `add_member`, this merges the pending commit
    /// locally *before* returning (optimistic merge), so the group advances one
    /// epoch as soon as this call succeeds. The caller MUST reliably deliver the
    /// returned commit to all remaining members; if delivery fails, this device
    /// will be one epoch ahead of its peers with no rollback.
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
        group
            .merge_pending_commit(&self.provider)
            .map_err(EngineError::mls)?;
        commit.tls_serialize_detached().map_err(EngineError::serde)
    }

    /// Create a group and immediately add the org compliance member.
    /// Returns the Welcome to deliver to the compliance device.
    ///
    /// The compliance member is a normal, VISIBLE MLS member: the server still
    /// cannot read traffic, but an auditor holding the escrowed compliance key can
    /// decrypt the archive, and the member appears in the group roster (no hidden
    /// participant).
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

    /// Export the group's GroupInfo (with ratchet tree) so a new member can join a
    /// PUBLIC channel via external commit. Not secret: the ratchet tree is public.
    pub fn export_group_info(&self, group_id: &[u8]) -> Result<Vec<u8>, EngineError> {
        let group = self
            .groups
            .get(group_id)
            .ok_or_else(|| EngineError::UnknownGroup(hex(group_id)))?;
        let msg = group
            .export_group_info(&self.provider, &self.identity.signer, true)
            .map_err(EngineError::mls)?;
        msg.tls_serialize_detached().map_err(EngineError::serde)
    }

    /// Join a PUBLIC channel via an MLS external commit, using the group's
    /// exported GroupInfo (which carries the public ratchet tree). Returns the
    /// external-commit message to fan out to existing members.
    pub fn join_by_external_commit(
        &mut self,
        group_info_bytes: &[u8],
    ) -> Result<Vec<u8>, EngineError> {
        let msg_in =
            MlsMessageIn::tls_deserialize_exact(group_info_bytes).map_err(EngineError::serde)?;
        let vgi = match msg_in.extract() {
            MlsMessageBodyIn::GroupInfo(gi) => gi,
            _ => return Err(EngineError::Mls("expected a GroupInfo message".to_string())),
        };
        let config = MlsGroupJoinConfig::builder()
            .use_ratchet_tree_extension(true)
            .build();
        let (mut group, commit, _group_info) = MlsGroup::join_by_external_commit(
            &self.provider,
            &self.identity.signer,
            None, // ratchet_tree carried in the GroupInfo extension
            vgi,
            &config,
            None, // capabilities: default
            None, // extensions: default
            &[],  // aad
            self.identity.credential_with_key.clone(),
        )
        .map_err(EngineError::mls)?;
        group
            .merge_pending_commit(&self.provider)
            .map_err(EngineError::mls)?;
        let gid = group.group_id().as_slice().to_vec();
        self.groups.insert(gid, group);
        commit.tls_serialize_detached().map_err(EngineError::serde)
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
impl Incoming {
    pub fn unwrap_application(self) -> Vec<u8> {
        match self {
            Incoming::Application(p) => p,
            other => panic!("not application: {:?}", other),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn bob_joins_public_group_by_external_commit() {
        let mut alice = Engine::new(b"alice@corp");
        let mut bob = Engine::new(b"bob@corp");
        let group_id = b"public-1".to_vec();
        alice.create_group(&group_id).unwrap();

        let gi = alice.export_group_info(&group_id).unwrap();
        assert!(!gi.is_empty());

        let commit = bob.join_by_external_commit(&gi).unwrap();
        assert!(bob.has_group(&group_id));
        assert!(!commit.is_empty());

        alice.process(&group_id, &commit).unwrap();
        let ct = alice.encrypt(&group_id, b"hello bob").unwrap();
        assert_eq!(bob.process(&group_id, &ct).unwrap().unwrap_application(), b"hello bob");
    }

    #[test]
    fn engine_exposes_signing_public_key() {
        let e = Engine::new(b"alice@corp");
        let pk = e.signing_public_key();
        assert_eq!(pk.len(), 32, "Ed25519 public key is 32 bytes");
        assert_eq!(pk, e.signing_public_key()); // stable across calls
    }

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
        // Bob processes the removal commit (he learns he is out; result ignored).
        let _ = bob.process(&group_id, &remove);

        // Alice sends a new-epoch message.
        let ct = alice.encrypt(&group_id, b"secret after removal").unwrap();

        // Guard against a false pass: Bob must still be tracking the group, so
        // the failure below is genuine eviction — not a missing-group lookup error.
        assert!(bob.has_group(&group_id), "Bob must still track the group locally");

        // Bob must NOT be able to decrypt it, and the failure must be an MLS
        // eviction/decryption error (not UnknownGroup or a deserialize error).
        match bob.process(&group_id, &ct) {
            Err(EngineError::Mls(_)) => {}
            other => panic!(
                "removed member must fail to decrypt with an MLS error, got {:?}",
                other
            ),
        }

        // Positive control: Alice (still a member) decrypts her own new-epoch
        // traffic, proving the epoch itself is functional — only Bob is locked out.
        let mut carol = Engine::new(b"carol@corp");
        let add_carol = alice
            .add_member(&group_id, &carol.key_package_bytes().unwrap())
            .unwrap();
        carol.join_from_welcome(&add_carol.welcome).unwrap();
        let ct2 = alice.encrypt(&group_id, b"still working").unwrap();
        match carol.process(&group_id, &ct2).unwrap() {
            Incoming::Application(pt) => assert_eq!(pt, b"still working"),
            other => panic!("expected application message, got {:?}", other),
        }
    }

    #[test]
    fn remaining_member_applies_membership_commit() {
        let mut alice = Engine::new(b"alice@corp");
        let mut bob = Engine::new(b"bob@corp");
        let carol = Engine::new(b"carol@corp");

        let group_id = b"team-1".to_vec();
        alice.create_group(&group_id).unwrap();

        // Add Bob first.
        let add_bob = alice.add_member(&group_id, &bob.key_package_bytes().unwrap()).unwrap();
        bob.join_from_welcome(&add_bob.welcome).unwrap();

        // Now Alice adds Carol; Bob (existing member) must apply the commit.
        let add_carol = alice.add_member(&group_id, &carol.key_package_bytes().unwrap()).unwrap();
        let applied = bob.process(&group_id, &add_carol.commit).unwrap();
        assert!(matches!(applied, Incoming::CommitApplied),
            "existing member should apply the add-Carol commit, got {:?}", applied);
    }

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

    #[test]
    fn engine_state_survives_export_restore() {
        let mut alice = Engine::new(b"alice@corp");
        let group_id = b"team-1".to_vec();
        alice.create_group(&group_id).unwrap();
        let ct_before = alice.encrypt(&group_id, b"before reload").unwrap();
        assert!(!ct_before.is_empty());
        let state = alice.export_state().unwrap();
        assert!(!state.is_empty());
        drop(alice);
        let mut restored = Engine::restore(&state).unwrap();
        assert!(restored.has_group(&group_id), "group must survive restore");
        let ct_after = restored.encrypt(&group_id, b"after reload").unwrap();
        assert!(!ct_after.is_empty());
        assert_eq!(restored.signing_public_key().len(), 32);
    }

    #[test]
    fn restored_engine_decrypts_peer_message() {
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
        assert_eq!(bob.process(&gid, &ct).unwrap().unwrap_application(), b"hi bob after reload");
    }

    #[test]
    fn process_rejects_malformed_bytes() {
        let mut alice = Engine::new(b"alice@corp");
        let group_id = b"team-1".to_vec();
        alice.create_group(&group_id).unwrap();
        let result = alice.process(&group_id, &[0xde, 0xad, 0xbe, 0xef]);
        assert!(result.is_err(), "malformed inbound bytes must be rejected cleanly");
    }
}
