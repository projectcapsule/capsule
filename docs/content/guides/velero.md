# Tenants Backup and Restore with Velero

[Velero](https://velero.io) is a backup and restore solution that performs disaster recovery and migrates Kubernetes cluster resources and persistent volumes.

Using Velero in a Kubernetes cluster where Capsule is installed can lead to an incomplete restore of the cluster's Tenants. This is because Velero omits the `ownerReferences` section from the tenant's namespace manifests when backup them.

To avoid this problem you can use the script `velero-restore.sh` under the `hack/` folder.

In case of a data loss, the right thing to do is to restore the cluster with **Velero** at first. Once Velero has finished, you can proceed using the script to complete the restoration.

```bash
./velero-restore.sh --kubeconfing /path/to/your/kubeconfig restore
```

Running this command, we are going to patch the tenant's namespaces manifests that are actually `ownerReferences`-less. Once the command has finished its run, you got the cluster back.

Additionally, you can also specify a selected range of tenants to be restored:

```bash
./velero-restore.sh --tenant "gas oil" restore
```

In this way, only the tenants **gas** and **oil** will be restored.