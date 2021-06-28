# Block access to cluster resources

**Profile Applicability:** L1

**Type:** Configuration Check

**Category:** Control Plane Isolation

**Description:** Tenants should not be able to view, edit, create, or delete cluster (non-namespaced) resources such Node, ClusterRole, ClusterRoleBinding, etc.

**Rationale:** Access controls should be configured for tenants so that a tenant cannot list, create, modify or delete cluster resources

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
EOF

./create-user.sh alice oil
```

As cluster admin, run the following command to retrieve the list of non-namespaced resources
```bash 
kubectl --kubeconfig cluster-admin api-resources --namespaced=false
```
For all non-namespaced resources, and each verb (get, list, create, update, patch, watch, delete, and deletecollection) issue the following command:

```bash 
kubectl --kubeconfig alice auth can-i <verb> <resource>
```
Each command must return `no`

**Cleanup:**
As cluster admin, delete all the created resources

```bash 
kubectl --kubeconfig cluster-admin delete tenant oil
```