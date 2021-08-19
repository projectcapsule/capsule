# Allow self-service management of Network Policies

**Profile Applicability:** L2

**Type:** Behavioral

**Category:** Self-Service Operations

**Description:** Tenants should be able to perform self-service operations by creating their own network policies in their namespaces.

**Rationale:** Enables self-service management of network-policies.

**Audit:**

As cluster admin, create a tenant

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
      - Egress
      - Ingress
EOF

./create-user.sh alice oil

```

As tenant owner, run the following command to create a namespace in the given tenant

```bash 
kubectl --kubeconfig alice create ns oil-production
kubectl --kubeconfig alice config set-context --current --namespace oil-production
```

As tenant owner, retrieve the networkpolicies resources in the tenant namespace

```bash 
kubectl --kubeconfig alice get networkpolicies 
NAME            POD-SELECTOR   AGE
capsule-oil-0   <none>         7m5s
```

As a tenant, checks for permissions to manage networkpolicy for each verb

```bash 
kubectl --kubeconfig alice auth can-i get networkpolicies
kubectl --kubeconfig alice auth can-i create networkpolicies
kubectl --kubeconfig alice auth can-i update networkpolicies
kubectl --kubeconfig alice auth can-i patch networkpolicies
kubectl --kubeconfig alice auth can-i delete networkpolicies
kubectl --kubeconfig alice auth can-i deletecollection networkpolicies
```

Each command must return 'yes'

**Cleanup:**
As cluster admin, delete all the created resources

```bash 
kubectl --kubeconfig cluster-admin delete tenant oil
```