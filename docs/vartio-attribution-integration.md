# Tapio in the Vartio Attribution Architecture

**Status:** Integration proposal. It changes no current Tapio behavior.

**Canonical attribution meaning:** Vartio's
`docs/vartio/architecture-proposal.md`. This document defines only Tapio's
participation and non-responsibilities.

## 1. Decision

Tapio remains the opinionated, selective anomaly-evidence product.

It is not redefined as Vartio's continuous process-identity layer. Jälki is the
first implementation base for continuous RuntimeSubject lifecycle and interval
receipts because that path requires substantially less architectural change.

Tapio may later share privileged kernel machinery inside `false-agent`, but
shared machinery does not merge Tapio, Jälki, or Ruuma semantics.

## 2. Current role

Tapio currently observes selected Linux/Kubernetes anomalies:

```text
network connection failures and degradation
OOM kills and abnormal exits
storage errors and latency
CPU stalls and memory pressure
```

It filters and classifies near the node, then emits named `kernel.*`
occurrences. It deliberately does not emit every syscall or own downstream
causal interpretation.

That remains useful to Vartio: an anomaly occurrence can support or qualify an
Operational Chain without becoming Actor attribution by itself.

## 3. Proposed data path

Standalone deployment remains valid:

```text
Tapio agent -> existing Tapio sinks
```

The proposed Vartio path is:

```text
Tapio capture/classification
  -> neutral named anomaly occurrence
  -> Vartio source ingress
  -> Vartio admission and relationship claims
  -> Operational Chain / ActivityReconstruction
  -> Ahti
```

Tapio must not write Vartio conclusions or bypass the Vartio append boundary.

## 4. Evidence contract

For each admitted anomaly Tapio should preserve, when available:

```text
occurrence type and schema version
observed event time and clock basis
node identity and boot ID
collector identity, instance, and failure domain
probe/observer version
configuration generation and digest
source hook/mechanism
immutable Kubernetes UIDs
capture-loss state relevant to the observation
```

When a shared RuntimeSubject contract is available, Tapio may attach:

```text
runtime_subject_id
RuntimeSubjectTupleV1
binding method/version
```

That is a reference to a factual process identity. It does not assert an Actor,
Execution, Principal, authority path, or causal chain.

If an anomaly cannot be tied to one RuntimeSubject, Tapio should preserve the
actual scope (node, cgroup, container, Pod, or candidate set) rather than choose
a process.

## 5. Relationship to continuous coverage

Tapio's current anomaly stream is not complete process or network activity
coverage. The absence of a Tapio anomaly means only that no matching anomaly
was emitted under the active observer configuration and capture state.

Tapio must not support statements such as:

```text
the process made no network requests
the process opened no files
nothing abnormal happened
```

unless a future versioned Tapio surface explicitly defines every hook,
threshold, scope, loss rule, and permitted negative statement needed for that
claim.

If Tapio later emits interval receipts, they must identify Tapio surface
versions and configuration. A receipt for anomaly classification is not a
receipt for Jälki process lifecycle, even when both lanes run in one binary.

## 6. false-agent packaging

The eventual packaging may look like:

```text
false-agent
  shared BTF/CO-RE and eBPF loading
  shared ring/event transport
  shared node and Kubernetes enrichment
  shared health/loss plumbing

  Jälki continuous + dynamic evidence lane
  Tapio selective anomaly lane
  Ruuma bounded-capture lane
```

Tapio retains:

- its named anomaly vocabulary;
- Evidence Profile validation/compilation;
- selective filtering and classification;
- standalone operation and existing sinks;
- its agent/controller product boundary.

Shared infrastructure may remove duplicate privileged deployment work. It must
not turn Tapio configuration into Vartio policy or make one lane's receipt prove
another lane's coverage.

## 7. Minimal integration work

No Tapio rewrite is prerequisite for the first Vartio vertical.

When a concrete Vartio question needs Tapio evidence, the smallest useful slice
is:

1. define a native Vartio ingress adapter or reuse a proven shared transport;
2. carry stable node/boot/collector identity;
3. attach RuntimeSubject identity only where the event actually resolves to one;
4. preserve observer configuration and loss facts;
5. add fixtures proving Vartio admits the occurrence without strengthening it;
6. add forbidden-claim tests.

Do not add process lifecycle, generic probe APIs, Actor concepts, authority
logic, or Operational Chains to Tapio merely for architectural symmetry.

## 8. Acceptance cases

The integration should demonstrate:

| Case | Required result | Forbidden result |
|---|---|---|
| OOM kill tied to one RuntimeSubject | factual anomaly linked to that RuntimeSubject | Actor performed an action solely because the process was killed |
| node-wide CPU stall | node-scoped occurrence | invented process or Actor owner |
| missing Pod enrichment | admit node/process evidence with an unresolved placement gap | discard the occurrence or choose a Pod by name |
| threshold configuration changes mid-interval | split/qualify applicable observation state | one continuous receipt under the old configuration |
| shared false-agent collector | preserve common failure-domain identity | count Jälki and Tapio lanes as independent corroboration |
| no anomaly emitted | no matching anomaly observation | broad claim that nothing abnormal happened |

## 9. Boundary

Tapio's role in this architecture is:

> Emit selected, named runtime anomaly facts with enough provenance for Vartio
> to use them honestly, while remaining independent of Vartio attribution and
> continuous Jälki coverage semantics.
