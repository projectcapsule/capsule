# Tenants Backup and Restore with Velero

[Velero](https://velero.io) is a backup and restore solution that performs data protection, disaster recovery and migrates Kubernetes cluster from on-premises to the Cloud or between different Clouds.

When coming to backup and restore in Kubernetes, we have two main requirements:

- Configurations backup
- Data backup

The first requirement aims to backup all the resources stored into `etcd` database, for example: `namespaces`, `pods`, `services`, `deployments`, etc. The second is about how to backup stateful application data as volumes.

The main limitation of Velero is the multi tenancy. Currently, Velero does not support multi tenancy meaning it can be only used from admin users and so it cannot provided "as a service" to the users. This means that the cluster admin needs to take care of users' backup.

Assuming you have multiple tenants managed by Capsule, for example `oil` and `gas`, as cluster admin, you can to take care of scheduling backups for:

- Tenant cluster resources
- Namespaces belonging to each tenant

## Create backup of a tenant
Create a backup of the tenant `oil`. It consists in two different backups:

- backup of the tenant resource
- backup of all the resources belonging to the tenant

To backup the `oil` tenant selectively, label the tenant as:

```
kubectl label tenant oil capsule.clastix.io/tenant=oil
```

and create the backup

```
velero create backup oil-tenant \
    --include-cluster-resources=true \
    --include-resources=tenants.capsule.clastix.io \
    --selector capsule.clastix.io/tenant=oil
```

resulting in the following Velero object:

```yaml
apiVersion: velero.io/v1
kind: Backup
metadata:
  name: oil-tenant
spec:
  defaultVolumesToRestic: false
  hooks: {}
  includeClusterResources: true
  includedNamespaces:
  - '*'
  includedResources:
  - tenants.capsule.clastix.io
  labelSelector:
    matchLabels:
      capsule.clastix.io/tenant: oil
  metadata: {}
  storageLocation: default
  ttl: 720h0m0s
```

Create a backup of all the resources belonging to the `oil` tenant namespaces:

```
velero create backup oil-namespaces \
    --include-cluster-resources=false \
    --include-namespaces oil-production,oil-development,oil-marketing
```

resulting to the following Velero object:

```yaml
apiVersion: velero.io/v1
kind: Backup
metadata:
  name: oil-namespaces
spec:
  defaultVolumesToRestic: false
  hooks: {}
  includeClusterResources: false
  includedNamespaces:
  - oil-production
  - oil-development
  - oil-marketing
  metadata: {}
  storageLocation: default
  ttl: 720h0m0s
```

> Velero requires an Object Storage backend where to store backups, you should take care of this requirement before to use Velero.

## Restore a tenant from the backup
To recover the tenant after a disaster, or to migrate it to another cluster, create a restore from the previous backups:

```
velero create restore --from-backup oil-tenant
velero create restore --from-backup oil-namespaces
```

Using Velero to restore a Capsule tenant can lead to an incomplete recovery of tenant because the namespaces restored with Velero do not have the `OwnerReference` field used to bind the namespaces to the tenant. For this reason, all restored namespaces are not bound to the tenant:

```
kubectl get tnt
NAME   STATE    NAMESPACE QUOTA   NAMESPACE COUNT   NODE SELECTOR     AGE
gas    active   9                 5                 {"pool":"gas"}    34m
solar  active   9                 8                 {"pool":"solar"}  33m
oil    active   9                 0 # <<<           {"pool":"oil"}    54m
```

To avoid this problem you can use the script `velero-restore.sh` located under the `hack/` folder:

```
./velero-restore.sh --kubeconfing /path/to/your/kubeconfig --tenant "oil" restore
```

Running this command, we are going to patch the tenant's namespaces manifests that are actually `ownerReferences`-less. Once the command has finished its run, you got the tenant back.

```
kubectl get tnt
NAME   STATE    NAMESPACE QUOTA   NAMESPACE COUNT   NODE SELECTOR     AGE
gas    active   9                 5                 {"pool":"gas"}    44m
solar  active   9                 8                 {"pool":"solar"}  43m
oil    active   9                 3 # <<<           {"pool":"oil"}    12s
```
