# Block access to multitenant resources

**Profile Applicability:** L1

**Type:** Behavioral

**Category:** Tenant Isolation

**Description:** Block network traffic among namespaces from different tenants.

**Rationale:** Tenants cannot access services and pods in another tenant's namespaces.

**Audit:**

As cluster admin, create a couple of tenants 

```yaml
kubectl create -f - <<EOF
apiVersion: capsule.clastix.io/v1beta1
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - kind: User
    name: alice
  networkPolicies:
    items:
    - ingress:
      - from:
        - namespaceSelector:
            matchLabels:
              capsule.clastix.io/tenant: oil
      podSelector: {}
      policyTypes:
      - Ingress
EOF

./create-user.sh alice oil
```

and

```yaml
kubectl create -f - <<EOF
apiVersion: capsule.clastix.io/v1beta1
kind: Tenant
metadata:
  name: gas
spec:
  owners:
  - kind: User
    name: joe
  networkPolicies:
    items:
    - ingress:
      - from:
        - namespaceSelector:
            matchLabels:
              capsule.clastix.io/tenant: gas
      podSelector: {}
      policyTypes:
      - Ingress
EOF

./create-user.sh joe gas
```

As `oil` tenant owner, run the following commands to create a namespace and resources in the given tenant

```bash 
kubectl --kubeconfig alice create ns oil-production
kubectl --kubeconfig alice config set-context --current --namespace oil-production
kubectl --kubeconfig alice run webserver --image nginx:latest
kubectl --kubeconfig alice expose pod webserver --port 80
```

As `gas` tenant owner, run the following commands to create a namespace and resources in the given tenant

```bash 
kubectl --kubeconfig joe create ns gas-production
kubectl --kubeconfig joe config set-context --current --namespace gas-production
kubectl --kubeconfig joe run webserver --image nginx:latest
kubectl --kubeconfig joe expose pod webserver --port 80
```

As `oil` tenant owner, verify you can access the service in `oil` tenant namespace but not in the `gas` tenant namespace

```bash 
kubectl --kubeconfig alice exec webserver -- curl http://webserver.oil-production.svc.cluster.local
kubectl --kubeconfig alice exec webserver -- curl http://webserver.gas-production.svc.cluster.local
```

Viceversa, as `gas` tenant owner, verify you can access the service in `gas` tenant namespace but not in the `oil` tenant namespace

```bash 
kubectl --kubeconfig alice exec webserver -- curl http://webserver.oil-production.svc.cluster.local
kubectl --kubeconfig alice exec webserver -- curl http://webserver.gas-production.svc.cluster.local
```


**Cleanup:**
As cluster admin, delete all the created resources

```bash 
kubectl --kubeconfig cluster-admin delete tenants oil gas
```