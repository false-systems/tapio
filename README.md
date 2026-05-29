<p align="center">
  <br>
  <code>T A P I O</code>
  <br>
  <br>
  Opinionated eBPF observer for Linux and Kubernetes
  <br>
  <br>
  <img src="https://img.shields.io/badge/rust-2024%20edition-f74c00" alt="Rust">
  <img src="https://img.shields.io/badge/ebpf-kernel%205.8%2B-orange" alt="eBPF">
  <img src="https://img.shields.io/badge/license-Apache%202.0-blue" alt="License">
</p>

---

Tapio is an opinionated eBPF observer for Linux and Kubernetes systems.

It watches a selected set of kernel-level signals, filters noise close to the node, enriches anomalies with Kubernetes context when available, and emits structured operational events.

> The kernel already knows when things go wrong. Tapio watches closely enough to notice.

Tapio emits evidence, not exhaust.

---

## Why Tapio

Most observability stacks see the application. The kernel sees what actually happened: the OOM kill, the refused connection, the I/O error, the CPU stall. That signal is already there — Tapio's job is to notice the failures that matter and turn them into structured events you can act on.

Tapio does not try to replace Prometheus, Grafana, OpenTelemetry, or your observability stack. It provides something lower-level and more direct: structured kernel evidence.

Tapio does not guess at root cause. It observes kernel-level anomalies and preserves the evidence. Correlation, explanation, storage, and remediation belong downstream.

---

## Opinionated by design

Tapio is opinionated by design.

It is not a generic eBPF framework and it does not try to forward every kernel event into userspace.

Tapio watches a selected set of kernel signals, classifies specific failure patterns, and emits structured anomaly events. The goal is not maximum event volume. The goal is useful kernel evidence.

That means Tapio makes product decisions:

- some events are ignored,
- some filtering happens inside BPF,
- some classification happens in Rust,
- only named anomalies are emitted,
- reasoning and remediation are left downstream.

This keeps Tapio close to the kernel without turning it into another noisy telemetry pipe.

---

## What Tapio observes

Tapio is organized around four observers, each watching a specific slice of kernel activity and emitting named anomalies.

| Observer | Kernel sources | Anomalies |
|----------|----------------|-----------|
| Network | `inet_sock_set_state`, `tcp_receive_reset`, `tcp_retransmit_skb` | `kernel.network.connection_refused`, `kernel.network.connection_timeout`, `kernel.network.retransmit_spike`, `kernel.network.rtt_degradation`, `kernel.network.rst_storm` |
| Container | `sched_process_exit`, `oom/mark_victim` | `kernel.container.oom_kill`, `kernel.container.abnormal_exit` |
| Storage | `block_rq_issue`, `block_rq_complete` | `kernel.storage.io_error`, `kernel.storage.latency_spike` |
| Node PMC | `perf_event` counters: cycles, instructions, stalls | `kernel.node.cpu_stall`, `kernel.node.memory_pressure`, `kernel.node.ipc_degradation` |

Anomaly types are stable product concepts, not arbitrary event labels. They follow a `kernel.<observer>.<anomaly>` naming convention.

---

## How it works

```
Kernel signals
  ↓
eBPF programs
  ↓
Ring buffers
  ↓
Rust userspace parser
  ↓
Edge anomaly filtering
  ↓
Kubernetes enrichment
  ↓
Sinks
```

```
Kernel signals                  Rust userspace                    Outputs
──────────────                  ──────────────                    ───────
inet_sock_set_state ──┐
tcp_receive_reset  ───┤
tcp_retransmit_skb ───┼──► ring buffer ──► parse ──► filter ──► enrich ──► stdout
                      │                              anomaly?   kube-rs     file (.tapio/)
sched_process_exit ───┤                                                    HTTP
oom/mark_victim    ───┤                                                    OTLP
                      │
block_rq_issue     ───┤
block_rq_complete  ───┤
                      │
perf_event counters ──┘
```

The BPF/Rust boundary is defined by packed C structs mirrored in `tapio-common/src/ebpf.rs`. Size assertions enforce layout agreement at compile time. CO-RE (`preserve_access_index` + `bpf_core_read`) handles kernel struct field access across versions; tracepoint argument structs use stable kernel ABI layouts.

---

## Filtering model

Tapio filters at two levels, choosing the cheapest place to drop noise.

1. **BPF-side filtering** — for cheap, obvious decisions made before an event ever crosses into userspace.
2. **Rust-side anomaly classification** — for stateful or richer detection that needs context or thresholds.

| Observer | Where it filters | Behavior |
|----------|------------------|----------|
| Storage | BPF | Only I/O errors and latency spikes cross into userspace. |
| Container | BPF | Only abnormal exits cross: non-zero exit code, terminating signal, or OOM kill. |
| Network | Rust | BPF emits state transitions and network signals; Rust classifies retransmit spikes, RTT degradation, reset storms, connection failures, and timeouts. |
| Node PMC | Rust | BPF samples performance counters; Rust detects IPC degradation, CPU stalls, and memory pressure. |

Thresholds for the Rust-side and BPF-side detectors (RTT spike ratio, I/O latency, PMC stall percentages, IPC) are tunable via the TOML config file — see [Agent](#agent).

---

## Output format

Tapio emits structured anomaly events. In JSON output, these are represented as occurrence documents (a FALSE Protocol-compatible schema).

Tapio fills factual fields only: timestamp, source, observer/type, severity, outcome, error code and message, kernel data, and Kubernetes context when available. It does **not** fill reasoning fields — root cause, causal chains, explanations, and remediation are left for downstream systems.

```json
{
  "id": "01JA1B2C3D4E5F6G7H8J9K0L1M",
  "timestamp": "2026-04-03T14:23:01.042Z",
  "source": "tapio",
  "type": "kernel.container.oom_kill",
  "protocol_version": "1.0",
  "severity": "critical",
  "outcome": "failure",
  "error": {
    "code": "OOM_KILL",
    "message": "OOM kill pid=1234 (usage=512MB, limit=0MB)"
  },
  "context": {
    "node": "worker-3",
    "namespace": "default",
    "entities": [
      { "kind": "pod", "id": "default/checkout-api-7f8b9", "name": "checkout-api-7f8b9" },
      { "kind": "deployment", "id": "default/checkout-api", "name": "checkout-api" }
    ]
  },
  "data": {
    "pid": 1234,
    "tid": 1234,
    "exit_code": 137,
    "signal": 9,
    "memory_usage_bytes": 536870912,
    "memory_limit_bytes": 0,
    "cgroup_id": 8429,
    "timestamp_ns": 1743691381042000000
  }
}
```

`memory_limit_bytes` may be `0` for OOM kills because the BPF tracepoint does not expose the cgroup limit. `context` is populated when Kubernetes enrichment is enabled and `NODE_NAME` is set (see [Kubernetes](#kubernetes)).

---

## CLI

The `tapio` CLI reads events from a local data directory and is fully decoupled from the running agent process — it inspects what has been written, so it works the same whether the agent is running or not.

```bash
tapio status                    # observer status, event counts
tapio watch                     # live anomaly stream
tapio watch --network           # filter by observer
tapio watch --json              # machine-readable output
tapio recent                    # last 20 anomalies
tapio recent --since 5m         # time window
tapio health                    # node health across all observers
tapio health network            # drill into one observer
```

It reads from `.tapio/occurrences/*.json` by default. Override with `--data-dir` or the `TAPIO_DATA_DIR` environment variable.

The CLI also exposes a read-only MCP server (`tapio mcp`, stdio JSON-RPC 2.0) so AI agents can query recent anomalies and node health from the same local event store.

---

## Agent

The agent (`tapio-agent`) is the process that loads the eBPF programs, reads the ring buffers, filters and enriches events, and emits them to sinks.

```bash
tapio-agent --sink=stdout                  # JSON lines to stdout
tapio-agent --sink=file                    # .tapio/occurrences/*.json
tapio-agent --sink=stdout --sink=file      # multiple sinks (fan-out)
tapio-agent --ebpf-dir /opt/tapio/ebpf     # compiled .o location
```

Flags:

- `--config <path>` — TOML config file (default `/etc/tapio/tapio.toml`); tunes thresholds, metrics, and the OTLP sink.
- `--sink <name>` — output sink (`stdout`, `file`, `http`, `otlp`), repeatable.
- `--ebpf-dir <path>` — directory of compiled `.o` files (default `/opt/tapio/ebpf`).
- `--data-dir <path>` — directory for the file sink (default `.tapio/occurrences`).
- `--http-endpoint <url>` — endpoint for the `http` sink.

The agent requires the capabilities `CAP_BPF`, `CAP_PERFMON`, and `CAP_NET_ADMIN`.

Tapio runs without any external service: `--sink=stdout` and `--sink=file` work standalone, with no Kubernetes and no network destination.

---

## Kubernetes

Tapio runs as a privileged DaemonSet, one agent per node.

Kubernetes context is populated when enrichment is enabled and `NODE_NAME` is set, usually through the Kubernetes Downward API. If `NODE_NAME` is unset or the API is unreachable, enrichment is skipped and events are emitted without pod context — local mode keeps working.

Kubernetes is optional. Tapio observes the Linux kernel; the Kubernetes layer only adds pod, namespace, and workload context to the evidence it already has.

---

## Sinks

Tapio emits structured anomaly events to local files, stdout, HTTP endpoints, or OTLP-compatible systems.

| Sink | Output |
|------|--------|
| `StdoutSink` | JSON lines to stdout |
| `FileSink` | one `.json` document per event under the data directory |
| `HttpSink` | batched JSON `POST` to an HTTP ingest endpoint |
| `OtlpSink` | OTLP/HTTP logs export (protobuf + gzip) to any OTLP-compatible collector |
| `MultiSink` | fan-out wrapper; sends to all configured sinks, logs errors without short-circuiting |

Sinks implement the `tapio_common::sink::Sink` trait (sync `send`/`flush`/`name`). The local sinks (`stdout`, `file`) are the default path and need nothing external. The network sinks (`http`, `otlp`) are optional integrations for forwarding evidence to a collector or ingest service of your choice.

---

## Building

```bash
cargo build --release -p tapio-agent    # ~8MB, LTO + strip + opt-level=z + panic=abort
cargo build --release -p tapio-cli      # runs anywhere (no eBPF dependency)
cargo test --workspace
cargo clippy --workspace --all-targets -- -D warnings
```

Rust edition 2024, MSRV 1.85. The agent requires Linux with kernel 5.8+ and BTF. The CLI runs on any platform — only the agent depends on eBPF.

---

## Building eBPF programs

There is no build script — the four eBPF C programs are compiled with clang and placed in `--ebpf-dir`. The agent loads the pre-compiled `.o` files at runtime. All four are required.

```bash
for prog in network_monitor container_monitor storage_monitor node_pmc_monitor; do
  clang -O2 -g -target bpf -D__TARGET_ARCH_x86 -I ebpf/headers \
    -c ebpf/${prog}.c -o /opt/tapio/ebpf/${prog}.o
done
```

Use `-D__TARGET_ARCH_arm64` instead of `-D__TARGET_ARCH_x86` for arm64 nodes.

---

## Architecture

```
tapio-common/     #[repr(C)] event structs, kernel.* anomaly types, occurrence schema, Sink trait
tapio-agent/      DaemonSet — eBPF load → ring buffer → parse → filter → enrich → emit
tapio-cli/        CLI commands + MCP server, reads .tapio/occurrences/
ebpf/             4 C programs + shared headers
```

- **tapio-common** is platform-independent: the shared `#[repr(C)]` event structs, the `kernel.*` anomaly type constants, the occurrence schema, and the `Sink` trait.
- **tapio-agent** is Linux-only (aya requires the kernel). It owns the observers, the K8s enricher, the sinks, and metrics.
- **tapio-cli** is platform-independent and reads the local event store; it never talks to the agent directly.

---

## What Tapio does not do

Tapio is intentionally selective. It does not:

- forward every kernel event into userspace (it is not a generic eBPF framework),
- store or index events long-term (it is not a datastore),
- correlate events across nodes or explain them (it is not an intelligence layer),
- replace Prometheus, Grafana, or OpenTelemetry (it is not a full observability platform),
- fill reasoning fields — root cause, causal chains, remediation (those belong downstream).

Tapio owns node-level observation. Everything else — storage, correlation, dashboards, explanation — can consume its evidence later.

---

Apache 2.0
