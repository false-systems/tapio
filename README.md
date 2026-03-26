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

Your cluster is lying to you. Kubernetes says a pod restarted — not *why*. Metrics say memory went up — not that the OOM killer fired at PID 1234 with 512Mi used of 512Mi limit. Logs say a connection failed — not that TCP retransmits hit 23% between your frontend and database.

Tapio attaches eBPF tracepoints to the kernel and reports what actually happened. No sampling, no aggregation, no interpretation — just kernel truth, filtered to the anomalies that matter.

```
cargo build --release -p tapio-agent    # DaemonSet binary (~8MB)
cargo build --release -p tapio-cli      # CLI + MCP server
cargo test --workspace                  # all tests
```

> *Tapio* — Finnish spirit of the forest. The founding tool of [False Systems](https://github.com/false-systems).

---

## How it works

Four eBPF programs attach to kernel tracepoints. Each captures raw events into a ring buffer. Rust userspace reads, parses, and filters — only anomalies survive. Each anomaly becomes a FALSE Protocol occurrence enriched with Kubernetes pod context.

```
Kernel                           Userspace (Rust)                     Output
─────────────────────────────────────────────────────────────────────────────────

inet_sock_set_state ──┐
tcp_receive_reset  ───┤          ┌─────────────┐    ┌───────────┐
tcp_retransmit_skb ───┼── ring ──► parse + ─────►    │ enrich    │    → stdout
                      │  buffer  │ filter      │    │ (kube-rs  │    → .tapio/
sched_process_exit ───┤          │             │    │  pod ctx) │    → POLKU
oom/mark_victim    ───┤          │ ~1% pass    │    │           │    → Grafana
                      │          └─────────────┘    └───────────┘
block_rq_issue     ───┤           only anomalies     which pod?
block_rq_complete  ───┤                               which node?
                      │                               which deployment?
perf_event counters ──┘
```

Millions of kernel events per second. ~1% survive filtering. Each survivor gets full context.

---

## What the kernel sees

| Observer | Tracepoints | Anomalies |
|----------|------------|-----------|
| **Network** | `inet_sock_set_state` · `tcp_receive_reset` · `tcp_retransmit_skb` | Connection refused, SYN timeout, retransmit spike, RTT degradation, RST storm |
| **Container** | `sched_process_exit` · `oom/mark_victim` | OOM kills with memory numbers, abnormal exits with signal/code |
| **Storage** | `block_rq_issue` · `block_rq_complete` | I/O errors by device, latency spikes with exact duration |
| **Node** | perf_event counters (cycles, instructions, stalls) | CPU IPC degradation, memory stall pressure |

Every anomaly carries the raw kernel data. Not a metric, not a log line — the actual struct the kernel produced.

---

## Edge filtering

Tapio doesn't send everything. Each observer has hardcoded anomaly detectors:

| Anomaly | Detection logic |
|---------|----------------|
| `kernel.network.retransmit_spike` | Retransmit rate >5% AND >100 packets observed |
| `kernel.network.rtt_degradation` | Current RTT exceeds learned baseline by threshold |
| `kernel.network.connection_refused` | TCP RST received during SYN_SENT state |
| `kernel.container.oom_kill` | `oom/mark_victim` tracepoint fired |
| `kernel.container.abnormal_exit` | Non-zero exit code from `sched_process_exit` |
| `kernel.storage.io_error` | Block I/O completed with non-zero error code |
| `kernel.storage.latency_spike` | I/O latency exceeds severity threshold |
| `kernel.node.ipc_degradation` | Instructions-per-cycle drops below threshold |

Normal traffic is invisible. Only the anomalies reach userspace.

---

## Sinks

Pluggable output. Use one or many simultaneously.

```bash
tapio-agent --sink=stdout                  # JSON lines, pipe to anything
tapio-agent --sink=file                    # .tapio/occurrences/ for AI/MCP
tapio-agent --sink=polku                   # gRPC → POLKU → AHTI
tapio-agent --sink=grafana                 # OTLP → Grafana/Tempo/Loki
tapio-agent --sink=polku --sink=file       # production: ship + persist
```

All sinks implement one trait:

```rust
trait Sink: Send + Sync {
    async fn send(&self, occurrence: &Occurrence) -> Result<(), SinkError>;
    async fn flush(&self) -> Result<(), SinkError>;
    fn name(&self) -> &str;
}
```

---

## CLI

```bash
tapio status                    # observer health, event rates, uptime
tapio watch                     # live anomaly stream (kernel tail -f)
tapio watch --network           # filter by observer
tapio recent                    # last 20 anomalies, one-line summaries
tapio recent --since 5m         # time-filtered
tapio show <id>                 # full occurrence, pretty-printed
tapio health                    # node health: network, storage, memory, cpu
tapio health network            # detailed network stats
tapio explain <pod>             # all kernel events touching this pod
tapio context                   # export .tapio/context.json
tapio mcp                       # start MCP server (stdio)
```

`tapio` with no args shows status. `--json` on any command for machine output.

---

## AI native

Tapio provides context, not conclusions. The kernel sees — AI thinks.

### FALSE Protocol occurrences

Every anomaly is a structured occurrence with factual kernel data:

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
      { "kind": "pod", "id": "default/nginx-abc" },
      { "kind": "deployment", "id": "default/nginx" }
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

No `possible_causes`. No `suggested_fix`. No `reasoning`. Just what happened.

### MCP tools

Six tools for AI agents to query kernel context:

| Tool | Returns |
|------|---------|
| `tapio_status` | Observer states, eBPF programs attached, event rates |
| `tapio_node_health` | Per-category anomaly counts and rates |
| `tapio_recent_anomalies` | Last N occurrences, filterable by type/namespace |
| `tapio_pod_context` | All kernel observations for a specific pod |
| `tapio_connection_context` | Network path health between two endpoints |
| `tapio_node_context` | Full node snapshot — everything Tapio knows |

---

## Lean

| | Tapio | Typical Go agent | Commercial eBPF |
|---|---|---|---|
| Binary | ~8 MB | 30–40 MB | 50+ MB |
| Memory (idle) | ~8 MB | 30+ MB | 100+ MB |
| Startup to first event | <100 ms | ~500 ms | seconds |
| Docker image | `FROM scratch` | `FROM ubuntu` | hundreds of MB |
| Price | free | free | $$$/node/month |

Release builds: LTO, single codegen unit, stripped, `opt-level=z`, `panic=abort`.

---

## Project layout

```
tapio/
  tapio-common/     shared types — eBPF structs (#[repr(C)]), events, FALSE Protocol, sink trait
  tapio-agent/      DaemonSet binary — load eBPF, observe, filter, enrich, emit
  tapio-cli/        CLI + MCP server — query, watch, explain
  ebpf/             4 C programs + headers (the kernel truth)
  eval/oracle/      ground-truth test binary
  deploy/           Helm chart, DaemonSet manifests
```

---

## Part of False Systems

| Tool | Finnish | Role |
|------|---------|------|
| **TAPIO** | spirit of the forest | eBPF kernel observer |
| [**POLKU**](https://github.com/false-systems/polku) | path | Programmable protocol hub |
| [**AHTI**](https://github.com/false-systems/ahti) | spirit of the sea | Infrastructure knowledge engine |
| [**RAUTA**](https://github.com/false-systems/rauta) | iron | AI-native API gateway |
| [**RAUHA**](https://github.com/false-systems/rauha) | peace | Isolation-first container runtime |
| [**SYKLI**](https://github.com/false-systems/sykli) | cycle | AI-native CI engine |
| [**NOPEA**](https://github.com/false-systems/nopea) | fast | Deployment with memory |

---

Apache 2.0
