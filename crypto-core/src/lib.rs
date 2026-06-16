use openmls::prelude::Ciphersuite;

pub const DEFAULT_CIPHERSUITE: Ciphersuite =
    Ciphersuite::MLS_128_DHKEMX25519_AES128GCM_SHA256_Ed25519;

#[cfg(test)]
mod tests {
    #[test]
    fn ciphersuite_is_mti() {
        use openmls::prelude::Ciphersuite;
        let cs = crate::DEFAULT_CIPHERSUITE;
        assert_eq!(cs, Ciphersuite::MLS_128_DHKEMX25519_AES128GCM_SHA256_Ed25519);
    }
}
