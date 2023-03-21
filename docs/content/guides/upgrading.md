# Capsule upgrading guide

List of Tenant API changes:

- [Capsule v0.1.0](https://github.com/clastix/capsule/releases/tag/v0.1.0) bump to `v1beta1` from `v1alpha1`.
- [Capsule v0.2.0](https://github.com/clastix/capsule/releases/tag/v0.2.0) bump to `v1beta2` from `v1beta1`, deprecating `v1alpha1`.
- [Capsule v0.3.0](https://github.com/clastix/capsule/releases/tag/v0.3.0) missing enums required by [Capsule Proxy](https://github.com/clastix/capsule-proxy).

This document aims to provide support and a guide on how to perform a clean upgrade to the latest API version in order to avoid service disruption and data loss.

As an installation method, Helm is given for granted, YMMV using the `kustomize` manifest.

## Considerations

We strongly suggest performing a full backup of your Kubernetes cluster, such as storage and etcd.
Use your favourite tool according to your needs.

# Upgrading from v0.2.x to v0.3.x

A minor bump has been requested due to some missing enums in the Tenant resource.

## Scale down the Capsule controller

Using the `kubectl` or Helm, scale down the Capsule controller manager: this is required to avoid the old Capsule version from processing objects that aren't yet installed as a CRD.

```
helm upgrade -n capsule-system capsule --set "replicaCount=0" 
```

## Patch the Tenant custom resource definition

Unfortunately, Helm doesn't manage the lifecycle of Custom Resource Definitions, additional details can be found [here](https://github.com/helm/community/blob/f9e06c16d89ccea1bea77c01a6a96ae3b309f823/architecture/crds.md).

This process must be executed manually as follows:

```
kubectl apply -f https://raw.githubusercontent.com/clastix/capsule/v0.3.0/charts/capsule/crds/tenant-crd.yaml
```

## Update your Capsule Helm chart

Ensure to update the Capsule repository to fetch the latest changes.

```
helm repo update
```

The latest Chart must be used, at the current time, >=0.4.0 is expected for Capsule >=v0.3.0, you can fetch the full list of available charts with the following command.

```
helm search repo -l clastix/capsule
```

Since the Tenant custom resource definition has been patched with new fields, we can install back Capsule using the provided Helm chart.

```
helm upgrade --install capsule clastix/capsule -n capsule-system --create-namespace --version 0.4.0
```

This will start the Operator with the latest changes, and perform the required sync operations like:

1. Ensuring the CA is still valid
2. Ensuring a TLS certificate is valid for the local webhook server
3. If not using the cert-manager integration, patching the Validating and Mutating Webhook Configuration resources with the Capsule CA
4. If not using the cert-manager integration, patching the Capsule's Custom Resource Definitions conversion webhook fields with the Capsule CA

# Upgrading from v0.1.3 to v0.2.x

## Scale down the Capsule controller

Using the `kubectl` or Helm, scale down the Capsule controller manager: this is required to avoid the old Capsule version from processing objects that aren't yet installed as a CRD.

```
helm upgrade -n capsule-system capsule --set "replicaCount=0" 
```

> Ensure that all the Pods have been removed correctly.

## Migrate manually the `CapsuleConfiguration` to the latest API version

With the v0.2.x release of Capsule and the new features introduced, the resource `CapsuleConfiguration` is offering a new API version, bumped to `v1beta1` from `v1alpha1`.

Essentially, the `CapsuleConfiguration` is storing configuration flags that allow Capsule to be configured on the fly without requiring the operator to reload.
This resource is read at the operator init-time when the conversion webhook offered by Capsule is not yet ready to serve any request.

Migrating from v0.1.3 to v0.2.x requires a manual conversion of your `CapsuleConfiguration` according to the latest version (currently, `v1beta2`).
You can find further information about it at the section `CRDs APIs`.

The deletion of the `CapsuleConfiguration` resource is required, along with the update of the related CRD.

```
kubectl delete capsuleconfiguration default
kubectl apply -f https://raw.githubusercontent.com/clastix/capsule/v0.2.1/charts/capsule/crds/capsuleconfiguration-crd.yaml
```

During the Helm upgrade, a new `CapsuleConfiguration` will be created: please, refer to the Helm Chart values to pick up your desired settings.

## Patch the Tenant custom resource definition

Unfortunately, Helm doesn't manage the lifecycle of Custom Resource Definitions, additional details can be found [here](https://github.com/helm/community/blob/f9e06c16d89ccea1bea77c01a6a96ae3b309f823/architecture/crds.md).

This process must be executed manually as follows:

```
kubectl apply -f https://raw.githubusercontent.com/clastix/capsule/v0.2.1/charts/capsule/crds/globaltenantresources-crd.yaml
kubectl apply -f https://raw.githubusercontent.com/clastix/capsule/v0.2.1/charts/capsule/crds/tenant-crd.yaml
kubectl apply -f https://raw.githubusercontent.com/clastix/capsule/v0.2.1/charts/capsule/crds/tenantresources-crd.yaml
```

> We're giving for granted that Capsule is installed in the `capsule-system` Namespace.
> According to your needs you can change the Namespace at your wish, e.g.:
>
> ```bash
> CUSTOM_NS="tenancy-operations"
> 
> for CR in capsuleconfigurations.capsule.clastix.io globaltenantresources.capsule.clastix.io tenantresources.capsule.clastix.io tenants.capsule.clastix.io; do
>   kubectl patch crd capsuleconfigurations.capsule.clastix.io --type='json' -p=" [{'op': 'replace', 'path': '/spec/conversion/webhook/clientConfig/service/namespace', 'value': "${CUSTOM_NS}"}]"
> done
> ```

## Update your Capsule Helm chart

Ensure to update the Capsule repository to fetch the latest changes.

```
helm repo update
```

The latest Chart must be used, at the current time, >0.3.0 is expected for Capsule >v0.2.0, you can fetch the full list of available charts with the following command.

```
helm search repo -l clastix/capsule
```

Since the Tenant custom resource definition has been patched with new fields, we can install back Capsule using the provided Helm chart.

```
helm upgrade --install capsule clastix/capsule -n capsule-system --create-namespace --version 0.3.0
```

This will start the Operator with the latest changes, and perform the required sync operations like:

1. Ensuring the CA is still valid
2. Ensuring a TLS certificate is valid for the local webhook server
3. If not using the cert-manager integration, patching the Validating and Mutating Webhook Configuration resources with the Capsule CA
4. If not using the cert-manager integration, patching the Capsule's Custom Resource Definitions conversion webhook fields with the Capsule CA

## Ensure the conversion webhook is working

Kubernetes Custom Resource definitions provide a conversion webhook that is used by an Operator to perform a seamless conversion between resources with different versioning.

With the fresh new installation, Capsule patches all the required moving parts to ensure this conversion is put in place and uses the latest version (actually, `v1beta2`) for presenting the Tenant resources.

You can check this behaviour by issuing the following command:

```
$: kubectl get tenants.v1beta2.capsule.clastix.io
NAME   NAMESPACE QUOTA   NAMESPACE COUNT   OWNER NAME   OWNER KIND   NODE SELECTOR                  AGE
oil    3                 0                 alice        User         {"kubernetes.io/os":"linux"}   3m43s
```

You should see all the previous Tenant resources converted in the new format and structure.

```
$: kubectl get tenants.v1beta2.capsule.clastix.io 
NAME   STATE    NAMESPACE QUOTA   NAMESPACE COUNT   NODE SELECTOR                  AGE
oil    Active   3                 0                 {"kubernetes.io/os":"linux"}   3m38s
```

> Resources are still persisted in etcd using the previous Tenant version (`v1beta1`) and the conversion is executed on-the-fly thanks to the conversion webhook.
> If you'd like to decrease the pressure on Capsule due to the conversion webhook, we suggest performing a resource patching using the command `kubectl replace`:
> in this way, the API Server will update the etcd key with the specification according to the new versioning, allowing to skip the conversion.
>
> The `kubectl replace` command must be triggered when the Capsule webhook is up and running to allow the conversion between versions.

# Upgrading from < v0.1.0 up to v0.1.3

## Uninstall the old Capsule release

If you're using Helm as package manager, all the Operator resources such as Deployment, Service, Role Binding, etc. must be deleted.

```
helm uninstall -n capsule-system capsule 
```

Ensure that everything has been removed correctly, especially the Secret resources.

## Patch the Tenant custom resource definition

Unfortunately, Helm doesn't manage the lifecycle of Custom Resource Definitions, additional details can be found [here](https://github.com/helm/community/blob/f9e06c16d89ccea1bea77c01a6a96ae3b309f823/architecture/crds.md).

This process must be executed manually as follows:

```
kubectl apply -f https://raw.githubusercontent.com/clastix/capsule/v0.1.0/charts/capsule/crds/tenant-crd.yaml
```

> Please note the Capsule version in the said URL, your mileage may vary according to the desired upgrading version.

## Install the Capsule operator using Helm

Since the Tenant custom resource definition has been patched with new fields, we can install back Capsule using the provided Helm chart.

```
helm upgrade --install capsule clastix/capsule -n capsule-system --create-namespace --version=DESIRED_VERSION
```

> Please, note the `DESIRED_VERSION`: you have to pick the Helm chart version according to the Capsule version you'd like to upgrade to.
>
> You can retrieve it by browsing the GitHub source code picking the Capsule tag as ref and inspecting the file `Chart.yaml` available in the folder `charts/capsule`.

This will start the operator that will perform several required actions, such as:

1. Generating a new CA
2. Generating new TLS certificates for the local webhook server
3. Patching the Validating and Mutating Webhook Configuration resources with the fresh new CA
4. Patching the Custom Resource Definition tenant conversion webhook CA

## Ensure the conversion webhook is working

Kubernetes Custom Resource definitions provide a conversion webhook that is used by an Operator to perform a seamless conversion between resources with different versioning.

With the fresh new installation, Capsule patched all the required moving parts to ensure this conversion is put in place and using the latest version (actually, `v1beta1`) for presenting the Tenant resources.

You can check this behaviour by issuing the following command:

```
$: kubectl get tenants.v1beta1.capsule.clastix.io
NAME   NAMESPACE QUOTA   NAMESPACE COUNT   OWNER NAME   OWNER KIND   NODE SELECTOR                  AGE
oil    3                 0                 alice        User         {"kubernetes.io/os":"linux"}   3m43s
```

You should see all the previous Tenant resources converted into the new format and structure.

```
$: kubectl get tenants.v1beta1.capsule.clastix.io 
NAME   STATE    NAMESPACE QUOTA   NAMESPACE COUNT   NODE SELECTOR                  AGE
oil    Active   3                 0                 {"kubernetes.io/os":"linux"}   3m38s
```

> Resources are still persisted in etcd using the v1alpha1 specification and the conversion is executed on-the-fly thanks to the conversion webhook.
> If you'd like to decrease the pressure on Capsule due to the conversion webhook, we suggest performing a resource patching using the command kubectl replace: in this way, the API Server will update the etcd key with the specification according to the new versioning, allowing to skip the conversion.
