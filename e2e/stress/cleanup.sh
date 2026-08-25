#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
CLUSTER_NAME=${CLUSTER_NAME:-capsule-stress}
PREFIX=${PREFIX:-stress}

if [[ "${1:-}" == "--workload-only" ]]; then
  kubectl delete namespaces \
    --selector "projectcapsule.dev/stress-run=${PREFIX}" \
    --ignore-not-found \
    --wait
  kubectl delete tenants \
    --selector "projectcapsule.dev/stress-run=${PREFIX}" \
    --ignore-not-found \
    --wait
  exit 0
fi

kwokctl --config "${ROOT_DIR}/e2e/stress/kwok.yaml" delete cluster \
  --name "${CLUSTER_NAME}" \
  --force
