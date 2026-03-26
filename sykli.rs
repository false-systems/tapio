#!/usr/bin/env -S cargo +nightly -Zscript
---
[dependencies]
sykli = { git = "https://github.com/yairfalse/sykli", package = "sykli-sdk-rust" }
---

fn main() {
    let s = sykli::Pipeline::new();

    // Static analysis (parallel)
    s.task("fmt-check")
        .run("cargo fmt --check")
        .inputs(&["**/*.rs", "Cargo.toml", "Cargo.lock"]);

    s.task("clippy")
        .run("cargo clippy --workspace --all-targets -- -D warnings")
        .inputs(&["**/*.rs", "Cargo.toml", "Cargo.lock"]);

    // Tests (after static analysis)
    s.task("test")
        .run("cargo test --workspace")
        .after(&["fmt-check", "clippy"])
        .inputs(&["**/*.rs", "Cargo.toml", "Cargo.lock"]);

    // Build (after tests)
    s.task("build")
        .run("cargo build --release")
        .after(&["test"]);

    s.emit();
}
