# Velero Backup Restoration

Velero is a backup system that perform disaster recovery, and migrate Kubernetes cluster resources and persistent volumes.

Using this in a Kubernetes cluster where Capsule is installed can lead to an incomplete restore of the cluster's Tenants. This is due to the fact that Velero omits the `ownerReferences` section from the tenant's namespace manifests when backup them.

To avoid this problem you can use the script `velero-restore.sh` under the `hack/` folder.

Below are some examples on how to use the script to avoid incomplete restorations.

## Getting Started

In case of a data loss, the right thing to do is to restore the cluster with **Velero** at first. Once Velero has finished, you can proceed using the script to complete the restoration.

```bash
./velero-restore.sh --kubeconfing /path/to/your/kubeconfig restore
```

Running this command, we are going to patch the tenant's namespaces manifests that are actually `ownerReferences`-less. Once the command has finished its run, you got the cluster back.