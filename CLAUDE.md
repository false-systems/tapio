# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

TAPIO v4 is a lean, AI-native eBPF edge observer for Kubernetes. It captures kernel-level anomalies (OOM kills, connection failures, I/O errors, CPU stalls), enriches them with K8s pod context, and emits FALSE Protocol Occurrences to pluggable sinks (stdout, file, POLKU, Grafana).

TAPIO is the founding tool of False Systems. It provides context to AI agents — facts, not reasoning. The kernel sees; AI thinks.

Part of the False Systems ecosystem: TAPIO (eBPF edge) → POLKU (protocol hub) → AHTI (central intelligence). Sibling tools: SYKLI (CI), NOPEA (deploy), RAUTA (gateway), RAUHA (runtime).

## Commands

```bash
cargo check --workspace                        # type-check all crates
cargo check -p tapio-common -p tapio-cli       # check platform-independent crates (works on macOS)
cargo test --workspace                         # run all tests
cargo test -p tapio-common                     # test single crate
cargo test -p tapio-common -- test_name        # run single test
cargo clippy --workspace --all-targets -- -D warnings   # lint
cargo fmt --check                              # format check
cargo build --release                          # release build (LTO + strip, ~8MB)

# CI (via sykli — requires cargo +nightly -Zscript)
sykli                                          # run full pipeline: fmt → clippy → test → build
```

Pre-commit hook runs `cargo fmt --check` and `cargo clippy --workspace --all-targets -- -D warnings`. Fix both before committing.

Rust edition 2024, MSRV 1.85. tapio-agent only compiles on Linux (aya dependency).

## Architecture

### Workspace crates

- **tapio-common**: Shared types. `#[repr(C)]` eBPF event structs mirroring the C programs, `kernel.*` event type hierarchy, FALSE Protocol `Occurrence` builder, `Sink` trait.
- **tapio-agent**: DaemonSet binary. Loads eBPF C programs via aya, reads ring buffers, parses events, filters anomalies, enriches with K8s context (kube-rs), emits Occurrences to sinks. Linux-only (aya requires kernel). Internal modules: `observer/` (four observer submodules), `sink/` (`StdoutSink`, `FileSink`, `PolkuSink`), `enricher.rs` (K8s pod enrichment, also `cfg(target_os = "linux")`).
- **tapio-cli**: User/AI interface. Single-file crate (`main.rs`). CLI commands (`tapio status`, `tapio watch`, `tapio health`, `tapio recent`). Reads from `.tapio/occurrences/*.json` — decoupled from the agent process. MCP server (`tapio mcp`) exposes `tapio_recent_anomalies`, `tapio_node_health`, `tapio_watch_stream` tools via stdio JSON-RPC 2.0.

### eBPF programs (C, in `ebpf/`)

Four C programs compiled with clang, loaded at runtime by aya. These capture raw kernel data — all parsing and filtering happens in Rust userspace.

| Program | Tracepoints | Detects |
|---------|------------|---------|
| `network_monitor.c` | `inet_sock_set_state`, `tcp_receive_reset`, `tcp_retransmit_skb` | Connection failures, RST storms, retransmit spikes, RTT degradation |
| `container_monitor.c` | `sched_process_exit`, `oom/mark_victim` | OOM kills, abnormal container exits |
| `storage_monitor.c` | `block_rq_issue`, `block_rq_complete` | I/O errors, latency spikes |
| `node_pmc_monitor.c` | perf_event counters | CPU IPC degradation, memory stalls |

Shared headers in `ebpf/headers/`: `vmlinux_minimal.h`, `conn_tracking.h`, `metrics.h`, `tcp.h`, `node_pmc_monitor.h`.

### eBPF compilation

No build script exists — eBPF C programs are compiled manually with clang and placed in `--ebpf-dir` (default `/opt/tapio/ebpf`). The agent loads pre-compiled `.o` files at runtime via `aya::Ebpf::load_file()`.

```bash
# Compile a single eBPF program (example)
clang -O2 -g -target bpf -D__TARGET_ARCH_x86 -I ebpf/headers \
  -c ebpf/network_monitor.c -o /opt/tapio/ebpf/network_monitor.o
```

Requires kernel 5.8+ and capabilities: `CAP_BPF`, `CAP_PERFMON`, `CAP_NET_ADMIN`. The agent runs as a privileged DaemonSet.

### Event flow

```
eBPF (C) → ring buffer → parse (#[repr(C)] structs) → filter (anomaly?) → enrich (K8s pod context) → Occurrence → Sink
```

### Sinks (pluggable output)

Implement the `Sink` trait from `tapio-common/src/sink.rs` (synchronous — async wrappers belong in the agent). Current: `StdoutSink` (JSON lines), `FileSink` (one `.json` per occurrence in `.tapio/`), `PolkuSink` (HTTP POST with batching and exponential backoff), `GrafanaSink` (OTLP/HTTP with gzip, maps Occurrences to LogRecords) in `tapio-agent/src/sink/`, plus `MultiSink` (fan-out, logs errors, doesn't short-circuit) defined in `tapio-agent/src/main.rs`.

### FALSE Protocol

TAPIO emits Occurrences in the `kernel.*` type namespace. It fills factual fields (error code, message, data) but NOT reasoning fields. AI agents and AHTI do the reasoning.

### MCP server

Exposes kernel context to AI agents via stdio JSON-RPC 2.0 transport (`tapio mcp`). Three tools: `tapio_recent_anomalies` (filtered by minutes/observer/severity), `tapio_node_health` (health status + anomaly counts), `tapio_watch_stream` (snapshot of recent events). Tools return data, not analysis.

## Rules

- **No `.unwrap()` in production code** — use `?` or proper error handling
- **No `println!`** — use `tracing::{info, warn, error, debug}`
- **No dead code, no stubs, no TODOs** — complete or don't commit
- **`#[repr(C)]` structs must match C layouts exactly** — a mismatch silently corrupts data. Every struct in `ebpf.rs` has a `size_of` assertion test. Use `std::ptr::read_unaligned` for packed structs to avoid UB
- **TAPIO provides context, not reasoning** — facts in Occurrences, no `possible_causes` or `suggested_fix` fields
- **aya is Linux-only** — use `cfg(target_os = "linux")` for eBPF code and K8s enricher, keep tapio-common and tapio-cli platform-independent
- **Lean** — every dependency must justify its existence. Target: <8MB release binary, <10MB RSS

## Testing patterns

Each observer module (`network.rs`, `container.rs`, `storage.rs`, `node_pmc.rs`) tests `classify()` and `build_occurrence()` independently of the eBPF `run()` loop. Test helpers construct events via `unsafe { std::mem::zeroed::<T>() }` then set relevant fields. Occurrence tests call `.validate()` to confirm correctness and test JSON serialization round-trips.

## Agent CLI flags

The `tapio-agent` binary accepts:
- `--config <path>` — TOML config file (default: `/etc/tapio/tapio.toml`)
- `--sink <name>` — output sink (`stdout`, `file`, `polku`, `grafana`), repeatable for multi-sink
- `--ebpf-dir <path>` — directory with compiled `.o` files (default: `/opt/tapio/ebpf`)
- `--data-dir <path>` — directory for file sink output (default: `.tapio/occurrences`)

## Environment variables

- **`NODE_NAME`**: Required for K8s enrichment. Set to the Kubernetes node name. If unset, enrichment is disabled (occurrences emitted without pod context). Typically set via the Downward API in the DaemonSet spec.
- **`RUST_LOG`**: Controls log verbosity (e.g. `RUST_LOG=info` or `RUST_LOG=tapio_agent=debug`).
- **`TAPIO_DATA_DIR`**: Override the CLI's default data directory (default: `.tapio/occurrences`).

## Observability

- **Logging**: `tracing` crate, env-filtered via `RUST_LOG`
- **Metrics**: `prometheus` crate on configurable port (default `:9090`) via axum. Enable with `[metrics] enabled = true` in config. Families: `tapio_events_total`, `tapio_anomalies_total`, `tapio_lost_events_total`, `tapio_sink_writes_total`, `tapio_k8s_cache_size`, `tapio_k8s_reflector_up`
- **Metric prefix**: `tapio_`
- **eBPF-side metrics**: Per-CPU counters in `tapio_metrics` map (defined in `metrics.h`), including `METRIC_NETWORK_BASELINE_REJECTED` for RTT baseline health
