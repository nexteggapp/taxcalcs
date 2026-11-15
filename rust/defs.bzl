"""Settings every crate under //rust shares, as rust/Cargo.toml sets them."""

# [workspace.lints.rust]. Clippy's lints run under Cargo; see rust/README.md.
RUSTC_FLAGS = [
    "-Dmissing_docs",
    "-Funsafe_code",
]

# A golden test reads the vectors //golden writes, from its runfiles tree,
# whose root TAXCALCS_ROOT names. See //rust/golden.
GOLDEN_DATA = ["//golden:testdata"]

GOLDEN_ENV = {
    "TAXCALCS_ROOT": ".",
    # The taxlisp evaluator recurses deeply, and an unoptimised build's frames
    # outgrow the 2 MiB a test thread gets by default. See .cargo/config.toml.
    "RUST_MIN_STACK": "8388608",
}
