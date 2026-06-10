#!/usr/bin/env bash
#
# Enforce Tapio's crate dependency boundaries:
#   tapio-agent observes      — no inbound server, no cluster brain, no k8s client
#   tapio-wire  is tiny       — protocol structs only, no server/client frameworks
#   tapio-profile is pure     — validation/compilation never enters node hot paths
#   tapio-controller coordinates — owns the server/k8s deps
#
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

fail=0

# Per-dependency guidance so a violation explains where the code belongs.
guidance() {
  case "$1" in
    axum)        printf 'must not expose an inbound server. Move server behavior to tapio-controller.' ;;
    hyper)       printf 'must not embed an HTTP server/client stack. Keep the minimal sink client; move rich HTTP to tapio-controller.' ;;
    tonic)       printf 'must not speak gRPC. Coordination protocols belong in tapio-controller.' ;;
    reqwest)     printf 'must not pull a full HTTP client. Use the minimal sink client instead.' ;;
    kube)        printf 'must not run a Kubernetes client. Kubernetes enrichment belongs in tapio-controller.' ;;
    k8s-openapi) printf 'must not pull Kubernetes API types. They belong in tapio-controller.' ;;
    tapio-profile) printf 'must not validate or compile Evidence Profiles. It consumes compiled wire config only.' ;;
    *)           printf 'is a forbidden dependency for this crate.' ;;
  esac
}

# check_boundary <package> <space-separated forbidden crate names>
check_boundary() {
  local package="$1"
  shift
  local tree
  tree="$(cargo tree -p "$package")"

  local crate
  for crate in "$@"; do
    if grep -Eq "(^|[[:space:]])${crate}[[:space:]]+v" <<<"$tree"; then
      printf '%s dependency boundary violation:\n' "$package" >&2
      printf '  found forbidden dependency: %s\n' "$crate" >&2
      printf '  %s %s\n' "$package" "$(guidance "$crate")" >&2
      fail=1
    fi
  done
}

# tapio-agent: node observer. No server, no gRPC, no Kubernetes client, no profile logic.
check_boundary tapio-agent kube k8s-openapi axum hyper tonic reqwest tapio-profile

# tapio-cli: platform-independent local inspection. No profile validation/compilation path.
check_boundary tapio-cli tapio-profile

# tapio-wire: tiny protocol structs. No server/client frameworks at all.
check_boundary tapio-wire kube k8s-openapi axum hyper tonic reqwest

if (( fail != 0 )); then
  printf '\nDependency boundaries violated. See messages above.\n' >&2
  exit 1
fi

printf 'dependency boundary checks passed\n'
