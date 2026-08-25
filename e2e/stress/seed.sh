#!/usr/bin/env bash

set -euo pipefail

CLUSTER_NAME=${CLUSTER_NAME:-capsule-stress}
KUBE_CONTEXT=${KUBE_CONTEXT:-kwok-${CLUSTER_NAME}}
TENANTS=${TENANTS:-200}
NAMESPACES_PER_TENANT=${NAMESPACES_PER_TENANT:-3}
PREFIX=${PREFIX:-stress}
FIELD_MANAGER=${FIELD_MANAGER:-capsule-stress}

kubectl config use-context "${KUBE_CONTEXT}" >/dev/null

generate_tenants() {
  local tenant_number tenant_name

  for ((tenant_number = 1; tenant_number <= TENANTS; tenant_number++)); do
    printf -v tenant_name '%s-tenant-%04d' "${PREFIX}" "${tenant_number}"
    cat <<EOF
---
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: ${tenant_name}
  labels:
    projectcapsule.dev/stress: "true"
    projectcapsule.dev/stress-run: ${PREFIX}
spec:
  owners:
    - name: ${tenant_name}-owner
      kind: User
EOF
  done
}

generate_namespaces() {
  local tenant_number namespace_number tenant_name namespace_name

  for ((tenant_number = 1; tenant_number <= TENANTS; tenant_number++)); do
    printf -v tenant_name '%s-tenant-%04d' "${PREFIX}" "${tenant_number}"

    for ((namespace_number = 1; namespace_number <= NAMESPACES_PER_TENANT; namespace_number++)); do
      printf -v namespace_name '%s-ns-%02d' "${tenant_name}" "${namespace_number}"
      cat <<EOF
---
apiVersion: v1
kind: Namespace
metadata:
  name: ${namespace_name}
  labels:
    capsule.clastix.io/tenant: ${tenant_name}
    projectcapsule.dev/stress: "true"
    projectcapsule.dev/stress-run: ${PREFIX}
EOF
    done
  done
}

echo "Applying ${TENANTS} Tenants"
generate_tenants | kubectl apply \
  --server-side \
  --field-manager "${FIELD_MANAGER}" \
  --filename -

kubectl wait tenant \
  --selector "projectcapsule.dev/stress-run=${PREFIX}" \
  --for condition=Ready \
  --timeout 10m

echo "Applying $((TENANTS * NAMESPACES_PER_TENANT)) Namespaces"
generate_namespaces | kubectl apply \
  --server-side \
  --field-manager "${FIELD_MANAGER}" \
  --filename -

echo "Waiting for every Tenant to report ${NAMESPACES_PER_TENANT} namespaces"
deadline=$((SECONDS + 900))
while ((SECONDS < deadline)); do
  ready=$(kubectl get tenants \
    --selector "projectcapsule.dev/stress-run=${PREFIX}" \
    --output jsonpath="{range .items[?(@.status.size==${NAMESPACES_PER_TENANT})]}{.metadata.name}{'\n'}{end}" \
    | wc -l | tr -d ' ')

  if [[ "${ready}" == "${TENANTS}" ]]; then
    echo "Stress environment ready: ${TENANTS} Tenants, $((TENANTS * NAMESPACES_PER_TENANT)) Namespaces"
    exit 0
  fi

  echo "Tenants converged: ${ready}/${TENANTS}"
  sleep 5
done

echo "timed out waiting for Tenant namespace status convergence" >&2
exit 1
