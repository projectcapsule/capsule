# Taint namespaces
With Capsule, Bill can _"taint"_ the namespaces created by Alice with an additional labels and/or annotations. There is no specific semantic assigned to these labels and annotations: they just will be assigned to the namespaces in the tenant as they are created by Alice. This can help the cluster admin to implement specific use cases. As for example, it can be used to implement backup as a service for namespaces in the tenant.

Bill assigns an additional label to the `oil` tenant to force the backup system to take care of Alice's namespaces: 

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: oil
spec:
  owner:
    name: alice
    kind: User
  namespaceQuota: 3
  namespacesMetadata:
    additionalLabels:
      capsule.clastix.io/backup: "true"
```

or by annotations:

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: oil
spec:
  owner:
    name: alice
    kind: User
  namespaceQuota: 3
  namespacesMetadata:
    additionalAnnotations:
      capsule.clastix.io/do_stuff: backup
```

When Alice creates a namespace, this will inherit the given label and/or annotation:

```yaml
kind: Namespace
apiVersion: v1
metadata:
  name: oil-production
  labels:
    capsule.clastix.io/backup: "true"    # here the additional label
    capsule.clastix.io/tenant: oil
  annotations:
    capsule.clastix.io/do_stuff: backup  # here the additional annotation
```

# What’s next
See how Bill, the cluster admin, can assign multiple tenants to the same owner. [Multiple tenants owned by the same user]().