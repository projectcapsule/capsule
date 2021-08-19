# Create namespaces
Alice, once logged with her credentials, can create a new namespace in her tenant, as simply issuing:

```
kubectl create ns oil-production
```

Alice started the name of the namespace prepended by the name of the tenant: this is not a strict requirement but it is highly suggested because it is likely that many different tenants would like to call their namespaces `production`, `test`, or `demo`, etc.

The enforcement of this naming convention is optional and can be controlled by the cluster administrator with the `--force-tenant-prefix` option as an argument of the Capsule controller.

When Alice creates the namespace, the Capsule controller listening for creation and deletion events assigns to Alice the following roles:

```yaml
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: namespace:admin
  namespace: oil-production
subjects:
- kind: User
  name: alice
roleRef:
  kind: ClusterRole
  name: admin
  apiGroup: rbac.authorization.k8s.io
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: namespace-deleter
  namespace: oil-production
subjects:
- kind: User
  name: alice
roleRef:
  kind: ClusterRole
  name: namespace-deleter
  apiGroup: rbac.authorization.k8s.io
```

So Alice is the admin of the namespaces:

```
kubectl get rolebindings -n oil-production
NAME              ROLE                AGE
namespace:admin   ClusterRole/admin   9m5s 
namespace-deleter ClusterRole/admin   9m5s 
```

The said Role Binding resources are automatically created by Capsule controller when Alice creates a namespace in the tenant.

Alice can deploy any resource in the namespace, according to the predefined
[`admin` cluster role](https://kubernetes.io/docs/reference/access-authn-authz/rbac/#user-facing-roles).

```
kubectl -n oil-development run nginx --image=docker.io/nginx 
kubectl -n oil-development get pods
```

Bill, the cluster admin, can control how many namespaces Alice, creates by setting a quota in the tenant manifest `spec.namespaceOptions.quota`

```yaml
apiVersion: capsule.clastix.io/v1beta1
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: alice
    kind: User
  namespaceOptions:
    quota: 3
```

Alice can create additional namespaces according to the quota:

```
kubectl create ns oil-development
kubectl create ns oil-test
```

While Alice creates namespaces, the Capsule controller updates the status of the tenant so Bill, the cluster admin, can check the status:

```
kubectl describe tenant oil
```

```yaml
...
status:
  Namespaces:
    oil-development
    oil-production
    oil-test
  size:  3 # current namespace count
...
```

Once the namespace quota assigned to the tenant has been reached, Alice cannot create further namespaces

```
kubectl create ns oil-training
Error from server (Cannot exceed Namespace quota: please, reach out to the system administrators): admission webhook "namespace.capsule.clastix.io" denied the request.
```
The enforcement on the maximum number of namespaces per Tenant is the responsibility of the Capsule controller via its Dynamic Admission Webhook capability.

# What’s next
See how Alice, the tenant owner, can assign different user roles in the tenant. [Assign permissions](./permissions.md).
