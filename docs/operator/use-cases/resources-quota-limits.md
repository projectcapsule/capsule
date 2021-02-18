# Enforce resources quota and limits
With help of Capsule, Bill and the cluster admin can set and enforce resources quota and limits for the Alice's tenant

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: oil
spec:
  owner:
    name: alice
    kind: User
  resourceQuotas:
  - hard:
      limits.cpu: "8"
      limits.memory: 16Gi
      requests.cpu: "8"
      requests.memory: 16Gi
    scopes:
    - NotTerminating
  - hard:
      pods: "100"
      services: "50"
  - hard:
      requests.storage: 10Gi
  ...
```

The resources quotas above will be inherited by all the namespaces created by Alice. In our case, when Alice creates the namespace `oil-production`, Capsule creates three resource quotas:

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

Alice can create any resource according to the assigned quotas:

```
alice@caas# kubectl -n oil-production create deployment nginx --image=nginx:latest 
```

To check the remaining resources in the `oil-production` namespace, she gets the ResourceQuota:

```
alice@caas# kubectl -n oil-production get resourcequota
NAME            AGE   REQUEST                                      LIMIT
capsule-oil-0   42h   requests.cpu: 1/8, requests.memory: 1/16Gi   limits.cpu: 1/8, limits.memory: 1/16Gi
capsule-oil-1   42h   pods: 1/10                                   
capsule-oil-2   42h   requests.storage: 0/100Gi
```

By inspecting the annotations in ResourceQuota, Alice can see the used resources at tenant level and the related hard quota:

```yaml
alice@caas# kubectl get resourcequotas capsule-oil-1 -o yaml
apiVersion: v1
kind: ResourceQuota
metadata:
  annotations:
    quota.capsule.clastix.io/used-pods: "1"
    quota.capsule.clastix.io/hard-pods: "10"
...
```

At the tenant level, the Capsule controller watches the resources usage for each Tenant namespace and adjusts it as an aggregate of all the namespaces using the said annotations. When the aggregate usage reaches the hard quota, then the native `ResourceQuota` Admission Controller in Kubernetes denies the Alice's request.

Bill, the cluster admin, can also set Limit Ranges for each namespace in the Alice's tenant by defining limits in the tenant spec:

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: oil
spec:
  owner:
    name: alice
    kind: User
  limitRanges:
  - limits:
    - max:
        cpu: "1"
        memory: 1Gi
      min:
        cpu: 50m
        memory: 5Mi
      type: Pod
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
    - max:
        storage: 10Gi
      min:
        storage: 1Gi
      type: PersistentVolumeClaim 
  ...
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

Being the limit range specific of single resources, there is no aggregate to count.

Having access to resource quotas and limits, Alice still doesn't have permissions to change or delete the resources according to the assigned RBAC profile.

```
alice@caas# kubectl -n oil-production auth can-i patch resourcequota
no - no RBAC policy matched

alice@caas# kubectl -n oil-production auth can-i patch limitranges
no - no RBAC policy matched
```

# What’s next
See how Bill, the cluster admin, can assign a pool of nodes to Alice's tenant. [Assign a nodes pool](./nodes-pool.md).
