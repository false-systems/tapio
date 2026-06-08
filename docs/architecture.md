# Tapio Architecture

Tapio is a tiny, opinionated eBPF observer for Linux and Kubernetes that emits structured kernel evidence.

`Tapio emits evidence, not exhaust.`

`tapio-agent observes.`
`tapio-controller coordinates.`
`downstream systems explain.`

`Tapio keeps intelligence out of the kernel and bloat out of the node agent.`

`No kernel-space agent society.`

`Missing evidence can be counted. Wrong evidence must not be emitted.`

Tapio is not an observability platform, dashboard system, SIEM, policy engine, AI root-cause engine, Cilium-lite, Tetragon-lite, or Kubernetes operator disguised as a node agent.

## Runtime Model

### eBPF Programs

eBPF programs observe selected kernel facts, keep tiny bounded correlation state, emit fixed event structs, expose counters, and read tiny runtime config from BPF maps.

Allowed responsibilities:

- network, storage, container/process, and optional node/PMC observation;
- bounded factual correlation state in BPF maps;
- ambiguity detection;
- malformed, lost, and correlation-drop counters;
- ring buffer event emission.

Forbidden responsibilities:

- root-cause analysis, policy orchestration, Kubernetes awareness, dynamic rule engines, large state machines, or rich eBPF-to-eBPF messaging.

### tapio-agent

`tapio-agent` is the node-local observer runtime. It loads and attaches eBPF programs, reads ring buffers, parses fixed structs safely, maintains tiny local metrics, writes small config values into BPF maps, batches bounded evidence, sends to configured sinks or a controller, and continues observing when the controller is unavailable.

Final target constraints:

- no Kubernetes API client, `kube`, or `k8s-openapi`;
- no inbound HTTP server, `axum`, gRPC, `tonic`, or `reqwest`;
- avoid `hyper` unless explicitly justified;
- no cluster cache, pod informer, CRD logic, TokenReview, policy engine, rule engine, or AI/RCA fields.

### tapio-controller

`tapio-controller` is one-per-cluster coordination. It receives agent hello, heartbeat, config pull, and event batch requests over `tapio-wire/v1`; tracks agents and stale heartbeats; validates event batches; counts accepted/rejected events; and later may own Kubernetes metadata, TokenReview, cluster config, and downstream routing.

The controller may use server and Kubernetes dependencies. `axum`, `kube`, and `k8s-openapi` belong here when needed, not in the agent.

### tapio-wire

`tapio-wire` is the small versioned protocol crate. It defines JSON-compatible request/response structs, validation, the version constant, and the facts-not-reasoning boundary for hello, config, heartbeat, and event batches.

It must not depend on HTTP server/client frameworks, Kubernetes crates, gRPC crates, or heavy client stacks.

### Downstream Systems

Tapio preserves kernel evidence. Downstream systems decide what to do with it. Long-term storage, causal graphs, root-cause analysis, AI explanation, policy decisions, remediation, dashboards, and alert routing belong downstream.

## Communication Model

- eBPF to eBPF: shared BPF maps for tiny factual state only; tail calls only for verifier pressure or strict modularity.
- eBPF to agent: ring buffer events for completed facts; maps for counters/state.
- Agent to eBPF: BPF config maps for tiny runtime config.
- Agent to controller: outbound-only HTTP/1.1 plus JSON using `tapio-wire/v1`.
- Controller to downstream: future or optional exports/sinks.

The controller never calls into the agent. The agent exposes no controller-facing inbound server.

The v0 controller protocol has four operations:

- `POST /v1/agents/hello`
- `GET /v1/agents/config`
- `POST /v1/agents/heartbeat`
- `POST /v1/events`

## Failure Model

Controller down: eBPF observation and ring buffer consumption continue. The agent uses last known valid config or local defaults.

Config fetch failure: increment visible failure counters, keep current config, and retry with backoff.

Event queue overflow: drop according to a documented bounded policy and increment drop counters. Silent evidence loss is a correctness bug.

Malformed eBPF events: reject the record, increment malformed counters, and do not emit guessed evidence.

Ambiguous storage correlation: do not emit misleading evidence. Increment `tapio_correlation_drops_total{observer="storage",reason="ambiguous_inflight_io"}`.

Sink/controller send failure: increment failure counters and do not block ring buffer consumption.

Stale/missing agents: controller detects stale heartbeats; this is cluster coordination, not node-agent policy.

## Security Model

The tiny agent client must not silently downgrade HTTPS to plaintext. If TLS is not implemented, `https://` controller URLs must be rejected clearly. Redirects must not be followed by default.

Bearer tokens must never be logged. Any token wrapper must redact `Debug`.

Plaintext HTTP is allowed only when explicitly configured and documented as in-cluster, local, or trusted-network mode. Production TLS may initially be delegated to a service mesh, proxy, or local collector, but docs must say that directly.

Future Kubernetes TokenReview and service-account identity validation belong in the controller.

## Performance and Size Model

Ring buffer consumption must not block on controller I/O. Serialization should stay out of the ring buffer hot path where practical. Event batches, request bodies, queues, logs, and retry loops must be bounded.

Idle overhead should be near-zero. Avoid heap churn and heavy dependencies in hot paths.

Existing eBPF object and map budgets remain part of architecture enforcement. Agent binary size must be measured before and after major changes. The target is the smallest practical agent; dependency and binary budgets are CI concerns, not release afterthoughts.

### Binary budget model

The node agent uses a **two-level budget** so CI prevents regression without lying about the current baseline:

| Budget | Env var | Default | Behavior |
| --- | --- | --- | --- |
| Hard | `AGENT_MAX_BYTES` | `1750000` | fail if `tapio-agent` exceeds it |
| Target | `AGENT_TARGET_BYTES` | `1500000` | warn (do not fail) if exceeded |
| CLI hard | `CLI_MAX_BYTES` | `900000` | fail if `tapio` exceeds it |

The hard budget stops growth; the target budget guides slimming. `tapio-controller` size is reported but has no hard budget yet.

**Ratcheting down:** as slimming work lands and the agent stays comfortably below a smaller size in CI, lower `AGENT_TARGET_BYTES` first, then lower `AGENT_MAX_BYTES` once the new ceiling holds. Override a budget only intentionally and with a documented reason, e.g. `AGENT_MAX_BYTES=1800000 scripts/verify-lean.sh`. The current baseline (`tapio-agent` ~1.64 MB) is above target and under the hard limit — slimming is wanted, regression past 1.75 MB is blocked.

### Lean-gate reliability

`scripts/verify-lean.sh` must not silently pass when it could not fully verify. The HTTP sink test needs real loopback networking; some restricted sandboxes reject the bind with `EPERM`. The test skips loudly on that specific error (it is a host limitation, not a code defect), and the lean gate detects the skip:

- by default a skipped required test **fails** the gate (it did not fully verify);
- `TAPIO_LEAN_ALLOW_DEGRADED=1` accepts a clearly-labeled **PARTIAL** run;
- `TAPIO_LEAN_REQUIRE_NET=1` forces the test to run (and fail) where networking is expected.

## Dependency Boundaries

| Crate | Allowed | Forbidden |
| --- | --- | --- |
| `tapio-agent` | `aya`, `tokio`, `serde`, local sinks, tiny outbound HTTP if justified | `kube`, `k8s-openapi`, `axum`, `tonic`, `reqwest`, inbound server, Kubernetes watches |
| `tapio-controller` | `axum`, future `kube`/`k8s-openapi`, validation, registry, routing | controller-to-agent RPC, dashboards, policy/RCA engines |
| `tapio-wire` | `serde`, `serde_json`, validation errors | `axum`, `hyper`, `kube`, `k8s-openapi`, `tonic`, `reqwest` |
| Downstream | storage, alerting, RCA, AI, dashboards, policy/remediation | node hot-path observation |

Current audit from this checkout:

| Dependency | Present in agent? | Why? | Belongs in final agent? | Action |
| --- | --- | --- | --- | --- |
| `kube` | no | Not in current tree. | no | Forbid. |
| `k8s-openapi` | no | Not in current tree. | no | Forbid. |
| `axum` | no | Controller owns HTTP server. | no | Forbid in agent; keep in controller. |
| `hyper` | no | Pulled by controller through `axum`, not agent. | maybe | Warn/fail unless a tiny client proves unavoidable. |
| `tonic` | no | Not in current tree. | no | Forbid. |
| `reqwest` | no | Not in current tree. | no | Forbid. |

`tapio-wire` currently depends only on `serde`, `serde_json`, and `thiserror`.

## Agent Dependency Migration Plan

This checkout does not currently show direct agent violations for `kube`, `k8s-openapi`, `axum`, `hyper`, `tonic`, or `reqwest`. If those reappear, handle them in this order:

1. `docs: define Tapio runtime architecture and dependency boundaries`
2. `chore(agent): audit and gate forbidden dependencies`
3. `refactor(agent): move Kubernetes enrichment to controller boundary`
4. `refactor(agent): remove inbound HTTP server path`
5. `feat(agent): add tiny outbound controller client`
6. `ci: enforce tapio-agent dependency and binary budgets`

Do not add the outbound controller client before the agent dependency boundary is clean.

## Future Agent Outbound Client Design

The future client is outbound-only HTTP/1.1 plus JSON, using `tapio-wire/v1`. It must not use gRPC, expose an inbound server, follow redirects, log bearer tokens, or accept `https://` when TLS is unsupported.

Required mechanics:

- bounded event queue, max batch size, max request size, max heartbeat size;
- monotonically increasing event batch sequence numbers;
- retry with backoff and no infinite blocking;
- failure and drop counters;
- config fallback to last known valid config or defaults;
- controller outage never stops eBPF observation.

## Guardrails

CI should run:

- `cargo fmt --all --check`
- `cargo clippy --workspace --all-targets -- -D warnings`
- `cargo test --workspace`
- `cargo build --release -p tapio-agent -p tapio-controller`
- dependency boundary checks for `tapio-agent` and `tapio-wire`
- release binary-size reporting and agreed budgets
- existing eBPF object and map budgets
- Linux/Lima network smoke test for kernel/runtime behavior changes

Hard-failing dependency checks are safe once the baseline is clean. Binary budgets should become hard fail after the measured baseline and discrepancy history are documented.

## Non-Goals

Tapio will not add a dashboard, generic policy engine, AI/RCA layer, Kubernetes watches in the agent, gRPC in the agent, inbound agent server, CRDs in the first hardening phase, admission controller, storage database, remote shell/control path, or new anomaly families as part of architecture enforcement.
