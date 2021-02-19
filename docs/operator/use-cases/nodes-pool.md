# Assign a node's pool
Bill, the cluster admin, can dedicate a pool of worker nodes to the `oil` tenant, to isolate the tenant applications from other noisy neighbors.

These nodes are labeled by Bill as `pool=oil`

```
bill@caas# kubectl get nodes --show-labels

NAME                      STATUS   ROLES             AGE   VERSION   LABELS
...
worker06.acme.com         Ready    worker            8d    v1.18.2   pool=oil
worker07.acme.com         Ready    worker            8d    v1.18.2   pool=oil
worker08.acme.com         Ready    worker            8d    v1.18.2   pool=oil
```

The label `pool=oil` is defined as node selector in the tenant manifest:

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: oil
spec:
  owner:
    name: alice
    kind: User
  nodeSelector:
    pool: oil
  ...
```

The Capsule controller makes sure that any namespace created in the tenant has the annotation: `scheduler.alpha.kubernetes.io/node-selector: pool=oil`. This annotation tells the scheduler of Kubernetes to assign the node selector `pool=oil` to all the pods deployed in the tenant.

The effect is that all the pods deployed by Alice are placed only on the designated pool of nodes.

Any attempt of Alice to change the selector on the pods will result in the following error from
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

# What’s next
See how Bill, the cluster admin, can assign an Ingress Class to Alice's tenant. [Assign Ingress Classes](./ingress-classes.md).
