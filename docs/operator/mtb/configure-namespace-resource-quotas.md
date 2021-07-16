# Configure namespace resource quotas

**Profile Applicability:** L1

**Type:** Configuration

**Category:** Fairness

**Description:** Namespace resource quotas should be used to allocate, track, and limit a tenant's use of shared resources.

**Rationale:** Resource quotas must be configured for each tenant namespace, to guarantee isolation and fairness across tenants.

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
        requests.storage: 100Gi
EOF

./create-user.sh alice oil

```

As tenant owner, run the following command to create a namespace in the given tenant

```bash 
kubectl --kubeconfig alice create ns oil-production
kubectl --kubeconfig alice config set-context --current --namespace oil-production
```

As tenant owner, retrieve the configured quotas in the tenant namespace:

```bash 
kubectl --kubeconfig alice get quota
NAME            AGE   REQUEST                                      LIMIT
capsule-oil-0   24s   requests.cpu: 0/8, requests.memory: 0/16Gi   limits.cpu: 0/8, limits.memory: 0/16Gi                 
capsule-oil-1   24s   requests.storage: 0/10Gi                     
```

Make sure that a quota is configured for CPU, memory, and storage resources.

**Cleanup:**
As cluster admin, delete all the created resources

```bash 
kubectl --kubeconfig cluster-admin delete tenant oil
```