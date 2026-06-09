# Agent -> eBPF Config ABI

CO-RE is for kernel structs, not Tapio config.

CO-RE relocates eBPF reads of kernel data structures against the running
kernel's BTF. Tapio runtime profile config is not a kernel struct. It is
Tapio's own private ABI between `tapio-agent`, a BPF map value, and Tapio eBPF
programs.

That ABI must be fixed-layout, versioned, bounded, and auditable.

## Boundary

The intended chain is:

```text
EvidenceProfile YAML
  -> tapio-profile validates and compiles
  -> CompiledConfig
  -> tapio-controller serves generation/hash
  -> tapio-agent receives compiled primitive config
  -> tapio-agent writes identical tapio_config bytes into observer BPF ARRAY[0] carriers
  -> eBPF programs read primitive flags and thresholds
  -> emitted events include the config generation that judged them
```

The agent applies primitive config. eBPF reads flags and thresholds.

eBPF does not evaluate profiles, execute rules, parse YAML, know about Vartio,
know about Kubernetes labels, or perform root-cause analysis. Tapio keeps
intelligence out of the kernel and bloat out of the node agent.

## One ABI, N Carriers

The permanent law is one ABI:

- one shared `tapio_config` header;
- one fixed layout;
- one Rust mirror;
- one drift-detection point;
- one ABI version;
- one generation field.

The v0 carrier strategy is N carriers:

- each separately loaded observer object owns one single-entry
  `BPF_MAP_TYPE_ARRAY`;
- every carrier has `max_entries = 1`;
- every carrier value type is the shared `struct tapio_config`;
- `tapio-agent` writes identical bytes to index `0` in every carrier;
- each eBPF program reads its own object's index `0`.

One ABI is essential and permanent. One carrier is an optimization, deferred.

Do not scatter individual knobs across many maps in v0. Do not create
per-observer config layouts in v0.

The rationale is simple:

- one layout contract;
- one place to inspect bytes per observer object;
- one generation to stamp into events;
- smaller conceptual config surface;
- simpler object and map budget story.

Momentary cross-observer generation skew during fan-out is accepted and benign.
If `tapio-agent` updates one observer's carrier before another, the emitted
events still carry the generation that judged them. Skew is observable, never
silent.

Consolidation to a single pinned shared bpffs map is deferred until atomic
cross-observer generation flips matter.

The zero value of every config field must be the inert value.

All-zeros, which is the kernel's initial array-map state, means no observer
emits: `abi_version = 0` fails the version guard, `flags = 0` disables
everything, and all thresholds/counts are inert. No flag bit may ever mean
"set to disable". This is also the cold-start behavior: the agent loads
objects, maps are zeroed, and nothing emits until the first compiled config
lands.

## Config Layout

The v0 config layout is fixed-width and versioned. Field names should match the
currently implemented observer knobs when the ABI is implemented.

Conceptual C layout:

```c
#define TAPIO_CONFIG_ABI_VERSION 1

#define TAPIO_F_NETWORK   (1ULL << 0)
#define TAPIO_F_STORAGE   (1ULL << 1)
#define TAPIO_F_CONTAINER (1ULL << 2)
#define TAPIO_F_NODE_PMC  (1ULL << 3)

struct tapio_config {
    __u32 abi_version;
    __u32 generation;

    __u64 flags;

    __u64 slow_io_threshold_ns;
    __u64 conn_refused_window_ns;
    __u32 conn_refused_min_count;
    __u32 rtt_spike_multiplier;
    __u32 rtt_min_baseline_samples;

    __u32 ignore_exit_count;
    __s32 ignore_exit_codes[16];

    __u32 _pad;
};
```

The v0 field set tracks the intended primitive knobs:

- storage slow I/O threshold;
- network connection-refused window and minimum count;
- network RTT spike multiplier;
- network baseline sample count if it becomes profile-controlled;
- bounded container exit-code ignore list;
- observer enablement flags.

Rules:

- fixed-width integers only;
- no pointers;
- no strings;
- no heap data;
- no variable-length arrays;
- no C enum types;
- use integer constants for actions and modes;
- use explicit padding where needed;
- bounded arrays only;
- count fields must be clamped before use;
- Rust mirrors must use `#[repr(C)]`;
- eBPF structs must use C layout;
- Rust and C sizes must match;
- important offsets should be asserted where project patterns support it.

`abi_version` must be first. `generation` must be early and cheap to read.
`flags` is a bitset. eBPF must not use string-based observer names.

## ABI Versioning

`TAPIO_CONFIG_ABI_VERSION` starts at `1`.

Bump it on any layout change that changes size, field order, field meaning, or
interpretation. `tapio-agent` and Tapio eBPF programs must agree on the ABI
version. Do not implement multi-version support in v0.

Required behavior:

- maps start as kernel-zeroed inert state;
- agent writes a non-zero compiled config with
  `abi_version = TAPIO_CONFIG_ABI_VERSION`;
- eBPF copies `tapio_config` from map index `0` to stack once per invocation
  before making decisions;
- eBPF reads the stack copy and checks `abi_version`;
- eBPF ignores config and returns without emitting events when the version is
  missing or mismatched;
- agent load/init fails loudly if it cannot initialize or verify the config ABI;
- observer health is not declared until ABI initialization succeeds.

A mismatched agent/program pair must fail visibly, not misread offsets silently.

## Generation Stamping

Every emitted event should eventually carry the config generation that judged
it.

Purpose:

- fleet convergence becomes observable in the event stream;
- operators can know which compiled envelope produced an event;
- Vartio can later reason about correspondence: "this node emitted event E
  while executing envelope generation G / hash H."

Rules:

- eBPF reads `generation` from `tapio_config`;
- eBPF stamps generation into event structs where event layout supports it;
- userspace preserves generation in emitted occurrence/event JSON;
- generation is `u32` in kernel config for compactness unless the codebase
  already moves to `u64`;
- content hash does not enter eBPF in v0;
- content hash remains controller/agent/userspace metadata.

The ant is small, but it wears a version number.

If event structs cannot be changed in the same PR as the config carrier, record
generation stamping as a required follow-up. Do not half-stamp only some events
without documenting consistency.

Preferred eventual event field:

```c
__u32 config_generation;
```

Place it early enough to be cheap and stable, preserve alignment, update
explicit padding, and avoid accidental struct-size surprises.

## Update Semantics

A userspace map update may race with an eBPF program reading fields.

For v0, accepted semantics are:

- config tearing is tolerated only for independent primitive thresholds and
  flags;
- no event may become unsafe because of tearing;
- no out-of-bounds read is allowed;
- no profile logic depends on complex cross-field consistency inside eBPF;
- eBPF programs read only the fields they need near handler entry;
- count+array pairs require special handling.

For fields such as:

```c
__u32 ignore_exit_count;
__s32 ignore_exit_codes[16];
```

the rules are:

- agent writes array values first and `ignore_exit_count` last;
- eBPF clamps `ignore_exit_count` to `16` before reading;
- eBPF never reads beyond the fixed array bound;
- a partially updated config may cause one event to be judged by a previous or
  next bounded set, but must not cause undefined behavior or out-of-bounds
  access.

Each eBPF program copies the whole `tapio_config` value to stack once near
handler entry, then reads only the stack copy for decisions in that invocation.
This is the v0 tearing mitigation: each event is judged by one
consistent-enough snapshot. The struct is intentionally small enough to fit
within verifier stack limits alongside existing handler locals.

Do not implement double-buffering in v0 unless current code already needs it.
If config fields become more interdependent later, move to a two-slot array plus
active-index map:

- write inactive slot;
- flip active index;
- readers use active slot.

That is deferred until measured or needed.

## Runtime Config vs `.rodata`

Runtime profile values must go through the config map, not `.rodata`.

Reasons:

- `.rodata` is load-time/frozen;
- changing `.rodata` means reload/reattach;
- profile changes should roll out without eBPF reload;
- reload-free config rollout is worth a few cheap flag and threshold reads.

Allowed `.rodata` use:

- build-time constants;
- structural constants that do not change per profile;
- constants that would require a new agent build anyway.

Not allowed in `.rodata`:

- observer enabled/disabled flags;
- thresholds;
- windows;
- sample rates;
- bounded lists;
- profile generation.

Runtime profile changes update maps, not programs.

## Fresh Boot and Stale Config

A fresh agent with no compiled config starts with observers disabled. The agent
must not bake a default Evidence Profile into the binary.

The BPF config carriers start in the kernel's zeroed inert state until real
compiled config is applied:

```c
abi_version = 0
generation = 0
flags = 0
thresholds = 0
counts = 0
```

With `abi_version = 0`, observers fail the version guard. With
`abi_version = TAPIO_CONFIG_ABI_VERSION` and `flags = 0`, observers return
early.

If last-known-good compiled config exists later, the agent may start from it,
but it must report cached/stale state and the cached generation/hash. It must
not silently pretend it is current.

## Event Safety

Config generation stamping must not weaken existing event safety rules:

- ring-buffer events must be zero-initialized before writes;
- fixed struct size/layout must be asserted;
- userspace must bounds-check before reading;
- packed reads must remain safe;
- malformed or truncated records must be counted/logged;
- reasoning fields remain absent;
- ambiguous evidence must be counted rather than emitted misleadingly.

If event structs are extended with `config_generation`, update Rust parser
structs, size asserts, offset asserts where present, tests, docs, and smoke
tests that assert event shape.

Do not casually change event schema without tests.

## Diagnostics

No observer should silently run under a mismatched config ABI.

Agent-side initialization failures must be clear. Runtime config/ABI mismatches
should be visible, either through a focused metric such as:

```text
tapio_config_errors_total{observer="network",reason="abi_mismatch"}
```

or through an existing project metric if that remains cleaner. Do not add metric
families casually.

## Vartio Relationship

Compiled Tapio config is a future Vartio-compatible evidence envelope.

Tapio owns the signal schema:

- which observers exist;
- which knobs exist;
- which thresholds are meaningful;
- which ranges are valid;
- which primitive fields are safe to hand to eBPF.

Vartio owns broader envelope/control-plane semantics later:

- identity;
- ownership;
- assignment;
- distribution;
- correspondence;
- audit;
- which actor caused this node to execute this envelope.

The eBPF config ABI carries only the tiny primitive subset required by the
kernel observer.

Do not put Vartio concepts into eBPF. Do not put actor identity into eBPF in
v0. Do not put content hash strings into eBPF in v0. The generation field is
enough for v0 kernel stamping.

## Explicit Refusals

The agent-to-kernel config ABI does not include:

- YAML parsing in `tapio-agent`;
- profile evaluation in `tapio-agent`;
- profile evaluation in eBPF;
- a DSL;
- a rules engine;
- a policy language;
- arbitrary expressions;
- Kubernetes logic in `tapio-agent`;
- Vartio logic in eBPF;
- content hash strings in eBPF;
- observer names as strings in eBPF;
- dynamic allocation in eBPF hot paths;
- `.rodata` runtime profile values.

## Implementation Checklist

When PR 2 implements this ABI:

1. Define `TAPIO_CONFIG_ABI_VERSION`.
2. Define `struct tapio_config` in the shared eBPF/user ABI location.
3. Add one single-entry BPF array carrier map per observer object.
4. Leave cold-start maps zeroed and inert until compiled config is available.
5. Add a Rust `#[repr(C)]` mirror type.
6. Add Rust size and important offset asserts.
7. Add C `_Static_assert` checks.
8. Add a tiny eBPF helper to get config index `0`.
9. Update observer handlers to read config near entry, check ABI, check flags,
   copy the config to stack once, return early if disabled, and clamp counts.
10. Stamp `generation` into emitted events if event schemas can safely change;
    otherwise document the follow-up.
11. Update userspace parsers if event structs change.
12. Update tests.
13. Update lean object/map budget checks intentionally.
14. Report map count, eBPF object-size, and `tapio-agent` binary impacts.

Adding config carriers changes map count for observers that did not already
have config maps. That increase is expected and required for runtime config.
No other map growth should occur.
