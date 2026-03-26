<p align="center">
  <br>
  <code>T A P I O</code>
  <br>
  <br>
  eBPF kernel observer for Kubernetes. Edge-filtered. AI-native.
  <br>
  <br>
  <img src="https://img.shields.io/badge/rust-2024%20edition-f74c00" alt="Rust">
  <img src="https://img.shields.io/badge/ebpf-kernel%205.8%2B-orange" alt="eBPF">
  <img src="https://img.shields.io/badge/license-Apache%202.0-blue" alt="License">
</p>

---

Kubernetes tells you what happened. Tapio tells you what *actually* happened.

```
$ tapio watch

14:23:01 CRITICAL  kernel.container.oom_kill
         pod default/checkout-api on worker-3
         memory: 512Mi / 512Mi  pid: 1234  signal: 9
         3rd OOM kill in 10 minutes

14:23:01 ERROR     kernel.network.retransmit_spike
         10.0.1.5:8080 → 10.0.2.3:5432  retransmit: 23%
         baseline RTT: 1.2ms  current: 89ms
         47 RSTs in last 60s

14:23:03 WARNING   kernel.storage.latency_spike
         device sda  operation: write  latency: 340ms
         pod default/postgres-0  cgroup: kubepods/burstable/pod-xyz
```

These aren't log lines. They're kernel tracepoint captures — the exact data structures the kernel produced when it killed your container, retransmitted your packets, or stalled your disk. No sampling, no aggregation, no interpretation.

> *Tapio* — Finnish spirit of the forest. The founding tool of [False Systems](https://github.com/false-systems).

---

## The Problem

Every observability tool tells you what Kubernetes *reported*. None of them tell you what the kernel *did*.

| What you're told | What actually happened |
|-----------------|----------------------|
| Pod restarted | OOM killer fired at PID 1234, 512Mi used of 512Mi limit, signal 9 |
| Connection failed | TCP SYN timed out after 3 retransmits, RST received from 10.0.2.3 |
| High latency | Block I/O on sda took 340ms, device error 0x05 (EIO) |
| Node pressure | CPU IPC dropped to 0.4 (from baseline 1.8), 67% cycles stalled on memory |

The kernel knows everything. It just doesn't tell anyone. Tapio listens.

---

## How It Works

Four eBPF programs attach to kernel tracepoints. Raw events flow through a ring buffer to Rust userspace, where anomaly detection filters millions of events per second down to the ~1% that matter. Each survivor gets enriched with Kubernetes context and emitted as a FALSE Protocol occurrence.

```
Kernel tracepoints                 Rust userspace                  Sinks
──────────────────                 ──────────────                  ─────

inet_sock_set_state ──┐
tcp_receive_reset  ───┤            parse
tcp_retransmit_skb ───┼── ring ──► filter (anomaly?) ──► enrich ──► stdout
                      │  buffer   only ~1% survives     (kube-rs   POLKU → AHTI
sched_process_exit ───┤                                  pod ctx)  Grafana
oom/mark_victim    ───┤                                            .tapio/
                      │
block_rq_issue     ───┤
block_rq_complete  ───┤
                      │
perf_event counters ──┘
```

eBPF captures. Rust parses. The kernel provides ground truth — Tapio makes it accessible.

---

## What the Kernel Sees

| Observer | Tracepoints | What it catches |
|----------|------------|-----------------|
| **Network** | `inet_sock_set_state` · `tcp_receive_reset` · `tcp_retransmit_skb` | Connection refused, SYN timeout, retransmit spikes, RTT degradation, RST storms |
| **Container** | `sched_process_exit` · `oom/mark_victim` | OOM kills with exact memory numbers, abnormal exits with signal and code |
| **Storage** | `block_rq_issue` · `block_rq_complete` | I/O errors by device, latency spikes with nanosecond precision |
| **Node** | perf_event counters | CPU IPC degradation, memory stall cycles |

Each anomaly carries the raw kernel struct. Not a derived metric — the actual bytes the kernel wrote.

---

## Edge Filtering

Tapio doesn't forward everything. Each observer has anomaly detectors tuned to what matters:

| Event type | Fires when |
|------------|-----------|
| `kernel.network.retransmit_spike` | Retransmit rate >5% over >100 packets |
| `kernel.network.rtt_degradation` | RTT exceeds learned baseline by threshold |
| `kernel.network.connection_refused` | RST during SYN_SENT |
| `kernel.container.oom_kill` | `oom/mark_victim` tracepoint fires |
| `kernel.container.abnormal_exit` | Non-zero exit code |
| `kernel.storage.io_error` | Block I/O completes with error |
| `kernel.storage.latency_spike` | I/O latency exceeds severity threshold |
| `kernel.node.ipc_degradation` | Instructions-per-cycle below baseline |

Normal traffic stays in the kernel. Only anomalies cross to userspace.

---

## Context, Not Conclusions

Tapio reports facts. AI agents and [AHTI](https://github.com/false-systems/ahti) do the thinking.

```json
{
  "source": "tapio",
  "type": "kernel.container.oom_kill",
  "severity": "critical",
  "outcome": "failure",
  "error": {
    "code": "OOM_KILL",
    "message": "Container killed by OOM killer"
  },
  "context": {
    "cluster": "prod-us-east",
    "node": "worker-3",
    "namespace": "default",
    "entities": [
      { "kind": "pod", "id": "default/checkout-api-7f8b9" },
      { "kind": "deployment", "id": "default/checkout-api" },
      { "kind": "node", "id": "worker-3" }
    ]
  },
  "data": {
    "memory_usage_bytes": 536870912,
    "memory_limit_bytes": 536870912,
    "pid": 1234,
    "exit_code": 137,
    "signal": 9,
    "cgroup_path": "/kubepods/burstable/pod-xyz/abc123"
  }
}
```

No `possible_causes`. No `suggested_fix`. No hallucinated reasoning. The kernel saw it — here's what it saw.

---

## CLI

```bash
tapio status                    # what's running, event rates
tapio watch                     # live anomaly stream
tapio watch --network           # filter by observer
tapio recent                    # last 20 anomalies
tapio recent --since 5m         # time window
tapio health                    # node health across all observers
tapio health network            # drill into network
tapio explain <pod>             # all kernel events for a pod
tapio mcp                       # MCP server for AI agents
```

`tapio` with no args shows status. `--json` on any command for machine output.

---

## MCP

AI agents query kernel context directly. Six tools, all returning data — never analysis:

| Tool | What it returns |
|------|----------------|
| `tapio_status` | Observer states, attached eBPF programs, event rates |
| `tapio_node_health` | Per-category anomaly counts and rates |
| `tapio_recent_anomalies` | Last N occurrences, filterable by type and namespace |
| `tapio_pod_context` | Every kernel observation for a specific pod |
| `tapio_connection_context` | Network path: RTT, retransmits, RSTs between two endpoints |
| `tapio_node_context` | Full node snapshot — everything Tapio knows, one call |

An AI debugging a production incident can ask TAPIO what the kernel saw, ask [AHTI](https://github.com/false-systems/ahti) for the causal chain, and ask [SYKLI](https://github.com/false-systems/sykli) if there was a recent deploy. Each tool provides its piece. The AI reasons across all of them.

---

## Sinks

Pluggable output. Ship to one or many:

```bash
tapio-agent --sink=stdout                  # JSON lines, pipe anywhere
tapio-agent --sink=file                    # .tapio/occurrences/ for AI/MCP
tapio-agent --sink=polku                   # gRPC → POLKU → AHTI
tapio-agent --sink=grafana                 # OTLP → Grafana Cloud
tapio-agent --sink=polku --sink=file       # ship + persist locally
```

---

## Building

```
cargo build --release -p tapio-agent      # DaemonSet binary
cargo build --release -p tapio-cli        # CLI + MCP server
cargo test --workspace                    # all tests
cargo clippy --workspace --all-targets    # lint
```

Release profile: LTO, single codegen unit, stripped, `opt-level=z`, `panic=abort`. Target: ~8MB binary, ~8MB RSS, <100ms to first event.

The agent requires Linux with eBPF (kernel 5.8+). The CLI runs anywhere.

---

## Architecture

```
tapio/
  tapio-common/     #[repr(C)] eBPF structs, kernel.* event types, FALSE Protocol, Sink trait
  tapio-agent/      DaemonSet binary — eBPF → ring buffer → parse → filter → enrich → emit
  tapio-cli/        CLI commands + MCP server
  ebpf/             4 C programs + headers — the kernel-side truth
  eval/oracle/      ground-truth test binary
  deploy/           Helm chart, DaemonSet manifests
```

---

## Trade-offs

**eBPF requires privileges.** The agent needs `CAP_BPF` + `CAP_PERFMON` + `CAP_NET_ADMIN`. It runs as a privileged DaemonSet. This is the cost of kernel visibility.

**IPv6 connection tracking is incomplete.** The network observer's RTT baseline and retransmit tracking currently uses IPv4-only map keys. IPv6 events are captured but lack connection-level intelligence. This is known and planned.

**C eBPF programs, not Rust.** The four kernel programs are written in C, loaded via [aya](https://github.com/aya-rs/aya). The Rust userspace defines matching `#[repr(C)]` structs manually. Incremental migration to aya-ebpf (pure Rust kernel programs) is possible but not started.

**Edge filtering is opinionated.** Anomaly thresholds are hardcoded. A retransmit rate of 4.9% is silent; 5.1% fires. This is the right trade-off for a proof of concept — configurable thresholds add complexity without clarity.

---

## Part of False Systems

| Tool | Finnish | Role |
|------|---------|------|
| **TAPIO** | spirit of the forest | eBPF kernel observer |
| [POLKU](https://github.com/false-systems/polku) | path | Programmable protocol hub |
| [AHTI](https://github.com/false-systems/ahti) | spirit of the sea | Infrastructure knowledge engine |
| [RAUTA](https://github.com/false-systems/rauta) | iron | AI-native API gateway |
| [RAUHA](https://github.com/false-systems/rauha) | peace | Isolation-first container runtime |
| [SYKLI](https://github.com/false-systems/sykli) | cycle | AI-native CI engine |
| [NOPEA](https://github.com/false-systems/nopea) | fast | Deployment with memory |

---

Apache 2.0
