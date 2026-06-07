#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="${OUT_DIR:-/tmp/tapio-smoke}"
EBPF_ARCH="${EBPF_ARCH:-}"
PORT="${PORT:-$((40000 + ($$ % 20000)))}"
CARGO_TARGET_DIR="${CARGO_TARGET_DIR:-$OUT_DIR/target}"
export CARGO_TARGET_DIR

cd "$ROOT"

section() {
  printf '\n==> %s\n' "$1"
}

detect_bpf_arch() {
  if [[ -n "$EBPF_ARCH" ]]; then
    printf '%s\n' "$EBPF_ARCH"
    return
  fi

  case "$(uname -m)" in
    arm64|aarch64) printf 'arm64\n' ;;
    x86_64|amd64) printf 'x86\n' ;;
    *) printf 'x86\n' ;;
  esac
}

cleanup() {
  if [[ -n "${agent_pid:-}" ]] && kill -0 "$agent_pid" 2>/dev/null; then
    sudo kill "$agent_pid" 2>/dev/null || true
    wait "$agent_pid" 2>/dev/null || true
  fi
}
trap cleanup EXIT

if [[ "$(uname -s)" != "Linux" ]]; then
  printf 'network eBPF smoke test requires Linux; run via Lima or Linux CI\n' >&2
  exit 2
fi

if ! command -v clang >/dev/null 2>&1; then
  printf 'clang is required for eBPF smoke test\n' >&2
  exit 2
fi

if [[ ! -r /usr/include/bpf/bpf_helpers.h ]]; then
  printf '/usr/include/bpf/bpf_helpers.h is required for eBPF smoke test\n' >&2
  exit 2
fi

section "Environment"
uname -a

rm -rf "$OUT_DIR"
mkdir -p "$OUT_DIR/ebpf" "$OUT_DIR/data" "$OUT_DIR/log"

section "Build"
cargo build --release -p tapio-agent

bpf_arch="$(detect_bpf_arch)"
for prog in network_monitor container_monitor storage_monitor node_pmc_monitor; do
  clang -O2 -g -target bpf "-D__TARGET_ARCH_${bpf_arch}" \
    -I ebpf/headers \
    -c "ebpf/${prog}.c" \
    -o "$OUT_DIR/ebpf/${prog}.o"
done

section "Start agent"
sudo env RUST_LOG=tapio_agent=info \
  "$CARGO_TARGET_DIR/release/tapio-agent" \
  --sink file \
  --data-dir "$OUT_DIR/data" \
  --ebpf-dir "$OUT_DIR/ebpf" \
  >"$OUT_DIR/log/agent.stdout" \
  2>"$OUT_DIR/log/agent.stderr" &
agent_pid="$!"

for _ in $(seq 1 100); do
  if grep -q 'network observer running' "$OUT_DIR/log/agent.stderr"; then
    break
  fi
  if ! kill -0 "$agent_pid" 2>/dev/null; then
    printf 'agent exited before network observer started\n' >&2
    cat "$OUT_DIR/log/agent.stderr" >&2
    exit 1
  fi
  sleep 0.1
done

if ! grep -q 'network observer running' "$OUT_DIR/log/agent.stderr"; then
  printf 'network observer did not start\n' >&2
  cat "$OUT_DIR/log/agent.stderr" >&2
  exit 1
fi

section "Trigger closed-port TCP connect"
for _ in $(seq 1 5); do
  timeout 1 bash -c ":</dev/tcp/127.0.0.1/${PORT}" >/dev/null 2>&1 || true
  sleep 0.1
done

section "Assert occurrence"
for _ in $(seq 1 100); do
  if grep -R -E -q '"type": *"kernel.network\.' "$OUT_DIR/data"; then
    if grep -R -E -q "\"dst_port\": *${PORT}" "$OUT_DIR/data"; then
      printf 'observed network occurrence for dst_port=%s\n' "$PORT"
      grep -R -E '"type":"kernel.network.|"dst_port":' "$OUT_DIR/data" | head -n 8
      exit 0
    fi
  fi
  sleep 0.1
done

printf 'no network occurrence with dst_port=%s observed\n' "$PORT" >&2
printf '\nagent stderr:\n' >&2
cat "$OUT_DIR/log/agent.stderr" >&2
printf '\noccurrences:\n' >&2
find "$OUT_DIR/data" -maxdepth 1 -type f | head -n 8 | while read -r file; do
  printf '%s\n' "$file" >&2
  cat "$file" >&2
done
exit 1
