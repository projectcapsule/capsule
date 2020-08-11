# Some use cases for Capsule

Capsule can workaround the flat structure of namespaces in Kubernetes by introducing a lightweight abstraction called _Tenant_. Within each tenant, users are free to create their namespaces and share all the assigned resources between the namespaces without the intervention of the cluster admin. Using Capsule administrators can implement complex multi-tenants scenarios like, for example, in a public or private Container-as-a-Service (CaaS) platform. Here an initial list of common scenarios addressed by Capsule.

Please, feel free to share your comments and propose new scenarios.

## Acme Corp. Container as a Service (CaaS)
Acme Corp. wants provides to its customers with a new CaaS service based on Kubernetes. To simplify the usage of Capsule, we'll work with the following actors:

* *Bill*:
  he is the cluster administrator from the operations department of Acme Corp.
  and he is in charge of admin and maintains the CaaS platform.
  Bill is also responsible for the onboarding of new customers and of the daily work to support all customers.

* *Alice*:
  she works as IT Project Leader at Oil Inc., a new customer of the
  Acme Corp. CaaS service.
  Alice is responsible for all the strategic IT projects
  and she is responsible also for a large team made of different background
  (developers, administrators, SRE engineers, etc.) and organized in separate departments.

* *Joe*:
  he works at Oil Inc., as a lead developer of a distributed team in Alice's organization.
  Joe is responsible for developing a mission-critical project at Oil Inc.

Acme Corp. can use Capsule to address the following scenarios:

* [Onboarding of a new customer](#onboarding-of-a-new-customer)
* [Create multiple namespaces in the tenant](#create-multiple-namespaces-in-the-tenant)
* [Assign permissions roles in the tenant](#assign-permissions-roles-in-the-tenant)
* [Resources quota enforcement in the tenant](#resources-quota-enforcement-in-the-tenant)
* [Control the placement of pods in the tenant](#control-the-placement-of-pods-in-the-tenant)
* [Control the Ingress selector in the tenant](#control-the-ingress-selector-in-the-tenant)
* [Assign Storage classes in the tenant](#assign-storage-classes-in-the-tenant)
* [Set network policies in the tenant](#set-network-policies-in-the-tenant)


### Onboarding of a new customer
Bill receives a new request from the CaaS onboarding system that a new
customer "Oil Inc." has to be on board. This request
reports the name of the tenant owner and the total amount of purchased
resources: namespaces, CPU, memory, storage, ...

Bill creates a new user account id `alice` in the Acme Corp. identity management
system and assign her to the group of the Capsule users. To keep the things
simple, we assume that Bill just creates a certificate for authentication on
the CaaS platform using X.509 certificate, so Alice's certificate has
`"/CN=alice/O=capsule.clastix.io"`.

Bill creates a new tenant `oil` in the CaaS manangement portal
according to the tenant's profile:

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  labels:
  annotations:
  name: oil
spec:
  owner: alice
  nodeSelector:
    vpc: oil
  ingressClasses:
  - haproxy
  storageClasses:
  - ceph-rbd
  namespaceQuota: 3
  resourceQuotas:
    - hard:
        limits.cpu: "8"
        limits.memory: 16Gi
        requests.cpu: "8"
        requests.memory: 16Gi
      scopes: ["NotTerminating"]
    - hard:
        pods : "10"
        services: "5"
    - spec:
        hard:
          requests.storage: "100Gi"
  limitRanges:
    - limits:
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
  networkPolicies:
    - policyTypes:
      - Ingress
      - Egress
      podSelector: {}
      ingress:
      - from:
        - namespaceSelector:
            matchLabels:
              tenant: oil
        - podSelector: {}
        - ipBlock:
            cidr: 192.168.0.0/16
      egress:
      - to:
        - ipBlock:
            cidr: 0.0.0.0/0
            except:
            - 192.168.0.0/16
```

Bill checks the new tenant is created and operational:

```
bill@caas# kubectl get tenants
NAME   NAMESPACE QUOTA   NAMESPACE COUNT   OWNER   AGE
oil    3                 0                 alice   3m
foo    10                9                 bar     30d
```

> Note that namespaces are not yet assigned to the new tenant.
> The CaaS users are free to create their namespaces in a self-service fashion
> and without any intervention from Bill.

Once the new tenant `oil` is in place, Bill sends the login
credentials to Alice along with the other relevant tenant details, for logging into the CaaS.

Alice logs into the CaaS by using her credentials and being part of the
`capsule.clastix.io` users group, she inherits the following authorization:

```yaml
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  labels:
  name: namespace:provisioner
rules:
- apiGroups: [""]
  resources: ["namespaces"]
  verbs: ["create"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: namespace:provisioner
subjects:
  - kind: Group
    name: capsule.clastix.io
roleRef:
  kind: ClusterRole
  name: namespace:provisioner
  apiGroup: rbac.authorization.k8s.io
```

Alice can log in to the CaaS platform and checks if she can create a namespace

```
alice@caas# kubectl auth can-i create namespaces
Warning: resource 'namespaces' is not namespace scoped
yes
``` 

or even delete the namespace

```
alice@caas# kubectl auth can-i delete ns -n oil-production
Warning: resource 'namespaces' is not namespace scoped
yes
```

However, cluster resources are not accessible to Alice

```
alice@caas# kubectl auth can-i get namespaces
Warning: resource 'namespaces' is not namespace scoped
no

alice@caas# kubectl auth can-i get nodes
Warning: resource 'nodes' is not namespace scoped
no

alice@caas# kubectl auth can-i get persistentvolumes
Warning: resource 'persistentvolumes' is not namespace scoped
no
```

including the `Tenant` resources

```
alice@caas# kubectl auth can-i get tenants
Warning: resource 'tenants' is not namespace scoped
no
```

### Create multiple namespaces in the tenant
Alice can create a new namespace in her tenant, as simply:

```
alice@caas# kubectl create ns oil-production
```

> Note that Alice started the name of her namespace with an identifier of her
> tenant: this is not a strict requirement but it is highly suggested because
> it is likely that many different users would like to call their namespaces
> as `production`, `test`, or `demo`, etc.
> 
> The enforcement of this naming convention, however, is optional and can be controlled by the cluster administrator with the `--force-tenant-prefix` option as argument of the Capsule controller.

When Alice creates the namespace, the Capsule controller, listening for creation
and deletion events assigns to Alice the following roles:

```yaml
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: namespace:admin
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
  name: namespace:deleter
  namespace: oil-production
subjects:
- kind: User
  name: alice
roleRef:
  kind: ClusterRole
  name: namespace:deleter
  apiGroup: rbac.authorization.k8s.io
```

If Alice inspects the namespace, she will see something like this:

```yaml
# kubectl get ns oil-production -o yaml
  
apiVersion: v1
kind: Namespace
metadata:
  annotations:
    capsule.clastix.io/ingress-classes: haproxy
    capsule.clastix.io/storage-classes: ceph-rbd
    scheduler.alpha.kubernetes.io/node-selector: vpc=oil
  creationTimestamp: "2020-05-27T13:49:30Z"
  labels:
    capsule.clastix.io/tenant: oil
  name: oil-production
  resourceVersion: "1651593"
  selfLink: /api/v1/namespaces/oil-production
  uid: e3b2efd4-a020-11ea-bba9-566fc1cb01af
spec:
  finalizers:
  - kubernetes
status:
  phase: Active
```

Alice is the admin of the namespace:

```
alice@caas# kubectl get rolebindings -n oil-production
NAME              ROLE                AGE
namespace:admin   ClusterRole/admin   9m5s 
namespace:deleter ClusterRole/admin   9m5s 
```

The said Role Binding resources are automatically created by Capsule when Alice creates a namespace in the tenant.

Alice can deploy any resource in the namespace, according to the predefined
[`admin` cluster role](https://kubernetes.io/docs/reference/access-authn-authz/rbac/#user-facing-roles). Or she can create additional namespaces, according to the `namespaceQuota` field of the tenant manifest:

```
alice@caas# kubectl create ns oil-development
alice@caas# kubectl create ns oil-test
```

While Alice creates Namespace resources, the Capsule controller updates the
status of the tenant so Bill, the cluster admin, can check its status:

```
bill@caas# kubectl describe tenant oil
```

```yaml
...
Status:
  Namespaces:
    oil-development
    oil-production
    oil-test
  Size:  3 # current namespace count
...
```

Once the namespace quota assigned to the tenant has been reached, Alice cannot create further namespaces

```
alice@caas# kubectl create ns oil-training
Error from server (Cannot exceed Namespace quota: please, reach out the system administrators): admission webhook "quota.namespace.capsule.clastix.io" denied the request.
```

The enforcement on the maximum number of Namespace resources per Tenant is in
charge of the Capsule controller via its Dynamic Admission Webhook capability.


### Assign permissions roles in the tenant
Alice acts as the tenant admin. Other users can operate inside the tenant with different levels of permissions and authorizations. Alice is responsible for creating roles and assigning these roles to other users to work in the same tenant.

One of the key design principles of the Capsule is the self-provisioning management from the tenant owner's perspective. Alice, the tenant owner, does not need to interact with Bill, the cluster admin, to complete her day-by-day duties. On the other side, Bill has not to deal with multiple requests coming from multiple tenant owners that probably will overwhelm him.

Capsule leaves Alice the freedom to create RBAC roles at the namespace level (or using the pre-defined roles already available in Kubernetes) and assign them to other users in the tenant according to needs and requirements. Being roles and rolebindings, limited to a namespace scope, Alice can assign the roles to the other users accessing the same tenant only after a namespace is created. This gives Alice the power to admin the tenant without asking the cluster admin.

From the cluster admin perspective, the only required action is to provision the other identities in the Identity Management system of Acme Corp. but this task can be done once, when onboarding a new tenant in the system and the users accessing the tenant can be part of the tenant business profile.

Capsule does not care about the authentication strategy used in the cluster and all the Kubernetes methods of [authentication](https://kubernetes.io/docs/reference/access-authn-authz/authentication/) are supported. The only requirement to use Capsule is Bill has to assign all the tenant identities to the `capsule.clastix.io` group.

Alice can create Roles and RoleBindings in each of the namespaces she created

```
alice@caas# kubectl auth can-i get roles
no

alice@caas# kubectl auth can-i get roles -n oil-development
yes

alice@caas# kubectl auth can-i get rolebindings -n oil-development
yes

```

so she can assign the role of namespace `oil-development` admin to Joe, another user accessing the tenant `oil`

```yaml
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
```

Joe now can operate on the namespace `oil-development` as admin but he has no access to the other namespaces `oil-production`, and `oil-test` that are part of the same tenant. 

### Resources quota enforcement in the tenant
When Alice creates the namespace `oil-production`, the Capsule controller creates
a set of namespaced objects, according to the tenant's manifest.

For example, there are three resource quotas

```yaml
kind: ResourceQuota
apiVersion: v1
metadata:
  name: compute
  namespace: oil-production
  labels:
    tenant: oil
spec:
  hard:
    limits.cpu: "8"
    limits.memory: 16Gi
    requests.cpu: "8"
    requests.memory: 16Gi
  scopes: ["NotTerminating"]
---
kind: ResourceQuota
apiVersion: v1
metadata:
  name: count
  namespace: oil-production
  labels:
    tenant: oil
spec:
  hard:
    pods : "10"
    services: "5"
---
kind: ResourceQuota
apiVersion: v1
metadata:
  name: storage
  namespace: oil-production
  labels:
    tenant: oil
spec:
  hard:
    requests.storage: "10Gi"
```

and a Limit Range:

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

In the namespace, Alice can create any resource according to the assigned Resource Quota:

```
alice@caas# kubectl -n oil-production create deployment nginx --image=nginx:latest 
```

To check the remaining quota in the `oil-production` namespace, she gets the list of resource quotas:

```
alice@caas# kubectl -n oil-production get resourcequota
NAME            AGE   REQUEST                                      LIMIT
capsule-oil-0   42h   requests.cpu: 1/8, requests.memory: 1/16Gi   limits.cpu: 1/8, limits.memory: 1/16Gi
capsule-oil-1   42h   pods: 2/10                                   
capsule-oil-2   42h   requests.storage: 0/100Gi
```

and inspecting the quota annotations:

```yaml
# kubectl get resourcequotas capsule-oil-1 -o yaml
apiVersion: v1
kind: ResourceQuota
metadata:
  annotations:
    quota.capsule.clastix.io/used-pods: "1" 
...
```

> Nota Bene:
> at Namespace level, the quota enforcement is under the control of the default
> _ResourceQuota Admission Controller_ enabled on the Kubernetes API server
> using the flag `--enable-admission-plugins=ResourceQuota`.

At the tenant level, the Capsule controller watches the Resource Quota usage for each
Tenant's namespace and adjusts it as an aggregate of all the namespaces using
the said annotation pattern (`quota.capsule.clastix.io/<quota_name>`)

The used Resource Quota counts all the used resources as an aggregate of all the
Namespace resources in the `oil` tenant namespaces:

- `oil-production`
- `oil-development`
- `oil-test` 

When the aggregate usage reaches the hard quota limits,
then the ResourceQuota Admission Controller denies Alice's request.

In addition to Resource Quota, the Capsule controller create limits ranges in each namespace according to the tenant manifest.

Alice can inspect Limit Ranges for her namespaces:

```
alice@caas# kubectl -n oil-production get limitranges
NAME            CREATED AT
capsule-oil-0   2020-07-20T18:41:15Z

# kubectl -n oil-production describe limitranges limits
Name:                  capsule-oil-0
Namespace:             oil-production
Type                   Resource  Min  Max   Default Request  Default Limit  Max Limit/Request Ratio
----                   --------  ---  ---   ---------------  -------------  -----------------------
Pod                    cpu       50m  1     -                -              -
Pod                    memory    5Mi  1Gi   -                -              -
Container              cpu       50m  1     100m             200m           -
Container              memory    5Mi  1Gi   10Mi             100Mi          -
PersistentVolumeClaim  storage   1Gi  10Gi  -                -              -
```

Being the limit range specific of single resources:

- Pod
- Container
- Persistent Volume Claim

there is no aggregate to count.

Having access to resource quota and limits, however, Alice is not able to change
or delete it according to the assigned RBAC profile.

```
alice@caas# kubectl -n oil-production auth can-i patch resourcequota
no - no RBAC policy matched

alice@caas# kubectl -n oil-production auth can-i patch limitranges
no - no RBAC policy matched
```

> Nota Bene:
> Limit ranges enforcement for a single pod, container, and persistent volume
> claim is done by the default _LimitRanger Admission Controller_ enabled on
> the Kubernetes API server: using the flag
> `--enable-admission-plugins=LimitRanger`.


### Control the placement of pods in the tenant
Bill, the cluster admin of the Acme Corp. CaaS platform can dedicate a pool of worker nodes to the `oil` tenant, to isolate the tenant applications from other noisy neighbors.

These nodes are labeled by Bill as `pool=oil`

```
bill@caas# kubectl get nodes --show-labels

NAME                      STATUS   ROLES             AGE   VERSION   LABELS
...
worker01.acme.com         Ready    worker            8d    v1.18.2   pool=caas
worker02.acme.com         Ready    worker            8d    v1.18.2   pool=caas
worker03.acme.com         Ready    worker            8d    v1.18.2   pool=caas
worker04.acme.com         Ready    worker            8d    v1.18.2   pool=caas
worker05.acme.com         Ready    worker            8d    v1.18.2   pool=caas
worker06.acme.com         Ready    worker            8d    v1.18.2   pool=oil
worker07.acme.com         Ready    worker            8d    v1.18.2   pool=oil
worker08.acme.com         Ready    worker            8d    v1.18.2   pool=oil
```

The label `pool=oil` is defined as node selector in the tenant manifest:

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  labels:
  annotations:
  name: oil
spec:
  ...
  nodeSelector:
    pool: oil
  ...
```

The Capsule controller makes sure that any namespace created in the tenant has the annotation: `scheduler.alpha.kubernetes.io/node-selector: pool=oil`. This annotation tells the scheduler of Kubernetes to assign the node selector `pool=oil` to all the pods deployed in the tenant. The effect is that all the pods deployed by Alice are placed only on the designated pool of nodes.

Any tentative of Alice to change the selector on the pods will result in the following error from
the `PodNodeSelector` Admission Controller plugin:

```
Error from server (Forbidden): pods "busybox" is forbidden:
pod node label selector conflicts with its namespace node label selector
```

RBAC prevents Alice to change the annotation on the namespace:

```
alice@caas# kubectl auth can-i edit ns -n production
Warning: resource 'namespaces' is not namespace scoped
no
```

### Control the Ingress selector in the tenant
An Ingress Controller is used in Kubernetes to publish services and applications outside of the cluster. An Ingress Controller can be provisioned to accept only Ingresses with a given Ingress Class. Bill can assign a set of dedicated Ingress Classes to the `oil` tenant to force the Ingresses in the `oil` tenant to be published only on the assigned Ingress Controller: 

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  labels:
  annotations:
  name: oil
spec:
  ...
  ingressClasses:
  - oil
  ...
```

The Capsule controller assures that all Ingresses created in the tenant can use only one of the valid Ingress Classes. This is achieved by checking the annotation `kubernetes.io/ingress.class:` in the Ingress.

Alice, as tenant owner, gets the list of valid Ingress Classes by checking any of her namespaces:

```
alice@caas# kubectl describe ns oil-production
Name:         oil-production
Labels:       capsule.clastix.io/tenant=oil
Annotations:  capsule.clastix.io/ingress-classes: oil
...
```

Alice creates an Ingress using a valid Ingress Class in the annotation:

```yaml
apiVersion: extensions/v1beta1
kind: Ingress
metadata:
  name: nginx
  namespace: oil-production
  annotations:
    kubernetes.io/ingress.class: oil
spec:
  rules:
  - host: web.oil-inc.com
    http:
      paths:
      - backend:
          serviceName: nginx
          servicePort: 80
        path: /
```

Any tentative of Alice to use a not valid Ingress Class, e.g. `default`, will fail:

```
Error from server: error when creating nginx": admission webhook "extensions.ingress.capsule.clastix.io" denied the request: Ingress Class default is forbidden for the current Tenant
```

The effect of this policy is that the services created in the tenant will be published only on the Ingress Controller designated to accept one of the valid Ingress Classes.

### Assign Storage classes for the tenant
The Acme Corp. can provide persistent storage infrastructure to their tenants. Different types of storage requirements, with different levels of QoS, eg. SSD versus HDD, are available for different tenants according to the tenant's profile. To meet these different requirements, Bill, the cluster administrator, can provision different Storage Classes and assign them to the tenant:

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  labels:
  annotations:
  name: oil
spec:
  storageClasses:
  - ceph-rbd
  - ceph-nfs
  ...
```

Alice, as tenant owner, gets the list of valid Storage Classes by checking any of the her namespaces:

```
alice@caas# kubectl describe ns oil-production
Name:         oil-production
Labels:       capsule.clastix.io/tenant=oil
Annotations:  capsule.clastix.io/storage-classes: ceph-rbd,ceph-nfs
...
```

The Capsule controller will ensure that all Persistent Volume Claims created by Alice will use only one of the assigned storage classes:

For example:

```yaml
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
```

Any tentative of Alice to use a not valid Ingress Class, e.g. `default`, will fail::
```
Error from server: error when creating persistent volume claim pvc:
admission webhook "pvc.capsule.clastix.io" denied the request:
Storage Class default is forbidden for the current Tenant
```

### Set network policies in the tenant
Kubernetes network policies allow controlling network traffic between namespaces
and between pods in the same namespace. Bill, the cluster admin, must enforce network
traffic isolation between different tenants while leaving to Alice, the tenant owner,
the freedom to set isolation between namespaces in the same tenant or even
between pods in the same namespace.

To meet this requirement, Bill needs to
define network policies that deny pods belonging to a tenant namespace to
access pods in namespaces belonging to other tenants or in system namespaces,
(e.g. `kube-system`).
Also, Bill must assure that pods belonging to a tenant namespace cannot access
other network infrastructure like cluster nodes, load balancers, and virtual
machines running other services.  

Bill can specify network policies in the tenant manifest,
according to the CaaS platform requirements:

```yaml
...
  networkPolicies:
    - policyTypes:
      - Ingress
      - Egress
      podSelector: {}
      ingress:
      - from:
        - namespaceSelector: {}
        - podSelector: {}
        - ipBlock:
            cidr: 192.168.0.0/16
      egress:
      - to:
        - ipBlock:
            cidr: 0.0.0.0/0
            except:
            - 192.168.0.0/16
```

The Capsule controller, watching for Namespace creation,
creates the Network Policies for each Namespace in the tenant.

Alice has access to these network policies:

```
alice@caas# kubectl -n oil-production get networkpolicies
NAME            POD-SELECTOR   AGE
capsule-oil-0   <none>         42h


alice@caas# kubectl -n oil-production describe networkpolicy
Name:         capsule-oil-0
Namespace:    oil-production
Created on:   2020-07-20 20:40:28 +0200 CEST
Labels:       capsule.clastix.io/network-policy=0
              capsule.clastix.io/tenant=oil
Annotations:  <none>
Spec:
  PodSelector:     <none> (Allowing the specific traffic to all pods in this namespace)
  Allowing ingress traffic:
    To Port: <any> (traffic allowed to all ports)
    From:
      NamespaceSelector: <none>
    From:
      PodSelector: <none>
    From:
      IPBlock:
        CIDR: 192.168.0.0/12
        Except: 
  Allowing egress traffic:
    To Port: <any> (traffic allowed to all ports)
    To:
      IPBlock:
        CIDR: 0.0.0.0/0
        Except: 192.168.0.0/12
  Policy Types: Ingress, Egress  
```

Alice can create, patch, and delete additional network policies within her namespaces

```
alice@caas# kubectl -n oil-production auth can-i get networkpolicies
yes

alice@caas# kubectl -n oil-production auth can-i delete networkpolicies
yes

alice@caas# kubectl -n oil-production auth can-i patch networkpolicies
yes
```

For example, she can create

```yaml
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
```

Check all the network policies

```
alice@caas# kubectl -n oil-production get networkpolicies
NAME                        POD-SELECTOR   AGE
capsule-oil-0               <none>         42h
production-network-policy   <none>         3m
```

an delete the namespace network-policies

```
alice@caas# kubectl -n oil-production delete networkpolicy production-network-policy
```


However, the Capsule controller prevents Alice to delete the tenant network policy:

```
alice@caas# kubectl -n oil-production delete networkpolicy capsule-oil-0
Error from server (Capsule Network Policies cannot be deleted: please, reach out the system administrators): admission webhook "validating.network-policy.capsule.clastix.io" denied the request: Capsule Network Policies cannot be deleted: please, reach out the system administrators
```


