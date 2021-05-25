# Enforcing Pod Priority Classes

> Pods can have priority. Priority indicates the importance of a Pod relative to other Pods.
> If a Pod cannot be scheduled, the scheduler tries to preempt (evict) lower priority Pods to make scheduling of the pending Pod possible.
> 
> [Kubernetes documentation](https://kubernetes.io/docs/concepts/scheduling-eviction/pod-priority-preemption/) 

In a multi-tenant cluster where not all users are trusted, a tenant owner could create Pods at the highest possible priorities, causing other Pods to be evicted/not get scheduled.

At the current state, Capsule doesn't have, yet, a CRD key to handle the enforced [Priority Class](https://kubernetes.io/docs/concepts/scheduling-eviction/pod-priority-preemption/#priorityclass).

Enforcement is feasible using the Tenant's annotations field, as following:

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: oil
  annotations:
    priorityclass.capsule.clastix.io/allowed: default
    priorityclass.capsule.clastix.io/allowed-regex: "^tier-.*$"
spec:
  owner:
    kind: User
    name: alice
```

With the said Tenant specification Alice can create Pod resource if `spec.priorityClassName` equals to:

- `default`, as mentioned in the annotation `priorityclass.capsule.clastix.io/allowed`
- `tier-gold`, `tier-silver`, or `tier-bronze`, since these compile the regex declared in the annotation `priorityclass.capsule.clastix.io/allowed-regex`

If a Pod is going to use a non-allowed _Priority Class_, it will be rejected by the Validation Webhook enforcing it.
