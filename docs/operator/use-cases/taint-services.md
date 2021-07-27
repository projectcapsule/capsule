# Taint services
With Capsule, Bill can _"taint"_ the services created by Alice with an additional labels and/or annotations. There is no specific semantic assigned to these labels and annotations: they just will be assigned to the services in the tenant as they are created by Alice. This can help the cluster admin to implement specific use cases.

Bill assigns additional labels and annotations to all services created in the `oil` tenant: 

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
    additionalMetadata:
      annotations:
        capsule.clastix.io/backup: "true"
      labels:
        capsule.clastix.io/tenant: oil
EOF
```

When Alice creates a service in a namespace, this will inherit the given label and/or annotation.

# What’s next
This end our tour in Capsule use cases. As we improve Capsule, more use cases about multi-tenancy, policy admission control, and cluster governance will be covered in the future.

Stay tuned!