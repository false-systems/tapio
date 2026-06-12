<p align="center">
  <br>
  <code>T A P I O</code>
  <br>
  <br>
  Opinionated eBPF observer for Linux and Kubernetes systems
  <br>
  <br>
  <img src="https://img.shields.io/badge/rust-2024%20edition-f74c00" alt="Rust">
  <img src="https://img.shields.io/badge/ebpf-kernel%205.8%2B-orange" alt="eBPF">
  <img src="https://img.shields.io/badge/license-Apache%202.0-blue" alt="License">
</p>

---

Tapio watches selected kernel facts and emits structured anomaly evidence.

It is not a generic eBPF framework. It is not a dashboard. It is not a root-cause engine. Tapio keeps the node agent small, fast, and boringly reliable.

> Tapio emits evidence, not exhaust.

## What Tapio Is

Tapio observes kernel-level failure facts that are easy to miss from application logs alone:

- a TCP connection was refused;
- a process was killed by OOM;
- a block I/O completed with an error or abnormal latency;
- CPU performance counters show stalls or IPC degradation.

Tapio turns those facts into named `kernel.*` anomaly events. Downstream systems can store, correlate, explain, alert, or remediate. Tapio does not guess.

## Current Shape

Tapio is a Rust workspace with six crates:

| Crate | Role |
| --- | --- |
| `tapio-agent` | Linux node agent. Loads eBPF, drains ring buffers, classifies facts, emits to sinks. |
| `tapio-controller` | Minimal cluster coordination skeleton for the agent/controller boundary. |
| `tapio-profile` | Pure Evidence Profile validation and compilation into wire config. |
| `tapio-wire` | Shared JSON protocol structs for the agent/controller boundary. |
| `tapio-cli` | Platform-independent CLI and MCP server for local event files. |
| `tapio-common` | Shared ABI structs, occurrence schema, anomaly constants, and sink traits. |

The product split is intentional:

```text
tapio-profile validates and compiles.
tapio-controller coordinates.
tapio-agent observes.
downstream systems explain.
```

The node agent must not grow Kubernetes watches, controller HTTP server dependencies, dashboards, policy engines, or reasoning fields.

## What Tapio Observes

| Observer | Kernel sources | Emitted anomalies |
| --- | --- | --- |
| Network | `inet_sock_set_state`, `tcp_receive_reset`, `tcp_retransmit_skb` | `kernel.network.connection_refused`, `kernel.network.connection_timeout`, `kernel.network.retransmit_spike`, `kernel.network.rtt_degradation` |
| Container | `sched_process_exit`, `oom/mark_victim` | `kernel.container.oom_kill`, `kernel.container.abnormal_exit` |
| Storage | `block_rq_issue`, `block_rq_complete` | `kernel.storage.io_error`, `kernel.storage.latency_spike` |
| Node PMC | `perf_event` counters | `kernel.node.cpu_stall`, `kernel.node.memory_pressure`, `kernel.node.ipc_degradation` |

Anomaly names are stable product concepts. They are not arbitrary metric labels.

## How It Works

```text
kernel tracepoints / perf events
  -> eBPF programs
  -> ring buffers
  -> Rust userspace parser
  -> anomaly classification
  -> sinks
```

Tapio filters close to the source:

- **BPF-side filtering:** cheap, obvious drops before crossing into userspace.
- **Rust-side classification:** threshold and state checks where userspace context is safer.

The BPF/Rust boundary is fixed by C structs mirrored in `tapio-common/src/ebpf.rs`. Tests assert size and field offsets so layout drift is caught before runtime.

Storage has a strict correctness rule: if `block_rq_issue` and `block_rq_complete` cannot uniquely correlate an inflight request, Tapio drops that completion and increments `tapio_correlation_drops_total{observer="storage",reason="ambiguous_inflight_io"}`. Missing evidence can be counted. Wrong evidence is not emitted.

## Example Event

Tapio emits occurrence JSON. Fields are factual: timestamp, anomaly type, severity, outcome, error, and kernel data. Reasoning fields are intentionally absent.

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
  "data": {
    "pid": 1234,
    "tid": 1234,
    "exit_code": 137,
    "signal": 9,
    "memory_usage_bytes": 536870912,
    "memory_limit_bytes": 0,
    "cgroup_id": 8429,
    "config_generation": 1,
    "timestamp_ns": 1743691381042000000
  }
}
```

`memory_limit_bytes` can be `0` for OOM kills because the kernel tracepoint does not expose the cgroup limit. Cluster context belongs in `tapio-controller` or downstream systems, not in the node hot path.

`config_generation` records which runtime config judged the event, so fleet config convergence is observable in the event stream itself.

## Quick Commands

```bash
cargo test --workspace
cargo clippy --workspace --all-targets -- -D warnings
scripts/verify-lean.sh
```

On Linux or inside Lima, also run:

```bash
scripts/smoke-ebpf-network.sh
scripts/smoke-agent-controller.sh
```

The eBPF smoke test builds the agent, loads real eBPF programs, triggers a TCP connection to a closed localhost port, and checks that Tapio records a network occurrence with the exact destination port. The agent/controller smoke test builds the agent and controller, then verifies hello, heartbeat, event payload delivery through `/v1/status` and trace logs, controller outage behavior, recovery, and agent restart.

## Agent

`tapio-agent` is Linux-only. It requires kernel 5.8+ with BTF and the capabilities `CAP_BPF`, `CAP_PERFMON`, and `CAP_NET_ADMIN`.

```bash
tapio-agent --sink=stdout
tapio-agent --sink=file
tapio-agent --sink=stdout --sink=file
tapio-agent --controller-endpoint=http://tapio-controller:8080 --sink=controller
tapio-agent --ebpf-dir /opt/tapio/ebpf
```

Important flags:

- `--config <path>`: TOML config file, default `/etc/tapio/tapio.toml`.
- `--sink <name>`: `stdout`, `file`, `http`, `controller`, or `otlp` when built with `--features otlp`; repeatable.
- `--ebpf-dir <path>`: directory containing compiled `.o` files.
- `--data-dir <path>`: file sink output directory, default `.tapio/occurrences`.
- `--http-endpoint <url>`: HTTP sink endpoint.
- `--controller-endpoint <url>`: controller base URL for config, hello, heartbeat, and controller event sink traffic.
- `--heartbeat-interval <seconds>`: controller heartbeat interval, minimum 5 seconds.

The agent runs standalone with `stdout` or `file` sinks. It does not need Kubernetes, a controller, or a network destination.

## CLI

The `tapio` CLI reads local occurrence files. It does not talk to the running agent.

```bash
tapio status
tapio watch
tapio watch --network
tapio watch --json
tapio recent
tapio recent --since 5m
tapio health
tapio health network
```

By default, the CLI reads `.tapio/occurrences/*.json`. Override that with `--data-dir` or `TAPIO_DATA_DIR`.

The CLI also provides `tapio mcp`, a read-only stdio JSON-RPC MCP server for querying recent anomalies and node health.

## Sinks

| Sink | Output |
| --- | --- |
| `stdout` | JSON lines to stdout |
| `file` | one occurrence JSON file per event |
| `http` | batched JSON `POST` to an HTTP endpoint |
| `controller` | bounded batched `tapio-wire/v1` event POSTs to `POST /v1/events` |
| `otlp` | OTLP/HTTP logs export when built with `--features otlp` |

Sink guarantees:

- Local `stdout` and `file` sinks are the default zero-service path.
- Controller sink overflow and send failures are counted and never block ring-buffer consumption.
- HTTP/OTLP export failures are surfaced as sink errors and counters.
- `otlp` rejects `https://` endpoints before opening a TCP connection or sending auth. Use a local collector, proxy, sidecar, or service mesh for TLS termination.

## Metrics

Prometheus metrics are optional and disabled by default. Enable them in TOML with `[metrics] enabled = true`.

Key metrics:

- `tapio_events_total`: ring-buffer records drained by userspace.
- `tapio_anomalies_total`: emitted anomalies by observer and type.
- `tapio_lost_events_total`: eBPF ring-buffer reserve failures.
- `tapio_malformed_events_total`: malformed/truncated records dropped by userspace.
- `tapio_correlation_drops_total`: intentionally dropped ambiguous evidence.
- `tapio_drain_cap_total`: drain loops that hit the per-tick cap.
- `tapio_sink_writes_total`: sink write attempts by sink and result.
- `tapio_sink_drops_total`: sink events dropped by sink and reason.
- `tapio_controller_send_failures_total`: failed hello, heartbeat, or event sends to the controller.
- `tapio_config_fetch_total`: controller config poll outcomes.

## Building eBPF Objects

There is no build script for production deployment. Compile the four eBPF C programs with clang and place them in the agent `--ebpf-dir`.

```bash
for prog in network_monitor container_monitor storage_monitor node_pmc_monitor; do
  clang -O2 -g -target bpf -D__TARGET_ARCH_x86 -I ebpf/headers \
    -c ebpf/${prog}.c -o /opt/tapio/ebpf/${prog}.o
done
```

Use `-D__TARGET_ARCH_arm64` for arm64 nodes.

## Lean Verification

`scripts/verify-lean.sh` checks:

- formatting;
- clippy;
- tests;
- release binary budgets;
- dependency boundaries;
- eBPF object budgets when Linux headers are available;
- eBPF map count and `max_entries` budgets.

Current budget model:

| Binary | Budget |
| --- | --- |
| `tapio-agent` | target 1.25 MB, hard 1.5 MB |
| `tapio` | hard 900 KB |
| `tapio-controller` | reported, no hard budget yet |

The script writes dependency snapshots under `/tmp/tapio-lean`. Budget increases should be explicit and justified.

## Agent / Controller Split

The current agent/controller boundary is agent-initiated HTTP/1.1 plus JSON using `tapio-wire/v1`.

Controller endpoints:

- `POST /v1/agents/hello`
- `GET /v1/agents/config`
- `POST /v1/agents/heartbeat`
- `POST /v1/events`

The controller is the HTTP server. The agent does not expose an inbound controller API and does not use gRPC on this path.

See:

- [docs/architecture.md](docs/architecture.md)
- [docs/agent-controller.md](docs/agent-controller.md)

## Runtime Config

Observer behavior is runtime-configurable through a versioned agent-to-kernel ABI, without eBPF reloads:

```text
EvidenceProfile YAML
  -> tapio-profile validates and compiles
  -> CompiledConfig (tapio-wire)
  -> tapio-controller distributes
  -> tapio-agent writes tapio_config map carriers
  -> eBPF programs read primitive flags and thresholds
  -> emitted events carry config_generation
  -> agent heartbeats report the applied config hash
```

The kernel side is deliberately dumb: a fixed-layout, version-checked `struct tapio_config` per observer object. All-zeros (the kernel's cold-start map state) is inert — nothing emits until real config lands. On ABI version mismatch, observers stay silent instead of misreading fields.

The operator side is deliberately strict: profiles are versioned YAML documents validated against a closed schema. Unknown fields, unknown observers, and out-of-range values are rejected, not ignored. `compile` is infallible — every failure happens during validation.

The controller convergence signal is the compiled config hash. Agents report
the hash they have actually applied in heartbeats; an empty hash means a
controller-mode agent is still unconfigured.

See [docs/agent-kernel-config-abi.md](docs/agent-kernel-config-abi.md).

## Repository Layout

```text
tapio-agent/      node-local eBPF observer
tapio-controller/ cluster coordination skeleton
tapio-profile/    Evidence Profile validation and compilation
tapio-wire/       agent/controller protocol structs
tapio-cli/        CLI and MCP server for local occurrence files
tapio-common/     shared ABI structs, occurrences, events, sinks
ebpf/             eBPF C programs and headers
scripts/          lean checks, dependency checks, runtime smoke tests
docs/             architecture, agent/controller, and config ABI notes
```

## Non-Goals

Tapio intentionally does not:

- forward every kernel event;
- infer root cause;
- fill reasoning, explanation, remediation, or suggested-fix fields;
- store/index events long-term;
- replace Prometheus, Grafana, or OpenTelemetry;
- put Kubernetes watches in the node agent;
- expose an inbound controller API from the agent;
- use gRPC for the v0 agent/controller path;
- become a generic observability platform.

Tapio owns node-level kernel evidence. Everything else can consume that evidence later.

---

Apache 2.0
