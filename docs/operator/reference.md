# Reference

## Custom Resource Definition
Capsule operator uses a single Custom Resources Definition (CRD) for _Tenants_. An instance of _Tenant_ has the following structure:

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: tenant
  labels:
  annotations:
spec:
  owner:                  # required
  nodeSelector:
  namespaceQuota:         # required
  namespacesMetadata:
  servicesMetadata:
  ingressClasses:         # required
  storageClasses:         # required
  containerRegistries:
  additionalRoleBindings:
  resourceQuotas:
  limitRanges:            # required 
  networkPolicies:
status:
  namespaces:
  size:
```

In Caspule, Tenants are cluster wide resources. You need for cluster wide permissions to work with tenants.

```
$ kubectl get tenants
NAME      NAMESPACE QUOTA   NAMESPACE COUNT   OWNER NAME   OWNER KIND   NODE SELECTOR     AGE
cap       9                 1                 joe          User         {"pool":"cmp"}    5d4h
gas       6                 2                 alice        User         {"node":"worker"} 5d4h
oil       9                 4                 alice        User         {"pool":"cmp"}    5d4h
sample    9                 0                 alice        User         {"key":"value"}   29h
```

using the short name:

```
$ kubectl get tnt
NAME      NAMESPACE QUOTA   NAMESPACE COUNT   OWNER NAME   OWNER KIND   NODE SELECTOR     AGE
cap       9                 1                 joe          User         {"pool":"cmp"}    5d4h
gas       6                 2                 alice        User         {"node":"worker"} 5d4h
oil       9                 4                 alice        User         {"pool":"cmp"}    5d4h
sample    9                 0                 alice        User         {"key":"value"}   29h
```

Follows a reference for all the fields in the Tenant resource.

### name
Metadata `name` can contain any valid symbol from the regex: `[a-z0-9]([-a-z0-9]*[a-z0-9])?`.

### owner
Field `owner` specify the ownership of the tenant:

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

### nodeSelector
Field `nodeSelector` specify the label to control the placement of pods on a given pool of worker nodes:

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: tenant
spec:
  nodeSelector:
    key: value
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
    key: value
```

Please, see how to [Assigning Pods to Nodes](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/) documentation.

The tenant owner is not allowed to change or remove the annotation from the namespace.

### namespaceQuota
Field `namespaceQuota` specify the maximum number of namespaces allowed for that tenant.

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: tenant
spec:
  namespaceQuota: 10
```
Once the namespace quota assigned to the tenant has been reached, yhe tenant owner cannot create further namespaces.

### namespacesMetadata
Field `namespacesMetadata` specify additional labels and annotations the Capsule operator places on any _Namespace_ in the tenant.

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
      key: value
```

Al namespaces in the tenant will have:

```yaml
kind: Namespace
apiVersion: v1
metadata:
  annotations:
    <annotations>
  labels:
    key: value
```

The tenant owner is not allowed to change or remove such labels and annotations from the namespace.

### servicesMetadata
Field `servicesMetadata` specify additional labels and annotations the Capsule operator places on any _Service_ in the tenant.

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
      key: value
```

Al services in the tenant will have:

```yaml
kind: Service
apiVersion: v1
metadata:
  annotations:
    <annotations>
  labels:
    key: value
```

The tenant owner is not allowed to change or remove such labels and annotations from the service objects.

### ingressClasses
Field `ingressClasses` specify the Ingress Classes assigned to the tenant.

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: tenant
spec:
  ingressClasses:
     allowed:
     - <class>
     allowedRegex: "^.*$"
```

Capsule assures that all _Ingresses_ resources created in the tenant can use only one of the allowed Ingress Classes.

```yaml
apiVersion: extensions/v1beta1
kind: Ingress
metadata:
  name: <name>
  namespace:
  annotations:
    kubernetes.io/ingress.class: <class>
```
> Ingress resources are supported in both the versions, `networking.k8s.io/v1beta1` and `networking.k8s.io/v1`.

Allowed Ingress Classes are reported into namespaces as annotations, so the tenant owner can check them

```yaml
kind: Namespace
apiVersion: v1
metadata:
  annotations:
    capsule.clastix.io/ingress-classes: <class>
```
Any tentative of tenant owner to use a not allowed Ingress Class will fail.

### storageClasses
Field `storageClasses` specify the Storage Classes assigned to the tenant.

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: tenant
spec:
  storageClasses:
     allowed:
     - <class>
     allowedRegex: "^.*$"
```

Capsule assures that all _PersistentVolumeClaim_ resources created in the tenant can use only one of the allowed Storage Classes.

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: <name>
  namespace:
spec:
  storageClassName: <class>
```

Allowed Ingress Classes are reported into namespaces as annotations, so the tenant owner can check them

```yaml
kind: Namespace
apiVersion: v1
metadata:
  annotations:
    capsule.clastix.io/storage-classes: <class>
```

Any tentative of tenant owner to use a not allowed Storage Class will fail.

### containerRegistries
Field `containerRegistries` specify the Trusted Image Registries assigned to the tenant.

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: tenant
spec:
  containerRegistries:
     allowed:
     - <registry>
     allowedRegex: "^.*$"
```

Capsule assures that all _Pods_ resources created in the tenant can use only one of the allowed trusted registries.

Allowed Registries are reported into namespaces as annotations, so the tenant owner can check them

```yaml
kind: Namespace
apiVersion: v1
metadata:
  annotations:
    capsule.clastix.io/registries: <registry>
```

Any tentative of tenant owner to use a not allowed registry will fail.

> In case of naked and official images hosted on Docker Hub, Capsule is going
> to retrieve the registry even if it's not explicit: a `busybox:latest` Pod
> running on a Tenant allowing `docker.io` will not blocked, even if the image
> field is not explicit as `docker.io/busybox:latest`.

### additionalRoleBindings
Field `additionalRoleBindings` specify additional _RoleBindings_ assigned to the tenant.

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

### resourceQuotas
Field `resourceQuotas` specify a list of _ResourceQuota_ resources assigned to the tenant.

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

Please, refer to [ResourceQuota](https://kubernetes.io/docs/concepts/policy/resource-quotas/) documentation for the resource subjects.

The assigned quotas is inherited by any namespace created in the tenant

```yaml
kind: ResourceQuota
apiVersion: v1
metadata:
  name: compute
  namespace:
  labels:
    capsule.clastix.io/resource-quota=0
    capsule.clastix.io/tenant=tenant  
  annotations: # used resources in the tenant as aggregate
    quota.capsule.clastix.io/used-limits.cpu=<tenant_used_value>       
    quota.capsule.clastix.io/used-limits.memory=<tenant_used_value>
    quota.capsule.clastix.io/used-requests.cpu=<tenant_used_value>
    quota.capsule.clastix.io/used-requests.memory=<tenant_used_value>
spec:
  hard:
    limits.cpu: <hard_value>
    limits.memory: <hard_value>
    requests.cpu: <hard_value>
    requests.memory: <hard_value>
status:
  hard:
    limits.cpu: <hard_value>
    limits.memory: <hard_value>
    requests.cpu: <hard_value>
    requests.memory: <hard_value>
  used:
    limits.cpu: <namespace_used_value>
    limits.memory: <namespace_used_value>
    requests.cpu: <namespace_used_value>
    requests.memory: <namespace_used_value>
```

The Capsule operator aggregates ResourceQuota at tenant level, so that the hard quota limit is never crossed for the given tenant. This permits the tenant owner to consume resources in the tenant regardless of the namespace.

The annotations `quota.capsule.clastix.io/used-limits.resource=<tenant_used_value>` are updated in realtime by Capsule, according to the actual aggredated usage of resource in the tenant.

> Nota Bene:
> while Capsule controls quota at tenant level, at namespace level the quota enforcement is under the control of the default _ResourceQuota Admission Controller_ enabled on the Kubernetes API server using the flag `--enable-admission-plugins=ResourceQuota`.

The tenant owner is not allowed to change or remove ResourceQuota from the namespace.

### limitRanges
Field `limitRanges` specify the _LimitRanges_ assigned to the tenant.

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

Please, refer to [LimitRange](https://kubernetes.io/docs/concepts/policy/limit-range/) documentation for the types of limits you can assign.

The assigned LimitRange is inherited by any namespace created in the tenant

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

> Nota Bene:
> Limit ranges enforcement for a single pod, container, and persistent volume
> claim is done by the default _LimitRanger Admission Controller_ enabled on
> the Kubernetes API server: using the flag
> `--enable-admission-plugins=LimitRanger`.

Being the limit range specific of single resources:

- Pod
- Container
- Persistent Volume Claim

there is no aggregate to count.

The tenant owner is not allowed to change or remove LimitRanges from the namespace.

### networkPolicies
Field `networkPolicies` specify the _NetworkPolicies_ assigned to the tenant.

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

Please, refer to [NetworkPolicies](https://kubernetes.io/docs/concepts/services-networking/network-policies/) documentation for the subjects of a NetworkPolicy.

The assigned NetworkPolicy is inherited by any namespace created in the tenant.

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

The tenant owner can create, patch and delete additional NetworkPolicy to refine the assigned one. However, the tenant owner cannot delete the NetworkPolicy set at tenant level.

## RBAC




## Admission Controller
Capsule implements Kubernetes multi-tenancy capabilities using a minimum set of standard [Admission Controllers](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/) enabled on the Kubernetes APIs server.

Here the list of required Admission Controllers you have to enable to get full support from Capsule:

* PodNodeSelector
* LimitRanger
* ResourceQuota
* MutatingAdmissionWebhook
* ValidatingAdmissionWebhook

In addition to the required controllers above, Capsule implements its own set through the [Dynamic Admission Controller](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/) mechanism, providing callbacks to add further validation or resource patching:

* capsule-mutating-webhook-configuration
* capsule-validating-webhook-configuration

## Command options
