# Implementing a multi-tenant scenario in Kubernetes

Capsule is a framework to implement multi-tenant and policy-driven scenarios in Kubernetes. In this tutorial, we'll focus on a hypothetical case covering the main features of the Capsule Operator.

***Acme Corp***, our sample organization, is building a Container as a Service platform (CaaS) to serve multiple lines of business, or departments, e.g. _Oil_, _Gas_, _Solar_, _Wind_, _Water_. Each department has its team of engineers that are responsible for the development, deployment, and operating of their digital products. We'll work with the following actors:

* ***Bill***: the cluster administrator from the operations department of _Acme Corp_.

* ***Alice***: the project leader in the _Oil_ & _Gas_ departments. She is responsible for a team made of different job responsibilities: e.g. developers, administrators, SRE engineers, etc.
  
* ***Joe***: works as a lead developer of a distributed team in Alice's organization.

* ***Bob***: is the head of engineering for the _Water_ department, the main and historical line of business at _Acme Corp_.


## Assign Tenant ownership

### User as tenant owner
Bill, the cluster admin, receives a new request from _Acme Corp_'s CTO asking for a new tenant to be onboarded and Alice user will be the tenant owner. Bill then assigns Alice's identity of `alice` in the _Acme Corp_. identity management system. Since Alice is a tenant owner, Bill needs to assign `alice` the Capsule group defined by `--capsule-user-group` option, which defaults to `capsule.clastix.io`.

To keep things simple, we assume that Bill just creates a client certificate for authentication using X.509 Certificate Signing Request, so Alice's certificate has `"/CN=alice/O=capsule.clastix.io"`.

Bill creates a new tenant `oil` in the CaaS management portal according to the tenant's profile:

```yaml
kubectl create -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: alice
    kind: User
EOF
```

Bill checks if the new tenant is created and operational:

```
kubectl get tenant oil
NAME   STATE    NAMESPACE QUOTA   NAMESPACE COUNT   NODE SELECTOR   AGE
oil    Active                     0                                 33m
```

> Note that namespaces are not yet assigned to the new tenant.
> The tenant owners are free to create their namespaces in a self-service fashion
> and without any intervention from Bill.

Once the new tenant `oil` is in place, Bill sends the login credentials to Alice.

Alice can log in using her credentials and check if she can create a namespace

```
kubectl auth can-i create namespaces
yes
``` 

or even delete the namespace

```
kubectl auth can-i delete ns -n oil-production
yes
```

However, cluster resources are not accessible to Alice

```
kubectl auth can-i get namespaces
no

kubectl auth can-i get nodes
no

kubectl auth can-i get persistentvolumes
no
```

including the `Tenant` resources

```
kubectl auth can-i get tenants
no
```

### Group of users as tenant owner
In the example above, Bill assigned the ownership of `oil` tenant to `alice` user. If another user, e.g. Bob needs to administer the `oil` tenant, Bill can assign the ownership of `oil` tenant to such user too:

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: alice
    kind: User
  - name: bob
    kind: User
EOF
```

However, it's more likely that Bill assigns the ownership of the `oil` tenant to a group of users instead of a single one. Bill creates a new group account `oil-users` in the Acme Corp. identity management system and then he assigns Alice and Bob identities to the `oil-users` group.

The tenant manifest is modified as in the following:

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: oil-users
    kind: Group
EOF
```

With the configuration above, any user belonging to the `oil-users` group will be the owner of the `oil` tenant with the same permissions of Alice. For example, Bob can log in with his credentials and issue

```
kubectl auth can-i create namespaces
yes
```

### Robot account as tenant owner

As GitOps methodology is gaining more and more adoption everywhere, it's more likely that an application (Service Account) should act as Tenant Owner. In Capsule, a Tenant can also be owned by a Kubernetes _ServiceAccount_ identity.

The tenant manifest is modified as in the following:

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: system:serviceaccount:tenant-system:robot
    kind: ServiceAccount
EOF
```

Bill can create a Service Account called `robot`, for example, in the `tenant-system` namespace and leave it to act as Tenant Owner of the `oil` tenant

```
kubectl --as system:serviceaccount:tenant-system:robot --as-group capsule.clastix.io auth can-i create namespaces
yes
```

The service account has to be part of Capsule group, so Bill has to set in the `CapsuleConfiguration`

```yaml
apiVersion: capsule.clastix.io/v1beta2
kind: CapsuleConfiguration
metadata:
  name: default
spec:
  userGroups:
  - system:serviceaccounts:tenant-system
```

since each service account in a namespace is a member of following group:

```
system:serviceaccounts:{service-account-namespace}
```

You can change the CapsuleConfiguration at install time with a helm parameter:
```
helm upgrade -i  \
  capsule \
  clastix/capsule \
  -n capsule-system \
  --set manager.options.capsuleUserGroups=system:serviceaccounts:tenant-system \
  --create-namespace
```

Or after installation:
```
kubectl patch capsuleconfigurations default \
  --patch '{"spec":{"userGroups":["capsule.clastix.io","system:serviceaccounts:tenant-system"]}}' \
  --type=merge
```

> Please, pay attention when setting a service account acting as tenant owner. Make sure you're not using the group `system:serviceaccounts` or the group `system:serviceaccounts:{capsule-namespace}` as Capsule group, otherwise you'll create a short-circuit in the Capsule controller, being Capsule itself controlled by a serviceaccount. 

### Roles assigned to Tenant Owners

By default, all Tenant Owners will be granted with two ClusterRole resources using the RoleBinding API:

1. the Kubernetes default one, [`admin`](https://kubernetes.io/docs/reference/access-authn-authz/rbac/#user-facing-roles), that grants most of the namespace scoped resources

2. a custom one, created by Capsule, named `capsule-namespace-deleter`, allowing to delete the created namespaces

In the example below, assuming the tenant owner creates a namespace `oil-production` in Tenant `oil`, you'll see the Role Bindings giving the tenant owner full permissions on the tenant namespaces:

```
$: kubectl get rolebindings -n oil-production
NAME                                      ROLE                                    AGE
capsule-oil-0-admin                       ClusterRole/admin                       6s
capsule-oil-1-capsule-namespace-deleter   ClusterRole/capsule-namespace-deleter   5s
```

When Alice creates the namespaces, the Capsule controller assigns to Alice the following permissions, so that Alice can act as the admin of all the tenant namespaces.

```yaml
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: capsule-oil-0-admin
  namespace: oil-production
subjects:
- kind: User
  name: alice
roleRef:
  kind: ClusterRole
  name: admin
  apiGroup: rbac.authorization.k8s.io
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: capsule-oil-1-capsule-namespace-deleter
  namespace: oil-production
subjects:
- kind: User
  name: alice
roleRef:
  kind: ClusterRole
  name: capsule-namespace-deleter
  apiGroup: rbac.authorization.k8s.io
```

In some cases, the cluster admin needs to narrow the range of permissions assigned to tenant owners by assigning a Cluster Role with less permissions than above. Capsule supports the dynamic assignment of any ClusterRole resources for each Tenant Owner.

For example, assign user `Joe` the tenant ownership with only [view](https://kubernetes.io/docs/reference/access-authn-authz/rbac/#user-facing-roles) permissions on tenant namespaces:

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: alice
    kind: User
  - name: joe
    kind: User
    clusterRoles:
      - view
EOF
```

you'll see the new Role Bindings assigned to Joe:

```
kubectl -n oil-production get rolebindings
NAME                                      ROLE                                    AGE
capsule-oil-0-admin                       ClusterRole/admin                       3s
capsule-oil-1-capsule-namespace-deleter   ClusterRole/capsule-namespace-deleter   3s
capsule-oil-2-view                        ClusterRole/view                        3s
```

so that Joe can only view resources in the tenant namespaces:

```
kubectl --as joe --as-group capsule.clastix.io auth can-i delete pods -n oil-marketing
no
```

> Please, note that, despite created with more restricted permissions, a tenant owner can still create namespaces in the tenant because he belongs to the `capsule.clastix.io` group.
> If you want a user not acting as tenant owner, but still operating in the tenant, you can assign additional `RoleBindings` without assigning him the tenant ownership.

Custom ClusterRoles are also supported. Assuming the cluster admin creates:

```yaml
kubectl apply -f - << EOF
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: prometheus-servicemonitors-viewer
rules:
- apiGroups: ["monitoring.coreos.com"]
  resources: ["servicemonitors"]
  verbs: ["get", "list", "watch"]
EOF
```

These permissions can be granted to Joe

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: alice
    kind: User
  - name: joe
    kind: User
    clusterRoles:
      - view
      - prometheus-servicemonitors-viewer
EOF
```

For the given configuration, the resulting RoleBinding resources are the following ones:

```
kubectl -n oil-production get rolebindings
NAME                                              ROLE                                            AGE
capsule-oil-0-admin                               ClusterRole/admin                               90s
capsule-oil-1-capsule-namespace-deleter           ClusterRole/capsule-namespace-deleter           90s
capsule-oil-2-view                                ClusterRole/view                                90s
capsule-oil-3-prometheus-servicemonitors-viewer   ClusterRole/prometheus-servicemonitors-viewer   25s
```

### Assign additional Role Bindings
The tenant owner acts as admin of tenant namespaces. Other users can operate inside the tenant namespaces with different levels of permissions and authorizations. 

Assuming the cluster admin creates:

```yaml
kubectl apply -f - << EOF
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: prometheus-servicemonitors-viewer
rules:
- apiGroups: ["monitoring.coreos.com"]
  resources: ["servicemonitors"]
  verbs: ["get", "list", "watch"]
EOF
```

These permissions can be granted to a user without giving the role of tenant owner:

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: alice
    kind: User
  additionalRoleBindings:
  - clusterRoleName: 'prometheus-servicemonitors-viewer'
    subjects:
    - apiGroup: rbac.authorization.k8s.io
      kind: User
      name: joe
EOF
```

## Create namespaces
Alice, once logged with her credentials, can create a new namespace in her tenant, as simply issuing:

```
kubectl create ns oil-production
```

Alice started the name of the namespace prepended by the name of the tenant: this is not a strict requirement but it is highly suggested because it is likely that many different tenants would like to call their namespaces `production`, `test`, or `demo`, etc.

The enforcement of this naming convention is optional and can be controlled by the cluster administrator with the `spec.forceTenantPrefix` option for the loaded `CapsuleConfiguration`.

> For more information, please, refer to the [`CapsuleConfiguration` API CRD](https://capsule.clastix.io/docs/general/crds-apis/#capsuleconfigurationspec-1).

Alice can deploy any resource in any of the namespaces

```
kubectl -n oil-development run nginx --image=docker.io/nginx 
kubectl -n oil-development get pods
```

The cluster admin, can control how many namespaces Alice, creates by setting a quota in the tenant manifest `spec.namespaceOptions.quota`

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: alice
    kind: User
  namespaceOptions:
    quota: 3
EOF
```

Alice can create additional namespaces according to the quota:

```
kubectl create ns oil-development
kubectl create ns oil-test
```

While Alice creates namespaces, the Capsule controller updates the status of the tenant so Bill, the cluster admin, can check the status:

```
kubectl describe tenant oil
```

```yaml
...
status:
  Namespaces:
    oil-development
    oil-production
    oil-test
  Size:   3 # current namespace count
  State:  Active
...
```

Once the namespace quota assigned to the tenant has been reached, Alice cannot create further namespaces

```
kubectl create ns oil-training
Error from server (Cannot exceed Namespace quota: please, reach out to the system administrators):
admission webhook "namespace.capsule.clastix.io" denied the request.
```
The enforcement on the maximum number of namespaces per Tenant is the responsibility of the Capsule controller via its Dynamic Admission Webhook capability.

## Assign multiple tenants
A single team is likely responsible for multiple lines of business. For example, in our sample organization Acme Corp., Alice is responsible for both the Oil and Gas lines of business. It's more likely that Alice requires two different tenants, for example, `oil` and `gas` to keep things isolated.

By design, the Capsule operator does not permit a hierarchy of tenants, since all tenants are at the same levels. However, we can assign the ownership of multiple tenants to the same user or group of users.

Bill, the cluster admin, creates multiple tenants having `alice` as owner:

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: alice
    kind: User
EOF
```

and

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: gas
spec:
  owners:
  - name: alice
    kind: User
EOF
```

Alternatively, the ownership can be assigned to a group called `oil-and-gas`:

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: oil-and-gas
    kind: Group
EOF
```

and

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: gas
spec:
  owners:
  - name: oil-and-gas
    kind: Group
EOF
```

The two tenants remain isolated from each other in terms of resources assignments, e.g. _ResourceQuota_, _Nodes Pool_, _Storage Classes_ and _Ingress Classes_, and in terms of governance, e.g. _NetworkPolicies_, _PodSecurityPolicies_, _Trusted Registries_, etc.


When Alice logs in, she has access to all namespaces belonging to both the `oil` and `gas` tenants.

```
kubectl create ns oil-production
kubectl create ns gas-production
```

When the enforcement of the naming convention with the `forceTenantPrefix` option is enabled, the namespaces are automatically assigned to the right tenant by Capsule because the operator does a lookup on the tenant names. If the `forceTenantPrefix` option, is not set,  Alice needs to specify the tenant name as a label `capsule.clastix.io/tenant=<desired_tenant>` in the namespace manifest:

```yaml
kubectl apply -f - << EOF
kind: Namespace
apiVersion: v1
metadata:
  name: gas-production
  labels:
    capsule.clastix.io/tenant: gas
EOF
```

If not specified, Capsule will deny with the following message: `Unable to assign namespace to tenant. Please use capsule.clastix.io/tenant label when creating a namespace.`

## Assign resources quota
With help of Capsule, Bill, the cluster admin, can set and enforce resources quota and limits for Alice's tenant.

Set resources quota for each namespace in the Alice's tenant by defining them in the tenant spec:

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: alice
    kind: User
  namespaceOptions:
    quota: 3
  resourceQuotas:
    scope: Tenant
    items:
    - hard:
        limits.cpu: "8"
        limits.memory: 16Gi
        requests.cpu: "8"
        requests.memory: 16Gi
    - hard:
        pods: "10"
  limitRanges:
    items:
    - limits:
      - default:
          cpu: 500m
          memory: 512Mi
        defaultRequest:
          cpu: 100m
          memory: 10Mi
        type: Container
EOF
```

The resource quotas above will be inherited by all the namespaces created by Alice. In our case, when Alice creates the namespace `oil-production`, Capsule creates the following resource quotas:

```yaml
kind: ResourceQuota
apiVersion: v1
metadata:
  name: capsule-oil-0
  namespace: oil-production
  labels:
    tenant: oil
spec:
  hard:
    limits.cpu: "8"
    limits.memory: 16Gi
    requests.cpu: "8"
    requests.memory: 16Gi
---
kind: ResourceQuota
apiVersion: v1
metadata:
  name: capsule-oil-1
  namespace: oil-production
  labels:
    tenant: oil
spec:
  hard:
    pods : "10"
```

Alice can create any resource according to the assigned quotas:

```
kubectl -n oil-production create deployment nginx --image nginx:latest --replicas 4
```

At namespace `oil-production` level, Alice can see the used resources by inspecting the `status` in ResourceQuota:

```yaml
kubectl -n oil-production get resourcequota capsule-oil-1 -o yaml
...
status:
  hard:
    pods: "10"
    services: "50"
  used:
    pods: "4"
```

At tenant level, the behaviour is controlled by the `spec.resourceQuotas.scope` value:

* Tenant (default)
* Namespace

### Enforcement at tenant level
By setting enforcement at tenant level, i.e. `spec.resourceQuotas.scope=Tenant`, Capsule aggregates resources usage for all namespaces in the tenant and adjusts all the `ResourceQuota` usage as aggregate. In such case, Alice can check the used resources at the tenant level by inspecting the `annotations` in ResourceQuota object of any namespace in the tenant:

```yaml
kubectl -n oil-production get resourcequotas capsule-oil-1 -o yaml
apiVersion: v1
kind: ResourceQuota
metadata:
  annotations:
    quota.capsule.clastix.io/used-pods: "4"
    quota.capsule.clastix.io/hard-pods: "10"
...
```

or

```yaml
kubectl -n oil-development get resourcequotas capsule-oil-1 -o yaml
apiVersion: v1
kind: ResourceQuota
metadata:
  annotations:
    quota.capsule.clastix.io/used-pods: "4"
    quota.capsule.clastix.io/hard-pods: "10"
...
```

When the aggregate usage for all namespaces crosses the hard quota, then the native `ResourceQuota` Admission Controller in Kubernetes denies Alice's request to create resources exceeding the quota: 

```
kubectl -n oil-development create deployment nginx --image nginx:latest --replicas 10
```

Alice cannot schedule more pods than the admitted at tenant aggregate level.

```
kubectl -n oil-development get pods
NAME                     READY   STATUS    RESTARTS   AGE
nginx-55649fd747-6fzcx   1/1     Running   0          12s
nginx-55649fd747-7q6x6   1/1     Running   0          12s
nginx-55649fd747-86wr5   1/1     Running   0          12s
nginx-55649fd747-h6kbs   1/1     Running   0          12s
nginx-55649fd747-mlhlq   1/1     Running   0          12s
nginx-55649fd747-t48s5   1/1     Running   0          7s
```

and 

```
kubectl -n oil-production get pods
NAME                     READY   STATUS    RESTARTS   AGE
nginx-55649fd747-52fsq   1/1     Running   0          22m
nginx-55649fd747-9q8n5   1/1     Running   0          22m
nginx-55649fd747-r8vzr   1/1     Running   0          22m
nginx-55649fd747-tkv7m   1/1     Running   0          22m
```

### Enforcement at namespace level

By setting enforcement at the namespace level, i.e. `spec.resourceQuotas.scope=Namespace`, Capsule does not aggregate the resources usage and all enforcement is done at the namespace level.

## Pods and containers limits

Bill, the cluster admin, can also set Limit Ranges for each namespace in Alice's tenant by defining limits for pods and containers in the tenant spec:

```yaml
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
...
  limitRanges:
    items:
      - limits:
          - type: Pod
            min:
              cpu: "50m"
              memory: "5Mi"
            max:
              cpu: "1"
              memory: "1Gi"
      - limits:
          - type: Container
            defaultRequest:
              cpu: "100m"
              memory: "10Mi"
            default:
              cpu: "200m"
              memory: "100Mi"
            min:
              cpu: "50m"
              memory: "5Mi"
            max:
              cpu: "1"
              memory: "1Gi"
      - limits:
          - type: PersistentVolumeClaim
            min:
              storage: "1Gi"
            max:
              storage: "10Gi"
```

Limits will be inherited by all the namespaces created by Alice. In our case, when Alice creates the namespace `oil-production`, Capsule creates the following:
 
```yaml
apiVersion: v1
kind: LimitRange
metadata:
  name: capsule-oil-0
  namespace: oil-production
spec:
  limits:
    - max:
        cpu: "1"
        memory: 1Gi
      min:
        cpu: 50m
        memory: 5Mi
      type: Pod
---
apiVersion: v1
kind: LimitRange
metadata:
  name: capsule-oil-1
  namespace: oil-production
spec:
  limits:
    - default:
        cpu: 200m
        memory: 100Mi
      defaultRequest:
        cpu: 100m
        memory: 10Mi
      max:
        cpu: "1"
        memory: 1Gi
      min:
        cpu: 50m
        memory: 5Mi
      type: Container
---
apiVersion: v1
kind: LimitRange
metadata:
  name: capsule-oil-2
  namespace: oil-production
spec:
  limits:
    - max:
        storage: 10Gi
      min:
        storage: 1Gi
      type: PersistentVolumeClaim
```

> Note: being the limit range specific of single resources, there is no aggregate to count.

Alice doesn't have permission to change or delete the resources according to the assigned RBAC profile.

```
kubectl -n oil-production auth can-i patch resourcequota
no
kubectl -n oil-production auth can-i delete resourcequota
no
kubectl -n oil-production auth can-i patch limitranges
no
kubectl -n oil-production auth can-i delete limitranges
no
```


## Assign Pod Priority Classes

Pods can have priority. Priority indicates the importance of a Pod relative to other Pods. If a Pod cannot be scheduled, the scheduler tries to preempt (evict) lower priority Pods to make scheduling of the pending Pod possible. See [Kubernetes documentation](https://kubernetes.io/docs/concepts/scheduling-eviction/pod-priority-preemption/). 

In a multi-tenant cluster, not all users can be trusted, as a tenant owner could create Pods at the highest possible priorities, causing other Pods to be evicted/not get scheduled.

To prevent misuses of Pod Priority Class, Bill, the cluster admin, can enforce the allowed Pod Priority Class at tenant level:

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: alice
    kind: User
  priorityClasses:
    allowed:
    - custom
    allowedRegex: "^tier-.*$"
    matchLabels:
      env: "production"
EOF
```

With the said Tenant specification, Alice can create a Pod resource if `spec.priorityClassName` equals to:

- `custom`
- `tier-gold`, `tier-silver`, or `tier-bronze`, since these compile the allowed regex. 
- Any PriorityClass which has the label `env` with the value `production`

If a Pod is going to use a non-allowed _Priority Class_, it will be rejected by the Validation Webhook enforcing it.

### Assign Pod Priority Class as tenant default

It's possible to assign each tenant a PriorityClass which will be used, if no PriorityClass is set on pod basis:

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: alice
    kind: User
  priorityClasses:
    allowed:
    - custom
    default: "tenant-default"
    allowedRegex: "^tier-.*$"
    matchLabels:
      env: "production"
EOF
```

Here's how the new PriorityClass could look like

```yaml
kubectl apply -f - << EOF
apiVersion: scheduling.k8s.io/v1
kind: PriorityClass
metadata:
  name: tenant-default
value: 1313
preemptionPolicy: Never
globalDefault: false
description: "This is the default PriorityClass for the oil-tenant"
EOF
```

If a Pod has no value for `spec.priorityClassName`, the default value for PriorityClass (`tenant-default`) will be used.

> This feature allows specifying a custom default value on a Tenant basis, bypassing the global cluster default (`globalDefault=true`) that acts only at the cluster level.

**Note**: This feature supports type `PriorityClass` only on API version `scheduling.k8s.io/v1`

## Assign Pod Runtime Classes

Pods can be assigned different runtime classes. With the assigned runtime you can control Container Runtime Interface (CRI) is used for each pod.
See [Kubernetes documentation](https://kubernetes.io/docs/concepts/containers/runtime-class/) for more information. 

To prevent misuses of Pod Runtime Classes, Bill, the cluster admin, can enforce the allowed Pod Runtime Class at tenant level:

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: alice
    kind: User
  runtimeClasses:
    allowed:
    - legacy
    allowedRegex: "^hardened-.*$"
    matchLabels:
      env: "production"
EOF
```

With the said Tenant specification, Alice can create a Pod resource if `spec.runtimeClassName` equals to:

- `legacy`
- e.g.: `hardened-crio` or `hardened-containerd`, since these compile the allowed regex (`^hardened-.*$"`).
- any RuntimeClass which has the label `env` with the value `production`

If a Pod is going to use a non-allowed _Runtime Class_, it will be rejected by the Validation Webhook enforcing it.

## Assign Nodes Pool
Bill, the cluster admin, can dedicate a pool of worker nodes to the `oil` tenant, to isolate the tenant applications from other noisy neighbors.

These nodes are labeled by Bill as `pool=oil`

```
kubectl get nodes --show-labels

NAME                      STATUS   ROLES             AGE   VERSION   LABELS
...
worker06.acme.com         Ready    worker            8d    v1.18.2   pool=oil
worker07.acme.com         Ready    worker            8d    v1.18.2   pool=oil
worker08.acme.com         Ready    worker            8d    v1.18.2   pool=oil
```

The label `pool=oil` is defined as node selector in the tenant manifest:

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: alice
    kind: User
  nodeSelector:
    pool: oil
    kubernetes.io/os: linux
EOF
```

The Capsule controller makes sure that any namespace created in the tenant has the annotation: `scheduler.alpha.kubernetes.io/node-selector: pool=oil`. This annotation tells the scheduler of Kubernetes to assign the node selector `pool=oil` to all the pods deployed in the tenant. The effect is that all the pods deployed by Alice are placed only on the designated pool of nodes.

Multiple node selector labels can be defined as in the following snippet:

```yaml
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: alice
    kind: User
  nodeSelector:
    pool: oil
    kubernetes.io/os: linux
    kubernetes.io/arch: amd64
    hardware: gpu
```

Any attempt of Alice to change the selector on the pods will result in an error from the `PodNodeSelector` Admission Controller plugin.

Also, RBAC prevents Alice to change the annotation on the namespace:

```
kubectl auth can-i edit ns -n oil-production
no
```

## Assign Ingress Classes
An Ingress Controller is used in Kubernetes to publish services and applications outside of the cluster. An Ingress Controller can be provisioned to accept only Ingresses with a given Ingress Class.

Bill can assign a set of dedicated Ingress Classes to the `oil` tenant to force the applications in the `oil` tenant to be published only by the assigned Ingress Controller: 

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: alice
    kind: User
  ingressOptions:
    allowedClasses:
      allowed:
      - legacy
      allowedRegex: ^\w+-lb$
      matchLabels:
        env: "production"
EOF
```

With the said Tenant specification, Alice can create a Ingress resource if `spec.ingressClassName` or `metadata.annotations."kubernetes.io/ingress.class"` equals to:

- `legacy`
- eg. `haproxy-lb` or `nginx-lb`, since these compile the allowed regex (`^\w+-lb$`).
- Any IngressClass which has the label `env` with the value `production`

If an Ingress is going to use a non-allowed _IngressClass_, it will be rejected by the Validation Webhook enforcing it.

Alice can create an Ingress using only an allowed Ingress Class:

```yaml
kubectl -n oil-production apply -f - << EOF
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: nginx
  namespace: oil-production
  annotations:
    kubernetes.io/ingress.class: legacy
spec:
  rules:
  - host: oil.acmecorp.com
    http:
      paths:
      - backend:
          service:
            name: nginx
            port:
              number: 80
        path: /
        pathType: ImplementationSpecific
EOF
```

Any attempt of Alice to use a non-valid Ingress Class, or missing it, is denied by the Validation Webhook enforcing it.

### Assign Ingress Class as tenant default

It's possible to assign each tenant an Ingress Class which will be used, if a class is not set on ingress basis:

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: alice
    kind: User
  ingressOptions:
    allowedClasses:
      allowed:
      - legacy
      default: "tenant-default"
      allowedRegex: ^\w+-lb$
      matchLabels:
        env: "production"
EOF
```

Here's how the Tenant default IngressClass could look like:

```yaml
kubectl apply -f - << EOF
apiVersion: networking.k8s.io/v1
kind: IngressClass
metadata:
  labels:
    app.kubernetes.io/component: controller
  name: tenant-default
  annotations:
    ingressclass.kubernetes.io/is-default-class: "false"
spec:
  controller: k8s.io/customer-nginx
EOF
```

If an Ingress has no value for `spec.ingressClassName` or `metadata.annotations."kubernetes.io/ingress.class"`, the `tenant-default` IngressClass is automatically applied to the Ingress resource.

> This feature allows specifying a custom default value on a Tenant basis, bypassing the global cluster default (with the annotation `metadata.annotations.ingressclass.kubernetes.io/is-default-class=true`) that acts only at the cluster level.
> 
> More information: [Default IngressClass](https://kubernetes.io/docs/concepts/services-networking/ingress/#default-ingress-class)

**Note**: This feature is offered only by API type `IngressClass` in group `networking.k8s.io` version `v1`.
However, resource `Ingress` is supported in `networking.k8s.io/v1` and `networking.k8s.io/v1beta1`

## Assign Ingress Hostnames
Bill can control ingress hostnames in the `oil` tenant to force the applications to be published only using the given hostname or set of hostnames: 

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: alice
    kind: User
  ingressOptions:
    allowedHostnames:
      allowed:
        - oil.acmecorp.com
      allowedRegex: ^.*acmecorp.com$
EOF
```

The Capsule controller assures that all Ingresses created in the tenant can use only one of the valid hostnames.

Alice can create an Ingress using any allowed hostname

```yaml
kubectl apply -f - << EOF
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: nginx
  namespace: oil-production
  annotations:
    kubernetes.io/ingress.class: oil
spec:
  rules:
  - host: web.oil.acmecorp.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: nginx
            port:
              number: 80
EOF
```

Any attempt of Alice to use a non-valid hostname is denied by the Validation Webhook enforcing it.

## Control Hostname collision in Ingresses
In a multi-tenant environment, as more and more ingresses are defined, there is a chance of collision on the hostname leading to unpredictable behavior of the Ingress Controller. Bill, the cluster admin, can enforce hostname collision detection at different scope levels:

1. Cluster
2. Tenant
3. Namespace
4. Disabled (default)

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: alice
    kind: User
  - name: joe
    kind: User
  ingressOptions:
    hostnameCollisionScope: Tenant
EOF
```

When a tenant owner creates an Ingress resource, Capsule will check the collision of hostname in the current ingress with all the hostnames already used, depending on the defined scope.

For example, Alice, one of the tenant owners, creates an Ingress


```yaml
kubectl -n oil-production apply -f - << EOF
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: nginx
  namespace: oil-production
spec:
  rules:
  - host: web.oil.acmecorp.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: nginx
            port:
              number: 80
EOF
```

Another user, Joe creates an Ingress having the same hostname

```yaml
kubectl -n oil-development apply -f - << EOF
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: nginx
  namespace: oil-development
spec:
  rules:
  - host: web.oil.acmecorp.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: nginx
            port:
              number: 80
EOF
```

When a collision is detected at scope defined by `spec.ingressOptions.hostnameCollisionScope`, the creation of the Ingress resource will be rejected by the Validation Webhook enforcing it. When `hostnameCollisionScope=Disabled`, no collision detection is made at all.


## Assign Storage Classes
Persistent storage infrastructure is provided to tenants. Different types of storage requirements, with different levels of QoS, eg. SSD versus HDD, are available for different tenants according to the tenant's profile. To meet these different requirements, Bill, the cluster admin can provision different Storage Classes and assign them to the tenant:

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
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
    matchLabels:
      env: "production"
EOF
```

With the said Tenant specification, Alice can create a Persistent Volume Claims if `spec.storageClassName` equals to:

- `ceph-rbd` or `ceph-nfs`
- eg. `ceph-hdd` or `ceph-ssd`, since these compile the allowed regex (`^ceph-.*$`).
- Any IngressClass which has the label `env` with the value `production`

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

If a Persistent Volume Claim is going to use a non-allowed _Storage Class_, it will be rejected by the Validation Webhook enforcing it.

### Assign Storage Class as tenant default

It's possible to assign each tenant a StorageClass which will be used, if no value is set on Persistent Volume Claim basis:

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: alice
    kind: User
  storageClasses:
    default: "tenant-default"
    allowed:
    - ceph-rbd
    - ceph-nfs
    allowedRegex: "^ceph-.*$"
    matchLabels:
      env: "production"
EOF
```

Here's how the new Storage Class could look like

```yaml
kubectl apply -f - << EOF
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: tenant-default
  annotations:
    storageclass.kubernetes.io/is-default-class: "false"
provisioner: kubernetes.io/no-provisioner
volumeBindingMode: WaitForFirstConsumer
EOF
```

If a Persistent Volume Claim has no value for `spec.storageClassName` the `tenant-default` value will be used on new Persistent Volume Claim resources.

> This feature allows specifying a custom default value on a Tenant basis, bypassing the global cluster default (`.metadata.annotations.storageclass.kubernetes.io/is-default-class=true`) that acts only at the cluster level.
>
> See the [Default Storage Class](https://kubernetes.io/docs/tasks/administer-cluster/change-default-storage-class/) section on Kubernetes documentation.

**Note**: This feature supports type `StorageClass` only on API version `storage.k8s.io/v1`

## Assign Network Policies
Kubernetes network policies control network traffic between namespaces and between pods in the same namespace. Bill, the cluster admin, can enforce network traffic isolation between different tenants while leaving to Alice, the tenant owner, the freedom to set isolation between namespaces in the same tenant or even between pods in the same namespace.

To meet this requirement, Bill needs to define network policies that deny pods belonging to Alice's namespaces to access pods in namespaces belonging to other tenants, e.g. Bob's tenant `water`, or in system namespaces, e.g. `kube-system`.

> Keep in mind, that because of how the NetworkPolicies API works, the users can still add a policy which contradicts what the Tenant has set, resulting in users being able to circumvent the initial limitation set by the tenant admin.
>
> Two options can be put in place to mitigate this potential privilege escalation:
> 1. providing a restricted role rather than the default `admin` one
> 2. using Calico's `GlobalNetworkPolicy`, or Cilium's `CiliumClusterwideNetworkPolicy` which are defined at the cluster-level, thus creating an order of packet filtering.

Also, Bill can make sure pods belonging to a tenant namespace cannot access other network infrastructures like cluster nodes, load balancers, and virtual machines running other services.  

Bill can set network policies in the tenant manifest, according to the requirements:

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: alice
    kind: User
  networkPolicies:
    items:
    - policyTypes:
      - Ingress
      - Egress
      egress:
      - to:
        - ipBlock:
            cidr: 0.0.0.0/0
            except:
              - 192.168.0.0/16 
      ingress:
      - from:
        - namespaceSelector:
            matchLabels:
              capsule.clastix.io/tenant: oil
        - podSelector: {}
        - ipBlock:
            cidr: 192.168.0.0/16
      podSelector: {}
EOF
```

The Capsule controller, watching for namespace creation, creates the Network Policies for each namespace in the tenant.

Alice has access to network policies:

```
kubectl -n oil-production get networkpolicies
NAME            POD-SELECTOR   AGE
capsule-oil-0   <none>         42h
```

Alice can create, patch, and delete additional network policies within her namespaces

```
kubectl -n oil-production auth can-i get networkpolicies
yes

kubectl -n oil-production auth can-i delete networkpolicies
yes

kubectl -n oil-production auth can-i patch networkpolicies
yes
```

For example, she can create

```yaml
kubectl -n oil-production apply -f - << EOF
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  labels:
  name: production-network-policy
  namespace: oil-production
spec:
  podSelector: {}
  policyTypes:
  - Ingress
  - Egress
EOF
```

Check all the network policies

```
kubectl -n oil-production get networkpolicies
NAME                        POD-SELECTOR   AGE
capsule-oil-0               <none>         42h
production-network-policy   <none>         3m
```

And delete the namespace network policies

```
kubectl -n oil-production delete networkpolicy production-network-policy
```

Any attempt of Alice to delete the tenant network policy defined in the tenant manifest is denied by the Validation Webhook enforcing it.

## Enforce Pod container image PullPolicy

Bill is a cluster admin providing a Container as a Service platform using shared nodes.

Alice, a Tenant Owner, can start container images using private images: according to the Kubernetes architecture, the `kubelet` will download the layers on its cache.

Bob, an attacker, could try to schedule a Pod on the same node where Alice is running her Pods backed by private images: they could start new Pods using `ImagePullPolicy=IfNotPresent` and be able to start them, even without required authentication since the image is cached on the node. 

To avoid this kind of attack, Bill, the cluster admin, can force Alice, the tenant owner, to start her Pods using only the allowed values for `ImagePullPolicy`, enforcing the `kubelet` to check the authorization first.

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: alice
    kind: User
  imagePullPolicies:
  - Always
EOF
```

Allowed values are: `Always`, `IfNotPresent`, `Never`.

Any attempt of Alice to use a disallowed `imagePullPolicies` value is denied by the Validation Webhook enforcing it.


## Assign Trusted Images Registries
Bill, the cluster admin, can set a strict policy on the applications running into Alice's tenant: he'd like to allow running just images hosted on a list of specific container registries.

The spec `containerRegistries` addresses this task and can provide a combination with hard enforcement using a list of allowed values.


```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: alice
    kind: User
  containerRegistries:
    allowed:
    - docker.io
    - quay.io
    allowedRegex: 'internal.registry.\\w.tld'
```

> In case of Pod running `non-FQCI` (non fully qualified container image) containers, the container registry enforcement will disallow the execution.
> If you would like to run a `busybox:latest` container that is commonly hosted on Docker Hub, the Tenant Owner has to specify its name explicitly, like `docker.io/library/busybox:latest`.

A Pod running `internal.registry.foo.tld/capsule:latest` as registry will be allowed, as well `internal.registry.bar.tld` since these are matching the regular expression.

> A catch-all regex entry as `.*` allows every kind of registry, which would be the same result of unsetting `containerRegistries` at all.

Any attempt of Alice to use a not allowed `containerRegistries` value is denied by the Validation Webhook enforcing it.

## Create Custom Resources
Capsule grants admin permissions to the tenant owners but is only limited to their namespaces. To achieve that, it assigns the ClusterRole [admin](https://kubernetes.io/docs/reference/access-authn-authz/rbac/#user-facing-roles) to the tenant owner. This ClusterRole does not permit the installation of custom resources in the namespaces.

In order to leave the tenant owner to create Custom Resources in their namespaces, the cluster admin defines a proper Cluster Role. For example:

```yaml
kubectl apply -f - << EOF
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: argoproj-provisioner
rules:
- apiGroups:
  - argoproj.io
  resources:
  - applications
  - appprojects
  verbs:
  - create
  - get
  - list
  - watch
  - update
  - patch
  - delete
EOF
```

Bill can assign this role to any namespace in the Alice's tenant by setting it in the tenant manifest:

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: alice
    kind: User
  - name: joe
    kind: User
  additionalRoleBindings:
    - clusterRoleName: 'argoproj-provisioner'
      subjects:
        - apiGroup: rbac.authorization.k8s.io
          kind: User
          name: alice
        - apiGroup: rbac.authorization.k8s.io
          kind: User
          name: joe
EOF
```

With the given specification, Capsule will ensure that all Alice's namespaces will contain a _RoleBinding_ for the specified _Cluster Role_. For example, in the `oil-production` namespace, Alice will see:

```yaml
kind: RoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: capsule-oil-argoproj-provisioner
  namespace: oil-production
subjects:
  - kind: User
    apiGroup: rbac.authorization.k8s.io
    name: alice
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: argoproj-provisioner
```

With the above example, Capsule is leaving the tenant owner to create namespaced custom resources.

> Take Note: a tenant owner having the admin scope on its namespaces only, does not have the permission to create Custom Resources Definitions (CRDs) because this requires a cluster admin permission level. Only Bill, the cluster admin, can create CRDs. This is a known limitation of any multi-tenancy environment based on a single shared control plane.

## Assign custom resources quota

Kubernetes offers by default `ResourceQuota` resources, aimed to limit the number of basic primitives in a Namespace.

Capsule already provides the sharing of these constraints across the Tenant Namespaces, however, limiting the amount of namespaced Custom Resources instances is not upstream-supported.

Starting from Capsule **v0.1.1**, this can be done using a special annotation in the Tenant manifest.

Imagine the case where a Custom Resource named `MySQL` in the API group `databases.acme.corp/v1` usage must be limited in the Tenant `oil`: this can be done as follows.

```yaml
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
  annotations:
    quota.resources.capsule.clastix.io/mysqls.databases.acme.corp_v1: "3"
spec:
  additionalRoleBindings:
  - clusterRoleName: mysql-namespace-admin
    subjects:
      - kind: User
        name: alice
  owners:
  - name: alice
    kind: User
```

> The Additional Role Binding referring to the Cluster Role `mysql-namespace-admin` is required to let Alice manage their Custom Resource instances.

> The pattern for the `quota.resources.capsule.clastix.io` annotation is the following:
> `quota.resources.capsule.clastix.io/${PLURAL_NAME}.${API_GROUP}_${API_VERSION}`
>
> You can figure out the required fields using `kubectl api-resources`.

When `alice` will create a `MySQL` instance in one of their Tenant Namespace, the Cluster Administrator can easily retrieve the overall usage.

```yaml
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
  annotations:
    quota.resources.capsule.clastix.io/mysqls.databases.acme.corp_v1: "3"
    used.resources.capsule.clastix.io/mysqls.databases.acme.corp_v1: "1"
spec:
  owners:
  - name: alice
    kind: User
```

> This feature is still in an alpha stage and requires a high amount of computing resources due to the dynamic client requests.

## Assign Additional Metadata
The cluster admin can _"taint"_ the namespaces created by tenant owners with additional metadata as labels and annotations. There is no specific semantic assigned to these labels and annotations: they will be assigned to the namespaces in the tenant as they are created. This can help the cluster admin to implement specific use cases as, for example, leave only a given tenant to be backed up by a backup service.

Assigns additional labels and annotations to all namespaces created in the `oil` tenant: 

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: alice
    kind: User
  namespaceOptions:
    additionalMetadata:
      annotations:
        storagelocationtype: s3
      labels:
        capsule.clastix.io/backup: "true"
EOF
```

When the tenant owner creates a namespace, it inherits the given label and/or annotation:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  annotations:
    storagelocationtype: s3
  labels:
    capsule.clastix.io/tenant: oil
    kubernetes.io/metadata.name: oil-production
    name: oil-production
    capsule.clastix.io/backup: "true"
  name: oil-production
  ownerReferences:
  - apiVersion: capsule.clastix.io/v1beta2
    blockOwnerDeletion: true
    controller: true
    kind: Tenant
    name: oil
spec:
  finalizers:
  - kubernetes
status:
  phase: Active
```

Additionally, the cluster admin can _"taint"_ the services created by the tenant owners with additional metadata as labels and annotations.

Assigns additional labels and annotations to all services created in the `oil` tenant: 

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: alice
    kind: User
  serviceOptions:
    additionalMetadata:
      labels:
        capsule.clastix.io/backup: "true"
EOF
```

When the tenant owner creates a service in a tenant namespace, it inherits the given label and/or annotation:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: nginx
  namespace: oil-production
  labels:
    capsule.clastix.io/backup: "true"
spec:
  ports:
  - protocol: TCP
    port: 80 
    targetPort: 8080 
  selector:
    run: nginx
  type: ClusterIP 
```

## Cordon a Tenant

Bill needs to cordon a Tenant and its Namespaces for several reasons:

- Avoid accidental resource modification(s) including deletion during a Production Freeze Window
- During the Kubernetes upgrade, to prevent any workload updates
- During incidents or outages
- During planned maintenance of a dedicated nodes pool in a BYOD scenario

With this said, the Tenant Owner and the related Service Account living into managed Namespaces, cannot proceed to any update, create or delete action.

This is possible by just toggling the specific Tenant specification:

```shell
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  cordoned: true
  owners:
  - kind: User
    name: alice
```

Any operation performed by Alice, the Tenant Owner, will be rejected by the Admission controller.

Uncordoning can be done by removing the said specification key:

```shell
$ cat <<EOF | kubectl apply -f -
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  cordoned: false
  owners:
  - kind: User
    name: alice
EOF

$ kubectl --as alice --as-group capsule.clastix.io -n oil-dev create deployment nginx --image nginx
deployment.apps/nginx created
```

Status of cordoning is also reported in the `state` of the tenant:

```shell
kubectl get tenants
NAME     STATE    NAMESPACE QUOTA   NAMESPACE COUNT   NODE SELECTOR    AGE
bronze   Active                     2                                  3d13h
gold     Active                     2                                  3d13h
oil      Cordoned                   4                                  2d11h
silver   Active                     2                                  3d13h
```


## Deny Service Types
Bill, the cluster admin, can prevent the creation of services with specific service types.

### NodePort
When dealing with a _shared multi-tenant_ scenario, multiple _NodePort_ services can start becoming cumbersome to manage. The reason behind this could be related to the overlapping needs by the Tenant owners, since a _NodePort_ is going to be open on all nodes and, when using `hostNetwork=true`, accessible to any _Pod_ although any specific `NetworkPolicy`.

Bill, the cluster admin, can block the creation of services with `NodePort` service type for a given tenant

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: alice
    kind: User
  serviceOptions:
    allowedServices:
      nodePort: false
EOF
```

With the above configuration, any attempt of Alice to create a Service of type `NodePort` is denied by the Validation Webhook enforcing it. Default value is `true`.

### ExternalName
Service with the type of `ExternalName` has been found subject to many security issues. To prevent tenant owners to create services with the type of `ExternalName`, the cluster admin can prevent a tenant to create them:

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: alice
    kind: User
  serviceOptions:
    allowedServices:
      externalName: false
EOF
```

With the above configuration, any attempt of Alice to create a Service of type `externalName` is denied by the Validation Webhook enforcing it. Default value is `true`.

### LoadBalancer

Same as previously, the Service of type of `LoadBalancer` could be blocked for various reasons. To prevent tenant owners to create these kinds of services, the cluster admin can prevent a tenant to create them:

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: alice
    kind: User
  serviceOptions:
    allowedServices:
      loadBalancer: false
EOF
```

With the above configuration, any attempt of Alice to create a Service of type `LoadBalancer` is denied by the Validation Webhook enforcing it. Default value is `true`.


## Deny Wildcard Hostname in Ingresses

Bill, the cluster admin, can deny the use of wildcard hostname in Ingresses. Let's assume that **Acme Corp.** uses the domain `acme.com`.

As a tenant owner of `oil`, Alice creates an Ingress with the host like `- host: "*.acme.com"`. That can lead problems for the `water` tenant because Alice can deliberately create ingress with host: `water.acme.com`.

To avoid this kind of problems, Bill can deny the use of wildcard hostnames in the following way:

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
    - name: alice
      kind: User
  ingressOptions:
    allowWildcardHostnames: false
EOF
```

Doing this, Alice will not be able to use `water.acme.com`, being the tenant owner of `oil` and `gas` only.

## Deny labels and annotations on Namespaces

By default, capsule allows tenant owners to add and modify any label or annotation on their namespaces. 

But there are some scenarios, when tenant owners should not have an ability to add or modify specific labels or annotations (for example, this can be labels used in [Kubernetes network policies](https://kubernetes.io/docs/concepts/services-networking/network-policies/) which are added by cluster administrator).

Bill, the cluster admin, can deny Alice to add specific labels and annotations on namespaces:

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  namespaceOptions:
    forbiddenAnnotations:
      denied:
          - foo.acme.net
          - bar.acme.net
      deniedRegex: .*.acme.net 
    forbiddenLabels:
      denied:
          - foo.acme.net
          - bar.acme.net
      deniedRegex: .*.acme.net
  owners:
  - name: alice
    kind: User
EOF
```

## Deny labels and annotations on Nodes

When using `capsule` together with [capsule-proxy](https://github.com/clastix/capsule-proxy), Bill can allow Tenant Owners to [modify Nodes](/docs/proxy/overview).

By default, it will allow tenant owners to add and modify any label or annotation on their nodes. 

But there are some scenarios, when tenant owners should not have an ability to add or modify specific labels or annotations (there are some types of labels or annotations, which must be protected from modifications - for example, which are set by `cloud-providers` or `autoscalers`).

Bill, the cluster admin, can deny Tenant Owners to add or modify specific labels and annotations on Nodes:

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: CapsuleConfiguration
metadata:
  name: default 
spec:
  nodeMetadata:
    forbiddenAnnotations:
      denied:
        - foo.acme.net
        - bar.acme.net
      deniedRegex: .*.acme.net
    forbiddenLabels:
      denied:
        - foo.acme.net
        - bar.acme.net
      deniedRegex: .*.acme.net
  userGroups:
    - capsule.clastix.io
    - system:serviceaccounts:default
EOF
```

> **Important note**
>
>Due to [CVE-2021-25735](https://github.com/kubernetes/kubernetes/issues/100096) this feature is only supported for Kubernetes version older than:
>* v1.18.18
>* v1.19.10
>* v1.20.6
>* v1.21.0

## Protecting tenants from deletion

Sometimes it is important to protect business critical tenants from accidental deletion. 
This can be achieved by toggling `preventDeletion` specification key on the tenant:

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta2
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: alice
    kind: User
  preventDeletion: true
EOF
```

## Replicating resources across a set of Tenants' Namespaces

When developing an Internal Developer Platform the Platform Administrator could want to propagate a set of resources.
These could be Secret, ConfigMap, or other kinds of resources that the tenants would require to use the platform.

> A generic example could be the container registry secrets, especially in the context where the Tenants can just use a specific registry.

Starting from Capsule v0.2.0, a new set of Custom Resource Definitions have been introduced, such as the `GlobalTenantResource`, let's start with a potential use-case using the personas described at the beginning of this document.

**Bill** created the Tenants for **Alice** using the `Tenant` CRD, and labels these resources using the following command:

```
$: kubectl label tnt/oil energy=fossil
tenant oil labeled

$: kubectl label tnt/gas energy=fossil
tenant oil labeled
```

In the said scenario, these Tenants must use container images from a trusted registry, and that would require the usage of specific credentials for the image pull.

The said container registry is deployed in the cluster in the namespace `harbor-system`, and this Namespace contains all image pull secret for each Tenant, e.g.: a secret named `harbor-system/fossil-pull-secret` as follows.

```
$: kubectl -n harbor-system get secret --show-labels
NAME                 TYPE     DATA   AGE   LABELS
fossil-pull-secret   Opaque   1      28s   tenant=fossil
```

These credentials would be distributed to the Tenant owners manually, or vice-versa, the owners would require those.
Such a scenario would be against the concept of the self-service solution offered by Capsule, and **Bill** can solve this by creating the `GlobalTenantResource` as follows.

```yaml
apiVersion: capsule.clastix.io/v1beta2
kind: GlobalTenantResource
metadata:
  name: fossil-pull-secrets
spec:
  tenantSelector:
    matchLabels:
      energy: fossil
  resyncPeriod: 60s
  resources:
    - namespacedItems:
        - apiVersion: v1
          kind: Secret
          namespace: harbor-system
          selector:
            matchLabels:
              tenant: fossil
```

A full reference of the API is available in the [CRDs API section](/docs/general/crds-apis), just explaining the expected behaviour and the resulting outcome:

> Capsule will select all the Tenant resources according to the key `tenantSelector`.
> Each object defined in the `namespacedItems` and matching the provided `selector` will be replicated into each Namespace bounded to the selected Tenants.
> Capsule will check every 60 seconds if the resources are replicated and in sync, as defined in the key `resyncPeriod`.

The `GlobalTenantResource` is a cluster-scoped resource, thus it has been designed for cluster administrators and cannot be used by Tenant owners: for that purpose, the `TenantResource` one can help.

## Replicating resources across Namespaces of a Tenant

Although Capsule is supporting a few amounts of personas, it can be used to allow building an Internal Developer Platform used barely by Tenant owners, or users created by these thanks to Service Account.

In a such scenario, a Tenant Owner would like to distribute resources across all the Namespace of their Tenant, without the need to establish a manual procedure, or the need for writing a custom automation.

The Namespaced-scope API `TenantResource` allows to replicate resources across the Tenant's Namespace.

> The Tenant owners must have proper RBAC configured in order to create, get, update, and delete their `TenantResource` CRD instances.
> This can be achieved using the Tenant key `additionalRoleBindings` or a custom Tenant owner role, compared to the default one (`admin`).

For our example, **Alice**, the project lead for the `solar` tenant, wants to provision automatically a **DataBase** resource for each Namespace of their Tenant: these are the Namespace list.

```
$: kubectl get namespaces -l capsule.clastix.io/tenant=solar --show-labels
NAME           STATUS   AGE   LABELS
solar-1        Active   59s   capsule.clastix.io/tenant=solar,environment=production,kubernetes.io/metadata.name=solar-1,name=solar-1
solar-2        Active   58s   capsule.clastix.io/tenant=solar,environment=production,kubernetes.io/metadata.name=solar-2,name=solar-2
solar-system   Active   62s   capsule.clastix.io/tenant=solar,kubernetes.io/metadata.name=solar-system,name=solar-system
```

**Alice** creates a `TenantResource` in the Tenant namespace `solar-system` as follows.

```yaml
apiVersion: capsule.clastix.io/v1beta2
kind: TenantResource
metadata:
  name: solar-db
  namespace: solar-system
spec:
  resyncPeriod: 60s
  resources:
    - namespaceSelector:
        matchLabels:
          environment: production
      rawItems:
        - apiVersion: postgresql.cnpg.io/v1
          kind: Cluster
          metadata:
            name: postgresql
          spec:
            description: PostgreSQL cluster for the Solar project
            instances: 3
            postgresql:
              pg_hba:
                - hostssl app all all cert
            primaryUpdateStrategy: unsupervised
            storage:
              size: 1Gi
```

The expected result will be the object `Cluster` for the API version `postgresql.cnpg.io/v1` to get created in all the Solar tenant namespaces matching the label selector declared by the key `namespaceSelector`.

```
$: kubectl get clusters.postgresql.cnpg.io -A
NAMESPACE   NAME         AGE   INSTANCES   READY   STATUS                     PRIMARY
solar-1     postgresql   80s   3           3       Cluster in healthy state   postgresql-1
solar-2     postgresql   80s   3           3       Cluster in healthy state   postgresql-1
```

The `TenantResource` object has been created in the namespace `solar-system` that doesn't satisfy the Namespace selector. Furthermore, Capsule will automatically inject the required labels to avoid a `TenantResource` could start polluting other Namespaces.

Eventually, using the key `namespacedItem`, it is possible to reference existing objects to get propagated across the other Tenant namespaces: in this case, a Tenant Owner can just refer to objects in their Namespaces, preventing a possible escalation referring to non owned objects.

As with `GlobalTenantResource`, the full reference of the API is available in the [CRDs API section](/docs/general/crds-apis).

## Preventing PersistentVolume cross mounting across Tenants

Any Tenant owner is able to create a `PersistentVolumeClaim` that, backed by a given _StorageClass_, will provide volumes for their applications.

In most cases, once a `PersistentVolumeClaim` is deleted, the bounded `PersistentVolume` will be recycled due.

However, in some scenarios, the `StorageClass` or the provisioned `PersistentVolume` itself could change the retention policy of the volume, keeping it available for recycling and being consumable for another Pod.

In such a scenario, Capsule enforces the Volume mount only to the Namespaces belonging to the Tenant on which it's been consumed, by adding a label to the Volume as follows.

```yaml
apiVersion: v1
kind: PersistentVolume
metadata:
  annotations:
    pv.kubernetes.io/provisioned-by: rancher.io/local-path
  creationTimestamp: "2022-12-22T09:54:46Z"
  finalizers:
  - kubernetes.io/pv-protection
  labels:
    capsule.clastix.io/tenant: atreides
  name: pvc-1b3aa814-3b0c-4912-9bd9-112820da38fe
  resourceVersion: "2743059"
  uid: 9836ae3e-4adb-41d2-a416-0c45c2da41ff
spec:
  accessModes:
  - ReadWriteOnce
  capacity:
    storage: 10Gi
  claimRef:
    apiVersion: v1
    kind: PersistentVolumeClaim
    name: melange
    namespace: caladan
    resourceVersion: "2743014"
    uid: 1b3aa814-3b0c-4912-9bd9-112820da38fe
```

Once the `PeristentVolume` become available again, it can be referenced by any `PersistentVolumeClaim` in the `atreides` Tenant Namespace resources.

If another Tenant, like `harkonnen`, tries to use it, it will get an error:

```
$: k describe pv pvc-9788f5e4-1114-419b-a830-74e7f9a33f5d
Name:              pvc-9788f5e4-1114-419b-a830-74e7f9a33f5d
Labels:            capsule.clastix.io/tenant=atreides
Annotations:       pv.kubernetes.io/provisioned-by: rancher.io/local-path
Finalizers:        [kubernetes.io/pv-protection]
StorageClass:      standard
Status:            Available
...

$: cat /tmp/pvc.yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: melange
  namespace: harkonnen
spec:
  storageClassName: standard
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 3Gi
  volumeName: pvc-9788f5e4-1114-419b-a830-74e7f9a33f5d

$: k apply -f /tmp/pvc.yaml
Error from server: error when creating "/tmp/pvc.yaml": admission webhook "pvc.capsule.clastix.io" denied the request: PeristentVolume pvc-9788f5e4-1114-419b-a830-74e7f9a33f5d cannot be used by the following Tenant, preventing a cross-tenant mount
```

---

This ends our tutorial on how to implement complex multi-tenancy and policy-driven scenarios with Capsule. As we improve it, more use cases about multi-tenancy, policy admission control, and cluster governance will be covered in the future.

Stay tuned!
