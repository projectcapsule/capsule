#!/usr/bin/env bash
# Copyright 2020-2026 Project Capsule Authors
# SPDX-License-Identifier: Apache-2.0

# Read-only, best-effort diagnostics after an e2e failure. Do not fetch Secrets,
# kubeconfigs, or rendered target data. Bound calls so an unavailable API does
# not prevent the remaining observations from being collected.
set -u

capture() {
  printf '\n>>> kubectl %s\n' "$*"
  kubectl --request-timeout=10s "$@" 2>&1 || true
}

capture get pods -A -o wide
capture get events -n capsule-system --sort-by=.metadata.creationTimestamp
capture get endpointslices -n capsule-system
capture get namespaces -l env=e2e -o 'custom-columns=NAME:.metadata.name,DELETING:.metadata.deletionTimestamp,CONDITIONS:.status.conditions'
capture get resourcepermits -A -o 'custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name,PHASE:.status.phase,KEEP-UNTIL:.status.keepUntil,CONDITIONS:.status.conditions,ITEMS:.status.processedItems'
capture get tenantresources -A -o 'custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name,CONDITIONS:.status.conditions,ITEMS:.status.processedItems'
capture get globaltenantresources -o 'custom-columns=NAME:.metadata.name,CONDITIONS:.status.conditions,ITEMS:.status.processedItems'

pods=$(kubectl --request-timeout=10s get pods -n capsule-system -l app.kubernetes.io/name=capsule -o name 2>/dev/null) || pods=""
for pod in $pods; do
  capture get -n capsule-system "$pod" -o 'custom-columns=NAME:.metadata.name,CONDITIONS:.status.conditions,CONTAINERS:.status.containerStatuses'
  capture logs -n capsule-system "$pod" --all-containers --timestamps --tail=300 --limit-bytes=150000
  capture logs -n capsule-system "$pod" --all-containers --previous --timestamps --tail=300 --limit-bytes=150000
done
