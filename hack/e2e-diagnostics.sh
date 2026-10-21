#!/usr/bin/env bash

# Collects the Capsule controller state after a failed e2e run so a hang can be
# diagnosed from CI: pod status and events, current and previous container logs,
# goroutine and mutex profiles over pprof (the e2e install starts the manager
# with --enable-pprof), and the kubelet/containerd journal of the kind nodes.
#
# usage: [CLUSTER_NAME=capsule] [KUBECTL=kubectl] ./hack/e2e-diagnostics.sh [output dir]

set -uo pipefail

OUT="${1:-.e2e-diagnostics}"
CLUSTER_NAME="${CLUSTER_NAME:-capsule}"
KUBECTL="${KUBECTL:-kubectl}"
NAMESPACE=capsule-system
SELECTOR=app.kubernetes.io/name=capsule
PPROF_PORT=18082

mkdir -p "$OUT"
date -u +%FT%TZ > "$OUT/collected-at.txt"

$KUBECTL get nodes -o wide > "$OUT/nodes.txt" 2>&1
{
  $KUBECTL -n "$NAMESPACE" get pods -o wide
  echo
  $KUBECTL -n "$NAMESPACE" get endpointslices -o wide
} > "$OUT/pods.txt" 2>&1
$KUBECTL -n "$NAMESPACE" get events --sort-by=.lastTimestamp > "$OUT/events.txt" 2>&1
$KUBECTL -n "$NAMESPACE" describe pods -l "$SELECTOR" > "$OUT/describe.txt" 2>&1
$KUBECTL get --raw '/readyz?verbose' > "$OUT/apiserver-readyz.txt" 2>&1

for pod in $($KUBECTL -n "$NAMESPACE" get pods -l "$SELECTOR" -o name 2>/dev/null); do
  name="${pod#pod/}"
  $KUBECTL -n "$NAMESPACE" logs "$pod" --tail=5000 > "$OUT/$name.log" 2>&1
  if ! $KUBECTL -n "$NAMESPACE" logs "$pod" --previous --tail=5000 > "$OUT/$name.previous.log" 2>&1; then
    rm -f "$OUT/$name.previous.log"
  fi

  # A goroutine dump shows what every handler and reconciler is blocked on; it
  # works even when the process no longer answers webhooks or writes logs.
  $KUBECTL -n "$NAMESPACE" port-forward "$pod" "$PPROF_PORT:8082" > /dev/null 2>&1 &
  forward=$!
  sleep 2
  curl -sf -m 30 "http://127.0.0.1:$PPROF_PORT/debug/pprof/goroutine?debug=2" > "$OUT/$name.goroutines.txt" 2>&1 \
    || echo "pprof not reachable for $name" >> "$OUT/$name.goroutines.txt"
  kill "$forward" 2>/dev/null
  wait "$forward" 2>/dev/null
done

# kind nodes are containers on the host: capture the node side of the log pipeline.
for node in $(docker ps --filter "label=io.x-k8s.kind.cluster=$CLUSTER_NAME" --format '{{.Names}}' 2>/dev/null); do
  docker exec "$node" sh -c '
    uptime; echo; free -m; echo; df -h /var/log; echo
    ls -la --time-style=full-iso /var/log/pods/capsule-system_capsule-controller-manager-*/manager/ 2>/dev/null; echo
    top -b -n 1 | head -n 25
  ' > "$OUT/$node.node.txt" 2>&1
  docker exec "$node" journalctl -u kubelet --no-pager -n 300 > "$OUT/$node.kubelet.log" 2>&1
  docker exec "$node" journalctl -u containerd --no-pager -n 300 > "$OUT/$node.containerd.log" 2>&1
done

echo "=== Capsule pods ==="
cat "$OUT/pods.txt"
echo
echo "=== last events ==="
tail -n 30 "$OUT/events.txt"
for dump in "$OUT"/*.goroutines.txt; do
  [ -f "$dump" ] || continue
  echo
  echo "=== $(basename "$dump" .goroutines.txt): $(grep -c '^goroutine ' "$dump") goroutines, most common top frames ==="
  grep -A1 '^goroutine ' "$dump" | grep -vE '^(goroutine |--)' | sed 's/([^()]*)$//' | sort | uniq -c | sort -rn | head -n 15
done
echo
echo "diagnostics written to $OUT"
