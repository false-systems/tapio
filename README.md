# Tapio

**Kernel eyes for Kubernetes.**

Your cluster is lying to you. Kubernetes API events tell you a pod restarted — not *why*. Metrics tell you memory went up — not that the OOM killer fired at PID 1234 with 512Mi used of 512Mi limit. Logs tell you a connection failed — not that TCP retransmits hit 23% on the path between your frontend and database.

Tapio sees what the kernel sees. eBPF tracepoints capture the ground truth — OOM kills, connection failures, I/O errors, CPU stalls — and emits it as structured FALSE Protocol occurrences that AI agents and humans can actually use.

> *Tapio* (Finnish) — spirit of the forest. The original tool that started [False Systems](https://github.com/false-systems).

## What it observes

| Observer | Kernel tracepoints | Anomalies detected |
|----------|-------------------|-------------------|
| **Network** | `inet_sock_set_state`, `tcp_receive_reset`, `tcp_retransmit_skb` | Connection refused, SYN timeout, retransmit spike, RTT degradation |
| **Container** | `sched_process_exit`, `oom/mark_victim` | OOM kills, abnormal exits |
| **Storage** | `block_rq_issue`, `block_rq_complete` | I/O errors, latency spikes |
| **Node** | perf_event counters | CPU IPC degradation, memory stalls |

## How it works

```
eBPF (kernel) ──→ ring buffer ──→ parse + filter ──→ enrich (K8s context) ──→ Occurrence ──→ sink
                                  (is this an anomaly?)  (which pod? node?)                    │
                                                                                    ┌──────────┤
                                                                                    ▼          ▼
                                                                                  stdout    POLKU → AHTI
                                                                                  file      Grafana
```

Tapio filters at the edge. Millions of kernel events per second become the ~1% that matter — the anomalies. Each one becomes a FALSE Protocol occurrence with full kernel context.

## Sinks

```bash
tapio-agent --sink=stdout          # JSON occurrences, pipe anywhere
tapio-agent --sink=file            # .tapio/occurrences/ (local, for AI/MCP)
tapio-agent --sink=polku           # gRPC stream → POLKU → AHTI
tapio-agent --sink=grafana         # OTLP → Grafana Cloud/Tempo/Loki
```

Multiple sinks at once: `--sink=polku --sink=file`

## CLI

```bash
tapio status                # observer health, event rates
tapio watch                 # live anomaly stream
tapio watch --network       # filter by observer
tapio recent                # last 20 anomalies
tapio health                # node health: network, storage, memory, cpu
tapio explain <pod>         # all kernel events for a pod
tapio mcp                   # start MCP server for AI agents
```

## AI native

Tapio provides context, not conclusions. It fills the factual fields — what the kernel saw — and lets AI agents reason about it.

```json
{
  "source": "tapio",
  "type": "kernel.container.oom_kill",
  "severity": "critical",
  "outcome": "failure",
  "error": { "code": "OOM_KILL", "message": "Container killed by OOM killer" },
  "context": { "node": "worker-3", "namespace": "default",
    "entities": [{ "kind": "pod", "id": "default/nginx-abc" }] },
  "data": { "memory_usage_bytes": 536870912, "memory_limit_bytes": 536870912,
    "pid": 1234, "exit_code": 137, "signal": 9 }
}
```

MCP tools let AI agents query kernel context directly:

```
tapio_node_health          — factual health snapshot
tapio_recent_anomalies     — last N anomalies with full kernel data
tapio_pod_context           — all kernel observations for a pod
tapio_connection_context   — network path health between two points
```

## Building

```bash
cargo build --release                    # agent + CLI
cargo test --workspace                   # all tests
cargo clippy --workspace --all-targets   # lint
```

Release builds use LTO + strip for minimal binary size (~8MB).

The eBPF C programs in `ebpf/` are loaded at runtime via [aya](https://github.com/aya-rs/aya). The agent requires Linux with eBPF support (kernel 5.8+). The CLI works on any platform.

## Architecture

```
tapio/
├── tapio-common/     # shared types: eBPF structs, event types, FALSE Protocol, sink trait
├── tapio-agent/      # DaemonSet binary: eBPF → observe → emit
├── tapio-cli/        # CLI + MCP server
├── ebpf/             # 4 C programs (the kernel truth)
└── eval/oracle/      # ground-truth tests
```

Part of **False Systems**: TAPIO (eBPF edge) → [POLKU](https://github.com/false-systems/polku) (protocol hub) → [AHTI](https://github.com/false-systems/ahti) (central intelligence)

## License

Apache 2.0
