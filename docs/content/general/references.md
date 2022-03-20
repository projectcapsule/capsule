# Reference

Reference document for Capsule Operator configuration

## Custom Resource Definition

Capsule operator uses a Custom Resources Definition (CRD) for _Tenants_. Tenants are cluster wide resources, so you need cluster level permissions to work with tenants. You can learn about tenant CRD by the `kubectl explain` command:

```
kubectl explain tenant

KIND:     Tenant
VERSION:  capsule.clastix.io/v1beta1

DESCRIPTION:
     Tenant is the Schema for the tenants API

FIELDS:
   apiVersion   <string>
     APIVersion defines the versioned schema of this representation of an object.
     Servers should convert recognized schemas to the latest internal value,
     and may reject unrecognized values. More info:
     https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources

   kind <string>
     Kind is a string value representing the REST resource this object represents.
     Servers may infer this from the endpoint the client submits requests to.
     Cannot be updated. In CamelCase. More info:
     https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds

   metadata     <Object>
     Standard object's metadata. More info:
     https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata

   spec <Object>
     TenantSpec defines the desired state of Tenant

   status       <Object>
     Returns the observed state of the Tenant
```

For Tenant spec:

```
kubectl explain tenant.spec

KIND:     Tenant
VERSION:  capsule.clastix.io/v1beta1

RESOURCE: spec <Object>

DESCRIPTION:
     TenantSpec defines the desired state of Tenant

FIELDS:
   additionalRoleBindings       <[]Object>
     Specifies additional RoleBindings assigned to the Tenant. Capsule will
     ensure that all namespaces in the Tenant always contain the RoleBinding for
     the given ClusterRole. Optional.

   containerRegistries  <Object>
     Specifies the trusted Image Registries assigned to the Tenant. Capsule
     assures that all Pods resources created in the Tenant can use only one of
     the allowed trusted registries. Optional.

   imagePullPolicies    <[]string>
     Specify the allowed values for the imagePullPolicies option in Pod
     resources. Capsule assures that all Pod resources created in the Tenant can
     use only one of the allowed policy. Optional.

   ingressOptions       <Object>
     Specifies options for the Ingress resources, such as allowed hostnames and
     IngressClass. Optional.

   limitRanges  <Object>
     Specifies the resource min/max usage restrictions to the Tenant. The assigned
     values are inherited by any namespace created in the Tenant. Optional.

   namespaceOptions     <Object>
     Specifies options for the Namespaces, such as additional metadata or
     maximum number of namespaces allowed for that Tenant. Once the namespace
     quota assigned to the Tenant has been reached, the Tenant owner cannot
     create further namespaces. Optional.

   networkPolicies      <Object>
     Specifies the NetworkPolicies assigned to the Tenant. The assigned
     NetworkPolicies are inherited by any namespace created in the Tenant.
     Optional.

   nodeSelector <map[string]string>
     Specifies the label to control the placement of pods on a given pool of
     worker nodes. All namesapces created within the Tenant will have the node
     selector annotation. This annotation tells the Kubernetes scheduler to
     place pods on the nodes having the selector label. Optional.

   owners       <[]Object> -required-
     Specifies the owners of the Tenant. Mandatory.

   priorityClasses      <Object>
     Specifies the allowed priorityClasses assigned to the Tenant. Capsule
     assures that all pods created in the Tenant can use only one
     of the allowed priorityClasses. Optional.

   resourceQuotas       <Object>
     Specifies a list of ResourceQuota resources assigned to the Tenant. The
     assigned values are inherited by any namespace created in the Tenant. The
     Capsule operator aggregates ResourceQuota at Tenant level, so that the hard
     quota is never crossed for the given Tenant. This permits the Tenant owner
     to consume resources in the Tenant regardless of the namespace. Optional.

   serviceOptions       <Object>
     Specifies options for the Service, such as additional metadata or block of
     certain type of Services. Optional.

   storageClasses       <Object>
     Specifies the allowed StorageClasses assigned to the Tenant. Capsule
     assures that all PersistentVolumeClaim resources created in the Tenant can
     use only one of the allowed StorageClasses. Optional.
```

and Tenant status:

```
kubectl explain tenant.status
KIND:     Tenant
VERSION:  capsule.clastix.io/v1beta1

RESOURCE: status <Object>

DESCRIPTION:
     Returns the observed state of the Tenant

FIELDS:
   namespaces   <[]string>
     List of namespaces assigned to the Tenant.

   size <integer> -required-
     How many namespaces are assigned to the Tenant.

   state        <string> -required-
     The operational state of the Tenant. Possible values are "Active",
     "Cordoned".
```

## Capsule Configuration

The Capsule configuration can be piloted by a Custom Resource definition named `CapsuleConfiguration`.

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: CapsuleConfiguration
metadata:
  name: default
  annotations:
    capsule.clastix.io/ca-secret-name: "capsule-ca"
    capsule.clastix.io/tls-secret-name: "capsule-tls"
    capsule.clastix.io/mutating-webhook-configuration-name: "capsule-mutating-webhook-configuration"
    capsule.clastix.io/validating-webhook-configuration-name: "capsule-validating-webhook-configuration"
spec:
  userGroups: ["capsule.clastix.io"]
  forceTenantPrefix: false
  protectedNamespaceRegex: ""
```

Option | Description                                                                  | Default
--- |------------------------------------------------------------------------------| ---
`.spec.forceTenantPrefix` | Force the tenant name as prefix for namespaces: `<tenant_name>-<namespace>`. | `false`
`.spec.userGroups` | Array of Capsule groups to which all tenant owners must belong.              | `[capsule.clastix.io]`
`.spec.protectedNamespaceRegex` | Disallows creation of namespaces matching the passed regexp.                 | `null`
`.metadata.annotations.capsule.clastix.io/ca-secret-name` | Set the Capsule Certificate Authority secret name                            | `capsule-ca`
`.metadata.annotations.capsule.clastic.io/tls-secret-name` | Set the Capsule TLS secret name                                              | `capsule-tls`
`.metadata.annotations.capsule.clastix.io/mutating-webhook-configuration-name` | Set the MutatingWebhookConfiguration name                                    | `mutating-webhook-configuration-name`
`.metadata.annotations.capsule.clastix.io/validating-webhook-configuration-name` | Set the ValidatingWebhookConfiguration name                                  | `validating-webhook-configuration-name`

Upon installation using Kustomize or Helm, a `capsule-default` resource will be created.
The reference to this configuration is managed by the CLI flag `--configuration-name`.  

## Capsule Permissions

In the current implementation, the Capsule operator requires cluster admin permissions to fully operate. Make sure you deploy Capsule having access to the default `cluster-admin` ClusterRole.

## Admission Controllers

Capsule implements Kubernetes multi-tenancy capabilities using a minimum set of standard [Admission Controllers](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/) enabled on the Kubernetes APIs server.

Here the list of required Admission Controllers you have to enable to get full support from Capsule:

* PodNodeSelector
* LimitRanger
* ResourceQuota
* MutatingAdmissionWebhook
* ValidatingAdmissionWebhook

In addition to the required controllers above, Capsule implements its own set through the [Dynamic Admission Controller](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/) mechanism, providing callbacks to add further validation or resource patching.

To see Admission Controls installed by Capsule:

```
$ kubectl get ValidatingWebhookConfiguration
NAME                                       WEBHOOKS   AGE
capsule-validating-webhook-configuration   8          2h

$ kubectl get MutatingWebhookConfiguration
NAME                                       WEBHOOKS   AGE
capsule-mutating-webhook-configuration     1          2h
```

## Command Options

The Capsule operator provides the following command options:

Option | Description | Default
--- | --- | ---
`--metrics-addr` | The address and port where `/metrics` are exposed. | `127.0.0.1:8080`
`--enable-leader-election` | Start a leader election client and gain leadership before executing the main loop. | `true`
`--zap-log-level` | The log verbosity with a value from 1 to 10 or the basic keywords.  | `4`
`--zap-devel` | The flag to get the stack traces for deep debugging.  | `null`
`--configuration-name` | The Capsule Configuration CRD name, default is installed automatically | `capsule-default`


## Created Resources

Once installed, the Capsule operator creates the following resources in your cluster:

```
NAMESPACE       RESOURCE
                namespace/capsule-system
                customresourcedefinition.apiextensions.k8s.io/tenants.capsule.clastix.io
                customresourcedefinition.apiextensions.k8s.io/capsuleconfigurations.capsule.clastix.io
                clusterrole.rbac.authorization.k8s.io/capsule-proxy-role
                clusterrole.rbac.authorization.k8s.io/capsule-metrics-reader
                capsuleconfiguration.capsule.clastix.io/capsule-default
                mutatingwebhookconfiguration.admissionregistration.k8s.io/capsule-mutating-webhook-configuration
                validatingwebhookconfiguration.admissionregistration.k8s.io/capsule-validating-webhook-configuration
capsule-system  clusterrolebinding.rbac.authorization.k8s.io/capsule-manager-rolebinding
capsule-system  clusterrolebinding.rbac.authorization.k8s.io/capsule-proxy-rolebinding
capsule-system  secret/capsule-ca
capsule-system  secret/capsule-tls
capsule-system  service/capsule-controller-manager-metrics-service
capsule-system  service/capsule-webhook-service
capsule-system  deployment.apps/capsule-controller-manager
```
