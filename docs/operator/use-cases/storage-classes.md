# Assign Storage Classes
The Acme Corp. can provide persistent storage infrastructure to their tenants. Different types of storage requirements, with different levels of QoS, eg. SSD versus HDD, are available for different tenants according to the tenant's profile. To meet these different requirements, Bill, the cluster admin can provision different Storage Classes and assign them to the tenant:

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: oil
spec:
  owner:
    name: alice
    kind: User
  storageClasses:
    allowed:
    - ceph-rbd
    - ceph-nfs
  ...
```

It is also possible to use a regular expression for assigning Storage Classes:

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: oil
spec:
  owner:
    name: alice
    kind: User
  storageClasses:
     allowedRegex: "^ceph-.*$"
  ...
```

Alice, as tenant owner, gets the list of valid Storage Classes by checking any of the her namespaces:

```
alice@caas# kubectl describe ns oil-production
Name:         oil-production
Labels:       capsule.clastix.io/tenant=oil
Annotations:  capsule.clastix.io/storage-classes: ceph-rbd,ceph-nfs
              capsule.clastix.io/storage-classes-regexp: ^ceph-.*$
...
```

The Capsule controller will ensure that all Persistent Volume Claims created by Alice will use only one of the assigned storage classes:

For example:

```yaml
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
```

Any attempt of Alice to use a non valid Storage Class, e.g. `default`, will fail::
```
Error from server: error when creating persistent volume claim pvc:
admission webhook "pvc.capsule.clastix.io" denied the request:
Storage Class default is forbidden for the current Tenant
```

# What’s next
See how Bill, the cluster admin, can assign Network Policies to Alice's tenant. [Assign Network Policies](./network-policies.md).
