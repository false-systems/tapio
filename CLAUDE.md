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

# CI (via sykli)
sykli                                          # run full pipeline: fmt → clippy → test → build
```

## Architecture

### Workspace crates

- **tapio-common**: Shared types. `#[repr(C)]` eBPF event structs mirroring the C programs, `kernel.*` event type hierarchy, FALSE Protocol `Occurrence` builder, `Sink` trait.
- **tapio-agent**: DaemonSet binary. Loads eBPF C programs via aya, reads ring buffers, parses events, filters anomalies, enriches with K8s context (kube-rs), emits Occurrences to sinks. Linux-only (aya requires kernel). Exposes Prometheus metrics on `:9090`.
- **tapio-cli**: User/AI interface. CLI commands (`tapio status`, `tapio watch`, `tapio health`, `tapio recent`) and MCP server (`tapio mcp`) for AI agent integration.

### eBPF programs (C, in `ebpf/`)

Four C programs compiled with clang, loaded at runtime by aya. These capture raw kernel data — all parsing and filtering happens in Rust userspace.

| Program | Tracepoints | Detects |
|---------|------------|---------|
| `network_monitor.c` | `inet_sock_set_state`, `tcp_receive_reset`, `tcp_retransmit_skb` | Connection failures, RST storms, retransmit spikes, RTT degradation |
| `container_monitor.c` | `sched_process_exit`, `oom/mark_victim` | OOM kills, abnormal container exits |
| `storage_monitor.c` | `block_rq_issue`, `block_rq_complete` | I/O errors, latency spikes |
| `node_pmc_monitor.c` | perf_event counters | CPU IPC degradation, memory stalls |

Shared headers in `ebpf/headers/`: `vmlinux_minimal.h`, `conn_tracking.h`, `metrics.h`, `tcp.h`.

### Event flow

```
eBPF (C) → ring buffer → parse (#[repr(C)] structs) → filter (anomaly?) → enrich (K8s pod context) → Occurrence → Sink
```

### Sinks (pluggable output)

Implement the `Sink` trait from `tapio-common`. Planned: `StdoutSink`, `FileSink` (`.tapio/`), `PolkuSink` (gRPC to POLKU), `GrafanaSink` (OTLP).

### FALSE Protocol

TAPIO emits Occurrences in the `kernel.*` type namespace. It fills factual fields (error code, message, data) but NOT reasoning fields. AI agents and AHTI do the reasoning.

### MCP server

Exposes kernel context to AI agents via stdio transport. Tools return data, not analysis. An AI agent asks TAPIO "what's happening on this node?" and gets structured kernel facts.

## Rules

- **No `.unwrap()` in production code** — use `?` or proper error handling
- **No `println!`** — use `tracing::{info, warn, error, debug}`
- **No dead code, no stubs, no TODOs** — complete or don't commit
- **`#[repr(C)]` structs must match C layouts exactly** — a mismatch silently corrupts data
- **TAPIO provides context, not reasoning** — facts in Occurrences, no `possible_causes` or `suggested_fix` fields
- **aya is Linux-only** — use `cfg(target_os = "linux")` for eBPF code, keep tapio-common and tapio-cli platform-independent
- **Lean** — every dependency must justify its existence. Target: <8MB release binary, <10MB RSS

## Observability

- **Logging**: `tracing` crate, env-filtered via `RUST_LOG`
- **Metrics**: `prometheus` crate, exposed on `:9090` via axum (`GET /metrics`, `GET /health`)
- **Metric prefix**: `tapio_` (e.g., `tapio_anomalies_total`, `tapio_events_processed_total`)
