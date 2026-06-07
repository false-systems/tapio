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
  done
  ls -lh "$ebpf_out"
fi

section "Lean verification complete"
