# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

Tapio is an opinionated eBPF observer for Linux and Kubernetes systems. It watches a selected set of kernel-level signals (OOM kills, connection failures, I/O errors, CPU stalls), filters noise close to the node, enriches anomalies with Kubernetes context when available, and emits structured anomaly events to local or external sinks (stdout, file, HTTP, OTLP).

Tapio is opinionated by design: it does not forward every kernel event. It decides what is worth crossing the kernel/userspace boundary, what counts as a named anomaly, and what to preserve as factual evidence. Reasoning, correlation, storage, and remediation are explicitly left to downstream systems. Tapio emits evidence, not exhaust.

Tapio runs standalone — `stdout` and `file` sinks need no external service. It is not a generic eBPF framework, not a datastore, and not an observability platform. In JSON output, events are FALSE Protocol-compatible occurrence documents; the protocol is a supported output format, not a dependency.

## Commands

```bash
cargo check --workspace                        # type-check all crates
cargo check -p tapio-common -p tapio-cli -p tapio-profile  # check platform-independent crates (works on macOS)
cargo test --workspace                         # run all tests
cargo test -p tapio-common                     # test single crate
cargo test -p tapio-profile                    # test Evidence Profile validation/compilation
cargo test -p tapio-common -- test_name        # run single test
cargo clippy --workspace --all-targets -- -D warnings   # lint
cargo fmt --check                              # format check
cargo build --release -p tapio-agent           # Linux-only agent (~8MB, LTO + strip + opt-level=z + panic=abort)
cargo build --release -p tapio-cli             # CLI — builds on any platform (no eBPF dependency)

# CI (via sykli — requires cargo +nightly -Zscript)
sykli                                          # run full pipeline: fmt → clippy → test → build
```

Pre-commit hook runs `cargo fmt --check` and `cargo clippy --workspace --all-targets -- -D warnings`. Fix both before committing.

Rust edition 2024, MSRV 1.85. tapio-agent only compiles on Linux (aya dependency).

## Architecture

### Workspace crates

- **tapio-common**: Shared types. `#[repr(C)]` eBPF event structs mirroring the C programs, `kernel.*` event type hierarchy, FALSE Protocol `Occurrence` builder, `Sink` trait.
- **tapio-profile**: Pure Evidence Profile validation and compilation. Takes already-deserialized operator documents, returns `ValidatedProfile` or structured `ProfileError`, and compiles only validated profiles into `tapio-wire` compiled config.
- **tapio-agent**: DaemonSet binary. Loads eBPF C programs via aya, reads ring buffers, parses events, filters anomalies, and emits events to sinks. Linux-only (aya requires kernel). Internal modules: `observer/` (four observer submodules), `sink/` (`StdoutSink`, `FileSink`, `HttpSink`, `ControllerSink`, optional `OtlpSink` behind `--features otlp`), `config.rs` (TOML loader — handles tunable knobs only: `thresholds`, `metrics`, `otlp`. Operational paths like sinks/ebpf-dir/data-dir are CLI-only — disjoint scopes, not overlapping), `metrics.rs` (Prometheus registry, non-global — passed via `Arc` to observers and sinks).
- **tapio-controller**: Cluster coordination binary. Owns the HTTP server side of `tapio-wire/v1`, in-memory agent registry, heartbeat state, and future Kubernetes-aware coordination.
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

Use `-D__TARGET_ARCH_arm64` instead of `-D__TARGET_ARCH_x86` for arm64 nodes. All four `.o` files are required at runtime. The BPF/userspace boundary uses CO-RE (`preserve_access_index` + `bpf_core_read`) for kernel struct field access across versions; tracepoint argument structs rely on stable kernel ABI layouts.

Requires kernel 5.8+ (with BTF) and capabilities: `CAP_BPF`, `CAP_PERFMON`, `CAP_NET_ADMIN`. The agent runs as a privileged DaemonSet.

### Event flow

```
eBPF (C) → ring buffer → parse (#[repr(C)] structs) → filter (anomaly?) → Occurrence → Sink
```

### Sinks (pluggable output)

Implement the `Sink` trait from `tapio-common/src/sink.rs` (synchronous — async wrappers belong in the agent). Current: `StdoutSink` (JSON lines), `FileSink` (one `.json` per event in `.tapio/`), `HttpSink` (batched JSON POST with exponential backoff), `ControllerSink` (bounded queue and batched `tapio-wire/v1` event POSTs to the controller), optional `OtlpSink` behind `--features otlp` (OTLP/HTTP logs with gzip, maps events to LogRecords) in `tapio-agent/src/sink/`, plus `MultiSink` (fan-out, logs errors, doesn't short-circuit) defined in `tapio-agent/src/main.rs`. `stdout`/`file` are the standalone default; `http`/`otlp` are optional integrations for forwarding to a collector.

Network sink guarantees and non-goals:
- `OtlpSink` is plaintext `http://` only. `https://` endpoints are rejected before any connection opens or `Authorization` header can be written. Terminate TLS at a collector, sidecar, node-local proxy, or trusted boundary.
- `HttpSink` and `OtlpSink` return `SinkError` when an export fails after a batch is dropped. `MultiSink` records those as `tapio_sink_writes_total{result="err"}` and still attempts later sinks.
- Tapio does not implement a full HTTP/TLS client in the minimal sinks.

### FALSE Protocol

Tapio emits structured anomaly events in the `kernel.*` type namespace (FALSE Protocol-compatible occurrence JSON). It fills factual fields (error code, message, data) but NOT reasoning fields. Correlation, explanation, and remediation are downstream concerns.

### MCP server

Exposes kernel context to AI agents via stdio JSON-RPC 2.0 transport (`tapio mcp`). Three tools: `tapio_recent_anomalies` (filtered by minutes/observer/severity), `tapio_node_health` (health status + anomaly counts), `tapio_watch_stream` (snapshot of recent events). Tools return data, not analysis.

## Rules

- **No `.unwrap()` in production code** — use `?` or proper error handling
- **No `println!`** — use `tracing::{info, warn, error, debug}`
- **No dead code, no stubs, no TODOs** — complete or don't commit
- **`#[repr(C)]` structs must match C layouts exactly** — a mismatch silently corrupts data. Every struct in `ebpf.rs` has a `size_of` assertion test. Use `std::ptr::read_unaligned` for packed structs to avoid UB
- **eBPF event structs must zero padding before filling** — `__builtin_memset(evt, 0, sizeof(*evt))` immediately after `bpf_ringbuf_reserve()`. Padding bytes between fields otherwise contain raw kernel-stack contents (an info-leak / CVE-class bug). See `storage_monitor.c:174` for the canonical pattern
- **Agent → eBPF config is not CO-RE** — CO-RE relocates kernel struct accesses; Tapio runtime config is a private fixed-layout ABI between `tapio-agent`, a BPF map value, and Tapio eBPF programs. Keep it versioned, bounded, `#[repr(C)]`/C-layout asserted, and primitive-only. See `docs/agent-kernel-config-abi.md`.
- **No profile logic in the agent/kernel boundary** — the agent writes primitive compiled config to `tapio_config`; eBPF reads flags and thresholds. No YAML, Vartio concepts, Kubernetes labels, rules, DSLs, profile evaluation, strings, pointers, heap data, or arbitrary expressions belong in eBPF runtime config.
- **Zero config is inert** — all-zero `tapio_config` is the cold-start state and must emit nothing: `abi_version = 0` fails the guard, `flags = 0` disables observers, and no flag bit may ever mean "set to disable".
- **Runtime profile values use maps, not `.rodata`** — observer flags, thresholds, windows, bounded lists, sample rates, and config generation must be map-updated so profile rollout does not require eBPF reload. `.rodata` is for build-time/structural constants only.
- **Tapio provides context, not reasoning** — facts in events; never fill `possible_causes`, `suggested_fix`, or the reasoning block
- **Missing beats wrong** — if kernel tracepoints cannot uniquely correlate an event, drop and count it instead of emitting misleading evidence. Storage ambiguous inflight I/O uses `tapio_correlation_drops_total{observer="storage",reason="ambiguous_inflight_io"}`.
- **Opinionated, not generic** — Tapio emits named anomalies, not arbitrary kernel events; keep the observer/anomaly model selective
- **aya is Linux-only** — use `cfg(target_os = "linux")` for eBPF code, keep tapio-common and tapio-cli platform-independent
- **Agent/controller dependency boundary** — tapio-controller dependencies must never enter tapio-agent through workspace defaults. `tapio-agent` must not depend on `kube`, `k8s-openapi`, `axum`, `hyper`, `tonic`, or `reqwest`; `scripts/check-agent-deps.sh` enforces this.
- **Lean** — every dependency must justify its existence. Target: <8MB release binary, <10MB RSS
- **Lean gate** — run `scripts/verify-lean.sh` before release-worthy changes. It checks fmt, clippy, tests, release binary budgets, dependency tree output + boundaries, eBPF object budgets, eBPF map budgets, and eBPF compilation when clang/libbpf headers are available. The agent uses a **two-level binary budget**: hard `AGENT_MAX_BYTES` (default `1500000`, fails) and target `AGENT_TARGET_BYTES` (default `1320000`, warns); `CLI_MAX_BYTES` (default `900000`) is a hard limit. The hard budget is the line CI protects; the target is the next ratchet point and currently includes the measured controller heartbeat reporting path plus the bounded controller event sink and registration client. Override budgets only when the increase is intentional and documented (never to hide a regression); ratchet `AGENT_TARGET_BYTES` down before `AGENT_MAX_BYTES`. eBPF budget increases must be explicit in `scripts/verify-lean.sh`. Reliability knobs: `TAPIO_LEAN_ALLOW_DEGRADED=1` accepts a PARTIAL run when a host limitation (e.g. no loopback bind) forces a required test to skip; `TAPIO_LEAN_REQUIRE_NET=1` forces network tests to run where loopback is available. See `docs/architecture.md` "Performance and Size Model".
- **Runtime smoke** — run `scripts/smoke-ebpf-network.sh` on Linux/Lima for kernel behavior changes. It loads the real network observer, triggers a closed-port TCP connect, and checks that userspace emits a network occurrence with the exact destination port.

## Testing patterns

Each observer module (`network.rs`, `container.rs`, `storage.rs`, `node_pmc.rs`) tests `classify()` and `build_occurrence()` independently of the eBPF `run()` loop. Test helpers construct events via `unsafe { std::mem::zeroed::<T>() }` then set relevant fields. Occurrence tests call `.validate()` to confirm correctness and test JSON serialization round-trips.

## Agent CLI flags

The `tapio-agent` binary accepts:
- `--config <path>` — TOML config file (default: `/etc/tapio/tapio.toml`)
- `--sink <name>` — output sink (`stdout`, `file`, `http`, `controller`; `otlp` when built with `--features otlp`), repeatable for multi-sink
- `--ebpf-dir <path>` — directory with compiled `.o` files (default: `/opt/tapio/ebpf`)
- `--data-dir <path>` — directory for file sink output (default: `.tapio/occurrences`)
- `--http-endpoint <url>` — endpoint for the `http` sink (default: `http://localhost:8765`)
- `--controller-endpoint <url>` — controller base URL for compiled config polling (`http://` only; absent means standalone TOML mode)
- `--config-poll-interval <seconds>` — controller config polling interval, minimum 5 seconds (default: `30`)
- `--heartbeat-interval <seconds>` — controller heartbeat interval, minimum 5 seconds (default: `30`)

## Environment variables

- **`RUST_LOG`**: Controls log verbosity (e.g. `RUST_LOG=info` or `RUST_LOG=tapio_agent=debug`).
- **`TAPIO_DATA_DIR`**: Override the CLI's default data directory (default: `.tapio/occurrences`).

## Observability

- **Logging**: `tracing` crate, env-filtered via `RUST_LOG`
- **Metrics**: `prometheus` crate on configurable port (default `:9090`) via a tiny local HTTP handler. Enable with `[metrics] enabled = true` in config. **Binds to `127.0.0.1` by default** — set `bind_address = "0.0.0.0"` to expose to the node network (e.g. for cluster Prometheus scrape). Families: `tapio_events_total`, `tapio_anomalies_total`, `tapio_lost_events_total`, `tapio_malformed_events_total`, `tapio_correlation_drops_total`, `tapio_drain_cap_total`, `tapio_sink_writes_total`, `tapio_sink_drops_total`, `tapio_controller_send_failures_total`, `tapio_config_fetch_total`
- **Metric prefix**: `tapio_`
- **Controller config metric**: `tapio_config_fetch_total{result="applied|not_modified|error|rejected"}` counts controller-mode config poll outcomes.
- **eBPF-side metrics**: the shared per-CPU `tapio_metrics` map currently exports only `METRIC_LOST_EVENTS` to userspace. Do not add counters to `metrics.h` unless userspace reads and exposes them or the counter is otherwise consumed.
