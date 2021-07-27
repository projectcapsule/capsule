# Disable Service Types
Bill, the cluster admin, can prevent creation of services with specific service types.

## NodePort
When dealing with a _shared multi-tenant_ scenario, multiple _NodePort_ services can start becoming cumbersome to manage. Reason behind this could be related to the overlapping needs by the Tenant owners, since a _NodePort_ is going to be open on all nodes and, when using `hostNetwork=true`, accessible to any _Pod_ although any specific `NetworkPolicy`.

Bill, the cluster admin, can block the creation of services with `NodePort` service type for a given tenant

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta1
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: alice
    kind: User
  serviceOptions:
    allowedServices:
      nodePort: false
EOF
```

With the above configuration, any attempt of Alice to create a a Service of type `NodePort` is denied by the Validation Webhook enforcing it. Default value is `true`.

## ExternalName
Service with type of `ExternalName` has been found subject to many securty issue. To prevent tenant owners to create services with type of `ExternalName`, the cluster admin can prevent a tenant to create them:

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta1
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: alice
    kind: User
  serviceOptions:
    allowedServices:
      externalName: false
EOF
```

With the above configuration, any attempt of Alice to create a a Service of type `externalName` is denied by the Validation Webhook enforcing it. Default value is `true`.

# What’s next
See how Bill, the cluster admin, can set taints on the Alice's services. [Taint services](./taint-services.md).