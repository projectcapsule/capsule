# Assign Storage Classes
Persistent storage infrastructure is provided to tenants. Different types of storage requirements, with different levels of QoS, eg. SSD versus HDD, are available for different tenants according to the tenant's profile. To meet these different requirements, Bill, the cluster admin can provision different Storage Classes and assign them to the tenant:

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
  storageClasses:
    allowed:
    - ceph-rbd
    - ceph-nfs
    allowedRegex: "^ceph-.*$"
EOF
```

Capsule assures that all Persistent Volume Claims created by Alice will use only one of the valid storage classes:

```yaml
kubectl apply -f - << EOF
kind: PersistentVolumeClaim
apiVersion: v1
metadata:
  name: pvc
  namespace: oil-production
spec:
  storageClassName: ceph-rbd
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 12Gi
EOF
```

Any attempt of Alice to use a non-valid Storage Class, or missing it, is denied by the Validation Webhook enforcing it.

# What’s next
See how Bill, the cluster admin, can assign Network Policies to Alice's tenant. [Assign Network Policies](/docs/operator/use-cases/network-policies).
