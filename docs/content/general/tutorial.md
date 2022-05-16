# Implementing a multi-tenant scenario in Kubernetes

Capsule is a framework to implement multi-tenant and policy-driven scenarios in Kubernetes. In this tutorial, we'll focus on a hypothetical case covering the main features of the Capsule Operator.

***Acme Corp***, our sample organization, is building a Container as a Service platform (CaaS) to serve multiple lines of business. Each line of business has its team of engineers that are responsible for the development, deployment, and operating of their digital products. We'll work with the following actors:

* ***Bill***: the cluster administrator from the operations department of Acme Corp.

* ***Alice***: the IT Project Leader in the Oil & Gas Business Units. She is responsible for a team made of different job responsibilities (developers, administrators, SRE engineers, etc.) working in separate multiple departments.
  
* ***Joe***:
  He works at Acme Corp, as a lead developer of a distributed team in Alice's organization.

* ***Bob***:
  He is the head of Engineering for the Water Business Unit, the main and historical line of business at Acme Corp.


## Assign Tenant ownership

### Roles assigned to Tenant Owners

By default, all Tenant Owners will be granted with two ClusterRole resources using the RoleBinding API:

1. the Kubernetes default one, `admin`, that grants most of the Namespace scoped resources management operations
2. a custom one, named `capsule-namespace-deleter`, allowing to delete the created Namespace

```
$: kubectl get rolebindings.rbac.authorization.k8s.io
NAME                                      ROLE                                    AGE
capsule-oil-0-admin                       ClusterRole/admin                       6s
capsule-oil-1-capsule-namespace-deleter   ClusterRole/capsule-namespace-deleter   5s
capsule-oil-2-admin                       ClusterRole/admin                       5s
capsule-oil-3-capsule-namespace-deleter   ClusterRole/capsule-namespace-deleter   5s
```

Capsule supports the dynamic management of the assigned ClusterRole resources for each Tenant Owner.

```yaml
apiVersion: capsule.clastix.io/v1beta1
kind: Tenant
metadata:
  annotations:
    clusterrolenames.capsule.clastix.io/user.alice: editor,manager
    clusterrolenames.capsule.clastix.io/group.sre: readonly
  name: oil
spec:
  owners:
    - kind: User
      name: alice
    - kind: Group
      name: sre
```

For the given configuration, the resulting RoleBinding resources are the following ones:

```
$: kubectl get rolebindings.rbac.authorization.k8s.io
NAME                     ROLE                   AGE
capsule-oil-0-editor     ClusterRole/editor     21s
capsule-oil-1-manager    ClusterRole/manager    19s
capsule-oil-2-readonly   ClusterRole/readonly   2s
```

> The pattern for the annotation is `clusterrolenames.capsule.clastix.io/${KIND}.${NAME}`.
> The placeholders `${KIND}` and `${NAME}` are referring to the Tenant Owner specification fields, both lower-cased.

### User as tenant owner
Bill, the cluster admin, receives a new request from Acme Corp.'s CTO asking for a new tenant to be onboarded and Alice user will be the tenant owner. Bill then assigns Alice's identity of `alice` in the Acme Corp. identity management system. Since Alice is a tenant owner, Bill needs to assign `alice` the Capsule group defined by `--capsule-user-group` option, which defaults to `capsule.clastix.io`.

To keep things simple, we assume that Bill just creates a client certificate for authentication using X.509 Certificate Signing Request, so Alice's certificate has `"/CN=alice/O=capsule.clastix.io"`.

Bill creates a new tenant `oil` in the CaaS management portal according to the tenant's profile:

```yaml
kubectl create -f - << EOF
apiVersion: capsule.clastix.io/v1beta1
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
apiVersion: capsule.clastix.io/v1beta1
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
apiVersion: capsule.clastix.io/v1beta1
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
apiVersion: capsule.clastix.io/v1beta1
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: system:serviceaccount:default:robot
    kind: ServiceAccount
EOF
```

Bill can create a Service Account called `robot`, for example, in the `default` namespace and leave it to act as Tenant Owner of the `oil` tenant

```
kubectl --as system:serviceaccount:default:robot --as-group capsule.clastix.io auth can-i create namespaces
yes
```

The service account has to be part of Capsule group, so Bill has to set in the `CapsuleConfiguration`

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: CapsuleConfiguration
metadata:
  name: default
spec:
  userGroups:
  - system:serviceaccounts:default
```

since each service account in a namespace is a member of following group:

```
system:serviceaccounts:{service-account-namespace}
```

> Please, pay attention when setting a service account acting as tenant owner. Make sure you're not using the group `system:serviceaccounts` or the group `system:serviceaccounts:{capsule-namespace}` as Capsule group, otherwise you'll create a short-circuit in the Capsule controller, being Capsule itself controlled by a serviceaccount. 


## Create namespaces
Alice, once logged with her credentials, can create a new namespace in her tenant, as simply issuing:

```
kubectl create ns oil-production
```

Alice started the name of the namespace prepended by the name of the tenant: this is not a strict requirement but it is highly suggested because it is likely that many different tenants would like to call their namespaces `production`, `test`, or `demo`, etc.

The enforcement of this naming convention is optional and can be controlled by the cluster administrator with the `--force-tenant-prefix` option as an argument of the Capsule controller.

When Alice creates the namespace, the Capsule controller listening for creation and deletion events assigns to Alice the following roles:

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

So Alice is the admin of the namespaces:

```
kubectl get rolebindings -n oil-development
NAME                                      ROLE                                    AGE
capsule-oil-0-admin                       ClusterRole/admin                       5s
capsule-oil-1-capsule-namespace-deleter   ClusterRole/capsule-namespace-deleter   4s
```

The said Role Binding resources are automatically created by Capsule controller when the tenant owner Alice creates a namespace in the tenant.

Alice can deploy any resource in the namespace, according to the predefined
[`admin` cluster role](https://kubernetes.io/docs/reference/access-authn-authz/rbac/#user-facing-roles).

```
kubectl -n oil-development run nginx --image=docker.io/nginx 
kubectl -n oil-development get pods
```

Bill, the cluster admin, can control how many namespaces Alice, creates by setting a quota in the tenant manifest `spec.namespaceOptions.quota`

```yaml
apiVersion: capsule.clastix.io/v1beta1
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: alice
    kind: User
  namespaceOptions:
    quota: 3
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
  size:  3 # current namespace count
...
```

Once the namespace quota assigned to the tenant has been reached, Alice cannot create further namespaces

```
kubectl create ns oil-training
Error from server (Cannot exceed Namespace quota: please, reach out to the system administrators):
admission webhook "namespace.capsule.clastix.io" denied the request.
```
The enforcement on the maximum number of namespaces per Tenant is the responsibility of the Capsule controller via its Dynamic Admission Webhook capability.

## Assign permissions
Alice acts as the tenant admin. Other users can operate inside the tenant with different levels of permissions and authorizations. Alice is responsible for creating additional roles and assigning these roles to other users to work in the same tenant.

One of the key design principles of the Capsule is self-provisioning management from the tenant owner's perspective. Alice, the tenant owner, does not need to interact with Bill, the cluster admin, to complete her day-by-day duties. On the other side, Bill does not have to deal with multiple requests coming from multiple tenant owners that probably will overwhelm him.

Capsule leaves Alice, and the other tenant owners, the freedom to create RBAC roles at the namespace level, or using the pre-defined cluster roles already available in Kubernetes. Since roles and rolebindings are limited to a namespace scope, Alice can assign the roles to the other users accessing the same tenant only after the namespace is created. This gives Alice the power to administer the tenant without the intervention of the cluster admin.

From the cluster admin perspective, the only required action for Bill is to provide the other identities, eg. `joe` in the Identity Management system. This task can be done once when onboarding the tenant and the number of users accessing the tenant can be part of the tenant business profile.

Alice can create Roles and RoleBindings only in the namespaces she owns

```
kubectl auth can-i get roles -n oil-development
yes

kubectl auth can-i get rolebindings -n oil-development
yes
```

so she can assign the role of namespace `oil-development` admin to Joe, another user accessing the tenant `oil`

```yaml
kubectl --as alice --as-group capsule.clastix.io apply -f - << EOF
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  labels:
  name: oil-development:admin
  namespace: oil-development
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: admin
subjects:
- apiGroup: rbac.authorization.k8s.io
  kind: User
  name: joe
EOF
```

Joe now can operate on the namespace `oil-development` as admin but he has no access to the other namespaces `oil-production`, and `oil-test` that are part of the same tenant:

```
kubectl --as joe --as-group capsule.clastix.io auth can-i create pod -n oil-development
yes

kubectl --as joe --as-group capsule.clastix.io auth can-i create pod -n oil-production
no
```

> Please, note the user `joe`, in the example above, is not acting as tenant owner. He can just operate in `oil-development` namespace as admin.

## Assign multiple tenants
A single team is likely responsible for multiple lines of business. For example, in our sample organization Acme Corp., Alice is responsible for both the Oil and Gas lines of business. It's more likely that Alice requires two different tenants, for example, `oil` and `gas` to keep things isolated.

By design, the Capsule operator does not permit a hierarchy of tenants, since all tenants are at the same levels. However, we can assign the ownership of multiple tenants to the same user or group of users.

Bill, the cluster admin, creates multiple tenants having `alice` as owner:

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta1
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
apiVersion: capsule.clastix.io/v1beta1
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
apiVersion: capsule.clastix.io/v1beta1
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
apiVersion: capsule.clastix.io/v1beta1
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

When the enforcement of the naming convention with the `--force-tenant-prefix` option, is enabled, the namespaces are automatically assigned to the right tenant by Capsule because the operator does a lookup on the tenant names. If the `--force-tenant-prefix` option, is not set,   Alice needs to specify the tenant name as a label `capsule.clastix.io/tenant=<desired_tenant>` in the namespace manifest:

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
apiVersion: capsule.clastix.io/v1beta1
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
apiVersion: capsule.clastix.io/v1beta1
kind: Tenant
metadata:
  name: oil
spec:
...
  limitRanges:
    items:
    - type: Pod
      min:
        cpu: "50m"
        memory: "5Mi"
      max:
        cpu: "1"
        memory: "1Gi"
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
    - type: PersistentVolumeClaim
      min:
        storage: "1Gi"
      max:
        storage: "10Gi"
```

Limits will be inherited by all the namespaces created by Alice. In our case, when Alice creates the namespace `oil-production`, Capsule creates the following:
 
```yaml
kind: LimitRange
apiVersion: v1
metadata:
  name: limits
  namespace: oil-production
  labels:
    tenant: oil
spec:
  limits:
  - type: Pod
    min:
      cpu: "50m"
      memory: "5Mi"
    max:
      cpu: "1"
      memory: "1Gi"
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
  - type: PersistentVolumeClaim
    min:
      storage: "1Gi"
    max:
      storage: "10Gi"
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
apiVersion: capsule.clastix.io/v1beta1
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: alice
    kind: User
  priorityClasses:
    allowed:
    - default
    allowedRegex: "^tier-.*$"
EOF
```

With the said Tenant specification, Alice can create a Pod resource if `spec.priorityClassName` equals to:

- `default`
- `tier-gold`, `tier-silver`, or `tier-bronze`, since these compile the allowed regex. 

If a Pod is going to use a non-allowed _Priority Class_, it will be rejected by the Validation Webhook enforcing it.

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
apiVersion: capsule.clastix.io/v1beta1
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
apiVersion: capsule.clastix.io/v1beta1
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
apiVersion: capsule.clastix.io/v1beta1
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
      - default
      allowedRegex: ^\w+-lb$
EOF
```

Capsule assures that all Ingresses created in the tenant can use only one of the valid Ingress Classes.

Alice can create an Ingress using only an allowed Ingress Class:

```yaml
kubectl -n oil-production apply -f - << EOF
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: nginx
  namespace: oil-production
  annotations:
    kubernetes.io/ingress.class: default
spec:
  rules:
  - host: oil.acmecorp.com
    http:
      paths:
      - backend:
          serviceName: nginx
          servicePort: 80
        path: /
EOF
```

Any attempt of Alice to use a non-valid Ingress Class, or missing it, is denied by the Validation Webhook enforcing it.

## Assign Ingress Hostnames
Bill can control ingress hostnames in the `oil` tenant to force the applications to be published only using the given hostname or set of hostnames: 

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta1
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
apiVersion: capsule.clastix.io/v1beta1
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
apiVersion: capsule.clastix.io/v1beta1
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
EOF
```

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

Any attempt of Alice to use a non-valid Storage Class, or missing it, is denied by the Validation Webhook enforcing it.

## Assign Network Policies
Kubernetes network policies control network traffic between namespaces and between pods in the same namespace. Bill, the cluster admin, can enforce network traffic isolation between different tenants while leaving to Alice, the tenant owner, the freedom to set isolation between namespaces in the same tenant or even between pods in the same namespace.

To meet this requirement, Bill needs to define network policies that deny pods belonging to Alice's namespaces to access pods in namespaces belonging to other tenants, e.g. Bob's tenant `water`, or in system namespaces, e.g. `kube-system`.

Also, Bill can make sure pods belonging to a tenant namespace cannot access other network infrastructures like cluster nodes, load balancers, and virtual machines running other services.  

Bill can set network policies in the tenant manifest, according to the requirements:

```yaml
kubectl -n oil-production apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta1
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
kubectl -n oil-production apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta1
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
kubectl -n oil-production apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta1
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


## Assign Pod Security Policies
Bill, the cluster admin, can assign a dedicated Pod Security Policy (PSP) to Alice's tenant. This is likely to be a requirement in a multi-tenancy environment.

The cluster admin creates a PSP:

```yaml
kubectl -n oil-production apply -f - << EOF
apiVersion: policy/v1beta1
kind: PodSecurityPolicy
metadata:
  name: psp:restricted
spec:
  privileged: false
  # Required to prevent escalations to root.
  allowPrivilegeEscalation: false
  ...
EOF
```

Then create a _ClusterRole_ using or granting the said item

```yaml
kubectl -n oil-production apply -f - << EOF
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: psp:restricted
rules:
- apiGroups: ['policy']
  resources: ['podsecuritypolicies']
  resourceNames: ['psp:restricted']
  verbs: ['use']
EOF
```

Bill can assign this role to all namespaces in the Alice's tenant by setting it in the tenant manifest:

```yaml
kubectl -n oil-production apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta1
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: alice
    kind: User
  additionalRoleBindings:
  - clusterRoleName: psp:privileged
    subjects:
    - kind: "Group"
      apiGroup: "rbac.authorization.k8s.io"
      name: "system:authenticated"
EOF
```

With the given specification, Capsule will ensure that all Alice's namespaces will contain a _RoleBinding_ for the specified _Cluster Role_.

For example, in the `oil-production` namespace, Alice will see:

```yaml
kind: RoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: 'capsule-oil-psp:privileged'
  namespace: oil-production
  labels:
    capsule.clastix.io/role-binding: a10c4c8c48474963
    capsule.clastix.io/tenant: oil
subjects:
  - kind: Group
    apiGroup: rbac.authorization.k8s.io
    name: 'system:authenticated'
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: 'psp:privileged'
```

With the above example, Capsule is forbidding any authenticated user in `oil-production` namespace to run privileged pods and to perform privilege escalation as declared by the Cluster Role `psp:privileged`.

## Create Custom Resources
Capsule grants admin permissions to the tenant owners but is only limited to their namespaces. To achieve that, it assigns the ClusterRole [admin](https://kubernetes.io/docs/reference/access-authn-authz/rbac/#user-facing-roles) to the tenant owner. This ClusterRole does not permit the installation of custom resources in the namespaces.

In order to leave the tenant owner to create Custom Resources in their namespaces, the cluster admin defines a proper Cluster Role. For example:

```yaml
kubectl -n oil-production apply -f - << EOF
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
kubectl -n oil-production apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta1
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
apiVersion: capsule.clastix.io/v1beta1
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
apiVersion: capsule.clastix.io/v1beta1
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

## Taint namespaces
With Capsule, Bill can _"taint"_ the namespaces created by Alice with additional labels and/or annotations. There is no specific semantic assigned to these labels and annotations: they just will be assigned to the namespaces in the tenant as they are created by Alice. This can help the cluster admin to implement specific use cases. As it can be used to implement backup as a service for namespaces in the tenant.

Bill assigns additional labels and annotations to all namespaces created in the `oil` tenant: 

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta1
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
        capsule.clastix.io/backup: "true"
      labels:
        capsule.clastix.io/tenant: oil
EOF
```

When Alice creates a namespace, this will inherit the given label and/or annotation.

## Taint services
With Capsule, Bill can _"taint"_ the services created by Alice with additional labels and/or annotations. There is no specific semantic assigned to these labels and annotations: they just will be assigned to the services in the tenant as they are created by Alice. This can help the cluster admin to implement specific use cases.

Bill assigns additional labels and annotations to all services created in the `oil` tenant: 

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta1
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - name: alice
    kind: User
  serviceOptions:
    additionalMetadata:
      annotations:
        capsule.clastix.io/backup: "true"
      labels:
        capsule.clastix.io/tenant: oil
EOF
```

When Alice creates a service in a namespace, this will inherit the given label and/or annotation.

## Cordon a Tenant

Bill needs to cordon a Tenant and its Namespaces for several reasons:

- Avoid accidental resource modification(s) including deletion during a Production Freeze Window
- During the Kubernetes upgrade, to prevent any workload updates
- During incidents or outages
- During planned maintenance of a dedicated nodes pool in a BYOD scenario

With this said, the Tenant Owner and the related Service Account living into managed Namespaces, cannot proceed to any update, create or delete action.

This is possible just labeling the Tenant as follows:

```shell
kubectl label tenant oil capsule.clastix.io/cordon=enabled
tenant oil labeled
```

Any operation performed by Alice, the Tenant Owner, will be rejected by the Admission controller.

Uncordoning can be done by removing the said label:

```shell
$ kubectl label tenant oil capsule.clastix.io/cordon-
tenant.capsule.clastix.io/oil labeled

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
apiVersion: capsule.clastix.io/v1beta1
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
apiVersion: capsule.clastix.io/v1beta1
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
apiVersion: capsule.clastix.io/v1beta1
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
apiVersion: capsule.clastix.io/v1beta1
kind: Tenant
metadata:
  name: oil
  annotations:
    capsule.clastix.io/deny-wildcard: true
spec:
  owners:
  - name: alice
    kind: User
EOF
```

Doing this, Alice will not be able to use `water.acme.com`, being the tenant owner of `oil` and `gas` only.

## Deny labels and annotations on Namespaces

By default, capsule allows tenant owners to add and modify any label or annotation on their namespaces. 

But there are some scenarios, when tenant owners should not have an ability to add or modify specific labels or annotations (for example, this can be labels used in [Kubernetes network policies](https://kubernetes.io/docs/concepts/services-networking/network-policies/) which are added by cluster administrator).

Bill, the cluster admin, can deny Alice to add specific labels and annotations on namespaces:

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta1
kind: Tenant
metadata:
  name: oil
  annotations:
    capsule.clastix.io/forbidden-namespace-labels: foo.acme.net,bar.acme.net
    capsule.clastix.io/forbidden-namespace-labels-regexp: .*.acme.net
    capsule.clastix.io/forbidden-namespace-annotations: foo.acme.net,bar.acme.net
    capsule.clastix.io/forbidden-namespace-annotations-regexp: .*.acme.net
spec:
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
apiVersion: capsule.clastix.io/v1alpha1
kind: CapsuleConfiguration
metadata:
  name: default
  annotations:
    capsule.clastix.io/forbidden-node-labels: foo.acme.net,bar.acme.net
    capsule.clastix.io/forbidden-node-labels-regexp: .*.acme.net
    capsule.clastix.io/forbidden-node-annotations: foo.acme.net,bar.acme.net
    capsule.clastix.io/forbidden-node-annotations-regexp: .*.acme.net
spec:
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
This can be achieved by adding `capsule.clastix.io/protected` annotation on the tenant:

```yaml
kubectl apply -f - << EOF
apiVersion: capsule.clastix.io/v1beta1
kind: Tenant
metadata:
  name: oil
  annotations:
    capsule.clastix.io/protected: ""
spec:
  owners:
  - name: alice
    kind: User
EOF
```

---

This ends our tutorial on how to implement complex multi-tenancy and policy-driven scenarios with Capsule. As we improve it, more use cases about multi-tenancy, policy admission control, and cluster governance will be covered in the future.

Stay tuned!
