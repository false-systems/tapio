#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="${OUT_DIR:-/tmp/tapio-lean}"
EBPF_ARCH="${EBPF_ARCH:-}"
AGENT_MAX_BYTES="${AGENT_MAX_BYTES:-1500000}"
CLI_MAX_BYTES="${CLI_MAX_BYTES:-900000}"

cd "$ROOT"
mkdir -p "$OUT_DIR"

section() {
  printf '\n==> %s\n' "$1"
}

bytes() {
  wc -c < "$1" | tr -d ' '
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
    container_monitor) printf '19000\n' ;;
    storage_monitor) printf '26000\n' ;;
    node_pmc_monitor) printf '16000\n' ;;
    *) printf '0\n' ;;
  esac
}

ebpf_map_count_budget() {
  case "$1" in
    network_monitor) printf '4\n' ;;
    container_monitor) printf '2\n' ;;
    storage_monitor) printf '4\n' ;;
    node_pmc_monitor) printf '5\n' ;;
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
cargo test --workspace

section "Release build"
cargo build --release -p tapio-agent -p tapio-cli

agent_bin="$ROOT/target/release/tapio-agent"
cli_bin="$ROOT/target/release/tapio"
agent_bytes="$(bytes "$agent_bin")"
cli_bytes="$(bytes "$cli_bin")"

printf 'tapio-agent: %s (%s bytes)\n' "$(human_bytes "$agent_bytes")" "$agent_bytes"
printf 'tapio:       %s (%s bytes)\n' "$(human_bytes "$cli_bytes")" "$cli_bytes"

if (( agent_bytes > AGENT_MAX_BYTES )); then
  printf 'tapio-agent exceeds AGENT_MAX_BYTES=%s\n' "$AGENT_MAX_BYTES" >&2
  exit 1
fi

if (( cli_bytes > CLI_MAX_BYTES )); then
  printf 'tapio exceeds CLI_MAX_BYTES=%s\n' "$CLI_MAX_BYTES" >&2
  exit 1
fi

section "Dependency tree"
cargo tree --workspace > "$OUT_DIR/cargo-tree.txt"
printf 'saved %s\n' "$OUT_DIR/cargo-tree.txt"
printf 'direct tapio-agent dependencies:\n'
cargo tree -p tapio-agent --depth 1
scripts/check-agent-deps.sh

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
  ' "$source" ebpf/headers/metrics.h)"
  entries_budget="$(ebpf_map_max_entries_budget "$prog")"
  printf '%s max_entries ceiling: %s (budget %s)\n' "$prog" "$max_entries" "$entries_budget"
  if (( max_entries > entries_budget )); then
    fail_budget "eBPF max_entries" "$prog" "$max_entries" "$entries_budget"
  fi
done

section "Lean verification complete"
