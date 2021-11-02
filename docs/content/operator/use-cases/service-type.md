# Disable Service Types
Bill, the cluster admin, can prevent the creation of services with specific service types.

## NodePort
When dealing with a _shared multi-tenant_ scenario, multiple _NodePort_ services can start becoming cumbersome to manage. The reason behind this could be related to the overlapping needs by the Tenant owners, since a _NodePort_ is going to be open on all nodes and, when using `hostNetwork=true`, accessible to any _Pod_ although any specific `NetworkPolicy`.

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

With the above configuration, any attempt of Alice to create a Service of type `NodePort` is denied by the Validation Webhook enforcing it. Default value is `true`.

## ExternalName
Service with the type of `ExternalName` has been found subject to many security issues. To prevent tenant owners to create services with the type of `ExternalName`, the cluster admin can prevent a tenant to create them:

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

With the above configuration, any attempt of Alice to create a Service of type `externalName` is denied by the Validation Webhook enforcing it. Default value is `true`.

## LoadBalancer

Same as previously, the Service of type of `LoadBalancer` could be blocked for various reasons. To prevent tenant owners to create these kinds of services, the cluster admin can prevent a tenant to create them:

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
      loadBalancer: false
EOF
```

With the above configuration, any attempt of Alice to create a Service of type `LoadBalancer` is denied by the Validation Webhook enforcing it. Default value is `true`.

# What’s next
See how Bill, the cluster admin, can set taints on the Alice's services. [Taint services](/docs/operator/use-cases/taint-services).