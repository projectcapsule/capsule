# Block use of existing PVs

**Profile Applicability:** L1

**Type:** Configuration Check

**Category:** Data Isolation

**Description:** Avoid a tenant to mount existing volumes`.

**Rationale:** Tenants have to be assured that their Persistent Volumes cannot be reclaimed by other tenants.

**Audit:**

As cluster admin, create a tenant

```yaml
kubectl create -f - << EOF
apiVersion: capsule.clastix.io/v1beta1
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - kind: User
    name: alice
EOF

./create-user.sh alice oil
```

As tenant owner, check if you can access the persistent volumes

```bash 
kubectl --kubeconfig alice auth can-i get persistentvolumes
kubectl --kubeconfig alice auth can-i list persistentvolumes
kubectl --kubeconfig alice auth can-i watch persistentvolumes
```

You must receive for all the requests 'no'.
