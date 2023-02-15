#!/usr/bin/env bash

# This script test capsule with kind
# Good to use it before pull request

USER=alice
TENANT=oil
GROUP=capsule.clastix.io
KIND_CLUSTER_NAME=capsule-local-test

function error_action() {
    cleanup_action
    exit 1
}

function cleanup_action() {
    kind delete cluster --name=${KIND_CLUSTER_NAME}
    rm -f ./tenant-test.yaml
    rm -f ${USER}-${TENANT}.crt
    rm -f ${USER}-${TENANT}.key
    rm -f ${USER}-${TENANT}.kubeconfig
}

function check_command() {
    local command=$1

    if ! command -v $command &> /dev/null; then
        echo "Error: ${command} not found"
        exit 1
    fi
}

check_command kind
check_command kubectl

### Prepare Kind cluster

echo `date`": INFO: Create Kind Cluster"
error_create_kind=$(kind create cluster --name=${KIND_CLUSTER_NAME} 2>&1)
if [ $? -ne 0 ]; then
    echo `date`": $error_create_kind"
    exit 1
fi

echo `date`": INFO: Wait then Kind cluster be ready. Wait only 30 seconds"
counter=0
while true
do
    if [ $counter == 30 ]; then 
        echo `date`": ERROR: Kind cluster not ready for too long"
        error_action
    fi

    kubectl get nodes | grep " Ready " &>/dev/null
    if [ $? == 0 ]; then 
        break
    fi

    ((counter++))
    sleep 1
done

echo `date`": INFO: Kind cluster ready"

### Install helm capsule to Kind

echo `date`": INFO: Install helm capsule"
error_install_helm=$(helm install capsule ./charts/capsule/ -n capsule-system --create-namespace 2>&1)
if [ $? -ne 0 ]; then
    echo `date`": $error_install_helm"
    exit 1
fi

echo `date`": INFO: Wait then capsule POD be ready. Wait only 30 seconds"
counter=0
while true
do
    if [ $counter == 30 ]; then 
        echo `date`": ERROR: Kind cluster not ready for too long"
        error_action
    fi

    kubectl get pod -n capsule-system | grep " Running " &>/dev/null
    if [ $? == 0 ]; then 
        break
    fi

    ((counter++))
    sleep 1
done
sleep 5

echo `date`": INFO: Capsule ready"

### Tests

echo `date`": INFO: Create tenant"
cat >>./tenant-test.yaml<<EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: ${TENANT}
spec:
  owners:
  - name: ${USER}
    kind: User
EOF

error_create_tenant=$(kubectl create -f ./tenant-test.yaml 2>&1)
if [ $? -ne 0 ]; then
    echo `date`": $error_create_tenant"
    error_action
fi

echo `date`": INFO: Check tenant exist"
error_check_tenant=$(kubectl get tenant ${TENANT} 2>&1)
if [ $? -ne 0 ]; then
    echo `date`": ERROR: $error_check_tenant"
    error_action
fi

echo `date`": INFO: Create user ${USER} for tenant ${TENANT}"
error_create_user=$(./hack/create-user.sh ${USER} ${TENANT} 2>&1)
if [ $? -ne 0 ]; then
    echo `date`": ERROR: $error_create_user"
    error_action
fi

echo `date`": INFO: Create namespace from tenant user"
error_create_namespace=$(kubectl --kubeconfig=${USER}-${TENANT}.kubeconfig create ns ${TENANT}-test 2>&1)
if [ $? -ne 0 ]; then
    echo `date`": ERROR: $error_create_namespace"
    error_action
fi

echo `date`": INFO: Check namespace exist in tenant"
error_tenant=$(kubectl get tenant ${TENANT} -o yaml | grep namespaces -A1 | grep ${TENANT}-test 2>&1)
if [ $? -ne 0 ]; then
    echo `date`": ERROR: $error_tenant"
    error_action
fi

echo `date`": INFO: All ok"

cleanup_action