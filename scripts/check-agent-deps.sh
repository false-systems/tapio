#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

deny='(^|[[:space:]])(kube|k8s-openapi|axum|hyper|tonic|reqwest)[[:space:]]+v'

if cargo tree -p tapio-agent | grep -E "$deny"; then
  printf 'tapio-agent dependency denylist matched. Keep controller/server/Kubernetes deps out of the agent.\n' >&2
  exit 1
fi

printf 'tapio-agent dependency denylist passed\n'
