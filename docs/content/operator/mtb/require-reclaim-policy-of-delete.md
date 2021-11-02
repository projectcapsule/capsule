# Require PV reclaim policy of delete

**Profile Applicability:** L1

**Type:** Configuration Check

**Category:** Data Isolation

**Description:** Force a tenant to use a Storage Class with `reclaimPolicy=Delete`.

**Rationale:** Tenants have to be assured that their Persistent Volumes cannot be reclaimed by other tenants.

**Audit:**

As cluster admin, create a Storage Class with `reclaimPolicy=Delete`

```yaml
kubectl create -f - << EOF
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: delete-policy
reclaimPolicy: Delete
provisioner: clastix.io/nfs
EOF
```

As cluster admin, create a tenant and assign the above Storage Class

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
  storageClasses:
    allowed:
    - delete-policy
EOF

./create-user.sh alice oil
```

As tenant owner, run the following command to create a namespace in the given tenant

```bash 
kubectl --kubeconfig alice create ns oil-production
kubectl --kubeconfig alice config set-context --current --namespace oil-production
```

As tenant owner, creates a Persistent Volum Claim in the tenant namespace missing the Storage Class or using any other Storage Class: 

```yaml
kubectl --kubeconfig alice apply -f - << EOF
kind: PersistentVolumeClaim
apiVersion: v1
metadata:
  name: pvc
  namespace: oil-production
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 12Gi
EOF
```

You must receive an error message denying the request:

```
Error from server (A valid Storage Class must be used, one of the following (delete-policy)):
error when creating "STDIN": admission webhook "pvc.capsule.clastix.io" denied the request:
A valid Storage Class must be used, one of the following (delete-policy)
```

**Cleanup:**
As cluster admin, delete all the created resources

```bash 
kubectl --kubeconfig cluster-admin delete tenant oil
kubectl --kubeconfig cluster-admin delete storageclass delete-policy
```