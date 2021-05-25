# Disabling NodePort Services per Tenant

When dealing with a _shared multi-tenant_ scenario, _NodePort_ services can start becoming cumbersome to manage.

Reason behind this could be related to the overlapping needs by the Tenant owners, since a _NodePort_ is going to be open on all nodes and, when using `hostNetwork=true`, accessible to any _Pod_ although any specific `NetworkPolicy`.

Actually, Capsule doesn't block by default the creation of `NodePort` services.

Although this behavior is not yet manageable using a CRD key, if you need to prevent a Tenant from creating `NodePort` Services, the annotation `capsule.clastix.io/enable-node-ports` can be used as follows.

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: oil
  annotations:
    capsule.clastix.io/enable-node-ports: "false"
spec:
  owner:
    kind: User
    name: alice
```

With the said configuration, any Namespace owned by the Tenant will not be able to get a Service of type `NodePort` since the creation will be denied by the validation webhook.
