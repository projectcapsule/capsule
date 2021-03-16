# Create namespaces
Alice can create a new namespace in her tenant, as simply:

```
alice@caas# kubectl create ns oil-production
```

> Note that Alice started the name of her namespace with an identifier of her
> tenant: this is not a strict requirement but it is highly suggested because
> it is likely that many different tenants would like to call their namespaces
> as `production`, `test`, or `demo`, etc.
> 
> The enforcement of this naming convention is optional and can be controlled by the cluster administrator with the `--force-tenant-prefix` option as an argument of the Capsule controller.

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

Alice is the admin of the namespaces:

```
alice@caas# kubectl get rolebindings -n oil-production
NAME              ROLE                AGE
namespace:admin   ClusterRole/admin   9m5s 
namespace-deleter ClusterRole/admin   9m5s 
```

The said Role Binding resources are automatically created by Capsule when Alice creates a namespace in the tenant.

Alice can deploy any resource in the namespace, according to the predefined
[`admin` cluster role](https://kubernetes.io/docs/reference/access-authn-authz/rbac/#user-facing-roles).

```
alice@caas# kubectl -n oil-development run nginx --image=docker.io/nginx 
alice@caas# kubectl -n oil-development get pods
```

Alice can create additional namespaces, according to the `namespaceQuota` field of the tenant manifest:

```
alice@caas# kubectl create ns oil-development
alice@caas# kubectl create ns oil-test
```

While Alice creates namespace resources the Capsule controller updates the status of the tenant so Bill, the cluster admin, can check its status:

```
bill@caas# kubectl describe tenant oil
```

```yaml
...
status:
  namespaces:
    oil-development
    oil-production
    oil-test
  size:  3 # current namespace count
...
```

Once the namespace quota assigned to the tenant has been reached, Alice cannot create further namespaces

```
alice@caas# kubectl create ns oil-training
Error from server (Cannot exceed Namespace quota: please, reach out to the system administrators): admission webhook "quota.namespace.capsule.clastix.io" denied the request.
```
The enforcement on the maximum number of Namespace resources per Tenant is the responsibility of the Capsule controller via its Dynamic Admission Webhook capability.

# What’s next
See how Alice, the tenant owner, can assign different user roles in the tenant. [Assign permissions](./permissions.md).
