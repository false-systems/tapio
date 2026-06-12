# Tapio Wire API Standard

This document defines the v1 agent/controller wire policy.

Tapio has one spoken wire version in v0 of the product:
`tapio-wire/v1` on `/v1/...` routes. The controller is the only HTTP server.
The agent initiates all traffic.

## Protocol Identity

The protocol version appears in two places:

- the URL path prefix, for example `/v1/agents/hello`;
- the `wire_version` field carried by every JSON payload.

Those two identities move together. A request with an unsupported
`wire_version` is rejected with HTTP `400` and error code
`UNSUPPORTED_VERSION`, regardless of the path that delivered it.

The controller speaks exactly one version in v0 of the product. No
multi-version negotiation or compatibility shim exists.

The v0 controller protocol has five operations:

- `POST /v1/agents/hello`
- `GET /v1/agents/config`
- `POST /v1/agents/heartbeat`
- `POST /v1/events`
- `GET /v1/status`

## Compatibility Policy

Within `tapio-wire/v1`, changes are additive-only:

- new optional fields may be added when they have serde defaults;
- unknown fields in machine-to-machine payloads are ignored;
- existing fields must not be removed, renamed, have their type changed, or
  have their semantics changed.

Any removal, rename, type change, or semantic break requires
`tapio-wire/v2` plus `/v2/` routes.

No schema break may hide behind the same `wire_version` once any deployed
agent/controller pair speaks that shape.

## Unknown-Field Policy

Tapio deliberately uses different unknown-field behavior for different
surfaces.

Machine-to-machine wire payloads ignore unknown fields. This gives forward
compatibility: an older controller can tolerate a newer agent's additive
fields, and an older agent can tolerate a newer controller's additive fields.

Operator-facing `EvidenceProfile` YAML rejects unknown fields. Humans make
typos, and Tapio must not turn a typo into a silent outage.

Do not "fix" this asymmetry in either direction.

## Config Identity

`config_version` is the config generation. It is a base-10 `u32` rendered as a
string, assigned by the controller, starts at `1`, and is bumped every time the
controller observes a config change.

Generation resets when the controller process restarts. It is useful for
humans and for kernel event stamping, but it is not globally monotonic and is
not the fleet convergence key.

`config_hash` is the fleet convergence key. It has this shape:

```text
sha256:<lowercase-hex>
```

The hash is computed over the canonical `serde_json::to_vec` bytes of the
`CompiledConfig` value. Agents and operators should treat the hash, not the
generation, as the authoritative identity of the desired config.

Heartbeat payloads also carry `config_hash`. This field is additive in
`tapio-wire/v1`: older heartbeat JSON without it still deserializes and
validates with an empty hash. A controller-managed agent sets it to the hash
of the compiled config it has actually applied, not merely the config it last
fetched. Empty hash means the agent has not applied controller config yet.
In that unconfigured state, `config_version` is the string `"0"`.

When a controller-mode agent has not applied any compiled config, it reports
the degraded reason `unconfigured`. This is the first worked example of v1
additive evolution: new senders add `config_hash`, older payloads remain
accepted, and the protocol version does not change.

Enums carried in `tapio-wire/v1` payloads must include a receiver-side
catch-all variant. Adding an enum variant is sender-additive, but without a
catch-all it is receiver-breaking because older controllers reject the whole
payload during deserialization.

## Profile Input

In v0, profile documents enter the controller at startup. The controller may
parse an operator-trusted EvidenceProfile YAML file, validate it with
`tapio-profile`, compile it into `CompiledConfig`, and fail startup on any
parse or validation error.

The controller currently uses `serde_yaml 0.9` for this startup-only parser.
That crate is archived upstream, so it is intentionally contained to
`tapio-controller`, never linked into `tapio-agent` or `tapio-cli`, and
replaceable behind the same `Deserialize` boundary. YAML parsing is not part
of the profile core API.

## Config Caching

`GET /v1/agents/config` returns:

```text
ETag: "<config_hash>"
```

Agents send:

```text
If-None-Match: "<config_hash>"
```

When the tag matches the active config, the controller returns
`304 Not Modified` with no body. When it does not match, the controller returns
`200 OK` with the full `ConfigResponse`.

v0 uses exact single-tag matching only. It does not implement HTTP list
matching, weak validators, or `*` matching because the agent/controller path is
a closed protocol, not a general-purpose cache interface.

This is the v1 scaling story: many agents poll, and most receive cheap `304`
responses.

The `agent_id` and `node_name` query parameters are required for
`GET /v1/agents/config`, but v0 config is cluster-wide. The controller may
ignore those values for assignment until per-node or per-namespace config is
added later.

## Status Read Surface

`GET /v1/status` is a read-only controller snapshot. It reports controller
identity, start time, active config identity, event and batch counters,
registered agents, last heartbeat age, echoed heartbeat counters, observer
statuses, and per-agent event batch sequence state. It reports facts only:
ages and counters, not health, stale, or degraded verdicts. It has no query
parameters, no pagination, and inherits the v0 trust model with no auth until
the shared auth work lands.

## Error Envelope

Every controller-owned error response uses this JSON shape:

```json
{
  "error": {
    "code": "MISSING_FIELD",
    "message": "agent_id is required"
  }
}
```

`code` is SCREAMING_SNAKE and machine-matchable. `message` is human-readable.
Responses must not include stack traces, filesystem paths, Rust debug dumps, or
internal implementation details.

Status code policy:

| Status | Codes | Meaning |
| --- | --- | --- |
| `400` | `MISSING_FIELD`, `UNSUPPORTED_VERSION`, `INVALID_FIELD` | malformed request, unsupported wire version, or invalid wire field |
| `422` | `REASONING_FIELD` | semantically rejected event facts |
| `404` | `UNKNOWN_ENDPOINT` | no Tapio controller route matched |
| `500` | `INTERNAL_ERROR` | unexpected controller failure; body never carries internals |

## Explicit Refusals

These are policy, not omissions:

- no pagination in v0 because there are no list endpoints;
- no rate limiting in v0; the initial deployment model is in-cluster trust;
- no auth in v0; TLS and identity live at the mesh, proxy, or cluster boundary
  per `docs/agent-controller.md`;
- no content negotiation;
- JSON only;
- no profile CRUD API;
- no `POST /v1/profiles`;
- no push, watch, streaming, bidirectional RPC, or gRPC;
- no controller-to-agent calls;
- no multi-wire-version support in v0.
