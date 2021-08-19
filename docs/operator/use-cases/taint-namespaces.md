# Taint namespaces
With Capsule, Bill can _"taint"_ the namespaces created by Alice with additional labels and/or annotations. There is no specific semantic assigned to these labels and annotations: they just will be assigned to the namespaces in the tenant as they are created by Alice. This can help the cluster admin to implement specific use cases. As it can be used to implement backup as a service for namespaces in the tenant.

Bill assigns additional labels and annotations to all namespaces created in the `oil` tenant: 

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
  namespaceOptions:
    additionalMetadata:
      annotations:
        capsule.clastix.io/backup: "true"
      labels:
        capsule.clastix.io/tenant: oil
EOF
```

When Alice creates a namespace, this will inherit the given label and/or annotation.

# What’s next
See how Bill, the cluster admin, can assign multiple tenants to Alice. [Assign multiple tenants to an owner](./multiple-tenants.md).