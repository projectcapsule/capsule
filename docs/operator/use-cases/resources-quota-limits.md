# Enforce resources quota and limits
With help of Capsule, Bill, the cluster admin, can set and enforce resources quota and limits for Alice's tenant.

## Resources quota
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
apiVersion: capsule.clastix.io/v1alpha1
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
no - no RBAC policy matched

kubectl -n oil-production auth can-i patch limitranges
no - no RBAC policy matched
```

# What’s next

See how Bill, the cluster admin, can enforce the PriorityClass of Pods running of Alice's tenant namespaces. [Enforce Pod Priority Classes](./pod-priority-class.md)
