#!/usr/bin/env bash
set -euo pipefail

CLUSTER_NAME="${1:-capsule}"
NAMESPACE="${2:-capsule-system}"
SA_NAME="${3:-capsule}"
TARGET="${4:-kubeconfig-${SA_NAME}.yaml}"

SECRET_NAME="${SA_NAME}-static-token"

echo "👉 Using cluster: $CLUSTER_NAME"
echo "👉 Namespace: $NAMESPACE"
echo "👉 ServiceAccount: $SA_NAME"
echo "👉 Target: $TARGET"

echo "📄 Exporting kubeconfig..."
TMP_KUBECONFIG=$(mktemp)
kind get kubeconfig --name "$CLUSTER_NAME" > "$TMP_KUBECONFIG"

echo "🔐 Creating static token secret..."

kubectl -n "$NAMESPACE" apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: ${SECRET_NAME}
  annotations:
    kubernetes.io/service-account.name: ${SA_NAME}
type: kubernetes.io/service-account-token
EOF

echo "⏳ Waiting for token to populate..."
sleep 2

TOKEN=$(kubectl -n "$NAMESPACE" get secret "$SECRET_NAME" \
  -o jsonpath='{.data.token}' | base64 -d)

SERVER=$(kubectl config view --raw --kubeconfig "$TMP_KUBECONFIG" \
  -o jsonpath='{.clusters[0].cluster.server}')

CA_DATA=$(kubectl config view --raw --kubeconfig "$TMP_KUBECONFIG" \
  -o jsonpath='{.clusters[0].cluster.certificate-authority-data}')

mkdir -p "$(dirname "$TARGET")"

cat > "$TARGET" <<EOF
apiVersion: v1
kind: Config
clusters:
- name: kind
  cluster:
    certificate-authority-data: ${CA_DATA}
    server: ${SERVER}
contexts:
- name: ${SA_NAME}-context
  context:
    cluster: kind
    namespace: ${NAMESPACE}
    user: ${SA_NAME}
current-context: ${SA_NAME}-context
users:
- name: ${SA_NAME}
  user:
    token: ${TOKEN}
EOF

rm -f "$TMP_KUBECONFIG"

echo "✅ Done!"
echo "👉 SA kubeconfig written to: $TARGET"
echo
echo "export SERVICE_ACCOUNT=$SA_NAME"
echo "export NAMESPACE=$NAMESPACE"
echo "export KUBECONFIG=$TARGET"
