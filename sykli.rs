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

    // Keep controller/server/Kubernetes deps out of the node binary.
    s.task("agent-deps")
        .run("scripts/check-agent-deps.sh")
        .inputs(&["Cargo.toml", "Cargo.lock", "scripts/check-agent-deps.sh"]);

    // Tests (after static analysis)
    s.task("test")
        .run("cargo test --workspace")
        .after(&["fmt-check", "clippy", "agent-deps"])
        .inputs(&["**/*.rs", "Cargo.toml", "Cargo.lock"]);

    // Build (after tests)
    s.task("build")
        .run("cargo build --release")
        .after(&["test"]);

    s.emit();
}
