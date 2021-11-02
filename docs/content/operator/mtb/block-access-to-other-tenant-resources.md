# Block access to other tenant resources

**Profile Applicability:** L1

**Type:** Behavioral

**Category:** Tenant Isolation

**Description:** Each tenant has its own set of resources, such as namespaces, service accounts, secrets, pods, services, etc. Tenants should not be allowed to access each other's resources.

**Rationale:** Tenant's resources must be not accessible by other tenants.

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
EOF

./create-user.sh joe gas

```

As `oil` tenant owner, run the following command to create a namespace in the given tenant

```bash 
kubectl --kubeconfig alice create ns oil-production
kubectl --kubeconfig alice config set-context --current --namespace oil-production
```

As `gas` tenant owner, run the following command to create a namespace in the given tenant

```bash 
kubectl --kubeconfig joe create ns gas-production
kubectl --kubeconfig joe config set-context --current --namespace gas-production
```


As `oil` tenant owner, try to retrieve the resources in the `gas` tenant namespaces

```bash 
kubectl --kubeconfig alice get serviceaccounts --namespace  gas-production 
```

You must receive an error message:

```
Error from server (Forbidden): serviceaccount is forbidden:
User "oil" cannot list resource "serviceaccounts" in API group "" in the namespace "gas-production"
```

As `gas` tenant owner, try to retrieve the resources in the `oil` tenant namespaces

```bash 
kubectl --kubeconfig joe get serviceaccounts --namespace  oil-production 
```

You must receive an error message:

```
Error from server (Forbidden): serviceaccount is forbidden:
User "joe" cannot list resource "serviceaccounts" in API group "" in the namespace "oil-production"
```

**Cleanup:**
As cluster admin, delete all the created resources

```bash 
kubectl --kubeconfig cluster-admin delete tenants oil gas
```