#!/usr/bin/env bash

# Collects the Capsule controller state after a failed e2e run so a hang can be
# diagnosed from CI: pod status and events, the complete current and previous
# container logs plus the rotated log files from the nodes, the answer and
# latency of the readiness and liveness endpoints, a goroutine dump over pprof
# (the e2e install starts the manager with --enable-pprof), and the
# kubelet/containerd journal of the kind nodes.
#
# usage: [CLUSTER_NAME=capsule] [KUBECTL=kubectl] ./hack/e2e-diagnostics.sh [output dir]

set -uo pipefail

OUT="${1:-.e2e-diagnostics}"
CLUSTER_NAME="${CLUSTER_NAME:-capsule}"
KUBECTL="${KUBECTL:-kubectl}"
NAMESPACE=capsule-system
SELECTOR=app.kubernetes.io/name=capsule
PPROF_PORT=18082
PROBE_PORT=18080

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
  $KUBECTL -n "$NAMESPACE" logs "$pod" > "$OUT/$name.log" 2>&1
  if ! $KUBECTL -n "$NAMESPACE" logs "$pod" --previous > "$OUT/$name.previous.log" 2>&1; then
    rm -f "$OUT/$name.previous.log"
  fi

  $KUBECTL -n "$NAMESPACE" port-forward "$pod" "$PPROF_PORT:8082" "$PROBE_PORT:10080" > /dev/null 2>&1 &
  forward=$!
  sleep 2

  # The liveness check only pings the process; the readiness check also reads
  # the TLS Secret and webhook configurations through the API. Their latency
  # tells a stalled process apart from a stalled API client.
  for probe in healthz readyz; do
    echo "== /$probe?verbose =="
    curl -s -m 10 -w '\n(HTTP %{http_code} in %{time_total}s)\n' "http://127.0.0.1:$PROBE_PORT/$probe?verbose" 2>&1 \
      || echo "no answer from /$probe within 10s"
  done > "$OUT/$name.probes.txt"

  # A goroutine dump shows what every handler and reconciler is blocked on; it
  # works even when the process no longer answers webhooks or writes logs.
  curl -sf -m 30 "http://127.0.0.1:$PPROF_PORT/debug/pprof/goroutine?debug=2" > "$OUT/$name.goroutines.txt" 2>&1 \
    || echo "pprof not reachable for $name" >> "$OUT/$name.goroutines.txt"
  kill "$forward" 2>/dev/null
  wait "$forward" 2>/dev/null
done

# kind nodes are containers on the host: capture the node side of the log
# pipeline, including the rotated controller log files that kubectl logs no
# longer serves.
POD_LOG_GLOB='/var/log/pods/capsule-system_capsule-controller-manager-*'
nodes=$(docker ps --filter "label=io.x-k8s.kind.cluster=$CLUSTER_NAME" --format '{{.Names}}' 2>/dev/null)
for node in $nodes; do
  docker exec "$node" sh -c '
    uptime; echo; free -m; echo; df -h /var/log; echo
    systemctl show kubelet containerd -p Id -p ActiveState -p SubState -p NRestarts -p ExecMainStartTimestamp; echo
    ls -la --time-style=full-iso /var/log/pods/capsule-system_capsule-controller-manager-*/manager/ 2>/dev/null; echo
    top -b -n 1 | head -n 25
  ' > "$OUT/$node.node.txt" 2>&1
  # The whole journals: a runtime crash happens long before the suite gives up.
  docker exec "$node" journalctl -u kubelet --no-pager > "$OUT/$node.kubelet.log" 2>&1
  docker exec "$node" journalctl -u containerd --no-pager > "$OUT/$node.containerd.log" 2>&1
  if docker exec "$node" sh -c "ls -d $POD_LOG_GLOB" > /dev/null 2>&1; then
    docker exec "$node" sh -c 'cd /var/log/pods && tar -cz capsule-system_capsule-controller-manager-*' \
      > "$OUT/$node.pod-logs.tar.gz" 2> /dev/null
  fi
done

# Without kind nodes the cluster runs on this host (MicroShift in CI): same data from here.
# shellcheck disable=SC2086
if [ -z "$nodes" ] && ls -d $POD_LOG_GLOB > /dev/null 2>&1; then
  {
    uptime; echo; free -m; echo; df -h /var/log; echo
    systemctl show microshift crio -p Id -p ActiveState -p SubState -p NRestarts -p ExecMainStartTimestamp; echo
    ls -la --time-style=full-iso $POD_LOG_GLOB/manager/; echo
    top -b -n 1 | head -n 25
  } > "$OUT/host.node.txt" 2>&1
  journalctl -u microshift --no-pager > "$OUT/host.microshift.log" 2>&1
  (cd /var/log/pods && tar -cz capsule-system_capsule-controller-manager-*) > "$OUT/host.pod-logs.tar.gz" 2> /dev/null
fi

echo "=== Capsule pods ==="
cat "$OUT/pods.txt"
echo
echo "=== last events ==="
tail -n 30 "$OUT/events.txt"
for nodefile in "$OUT"/*.node.txt; do
  [ -f "$nodefile" ] || continue
  echo
  echo "=== $(basename "$nodefile" .node.txt): services ==="
  grep -E '^(Id|ActiveState|SubState|NRestarts|ExecMainStartTimestamp)=' "$nodefile"
done
for probes in "$OUT"/*.probes.txt; do
  [ -f "$probes" ] || continue
  echo
  echo "=== $(basename "$probes" .probes.txt): probes ==="
  cat "$probes"
done
for dump in "$OUT"/*.goroutines.txt; do
  [ -f "$dump" ] || continue
  echo
  echo "=== $(basename "$dump" .goroutines.txt): $(grep -c '^goroutine ' "$dump") goroutines, most common top frames ==="
  grep -A1 '^goroutine ' "$dump" | grep -vE '^(goroutine |--)' | sed 's/([^()]*)$//' | sort | uniq -c | sort -rn | head -n 15
done
echo
echo "diagnostics written to $OUT"
