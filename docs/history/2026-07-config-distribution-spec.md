> **Historical record — superseded, retained for provenance.**
>
> This is the four-PR specification that produced #650–#653 (agent config
> distribution). It is **not current** and must not be followed as guidance:
> it cites `tapio-wire/src/v0.rs`, since deleted, and a 1,250,000-byte agent
> budget that has since moved. It is kept because it is the only written
> record of why those four PRs were shaped the way they were.
>
> Committed 2026-08-11. Until then it existed only as an untracked file in one
> working tree, where any `git clean -fd` would have destroyed it.

# Tapio Config Distribution: Wire Standard, Controller, Agent Polling, Status Echo

You are completing the remaining three lines of the Final API Law:

```text
The profile crate validates and compiles.        DONE (#647, #649)
The controller distributes compiled config.      PR 2 of this document
The agent polls compiled config.                 PR 3 of this document
The agent reports the config hash it is running. PR 4 of this document
The kernel receives only primitive knobs.        DONE (#646)
```

This document specifies **four PRs, one per feature, in strict order**. Each
PR stands alone: it compiles, passes every gate, and could ship without the
PRs after it. Do not combine them. Do not reorder them. Open each PR with
`gh pr create` before starting the next branch from updated `main` (or from
the previous branch if review is pending — state which in the PR body).

## Context You Must Read First

- `CLAUDE.md` — project rules, all of them.
- `docs/agent-controller.md` — the runtime split and security assumptions.
- `docs/agent-kernel-config-abi.md` — the kernel ABI the compiled config maps onto.
- `tapio-wire/src/lib.rs` — the live v1 wire surface (`HelloRequest`,
  `ConfigResponse`, `HeartbeatRequest`, `EventBatchRequest`, `WireError`).
- `tapio-wire/src/v0.rs` — mostly dead: `AgentHello`, `AgentConfigRequest`,
  `AgentConfigResponse`, `AgentHeartbeat`, `EventBatch*`, `ObserverConfig`
  remnants are referenced by nothing outside their own tests. Only
  `CompiledConfig` and its sub-structs are alive, re-exported via `lib.rs`.
- `tapio-controller/src/lib.rs` — existing endpoints, `ControllerState`,
  `ApiError`. The config endpoint exists but serves a static default with no
  ETag, no hash, no generation, no profile input.
- `tapio-profile/src/lib.rs` — `validate`/`compile`, `BuiltinSet::v0()`,
  `ProfileError`.
- `tapio-agent/src/sink/http.rs` — the hand-rolled minimal HTTP/1.1 client
  (`post_json` over `std::net::TcpStream`). This is the only HTTP client the
  agent is allowed to have; you will extract and extend it, not add one.
- `tapio-agent/src/observer/mod.rs` — `write_tapio_config`,
  `metrics_map_fd`/`map_raw_fd` (the extract-fd-then-drop-borrow pattern you
  will reuse for runtime carrier rewrites).
- `tapio-agent/src/config.rs` — TOML thresholds and
  `Thresholds::tapio_config()` (the standalone-mode config source).
- `scripts/check-dependency-boundaries.sh`, `scripts/verify-lean.sh`.

Repository facts: Rust edition 2024, MSRV 1.85. Pre-commit runs fmt +
clippy `-D warnings`. Lean budgets: agent target 1,250,000 / hard 1,500,000
bytes (current: ~1,121,000 — you have ~130 KB of headroom to target; spend it
grudgingly), CLI hard 900,000. eBPF objects and maps have per-file budgets in
`verify-lean.sh`; none of these PRs may move them.

## Laws That Apply to Every PR

1. **The controller is the only HTTP server.** The agent initiates
   everything. No push, no watch, no streaming, no gRPC, no inbound agent API.
2. **The agent's dependency denylist is absolute**: no `axum`, `hyper`,
   `tonic`, `reqwest`, `kube`, `k8s-openapi`, `tapio-profile`. The agent may
   depend on `tapio-wire` (structs + serde only). The boundary script enforces
   this; you extend the script, never weaken it.
3. **The content hash is the fleet convergence key.** Generation is a
   human-friendly ordinal that resets with the controller process; the hash is
   authoritative. Nothing may treat generation as globally monotonic.
4. **Controller outage must not stop kernel observation.** The agent keeps
   running on its current config. Failures are counted, never silent.
5. **Zero config is inert** (kernel ABI law). A controller-managed agent that
   has never received config runs observers with zeroed carriers and emits
   nothing. No default profile is baked into the agent binary.
6. **Project rules in full**: no `.unwrap()` outside tests, no `println!`,
   no dead code, no stubs, no TODOs, tracing for logs, metrics families added
   only deliberately and documented in CLAUDE.md.
7. **Engineering doctrine**: never hold a lock across `.await`; use
   `tokio::sync::watch` for "latest value, many readers" config fan-out;
   bounded channels elsewhere; validate at the boundary, map domain errors to
   HTTP at the edge (`ApiError`), never leak internals in error bodies; test
   names read as behavior specifications.

---

## PR 1 — `feat(wire): consolidate wire surface and publish the API standard`

Branch: `feat/wire-api-standard`

### 1a. Kill the dead wire surface

`tapio-wire/src/v0.rs` survives only because `CompiledConfig` lives there.
Move `CompiledConfig`, `CompiledNetwork`, `CompiledStorage`,
`CompiledContainer`, `CompiledNodePmc` (and their tests) to a new
`tapio-wire/src/config.rs`, re-exported at the crate root exactly as today —
`tapio_profile` and `tapio-controller` must not need import changes. Delete
`v0.rs` and the `pub mod v0` entirely: `AgentHello`, `AgentConfigRequest`,
`AgentConfigResponse`, `AgentHeartbeat`, `EventBatch`, `EventBatchResponse`,
`AgentCounters`, `DegradedReason` (the v0 one), `WIRE_VERSION` ("tapio-wire/v0")
are dead code, and the project bans dead code. The v1 surface in `lib.rs` is
the only wire protocol.

### 1b. Structured error envelope

Replace the controller's `{"error": "<string>"}` body with one envelope used
by every error response:

```json
{ "error": { "code": "MISSING_FIELD", "message": "agent_id is required" } }
```

`code` is SCREAMING_SNAKE, machine-matchable, derived from the `WireError`
(and later `ProfileError`) variant. `message` is human-readable. No stack
traces, no file paths, no internals. Implement as one `IntoResponse` in the
controller; update controller tests asserting bodies.

### 1c. The standard: `docs/wire-api-standard.md`

Write the versioning standard as policy. It must cover, at minimum:

- **Protocol identity**: `tapio-wire/v1` appears in two places — the URL
  path (`/v1/...`) and the `wire_version` field every payload carries. They
  move together. A request with an unsupported `wire_version` is rejected
  `400 UNSUPPORTED_VERSION` regardless of path.
- **Compatibility policy**: within v1, changes are additive-only — new
  optional fields with serde defaults. Field removal, rename, type change, or
  semantic change requires `tapio-wire/v2` + `/v2/` routes. No multi-version
  support in v0 of the product: the controller speaks exactly one version.
- **The unknown-field asymmetry, stated as policy**: machine-to-machine wire
  payloads IGNORE unknown fields (forward compatibility — an older controller
  must tolerate a newer agent's additive fields and vice versa); the
  operator-facing EvidenceProfile YAML REJECTS unknown fields (humans make
  typos; machines don't). This asymmetry is deliberate. Document it so nobody
  "fixes" it in either direction.
- **Config identity**: `config_version` is the generation — a base-10 `u32`
  rendered as a string, assigned by the controller, starting at 1, bumped on
  every config change the controller observes, **reset when the controller
  restarts**. `config_hash` is `sha256:<lowercase-hex>` over the canonical
  `serde_json::to_vec` of the `CompiledConfig` value. The hash is the
  convergence key; the generation is for humans and kernel stamping.
- **Caching contract**: `GET /v1/agents/config` returns
  `ETag: "<config_hash>"`. Agents send `If-None-Match`; the controller
  returns `304 Not Modified` with no body on match. The scaling story: many
  agents poll, most receive cheap 304s.
- **Error envelope** (from 1b), with the status-code table:
  400 malformed/unsupported-version/missing-field, 422 semantic violations
  (reasoning fields), 404 unknown endpoint, 500 never carries internals.
- **Explicit refusals**: no pagination (no list endpoints exist), no
  rate-limiting in v0 (in-cluster trust), no auth in v0 (documented gap, TLS
  and identity at the mesh/proxy layer per `docs/agent-controller.md`), no
  content negotiation, JSON only.

### PR 1 tests and gates

- Wire round-trip tests survive the move unchanged (import paths only).
- Controller tests assert the new error envelope shape and codes.
- A test proving unknown fields in a v1 request payload are ignored, and one
  proving an unsupported `wire_version` is rejected with code
  `UNSUPPORTED_VERSION`.
- All gates: fmt, clippy, workspace tests, boundary script, verify-lean.

---

## PR 2 — `feat(controller): distribute compiled config with ETag`

Branch: `feat/controller-config-distribution`

### What the controller gains

1. **Profile input**: a `--profile <path>` flag (repeatable later; exactly one
   in v0). The file is EvidenceProfile YAML. The controller deserializes it
   (`serde_yaml`), runs `tapio_profile::validate` against `BuiltinSet::v0()`,
   and compiles. **Any failure is a startup failure**: print the structured
   `ProfileError` (its `Display` includes the field path) and exit non-zero.
   A controller that cannot parse its profile must not serve a fallback.
   With no `--profile` flag, the controller serves the compiled
   `production-default` builtin — the same bytes a minimal profile document
   produces.
2. **Config identity**: on startup the controller computes
   `config_hash = sha256:<hex>` of the canonical `serde_json::to_vec` of the
   `CompiledConfig` and assigns generation 1. `ConfigResponse` gains a
   `config_hash` field (additive, serde default — per the standard).
   `version` carries the generation.
3. **ETag caching**: `GET /v1/agents/config` sets `ETag: "<config_hash>"`.
   When `If-None-Match` matches, return `304` with empty body. Conditional
   logic lives in the axum handler; `ControllerState` stays HTTP-free.
4. **Dependencies**: `serde_yaml = "0.9"` and `sha2` enter
   **tapio-controller only**. Justify both in the PR body. `serde_yaml` is
   archived upstream; record in `docs/wire-api-standard.md` (operational
   notes) that it is pinned, used only at startup on operator-trusted input,
   and replaceable behind the same `Deserialize` boundary. The boundary
   script gains: `tapio-profile`, `serde_yaml`, and `sha2` forbidden in
   `tapio-agent` and `tapio-cli`.

### Deliberately not in PR 2

No SIGHUP/file-watch reload (controller restart is the v0 config-change
mechanism — generation reset is why the hash is authoritative). No profile
CRUD API. No per-node config: `agent_id`/`node_name` query params are
validated for presence but do not vary the response; the standard documents
that they exist so per-node assignment can become a controller-side change.

### PR 2 tests

- Controller startup: valid profile compiles and is served; invalid profile
  (bad range, unknown field, unknown base) exits with the field path in the
  error output (assert via the library API, not by spawning the binary, where
  possible — split `load_profile(path) -> Result<CompiledConfig, ...>` so it
  is testable).
- Endpoint: 200 with ETag and correct hash; `If-None-Match` hit → 304 with
  empty body; miss (stale hash) → 200 with full body; hash is stable across
  two identical controller constructions and changes when the profile
  changes one threshold.
- `ConfigResponse` with `config_hash` round-trips; an old-style payload
  without the field still deserializes (additive-field proof).

---

## PR 3 — `feat(agent): poll compiled config and apply it live`

Branch: `feat/agent-config-polling`

This is the largest PR. The agent gains a controller mode without losing
standalone mode, and config changes take effect without restarting observers
or reloading eBPF.

### Mode selection

- `--controller-endpoint <url>` absent → **standalone mode**: exactly today's
  behavior, TOML-driven, generation 1, no network. Untouched code paths.
- `--controller-endpoint <url>` present → **controller mode**: the TOML
  `[thresholds]` section is ignored (log one warning if present), observers
  start with **zeroed inert carriers** (the ABI cold-start state — they load,
  attach, and emit nothing), and a poll task fetches config. The first 200
  response compiles into live config; until then the agent is observably
  unconfigured (see PR 4's degraded reason).
- The endpoint must be `http://`. Reject `https://` at flag parsing with the
  same rationale and wording style as the sink rule. Never follow redirects.

### The poll task

A single tokio task (`JoinSet`-managed alongside the observers):

- `GET /v1/agents/config?agent_id=...&node_name=...` with `If-None-Match`
  when a hash is held, on a `--config-poll-interval` (default 30s, min 5s).
- On 200: parse `ConfigResponse`, verify `wire_version`, parse `version` as
  `u32` (reject the config and count it if not), apply (below), store hash.
- On 304: nothing. On error/timeout: keep current config, count, retry next
  tick with the same interval (no backoff sophistication in v0).
- HTTP client: extract `post_json`'s transport core from
  `tapio-agent/src/sink/http.rs` into a shared agent-internal module (e.g.
  `tapio-agent/src/httpc.rs`) supporting GET with headers, bounded response
  size (cap: 1 MiB), connect/read timeouts, status + header parsing
  sufficient for `ETag`. `HttpSink` is refactored onto the same core —
  one transport, two users. The client stays `std::net` + blocking inside
  `spawn_blocking`; do not introduce an async HTTP stack.

### Applying config live

Two halves, matching the two halves of `CompiledConfig`:

1. **Kernel knobs**: convert `CompiledConfig` → `TapioConfig`
   (enabled flags → `TAPIO_F_*` bits, thresholds verbatim,
   `ignore_exit_codes` truncated/counted per ABI rules, generation from
   `version`). Conversion lives in the agent (`config.rs` or a sibling),
   pure and unit-tested against the ABI invariants (count ≤ populated
   entries, abi_version always current).
   For runtime rewrite: at observer startup, extract the `tapio_config` map
   raw fd (the `metrics_map_fd` pattern — extract, drop the borrow) and
   register it with a shared `ConfigCarriers` registry
   (`Arc<Mutex<Vec<(&'static str, RawFd)>>>` is enough). The poll task writes
   the new `TapioConfig` to every registered carrier via the existing
   `bpf_map_update` syscall path. Per the ABI doc: tearing across observers
   during fan-out is accepted; events carry the generation that judged them.
2. **Userspace knobs** (PMC stall/IPC thresholds): a
   `tokio::sync::watch::Sender<PmcThresholds>` owned by the poll task;
   the node_pmc observer reads `watch::Receiver::borrow()` at classify time
   (permille/milli → f64 conversion at the watch boundary, one place).
   Standalone mode sends the TOML values once at startup through the same
   channel — one mechanism, two sources.

### Observability

- New metric family (deliberate, documented in CLAUDE.md):
  `tapio_config_fetch_total{result="applied|not_modified|error|rejected"}`.
- `tracing::info!` on every applied config with generation and hash.
- The startup log line states the mode (standalone/controller) exactly once.

### PR 3 tests

- Mode selection: standalone path produces today's `TapioConfig` from TOML
  (existing tests keep passing); controller mode with no config yet writes
  zeroed carriers (assert via the conversion/registry layer, not a live
  kernel).
- Conversion: `CompiledConfig` → `TapioConfig` field-by-field, including
  flag bits, generation parse failure rejection, and exit-code count
  invariants.
- HTTP client: GET with `If-None-Match` formats correctly; 304, 200, and
  oversized-body handling against a `std::net` loopback fixture (follow the
  existing loopback test pattern with the `TAPIO_LEAN_REQUIRE_NET` skip
  convention).
- Watch-channel threshold update reaches a `classify()` call.
- **Lean**: report the agent binary delta in the PR body. The wire dep and
  client refactor must keep the agent under the 1,250,000 target. If it
  does not, stop and say so — do not raise the budget in this PR.

### Runtime smoke (Linux/Lima, in the PR body)

Extend or sibling `scripts/smoke-ebpf-network.sh`: start the controller with
a profile whose `rtt_spike` or storage threshold differs from default, start
the agent in controller mode, assert an emitted occurrence carries
`config_generation` = the controller's generation and the changed threshold
behavior. This is the proof the whole chain works; a PR body without it is
incomplete.

---

## PR 4 — `feat(agent): report active config identity in heartbeats`

Branch: `feat/agent-config-status`

The last law: the agent reports the config hash it is actually running.

- `HeartbeatRequest` gains `config_hash: String` (additive, serde default
  empty — old payloads still validate). The agent fills it with the hash of
  the config it has **applied**, not the one it last fetched; empty when
  unconfigured.
- A new `DegradedReason::Unconfigured` variant: set in controller mode from
  startup until the first config is applied. (Check the existing
  `DegradedReason` enum in `lib.rs` for naming style.)
- The controller's stored heartbeat state exposes per-agent
  generation/hash so `stale_agents`-style queries can answer "which nodes
  have not converged to hash H" — add
  `ControllerState::unconverged_agents(target_hash) -> Vec<String>` with
  tests. No new endpoint for it in v0; it is library surface for the future
  status API.
- Docs sweep in the same PR: `docs/agent-controller.md` (heartbeat payload,
  convergence story), `docs/wire-api-standard.md` (the additive field, as
  the standard's own first worked example of v1 evolution), `CLAUDE.md`
  (metric family from PR 3 if not yet recorded, agent flags list), README
  Runtime Config section if the chain description changed.

### PR 4 tests

- Heartbeat round-trip with and without `config_hash` (additive proof).
- `unconverged_agents` over a mixed registry.
- Agent-side: heartbeat builder uses applied-not-fetched hash; unconfigured
  agents send empty hash + `Unconfigured` degraded reason.

---

## Explicit Refusals (all four PRs)

Written as policy, not omission:

- no last-known-good disk cache in the agent (explicitly deferred; the spec
  reserves it — do not half-build it);
- no controller persistence: registry, generation, and config are in-memory;
- no SIGHUP/hot-reload of the controller profile;
- no profile CRUD API, no `POST /v1/profiles`;
- no push, watch, streaming, or gRPC anywhere on this path;
- no TLS in the agent client (reject `https://`, document the boundary);
- no auth in v0 (documented gap in the standard, not an accident);
- no per-node or per-namespace config variation (params carried, ignored);
- no multi-wire-version support; v1 is the only spoken version;
- no async HTTP stack in the agent; `std::net` + `spawn_blocking` only;
- no new metric families beyond `tapio_config_fetch_total`.

## Gates (every PR, before opening)

```bash
cargo fmt --all --check
cargo clippy --workspace --all-targets -- -D warnings
cargo test --workspace
./scripts/check-dependency-boundaries.sh
./scripts/verify-lean.sh
```

If your host cannot compile eBPF or build the Linux agent, say so in the PR
body and label which numbers come from a limited host. Do not present
non-Linux binary sizes as the agent's size. PRs 3 and 4 additionally require
the runtime smoke evidence from a real Linux host (Lima qualifies).

## PR Body Template (all four)

Each PR body reports: scope (which PR of this document), public API/wire
changes, new dependencies and where they are allowed, binary budget deltas
with host noted, test counts, gate results, refusals touched, and — for PR 3
onward — smoke evidence with the observed `config_generation`.
