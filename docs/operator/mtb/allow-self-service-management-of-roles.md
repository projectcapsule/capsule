# Allow self-service management of Roles

**Profile Applicability:** L2

**Type:** Behavioral

**Category:** Self-Service Operations

**Description:** Tenants should be able to perform self-service operations by creating their own roles in their namespaces.

**Rationale:** Enables self-service management of roles.

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

As tenant owner, run the following command to create a namespace in the given tenant

```bash 
kubectl --kubeconfig alice create ns oil-production
kubectl --kubeconfig alice config set-context --current --namespace oil-production
```

As tenant owner, check for permissions to manage roles for each verb

```bash 
kubectl --kubeconfig alice auth can-i get roles
kubectl --kubeconfig alice auth can-i create roles
kubectl --kubeconfig alice auth can-i update roles
kubectl --kubeconfig alice auth can-i patch roles
kubectl --kubeconfig alice auth can-i delete roles
kubectl --kubeconfig alice auth can-i deletecollection roles
```

Each command must return 'yes'

**Cleanup:**
As cluster admin, delete all the created resources

```bash 
kubectl --kubeconfig cluster-admin delete tenant oil
```