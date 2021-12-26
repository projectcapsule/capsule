# Upgrading Tenant resource from v1alpha1 to v1beta1 version

With [Capsule v0.1.0](https://github.com/clastix/capsule/releases/tag/v0.1.0), the Tenant custom resource has been bumped to `v1beta1` from `v1alpha1` with additional fields addressing the new features implemented so far.

This document aims to provide support and a guide on how to perform a clean upgrade to the latest API version in order to avoid service disruption and data loss.

## Backup your cluster

We strongly suggest performing a full backup of your Kubernetes cluster, such as storage and etcd.
Use your favorite tool according to your needs.

## Uninstall the old Capsule release

If you're using Helm as package manager, all the Operator resources such as Deployment, Service, Role Binding, and etc. must be deleted.

```
helm uninstall -n capsule-system capsule 
```

Ensure that everything has been removed correctly, especially the Secret resources.

## Patch the Tenant custom resource definition

Helm doesn't manage the lifecycle of Custom Resource Definitions, additional details can be found [here](https://github.com/helm/community/blob/f9e06c16d89ccea1bea77c01a6a96ae3b309f823/architecture/crds.md).

This process must be executed manually as follows:

```
kubectl apply -f https://raw.githubusercontent.com/clastix/capsule/v0.1.0/config/crd/bases/capsule.clastix.io_tenants.yaml
```

> Please note the Capsule version in the said URL, your mileage may vary according to the desired upgrading version.

## Install the Capsule operator using Helm

Since the Tenant custom resource definition has been patched with new fields, we can install back Capsule using the provided Helm chart.

```
helm upgrade --install capsule clastix/capsule -n capsule-system --create-namespace
```

This will start the Operator that will perform several required actions, such as:

1. Generating a new CA 
2. Generating new TLS certificates for the local webhook server 
3. Patching the Validating and Mutating Webhook Configuration resources with the fresh new CA 
4. Patching the Custom Resource Definition tenant conversion webhook CA

## Ensure the conversion webhook is working

Kubernetes Custom Resource definitions provide a conversion webhook that is used by an Operator to perform seamless conversion between resources with different versioning.

With the fresh new installation, Capsule patched all the required moving parts to ensure this conversion is put in place, and using the latest version (actually, `v1beta1`) for presenting the Tenant resources.

You can check this behavior by issuing the following command:

```
$: kubectl get tenants.v1beta1.capsule.clastix.io
NAME   NAMESPACE QUOTA   NAMESPACE COUNT   OWNER NAME   OWNER KIND   NODE SELECTOR                  AGE
oil    3                 0                 alice        User         {"kubernetes.io/os":"linux"}   3m43s
```

You should see all the previous Tenant resources converted in the new format and structure.

```
$: kubectl get tenants.v1beta1.capsule.clastix.io 
NAME   STATE    NAMESPACE QUOTA   NAMESPACE COUNT   NODE SELECTOR                  AGE
oil    Active   3                 0                 {"kubernetes.io/os":"linux"}   3m38s
```

> Resources are still persisted in etcd using the `v1alpha1` specification and the conversion is executed on-the-fly thanks to the conversion webhook.
> If you'd like to decrease the pressure on Capsule due to the conversion webhook, we suggest performing a resource patching using the command `kubectl replace`:
> in this way, the API Server will update the etcd key with the specification according to the new versioning, allowing to skip the conversion.
