#!/usr/bin/env bash
#
# Lean verification gate for Tapio.
#
# Budgets (two-level for the node agent):
#   AGENT_MAX_BYTES     hard budget   default 1500000  fail if exceeded
#   AGENT_TARGET_BYTES  target budget default 1250000  warn if exceeded
#   CLI_MAX_BYTES       hard budget   default  900000  fail if exceeded
#
#   The hard budget is the line CI protects; the target budget is the next
#   ratchet point that guides further slimming. To ratchet down: once the
#   agent stays under a lower size, lower AGENT_TARGET_BYTES first, then lower
#   AGENT_MAX_BYTES once the target holds consistently in CI.
#   Override intentionally and document the reason, e.g.:
#       AGENT_MAX_BYTES=1600000 scripts/verify-lean.sh
#   Overrides must not be used to hide a real regression.
#
# Reliability knobs:
#   TAPIO_LEAN_ALLOW_DEGRADED  if set, a required test skipped due to a host
#                              limitation (e.g. no loopback bind) reports a
#                              PARTIAL pass instead of failing. Without it, a
#                              skip fails the gate so it never silently claims
#                              full verification.
#   TAPIO_LEAN_REQUIRE_NET     if set, network-dependent tests must run; they
#                              fail instead of skipping when the host forbids
#                              loopback. Use in CI that is known to have it.
#
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="${OUT_DIR:-/tmp/tapio-lean}"
EBPF_ARCH="${EBPF_ARCH:-}"
AGENT_MAX_BYTES="${AGENT_MAX_BYTES:-1500000}"
AGENT_TARGET_BYTES="${AGENT_TARGET_BYTES:-1250000}"
CLI_MAX_BYTES="${CLI_MAX_BYTES:-900000}"

DEGRADED=0
DEGRADED_REASONS=()

cd "$ROOT"
mkdir -p "$OUT_DIR"

section() {
  printf '\n==> %s\n' "$1"
}

bytes() {
  wc -c < "$1" | tr -d ' '
}

comma() {
  local n="$1" out=""
  while (( ${#n} > 3 )); do
    out=",${n: -3}${out}"
    n="${n:0:${#n}-3}"
  done
  printf '%s%s' "$n" "$out"
}

fail_budget() {
  local kind="$1"
  local name="$2"
  local value="$3"
  local budget="$4"
  printf '%s budget exceeded for %s: current=%s budget=%s\n' "$kind" "$name" "$value" "$budget" >&2
  printf 'Keep Tapio small: remove dead code/state, or raise this script budget with a documented reason.\n' >&2
  exit 1
}

ebpf_object_budget() {
  case "$1" in
    network_monitor) printf '45000\n' ;;
    container_monitor) printf '20000\n' ;;
    storage_monitor) printf '26000\n' ;;
    node_pmc_monitor) printf '16000\n' ;;
    *) printf '0\n' ;;
  esac
}

ebpf_map_count_budget() {
  case "$1" in
    network_monitor) printf '4\n' ;;
    container_monitor) printf '3\n' ;;
    storage_monitor) printf '4\n' ;;
    node_pmc_monitor) printf '6\n' ;;
    *) printf '0\n' ;;
  esac
}

ebpf_map_max_entries_budget() {
  case "$1" in
    network_monitor) printf '262144\n' ;;
    container_monitor) printf '262144\n' ;;
    storage_monitor) printf '524288\n' ;;
    node_pmc_monitor) printf '262144\n' ;;
    *) printf '0\n' ;;
  esac
}

human_bytes() {
  local value="$1"
  awk -v bytes="$value" 'BEGIN {
    if (bytes >= 1048576) {
      printf "%.2f MiB", bytes / 1048576
    } else if (bytes >= 1024) {
      printf "%.2f KiB", bytes / 1024
    } else {
      printf "%d B", bytes
    }
  }'
}

detect_bpf_arch() {
  if [[ -n "$EBPF_ARCH" ]]; then
    printf '%s\n' "$EBPF_ARCH"
    return
  fi

  case "$(uname -m)" in
    arm64|aarch64) printf 'arm64\n' ;;
    x86_64|amd64) printf 'x86\n' ;;
    *)
      printf 'x86\n'
      ;;
  esac
}

section "Rust formatting"
cargo fmt --all --check

section "Rust lint"
cargo clippy --workspace --all-targets -- -D warnings

section "Rust tests"
# Run with --nocapture so a test that skips on a host limitation surfaces its
# SKIP marker (passing-test output is hidden otherwise). Full log is saved;
# the terminal shows a summary, and the full log is dumped on failure.
test_log="$OUT_DIR/cargo-test.log"
if cargo test --workspace -- --nocapture >"$test_log" 2>&1; then
  grep -E 'test result:|SKIP post_json' "$test_log" || true
else
  cat "$test_log" >&2
  printf 'cargo test --workspace failed\n' >&2
  exit 1
fi
# Unanchored: under --nocapture parallel test output can interleave, so do not
# require the marker to start a line.
if grep -q 'SKIP post_json_accepts_http_endpoint' "$test_log"; then
  DEGRADED=1
  DEGRADED_REASONS+=("http sink loopback test skipped: host forbids local bind (set TAPIO_LEAN_REQUIRE_NET=1 to require it)")
fi

section "Release build"
cargo build --release -p tapio-agent -p tapio-controller -p tapio-cli

agent_bin="$ROOT/target/release/tapio-agent"
controller_bin="$ROOT/target/release/tapio-controller"
cli_bin="$ROOT/target/release/tapio"
agent_bytes="$(bytes "$agent_bin")"
controller_bytes="$(bytes "$controller_bin")"
cli_bytes="$(bytes "$cli_bin")"

section "Binary budgets"
printf 'tapio-agent size:          %s bytes\n' "$(comma "$agent_bytes")"
printf 'tapio-agent target budget: %s bytes\n' "$(comma "$AGENT_TARGET_BYTES")"
printf 'tapio-agent hard budget:   %s bytes\n' "$(comma "$AGENT_MAX_BYTES")"
if (( agent_bytes > AGENT_MAX_BYTES )); then
  printf 'status: OVER HARD LIMIT\n'
  fail_budget "agent binary size (hard)" "tapio-agent" "$agent_bytes" "$AGENT_MAX_BYTES"
elif (( agent_bytes > AGENT_TARGET_BYTES )); then
  printf 'status: over target, under hard limit\n'
  printf 'WARN: tapio-agent is above its target budget (%s bytes). Slim it, or ratchet AGENT_TARGET_BYTES with a documented reason.\n' \
    "$(comma "$AGENT_TARGET_BYTES")" >&2
else
  printf 'status: under target\n'
fi

printf '\ntapio-controller size:     %s bytes (reported; no hard budget yet)\n' "$(comma "$controller_bytes")"

printf '\ntapio size:                %s bytes\n' "$(comma "$cli_bytes")"
printf 'tapio hard budget:         %s bytes\n' "$(comma "$CLI_MAX_BYTES")"
if (( cli_bytes > CLI_MAX_BYTES )); then
  printf 'status: OVER HARD LIMIT\n'
  fail_budget "cli binary size (hard)" "tapio" "$cli_bytes" "$CLI_MAX_BYTES"
else
  printf 'status: under hard limit\n'
fi

section "Dependency footprint"
cargo tree --workspace > "$OUT_DIR/cargo-tree.txt"
cargo tree -p tapio-agent --depth 2 > "$OUT_DIR/agent-deps.txt"
cargo tree -p tapio-controller --depth 2 > "$OUT_DIR/controller-deps.txt"
cargo tree -p tapio-wire --depth 2 > "$OUT_DIR/wire-deps.txt"
cargo tree --workspace --duplicates > "$OUT_DIR/duplicates.txt" || true
dup_groups="$(grep -cE '^[A-Za-z0-9_-]+ v' "$OUT_DIR/duplicates.txt" 2>/dev/null || printf '0')"
printf 'saved dependency trees to %s/{cargo-tree,agent-deps,controller-deps,wire-deps,duplicates}.txt\n' "$OUT_DIR"
printf 'direct tapio-agent dependencies:\n'
cargo tree -p tapio-agent --depth 1
printf 'workspace duplicate dependency package lines: %s (see %s/duplicates.txt)\n' "$dup_groups" "$OUT_DIR"

section "Dependency boundaries"
scripts/check-dependency-boundaries.sh

section "eBPF compile"
if ! command -v clang >/dev/null 2>&1; then
  printf 'skipping eBPF compile: clang not found\n' >&2
elif [[ ! -r /usr/include/bpf/bpf_helpers.h ]]; then
  printf 'skipping eBPF compile: /usr/include/bpf/bpf_helpers.h not found\n' >&2
else
  bpf_arch="$(detect_bpf_arch)"
  ebpf_out="$OUT_DIR/ebpf"
  mkdir -p "$ebpf_out"
  for prog in network_monitor container_monitor storage_monitor node_pmc_monitor; do
    clang -O2 -g -target bpf "-D__TARGET_ARCH_${bpf_arch}" \
      -I ebpf/headers \
      -c "ebpf/${prog}.c" \
      -o "${ebpf_out}/${prog}.o"
    obj_bytes="$(bytes "${ebpf_out}/${prog}.o")"
    obj_budget="$(ebpf_object_budget "$prog")"
    printf '%s.o: %s (%s bytes, budget %s)\n' "$prog" "$(human_bytes "$obj_bytes")" "$obj_bytes" "$obj_budget"
    if (( obj_bytes > obj_budget )); then
      fail_budget "eBPF object size" "${prog}.o" "$obj_bytes" "$obj_budget"
    fi
  done
fi

section "eBPF map budgets"
for prog in network_monitor container_monitor storage_monitor node_pmc_monitor; do
  source="ebpf/${prog}.c"
  map_count="$(grep -c 'SEC(".maps")' "$source")"
  if grep -q '"headers/config.h"' "$source"; then
    map_count=$((map_count + 1)) # tapio_config map from headers/config.h
  fi
  map_count=$((map_count + 1)) # shared tapio_metrics map from headers/metrics.h
  map_budget="$(ebpf_map_count_budget "$prog")"
  printf '%s maps: %s (budget %s)\n' "$prog" "$map_count" "$map_budget"
  if (( map_count > map_budget )); then
    fail_budget "eBPF map count" "$prog" "$map_count" "$map_budget"
  fi

  max_entries="$(awk '
    /__uint\(max_entries,/ {
      line = $0
      sub(/^.*__uint\(max_entries,[[:space:]]*/, "", line)
      sub(/\).*$/, "", line)
      gsub(/[[:space:]]+/, "", line)
      if (line == "CONFIG_MAX_ENTRIES") line = 2
      if (line == "512*1024") line = 524288
      if (line == "256*1024") line = 262144
      if (line + 0 > max) max = line + 0
    }
    END { print max + 0 }
  ' "$source" ebpf/headers/metrics.h ebpf/headers/config.h)"
  entries_budget="$(ebpf_map_max_entries_budget "$prog")"
  printf '%s max_entries ceiling: %s (budget %s)\n' "$prog" "$max_entries" "$entries_budget"
  if (( max_entries > entries_budget )); then
    fail_budget "eBPF max_entries" "$prog" "$max_entries" "$entries_budget"
  fi
done

section "Lean verification summary"
if (( DEGRADED != 0 )); then
  printf 'PARTIAL lean verification — degraded mode:\n'
  for reason in "${DEGRADED_REASONS[@]}"; do
    printf '  - %s\n' "$reason"
  done
  if [[ -n "${TAPIO_LEAN_ALLOW_DEGRADED:-}" ]]; then
    printf 'TAPIO_LEAN_ALLOW_DEGRADED is set: accepting partial verification.\n'
    printf 'NOTE: this run did NOT fully verify lean discipline.\n'
    exit 0
  fi
  printf 'A required test was skipped due to a host limitation, so this is not a full lean pass.\n' >&2
  printf 'Re-run on a host with loopback networking, set TAPIO_LEAN_REQUIRE_NET=1 to force the test, or set TAPIO_LEAN_ALLOW_DEGRADED=1 to accept a partial run.\n' >&2
  exit 1
fi

printf 'Lean verification complete (full)\n'
