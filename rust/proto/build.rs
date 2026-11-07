//! Generates the buffa bindings from descriptors.binpb, which Bazel writes
//! (`bazel run //rust/proto:write_descriptors`), so no protoc is needed.

use std::error::Error;

fn main() -> Result<(), Box<dyn Error>> {
    let set = "descriptors.binpb";
    println!("cargo:rerun-if-changed={set}");
    let files = proto_files(&std::fs::read(set)?);
    buffa_build::Config::new()
        .descriptor_set(set)
        .files(&files)
        .generate_views(false)
        .generate_json(true)
        .include_file("_include.rs")
        .compile()?;
    Ok(())
}

/// The names of the files in a serialized FileDescriptorSet, leaving out
/// google/protobuf, whose types buffa-types and buffa-descriptor carry.
///
/// A FileDescriptorSet is field 1, repeated FileDescriptorProto, and a
/// FileDescriptorProto's name is its field 1; reading those two fields is all
/// this needs.
fn proto_files(set: &[u8]) -> Vec<String> {
    let mut out = Vec::new();
    let mut rest = set;
    while let Some((field, value, next)) = length_delimited(rest) {
        rest = next;
        if field != 1 {
            continue;
        }
        let mut file = value;
        while let Some((f, v, n)) = length_delimited(file) {
            file = n;
            if f == 1 {
                let name = String::from_utf8_lossy(v).into_owned();
                if !name.starts_with("google/protobuf/") {
                    out.push(name);
                }
                break;
            }
        }
    }
    out
}

/// The next field of `b`, if it is length-delimited, as its number, its
/// bytes and what follows. Other wire types are skipped.
fn length_delimited(mut b: &[u8]) -> Option<(u64, &[u8], &[u8])> {
    loop {
        let (key, n) = varint(b)?;
        b = &b[n..];
        let field = key >> 3;
        match key & 7 {
            0 => b = &b[varint(b)?.1..],
            1 => b = b.get(8..)?,
            5 => b = b.get(4..)?,
            2 => {
                let (len, n) = varint(b)?;
                let len = usize::try_from(len).ok()?;
                let value = b.get(n..n + len)?;
                return Some((field, value, &b[n + len..]));
            }
            _ => return None,
        }
    }
}

fn varint(b: &[u8]) -> Option<(u64, usize)> {
    let mut v = 0u64;
    for (i, byte) in b.iter().enumerate().take(10) {
        v |= u64::from(byte & 0x7f) << (7 * i);
        if byte & 0x80 == 0 {
            return Some((v, i + 1));
        }
    }
    None
}
