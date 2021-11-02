# Configure namespace object limits

**Profile Applicability:** L1

**Type:** Configuration

**Category:** Fairness

**Description:** Namespace resource quotas should be used to allocate, track and limit the number of objects, of a particular type, that can be created within a namespace.

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
        pods: 100
        services: 50
        services.loadbalancers: 3
        services.nodeports: 20
        persistentvolumeclaims: 100
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
NAME            AGE   REQUEST                 LIMIT
capsule-oil-0   23s   persistentvolumeclaims: 0/100,
                      pods: 0/100, services: 0/50,
                      services.loadbalancers: 0/3,
                      services.nodeports: 0/20  
```

Make sure that a quota is configured for API objects: `PersistentVolumeClaim`, `LoadBalancer`, `NodePort`, `Pods`, etc

**Cleanup:**
As cluster admin, delete all the created resources

```bash 
kubectl --kubeconfig cluster-admin delete tenant oil
```