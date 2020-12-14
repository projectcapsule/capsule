# Reference

* [Custom Resource Definition](#customer-resource-definition)
  * [Metadata](#metadata)
    * [name](#name)
  * [Spec](#spec)
    * [owner](#owner)
    * [nodeSelector](#nodeSelector)
    * [namespaceQuota](#namespaceQuota)
    * [namespacesMetadata](#namespacesMetadata)
    * [servicesMetadata](#servicesMetadata)
    * [ingressClasses](#ingressClasses)
    * [ingressHostNames](#ingressHostNames)
    * [storageClasses](#storageClasses)
    * [containerRegistries](#containerRegistries)
    * [additionalRoleBindings](#additionalRoleBindings)
    * [resourceQuotas](#resourceQuotas)
    * [limitRanges](#limitRanges)
    * [networkPolicies](#networkPolicies)
    * [externalServiceIPs](#externalServiceIPs)
  * [Status](#status)
    * [size](#size)
    * [namespaces](#namespaces)
* [Role Based Access Control](#role-based-access-control)
* [Admission Controllers](#admission-controller)
* [Command Options](#command-options)
* [Created Resources](#created-resources)


## Custom Resource Definition
Capsule operator uses a single Custom Resources Definition (CRD) for _Tenants_. Please, see the [Tenant Custom Resource Definition](https://github.com/clastix/capsule/blob/master/config/crd/bases/capsule.clastix.io_tenants.yaml). In Caspule, Tenants are cluster wide resources. You need for cluster level permissions to work with tenants.

### Metadata
#### name
Metadata `name` can contain any valid symbol from the regex: `[a-z0-9]([-a-z0-9]*[a-z0-9])?`.

### Spec
#### owner
The field `owner` is the only mandatory spec in a _Tenant_ manifest. It specifies the ownership of the tenant:

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: tenant
spec:
  owner: # required
    name: <name>
    kind: <User|Group>
```

The user and group names should be valid identities. Capsule does not care about the authentication strategy used in the cluster and all the Kubernetes methods of [Authentication](https://kubernetes.io/docs/reference/access-authn-authz/authentication/) are supported. The only requirement to use Capsule is to assign tenant users to the the group defined by `--capsule-user-group` option, which defaults to `capsule.clastix.io`.

Assignment to a group depends on the used authentication strategy.

For example, if you are using `capsule.clastix.io`, users authenticated through a _X.509_ certificate must have `capsule.clastix.io` as _Organization_: `-subj "/CN=${USER}/O=capsule.clastix.io"`

Users authenticated through an _OIDC token_ must have

```json
...
"users_groups": [
    "capsule.clastix.io",
    "other_group"
]
```

Permissions are controlled by RBAC.

#### nodeSelector
Field `nodeSelector` specifies the label to control the placement of pods on a given pool of worker nodes:

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: tenant
spec:
  nodeSelector:
    <key>: <value>
```

All namesapces created within the tenant will have the annotation:

```yaml
kind: Namespace
apiVersion: v1
metadata:
  annotations:
    scheduler.alpha.kubernetes.io/node-selector: 'key=value'
```

This annotation tells the Kubernetes scheduler to place pods on the nodes having that label:

```yaml
kind: Pod
apiVersion: v1
metadata:
  name: sample
spec:
  nodeSelector:
    <key>: <value>
```

> NB:
> While Capsule just enforces the annotation `scheduler.alpha.kubernetes.io/node-selector` at namespace level,
> the `nodeSelector` field in the pod template is under the control of the default _PodNodeSelector_ enabled
> on the Kubernetes API server using the flag `--enable-admission-plugins=PodNodeSelector`.

Please, see how to [Assigning Pods to Nodes](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/) documentation.

The tenant owner is not allowed to change or remove the annotation above from the namespace.

#### namespaceQuota
Field `namespaceQuota` specifies the maximum number of namespaces allowed for that tenant.

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: tenant
spec:
  namespaceQuota: <quota>
```
Once the namespace quota assigned to the tenant has been reached, yhe tenant owner cannot create further namespaces.

#### namespacesMetadata
Field `namespacesMetadata` specifies additional labels and annotations the Capsule operator places on any _Namespace_ in the tenant.

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: tenant
spec:
  namespacesMetadata:
    additionalAnnotations:
      <annotations>
    additionalLabels:
      <key>: <value>
```

Al namespaces in the tenant will have:

```yaml
kind: Namespace
apiVersion: v1
metadata:
  annotations:
    <annotations>
  labels:
    <key>: <value>
```

The tenant owner is not allowed to change or remove such labels and annotations from the namespace.

#### servicesMetadata
Field `servicesMetadata` specifies additional labels and annotations the Capsule operator places on any _Service_ in the tenant.

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: tenant
spec:
  servicesMetadata:
    additionalAnnotations:
      <annotations>
    additionalLabels:
      <key>: <value>
```

Al services in the tenant will have:

```yaml
kind: Service
apiVersion: v1
metadata:
  annotations:
    <annotations>
  labels:
    <key>: <value>
```

The tenant owner is not allowed to change or remove such labels and annotations from the _Service_.

#### ingressClasses
Field `ingressClasses` specifies the _IngressClass_ assigned to the tenant.

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: tenant
spec:
  ingressClasses:
    allowed:
    - <class>
    allowedRegex: <regex>
```

Capsule assures that all the _Ingress_ resources created in the tenant can use only one of the allowed _IngressClass_.

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: <name>
  namespace:
  annotations:
    kubernetes.io/ingress.class: <class>
```

> NB: _Ingress_ resources are supported in both the versions, `networking.k8s.io/v1beta1` and `networking.k8s.io/v1`.

Allowed _IngressClasses_ are reported into namespaces as annotations, so the tenant owner can check them

```yaml
kind: Namespace
apiVersion: v1
metadata:
  annotations:
    capsule.clastix.io/ingress-classes: <class>
    capsule.clastix.io/ingress-classes-regexp: <regex>
```
Any tentative of tenant owner to use a not allowed _IngressClass_ will fail.

#### ingressHostNames
Field `ingressHostNames` specifies the allowed hostnames in _Ingresses_ for the given tenant.

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: tenant
spec:
  ingressHostNames:
     allowed:
     - <hostname>
     allowedRegex: <regex>
```

Capsule assures that all _Ingress_ resources created in the tenant can use only one of the allowed hostnames.

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: <name>
  namespace:
  annotations:
spec:
  rules:
  - host: <hostname>
    http: {}
```

> NB: _Ingress_ resources are supported in both the versions, `networking.k8s.io/v1beta1` and `networking.k8s.io/v1`.

Any tentative of tenant owner to use one of not allowed hostnames will fail.

#### storageClasses
Field `storageClasses` specifies the _StorageClasses_ assigned to the tenant.

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: tenant
spec:
  storageClasses:
    allowed:
    - <class>
    allowedRegex: <regex>
```

Capsule assures that all _PersistentVolumeClaim_ resources created in the tenant can use only one of the allowed _StorageClasses_.

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: <name>
  namespace:
spec:
  storageClassName: <class>
```

Allowed _StorageClasses_ are reported into namespaces as annotations, so the tenant owner can check them

```yaml
kind: Namespace
apiVersion: v1
metadata:
  annotations:
    capsule.clastix.io/storage-classes: <class>
    capsule.clastix.io/storage-classes-regexp: <regex>
```

Any tentative of tenant owner to use a not allowed _StorageClass_ will fail.

#### containerRegistries
Field `containerRegistries` specifies the ttrusted image registries assigned to the tenant.

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: tenant
spec:
  containerRegistries:
     allowed:
     - <registry>
     allowedRegex: <regex>
```

Capsule assures that all _Pods_ resources created in the tenant can use only one of the allowed trusted registries.

Allowed registries are reported into namespaces as annotations, so the tenant owner can check them

```yaml
kind: Namespace
apiVersion: v1
metadata:
  annotations:
    capsule.clastix.io/allowed-registries-regexp: <regex>
    capsule.clastix.io/registries: <registry>
```

Any tentative of tenant owner to use a not allowed registry will fail.

> NB:
> In case of naked and official images hosted on Docker Hub, Capsule is going
> to retrieve the registry even if it's not explicit: a `busybox:latest` Pod
> running on a Tenant allowing `docker.io` will not blocked, even if the image
> field is not explicit as `docker.io/busybox:latest`.

#### additionalRoleBindings
Field `additionalRoleBindings` specifies additional _RoleBindings_ assigned to the tenant.

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: tenant
spec:
  additionalRoleBindings:
  - clusterRoleName: <ClusterRole>
    subjects:
    - kind: <Group|User|ServiceAccount>
      apiGroup: rbac.authorization.k8s.io
      name: <name>
```

Capsule will ensure that all namespaces in the tenant always contain the _RoleBinding_ for the given _ClusterRole_.

#### resourceQuotas
Field `resourceQuotas` specifies a list of _ResourceQuota_ resources assigned to the tenant.

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: tenant
spec:
  resourceQuotas:
  - hard:
      limits.cpu: <hard_value>
      limits.memory: <hard_value>
      requests.cpu: <hard_value>
      requests.memory: <hard_value>
```

Please, refer to [ResourceQuota](https://kubernetes.io/docs/concepts/policy/resource-quotas/) documentation for the subject.

The assigned quota are inherited by any namespace created in the tenant

```yaml
kind: ResourceQuota
apiVersion: v1
metadata:
  name: compute
  namespace:
  labels:
    capsule.clastix.io/resource-quota=0
    capsule.clastix.io/tenant=tenant  
  annotations:
    # used resources in the tenant
    quota.capsule.clastix.io/used-limits.cpu=<tenant_used_value>       
    quota.capsule.clastix.io/used-limits.memory=<tenant_used_value>
    quota.capsule.clastix.io/used-requests.cpu=<tenant_used_value>
    quota.capsule.clastix.io/used-requests.memory=<tenant_used_value>
    # hard quota for the tenant
    quota.capsule.clastix.io/hard-limits.cpu=<tenant_hard_value>       
    quota.capsule.clastix.io/hard-limits.memory=<tenant_hard_value>
    quota.capsule.clastix.io/hard-requests.cpu=<tenant_hard_value>
    quota.capsule.clastix.io/hard-requests.memory=<tenant_hard_value>
spec:
  hard:
    limits.cpu: <hard_value>
    limits.memory: <hard_value>
    requests.cpu: <hard_value>
    requests.memory: <hard_value>
status:
  hard:
    limits.cpu: <namespace_hard_value>
    limits.memory: <namespace_hard_value>
    requests.cpu: <namespace_hard_value>
    requests.memory: <namespace_hard_value>
  used:
    limits.cpu: <namespace_used_value>
    limits.memory: <namespace_used_value>
    requests.cpu: <namespace_used_value>
    requests.memory: <namespace_used_value>
```

The Capsule operator aggregates _ResourceQuota_ at tenant level, so that the hard quota is never crossed for the given tenant. This permits the tenant owner to consume resources in the tenant regardless of the namespace.

The annotations

```yaml
quota.capsule.clastix.io/used-<resource>=<tenant_used_value>
quota.capsule.clastix.io/hard-<resource>=<tenant_hard_value>
```

are updated in realtime by Capsule, according to the actual aggredated usage of resource in the tenant.

> NB:
> While Capsule controls quota at tenant level, at namespace level the quota enforcement
> is under the control of the default _ResourceQuota Admission Controller_ enabled on the
> Kubernetes API server using the flag `--enable-admission-plugins=ResourceQuota`.

The tenant owner is not allowed to change or remove the _ResourceQuota_ from the namespace.

#### limitRanges
Field `limitRanges` specifies the _LimitRanges_ assigned to the tenant.

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: tenant
spec:
  limitRanges:
  - limits:
    - type: Pod
      max:
        cpu: <value>
        memory: <value>
      min:
        cpu: <value>
        memory: <value> 
    - type: Container
      default:
        cpu: <value>
        memory: <value>
      defaultRequest:
        cpu: <value>
        memory: <value>
      max:
        cpu: <value>
        memory: <value>
      min:
        cpu: <value>
        memory: <value>          
    - type: PersistentVolumeClaim
      max:
        storage: <value>
      min:
        storage: <value>    
```

Please, refer to [LimitRange](https://kubernetes.io/docs/concepts/policy/limit-range/) documentation for the subject.

The assigned _LimitRanges_ are inherited by any namespace created in the tenant

```yaml
kind: LimitRange
apiVersion: v1
metadata:
  name: <name>
  namespace:
spec:
  limits:
  - type: Pod
    max:
      cpu: <value>
      memory: <value>
    min:
      cpu: <value>
      memory: <value> 
  - type: Container
    default:
      cpu: <value>
      memory: <value>
    defaultRequest:
      cpu: <value>
      memory: <value>
    max:
      cpu: <value>
      memory: <value>
    min:
      cpu: <value>
      memory: <value>          
  - type: PersistentVolumeClaim
    max:
      storage: <value>
    min:
      storage: <value>  
```

> NB:
> Limit ranges enforcement for a single pod, container, and persistent volume
> claim is done by the default _LimitRanger Admission Controller_ enabled on
> the Kubernetes API server: using the flag
> `--enable-admission-plugins=LimitRanger`.

Being the limit range specific of single resources, there is no aggregate to count.

The tenant owner is not allowed to change or remove _LimitRanges_ from the namespace.

#### networkPolicies
Field `networkPolicies` specifies the _NetworkPolicies_ assigned to the tenant.

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: tenant
spec:
  networkPolicies:
  - policyTypes:
    - Ingress
    - Egress
    egress:
    - to:
      - ipBlock:
          cidr: <value>
    ingress:
    - from:
      - namespaceSelector: {}
      - podSelector: {}
      - ipBlock:
          cidr: <value>
    podSelector: {}
```

Please, refer to [NetworkPolicies](https://kubernetes.io/docs/concepts/services-networking/network-policies/) documentation for the subjects of a _NetworkPolicy_.

The assigned _NetworkPolicies_ are inherited by any namespace created in the tenant.

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: <name>
  namespace:
spec:
  podSelector: {}
  ingress:
  - from:
    - namespaceSelector: {}
    - podSelector: {}
    - ipBlock:
        cidr: <value>
  egress:
  - to:
    - ipBlock:
        cidr: <value>
  policyTypes:
  - Ingress
  - Egress
```

The tenant owner can create, patch and delete additional _NetworkPolicy_ to refine the assigned one. However, the tenant owner cannot delete the _NetworkPolicies_ set at tenant level.

#### externalServiceIPs
Field `externalServiceIPs` specifies the external IPs that can be used in _Services_ with type `ClusterIP`.

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: tenant
spec:
  externalServiceIPs:
    allowed:
    - <cidr>
```

Capsule will ensure that all _Services_ in the tenant can contain only the allowed external IPs. This mitigate the [_CVE-2020-8554_] vulnerability where a potential attacker, able to create a _Service_ with type `ClusterIP` and set the `externalIPs` field, can intercept traffic to that IP. Leave only the allowed CIDRs list to be set as `externalIPs` field in a _Service_ with type `ClusterIP`.

To prevent users to set the `externalIPs` field, use an empty allowed list:

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: tenant
spec:
  externalServiceIPs:
    allowed: []
```

> NB: Missing of this controller, it exposes your cluster to the vulnerability [_CVE-2020-8554_].

### Status
#### size
Status field `size` reports the number of namespaces belonging to the tenant. It is reported as `NAMESPACE COUNT` in the `kubectl` output:

```
$ kubectl get tnt
NAME      NAMESPACE QUOTA   NAMESPACE COUNT   OWNER NAME   OWNER KIND   NODE SELECTOR     AGE
cap       9                 1                 joe          User         {"pool":"cmp"}    5d4h
gas       6                 2                 alice        User         {"node":"worker"} 5d4h
oil       9                 3                 alice        User         {"pool":"cmp"}    5d4h
sample    9                 0                 alice        User         {"key":"value"}   29h
```

#### namespaces
Status field `namespaces` reports the list of all namespaces belonging to the tenant.

```yaml
...
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: tenant
spec:
...
status:
  namespaces: 
    oil-development
    oil-production
    oil-marketing
  size: 3
```

## Role Based Access Control
In the current implementation, the Capsule operator requires cluster admin permissions to fully operate.

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
The Capsule operator provides following command options:

Option | Description | Default
--- | --- | ---
`--metrics-addr` | The address and port where `/metrics` are exposed. | `127.0.0.1:8080`
`--enable-leader-election` | Start a leader election client and gain leadership before executing the main loop. | `true`
`--force-tenant-prefix` | Force the tenant name as prefix for namespaces: `<tenant_name>-<namespace>`.  | `false`
`--zap-log-level` | The log verbosity with a value from 1 to 10 or the basic keywords.  | `4`
`--zap-devel` | The flag to get the stack traces for deep debugging.  | `null`
`--capsule-user-group` | Override the Capsule group to which all tenant owners must belong. | `capsule.clastix.io`
`--protected-namespace-regex` | Disallows creation of namespaces matching the passed regexp. | `null`

## Created Resources
Once installed, the Capsule operator creates the following resources in your cluster:

```
NAMESPACE       RESOURCE
                customresourcedefinition.apiextensions.k8s.io/tenants.capsule.clastix.io
                clusterrole.rbac.authorization.k8s.io/capsule-proxy-role
                clusterrole.rbac.authorization.k8s.io/capsule-metrics-reader
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