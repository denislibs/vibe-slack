//! RFC 9420 (MLS) interop correctness gate.
//!
//! This test asserts that the OpenMLS `message-protection` interop test
//! vectors are vendored alongside the crate and that they cover our pinned
//! mandatory-to-implement (MTI) ciphersuite,
//! `MLS_128_DHKEMX25519_AES128GCM_SHA256_Ed25519` (suite id `0x0001`).
//!
//! ## Why this is a structural gate (and not an in-crate vector runner)
//!
//! The plan's preferred path was to execute the vectors here via an OpenMLS
//! public runner. In openmls 0.6.0 that runner is **not** reachable from an
//! external crate:
//!
//! * The runner lives at
//!   `openmls/src/tree/tests_and_kats/kats/kat_message_protection.rs`
//!   (`MessageProtectionTest` + `run_test_vector`).
//! * Its parent module is declared `#[cfg(test)] pub mod kat_message_protection;`
//!   and `run_test_vector` itself is additionally `#[cfg(test)]`.
//! * `#[cfg(test)]` items are compiled only for OpenMLS's *own* `cargo test`
//!   invocation; they are not built even with the `test-utils` feature, and the
//!   `MessageProtectionTest` fields are private. There is therefore no public,
//!   callable runner we can invoke without forking/hacking around privacy.
//!
//! OpenMLS already executes these exact vectors in its own test suite, so our
//! pinned dependency is vector-verified upstream. Our gate here ensures the
//! vectors are vendored (so a dependency bump must consciously re-fetch them)
//! and that they cover our MTI ciphersuite.

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
