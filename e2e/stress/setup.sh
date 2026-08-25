#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
STRESS_DIR="${ROOT_DIR}/e2e/stress"
CLUSTER_NAME=${CLUSTER_NAME:-capsule-stress}
KIND_CLUSTER_NAME=${KIND_CLUSTER_NAME:-kwok-${CLUSTER_NAME}}
KUBE_CONTEXT=${KUBE_CONTEXT:-kwok-${CLUSTER_NAME}}
KUBERNETES_VERSION=${KUBERNETES_VERSION:-v1.35.0}
KWOK_NODES=${KWOK_NODES:-10}
CAPSULE_IMAGE=${CAPSULE_IMAGE:-capsule.local/projectcapsule/capsule}
CAPSULE_TAG=${CAPSULE_TAG:-stress}
CAPSULE_REGISTRY=${CAPSULE_IMAGE%%/*}
CAPSULE_REPOSITORY=${CAPSULE_IMAGE#*/}

for command in kwokctl kubectl kind helm docker; do
  if ! command -v "${command}" >/dev/null 2>&1; then
    echo "required command not found: ${command}" >&2
    exit 1
  fi
done

if ! kwokctl get clusters 2>/dev/null | grep -q "${CLUSTER_NAME}"; then
  kwokctl --config "${STRESS_DIR}/kwok.yaml" create cluster \
    --name "${CLUSTER_NAME}" \
    --runtime kind \
    --disable-qps-limits \
    --quiet-pull \
    --kind-node-image "kindest/node:${KUBERNETES_VERSION}" \
    --wait 2m \
    --timeout 5m
fi

kubectl config use-context "${KUBE_CONTEXT}" >/dev/null

make -C "${ROOT_DIR}" ko-build-capsule \
  CAPSULE_IMG="${CAPSULE_IMAGE}" \
  VERSION="${CAPSULE_TAG}"

kind load docker-image \
  "${CAPSULE_IMAGE}:${CAPSULE_TAG}" \
  --name "${KIND_CLUSTER_NAME}"

kwokctl --config "${STRESS_DIR}/kwok.yaml" scale node \
  --name "${CLUSTER_NAME}" \
  --replicas "${KWOK_NODES}"

kubectl label clusterrole admin \
  projectcapsule.dev/aggregate-to-controller=true \
  --overwrite

helm upgrade --install capsule "${ROOT_DIR}/charts/capsule" \
  --namespace capsule-system \
  --create-namespace \
  --dependency-update \
  --no-hooks \
  --values "${STRESS_DIR}/values.yaml" \
  --set-string "manager.image.registry=${CAPSULE_REGISTRY}" \
  --set-string "manager.image.repository=${CAPSULE_REPOSITORY}" \
  --set-string "manager.image.tag=${CAPSULE_TAG}" \
  --wait \
  --timeout 5m

kubectl rollout status deployment/capsule-controller-manager \
  --namespace capsule-system \
  --timeout 3m

echo "KWOK stress cluster is ready on context ${KUBE_CONTEXT}"
echo "Run ${STRESS_DIR}/seed.sh to create the Tenant workload"
