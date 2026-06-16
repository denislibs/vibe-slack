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

#[cfg(test)]
mod tests {
    use super::*;
    use openmls_rust_crypto::OpenMlsRustCrypto;

    #[test]
    fn generates_identity_and_key_package() {
        let provider = OpenMlsRustCrypto::default();
        let id = Identity::generate(&provider, b"alice@corp").unwrap();

        // Build one key package, serialize it.
        let kp = id.key_package(&provider).unwrap();
        let bytes = kp.tls_serialize_detached().unwrap();
        assert!(!bytes.is_empty());

        // Deserialize+validate, then re-serialize: must be byte-identical (stable round-trip).
        let parsed = deserialize_key_package(&bytes).unwrap();
        let reserialized = parsed.tls_serialize_detached().unwrap();
        assert_eq!(bytes, reserialized, "key package must round-trip byte-for-byte");
    }
}
