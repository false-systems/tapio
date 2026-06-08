# Repository Guidelines

## Project Structure & Module Organization

Tapio is a Rust 2024 workspace with five crates:

- `tapio-agent/`: Linux agent that loads eBPF objects, reads ring buffers, classifies events, and emits to sinks.
- `tapio-cli/`: platform-independent CLI for inspecting local event data.
- `tapio-common/`: shared event structs, sink traits, occurrence builders, and eBPF ABI mirrors.
- `tapio-controller/`: controller runtime entry points and library code.
- `tapio-wire/`: wire/protocol types shared across components.

eBPF programs live in `ebpf/`, shared headers in `ebpf/headers/`, scripts in `scripts/`, and architecture notes in `docs/`.

## Build, Test, and Development Commands

- `cargo check --workspace`: type-check all crates.
- `cargo check -p tapio-common -p tapio-cli`: check platform-independent crates on non-Linux hosts.
- `cargo test --workspace`: run Rust tests.
- `cargo clippy --workspace --all-targets -- -D warnings`: run CI lint checks.
- `cargo fmt --all --check`: verify formatting.
- `cargo build --release -p tapio-agent`: build the Linux-only agent.
- `cargo build --release -p tapio-cli`: build the CLI.
- `scripts/verify-lean.sh`: run fmt, clippy, tests, release size checks, dependency snapshot, and optional eBPF compile checks.
- `scripts/smoke-ebpf-network.sh`: Linux/Lima smoke test that loads real eBPF and verifies a network occurrence.

## Coding Style & Naming Conventions

Use Rust edition 2024 and MSRV 1.85. Keep code `rustfmt`-clean and `clippy -D warnings` clean. Prefer module names matching existing domains (`observer/network.rs`, `sink/file.rs`). Keep `tapio-common` and `tapio-cli` platform-independent; guard Linux code with `cfg(target_os = "linux")`.

For eBPF ABI structs, `#[repr(C)]` Rust layouts must match C exactly. Update `size_of` assertions when changing structs, and zero C event structs before writing fields.

## Testing Guidelines

Place focused unit tests near the code under test. Observer tests should exercise `classify()` and `build_occurrence()` outside the eBPF run loop, validate occurrences, and cover JSON round trips when output changes. Run `cargo test -p <crate> <test_name>` for targeted checks, then `cargo test --workspace`.

## Commit & Pull Request Guidelines

Git history uses Conventional Commit-style subjects such as `fix: harden ebpf runtime correctness`, `chore: trim stale ebpf state`, and `test: add post-merge verification coverage`.

PRs should include a behavior summary, tests run, linked issues when relevant, and notes for Linux-only or eBPF changes. Include smoke-test results for kernel/runtime changes and document intentional budget increases.

## Security & Configuration Tips

The agent requires Linux kernel 5.8+ with BTF and capabilities including `CAP_BPF`, `CAP_PERFMON`, and `CAP_NET_ADMIN`. Do not leak kernel padding bytes across the ring-buffer boundary; zero reserved event memory before population.
