# Use cases for Capsule

## Acme Corp. Public Container as a Service (CaaS) platform

Acme Corp. is a cloud provider that wants to enhance their public offer with a
new CaaS service based on Kubernetes.
Acme Corp. already provides an _Infrastructure as a Service_ (IaaS) platform
with VMs, Storage, DBaaS, and other managed traditional services.

### The background

The new CaaS service from Acme Corp. will include:

- **Shared CaaS**:

  * Shared infra and worker nodes.
  * Shared embedded registry.
  * Shared control plane.
  * Shared Public IP addresses.
  * Shared Persistent Storage.
  * Automatic backup of volumes.
  * Shared routing layer with shared wildcard certificate.
  * Multiple Namespaces isolation.
  * Single user account.
  * Resources Quotas and Limits.
  * Self Service Provisioning portal.
  * Shared Application Catalog.

- **Private CaaS**:

  * Dedicated infra and worker nodes.
  * Dedicated registry.
  * Dedicated routing layer with dedicated wildcard certificates.
  * Dedicated Public IP addresses.
  * Dedicated Persistent Storage.
  * Automatic backup of volumes.
  * Shared control plane.
  * Multiple Namespaces isolation.
  * Resources Quotas and Limits.
  * Self Service Provisioning portal.
  * Dedicated Application Catalog.
  * Multiple user accounts.
  * Optional access to VMs, Storage, Networks, DBaaS, and other managed
    traditional services from the IaaS offer.

### Involved actors

To simplify the design of Capsule, we'll work with following actors:

* *Bill*:
  he is the cluster administrator from the operations department of Acme Corp.
  and he is in charge of admin and mantain the CaaS platform.
  Bill is also responsible for the onboarding of new customers and of the
  daily work to support all customers.

* *Joe*:
  he works as DevOps engineer at Oil & Stracci Inc., a new customer of the
  Shared CaaS service.
  Joe is responsible for deploying and mantaining container based applications
  on the CaaS platform.

* *Alice*:
  she works as IT Project Leader at Bastard Bank Inc.,
  a new Private CaaS customer. Alice is responsible for a stategic IT project
  and she is responsible also for a large team made of different background
  (developers, administrators, SRE engineers, etc.) and organised in separated
  departments.


### Some scenarios:

* [onboarding of new customer](#onboarding-of-new-customer)
* [create namespaces in a tenant](#create-namespaces-in-a-tenant)
* [quota enforcement for a tenant](#quota-enforcement-for-a-tenant)
* [node selector for a tenant](#node-selector-for-a-tenant)
* [ingress selector for a tenant](#ingress-selector-for-a-tenant)
* [network policies for a tenant](#network-policies-for-a-tenant)
* [storage class for a tenant](#storage-class-for-a-tenant)
<!-- TODO: need to be implemented
* [access images registry from a tenant](#access-images-registry-from-a-tenant)
* [backup and restore in a tenant](#backup-and-restore-in-a-tenant)
* [user management](#user-management)
-->

### Onboarding of new Customer

Bill receives a new request from the CaaS onboarding system that a new
Shared CaaS customer "Oil & Stracci Inc." has to be on board. This request
reports the name of the tenant owner and the total amount of purchased
resources: namespaces, CPU, memory, storage, ...

Bill creates a new user account id `Joe` in the Acme Corp. identity management
system and assign Joe to the group of the Shared CaaS user. To keep the things
simple, we assume that Bill just creates a certificate for authentication on
the CaaS platform using X.509 certificate, so the Joe's certificate has
`"/CN=joe/O=capsule.clastix.io"`.

Bill creates a new tenant `oil-and-stracci-inc` in the CaaS manangement portal
according to the tenant's profile:

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  labels:
  annotations:
  name: oil-and-stracci-inc
spec:
  owner: joe
  nodeSelector:
    node-role.kubernetes.io/capsule: caas
  storageClasses:
  - ceph-rbd
  namespaceQuota: 10
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
        deployments: "5"
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
              tenant: oil-and-stracci-inc
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

> Note that namespaces are not yet assigned to the tenant.
> The CaaS users are free to create their namespaces in a self-service fashion
> and without any intervent from Bill.

Once the new tenant `oil-and-stracci-inc` is in place, Bill sends the login
credentials to Joe along with the tenant details, for logging into the CaaS.

Joe logs into the CaaS by using his credentials and being part of the
`capsule.clastix.io` users group, he inherits the following authorization:

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

Joe can login to the CaaS platform and checks if he can create a namespace.

```
# kubectl auth can-i create namespaces
Warning: resource 'namespaces' is not namespace scoped
yes
```  

However, cluster resources are not accessible to Joe

```
# kubectl auth can-i get namespaces
Warning: resource 'namespaces' is not namespace scoped
no

# kubectl auth can-i get nodes
Warning: resource 'nodes' is not namespace scoped
no

# kubectl auth can-i get persistentvolumes
Warning: resource 'persistentvolumes' is not namespace scoped
no
```

including the `Tenant` resources

```
# kubectl auth can-i get tenants
Warning: resource 'tenants' is not namespace scoped
no
```

### Create namespaces in a tenant

Joe can create a new namespace in his tenant, as simply:

```
# kubectl create ns oil-production
```

> Note that Joe started the name of his namespace with an identifier of his
> tenant: this is not a strict requirement but it is higly suggested because
> it is likely that many different users would like to call their namespaces
> as `production`, `test`, or `demo`, etc.
> 
> The enforcement of this rule, however, is not in charge of the Capsule
> controller and it is left to a policy engine.

When Joe creates the namespace, the Capsule controller, listening for creation
and deletion events, assigns to Joe the following roles:

```yaml
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: namespace:admin
  namespace: oil-production
subjects:
- kind: User
  name: joe
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
  name: joe
roleRef:
  kind: ClusterRole
  name: namespace:deleter
  apiGroup: rbac.authorization.k8s.io
```

If Joe inspects the namespace, he will see something like this:

```yaml
# kubectl get ns oil-production -o yaml
  
apiVersion: v1
kind: Namespace
metadata:
  annotations:
    capsule.k8s/owner: joe
    scheduler.alpha.kubernetes.io/node-selector: node-role.kubernetes.io/capsule=caas
  creationTimestamp: "2020-05-27T13:49:30Z"
  labels:
    tenant: oil-and-stracci-inc
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

Joe is the admin of the namespace:

```
# kubectl get rolebindings -n oil-production
NAME              ROLE                AGE
namespace:admin   ClusterRole/admin   9m5s 
namespace:deleter ClusterRole/admin   9m5s 
```

The said Role Binding resources are automatically created by the Capsule
controller when Joe creates a namespace in his tenant.

Joe can deploy any resource in his namespace, according to the predefined
[`admin` cluster role](https://kubernetes.io/docs/reference/access-authn-authz/rbac/#user-facing-roles). 

Also, Joe can delete the namespace

```
# kubectl auth can-i delete ns -n oil-production
Warning: resource 'namespaces' is not namespace scoped
yes
```

or he can create additional namespaces, according to the `namespaceQuota` field of the tenant manifest:

```
# kubectl create ns oil-development
# kubectl create ns oil-test
```

The enforcement on the maximum number of Namespace resources per Tenant is in
charge of the Capsule controller via a Dynamic Admission Webhook created and
managed by the Capsule controller.

While Joe creates Namespace resources, the Capsule controller updates the
status of the tenant as following:

```yaml
...
status:
  size: 3 # namespace count
  namespaces:
  - oil-production
  - oil-development
  - oil-test
...
```

### Quota enforcement for a tenant

When Joe creates the namespace `oil-production`, the Capsule controller creates
a set of namespaced objects, according to the Tenant's manifest.

For example, there are three resource quotas

```yaml
kind: ResourceQuota
apiVersion: v1
metadata:
  name: compute
  namespace: oil-production
  labels:
    tenant: oil-and-stracci-inc
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
    tenant: oil-and-stracci-inc
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
    tenant: oil-and-stracci-inc
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
    tenant: oil-and-stracci-inc
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

In their Namespace, Joe can create any resource according to the assigned
Resource Quota:

```
# kubectl -n oil-production create deployment nginx --image=nginx:latest 
```

To check the remaining quota in the `oil-production` namesapce, he can get the list of resource quotas:

```
# kubectl -n oil-production get resourcequota
NAME            AGE   REQUEST                                      LIMIT
capsule-oil-0   42h   requests.cpu: 1/8, requests.memory: 1/16Gi   limits.cpu: 1/8, limits.memory: 1/16Gi
capsule-oil-1   42h   pods: 2/10                                   
capsule-oil-2   42h   requests.storage: 0/100Gi
```

and inspecting the Quota annotations:

```yaml
# kubectl get resourcequotas capsule-oil-1 -o yaml
apiVersion: v1
kind: ResourceQuota
metadata:
  annotations:
    quota.capsule.clastix.io/used-pods: "0" 
...
```

> Nota Bene:
> at Namespace level, the quota enforcement is under the control of the default
> _ResourceQuota Admission Controller_ enabled on the Kubernetes API server
> using the flag `--enable-admission-plugins=ResourceQuota`.

At tenant level, the Capsule operator watches the Resource Quota usage for each
Tenant's Namespace and adjusts it as an aggregate of all the namespaces using
the said annotation pattern (`quota.capsule.clastix.io/<quota_name>`)

The used Resource Quota counts all the used resources as aggregate of all the
Namespace resources in the `oil-and-stracci-inc` Tenant namespaces:

- `oil-production`
- `oil-development`
- `oil-test` 

When the aggregate usage reaches the hard quota limits,
then the ResourceQuota Admission Controller denies the Joe's request.

> In addition to Resource Quota, the Capsule controller create limits ranges in
> each namespace according to the tenant manifest.
>
> Limit ranges enforcement for single pod, container, and persistent volume
> claim is done by the default _LimitRanger Admission Controller_ enabled on
> the Kubernetes API server: using the flag
> `--enable-admission-plugins=LimitRanger`.

Joe can inspect Limit Ranges for his namespaces:

```
# kubectl -n oil-production get limitranges
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

Having access to resource quota and limits, however Joe is not able to change
or delete it according to his RBAC profile.

```
# kubectl -n oil-production auth can-i patch resourcequota
no - no RBAC policy matched

# kubectl -n oil-production auth can-i patch limitranges
no - no RBAC policy matched
```

### Node selector for a Tenant

A Tenant assigned to a shared CaaS tenant, shares infra and worker nodes with
all the other shared CaaS tenants.

Bill, the cluster admin of the CaaS, dedicated a set of infra and worker nodes
to shared CaaS tenants.

These nodes have been previously labeled as `node-role.kubernetes.io/capsule=caas`
to be separated from nodes dedicated to private CaaS users

```
$ kubectl get nodes --show-labels

NAME                      STATUS   ROLES             AGE   VERSION   LABELS
master01.acme.com         Ready    master            8d    v1.18.2   node-role.kubernetes.io/capsule=caas
master02.acme.com         Ready    master            8d    v1.18.2   node-role.kubernetes.io/capsule=caas
master03.acme.com         Ready    master            8d    v1.18.2   node-role.kubernetes.io/capsule=caas
infra01.acme.com          Ready    infra             8d    v1.18.2   node-role.kubernetes.io/capsule=caas
infra02.acme.com          Ready    infra             8d    v1.18.2   node-role.kubernetes.io/capsule=caas
infra03.acme.com          Ready    infra             8d    v1.18.2   node-role.kubernetes.io/capsule=qos
infra04.acme.com          Ready    infra             8d    v1.18.2   node-role.kubernetes.io/capsule=qos
infra05.acme.com          Ready    infra             8d    v1.18.2   node-role.kubernetes.io/capsule=qos
infra06.acme.com          Ready    infra             8d    v1.18.2   node-role.kubernetes.io/capsule=qos
storage01.acme.com        Ready    storage           8d    v1.18.2   node-role.kubernetes.io/capsule=caas
storage02.acme.com        Ready    storage           8d    v1.18.2   node-role.kubernetes.io/capsule=caas
storage03.acme.com        Ready    storage           8d    v1.18.2   node-role.kubernetes.io/capsule=qos
storage04.acme.com        Ready    storage           8d    v1.18.2   node-role.kubernetes.io/capsule=qos
storage05.acme.com        Ready    storage           8d    v1.18.2   node-role.kubernetes.io/capsule=qos
storage06.acme.com        Ready    storage           8d    v1.18.2   node-role.kubernetes.io/capsule=qos
worker01.acme.com         Ready    worker            8d    v1.18.2   node-role.kubernetes.io/capsule=caas
worker02.acme.com         Ready    worker            8d    v1.18.2   node-role.kubernetes.io/capsule=caas
worker03.acme.com         Ready    worker            8d    v1.18.2   node-role.kubernetes.io/capsule=caas
worker04.acme.com         Ready    worker            8d    v1.18.2   node-role.kubernetes.io/capsule=caas
worker05.acme.com         Ready    worker            8d    v1.18.2   node-role.kubernetes.io/capsule=qos
worker06.acme.com         Ready    worker            8d    v1.18.2   node-role.kubernetes.io/capsule=qos
worker07.acme.com         Ready    worker            8d    v1.18.2   node-role.kubernetes.io/capsule=qos
worker08.acme.com         Ready    worker            8d    v1.18.2   node-role.kubernetes.io/capsule=qos
```

Bill should assure that all workload deployed by a shared CaaS users are
assigned to worker nodes labeled as `node-role.kubernetes.io/capsule=caas`.

On the Kubernetes API servers of the CaaS platform, Bill must enable the
`--enable-admission-plugins=PodNodeSelector` Admission Controller plugin.
This forces the CaaS platform to assign a dedicated selector to all pods
created in any namespace of the Tenant.

To help Bill, the Capsule controller must assure that any namespace created in
the tenant has the annotation:
`scheduler.alpha.kubernetes.io/node-selector: node-role.kubernetes.io/capsule=caas`.
The Capsule controller must force the annotation above for each namespace
created by any shared CaaS user.

For example, in the `oil-and-stracci-inc` tenant,
all pods deployed by Joe will have the selector

```yaml
...
nodeSelector:
  node-role.kubernetes.io/capsule: caas
...
```

Any temptative to change the selector, will result in the following error from
the `PodNodeSelector` Admission Controller plugin:

```
Error from server (Forbidden): error when creating "podshell.yaml": pods "busybox" is forbidden:
pod node label selector conflicts with its namespace node label selector
```

and no additional actions are required to the Capsule controller.

On the other side, a private CaaS tenant receives a dedicated set of infra e
worker nodes. Bill has to make sure that these nodes are labeled according,
for example `node-role.kubernetes.io/capsule=qos` to be separated from nodes
dedicated to other private CaaS tenants and the shared CaaS tenants.

The Capsule controller must assure that any namespace created in the tenant has
the annotation:
`scheduler.alpha.kubernetes.io/node-selector: node-role.kubernetes.io/capsule=qos`.
The Capsule controller must force the annotation above for each namespace created by any private CaaS user.

For example, in the `evil-corp` tenant, all pods deployed by Alice will have
the selector

```yaml
  ...
  nodeSelector:
    node-role.kubernetes.io/capsule: evil-corp
  ...
```

Any temptative to change the selector, will be denied byt the `PodNodeSelector`
Admission Controller plugin no additional actions are required to the
Capsule controller.

### Ingress selector for a tenant

A tenant assigned to a shared CaaS tenant shares the infra nodes with all the
other shared CaaS tenants. On these infra nodes, a single Ingress Controller is
installed and provisioned with a wildcard certificate.
All the applications within the tenant will be published as `*.caas.acme.com`

Bill provisioned an Ingress Controller on the shared CaaS to use a dedicated
ingress class: `--ingress-class=caas` as ingress selector.
All ingresses created in all the shared CaaS tenants must use this selector in
order to be published on the CaaS Ingress Controller.

The Capsule operator must assure that all ingresses created in any tenant
belonging to the shared CaaS, have the annotation
`kubernetes.io/ingress.class: caas` where the selector is specified in the
tenant resouce manifest:

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  labels:
  annotations:
  name: oil-and-stracci-inc
spec:
  ...
  ingressClass: caas
  ...
```

For example, in the `oil-production` namespace belonging to the
`oil-and-stracci-inc` tenant, Joe will see: 

```yaml
apiVersion: extensions/v1beta1
kind: Ingress
metadata:
  namespace: oil-production
  name: wordpress
  annotations:
    kubernetes.io/ingress.class: caas
spec:
  rules:
  - host: blog.caas.acme.com
    http:
      paths:
      - path: /
        backend:
          serviceName: wordpress
          servicePort: 80
```

Joe can create, change and delete `Ingress` resources, but the Capsule
controller will always force any change to the ingress selector annotation to be
`kubernetes.io/ingress.class: caas`.

On the other side, a private CaaS tenant receives a dedicated Ingress Controller
running on the infra nodes dedicated to that tenant only.
Bill provisions the dedicated Ingress Controller to use a dedicated ingress
class: `--ingress-class=evil-corp` as ingress selector and a dedicated wildcard
certificate, for example `*.evilcorp.com`. All ingresses created in the private
tenant must use this selector in order to be published on the dedicated Ingress
Controller.

The Capsule operator must assure that all ingresses created in the tenant,
have the annotation `kubernetes.io/ingress.class: evil-corp` where the selector
is specified into the tenant resouce manifest.

### Network policies for a tenant

Kubernetes network policies allow to control network traffic between namespaces
and between pods in the same namespace. The CaaS platform must enforce network
traffic isolation between different tenants while leaving to the tenant user
the freedom to set isolation between namespaces in the same tenant or even
between pods in the same namespace.

To meet this requirement, Bill, the CaaS platform administrator, needs to
define network policies that deny pods belonging to a tenant namespace to
access pods in namespaces belonging to other tenants or in system namespaces,
(e.g. `kube-system`).
Also Bill must assure that pods belonging to a tenant namespace cannot access
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
        - namespaceSelector:
            matchLabels:
              tenant: oil-and-stracci-inc
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

The tenat user (e.g. Joe) has access these network policies:

```
# kubectl -n oil-production get networkpolicies
NAME            POD-SELECTOR   AGE
capsule-oil-0   <none>         42h


# kubectl -n oil-production describe networkpolicy
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
      NamespaceSelector: capsule.clastix.io/tenant=oil
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

and he can create, patch, and delete Nework Policies

```
# kubectl -n oil-production auth can-i get networkpolicies
yes
# kubectl -n oil-production auth can-i delete networkpolicies
yes
# kubectl -n oil-production auth can-i patch networkpolicies
yes
```

However, the Caspule controller enforces the Tenant Network Policie resources
above and prevents Joe to change, or delete them.

### Storage Class for a tenant

The CaaS platform provides persistent storage infrastructure for shared and
private tenants. Different type of storage requirements, with different level
of QoS, eg. SSD versus HDD, can be provided by the platform according to the
tenants profile and needs. To meet these dirrerent requirements, Bill, the
admin of the CaaS platform, has to provision different storage classes and
assign a proper storage class to the tenants, by specifing it into the tenant
manifest:

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  labels:
  annotations:
  name: oil-and-stracci-inc
spec:
  storageClasses:
  - ceph-rbd
  ...
```

The Capsule controller will ensure that all Persistent Volume Claims created in
a Tenant will use one of the available storage classes (`ceph-rbd`,
in this case).

For example:

```yaml
kind: PersistentVolumeClaim
apiVersion: v1
metadata:
  name: pvc
  namespace:
spec:
  storageClassName: denied
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 12Gi
```

The creation of the said PVC will fail as following:
```
# kubectl apply -f my_pvc.yaml
Error from server: error when creating "/tmp/pvc.yaml":
admission webhook "pvc.capsule.clastix.io" denied the request:
Storage Class ceph-rbd is forbidden for the current Tenant
```
