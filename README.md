<p align="center">
  <br>
  <code>T A P I O</code>
  <br>
  <br>
  eBPF kernel observer for Kubernetes
  <br>
  <br>
  <img src="https://img.shields.io/badge/rust-2024%20edition-f74c00" alt="Rust">
  <img src="https://img.shields.io/badge/ebpf-kernel%205.8%2B-orange" alt="eBPF">
  <img src="https://img.shields.io/badge/license-Apache%202.0-blue" alt="License">
</p>

---

Four eBPF programs attach to kernel tracepoints. Events flow through ring buffers to Rust userspace, where anomaly detection filters noise. Anomalies are enriched with Kubernetes pod context and emitted as [FALSE Protocol](https://github.com/false-systems) occurrences.

```
Kernel tracepoints              Rust userspace                   Sinks
────────────────                ──────────────                   ─────
inet_sock_set_state ──┐
tcp_receive_reset  ───┤         parse
tcp_retransmit_skb ───┼── ring  filter (anomaly?)  enrich ──►   stdout
                      │  buffer                    (kube-rs)    file (.tapio/)
sched_process_exit ───┤                                         POLKU → AHTI
oom/mark_victim    ───┤                                         Grafana (OTLP)
                      │
block_rq_issue     ───┤
block_rq_complete  ───┤
                      │
perf_event counters ──┘
```

> *Tapio* — Finnish spirit of the forest. Part of [False Systems](https://github.com/false-systems): TAPIO (eBPF edge) → POLKU (protocol hub) → AHTI (intelligence).

---

## Observers

| Observer | Tracepoints | Anomalies |
|----------|------------|-----------|
| Network | `inet_sock_set_state`, `tcp_receive_reset`, `tcp_retransmit_skb` | `kernel.network.connection_refused`, `connection_timeout`, `retransmit_spike`, `rtt_degradation`, `rst_storm` |
| Container | `sched_process_exit`, `oom/mark_victim` | `kernel.container.oom_kill`, `abnormal_exit` |
| Storage | `block_rq_issue`, `block_rq_complete` | `kernel.storage.io_error`, `latency_spike` |
| Node PMC | `perf_event` (cycles, instructions, stalls) | `kernel.node.cpu_stall`, `memory_pressure`, `ipc_degradation` |

Edge filtering happens at two levels. The storage observer filters in BPF — only I/O errors and latency spikes cross to userspace. The container observer filters in BPF for abnormal exits (non-zero exit code or signal). The network observer emits on state transitions and anomalies; Rust classifies retransmit spikes, RTT degradation, and connection failures. The PMC observer sends all samples; Rust detects IPC degradation and memory stalls.

---

## Output

Occurrences carry raw kernel data. TAPIO fills factual fields (error code, message, data) but not reasoning fields — those are for downstream consumers like [AHTI](https://github.com/false-systems/ahti).

```json
{
  "source": "tapio",
  "type": "kernel.container.oom_kill",
  "severity": "critical",
  "outcome": "failure",
  "error": {
    "code": "OOM_KILL",
    "message": "OOM kill pid=1234 (usage=512MB, limit=512MB)"
  },
  "context": {
    "node": "worker-3",
    "namespace": "default",
    "entities": [
      { "kind": "pod", "id": "default/checkout-api-7f8b9" }
    ]
  },
  "data": {
    "pid": 1234,
    "exit_code": 137,
    "signal": 9,
    "memory_usage_bytes": 536870912,
    "memory_limit_bytes": 536870912,
    "cgroup_id": 8429
  }
}
```

---

## CLI

```bash
tapio status                    # observer status, event rates
tapio watch                     # live anomaly stream
tapio watch --network           # filter by observer
tapio watch --json              # machine-readable output
tapio recent                    # last 20 anomalies
tapio recent --since 5m         # time window
tapio health                    # node health across all observers
tapio health network            # drill into one observer
```

The CLI reads from `.tapio/occurrences/*.json` — decoupled from the agent process. Override with `--data-dir` or `TAPIO_DATA_DIR`.

---

## Agent

```bash
tapio-agent --sink=stdout                  # JSON lines to stderr
tapio-agent --sink=file                    # .tapio/occurrences/*.json
tapio-agent --sink=stdout --sink=file      # both
tapio-agent --ebpf-dir /opt/tapio/ebpf    # compiled .o location
```

Requires `CAP_BPF` + `CAP_PERFMON` + `CAP_NET_ADMIN`. Runs as a privileged DaemonSet. Set `NODE_NAME` via the Downward API for K8s enrichment.

---

## Building

```bash
cargo build --release -p tapio-agent    # ~8MB, LTO + strip + opt-level=z + panic=abort
cargo build --release -p tapio-cli      # runs anywhere (no eBPF dependency)
cargo test --workspace                  # 61 tests
cargo clippy --workspace --all-targets -- -D warnings
```

eBPF programs are compiled separately:

```bash
clang -O2 -g -target bpf -D__TARGET_ARCH_x86 -I ebpf/headers \
  -c ebpf/network_monitor.c -o /opt/tapio/ebpf/network_monitor.o
```

Rust edition 2024, MSRV 1.85. The agent requires Linux with kernel 5.8+ and BTF. The CLI runs on any platform.

---

## Architecture

```
tapio-common/     #[repr(C)] event structs, kernel.* types, FALSE Protocol, Sink trait
tapio-agent/      DaemonSet — eBPF load → ring buffer → parse → filter → enrich → emit
tapio-cli/        CLI commands, reads .tapio/occurrences/
ebpf/             4 C programs + shared headers
```

The BPF/Rust boundary is defined by packed C structs mirrored in `tapio-common/src/ebpf.rs`. Size assertions enforce layout agreement at compile time. CO-RE (`preserve_access_index` + `bpf_core_read`) handles kernel struct field access across versions; tracepoint argument structs use stable kernel ABI layouts.

Sinks implement `tapio_common::sink::Sink` (sync `send`/`flush`/`name`). Current: `StdoutSink`, `FileSink`, `MultiSink`. Planned: `PolkuSink` (gRPC), `GrafanaSink` (OTLP).

---

## Known limitations

**IPv6 connection tracking is incomplete.** RTT baseline and retransmit tracking use IPv4-only map keys. IPv6 events are captured but lack per-connection intelligence.

**Anomaly thresholds are hardcoded.** Retransmit rate, RTT spike ratio, I/O latency cutoffs, IPC degradation — all fixed constants. Configurable thresholds are not yet implemented.

**MCP server is stubbed.** `tapio mcp` returns an error. Planned: stdio transport exposing kernel context to AI agents.

---

Apache 2.0
