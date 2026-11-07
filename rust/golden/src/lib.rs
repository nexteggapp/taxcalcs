//! Reads the golden vectors in the repository's `golden/testdata`, which the
//! Go test in `golden/` writes from fixed inputs, so a Rust test can check
//! that it computes exactly what Go does.
//!
//! A vector file is JSON. A protobuf message in one is Go's deterministic
//! binary encoding in base64; [`decode`] turns it back into a message, and a
//! test compares messages, never bytes, because buffa and Go order oneof
//! fields differently on the wire.

use std::fmt::Debug;
use std::path::PathBuf;

use base64::Engine as _;
use serde::de::DeserializeOwned;

/// The path of `name` under `golden/testdata`.
pub fn path(name: &str) -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("../../golden/testdata")
        .join(name)
}

/// The path of `rel`, a path from the repository root, such as
/// `returns/rules/federal`.
pub fn repo_path(rel: &str) -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("../..")
        .join(rel)
}

/// Reads and parses the vector file `name`.
///
/// # Panics
///
/// When the file is missing or is not a `T`, which fails the test calling it.
pub fn read<T: DeserializeOwned>(name: &str) -> T {
    let p = path(name);
    let text =
        std::fs::read_to_string(&p).unwrap_or_else(|e| panic!("reading {}: {e}", p.display()));
    serde_json::from_str(&text).unwrap_or_else(|e| panic!("parsing {}: {e}", p.display()))
}

/// The bytes of a base64 field.
///
/// # Panics
///
/// When `b64` is not base64.
pub fn bytes(b64: &str) -> Vec<u8> {
    base64::engine::general_purpose::STANDARD
        .decode(b64)
        .unwrap_or_else(|e| panic!("bad base64 in a vector: {e}"))
}

/// The message a base64 field encodes; the default message for "".
///
/// # Panics
///
/// When the bytes are not an `M`.
pub fn decode<M: buffa::Message + Default>(b64: &str) -> M {
    M::decode_from_slice(&bytes(b64)).unwrap_or_else(|e| panic!("decoding a vector: {e}"))
}

/// Fails with the first differing line of the two values' pretty debug
/// forms, which is far easier to read than two whole messages.
///
/// # Panics
///
/// When `got != want`.
#[track_caller]
pub fn assert_same<T: PartialEq + Debug>(context: &str, got: &T, want: &T) {
    if got == want {
        return;
    }
    let (g, w) = (format!("{got:#?}"), format!("{want:#?}"));
    let (gl, wl): (Vec<&str>, Vec<&str>) = (g.lines().collect(), w.lines().collect());
    let at = gl
        .iter()
        .zip(&wl)
        .position(|(a, b)| a != b)
        .unwrap_or(gl.len().min(wl.len()));
    let from = at.saturating_sub(8);
    panic!(
        "{context}: Rust differs from Go at line {at}\n--- Go\n{}\n--- Rust\n{}",
        wl[from..(at + 8).min(wl.len())].join("\n"),
        gl[from..(at + 8).min(gl.len())].join("\n"),
    );
}
