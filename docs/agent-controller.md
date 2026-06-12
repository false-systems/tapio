# Tapio Agent/Controller Runtime Model

Tapio keeps intelligence out of the kernel and bloat out of the node agent.

The agent observes. The controller coordinates. Downstream systems explain.

See [architecture.md](architecture.md) for the source-of-truth architecture law, dependency boundaries, failure model, and migration roadmap.

## Runtime Split

`tapio-agent` is the tiny node-local eBPF observer. It loads eBPF programs, reads ring buffers, writes tiny BPF config maps, exposes local counters and health, batches kernel evidence, and sends evidence to local sinks or a controller. In Kubernetes it runs as a DaemonSet.

`tapio-controller` is the cluster coordination point. It receives agent hello, config, heartbeat, and event batch traffic over `tapio-wire/v1`, keeps an in-memory registry in v0, and may later become Kubernetes-aware. The controller may be heavier than the agent, but it must remain modest and boring.

Downstream systems store, correlate, explain, or act. Tapio does not infer root cause and does not emit reasoning fields.

## Communication Model

Kernel to agent communication stays unchanged:

- eBPF programs emit fixed structs to ring buffers for completed facts.
- eBPF programs use shared BPF maps only for tiny factual state.
- The agent writes tiny runtime values into BPF config maps where applicable.
- There is no kernel-space agent society.

Agent to controller communication is agent-initiated HTTP/1.1 plus JSON in v0. The controller is the HTTP server. The agent does not expose an inbound HTTP server for controller calls, and the v0 agent path does not use gRPC.

The v0 protocol is `tapio-wire/v1`:

- `POST /v1/agents/hello`
- `GET /v1/agents/config?agent_id=<agent-id>&node_name=<node-name>`
- `POST /v1/agents/heartbeat`
- `POST /v1/events`

Payloads are small, bounded, and versioned. Unknown future JSON fields are ignored by default, while missing required fields and unsupported wire versions fail clearly.

See [wire-api-standard.md](wire-api-standard.md) for the versioning,
compatibility, config identity, ETag caching, and error-envelope policy.

## Security Assumptions

The v0 controller endpoint is intended for explicit in-cluster plaintext HTTP or local development. TLS can be provided by the cluster, service mesh, or proxy layer initially.

The tiny agent client must not silently downgrade `https://` to plaintext. If an HTTPS controller URL is configured before TLS support exists in the agent client, the agent must reject it clearly. Redirects must not be followed by default. Bearer tokens, when configured later, must not be logged and must use redacted debug output.

Production Kubernetes mode should eventually bind agent identity to service account identity and validate it at the controller, for example with TokenReview. The controller must not trust `node_name` blindly in production mode.

## Failure Behavior

Controller outage must not stop kernel observation or ring buffer consumption. Agents keep the last known valid config, or safe local defaults if no config has ever been fetched. Event batching queues must be bounded; overflow and send failures must increment visible counters such as `tapio_controller_send_failures_total` and `tapio_sink_drops_total`.

Heartbeats include evidence-loss counters and degraded reasons. They do not include event payloads or high-cardinality labels.

## Non-Goals

- No controller-to-agent RPC.
- No inbound HTTP server in the agent.
- No gRPC in the v0 agent/controller path.
- No Kubernetes watches in the agent as part of this split.
- No CRDs, admission webhooks, database, dashboards, policy engine, rule engine, or AI/RCA layer in the first controller.
- No policy language, WASM, Lua, arbitrary scripts, or regex-heavy filtering in the agent.

## Dependency Guardrails

`tapio-wire` contains only shared protocol structs and validation. It has no HTTP server, HTTP client, Kubernetes, or gRPC dependencies.

`tapio-controller` owns the HTTP server dependency for the v0 endpoints.

`tapio-agent` must not inherit controller dependencies through workspace defaults. The v0 controller path must not add `kube`, `k8s-openapi`, `axum`, `hyper`, `tonic`, or `reqwest` to the agent dependency path. `scripts/check-agent-deps.sh` enforces this denylist.
