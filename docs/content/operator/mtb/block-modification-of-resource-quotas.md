# Block modification of resource quotas

**Profile Applicability:** L1

**Type:** Behavioral Check

**Category:** Tenant Isolation

**Description:** Tenants should not be able to modify the resource quotas defined in their namespaces

**Rationale:** Resource quotas must be configured for isolation and fairness between tenants. Tenants should not be able to modify existing resource quotas as they may exhaust cluster resources and impact other tenants.

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
  resourceQuotas:
    items:
    - hard:
        limits.cpu: "8"
        limits.memory: 16Gi
        requests.cpu: "8"
        requests.memory: 16Gi
    - hard:
        pods: "10"
        services: "50"
    - hard:
        requests.storage: 100Gi
EOF

./create-user.sh alice oil

```

As tenant owner, run the following command to create a namespace in the given tenant

```bash 
kubectl --kubeconfig alice create ns oil-production
kubectl --kubeconfig alice config set-context --current --namespace oil-production
```

As tenant owner, check the permissions to modify/delete the quota in the tenant namespace:

```bash 
kubectl --kubeconfig alice auth can-i create quota
kubectl --kubeconfig alice auth can-i update quota
kubectl --kubeconfig alice auth can-i patch quota
kubectl --kubeconfig alice auth can-i delete quota
kubectl --kubeconfig alice auth can-i deletecollection quota
```

Each command must return 'no'

**Cleanup:**
As cluster admin, delete all the created resources

```bash 
kubectl --kubeconfig cluster-admin delete tenant oil
```