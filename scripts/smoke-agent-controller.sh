#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="${OUT_DIR:-/tmp/tapio-smoke-controller}"
EBPF_ARCH="${EBPF_ARCH:-}"
PORT_A="${PORT_A:-$((40000 + ($$ % 10000)))}"
PORT_B1="${PORT_B1:-$((50000 + ($$ % 10000)))}"
PORT_B2="${PORT_B2:-$((51000 + ($$ % 10000)))}"
PORT_C="${PORT_C:-$((30000 + ($$ % 10000)))}"
PORT_D="${PORT_D:-$((20000 + ($$ % 10000)))}"
PORT_E="${PORT_E:-$((22000 + ($$ % 10000)))}"
CONTROLLER_PORT="${CONTROLLER_PORT:-$((21000 + ($$ % 10000)))}"
METRICS_PORT="${METRICS_PORT:-$((32000 + ($$ % 10000)))}"
CARGO_TARGET_DIR="${CARGO_TARGET_DIR:-$OUT_DIR/target}"
NODE_NAME="${NODE_NAME:-smoke-worker}"
AGENT_ID="${AGENT_ID:-node/${NODE_NAME}}"
export CARGO_TARGET_DIR

cd "$ROOT"

phase_passes=()

section() {
  printf '\n==> %s\n' "$1"
}

fail() {
  printf 'FAIL: %s\n' "$1" >&2
  dump_logs
  exit 1
}

pass_phase() {
  phase_passes+=("$1")
  printf 'PASS: %s\n' "$1"
}

dump_logs() {
  if [[ -d "${OUT_DIR:-}" ]]; then
    printf '\ncontroller stdout:\n' >&2
    cat "$OUT_DIR/log/controller.stdout" 2>/dev/null >&2 || true
    printf '\ncontroller stderr:\n' >&2
    cat "$OUT_DIR/log/controller.stderr" 2>/dev/null >&2 || true
    printf '\nagent stdout:\n' >&2
    cat "$OUT_DIR/log/agent.stdout" 2>/dev/null >&2 || true
    printf '\nagent stderr:\n' >&2
    cat "$OUT_DIR/log/agent.stderr" 2>/dev/null >&2 || true
  fi
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
  stop_agent
  stop_controller
}
trap cleanup EXIT

stop_agent() {
  if [[ -n "${agent_pid:-}" ]] && kill -0 "$agent_pid" 2>/dev/null; then
    sudo kill "$agent_pid" 2>/dev/null || true
    wait "$agent_pid" 2>/dev/null || true
  fi
  agent_pid=""
}

stop_controller() {
  if [[ -n "${controller_pid:-}" ]] && kill -0 "$controller_pid" 2>/dev/null; then
    kill "$controller_pid" 2>/dev/null || true
    wait "$controller_pid" 2>/dev/null || true
  fi
  controller_pid=""
}

case "$(uname -s)" in
  Linux) ;;
  Darwin)
    printf 'agent/controller smoke test requires Linux; on macOS run it inside a Lima VM:\n' >&2
    printf '  limactl shell <vm> -- bash scripts/smoke-agent-controller.sh\n' >&2
    exit 2
    ;;
  *)
    printf 'agent/controller smoke test requires Linux\n' >&2
    exit 2
    ;;
esac

for cmd in clang jq curl; do
  if ! command -v "$cmd" >/dev/null 2>&1; then
    printf '%s is required for agent/controller smoke test\n' "$cmd" >&2
    exit 2
  fi
done

if [[ ! -r /usr/include/bpf/bpf_helpers.h ]]; then
  printf '/usr/include/bpf/bpf_helpers.h is required for agent/controller smoke test\n' >&2
  exit 2
fi

section "Environment"
uname -a

rm -rf "$OUT_DIR"
mkdir -p "$OUT_DIR/ebpf" "$OUT_DIR/data" "$OUT_DIR/log"

cat >"$OUT_DIR/agent.toml" <<EOF
[metrics]
enabled = true
bind_address = "127.0.0.1"
port = ${METRICS_PORT}
EOF

section "Build"
cargo build --release -p tapio-agent -p tapio-controller

bpf_arch="$(detect_bpf_arch)"
for prog in network_monitor container_monitor storage_monitor node_pmc_monitor; do
  clang -O2 -g -target bpf "-D__TARGET_ARCH_${bpf_arch}" \
    -I ebpf/headers \
    -c "ebpf/${prog}.c" \
    -o "$OUT_DIR/ebpf/${prog}.o"
done

wait_for_http() {
  local port="$1"
  local path="$2"
  local name="$3"
  for _ in $(seq 1 100); do
    if curl -fsS "http://127.0.0.1:${port}${path}" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.1
  done
  fail "$name did not answer on port $port"
}

start_controller() {
  : >"$OUT_DIR/log/controller.stdout"
  : >"$OUT_DIR/log/controller.stderr"
  env RUST_LOG=tapio_controller=trace \
    "$CARGO_TARGET_DIR/release/tapio-controller" "127.0.0.1:${CONTROLLER_PORT}" \
    >"$OUT_DIR/log/controller.stdout" \
    2>"$OUT_DIR/log/controller.stderr" &
  controller_pid="$!"
  wait_for_http "$CONTROLLER_PORT" "/v1/status" "controller"
}

start_agent() {
  : >"$OUT_DIR/log/agent.stdout"
  : >"$OUT_DIR/log/agent.stderr"
  sudo env RUST_LOG=tapio_agent=debug TAPIO_NODE_NAME="$NODE_NAME" TAPIO_AGENT_ID="$AGENT_ID" \
    "$CARGO_TARGET_DIR/release/tapio-agent" \
    --config "$OUT_DIR/agent.toml" \
    --sink controller \
    --sink file \
    --controller-endpoint "http://127.0.0.1:${CONTROLLER_PORT}" \
    --heartbeat-interval 5 \
    --data-dir "$OUT_DIR/data" \
    --ebpf-dir "$OUT_DIR/ebpf" \
    >"$OUT_DIR/log/agent.stdout" \
    2>"$OUT_DIR/log/agent.stderr" &
  agent_pid="$!"

  for _ in $(seq 1 100); do
    if grep -q 'network observer running' "$OUT_DIR/log/agent.stderr"; then
      wait_for_http "$METRICS_PORT" "/metrics" "agent metrics"
      return 0
    fi
    if ! kill -0 "$agent_pid" 2>/dev/null; then
      fail "agent exited before network observer started"
    fi
    sleep 0.1
  done
  fail "network observer did not start"
}

status_json() {
  curl -fsS "http://127.0.0.1:${CONTROLLER_PORT}/v1/status"
}

metric_value() {
  local name="$1"
  local matcher="${2:-}"
  curl -fsS "http://127.0.0.1:${METRICS_PORT}/metrics" \
    | awk -v name="$name" -v matcher="$matcher" '
      $1 ~ "^" name {
        if (matcher == "" || index($0, matcher) > 0) {
          value = $NF
        }
      }
      END {
        if (value == "") { print 0 } else { print value }
      }
    '
}

accepted_total() {
  status_json | jq -r '.totals.accepted_events_total'
}

registered_at() {
  status_json | jq -r --arg agent "$AGENT_ID" '.agents[]? | select(.agent_id == $agent) | .registered_at_unix' | tail -n 1
}

sequence_field() {
  local field="$1"
  status_json | jq -r --arg agent "$AGENT_ID" ".agents[]? | select(.agent_id == \$agent) | .sequence.${field}" | tail -n 1
}

agent_present() {
  status_json | jq -e --arg agent "$AGENT_ID" '.agents[]? | select(.agent_id == $agent)' >/dev/null
}

trigger_connects() {
  local port="$1"
  for _ in $(seq 1 5); do
    timeout 1 bash -c ":</dev/tcp/127.0.0.1/${port}" >/dev/null 2>&1 || true
    sleep 0.1
  done
}

wait_for_file_occurrence() {
  local port="$1"
  for _ in $(seq 1 100); do
    if grep -R -E -q '"type": *"kernel.network\.' "$OUT_DIR/data" \
      && grep -R -E -q "\"dst_port\": *${port}" "$OUT_DIR/data"; then
      return 0
    fi
    sleep 0.1
  done
  fail "no network occurrence with dst_port=$port observed in file sink"
}

wait_for_accepted_delta() {
  local before="$1"
  for _ in $(seq 1 300); do
    local after
    after="$(accepted_total)"
    if (( after > before )); then
      return 0
    fi
    sleep 0.1
  done
  fail "controller accepted_events_total did not increase"
}

wait_for_trace_event_port() {
  local port="$1"
  for _ in $(seq 1 100); do
    if grep -q "\"dst_port\":${port}" "$OUT_DIR/log/controller.stdout" \
      || grep -q "\"dst_port\": ${port}" "$OUT_DIR/log/controller.stdout" \
      || grep -q "\\\\\"dst_port\\\\\":${port}" "$OUT_DIR/log/controller.stdout" \
      || grep -q "\\\\\"dst_port\\\\\": ${port}" "$OUT_DIR/log/controller.stdout"; then
      return 0
    fi
    sleep 0.1
  done
  fail "controller trace log did not include dst_port=$port"
}

wait_for_hello_round_trip() {
  for _ in $(seq 1 100); do
    if grep -q 'agent registered' "$OUT_DIR/log/controller.stdout" \
      && grep -q 'controller hello accepted' "$OUT_DIR/log/agent.stderr" \
      && agent_present; then
      return 0
    fi
    sleep 0.1
  done
  fail "controller did not register the agent"
}

wait_for_second_heartbeat() {
  local initial_count
  initial_count="$(grep -c 'agent heartbeat' "$OUT_DIR/log/controller.stdout" || true)"
  local last_count="$initial_count"
  local last_age=""
  for _ in $(seq 1 80); do
    local count
    count="$(grep -c 'agent heartbeat' "$OUT_DIR/log/controller.stdout" || true)"
    local age
    age="$(status_json | jq -r --arg agent "$AGENT_ID" '.agents[]? | select(.agent_id == $agent) | .last_heartbeat_age_seconds // empty' | tail -n 1)"
    last_count="$count"
    last_age="$age"
    if [[ -n "$age" ]] && (( count >= initial_count + 1 )) && (( age <= 5 )); then
      return 0
    fi
    sleep 0.5
  done
  fail "second heartbeat was not observed; initial_count=$initial_count last_count=$last_count last_age=${last_age:-empty}"
}

file_count() {
  find "$OUT_DIR/data" -type f -name '*.json' 2>/dev/null | wc -l
}

wait_metric_gt_zero() {
  local name="$1"
  local matcher="${2:-}"
  for _ in $(seq 1 120); do
    local value
    value="$(metric_value "$name" "$matcher")"
    if awk "BEGIN { exit !($value > 0) }"; then
      return 0
    fi
    sleep 0.5
  done
  fail "metric $name $matcher did not become > 0"
}

wait_for_new_registration_after() {
  local previous_registered_at="$1"
  for _ in $(seq 1 80); do
    local current
    current="$(registered_at || true)"
    if [[ -n "$current" && "$current" != "null" ]] && (( current > previous_registered_at )); then
      if grep -q 'agent registered' "$OUT_DIR/log/controller.stdout"; then
        return 0
      fi
    fi
    sleep 0.5
  done
  fail "agent did not re-register with restarted controller"
}

wait_for_sequence_gap() {
  for _ in $(seq 1 100); do
    local gaps
    gaps="$(sequence_field gaps_total)"
    if [[ -n "$gaps" && "$gaps" != "null" ]] && (( gaps > 0 )); then
      return 0
    fi
    sleep 0.5
  done
  fail "dropped batches did not surface as sequence gaps"
}

section "Start controller"
start_controller

section "Start agent"
start_agent

section "Phase A: happy path, content-assured"
wait_for_hello_round_trip
if [[ "$(sequence_field gaps_total)" != "0" ]]; then
  fail "initial sequence.gaps_total was not zero"
fi
before="$(accepted_total)"
trigger_connects "$PORT_A"
wait_for_file_occurrence "$PORT_A"
wait_for_accepted_delta "$before"
wait_for_trace_event_port "$PORT_A"
wait_for_second_heartbeat
pass_phase "A happy path"

section "Phase B1: controller pause, loss surfaces as sequence gap"
kill -STOP "$controller_pid"
trigger_connects "$PORT_B1"
wait_for_file_occurrence "$PORT_B1"
sleep 20
kill -CONT "$controller_pid"
before="$(accepted_total)"
trigger_connects "$PORT_B2"
wait_for_file_occurrence "$PORT_B2"
wait_for_accepted_delta "$before"
wait_for_sequence_gap
pass_phase "B1 loss surfaces as sequence gap"

section "Phase B2: controller outage"
before_files="$(file_count)"
previous_registered_at="$(registered_at)"
kill -9 "$controller_pid" 2>/dev/null || true
wait "$controller_pid" 2>/dev/null || true
controller_pid=""
trigger_connects "$PORT_C"
wait_for_file_occurrence "$PORT_C"
after_files="$(file_count)"
if (( after_files <= before_files )); then
  fail "file sink did not accumulate occurrences while controller was down"
fi
if ! kill -0 "$agent_pid" 2>/dev/null; then
  fail "agent exited during controller outage"
fi
wait_metric_gt_zero 'tapio_controller_send_failures_total'
wait_metric_gt_zero 'tapio_sink_drops_total' 'sink="controller",reason="send_failed"'
pass_phase "B2 controller outage"

section "Phase C: controller recovery"
start_controller
wait_for_new_registration_after "$previous_registered_at"
before="$(accepted_total)"
trigger_connects "$PORT_D"
wait_for_file_occurrence "$PORT_D"
wait_for_accepted_delta "$before"
regressions="$(sequence_field regressions_total)"
if [[ "$regressions" != "0" ]]; then
  fail "sequence.regressions_total after controller recovery was $regressions"
fi
pass_phase "C controller recovery"

section "Phase D: agent restart"
previous_registered_at="$(registered_at)"
stop_agent
sleep 1
start_agent
wait_for_new_registration_after "$previous_registered_at"
before="$(accepted_total)"
trigger_connects "$PORT_E"
wait_for_file_occurrence "$PORT_E"
wait_for_accepted_delta "$before"
regressions="$(sequence_field regressions_total)"
if [[ "$regressions" != "0" ]]; then
  fail "sequence.regressions_total after agent restart was $regressions"
fi
pass_phase "D agent restart"

printf '\nagent/controller smoke v2 passed phases:\n'
for phase in "${phase_passes[@]}"; do
  printf '  - %s\n' "$phase"
done
